package gameserver

// 钓具助手「耐力播报」：开关存 kv，搏鱼期间以 14/8 为心跳，每 5 秒用 24/27
// 临时聊天推送当前鱼的剩余耐力与超载。
//
// 数值来源见 specs/changes/stamina-report/spec.md，都是 14/8 原始 body 里的
// F32 小端字段，客户端算好后上报，服务端只读不写：
//
//	偏移 45（.1.4） 剩余体力比例 0~1，每尾新鱼重置为 1.0，长时间搏鱼单调下降
//	偏移 49（.1.5） 钓组最弱环强度 kgf，与服务端 gearBreakLimitKg 逐位一致
//
// 超载 = (剩余体力 × 标定拉力 − 钓组最弱环) ÷ 钓组最弱环，负数取 0。
// 拉得动时恒为 0%（小鱼满体力只有 0.25kgf，对 11kgf 的钓组永远是 0%），
// 只有鱼的力超过钓组承力才出正数，因此 0% 就是「能直接拉入户」。
//
// 这些字段都不进磨损账、不改结算，也不改变 14/8 的响应形状。

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	staminaReportInterval = 5 * time.Second
	staminaLoadOffset     = 45
	staminaLoadMinBytes   = staminaLoadOffset + 4
	// staminaRigLimitOffset 是同一帧里的「钓组最弱环强度 kgf」（.1.5）。实测 45 场
	// 搏鱼，它与服务端 gearBreakLimitKg 的结果在小数点后三位完全一致，所以直接读帧，
	// 不必再查装备库——装备数据缺失（读不到线组）时反而算不出分母。
	staminaRigLimitOffset   = 49
	staminaRigLimitMinBytes = staminaRigLimitOffset + 4
	// staminaRigLimitMaxKg 是合理上限，超过即视为坏帧。
	staminaRigLimitMaxKg = 500.0
)

func staminaReportKey(uid int64) string {
	return fmt.Sprintf("gear-assistant:stamina-report:%d", uid)
}

// readKVFlag 读取 kv 里的 "0"/"1" 布尔开关，供满红不爆与耐力播报共用。
func readKVFlag(ctx context.Context, db overloadProtectionReader, key string) (bool, error) {
	var raw string
	err := db.QueryRowContext(ctx, `SELECT v FROM kv WHERE k=?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch raw {
	case "1":
		return true, nil
	case "0":
		return false, nil
	default:
		return false, fmt.Errorf("开关设置无效")
	}
}

// fishStaminaFromLoad 从 14/8 原始 body 读取鱼剩余耐力比例。
// 偏移与量纲来自 2026-11-11 两次实机抓包（82 帧 / 144 帧，len=138，完整未截断）。
func fishStaminaFromLoad(body []byte) (float64, bool) {
	if len(body) < staminaLoadMinBytes {
		return 0, false
	}
	bits := binary.LittleEndian.Uint32(body[staminaLoadOffset : staminaLoadOffset+4])
	value := float64(math.Float32frombits(bits))
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1.0001 {
		return 0, false
	}
	return value, true
}

// fishRigLimitFromLoad 从 14/8 原始 body 读取钓组最弱环强度（kgf，.1.5），
// 也就是玩家原型：摩擦拉到锁轮时，整根钓组能承受的最大拉力。
func fishRigLimitFromLoad(body []byte) (float64, bool) {
	if len(body) < staminaRigLimitMinBytes {
		return 0, false
	}
	bits := binary.LittleEndian.Uint32(body[staminaRigLimitOffset : staminaRigLimitOffset+4])
	value := float64(math.Float32frombits(bits))
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > staminaRigLimitMaxKg {
		return 0, false
	}
	return value, true
}

// fishOverloadPct 返回「这条鱼现在的拉力」超出「整根钓组最大拉力」多少百分比：
//
//	鱼的当前拉力 = 标定拉力（fishFightPullKgfFor，与 27#8 同源）× 剩余体力
//	整根钓组最大拉力 = 帧内 .1.5（最弱环）
//
// 拉得动时返回 0：小鱼满体力只有 0.25kgf，对 11kgf 的钓组恒为 0%；大鱼才出正数
// （16kg 狗鱼 175%、26kg 鲤鱼 322%）。体力耗尽时分子同步归零，所以 0% 正好对应
// 「可以直接拉入户」。分子是标定值而非实测张力，因为实测张力已被泄力削平。
//
// pullKgf 由调用方在 st.mu 内用 fishFightPullKgfFor 取好再传进来：本函数只做算术，
// 不持有鱼对象，避免出锁后读 RolledFish 字段与钩确认/结算并发。
func fishOverloadPct(limitKg, pullKgf, stamina float64) (float64, bool) {
	if limitKg <= 0 || math.IsNaN(limitKg) || math.IsInf(limitKg, 0) ||
		pullKgf <= 0 || math.IsNaN(pullKgf) || math.IsInf(pullKgf, 0) ||
		stamina < 0 || math.IsNaN(stamina) || math.IsInf(stamina, 0) {
		return 0, false
	}
	current := stamina * pullKgf
	if current <= limitKg {
		return 0, true
	}
	return (current - limitKg) / limitKg * 100, true
}

// maybeReportStamina 在一条 14/8 到达时判断是否播报。
// 用上报本身当心跳做 5 秒节流，不另起 ticker：搏鱼结束、掉线或换竿后自然
// 停止，没有 goroutine 或定时器需要回收。now 由调用方注入以便测试。
//
// 顺序是「内存门控 → 读开关 → 锁内取数 → 推送」：非当前手持竿、非搏鱼态与
// 开关缓存命中时都不发 kv 查询，搏鱼期间也不再每帧查库。开关只在菜单写入后
// 失效（invalidateStaminaFlag）。
func (s *Server) maybeReportStamina(ctx *Ctx, st *FishState, now time.Time) {
	if s == nil || ctx == nil || ctx.Sess == nil || st == nil || s.DB == nil {
		return
	}
	uid := ctx.Sess.Userid
	// 仅当前手持竿：fishingState(uid) 是玩家当前选中的那根竿。
	if s.fishingState(uid) != st {
		return
	}
	value, ok := fishStaminaFromLoad(ctx.Body)
	if !ok {
		return
	}
	st.mu.Lock()
	fighting := staminaFightActiveLocked(st)
	flagCached, flagOn := st.staminaFlagCached, st.staminaFlagOn
	st.mu.Unlock()
	if !fighting {
		return
	}
	if !flagCached {
		// 读库在锁外：失败不写缓存，下一条 14/8 再试。
		on, err := readKVFlag(ctx.Sess.pushCtx(), s.DB.SQL, staminaReportKey(uid))
		if err != nil {
			return
		}
		st.mu.Lock()
		st.staminaFlagCached, st.staminaFlagOn = true, on
		st.mu.Unlock()
		flagOn = on
	}
	if !flagOn {
		return
	}
	// 节流判定与标定拉力都在锁内取好：出锁后只带值，不再碰 st 或 Pending，
	// 钩确认/结算并发改同一尾鱼时不会读到半成品。
	st.mu.Lock()
	fighting = staminaFightActiveLocked(st)
	var fishGUID string
	pull := 0.0
	if st.Pending != nil {
		fishGUID = st.Pending.FishGUID
		pull = s.fishFightPullKgfFor(st.Pending)
	}
	newFish := fishGUID != "" && st.lastStaminaFishGUID != fishGUID
	due := newFish || st.lastStaminaReportAt.IsZero() ||
		now.Sub(st.lastStaminaReportAt) >= staminaReportInterval
	allowed := fighting && due
	if allowed {
		st.lastStaminaReportAt = now
		st.lastStaminaFishGUID = fishGUID
	}
	st.mu.Unlock()
	if !allowed {
		return
	}
	text := fmt.Sprintf("[耐力] %.1f%%", value*100)
	// 超载依赖两件事：帧内上报的钓组最弱环，以及该鱼种的标定拉力。任一缺失
	// （坏帧、未知鱼种）就只报耐力，不猜。
	if limit, ok := fishRigLimitFromLoad(ctx.Body); ok {
		if pct, ok := fishOverloadPct(limit, pull, value); ok {
			text += fmt.Sprintf(" · [超载] %.0f%%", pct)
		}
	}
	ctx.Sess.Push(24, 27, buildChatEchoPush("", text, uint64(uid), 1))
}

// staminaFightActiveLocked 是播报的搏鱼态判定，调用方必须持有 st.mu。
func staminaFightActiveLocked(st *FishState) bool {
	return st.Hooked && st.Pending != nil && st.CastActive && !st.Resolved
}

// invalidateStaminaFlag 清掉进程内开关缓存，在菜单写入 kv 并提交后调用。
// 不清缓存的话，刚关掉的播报会继续按旧值播到下一条 14/8 之后。
func (s *Server) invalidateStaminaFlag(uid int64) {
	for _, st := range s.fishingStatesFor(uid) {
		st.mu.Lock()
		st.staminaFlagCached = false
		st.mu.Unlock()
	}
}

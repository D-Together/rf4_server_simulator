package gameserver

import (
	"context"
	"sort"
	"strings"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 本文件把持久化的成就与鱼种纪录转换为客户端 DTO。

// catch_log.grade 的档位：1 未达标 / 2 合格 / 3 奖杯（星）/ 4 蓝冠（超稀有）。
// recordTrophyGrade 是「渔夫稀有鱼」面板的准入门槛——只有上星及以上才入列，
// 也是水域奖杯「集齐」判定的同一条线（见 waterway_trophy.go）。
const recordTrophyGrade = 3

func unixToFiletime100ns(ts float64) uint64 {
	if ts <= 0 {
		return 0
	}
	return uint64(int64(ts*10_000_000 + 116444736000000000))
}

// buildAchievements305 返回 3/3 响应体，是**纯查询**：既不判定成就，
// 也不推送弹窗、不写状态。成就状态只在玩法事件发生时由
// evaluateAchievements 更新，打开面板永远不会改变任何东西。
func (s *Server) buildAchievements305(ctx context.Context, uid int64) (*proto.Value, error) {
	defs, err := s.DB.AchievementDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	states, err := s.DB.PlayerAchievementStates(ctx, uid)
	if err != nil {
		return nil, err
	}
	// 只读已持久化的状态，不在这里做任何补齐。
	items := make([]*proto.Value, 0, len(defs))
	for _, achID := range defs {
		st := states[achID]
		unlocked, claimed := uint32(0), uint32(0)
		if st.Unlocked {
			unlocked = 1
		}
		if st.Claimed {
			claimed = 1
		}
		items = append(items, wiregen.MustMakeObj("efnkohdcnld", map[int]*proto.Value{
			0: proto.U32(uint32(achID)),
			1: proto.U32(unlocked),
			2: proto.F32(float32(st.Progress)),
			3: proto.U32(claimed),
			4: proto.U64(unixToFiletime100ns(st.TS)),
		}))
	}
	return proto.Args(proto.S("114"), items...), nil
}

func (s *Server) buildFishTrophies323(ctx context.Context, uid int64) (*proto.Value, error) {
	rows, err := s.DB.PlayerCatchRecords(ctx, uid)
	if err != nil {
		return nil, err
	}

	// 客户端按水域分组，并在每组内按鱼种匹配唯一一张纪录卡。
	//
	// 只收**在该水域上星（grade 3）或蓝冠（grade 4）**的渔获。3/17 是「渔夫稀有鱼」
	// 面板，官服语义是奖杯陈列：没上星的鱼根本不该占位。之前的实现把每个鱼种的
	// 最重一条都塞进去，于是只要在这个水域钓过（哪怕只是合格 grade 2），面板上就
	// 会一直挂着那条"最大记录"，整片水域看着像全收集。
	//
	// 数据源 catch_log 现在是「钓到即记」：14/4 结算事务里就写下 source='catch'，
	// 14/5 入护只把同一行抬成 'keep'，14/6 放生保持 'catch'。所以放生过的鱼
	// 同样点亮面板 —— 与稀有鱼奖座、水域奖杯、307.f16 星号完全同步。
	//
	// fish_trophy_probe_all=1 是排查开关（默认关）：把该图鱼种表里的**每个**鱼种都
	// 下发一张卡（没钓过的补 0 克占位，档位一律写成「星」），客户端只会画出它本地
	// 「稀有鱼」白名单里的那几种。用它一次截图就能把白名单反推出来——服务端静态
	// 数据里没有这份清单，客户端拿到白名单外的卡会直接丢掉。
	// fish_trophy_probe_place 可只对单个水域（如 level_001_lake）生效，避免整片面板
	// 被占位卡刷满。
	probeAll := s.tunableF("fish_trophy_probe_all", 0) >= 1
	probePlace := strings.TrimSpace(s.tunableS("fish_trophy_probe_place", ""))
	minGrade := int64(recordTrophyGrade)
	if probeAll {
		minGrade = 1
	}
	groups := make(map[string][]store.CatchRecord)
	seen := make(map[string]bool)
	for _, r := range rows {
		if r.FishKey == "" || r.Grade < minGrade {
			continue
		}
		placeID := r.LocLevel
		if placeID == "" {
			placeID = r.Loc
		}
		if placeID == "" {
			continue
		}
		if probeAll && (probePlace == "" || placeID == probePlace) {
			r.Grade = recordTrophyGrade
		}
		groups[placeID] = append(groups[placeID], r)
		seen[placeID+"\x00"+r.FishKey] = true
	}
	if probeAll {
		byPlace, err := s.probeTrophyRoster(ctx)
		if err != nil {
			return nil, err
		}
		for placeID, fishKeys := range byPlace {
			if probePlace != "" && placeID != probePlace {
				continue
			}
			for _, fishKey := range fishKeys {
				if seen[placeID+"\x00"+fishKey] {
					continue
				}
				groups[placeID] = append(groups[placeID], store.CatchRecord{
					FishKey:  fishKey,
					LocLevel: placeID,
					Grade:    recordTrophyGrade,
				})
			}
		}
	}
	placeIDs := make([]string, 0, len(groups))
	for placeID := range groups {
		placeIDs = append(placeIDs, placeID)
	}
	sort.Strings(placeIDs)

	items := make([]*proto.Value, 0, len(placeIDs))
	for _, placeID := range placeIDs {
		bestByFish := make(map[string]store.CatchRecord)
		for _, r := range groups[placeID] {
			prev, exists := bestByFish[r.FishKey]
			if !exists || r.WeightG > prev.WeightG ||
				(r.WeightG == prev.WeightG && (r.TS < prev.TS || (r.TS == prev.TS && r.ID < prev.ID))) {
				bestByFish[r.FishKey] = r
			}
		}
		fishKeys := make([]string, 0, len(bestByFish))
		for fishKey := range bestByFish {
			// 面板收录清单已知时严格按清单过滤：loc_fish 里多出来的鱼种（这张图能钓到、
			// 但官方没编进渔夫图鉴的）不下发。客户端本来就会把白名单外的卡丢掉，过滤只
			// 是为了让「面板」和「奖杯判定」用同一份分母，避免看着满、其实差几种。
			if wl := trophyFishWhitelistSet(placeID); wl != nil && !probeAll {
				if _, ok := wl[fishKey]; !ok {
					continue
				}
			}
			fishKeys = append(fishKeys, fishKey)
		}
		sort.Strings(fishKeys)
		cards := make([]*proto.Value, 0, len(fishKeys))
		for _, fishKey := range fishKeys {
			cards = append(cards, s.recordCard320FromCatch(bestByFish[fishKey]))
		}
		items = append(items, wiregen.MustMakeObj("dochkomimic", map[int]*proto.Value{
			0: proto.StrVal(placeID),
			1: proto.Args(proto.S("129"), cards...),
		}))
	}
	return proto.Args(proto.S("132"), items...), nil
}

// probeTrophyRoster 返回每个水域在 loc_fish 里的全部鱼种，只给
// fish_trophy_probe_all 排查模式用。它描述的是「这张图能钓到什么」，
// 不是「渔夫图鉴收录哪些」——后者在客户端本地，服务端拿不到。
func (s *Server) probeTrophyRoster(ctx context.Context) (map[string][]string, error) {
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT a.alias, lf.fish_key
		FROM loc_fish lf
		JOIN loc_alias a ON a.loc = lf.loc
		WHERE a.alias <> ?
		ORDER BY a.alias, lf.fish_key`, waterwayTrophyHome)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]string, 18)
	for rows.Next() {
		var placeID, fishKey string
		if err := rows.Scan(&placeID, &fishKey); err != nil {
			return nil, err
		}
		if placeID == "" || fishKey == "" {
			continue
		}
		out[placeID] = append(out[placeID], fishKey)
	}
	return out, rows.Err()
}

// recordCard320FromCatch 构造渔夫稀有鱼面板（3/17）里的一行纪录卡 320。
//
// f13（oajeeklppia）与渔护 25.f11、溜鱼 27.f22 同名，是客户端取模型族的字段：
// 不写就退回该鱼种的默认族（历史混拼资源，也就是玩家说的"淘汰的老模型"），
// 于是面板里是旧模型、点进详情（走 25/渔护）却是新族，同一条鱼两个样子。
// 这里必须用与渔护同源的 fishModelVariantForFish，并以 catch_guid 为雌雄种子。
func (s *Server) recordCard320FromCatch(r store.CatchRecord) *proto.Value {
	grade := r.Grade
	if grade <= 0 {
		grade = 1
	}
	loc := r.LocLevel
	if loc == "" {
		loc = r.Loc
	}
	hook := uint32(27)
	if len(r.Bait) >= 7 && r.Bait[:7] == "spinner" {
		hook = 17
	}
	modelKey := s.fishModelVariantForFish(r.FishKey, r.WeightG, r.LengthM, r.Grade, r.GUID)
	return wiregen.MustMakeObj("ippcfiphgfl", map[int]*proto.Value{
		0:  proto.StrVal(r.FishKey),
		1:  proto.U32(uint32(grade)),
		2:  proto.U32(uint32(r.WeightG)),
		3:  proto.F32(float32(r.LengthM)),
		4:  proto.U64(uint64(r.XP)),
		5:  proto.U32(hook),
		6:  proto.StrVal(r.Bait),
		7:  proto.StrVal(loc),
		8:  proto.U64(unixToFiletime100ns(r.TS)),
		9:  proto.U32(0),
		10: proto.U32(0),
		11: proto.U32(0),
		12: proto.U32(uint32(grade)),
		13: recordModelField(modelKey),
		14: proto.U32(0),
	})
}

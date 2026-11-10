package gameserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"rf4server/internal/store"
)

// 稀有鱼 / 超级稀有鱼 的「鱼种数」里程碑奖座。
//
// 官方语义（按**鱼种**投入，同一种鱼重复上星只算一个）：
//
//	稀有鱼（上星 grade 3 或蓝冠 grade 4）种数达到 3/5/10/15/30/50/75/100
//	    → statuette_fisherman_<n>（物品名就是「N条稀有鱼」）
//	超级稀有鱼（蓝冠 grade 4）种数达到 1/3/5/7/10/15/20/30/50
//	    → statuette_supertrophy_<n>（物品名「N条超级稀有鱼」）
//
// 档位不是拍脑袋定的：两族奖座的 item_def 与本地化名写死了档位
// （server_data/static.sqlite3 里 category=72 的 statuette_fisherman_* /
// statuette_supertrophy_*，客户端 resources.assets 的 name 字段是「3条稀有鱼」这类），
// 所以「多一档 / 少一档」都能从物品定义反查，别按经验加。
//
// 与水域奖杯一样是纪念品：卖掉、丢掉都不重发，发放记录写 kv，不能只看背包。
const (
	rarityMilestoneContainer = "backpack"
	rarityMilestoneKeyFmt    = "rarity-milestone:%d:%s:%d"
	rarityKindRare           = "rare"
	rarityKindSuperRare      = "super"
)

// catch_log.grade 的档位：1 未达标 / 2 合格 / 3 奖杯（星）/ 4 蓝冠（超稀有）。
// 本功能按「鱼种数」发放：rare 取 grade>=recordTrophyGrade，super 取 grade>=superTrophyGrade。

// rarityMilestone 是一档奖励：鱼种数达标值 + 对应奖座 item_id。
type rarityMilestone struct {
	Species int64
	ItemID  string
}

// rareSpeciesMilestones 是「N条稀有鱼」的档位（上星或蓝冠都算）。
var rareSpeciesMilestones = []rarityMilestone{
	{3, "statuette_fisherman_3"},
	{5, "statuette_fisherman_5"},
	{10, "statuette_fisherman_10"},
	{15, "statuette_fisherman_15"},
	{30, "statuette_fisherman_30"},
	{50, "statuette_fisherman_50"},
	{75, "statuette_fisherman_75"},
	{100, "statuette_fisherman_100"},
}

// superRareSpeciesMilestones 是「N条超级稀有鱼」的档位（只认蓝冠 grade 4）。
var superRareSpeciesMilestones = []rarityMilestone{
	{1, "statuette_supertrophy_1"},
	{3, "statuette_supertrophy_3"},
	{5, "statuette_supertrophy_5"},
	{7, "statuette_supertrophy_7"},
	{10, "statuette_supertrophy_10"},
	{15, "statuette_supertrophy_15"},
	{20, "statuette_supertrophy_20"},
	{30, "statuette_supertrophy_30"},
	{50, "statuette_supertrophy_50"},
}

// raritySpeciesCounts 统计全服（不分水域）已经上星 / 蓝冠的**鱼种数**。
// 同一种鱼钓多少条都只算一个，这正是官方奖座名的口径（「3条稀有鱼」= 3 种）。
func (s *Server) raritySpeciesCounts(ctx context.Context, uid int64) (rare, super int64, err error) {
	if s == nil || s.DB == nil {
		return 0, 0, nil
	}
	err = s.DB.SQL.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT CASE WHEN grade>=? THEN fish_key END),
		       COUNT(DISTINCT CASE WHEN grade>=? THEN fish_key END)
		FROM catch_log WHERE uid=?`,
		int64(recordTrophyGrade), int64(superTrophyGrade), uid).Scan(&rare, &super)
	return rare, super, err
}

// rarityMilestoneAfterKeep 在 14/5 入护落库**之后**判定奖座。
//
// 判定口径与 3/17 图鉴、307.f16 星号完全一致：都只认 catch_log。catch_log 只在
// 入护时写入（14/6 放生不写），所以奖座也只能在入护后发——否则会出现玩家
// 「结算时拿到奖座、随后放生，背包里有奖座但图鉴不计数」的错位。
//
// 入护时本尾鱼已落库，直接按当前鱼种数补发所有跨过而未发的档位即可；
// 发放与登录巡检共用 grantRarityMilestones，kv 幂等标记保证不重复。
func (s *Server) rarityMilestoneAfterKeep(ctx context.Context, uid int64) []*store.Item {
	rare, super, err := s.raritySpeciesCounts(ctx, uid)
	if err != nil {
		s.log.Warn("稀有鱼种数统计失败", "uid", uid, "err", err)
		return nil
	}
	return s.grantRarityMilestones(ctx, uid, rare, super)
}

// issueRarityMilestone 发放一档奖座：kv 幂等标记与物品同一事务提交。
// 已发过（或标记丢了但奖座还在）返回 (nil, nil)。
func (s *Server) issueRarityMilestone(ctx context.Context, uid int64, kind string, m rarityMilestone) (*store.Item, error) {
	key := fmt.Sprintf(rarityMilestoneKeyFmt, uid, kind, m.Species)
	if _, done, kerr := s.DB.KVGet(ctx, key); kerr != nil && !errors.Is(kerr, sql.ErrNoRows) {
		return nil, kerr
	} else if done {
		return nil, nil
	}
	// 标记丢了但奖座还在（手工改库/回滚）时只补标记，不再发第二枚。
	var owned int64
	if err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, m.ItemID).Scan(&owned); err != nil {
		return nil, err
	}
	if owned > 0 {
		_ = s.DB.KVSet(ctx, key, "present")
		return nil, nil
	}
	it, err := s.prepareNewItem(ctx, uid, m.ItemID, rarityMilestoneContainer, 1, "")
	if err != nil {
		return nil, err
	}
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kv(k,v) VALUES(?,?)`,
		key, strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		// 并发（结算 + 登录巡检）已经抢到这一档，静默跳过。
		return nil, nil
	}
	row, _, err := grantPreparedItemTx(ctx, tx, it, false)
	if err != nil {
		return nil, fmt.Errorf("稀有鱼里程碑奖座入包失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.log.Info("发放稀有鱼里程碑奖座", "uid", uid, "kind", kind, "species", m.Species,
		"item", m.ItemID, "guid", row.GUID)
	return row, nil
}

// grantRarityMilestones 按当前鱼种数补发所有已达标但没发过的档位（可以一次跨多档）。
// 单档失败只记日志、不影响其他档位——奖座是纪念品，宁可晚发不可整单失败。
func (s *Server) grantRarityMilestones(ctx context.Context, uid, rare, super int64) []*store.Item {
	if s == nil || s.DB == nil {
		return nil
	}
	var out []*store.Item
	grant := func(kind string, ladders []rarityMilestone, have int64) {
		for _, m := range ladders {
			if have < m.Species {
				continue
			}
			it, err := s.issueRarityMilestone(ctx, uid, kind, m)
			if err != nil {
				s.log.Warn("稀有鱼里程碑发放失败", "uid", uid, "kind", kind,
					"species", m.Species, "item", m.ItemID, "err", err)
				continue
			}
			if it != nil {
				out = append(out, it)
			}
		}
	}
	grant(rarityKindRare, rareSpeciesMilestones, rare)
	grant(rarityKindSuperRare, superRareSpeciesMilestones, super)
	return out
}

// pushRarityMilestoneItems 把新发的奖座推 4/22，让客户端背包立刻出现。
// 登录与面板兜底都走这里：只发数据库不推的话，客户端要等下次拉背包才看得见。
func pushRarityMilestoneItems(sess *Session, items []*store.Item) {
	if sess == nil || sess.Srv == nil {
		return
	}
	for _, it := range items {
		if it != nil {
			sess.Srv.pushItemUpdate(sess.Userid, it)
		}
	}
}

// rarityMilestoneLabel 反查奖座对应的档位说明，用于入护播报（「稀有鱼图鉴 3 种」）。
func rarityMilestoneLabel(itemID string) string {
	for _, m := range rareSpeciesMilestones {
		if m.ItemID == itemID {
			return fmt.Sprintf("稀有鱼图鉴 %d 种", m.Species)
		}
	}
	for _, m := range superRareSpeciesMilestones {
		if m.ItemID == itemID {
			return fmt.Sprintf("超级稀有鱼图鉴 %d 种", m.Species)
		}
	}
	return ""
}

// pushRarityStatuettesOnKeep 在 14/5 入护后推送奖座：4/22 增量进背包，
// 同时在系统频道播报一行。入护不经过 801 结算界面，没有礼物弹窗，
// 靠播报让玩家知道「刚才那条鱼解锁了奖座」。
func pushRarityStatuettesOnKeep(sess *Session, items []*store.Item) {
	if sess == nil || sess.Srv == nil {
		return
	}
	for _, it := range items {
		if it == nil {
			continue
		}
		sess.Srv.pushItemUpdate(sess.Userid, it)
		label := rarityMilestoneLabel(it.ItemID)
		if label == "" {
			continue
		}
		name := sess.Srv.lootItemName(sess.pushCtx(), it.ItemID)
		if name == "" {
			name = it.ItemID
		}
		sess.pushSystemChat(fmt.Sprintf("收藏奖座解锁：%s（%s）", name, label))
	}
}

// ensureRarityMilestonesOnce 每会话只巡检一次，供背包列表（4/1、4/4）兜底。
//
// 3/1 登录时的巡检发完物品只推了 4/22 GUID，若客户端此刻还没把背包列表拉一遍，
// 玩家会以为「没补发」。在背包列表请求前再兜一次，新奖座直接出现在列表里，
// 打开背包就能看见，不依赖推送被接收。
func (s *Session) ensureRarityMilestonesOnce() {
	if s == nil || s.Srv == nil || s.Srv.DB == nil {
		return
	}
	const stateKey = "rarity-milestone-checked"
	if _, ok := s.GetState(stateKey); ok {
		return
	}
	s.SetState(stateKey, true)
	items := s.Srv.ensureRarityMilestones(s.pushCtx(), s.Userid)
	if len(items) > 0 {
		s.log.Info("登录巡检补发稀有鱼里程碑奖座", "count", len(items))
	}
}

// ensureRarityMilestones 登录/打开奖赏面板时巡检，补发离线期间跨过的档位，
// 也覆盖「本补丁上线前就已经有很多上星鱼种」的历史存档。幂等。
func (s *Server) ensureRarityMilestones(ctx context.Context, uid int64) []*store.Item {
	if s == nil || s.DB == nil {
		return nil
	}
	rare, super, err := s.raritySpeciesCounts(ctx, uid)
	if err != nil {
		s.log.Warn("稀有鱼种数巡检失败", "uid", uid, "err", err)
		return nil
	}
	return s.grantRarityMilestones(ctx, uid, rare, super)
}

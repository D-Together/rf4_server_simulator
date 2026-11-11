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

// rarityMilestoneBatch 是一族奖座在一次判定里的结果。
// Count 是判定时的鱼种数，客户端礼物卡（811.f1）要的正是这个「条数」参数，
// 填 0 时客户端会把整枚徽章跳过（809 实机扫频：来源码 3 + 参数 0 = 不画卡）。
type rarityMilestoneBatch struct {
	Count int64
	Items []*store.Item
}

// raritySpeciesCounts 统计玩家**钓到过**的上星 / 蓝冠鱼种数（catch_seen）。
//
// 官方结算界面（801）在玩家选择入护或放生之前就把奖座礼物画出来了，所以鱼种数在
// 「钓到」那一刻就成立（2026-11-11 回归）。catch_seen 在 14/4 结算事务里落库，
// 与同一事务里写下的 catch_log 行一一对应；图鉴、307.f16 星号、3/17 面板读的是
// catch_log，放生同样保留那一行，因此图鉴与奖座始终同步。
func (s *Server) raritySpeciesCounts(ctx context.Context, uid int64) (rare, super int64, err error) {
	if s == nil || s.DB == nil {
		return 0, 0, nil
	}
	err = s.DB.SQL.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT CASE WHEN best_grade>=? THEN fish_key END),
		       COUNT(DISTINCT CASE WHEN best_grade>=? THEN fish_key END)
		FROM catch_seen WHERE uid=?`,
		int64(recordTrophyGrade), int64(superTrophyGrade), uid).Scan(&rare, &super)
	return rare, super, err
}

// recordCatchSeenTx 在钓获结算事务内记下本尾鱼的鱼种、水域与最高档位。
//
// 必须与结算回执同事务：结算失败回滚时这一尾不能留在计数里，重试才能重新判定。
// 只有 grade>=3 的鱼落行（普通鱼永远不推进奖座），同鱼种同水域重复上星只抬高档位。
// locLevel 是水域奖杯（来源码 6）的判定维度——它要的是「这个水域的面板是否集齐」。
func recordCatchSeenTx(ctx context.Context, tx *sql.Tx, uid int64, species, locLevel string, grade int64, ts float64) error {
	if tx == nil || species == "" || grade < recordTrophyGrade {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO catch_seen(uid,loc_level,fish_key,best_grade,first_ts) VALUES(?,?,?,?,?)
		ON CONFLICT(uid,loc_level,fish_key) DO UPDATE SET best_grade=MAX(catch_seen.best_grade, excluded.best_grade)`,
		uid, locLevel, species, grade, ts)
	return err
}

// rarityMilestoneAfterCatch 在 14/4 结算事务**提交之后**判定两族奖座。
//
// 结算响应（801）要用它返回的物品挂礼物行（来源码 3「捕获了数条稀有鱼」），
// 所以判定必须发生在写 801 之前、且本尾鱼已经落进 catch_seen。
func (s *Server) rarityMilestoneAfterCatch(ctx context.Context, uid int64) (rare, super rarityMilestoneBatch, err error) {
	rareCount, superCount, err := s.raritySpeciesCounts(ctx, uid)
	if err != nil {
		return rarityMilestoneBatch{}, rarityMilestoneBatch{}, err
	}
	rareItems, superItems := s.grantRarityMilestones(ctx, uid, rareCount, superCount)
	return rarityMilestoneBatch{Count: rareCount, Items: rareItems},
		rarityMilestoneBatch{Count: superCount, Items: superItems}, nil
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
// 两族分开返回，调用方要把它们挂成两张礼物卡（稀有鱼 / 超级稀有鱼各一）。
// 单档失败只记日志、不影响其他档位——奖座是纪念品，宁可晚发不可整单失败。
func (s *Server) grantRarityMilestones(ctx context.Context, uid, rare, super int64) (rareItems, superItems []*store.Item) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	grant := func(kind string, ladders []rarityMilestone, have int64) []*store.Item {
		var out []*store.Item
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
		return out
	}
	return grant(rarityKindRare, rareSpeciesMilestones, rare),
		grant(rarityKindSuperRare, superRareSpeciesMilestones, super)
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

// rarityMilestoneLabel 反查奖座对应的档位说明，用于日志/播报文案。
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
// 结算路径发放失败（例如奖座物品定义临时读不到）时，这里也会兜住。
func (s *Server) ensureRarityMilestones(ctx context.Context, uid int64) []*store.Item {
	if s == nil || s.DB == nil {
		return nil
	}
	rare, super, err := s.rarityMilestoneAfterCatch(ctx, uid)
	if err != nil {
		s.log.Warn("稀有鱼种数巡检失败", "uid", uid, "err", err)
		return nil
	}
	return append(rare.Items, super.Items...)
}

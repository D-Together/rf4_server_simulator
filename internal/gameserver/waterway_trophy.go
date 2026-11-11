package gameserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"rf4server/internal/store"
)

// 水域奖杯（`trophy_level_<水域 systemID>`，物品类别 72 奖赏，
// wire 类 enikeammfen / type_id 2015，走 3/16 奖励面板）。
//
// 官方语义：把一个水域的「渔夫稀有鱼」面板集齐——该水域面板收录的每一种都至少有
// 一条上星（grade 3）或蓝冠（grade 4）记录——就发一枚该水域的奖杯作为奖励。
// 判定口径与 3/17 面板完全一致（同一份 trophyFishWhitelist 收录清单、同一条
// recordTrophyGrade 准入门槛）。
//
// 判定时机是 **14/4 结算当场**，与稀有鱼奖座同一时刻：官方结算界面在玩家选择入护
// 或放生之前就画出「捕获了该水域的所有稀有鱼」这张卡，说明官方的收集进度在
// 「钓到」那一刻就成立。catch_log 现在也在 14/4 结算事务里写入（source='catch'），
// 入护只把它抬成 'keep'、放生保持 'catch'——图鉴、307.f16 星号、3/17 面板、
// 321 统计与这里的水域进度因此都读同一套「钓到过」的记录，不再有两套口径。
//
// 奖杯是纪念品：卖掉、丢掉、转到小屋之后都不会重发，因此发放记录单独写在 kv 里，
// 不能依赖「现在背包里有没有这枚奖杯」来判断是否发过。
const (
	waterwayTrophyItemPrefix  = "trophy_level_"
	waterwayTrophyHome        = "level_000_home" // 家园不是水域，没有奖杯定义
	waterwayTrophyContainer   = "backpack"       // 先放背包，玩家自己决定要不要摆进小屋
	waterwayTrophyKeyTemplate = "waterway-trophy:%d:%s"
)

// waterwayTrophyProgress 是某个水域的奖杯进度：地图鱼种总数与已上星的鱼种数。
type waterwayTrophyProgress struct {
	Total int64
	Done  int64
}

// waterwayTrophyItemID 把水域 systemID 映射成奖杯 item_id。物品 id 是
// `trophy_level_<去掉 level_ 前缀的水域号>`（`level_001_lake` → `trophy_level_001_lake`）。
// 没有对应物品定义的水域（家园，以及将来新增但官方还没做奖杯的水域）返回空串。
func (s *Server) waterwayTrophyItemID(ctx context.Context, locLevel string) (string, error) {
	if s == nil || s.DB == nil || locLevel == "" || locLevel == waterwayTrophyHome {
		return "", nil
	}
	itemID := waterwayTrophyItemPrefix + strings.TrimPrefix(locLevel, "level_")
	var n int64
	if err := s.DB.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_def WHERE item_id=?`, itemID).Scan(&n); err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	return itemID, nil
}

// waterwayTrophyProgressFor 统计该水域「渔夫稀有鱼」面板里有多少种已经上星/蓝冠。
//
// 分母有两套口径，优先用面板收录清单：
//
//  1. 已知该水域的白名单（trophyFishWhitelist）——这是唯一正确的口径。面板只列
//     十来个鱼种，集齐它们就算完成；loc_fish 里那些「能钓到但不在面板上」的鱼
//     （欧洲六须鲇、白斑狗鱼、小体鲟……）根本不参与判定。
//  2. 白名单还没反推出来的水域，退回 loc_fish（官方按水域统计过的鱼种表）。这比
//     map_fish_roster 准（后者混进了 habitat 推断、这张图其实钓不到的鱼），但仍是
//     超集，只会让奖杯晚发、不会早发——补白名单即恢复。
//
// 只认 catch_log.grade >= recordTrophyGrade 的记录——这正是 3/17 面板的准入线。
func (s *Server) waterwayTrophyProgressFor(ctx context.Context, uid int64, locLevel string) (waterwayTrophyProgress, error) {
	var p waterwayTrophyProgress
	if s == nil || s.DB == nil || locLevel == "" {
		return p, nil
	}
	if wl := trophyFishWhitelistSet(locLevel); len(wl) > 0 {
		return s.whitelistTrophyProgress(ctx, uid, locLevel, wl)
	}
	err := s.DB.SQL.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT lf.fish_key),
		       COUNT(DISTINCT CASE WHEN cs.best_grade>=? THEN cs.fish_key END)
		FROM loc_fish lf
		JOIN loc_alias a ON a.loc = lf.loc
		LEFT JOIN catch_seen cs ON cs.uid=? AND cs.fish_key=lf.fish_key AND cs.loc_level=?
		WHERE a.alias=?`, recordTrophyGrade, uid, locLevel, locLevel).Scan(&p.Total, &p.Done)
	return p, err
}

// whitelistTrophyProgress 按面板收录清单算进度：分母是清单长度，分子是清单里在该
// 水域已上星（grade 3）或蓝冠（grade 4）的鱼种数。清单外的记录一律不参与。
//
// 数据源是 `catch_seen`（钓到即记，带 loc_level）。结算发生在入护之前，
// 所以不能等 catch_log 的 source 变成 'keep' 才判——那时结算界面早就发出去了。
// catch_seen 与 catch_log 在同一结算事务里落库，两者对「钓到过」的认定一致。
func (s *Server) whitelistTrophyProgress(ctx context.Context, uid int64, locLevel string, wl map[string]struct{}) (waterwayTrophyProgress, error) {
	// 分母 = 认得出的格子 + 认不出的格子（后者永远不算完成，见 trophyFishWhitelistMissing）。
	p := waterwayTrophyProgress{Total: int64(trophyFishWhitelistTotal(locLevel))}
	if p.Total == 0 {
		p.Total = int64(len(wl))
	}
	keys := make([]string, 0, len(wl))
	args := make([]any, 0, len(wl)+3)
	for k := range wl {
		keys = append(keys, k)
	}
	// IN 列表按 key 排序，保证 SQL 文本稳定（便于日志与 EXPLAIN 对照）。
	sort.Strings(keys)
	ph := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	q := `SELECT COUNT(DISTINCT fish_key) FROM catch_seen
		WHERE uid=? AND loc_level=? AND best_grade>=? AND fish_key IN (` + ph + `)`
	args = append(args, uid, locLevel, int64(recordTrophyGrade))
	for _, k := range keys {
		args = append(args, k)
	}
	if err := s.DB.SQL.QueryRowContext(ctx, q, args...).Scan(&p.Done); err != nil {
		return p, err
	}
	return p, nil
}

// grantWaterwayTrophy 在该水域集齐后发放奖杯。已经发过、没集齐、或该水域没有奖杯
// 定义时返回 (nil, nil)；只有真的新发一枚时才返回物品行。
func (s *Server) grantWaterwayTrophy(ctx context.Context, uid int64, locLevel string) (*store.Item, error) {
	itemID, err := s.waterwayTrophyItemID(ctx, locLevel)
	if err != nil || itemID == "" {
		return nil, err
	}
	p, err := s.waterwayTrophyProgressFor(ctx, uid, locLevel)
	if err != nil {
		return nil, err
	}
	if p.Total <= 0 || p.Done < p.Total {
		return nil, nil
	}
	return s.issueWaterwayTrophy(ctx, uid, locLevel, itemID, p)
}

// issueWaterwayTrophy 是真正的发放：kv 幂等标记与物品在同一事务里提交。调用方必须
// 已经确认「集齐」（p.Done >= p.Total），这里只做幂等与入包。
func (s *Server) issueWaterwayTrophy(ctx context.Context, uid int64, locLevel, itemID string, p waterwayTrophyProgress) (*store.Item, error) {
	key := fmt.Sprintf(waterwayTrophyKeyTemplate, uid, locLevel)
	if _, done, kerr := s.DB.KVGet(ctx, key); kerr != nil && !errors.Is(kerr, sql.ErrNoRows) {
		return nil, kerr
	} else if done {
		return nil, nil
	}
	// 标记丢了但奖杯还在（手工改库/回滚）时只补标记，不再发第二枚。
	var owned int64
	if err := s.DB.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, itemID).Scan(&owned); err != nil {
		return nil, err
	}
	if owned > 0 {
		_ = s.DB.KVSet(ctx, key, "present")
		return nil, nil
	}
	it, err := s.prepareNewItem(ctx, uid, itemID, waterwayTrophyContainer, 1, "")
	if err != nil {
		return nil, err
	}
	// prepareNewItem 只构造物品对象，不落库——真正写进 item 表必须走
	// grantPreparedItemTx，并且和 kv 幂等标记放在同一个事务里，否则会出现
	// 「标记写了、物品没进包」的半成品状态（之后巡检认为已发，永久不发）。
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
		// 并发巡检/结算已经抢到这一枚，静默跳过。
		return nil, nil
	}
	row, _, err := grantPreparedItemTx(ctx, tx, it, false)
	if err != nil {
		return nil, fmt.Errorf("奖杯入包失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.log.Info("发放水域奖杯", "uid", uid, "water", locLevel, "item", itemID,
		"奖杯鱼种", p.Done, "水域鱼种", p.Total, "guid", row.GUID)
	return row, nil
}

// waterwayTrophyAfterCatch 在 14/4 结算事务提交之后判定**本尾鱼所在水域**是否集齐。
// 返回的物品由调用方挂 801.f4 的礼物行（来源码 6「捕获了该水域的所有稀有鱼」）
// 与 801.f8 奖励列表，客户端在结算界面当场弹卡——与稀有鱼奖座同一时机、同一表现。
//
// 只判本水域：一尾鱼只可能在它被钓上来的那片水里补上最后一个格子。判定数据源是
// catch_seen（本尾鱼已在结算事务里落库，且带 loc_level），所以跨水域的同名鱼种
// 不会互相顶进度。图鉴同样记这一尾（catch_log 同事务写入），两边永远同步。
func (s *Server) waterwayTrophyAfterCatch(ctx context.Context, uid int64, locLevel string) []*store.Item {
	if s == nil || s.DB == nil || locLevel == "" || locLevel == waterwayTrophyHome {
		return nil
	}
	it, err := s.grantWaterwayTrophy(ctx, uid, locLevel)
	if err != nil {
		s.log.Warn("水域奖杯判定失败", "uid", uid, "water", locLevel, "err", err)
		return nil
	}
	if it == nil {
		return nil
	}
	return []*store.Item{it}
}

// ensureWaterwayTrophies 巡检所有水域，补发已经集齐但还没拿到的奖杯。登录、打开
// 稀有鱼面板时调用，覆盖「离线期间集齐」与「升级前就已完成」两种历史情况。
func (s *Server) ensureWaterwayTrophies(ctx context.Context, uid int64) []*store.Item {
	if s == nil || s.DB == nil {
		return nil
	}
	// 只巡检有鱼种清单的正式水域：loc_fish 里出现、且能在 loc_alias 反查到 systemID。
	// 家园与「官方还没做奖杯」的水域在这里就被过滤掉，不必逐个试 item_def。
	rows, err := s.DB.SQL.QueryContext(ctx, `SELECT DISTINCT a.alias FROM loc_fish lf
		JOIN loc_alias a ON a.loc = lf.loc
		WHERE a.alias LIKE 'level_%' AND a.alias<>? ORDER BY a.alias`, waterwayTrophyHome)
	if err != nil {
		s.log.Warn("水域奖杯巡检失败", "uid", uid, "err", err)
		return nil
	}
	var locs []string
	for rows.Next() {
		var loc string
		if err := rows.Scan(&loc); err != nil {
			rows.Close()
			return nil
		}
		locs = append(locs, loc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil
	}
	var out []*store.Item
	for _, loc := range locs {
		it, gerr := s.grantWaterwayTrophy(ctx, uid, loc)
		if gerr != nil {
			s.log.Warn("水域奖杯巡检单点失败", "uid", uid, "water", loc, "err", gerr)
			continue
		}
		if it != nil {
			out = append(out, it)
		}
	}
	return out
}

// pushWaterwayTrophies 把新发的奖杯推进背包增量，玩家不用重开界面就能看到。
func (s *Session) pushWaterwayTrophies(items []*store.Item) {
	if s == nil || len(items) == 0 {
		return
	}
	guids := make([]string, 0, len(items))
	for _, it := range items {
		if it != nil && it.GUID != "" {
			guids = append(guids, it.GUID)
		}
	}
	s.Push4_22ItemGUIDs(guids)
}

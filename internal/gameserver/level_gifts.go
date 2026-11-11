package gameserver

// 升级礼物：对齐官方 2023 更新（前 10 级每级一份，11 级按钓法给套装）。
// 内容表逐项转录自官方礼物展示画面（Rutube《Подарки в РР4. За первые 11
// уровней》，2023-08-02，全服统一；11 级路亚套 = Arrow S60L + Hydra Smart 2
// 25S，来源官方论坛"Подарок за 11 уровень"截图与社区合集，item_id 已核对
// 静态目录）。
//
// 发放方式：钓鱼游装备包同型直发背包（prepareNewItem + grantPreparedItemTx），
// 但不打 f12=16|256 临时标志、不登记 trip_grant、不回收——无使用时间限制，
// 保留物品原生等级锁。kv 按级幂等键与物品发放同事务提交，先到先得。

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// levelGift 礼物条目：计件物品按 Qty 精确发放（件），装备类 Qty 忽略（1 件）；
// Name 供聊天播报。
type levelGift struct {
	ItemID string
	Qty    int64
	Name   string
}

// levelGiftTable 2~10 级官方礼物（全服统一，与钓法无关）。
var levelGiftTable = map[int64][]levelGift{
	2:  {{"caddisfly", 30, "毛翅目幼虫"}},
	3:  {{"porridge_pearl_barley", 30, "大麦粥"}},
	4:  {{"porridge_semolina", 30, "碎麦米粥"}},
	5:  {{"clas_20059", 1, "鱼钩 CHK201 S16"}},
	6:  {{"worm_01", 30, "蚯蚓"}},
	7:  {{"mono_15094", 1, "鱼线 Indiana 8LB 100M"}},
	8:  {{"dough_sweet", 30, "甜面团"}},
	9:  {{"clas_20177", 1, "鱼钩 Happy Hook CL-12 S16"}},
	10: {{"naiad", 15, "蜉蝣幼虫"}},
}

// levelGiftFloatSet11 11 级浮子钓法套（官方画面：竿+线+漂+钩）。
var levelGiftFloatSet11 = []levelGift{
	{"tele_10112_5", 1, "Comfort Telescopic 5 竿"},
	{"mono_15212", 1, "Mono Original 11.5LB 鱼线"},
	{"bob_25164", 1, "Bob Original 4G YR 浮漂"},
	{"clas_20090", 1, "鱼钩 CHK401 S14"},
}

// levelGiftSpinSet11 11 级路亚（纺车）套：Arrow S60L + Hydra Smart 2 25S。
var levelGiftSpinSet11 = []levelGift{
	{"spin_10003_S60L", 1, "Arrow S60L 路亚竿"},
	{"spin_5826", 1, "Hydra Smart 2 25S 渔轮"},
}

// levelGiftFor 取某一级的礼物。11 级按升到该级最后一尾鱼的钓法分套：
// 纺车（路亚，skillCat=2）→ 路亚套；浮钓/底钓/海钓 → 浮子套
// （官方口径"float/bottom 的 11 级礼物是低配那套"）。
func levelGiftFor(level, skillCat int64) ([]levelGift, bool) {
	if level == 11 {
		if skillCat == 2 {
			return levelGiftSpinSet11, true
		}
		return levelGiftFloatSet11, true
	}
	g, ok := levelGiftTable[level]
	return g, ok
}

// levelGiftKey 每级一次的幂等键：先写键再发物品，二者同事务。
func levelGiftKey(uid, level int64) string {
	return fmt.Sprintf("level_gift:%d:%d", uid, level)
}

// levelGiftGrant 一次真实升级中实际发出去的单级礼物。
type levelGiftGrant struct {
	Level int64
	Items []*store.Item
}

// grantLevelUpGifts 在真实升级（from 级升到 to 级）后逐级发放官方礼物，返回
// 实际发放的各级礼物（含并入的既有堆），供调用方挂结算奖励列表与推 4/22；
// 推送时机由调用方按所在路径决定。发放成功的同时对该玩家的全部在线会话
// 聊天播报礼物内容（与官方 k00 升级播报同通道，已验证可渲染）。调用点必须
// 已经拿到非重放的结算结果；这里对每一级独立幂等，跨结算/放生/重连/重放
// 都不会重复发放。skillCat 是触发升级那尾鱼的钓法大类
// （fishingSkillCatForRig 口径），只影响 11 级分套。tunable
// level_gifts_enabled<=0 时整体关闭（旧行为）。
func (s *Server) grantLevelUpGifts(ctx context.Context, uid, fromLevel, toLevel, skillCat int64) []levelGiftGrant {
	if s.DB == nil || toLevel <= fromLevel {
		return nil
	}
	if s.tunableF("level_gifts_enabled", 1) <= 0 {
		return nil
	}
	var grants []levelGiftGrant
	for level := fromLevel + 1; level <= toLevel; level++ {
		rows, err := s.grantLevelUpGift(ctx, uid, level, skillCat)
		if err != nil {
			s.log.Warn("升级礼物发放失败", "uid", uid, "level", level, "err", err)
			continue
		}
		if len(rows) == 0 {
			continue
		}
		grants = append(grants, levelGiftGrant{Level: level, Items: rows})
		s.notifyLevelUpGiftChat(uid, level, skillCat)
	}
	return grants
}

// notifyLevelUpGiftChat 升级礼物聊天播报："升级礼物：A×30、B×1"。
// 与升级播报（k00）先后出现在玩家当前打开的频道里。
func (s *Server) notifyLevelUpGiftChat(uid, level, skillCat int64) {
	gifts, ok := levelGiftFor(level, skillCat)
	if !ok {
		return
	}
	parts := make([]string, 0, len(gifts))
	for _, g := range gifts {
		parts = append(parts, fmt.Sprintf("%s×%d", g.Name, g.Qty))
	}
	text := fmt.Sprintf("升级礼物（%d级）：%s", level, strings.Join(parts, "、"))
	for _, sess := range s.sessionsForUID(uid) {
		sess.pushSystemChat(text)
	}
}

// gift811Line 是一条 811 礼物行里的「礼物来源 + 物品」。
//
// source 决定客户端礼物卡片上那句说明，param 是该来源的占位参数（新级别礼物用等级），
// items 是这条礼物包含的物品。
type gift811Line struct {
	source int
	param  int64
	items  []*store.Item
}

// giftSourceNewLevel 是 811.f0 的「新级别礼物」来源码（实机已验证）。
const giftSourceNewLevel = 1

// giftSourceTrophyCountReached 是「捕获了数条稀有鱼」的来源码（结算弹窗上下文）：
// 稀有鱼 / 超级稀有鱼的**鱼种数**跨档时用它把奖座挂进 801.f4 的礼物卡
// （kDlg_Catched_GiftsTrophyCountReached）。
//
// 2026-11-11 反汇编客户端卡片取词函数（GameAssembly 0x180D4BD40）得到硬证据：
//
//	cmp eax, 5 ; ja default        ; eax = source-1，只有 1..6 进表
//	jmp [table + (source-1)*4]     ; 表在 0x180D4C04C，6 项
//	表项：1→0x180D4BDDD  2→0x180D4BE99  3→0x180D4BFA3
//	      4→0x180D4BEEC  5→0x180D4BEF8  6→0x180D4BF48
//
// 其中 3 的落点 **0x180D4BFA3 正是 `ja` 的越界目标**——也就是说 3 在客户端枚举里
// 是空号，没有专属文案分支，画不出卡片。这解释了「十种了结算界面还是不弹」。
// 809 塑封那次「单发 3 一个徽章都不画」也不是「差参数」，而是同样落在这条空号路径。
//
// 有效值只有 1、2、4、5、6 五个，正好对上 Localization 的 5 个 Gifts key。
// 结合既往实机（2 = 游戏活动、4 = 塑封卡牌合集、5/6 = 集齐水域一族），
// 「数条稀有鱼」只有 5 与 6 两个候选。2026-11-11 用 settle_gift_debug_sources 一次
// 把 2/3/5/6 四条行同时挂进 801.f4 实测：2 = 游戏活动、5 = 捕获了数条稀有鱼、
// 6 = 捕获了该水域的所有稀有鱼，3 一条卡都没画 —— 与跳转表的结论吻合。
const giftSourceTrophyCountReached = 5

// giftSourceTrophyTableCollected 是「捕获了该水域的所有稀有鱼」的来源码（水域奖杯）。
//
// 同一张跳转表里 6 号分支就是它，2026-11-11 实机确认：把 6 挂进 801.f4 后，结算界面
// 弹出副标题「捕获了该水域的所有稀有鱼」的礼物卡。文案没有占位符，f1 随便给非 0。
const giftSourceTrophyTableCollected = 6

// debugGiftRowsSourceKey 是实机校准 811 来源码的一次性开关：值是逗号分隔的来源码，
// 非空时每次有鱼的 801 结算都额外挂同样多的礼物行，物品取玩家背包里现成的奖座
// （只进 801.f4 供客户端画卡，**不进 801.f8**，不写库、不发放任何东西）。
// 校准完把 tunable 删掉即恢复。
const debugGiftRowsSourceKey = "settle_gift_debug_sources"

// debugGiftRows 构造校准用的礼物行；无开关或取不到奖座时返回 nil。
func (s *Server) debugGiftRows(ctx context.Context, uid int64) []gift811Line {
	if s.DB == nil {
		return nil
	}
	spec := strings.TrimSpace(s.tunableS(debugGiftRowsSourceKey, ""))
	if spec == "" {
		return nil
	}
	var codes []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 || n > 255 {
			continue
		}
		codes = append(codes, n)
	}
	if len(codes) == 0 {
		return nil
	}
	rows, err := s.DB.SQL.QueryContext(ctx,
		`SELECT guid FROM item WHERE uid=? AND item_id LIKE 'statuette_%' ORDER BY item_id LIMIT ?`,
		uid, len(codes))
	if err != nil {
		return nil
	}
	var guids []string
	for rows.Next() {
		var g string
		if serr := rows.Scan(&g); serr == nil && g != "" {
			guids = append(guids, g)
		}
	}
	rows.Close()
	if len(guids) == 0 {
		return nil
	}
	out := make([]gift811Line, 0, len(codes))
	for i, code := range codes {
		row, gerr := s.DB.GetItemByGUID(ctx, guids[i%len(guids)])
		if gerr != nil || row == nil {
			continue
		}
		out = append(out, gift811Line{source: code, param: 10, items: []*store.Item{row}})
	}
	return out
}

// giftRows811 把若干条礼物打包成 811 礼物行数组。外层 args code 173（argcode_table
// by_elem 811→173），单行 {f0=来源, f1=参数, f2=礼物物品树(args 61), f3=空物品数组}。
// 客户端结算弹窗按来源取卡片（`kegidigbokk(source)`，GameAssembly 0x180D4BD40 /
// 收集 0x180D48C90），同一数组里可以同时有升级礼物与「数条稀有鱼」礼物。无礼物回 null。
func (s *Server) giftRows811(ctx context.Context, lines []gift811Line) (*proto.Value, error) {
	rows := proto.Args(proto.S("173"))
	for _, line := range lines {
		if len(line.items) == 0 {
			continue
		}
		items := proto.Args(proto.S("61"))
		for _, row := range line.items {
			if row == nil {
				continue
			}
			tree, err := s.ItemToTree(ctx, row)
			if err != nil {
				return nil, err
			}
			items.Items = append(items.Items, tree)
		}
		if len(items.Items) == 0 {
			continue
		}
		obj, err := wiregen.MakeObj("bkmnmjcfbic", map[int]*proto.Value{
			0: proto.U32(uint32(line.source)), // ehbdkjmbdee=来源码
			1: proto.U32(uint32(max64(0, line.param))),
			2: items,
			3: proto.Args(proto.S("61")), // 恒空数组（红线：数组不发 null）
		})
		if err != nil {
			return nil, err
		}
		rows.Items = append(rows.Items, obj)
	}
	if len(rows.Items) == 0 {
		return proto.Null(), nil
	}
	return rows, nil
}

// giftRows811Arg 把本次直发的升级礼物包装成 811 礼物行数组，供 807.f1（放流）与
// 801.f4（收鱼结算）携带。多级连升合并为一行，客户端对同来源行取物品并集渲染
// 单个礼物栈。无礼物时回 null（官服未升级样本该槽位即为 null）。
func (s *Server) giftRows811Arg(ctx context.Context, newLevel int64, rows []*store.Item) (*proto.Value, error) {
	if len(rows) == 0 {
		return proto.Null(), nil
	}
	return s.giftRows811(ctx, []gift811Line{{source: giftSourceNewLevel, param: newLevel, items: rows}})
}

// grantLevelUpGift 单级发放：幂等键与全部物品在同一事务内提交，先到先得。
// 计件物品按官方件数精确发放（prepareNewItemWithCount），装备类逐件建行；
// 任一物品构造失败则整体回滚，不留半份。
func (s *Server) grantLevelUpGift(ctx context.Context, uid, level, skillCat int64) ([]*store.Item, error) {
	gifts, ok := levelGiftFor(level, skillCat)
	if !ok {
		return nil, nil
	}
	prepared := make([]*store.Item, 0, len(gifts))
	for _, g := range gifts {
		def, err := s.DB.GetItemDefByItemID(ctx, g.ItemID)
		if err != nil || def == nil {
			return nil, fmt.Errorf("礼物物品定义不存在: %s", g.ItemID)
		}
		var it *store.Item
		if countableCategories[def.Category] {
			it, err = s.prepareNewItemWithCount(ctx, uid, g.ItemID, "backpack", g.Qty, "")
		} else {
			it, err = s.prepareNewItem(ctx, uid, g.ItemID, "backpack", 1, "")
		}
		if err != nil {
			return nil, fmt.Errorf("礼物物品构造失败: %s: %w", g.ItemID, err)
		}
		prepared = append(prepared, it)
	}

	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kv(k,v) VALUES(?,?)`,
		levelGiftKey(uid, level), "granted")
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, nil // 该等级已发放过，静默跳过
	}
	granted := make([]*store.Item, 0, len(prepared))
	for _, it := range prepared {
		row, _, err := grantPreparedItemTx(ctx, tx, it, true)
		if err != nil {
			return nil, fmt.Errorf("礼物入包失败: %s: %w", it.ItemID, err)
		}
		granted = append(granted, row)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.log.Info("发放升级礼物", "uid", uid, "level", level, "items", len(granted))
	return granted, nil
}

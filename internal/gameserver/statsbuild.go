package gameserver

import (
	"context"
	"sort"
	"strings"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 321 统计快照（3/2）。所有值都从玩家 catch_log 生成，不使用静态模板。

type catchStatRow struct {
	TS       float64
	Loc      string
	Spot     int64
	FishKey  string
	WeightG  int64
	LengthM  float64
	Source   string
	Bait     string
	Grade    int64
	XP       int64
	Silver   int64
	LocLevel string
	// GUID 见 store.CatchRecord.GUID：纪录卡的模型族要靠它跟渔护/溜鱼对齐。
	GUID string
	// TackleCat 是钓法大类快照（1=浮钓 2=路亚 3=底钓 11=海钓，0=未知）。
	// 0 是历史行：只能回退「拟饵/非拟饵」旧口径，不能当成已知的浮钓。
	TackleCat int64
}

// 钓法大类取值，与技能大类（skills_ops.go fishingSkillCatForRig）和
// catch_log.tackle_cat 同源。
const (
	tackleCatUnknown  = int64(0)
	tackleCatFloat    = int64(1)
	tackleCatSpinning = int64(2)
	tackleCatBottom   = int64(3)
	tackleCatMarine   = int64(11)
)

func (s *Server) loadCatchStatRows(ctx context.Context, uid int64) ([]catchStatRow, error) {
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT COALESCE(ts,0), COALESCE(loc,''), COALESCE(spot,0), COALESCE(fish_key,''),
		       COALESCE(weight_g,0), COALESCE(length_m,0), COALESCE(source,''), COALESCE(bait,''),
		       COALESCE(grade,0), COALESCE(xp,0), COALESCE(silver,0), COALESCE(loc_level,''),
		       COALESCE(catch_guid,''), COALESCE(tackle_cat,0)
		FROM catch_log WHERE uid=? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []catchStatRow
	for rows.Next() {
		var r catchStatRow
		if err := rows.Scan(&r.TS, &r.Loc, &r.Spot, &r.FishKey, &r.WeightG, &r.LengthM,
			&r.Source, &r.Bait, &r.Grade, &r.XP, &r.Silver, &r.LocLevel, &r.GUID, &r.TackleCat); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Server) playerCounter(ctx context.Context, uid int64, key string) int64 {
	var v int64
	_ = s.DB.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(value,0) FROM player_counters WHERE uid=? AND key=?`, uid, key).Scan(&v)
	return v
}

// buildStats321 构造 321 jahbacemfof。
func (s *Server) buildStats321(ctx context.Context, uid int64) (*proto.Value, error) {
	rows, err := s.loadCatchStatRows(ctx, uid)
	if err != nil {
		return nil, err
	}

	total := int64(len(rows))
	var rare, superRare int64
	dailyCount := map[string]int64{}
	dailyWeight := map[string]float64{}
	for _, r := range rows {
		if r.Grade >= 3 {
			rare++
		}
		if r.Grade >= 4 {
			superRare++
		}
		d := time.Unix(int64(r.TS), 0).In(time.Local).Format("2006-01-02")
		dailyCount[d]++
		dailyWeight[d] += float64(r.WeightG)
	}
	maxDailyCount, maxDailyWeightG := int64(0), 0.0
	for d, c := range dailyCount {
		if c > maxDailyCount {
			maxDailyCount = c
		}
		if dailyWeight[d] > maxDailyWeightG {
			maxDailyWeightG = dailyWeight[d]
		}
	}

	// 按诱饵和钓法聚合：路亚归入纺车 1/5/17，其余归入浮钓 3/1/27。
	// 路亚判定按饵定义品类（17=拟饵），不能用 item_id 前缀——spoon/
	// plug/topwater/softplastic 等拟饵不带 spinner 前缀，前缀判断会把
	// 它们全部误算进浮钓（2026-11-09 实测数据实锤）。
	distinctBaits := make([]string, 0, 16)
	seenBait := map[string]bool{}
	for _, r := range rows {
		if r.Bait != "" && !seenBait[r.Bait] {
			seenBait[r.Bait] = true
			distinctBaits = append(distinctBaits, r.Bait)
		}
	}
	lureByBait := s.baitLureFlags(ctx, distinctBaits)
	isLure := func(bait string) bool { return lureByBait[bait] }
	// familyOf 是本次统计唯一的钓法归属入口：有落库快照用快照，历史行回退
	// 「拟饵→路亚、其余→浮钓」。竿/钓组/诱饵聚合与三张钓法纪录卡共用它，
	// 避免同一尾鱼在纪录卡里算海钓、在聚合里算浮钓。
	familyOf := func(r catchStatRow) int64 { return tackleFamilyOf(r, isLure) }
	baitCount := map[string]int64{}
	baitRare := map[string]int64{}
	baitOrder := make([]string, 0)
	rodCount := map[int64]int64{}
	rodRare := map[int64]int64{}
	rigCount := map[int64]int64{}
	rigRare := map[int64]int64{}
	for _, r := range rows {
		if r.Bait == "" {
			continue
		}
		if baitCount[r.Bait] == 0 {
			baitOrder = append(baitOrder, r.Bait)
		}
		baitCount[r.Bait]++
		rod, rig := rodAndRigOfFamily(familyOf(r))
		if r.Grade >= 3 {
			baitRare[r.Bait]++
			rodRare[rod]++
			rigRare[rig]++
		}
		rodCount[rod]++
		rigCount[rig]++
	}

	// 拉力回溯：与 14/14 发鱼的拉力同源（鱼种搏鱼曲线 × 重量），
	// 历史渔获无落库变值，用基准力确定性重算——纪录卡只比排序维度。
	pullOf := func(r catchStatRow) float64 {
		f := &RolledFish{Species: r.FishKey, WeightG: r.WeightG, Grade: r.Grade}
		return s.fishFightPullFor(f)
	}
	cards := buildRecordCards(rows, pullOf, familyOf)
	card := func(key string) *proto.Value {
		r, ok := cards[key]
		return s.recordCard320(r, ok, isLure)
	}
	biggest := card("biggest")
	if biggest == nil {
		biggest = card("max_spin")
	}

	// f6 鱼列表：客户端 DTO ilhlknehckb.aknbobdjjnk 是 gillkjlbkje[]
	// （320 纪录卡数组，Cpp2IL 元数据实证），不是空容器。发空数组时统计
	// 面板的鱼列表一片空白。每鱼种取最重一条，按重量降序；卡片的
	// f9/f10/f11 分别是该鱼种总钓获、奖杯和超稀有计数（官服 3/2 抓包实证）。
	type speciesStat struct {
		best        catchStatRow
		total       int64
		trophy      int64
		superTrophy int64
	}
	bestBySpecies := map[string]*speciesStat{}
	for _, r := range rows {
		if r.FishKey == "" {
			continue
		}
		st := bestBySpecies[r.FishKey]
		if st == nil {
			st = &speciesStat{}
			bestBySpecies[r.FishKey] = st
		}
		if st.total == 0 || r.WeightG > st.best.WeightG {
			st.best = r
		}
		st.total++
		if r.Grade >= 3 {
			st.trophy++
		}
		if r.Grade >= 4 {
			st.superTrophy++
		}
	}
	fishArgs := proto.Args(proto.S("129"))
	fishRows := make([]*speciesStat, 0, len(bestBySpecies))
	for _, st := range bestBySpecies {
		fishRows = append(fishRows, st)
	}
	sort.SliceStable(fishRows, func(i, j int) bool { return fishRows[i].best.WeightG > fishRows[j].best.WeightG })
	for _, st := range fishRows {
		card := s.recordCard320(st.best, true, isLure)
		setRecordCard320Counts(card, st.total, st.trophy, st.superTrophy)
		fishArgs.Items = append(fishArgs.Items, card)
	}

	baitKeys := append([]string(nil), baitOrder...)
	sort.SliceStable(baitKeys, func(i, j int) bool {
		return baitCount[baitKeys[i]] > baitCount[baitKeys[j]]
	})
	// 321 明确区分 f19「饵料统计」与 f21「拟饵统计」（wire_spec 字段名实证），
	// 二者元素类不同，此前把所有钓饵都塞进 f21 是语义错配：
	//   f19 元素 = 318 gconafdfchk {cemjmlpafmc systemID, nomdkkcbclc 计数}，容器 args 127
	//   f21 元素 = 319 keoejejkenf（{品类, 鱼饵/拟饵 systemID, 字符串参数, 计数}），容器 args 128
	// 容器码来自 argcode_table.json 的 by_type_id / by_elem，不是推断。
	baitArgs := proto.Args(proto.S("127")) // 天然饵 → 饵料统计
	lureArgs := proto.Args(proto.S("128")) // 拟饵 → 拟饵统计
	for _, b := range baitKeys {
		if isLure(b) {
			_, _, hook := tackleOfBait(true)
			lureArgs.Items = append(lureArgs.Items, wiregen.MustMakeObj("keoejejkenf", map[int]*proto.Value{
				0: proto.U32(uint32(hook)),
				1: proto.StrVal(b),
				2: proto.StrVal(""),
				3: proto.U32(uint32(baitCount[b])),
			}))
			continue
		}
		baitArgs.Items = append(baitArgs.Items, wiregen.MustMakeObj("gconafdfchk", map[int]*proto.Value{
			0: proto.StrVal(b),
			1: proto.U32(uint32(baitCount[b])),
		}))
	}
	_ = baitRare

	rodArgs := proto.Args(proto.S("125"))
	for _, cat := range sortedCountKeys(rodCount) {
		rodArgs.Items = append(rodArgs.Items, wiregen.MustMakeObj("gjndofnpibn", map[int]*proto.Value{
			0: proto.U32(uint32(cat)), 1: proto.U32(uint32(rodCount[cat])),
			2: proto.U32(uint32(rodRare[cat])), 3: proto.U32(0),
		}))
	}
	rigArgs := proto.Args(proto.S("126"))
	for _, cat := range sortedCountKeys(rigCount) {
		rigArgs.Items = append(rigArgs.Items, wiregen.MustMakeObj("emehdoalanl", map[int]*proto.Value{
			0: proto.U32(uint32(cat)), 1: proto.U32(uint32(rigCount[cat])),
			2: proto.U32(uint32(rigRare[cat])), 3: proto.U32(0),
		}))
	}

	// 字段槽位语义按客户端 UI 消费链对齐：
	// 0 总钓获 1 稀有 2 蓝冠 3 脱钩 4 单日条数 5 单日重量(kg) 6 鱼种卡列表
	// 7 最大渔获 8 最优竞争力（拉力） 9 冒险鱼卡 10 最多银币
	// 12 最多经验 14 浮钓最大 15 路亚最大 16 底钓最大鱼
	// 17 竿类聚合（族级代表值，非真实竿型） 18 钓组聚合（同上）
	// 19 饵料聚合 20 制作聚合 21 拟饵聚合
	// 11/13/22 语义未定，恒发 0；9（冒险鱼）缺业务口径，发 null。
	//
	// 注意 11/13 不是「损坏的钓竿/渔轮」：按该假设下发实测后，面板对应卡片
	// 仍显示 0，故不得用未证实的语义占用槽位。
	return wiregen.MustMakeObj("jahbacemfof", map[int]*proto.Value{
		0: proto.U32(uint32(total)),
		1: proto.U32(uint32(rare)),
		2: proto.U32(uint32(superRare)), // 蓝冠（grade>=4）
		3: proto.U32(uint32(s.playerCounter(ctx, uid, "escapes"))),
		4: proto.U32(uint32(maxDailyCount)),
		// 客户端反编译实证：DTO ilhlknehckb.cpenllldpnk 是 float（典型范围
		// [0, 2.167] = kg 量纲），此处必须发 F32 位型。
		5:  proto.F32(float32(maxDailyWeightG / 1000.0)),
		6:  fishArgs,
		7:  biggest,
		8:  card("strongest"),
		9:  proto.Null(),
		10: card("max_silver"),
		11: proto.U64(0), // 语义未定，见上方说明
		12: card("max_xp"),
		13: proto.U64(0), // 语义未定，见上方说明
		14: card("max_float"),
		15: card("max_spin"),
		16: card("max_bottom"),
		17: rodArgs,
		18: rigArgs,
		19: baitArgs,
		20: proto.Null(),
		21: lureArgs,
		22: proto.U32(0),
	}), nil
}

// baitLureFlags 批量判定饵是否为拟饵（路亚）：按 item_def 品类 17 判定，
// 定义缺失时回退旧 spinner 前缀判断（历史/未知饵不误判）。
func (s *Server) baitLureFlags(ctx context.Context, baits []string) map[string]bool {
	out := make(map[string]bool, len(baits))
	for _, b := range baits {
		if b == "" {
			continue
		}
		if _, done := out[b]; done {
			continue
		}
		lure := strings.HasPrefix(b, "spinner")
		if def, err := s.DB.GetItemDefByItemID(ctx, b); err == nil && def != nil {
			lure = def.Category == catLure
		}
		out[b] = lure
	}
	return out
}

// tackleFamilyOf 返回这条渔获的钓法大类。
//
// 快照优先：catch_log.tackle_cat 是入护当刻的钓法真源（见 migration
// 00055）。历史行没有快照（0）时只能回退旧口径——拟饵算路亚、其余算浮钓；
// 这个回退会把海钓/底钓历史渔获算进浮钓，是已修的错源，只在无法判定时使用。
func tackleFamilyOf(r catchStatRow, isLure func(string) bool) int64 {
	switch r.TackleCat {
	case tackleCatFloat, tackleCatSpinning, tackleCatBottom, tackleCatMarine:
		return r.TackleCat
	}
	if isLure(r.Bait) {
		return tackleCatSpinning
	}
	return tackleCatFloat
}

// tackleCatForRig 把钓组类型映射到钓法大类，与技能大类口径一致
// （skills_ops.go fishingSkillCatForRig）。
//
// 与技能大类的唯一区别：技能页对未知正数是"按浮钓计"（保证熟练度有归属），
// 但统计快照要落库且不可回溯，把未知钓组写成浮钓会永久污染浮钓纪录卡，
// 因此这里先确认钓组属于已知四族之一，未知值返回 0（未知），由 321 侧回退。
func tackleCatForRig(rigType int64) int64 {
	if rigType <= 0 {
		return tackleCatUnknown
	}
	if marineRigType(rigType) {
		return tackleCatMarine
	}
	if bottomRigTypes[rigType] {
		return tackleCatBottom
	}
	if rigTypeLureFamily[rigType] {
		return tackleCatSpinning
	}
	if floatRigTypes[rigType] {
		return tackleCatFloat
	}
	return tackleCatUnknown
}

// rodAndRigOfFamily 给出钓法族对应的代表竿型/钓组类型（wire 类 gjndofnpibn.f0
// celffbjpmma / emehdoalanl.f0 jppmjnahbpo 的枚举值）。
//
// 注意这是**族级代表值，不是真实竿型/钓组**：catch_log 目前只记录钓法大类
// （tackle_cat），没有记录当次使用的具体 rod_type 与 rig_type。因此同族内的
// 差异会被合并——例如底钓族的 Feeder/Picker/Carp/Spod 全部计为 Feeder(10)，
// 路亚族的 Spinning/Casting/Jerking 全部计为 Spinning(1)。
// 要按真实竿型/钓组统计，需在 catch_log 同步保存 rod_type/rig_type 后再聚合。
func rodAndRigOfFamily(family int64) (rod, rig int64) {
	switch family {
	case tackleCatSpinning:
		return 1, 5 // Spinning 纺车 / rig_Lure
	case tackleCatBottom:
		return 10, 12 // Feeder 飞德 / rig_BottomSimple
	case tackleCatMarine:
		return 15, 50 // Pilker 海竿 / rig_MarinePilker
	default:
		return 3, 1 // Telescopic 手竿 / rig_FloatSimple
	}
}

// tackleOfBait 钓法归属：路亚归纺车（竿 1/钓组 5/钩品 17），其余归
// 浮钓竿（竿 3/钓组 1/钩品 27）。仅用于没有钓法快照的诱饵卡与钩品字段。
func tackleOfBait(lure bool) (rod, rig, hook int64) {
	if lure {
		return 1, 5, 17
	}
	return 3, 1, 27
}

func sortedCountKeys(m map[int64]int64) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// buildRecordCards 只为 bait 非空的日志生成纪录卡。当前可可靠计算的七张卡：
//
//	biggest      全场最重
//	strongest    搏鱼拉力最强（鱼种冲刺倍率 × 重量，与 14/14 拉力同源）
//	max_xp       单条经验最多
//	max_silver   单条银币最多
//	max_float    浮钓族最重
//	max_spin     路亚族最重
//	max_bottom   底钓族最重
//
// 三张钓法卡按 familyOf 互斥分族；海钓渔获没有对应槽位，只参与 biggest /
// strongest / max_xp / max_silver 这类不分钓法的卡。
func buildRecordCards(rows []catchStatRow, pullOf func(catchStatRow) float64, familyOf func(catchStatRow) int64) map[string]catchStatRow {
	out := map[string]catchStatRow{}
	var usable []catchStatRow
	for _, r := range rows {
		if r.Bait != "" {
			usable = append(usable, r)
		}
	}
	if len(usable) == 0 {
		return out
	}
	byWeight := append([]catchStatRow(nil), usable...)
	sort.SliceStable(byWeight, func(i, j int) bool { return byWeight[i].WeightG > byWeight[j].WeightG })
	byXP := append([]catchStatRow(nil), usable...)
	sort.SliceStable(byXP, func(i, j int) bool { return byXP[i].XP > byXP[j].XP })
	bySilver := append([]catchStatRow(nil), usable...)
	sort.SliceStable(bySilver, func(i, j int) bool { return bySilver[i].Silver > bySilver[j].Silver })
	// 先为每条渔获算一次拉力，避免 comparator 反复查 species_weight。
	byPull := make([]struct {
		row  catchStatRow
		pull float64
	}, len(usable))
	for i, r := range usable {
		byPull[i] = struct {
			row  catchStatRow
			pull float64
		}{row: r, pull: pullOf(r)}
	}
	sort.SliceStable(byPull, func(i, j int) bool { return byPull[i].pull > byPull[j].pull })
	// 三张钓法纪录卡严格按钓法族互斥：浮钓只收浮钓、路亚只收路亚、底钓只收
	// 底钓；海钓没有对应槽位，因此不进任何一张（此前海钓渔获被算进浮钓）。
	var floatC, spinC, bottomC []catchStatRow
	for _, r := range usable {
		switch familyOf(r) {
		case tackleCatSpinning:
			spinC = append(spinC, r)
		case tackleCatBottom:
			bottomC = append(bottomC, r)
		case tackleCatMarine:
			// 海钓渔获没有独立纪录卡槽位，不进浮钓/路亚/底钓任一张。
		default:
			floatC = append(floatC, r)
		}
	}
	sort.SliceStable(floatC, func(i, j int) bool { return floatC[i].WeightG > floatC[j].WeightG })
	sort.SliceStable(spinC, func(i, j int) bool { return spinC[i].WeightG > spinC[j].WeightG })
	sort.SliceStable(bottomC, func(i, j int) bool { return bottomC[i].WeightG > bottomC[j].WeightG })

	out["biggest"] = byWeight[0]
	out["strongest"] = byPull[0].row
	out["max_xp"] = byXP[0]
	out["max_silver"] = bySilver[0]
	if len(floatC) > 0 {
		out["max_float"] = floatC[0]
	}
	if len(spinC) > 0 {
		out["max_spin"] = spinC[0]
	}
	if len(bottomC) > 0 {
		out["max_bottom"] = bottomC[0]
	}
	return out
}

func setRecordCard320Counts(card *proto.Value, total, trophy, superTrophy int64) {
	if card == nil {
		return
	}
	for _, item := range []struct {
		name  string
		value int64
	}{
		{"ipmijfogljh", total},
		{"leklbplmgkj", trophy},
		{"iidphfokfdd", superTrophy},
	} {
		if idx := fieldIdxByCS(card.Class, item.name); idx >= 0 && idx < len(card.Fields) {
			card.Fields[idx].Num = ptrU64(uint64(item.value))
		}
	}
}

func (s *Server) recordCard320(r catchStatRow, ok bool, isLure func(string) bool) *proto.Value {
	if !ok {
		return nil
	}
	grade := r.Grade
	if grade == 0 {
		grade = 1
	}
	_, _, hook := tackleOfBait(isLure(r.Bait))
	loc := r.LocLevel
	if loc == "" {
		loc = r.Loc
	}
	// f13（oajeeklppia）= 模型族名，与渔护 25.f11、溜鱼 27.f22 同源；
	// 留 null 会退回鱼种默认族（老模型），统计/纪录列表就会跟详情打架。
	modelKey := s.fishModelVariantForFish(r.FishKey, r.WeightG, r.LengthM, r.Grade, r.GUID)
	return wiregen.MustMakeObj("ippcfiphgfl", map[int]*proto.Value{
		0:  proto.StrVal(r.FishKey),
		1:  proto.U32(uint32(grade)),
		2:  proto.U32(uint32(r.WeightG)),
		3:  proto.F32(float32(r.LengthM)),
		4:  proto.U64(uint64(r.XP)),
		5:  proto.U32(uint32(hook)),
		6:  proto.StrVal(r.Bait),
		7:  proto.StrVal(loc),
		8:  proto.U64(uint64(int64(r.TS*10_000_000) + 116444736000000000)),
		9:  proto.U32(0),
		10: proto.U32(0),
		11: proto.U32(0),
		12: proto.U32(uint32(grade)),
		13: recordModelField(modelKey),
		14: proto.U32(0),
	})
}

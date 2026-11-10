package gameserver

// 限时活动分支 9..20 的任务判定（273 条）。条件一律从客户端文案
// （assets/qualification_texts_zh.json 的 qlfctn_t_<id>）反推，与永久资格分支
// 同构：每条只绑定 qevFishLanded + 一个去重键函数。规则在 init 里并入
// qualificationRules，复用 evaluateQualificationsTx 的判定/封顶/增量链路。
// 「稀有鱼（星）」按个体档位 >=3（同永久分支）；仅文案明确说「鱼类/鱼种」
// 的 16603/20604 按物种稀有度 >=1 判。
//
// 水域 key 取自 loc_alias（官服记录卡/鱼获库实测）；鱼种 key 取自 loc_wiki_fish；
// 饵类 fopbhlmkcpe（2/7 昆虫、6 蠕虫、13 粥/面团）、味型 acclagajcdn、线强度
// kngaaefkpka（公斤）取自 item_def。竿型/硬度/钓组大类同时写入下方三张表，
// 供 5007.Idx4/5/6 下发与服务端竿型门控共用，保证「鱼竿」文案与判定一致。
//
// 缺口（qGapCounted，恒返回空键、保留静态分母，不造假进度）：
//   - 「以下/所列/指定/下列水域」名单不在文案里：12603 13602 13603 14703 16002
//     16103 17202 17302 17501 17602 17603 18302 18703 19101 19103 19202 19301
//     19603 20002 20101 20102 20104 20203。
//   - 「下列鱼种/四个鱼类/6 种鱼类」名单未知：12401 12403 16401 20401。
//   - 「假啮齿动物」拟饵 item 无匹配：14901 18901。
//   - 累计总重 30 公斤需重量累加计数器，现有集合长度机制不支持：13502 17502。
//   - 渔线轮尺寸字段未确认，且唯一需要它的 19603 水域名单本就未知：整体留空。

// 活动任务水域 key（loc_alias 实测）。
const (
	evLocKomarino  = "level_001_lake"            // 克马羚诺也湖
	evLocVyunok    = "level_002_kirzah_river"    // 惟有诺克河
	evLocKuori     = "level_003_viiveri_lake"    // 廓里湖
	evLocOstrog    = "level_004_torfyanoe_ozero" // 旧奥斯特罗格湖
	evLocVolkhov   = "level_005_volhov"          // 沃尔霍夫河
	evLocLadoga    = "level_006_ladoga"          // 拉多加湖
	evLocMedvezhye = "level_007_grass_lake"      // 梅德韦杰湖
	evLocSura      = "level_008_sura_river"      // 苏拉河
	evLocAkhtuba   = "level_009_ahtuba"          // 阿赫图巴河
	evLocBelaya    = "level_010_belaya"          // 白河
	evLocLadogaIs  = "level_011_ladoga_02"       // 拉多加湖群岛
	evLocNorthSea  = "level_012_north_sea"       // 挪威海
	evLocSevDon    = "level_013_sev_don"         // 北顿涅茨河
	evLocAmber     = "level_014_carp_lake"       // 琥珀湖
	evLocTunguska  = "level_015_tunguska"        // 下通古斯卡河
	evLocYama      = "level_016_yama"            // 亚马河
	evLocCopper    = "level_018_moskitnoe"       // 铜湖
	evLocElk       = "level_019_american_pond"   // 埃尔克湖
)

// qGapCounted 是缺口任务的占位计数函数：恒返回空键（本次不计入），保留静态
// 分母让面板正常显示 0/N。缺失信号见文件头与各任务注释，补齐后替换为真过滤。
func qGapCounted(qualificationEvent) string { return "" }

// eventGapRuleIDs 是 32 条缺口任务（Counted=qGapCounted）。这些任务当前没有任何
// 能推进进度的事件，若下发到面板会表现为永久 0/N，故 eventTaskList 跳过它们、
// 不作为可完成任务下发；规则表仍保留注册，待真实信号补齐后替换 qGapCounted
// 并从本表移除即可开放。追踪见 issue #379。
var eventGapRuleIDs = map[int64]bool{
	12401: true, 12403: true, 12603: true, 13502: true, 13602: true, 13603: true,
	14703: true, 14901: true, 15603: true, 16002: true, 16103: true, 16401: true,
	17202: true, 17302: true, 17501: true, 17502: true, 17602: true, 17603: true,
	18302: true, 18703: true, 18901: true, 19101: true, 19103: true, 19202: true,
	19301: true, 19603: true, 20002: true, 20101: true, 20102: true, 20104: true,
	20203: true, 20401: true,
}

// 活动任务竿型限制（celffbjpmma）；未收录=任意竿型。与 5007.Idx5 同源。
var eventRodTypes = map[int64][]int64{
	9203: {3}, 9302: {3}, 9402: {3}, 9501: {3},
	10001: {1}, 10101: {1},
	11201: {3}, 11203: {9, 10}, 11503: {3, 4, 5}, 11603: {9},
	12102: {9}, 12201: {10}, 12303: {3, 4, 5}, 12503: {9, 10}, 12602: {3},
	13001: {1, 2}, 13003: {3, 4, 5}, 13102: {3, 4, 5}, 13302: {3}, 13402: {3},
	13501: {1, 2}, 13603: {1, 2},
	14001: {1, 2}, 14002: {3, 4, 5}, 14101: {1, 2}, 14102: {3, 4, 5},
	14301: {3, 4, 5}, 14303: {1, 2}, 14401: {1, 2}, 14402: {3}, 14403: {3},
	14501: {1, 2}, 14503: {3}, 14602: {1, 2}, 14701: {1, 2}, 14802: {3}, 14902: {1, 2},
	15201: {3}, 15202: {1}, 15303: {1}, 15603: {9},
	16102: {3}, 16103: {1}, 16201: {9, 10}, 16403: {3}, 16503: {3},
	17201: {3}, 17302: {3}, 17402: {3}, 17501: {1, 2},
	18001: {1, 2}, 18401: {1, 2}, 18402: {3}, 18403: {3}, 18503: {3},
	18602: {1, 2}, 18603: {1, 2, 3}, 18802: {3}, 18902: {1, 2},
	19201: {3}, 19202: {1, 2}, 19303: {1, 2}, 19603: {9, 10, 11},
	20103: {3}, 20104: {1, 2}, 20201: {9, 10, 11}, 20403: {3},
}

// 活动任务竿硬度档限制（befnkekakei：1=UL 2=L 3=ML）；与 5007.Idx6 同源。
// 硬度判定本身在 Counted 里用 qUltralight/qLight/qMediumLightOrLight 执行。
var eventRodPowers = map[int64][]int64{
	11401: {1, 2}, 11503: {1, 2}, 12303: {1, 2}, 12602: {1},
	14401: {1, 2}, 14701: {1, 2}, 14801: {1},
	15401: {1, 2}, 16103: {1, 2, 3}, 16301: {1, 2, 3}, 16602: {1},
	18401: {1, 2}, 18403: {1, 2}, 18603: {1, 2},
	19303: {1, 2}, 20003: {1, 2, 3}, 20104: {1, 2, 3}, 20602: {1}, 20603: {1},
}

// 活动任务钓组大类（efgbdocjcan：1 浮钓 2 路亚 3 底钓）；与 5007.Idx4 同源。
var eventRigClass = map[int64]int64{
	9003: 1, 9102: 1, 9601: 1, 10002: 1, 10102: 1, 10403: 1, 10501: 1,
	9602: 3, 10003: 3, 10302: 3, 10402: 3,
	9502: 2, 10001: 2, 10101: 2, 10201: 2, 11102: 2, 11202: 2, 11303: 2,
	12501: 3, 12601: 2, 13401: 3, 13503: 2, 14003: 3, 14203: 3,
	14501: 2, 14701: 2, 15103: 2, 15202: 2, 15203: 3, 15303: 2,
	16501: 3, 17102: 1, 17401: 3, 17503: 1, 18002: 1, 18003: 3,
	18102: 1, 18702: 3, 19001: 1, 19202: 2, 19203: 3, 19303: 2,
	20501: 3,
}

// eventQualificationRules 是分支 9..20 的 273 条判定。数量型按鱼种/饵去重，
// 单尾条件型按 CatchGUID 去重；「N 条稀有鱼」按单尾计 N。缺口任务用 qGapCounted。
var eventQualificationRules = []qualificationRuleDef{
	// ---- 分支 9 MMXXIV（21）----
	{ID: 9001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino), qSpeciesIn("c.carp", "crucian"), qWeightAtLeast(1000))},
	{ID: 9002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino), qSpeciesIn("c.roach"), qWeightAtLeast(700))},
	{ID: 9003, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qFloatRig())},
	{ID: 9101, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok), qSpeciesIn("a.sleeper"), qWeightAtLeast(220))},
	{ID: 9102, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok), qFloatRig(), qSpeciesIn("e.chub"), qWeightAtLeast(1500))},
	// 9103 红虫香精=味型 16（bloodworm 的 acclagajcdn，待实机核对）。
	{ID: 9103, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qBaitFlavorIn(16))},
	{ID: 9201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("dace"), qWeightAtLeast(180))},
	{ID: 9202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("c_bleak"), qWeightAtLeast(85))},
	{ID: 9203, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino), qWeightAtLeast(2000))},
	{ID: 9301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok), qSpeciesIn("perch"), qWeightAtLeast(800))},
	{ID: 9302, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok), qSpeciesIn("bream"), qWeightAtLeast(2000))},
	{ID: 9303, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qIsNight())},
	{ID: 9401, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("wa_bream"), qWeightAtLeast(350))},
	{ID: 9402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("s.bream"), qWeightAtLeast(500))},
	{ID: 9403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("s.orfe"), qWeightAtLeast(1500))},
	{ID: 9501, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok), qSpeciesIn("tench"), qWeightAtLeast(1500))},
	{ID: 9502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok), qArtificialLure(), qSpeciesIn("n.pike"), qWeightAtLeast(2500))},
	{ID: 9503, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("b_bream"), qWeightAtLeast(100))},
	{ID: 9601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qFloatRig(), qSpeciesIn("c_nase"), qWeightAtLeast(400))},
	{ID: 9602, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok), qBottomRig(), qGradeAtLeast(3))},
	{ID: 9603, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino))},

	// ---- 分支 10 MMXXIV EXTRA（21）----
	{ID: 10001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qArtificialLure(), qSpeciesIn("barsch"), qWeightAtLeast(500))},
	{ID: 10002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qFloatRig(), qSpeciesIn("gudgeon"), qWeightAtLeast(90))},
	{ID: 10003, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino), qBottomRig(), qSpeciesIn("carp"), qWeightAtLeast(3000))},
	{ID: 10101, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok, evLocBelaya), qArtificialLure(), qSpeciesIn("e.pikeperch"), qWeightAtLeast(800))},
	{ID: 10102, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVyunok), qFloatRig(), qDrifting())},
	// 10103 地龙=vypolzok 前缀。
	{ID: 10103, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino, evLocVyunok), qBaitPrefix("vypolzok"))},
	{ID: 10201, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino, evLocVyunok), qArtificialLure())},
	{ID: 10202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("ruffe_n"))},
	{ID: 10203, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino, evLocVyunok), qBaitClassIn(2, 7))},
	{ID: 10301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qDrifting(), qSpeciesIn("a_grayling"), qWeightAtLeast(1000))},
	{ID: 10302, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qBottomRig(), qSpeciesIn("w_catfish"))},
	// 10303 文案「高白蛙」按高白鲑 peled 判（错别字，待实机核对）。
	{ID: 10303, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("peled"), qWeightAtLeast(1000))},
	{ID: 10401, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qSpeciesIn("riv_brown_trout"), qWeightAtLeast(1500))},
	{ID: 10402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qBottomRig(), qSpeciesIn("burbot"), qWeightAtLeast(1500))},
	{ID: 10403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qFloatRig(), qSpeciesIn("vendace"), qWeightAtLeast(350))},
	{ID: 10501, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocBelaya), qFloatRig(), qBaitClassIn(2, 7), qDrifting())},
	{ID: 10502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("eel"))},
	{ID: 10503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qSpeciesIn("riv_rb_trout", "gc_smelt"), qWeightAtLeast(2000))},
	{ID: 10601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qSpeciesIn("taimen"), qWeightAtLeast(2500))},
	{ID: 10602, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("sig_kuor"), qWeightAtLeast(1000))},
	{ID: 10603, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("b_carp"), qWeightAtLeast(5000))},

	// ---- 分支 11 VII（21）----
	{ID: 11001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino), qSpeciesIn("ruffe"), qWeightAtLeast(80))},
	{ID: 11002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("c.carp", "crucian"), qWeightAtLeast(1200))},
	{ID: 11003, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qSpeciesIn("e.chub"), qWeightAtLeast(1000))},
	// 11101 珍珠大麦粥=porridge_pearl_barley。
	{ID: 11101, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino, evLocVyunok), qBaitPrefix("porridge_pearl_barley"))},
	{ID: 11102, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocBelaya), qArtificialLure())},
	{ID: 11103, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("perch"), qWeightAtLeast(1200))},
	{ID: 11201, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog))},
	{ID: 11202, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qJigRig())},
	{ID: 11203, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocMedvezhye))},
	{ID: 11301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("s.bream"), qWeightAtLeast(650))},
	{ID: 11302, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye), qSpeciesIn("c_barbel"), qWeightAtLeast(5000))},
	{ID: 11303, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVolkhov), qLureFamilyIn("wobbler"))},
	{ID: 11401, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qLight(), qLureFamilyIn("spinner"))},
	{ID: 11402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye), qSpeciesIn("g_tench"))},
	{ID: 11403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov), qSpeciesIn("v_bream"), qWeightAtLeast(1000))},
	{ID: 11501, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("sevan_trout"), qWeightAtLeast(4000))},
	{ID: 11502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura), qSpeciesIn("e.pikeperch"), qWeightAtLeast(5000))},
	{ID: 11503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSevDon), qLight())},
	{ID: 11601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov), qSpeciesIn("l_salmon"), qWeightAtLeast(7000))},
	{ID: 11602, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("sabrefish"), qWeightAtLeast(1000))},
	{ID: 11603, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura))},

	// ---- 分支 12 VII EXTRA（21）----
	{ID: 12001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qBaitClassIn(2, 7), qWeightAtLeast(1200))},
	{ID: 12002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qWeightAtLeast(5000))},
	{ID: 12003, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("salvel_l"), qWeightAtLeast(1500))},
	{ID: 12101, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVolkhov), qWeightAtLeast(1000))},
	{ID: 12102, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocCopper), qWeightAtLeast(3000))},
	{ID: 12103, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qWeightAtLeast(7001))},
	{ID: 12201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov), qSpeciesIn("bream"), qWeightAtLeast(2501))},
	{ID: 12202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("taran"), qWeightAtLeast(800))},
	{ID: 12203, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok, evLocOstrog), qGradeAtLeast(3))},
	// 12301 文案夜晚 23:00-04:00，事件仅携带 19:00-06:00 夜间位，按 qIsNight 近似（待实机核对）。
	{ID: 12301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qIsNight(), qWeightAtLeast(3001))},
	{ID: 12302, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qSpeciesIn("bh_carp", "s_carp"))},
	{ID: 12303, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura, evLocSevDon), qLight(), qSpeciesIn("rudd"), qWeightAtLeast(1000))},
	// 12401 缺口：「下列鱼种之一」名单未知。
	{ID: 12401, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 12402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura), qSpeciesIn("asp"), qWeightAtLeast(4001))},
	// 12403 缺口：「下列 6 种鱼类」名单未知。
	{ID: 12403, Event: qevFishLanded, Counted: qGapCounted},
	// 12501 分支子线钓组=ThreeWay(10)，与永久 2103 同源。
	{ID: 12501, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadoga), qRigIn(10), qWeightAtLeast(1501))},
	{ID: 12502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov, evLocLadoga, evLocLadogaIs), qSpeciesIn("a_salmon"), qWeightAtLeast(15000))},
	// 12503 文案第三处水域缺失（「旧奥斯特罗格湖、琥珀湖或…」），按已知两水域判（待补）。
	{ID: 12503, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog, evLocAmber), qWeightAtLeast(12000))},
	{ID: 12601, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadogaIs), qLureFamilyIn("spoon"))},
	{ID: 12602, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadoga, evLocLadogaIs), qUltralight())},
	// 12603 缺口：「下列水域」名单未知。
	{ID: 12603, Event: qevFishLanded, Counted: qGapCounted},

	// ---- 分支 13 MMXXV（21）----
	{ID: 13001, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino))},
	{ID: 13002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("c.roach"), qWeightAtLeast(500))},
	{ID: 13003, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVyunok))},
	{ID: 13101, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("c.carp", "crucian"), qWeightAtLeast(800))},
	// 13102 毛翅目幼虫=caddisfly。
	{ID: 13102, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok, evLocKomarino), qBaitPrefix("caddisfly"), qWeightAtLeast(1000))},
	{ID: 13103, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qBaitPrefix("maggot"))},
	{ID: 13201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("s.orfe"), qWeightAtLeast(1000))},
	{ID: 13202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok, evLocBelaya), qSpeciesIn("c_bleak"), qWeightAtLeast(100))},
	{ID: 13203, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocBelaya))},
	{ID: 13301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("barsch"))},
	{ID: 13302, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok, evLocOstrog), qSpeciesIn("bream"), qWeightAtLeast(1500))},
	{ID: 13303, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog), qIsNight())},
	{ID: 13401, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qBottomRig(), qSpeciesIn("wa_bream"), qWeightAtLeast(350))},
	{ID: 13402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok, evLocOstrog), qSpeciesIn("s.bream"), qWeightAtLeast(500))},
	{ID: 13403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("a.sleeper"), qWeightAtLeast(350))},
	{ID: 13501, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok, evLocKomarino), qSpeciesIn("e.chub"), qWeightAtLeast(1000))},
	// 13502 缺口：累计总重 30 公斤需重量累加计数器，集合长度机制不支持。
	{ID: 13502, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 13503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qFromBoat(), qLureFamilyIn("spinner"))},
	{ID: 13601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye), qSpeciesIn("g_carp"), qWeightAtLeast(7000))},
	// 13602 缺口：「列出的任一水域」名单未知。
	{ID: 13602, Event: qevFishLanded, Counted: qGapCounted},
	// 13603 缺口：「下列任一水域」名单未知。
	{ID: 13603, Event: qevFishLanded, Counted: qGapCounted},

	// ---- 分支 14 MMXXV EXTRA（30，10 阶段）----
	{ID: 14001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok, evLocBelaya), qSpeciesIn("e.pikeperch"))},
	{ID: 14002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qWeightAtLeast(800))},
	{ID: 14003, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qBottomRig(), qSpeciesIn("carp", "m_carp"))},
	{ID: 14101, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVyunok, evLocKomarino, evLocBelaya), qWeightAtLeast(1000))},
	{ID: 14102, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVyunok), qDrifting(), qWeightAtLeast(500))},
	{ID: 14103, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok, evLocBelaya), qSpeciesIn("dace"), qWeightAtLeast(180))},
	{ID: 14201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocOstrog), qSpeciesIn("e.chub", "s.orfe"), qWeightAtLeast(1500))},
	{ID: 14202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qGradeAtLeast(3), qWeightAtLeast(500))},
	{ID: 14203, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog), qBottomRig(), qWeightAtLeast(1000))},
	{ID: 14301, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori))},
	{ID: 14302, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocMedvezhye), qBaitClassIn(13), qSpeciesIn("carp", "m_carp", "n_carp"))},
	{ID: 14303, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("burbot"), qWeightAtLeast(3000))},
	{ID: 14401, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVolkhov), qLight())},
	{ID: 14402, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSevDon), qLineStrengthAtMost(3))},
	{ID: 14403, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVolkhov), qWeightAtMost(100))},
	// 14501 小型勺子按 spoon 族判（尺寸阈值未明，待实机核对）。
	{ID: 14501, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSevDon), qLureFamilyIn("spoon"))},
	{ID: 14502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("common_carp"), qWeightAtLeast(5000))},
	{ID: 14503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qBaitClassIn(2, 7))},
	{ID: 14601, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qSpeciesIn("sterlet", "r_sturgeon"))},
	{ID: 14602, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qLineStrengthAtMost(12), qWeightAtLeast(5000))},
	{ID: 14603, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog))},
	{ID: 14701, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qJigRig(), qLight())},
	{ID: 14702, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura), qSpeciesIn("s_carp"), qWeightAtLeast(8000))},
	// 14703 缺口：「下列的任一水域」名单未知。
	{ID: 14703, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 14801, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qUltralight())},
	{ID: 14802, Event: qevFishLanded, Counted: qCountSingle(qLineStrengthAtMost(1.5), qWeightAtLeast(500))},
	{ID: 14803, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye, evLocCopper, evLocAmber), qSpeciesIn("c_barbel"), qWeightAtLeast(5000))},
	// 14901 缺口：「假啮齿动物」拟饵 item 无匹配。
	{ID: 14901, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 14902, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadogaIs), qSpeciesIn("salvel_l", "palia_lud", "palia_kr"))},
	// 14903 三种鲽鱼：按鲽科鱼种去重（sea_kambala/庸鲽/马舌鲽/长吻黄盖鲽/长拟庸鲽/大菱鲆，待实机核对名单）。
	{ID: 14903, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocNorthSea), qSpeciesIn("sea_kambala", "halibut_w", "halibut_b", "lh_kambala", "hippoglos_p", "turbot"))},

	// ---- 分支 15 VIII（21）----
	{ID: 15001, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qWeightAtLeast(500))},
	{ID: 15002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("bream"), qWeightAtLeast(500))},
	{ID: 15003, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qSpeciesIn("sig_p"), qWeightAtLeast(300))},
	// 15101 湿面包=bread。
	{ID: 15101, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino, evLocVyunok), qBaitPrefix("bread"))},
	{ID: 15102, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qSpeciesIn("perch"), qWeightAtLeast(1000))},
	{ID: 15103, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog), qArtificialLure())},
	// 15201 蛆虫蛹=maggot_caster。
	{ID: 15201, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog), qBaitPrefix("maggot_caster"))},
	{ID: 15202, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qLureFamilyIn("spoon"))},
	{ID: 15203, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocMedvezhye), qBottomRig(), qBaitClassIn(13))},
	{ID: 15301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("s.bream"), qWeightAtLeast(600))},
	{ID: 15302, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye, evLocOstrog), qSpeciesIn("tench", "g_tench"), qWeightAtLeast(1200))},
	{ID: 15303, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVolkhov), qArtificialLure(), qLureFamilyIn("wobbler"))},
	{ID: 15401, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qLight())},
	{ID: 15402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocMedvezhye), qSpeciesIn("g_tench"))},
	{ID: 15403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov, evLocSura), qSpeciesIn("v_bream"), qWeightAtLeast(1000))},
	{ID: 15501, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("sevan_trout"), qWeightAtLeast(3500))},
	{ID: 15502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura), qSpeciesIn("e.pikeperch"), qWeightAtLeast(4500))},
	// 15503 蠕虫类饵=饵类 6。
	{ID: 15503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qBaitClassIn(6))},
	{ID: 15601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("chartus_kuoriius"))},
	{ID: 15602, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("shemaya_cha"), qWeightAtLeast(250))},
	// 15603 缺口：「指定水域」名单未知（中调性派克竿）。
	{ID: 15603, Event: qevFishLanded, Counted: qGapCounted},

	// ---- 分支 16 VIII EXTRA（21）----
	{ID: 16001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qBaitClassIn(2, 7), qWeightAtLeast(1200))},
	// 16002 缺口：「指定水域」名单未知。
	{ID: 16002, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 16003, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qSpeciesIn("riv_brown_trout", "sevan_trout", "riv_rb_trout", "gc_smelt"))},
	{ID: 16101, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qWeightAtLeast(1000))},
	{ID: 16102, Event: qevFishLanded, Counted: qCountSpecies(qLineStrengthAtMost(2))},
	// 16103 缺口：「指定水域」名单未知。
	{ID: 16103, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 16201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov, evLocSura), qSpeciesIn("bream"), qWeightAtLeast(2500))},
	{ID: 16202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("pumps_sunfish"), qWeightAtLeast(300))},
	{ID: 16203, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocOstrog), qGradeAtLeast(3))},
	{ID: 16301, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadoga, evLocSura, evLocSevDon), qIsNight(), qMediumLightOrLight(), qWeightAtLeast(1000))},
	{ID: 16302, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qSpeciesIn("s_carp", "bh_carp"))},
	{ID: 16303, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura, evLocAkhtuba, evLocSevDon), qSpeciesIn("rudd", "sabrefish"), qWeightAtLeast(1000))},
	// 16401 缺口：「下列四个鱼类」名单未知。
	{ID: 16401, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 16402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov, evLocSura), qSpeciesIn("asp", "burbot"), qWeightAtLeast(4500))},
	{ID: 16403, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qWeightAtMost(100))},
	{ID: 16501, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocMedvezhye), qBottomRig())},
	{ID: 16502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov, evLocLadoga, evLocLadogaIs), qSpeciesIn("a_salmon"), qWeightAtLeast(8000))},
	// 16503 幼虫饵=饵类 2。
	{ID: 16503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadoga, evLocLadogaIs), qBaitClassIn(2))},
	{ID: 16601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura, evLocLadoga, evLocLadogaIs), qSpeciesIn("n.pike"), qWeightAtLeast(10000))},
	{ID: 16602, Event: qevFishLanded, Counted: qCountSingle(qUltralight(), qGradeAtLeast(3))},
	{ID: 16603, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocCopper, evLocAmber), qRarityAtLeast(1))},

	// ---- 分支 17 MMXXVI（21）----
	{ID: 17001, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qBaitClassIn(6))},
	{ID: 17002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("s.bream"), qWeightAtLeast(350))},
	{ID: 17003, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("warm"), qWeightAtLeast(200))},
	{ID: 17101, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVyunok), qWeightBelow(50))},
	{ID: 17102, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qFloatRig(), qWeightAtLeast(1000))},
	{ID: 17103, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("g_shad"), qWeightAtLeast(400))},
	{ID: 17201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("bream"), qWeightAtLeast(1000))},
	// 17202 缺口：「所列任一水塘」名单未知。
	{ID: 17202, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 17203, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocBelaya))},
	{ID: 17301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("e.pikeperch"))},
	// 17302 缺口：「所列任一水塘」名单未知。
	{ID: 17302, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 17303, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog), qWeightAtLeast(500))},
	{ID: 17401, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qBottomRig(), qSpeciesIn("b_bream"), qWeightAtLeast(250))},
	{ID: 17402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKomarino, evLocVyunok, evLocOstrog), qSpeciesIn("s.bream"), qWeightAtLeast(500))},
	{ID: 17403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("perch"), qWeightAtLeast(1200))},
	// 17501 缺口：「所列任一水域」名单未知。
	{ID: 17501, Event: qevFishLanded, Counted: qGapCounted},
	// 17502 缺口：累计总重 30 公斤需重量累加计数器；「所列鲤鱼鱼种」名单亦未知。
	{ID: 17502, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 17503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qFloatRig(), qFromBoat())},
	{ID: 17601, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocMedvezhye), qWeightAtLeast(3000))},
	// 17602 缺口：「所列任何水域」名单未知。
	{ID: 17602, Event: qevFishLanded, Counted: qGapCounted},
	// 17603 缺口：「所列任一水域」名单未知。
	{ID: 17603, Event: qevFishLanded, Counted: qGapCounted},

	// ---- 分支 18 2026 特别奖（30，10 阶段）----
	{ID: 18001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok, evLocBelaya), qSpeciesIn("asp"))},
	{ID: 18002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qFloatRig(), qWeightAtLeast(800))},
	{ID: 18003, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKomarino), qBottomRig())},
	{ID: 18101, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("b_gill"), qWeightAtLeast(1000))},
	{ID: 18102, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVyunok), qFloatRig(), qBaitClassIn(2, 7))},
	{ID: 18103, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya, evLocVyunok), qSpeciesIn("crawfish"))},
	{ID: 18201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("wh_bass"), qWeightAtLeast(1000))},
	{ID: 18202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori, evLocBelaya), qSpeciesIn("riv_brown_trout"), qWeightAtLeast(1500))},
	{ID: 18203, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocOstrog), qSpeciesIn("burbot"), qWeightAtLeast(3000))},
	{ID: 18301, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocElk), qWeightAtLeast(500))},
	// 18302 缺口：「所列任何水域」名单未知。
	{ID: 18302, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 18303, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qSpeciesIn("salvel_l"), qWeightAtLeast(1000))},
	{ID: 18401, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qLight())},
	{ID: 18402, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSevDon), qLineStrengthAtMost(3))},
	{ID: 18403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qLight(), qWeightAtLeast(1500))},
	{ID: 18501, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("bp_bass"), qWeightAtLeast(1500))},
	{ID: 18502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("c_nase"), qWeightAtLeast(500))},
	{ID: 18503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadoga), qBaitClassIn(2, 7))},
	{ID: 18601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura, evLocTunguska), qSpeciesIn("sterlet", "syb_sterlet"))},
	{ID: 18602, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qLineStrengthAtMost(12), qWeightAtLeast(5000))},
	{ID: 18603, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVolkhov), qLight())},
	{ID: 18701, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("lm_b_bass"), qWeightAtLeast(3500))},
	{ID: 18702, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura), qBottomRig(), qSpeciesIn("s_carp", "bh_carp"), qWeightAtLeast(16000))},
	// 18703 缺口：「下列的任一水域」名单未知。
	{ID: 18703, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 18801, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori, evLocOstrog), qSpeciesIn("sig_ostr", "sig_kuor"), qWeightAtLeast(1000))},
	{ID: 18802, Event: qevFishLanded, Counted: qCountSingle(qLineStrengthAtMost(1.5), qWeightAtLeast(500))},
	{ID: 18803, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye, evLocCopper, evLocAmber), qSpeciesIn("c_barbel"), qWeightAtLeast(5000))},
	// 18901 缺口：「假啮齿动物」拟饵 item 无匹配。
	{ID: 18901, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 18902, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocAkhtuba), qSpeciesIn("k_salmon"), qWeightAtLeast(10000))},
	{ID: 18903, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocNorthSea), qSpeciesIn("red_fish", "sebastes_m", "sebastes_n"))},

	// ---- 分支 19 IX（22）----
	{ID: 19001, Event: qevFishLanded, Counted: qCountBaitItem(qLocLevelIn(evLocKomarino), qFloatRig(), qBaitCategory(27))},
	{ID: 19002, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVyunok), qSpeciesIn("gudgeon"), qWeightAtLeast(80))},
	{ID: 19003, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qSpeciesIn("sig_p"), qWeightAtLeast(300))},
	// 19101 缺口：「以下水域」名单未知（红蚓=muckworm 红蚯蚓，待实机核对）。
	{ID: 19101, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 19102, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qSpeciesIn("perch"), qWeightAtLeast(1000))},
	// 19103 缺口：「以下水域」名单未知。
	{ID: 19103, Event: qevFishLanded, Counted: qGapCounted},
	// 19201 树皮甲虫幼虫=scolytinae。
	{ID: 19201, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocOstrog, evLocElk), qBaitPrefix("scolytinae"))},
	// 19202 缺口：「以下水域」名单未知（带须铅头钩=rig 35）。
	{ID: 19202, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 19203, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocMedvezhye), qBottomRig(), qBaitClassIn(13))},
	// 19301 缺口：「以下水域」名单未知。
	{ID: 19301, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 19302, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye, evLocOstrog), qSpeciesIn("tench", "g_tench"), qWeightAtLeast(1200))},
	{ID: 19303, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocVolkhov), qLight(), qJigRig())},
	{ID: 19401, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qLineStrengthAtMost(5))},
	{ID: 19402, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov, evLocSura), qSpeciesIn("b_bream"), qWeightAtLeast(250))},
	{ID: 19403, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("resfish"), qWeightAtLeast(800))},
	{ID: 19501, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qAnyOf(qAllOf(qSpeciesIn("sevan_trout"), qWeightAtLeast(3500)), qAllOf(qSpeciesIn("chartus_kuoriius"), qWeightAtLeast(5000))))},
	{ID: 19502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori), qAnyOf(qAllOf(qSpeciesIn("sevan_trout"), qWeightAtLeast(3500)), qAllOf(qSpeciesIn("chartus_kuoriius"), qWeightAtLeast(5000))))},
	{ID: 19503, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura, evLocBelaya), qSpeciesIn("e.pikeperch"), qWeightAtLeast(4500))},
	{ID: 19504, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSura), qBaitClassIn(2, 7))},
	{ID: 19601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocVolkhov), qSpeciesIn("l_salmon"), qWeightAtLeast(4000))},
	{ID: 19602, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("shemaya_cha"), qWeightAtLeast(250))},
	// 19603 缺口：「以下水域」名单未知 + 渔线轮尺寸字段未确认。
	{ID: 19603, Event: qevFishLanded, Counted: qGapCounted},

	// ---- 分支 20 IX EXTRA（23）----
	{ID: 20001, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocBelaya), qBaitClassIn(2, 7), qWeightAtLeast(1200))},
	// 20002 缺口：「以下水域」名单未知。
	{ID: 20002, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 20003, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocKuori), qMediumLightOrLight(), qSpeciesIn("riv_brown_trout", "sevan_trout", "riv_rb_trout", "gc_smelt"))},
	// 20101 缺口：「以下水域」名单未知（圆腹雅罗鱼 >=2kg 或诸子鲦 >=3kg）。
	{ID: 20101, Event: qevFishLanded, Counted: qGapCounted},
	// 20102 缺口：同 20101。
	{ID: 20102, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 20103, Event: qevFishLanded, Counted: qCountSpecies(qLineStrengthAtMost(2))},
	// 20104 缺口：「以下水域」名单未知。
	{ID: 20104, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 20201, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura, evLocSevDon), qSpeciesIn("c_nase"), qWeightAtLeast(500))},
	{ID: 20202, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("rudd"), qWeightAtLeast(800))},
	// 20203 缺口：「以下任意水域」名单未知。
	{ID: 20203, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 20301, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocElk), qSpeciesIn("fwdrum"), qWeightAtLeast(4000))},
	{ID: 20302, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocMedvezhye, evLocSura), qWeightAtLeast(5000))},
	{ID: 20303, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSevDon), qSpeciesIn("asp"), qWeightAtLeast(5000))},
	// 20401 缺口：「下列四个鱼类」名单未知。
	{ID: 20401, Event: qevFishLanded, Counted: qGapCounted},
	{ID: 20402, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocElk), qSpeciesIn("s_bass", "hs_bass"))},
	{ID: 20403, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocSevDon), qWeightAtMost(100))},
	{ID: 20501, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocMedvezhye), qBottomRig())},
	{ID: 20502, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocKuori, evLocLadoga, evLocLadogaIs), qSpeciesIn("salvel_l", "palia_lud", "palia_kr"), qWeightAtLeast(3000))},
	{ID: 20503, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocLadoga, evLocLadogaIs))},
	{ID: 20601, Event: qevFishLanded, Counted: qCountSingle(qLocLevelIn(evLocSura, evLocLadoga, evLocLadogaIs), qSpeciesIn("e.pikeperch"), qWeightAtLeast(7000))},
	// 20602 「2 条稀有鱼或两种罕见鱼种」语义模糊，按 2 尾奖杯档（Grade>=3）统计（待实机核对）。
	{ID: 20602, Event: qevFishLanded, Counted: qCountSingle(qUltralight(), qGradeAtLeast(3))},
	// 20603 同 20602。
	{ID: 20603, Event: qevFishLanded, Counted: qCountSingle(qUltralight(), qGradeAtLeast(3))},
	{ID: 20604, Event: qevFishLanded, Counted: qCountSpecies(qLocLevelIn(evLocAmber, evLocCopper, evLocElk), qRarityAtLeast(1))},
}

func init() {
	qualificationRules = append(qualificationRules, eventQualificationRules...)
	// 活动任务里的顺水漂流条件，同步点亮 5007.Idx27=64（StreamMoving）。
	for _, id := range []int64{10102, 10301, 10501, 14102} {
		driftRuleIDs[id] = true
	}
}

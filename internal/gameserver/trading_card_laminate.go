package gameserver

// 27/4 卡册塑封/覆膜（客户端 lkokodffmln(Guid deck)；命令号 capstone 实锤
// 0x1805f994d：mov byte[rsp+0x20],4 + mov r9b,0x1b）。响应类型
// llgjfoldbcc（wire gglppphenoi/809 = pcnefbpgdhl 派生加 f0 奖励描述数组），
// 四值序 (f0 描述 plhpmhpadcg[], f1 奖励物品树, f2 备用, f3 Guid[])——解码
// 器 gglppphenoi.plphdfehkjl 依次读四值；奖励弹窗（blehdkolhfl）吃 f1 的
// 物品树，响应先经 eiihgcpdpbb 应用再回调。
//
// 语义（2026-11-08 实机）：集齐（f0 每物种一张）才能塑封；f1 密封时间戳
// 落卡册行（FILETIME！.NET Ticks 会被客户端 FromFileTime 渲染成 +1600 年
// → 3626/xx/xx）；奖座 = 卡册 def 属性 pnaapkplkfo，幂等补发；塑封后卡册
// 冻结——入册/取回一律拒绝（changeTradingCardAlbum 服务端守卫）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// deckSealed 读卡册行塑封时间戳（ckfmndabaei f1，U64；>0=已塑封）。
func deckSealed(fields []proto.Field) uint64 {
	if len(fields) > 1 && fields[1].Num != nil {
		return *fields[1].Num
	}
	return 0
}

// deckStatuettePlan 奖座发放计划（全部事务外准备——静态库查询不进事务的
// SQLite 单连接纪律）：def 顺序的奖座清单、已有行、缺发构造件，以及两种
// 来源的响应树。事务内只做权威判定（是否已有/是否补发）与 INSERT；
// ItemToTree 读静态库，不得在事务内调用（写锁期间池化读会饿死）。
type deckStatuettePlan struct {
	statuettes []string
	prepared   map[string]*store.Item
	trees      map[string]*proto.Value // GUID -> 事务外预生成的响应树
	containers map[string]string       // 已有奖座 GUID -> 事务外快照容器
}

// firstOwnedItemByItemID 事务外查玩家全部容器中的首个同定义物品。
// 奖座可被 4/6 移到房屋等容器，判断“已拥有”不能只看背包。
func (s *Server) firstOwnedItemByItemID(ctx context.Context, uid int64, itemID string) (*store.Item, error) {
	row := s.DB.SQL.QueryRowContext(ctx, `SELECT `+txItemCols+` FROM item
		WHERE uid=? AND item_id=? ORDER BY created_ts,guid LIMIT 1`, uid, itemID)
	it, err := scanTxItem(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return it, nil
}

// prepareDeckStatuettes 事务外读 def、已有奖座与缺发构造件，并预生成响应树。
// 已有行按 GUID 保存树和容器快照；缺发构造件落库时 GUID 不变。
func (s *Server) prepareDeckStatuettes(ctx context.Context, uid int64, deckID string) (*deckStatuettePlan, error) {
	def, err := s.DB.GetItemDefByItemID(ctx, deckID)
	if err != nil || def == nil || def.Category != 82 {
		return nil, fmt.Errorf("卡册定义无效")
	}
	var attrs struct {
		Statuettes []string `json:"pnaapkplkfo"`
	}
	if err := json.Unmarshal([]byte(def.AttrsJSON), &attrs); err != nil {
		return nil, err
	}
	plan := &deckStatuettePlan{
		prepared:   map[string]*store.Item{},
		trees:      map[string]*proto.Value{},
		containers: map[string]string{},
	}
	for _, st := range attrs.Statuettes {
		if st == "" {
			continue
		}
		plan.statuettes = append(plan.statuettes, st)
		existing, err := s.firstOwnedItemByItemID(ctx, uid, st)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			tree, err := s.ItemToTree(ctx, existing)
			if err != nil || tree == nil {
				return nil, fmt.Errorf("已有奖座树构造失败: %s", st)
			}
			plan.trees[existing.GUID] = tree
			plan.containers[existing.GUID] = existing.Container
			continue
		}
		prepared, err := s.prepareNewItem(ctx, uid, st, "backpack", 1, "")
		if err != nil {
			return nil, err
		}
		tree, err := s.ItemToTree(ctx, prepared)
		if err != nil || tree == nil {
			return nil, fmt.Errorf("待发奖座树构造失败: %s", st)
		}
		plan.prepared[st] = prepared
		plan.trees[prepared.GUID] = tree
	}
	return plan, nil
}

// deckComplete 集齐校验：f0 内嵌卡的鱼种数字 id 去重后须恰好覆盖卡组
// def 清单（成员校验而非只比数量——数量相同但混入外组卡/重复卡不得塑封）。
func deckComplete(cards []*proto.Value, wantNumeric map[uint32]bool) bool {
	seen := make(map[uint32]bool, len(cards))
	for _, entry := range cards {
		if len(entry.Fields) <= 4 {
			return false
		}
		id := entry.Fields[4].U32()
		if !wantNumeric[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return len(seen) == len(wantNumeric)
}

func hTradingCardLaminate(ctx *Ctx) ([]byte, bool) {
	if !ctx.Sess.Srv.requireDB(ctx) {
		return nil, false
	}
	if !ctx.DecodeOK || ctx.Tree == nil || ctx.Tree.Kind != proto.KindArgs || ctx.Tree.ArgCount() != 1 ||
		ctx.Tree.ArgItem(0) == nil || ctx.Tree.ArgItem(0).Kind != proto.KindGuid {
		ctx.ReplyFailure("System.ArgumentException", "卡册塑封参数无效")
		return nil, true
	}
	s := ctx.Sess.Srv
	c, uid := ctx.Sess.pushCtx(), ctx.Sess.Userid
	deckGUID := ctx.Tree.ArgItem(0).GuidHex()
	if deckGUID == "" || deckGUID == zeroGUID {
		ctx.ReplyFailure("System.ArgumentException", "卡册塑封参数无效")
		return nil, true
	}
	// 事务外只做静态库读取与奖座准备（SQLite 单连接纪律）：卡册 def、
	// 集齐判定的鱼种数字 id 集合、缺发奖座的构造件。塑封状态与集齐的
	// 权威校验在下方事务内以事务快照复查——否则并发的同 call 请求能同时
	// 通过预检，第二个获写锁后再插一遍奖座（2026-11-08 review P1）。
	deckDef, err := s.DB.GetItemDefByItemID(c, deckIDByGUIDLoose(c, s, uid, deckGUID))
	if err != nil || deckDef == nil || deckDef.Category != 82 {
		ctx.ReplyFailure("System.InvalidOperationException", "卡册定义无效")
		return nil, true
	}
	var attrs struct {
		Cards []string `json:"cjmjfnheidh"`
	}
	if err := json.Unmarshal([]byte(deckDef.AttrsJSON), &attrs); err != nil || len(attrs.Cards) == 0 {
		ctx.ReplyFailure("System.InvalidOperationException", "卡册定义无效")
		return nil, true
	}
	wantNumeric := make(map[uint32]bool, len(attrs.Cards))
	for _, cardID := range attrs.Cards {
		cardDef, cerr := s.DB.GetItemDefByItemID(c, cardID)
		if cerr != nil || cardDef == nil || cardDef.Category != 78 {
			ctx.ReplyFailure("System.InvalidOperationException", "卡册定义无效")
			return nil, true
		}
		wantNumeric[uint32(cardDef.NumericID)] = true
	}
	plan, err := s.prepareDeckStatuettes(c, uid, deckDef.ItemID)
	if err != nil {
		ctx.Log.Warn("卡册塑封奖座准备失败", "deck", deckDef.ItemID, "err", err)
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	// 结算弹窗的正中主体是**卡册本身**（官方实拍：塑封后的卡册卡面 + 封条）。
	// 客户端从响应的物品数组里取卡册来画这块——响应里没有卡册时它取
	// `[0]` 会抛 IndexOutOfRange（Player.log 2026-11-10 18:10 实机），
	// 于是整块正文渲染中断、只剩标题。所以卡册的密封态树必须随响应下发。
	// 树要在事务外构造（ItemToTree 读静态库，写锁期间池化读会饿死），因此把
	// 密封时间戳提前取定，事务内复用同一个值，保证树与落库一致。
	sealTs := uint64(proto.WindowsFiletimeNow())
	deckTree, err := s.buildSealedDeckTree(c, uid, deckGUID, sealTs)
	if err != nil {
		// 卡册树是这条响应能否渲染的前提（客户端在物品数组里找卡册画正中卡面，
		// 找不到就 IndexOutOfRange、整窗只剩标题）。构不出来就不能塑封：
		// 宁可整单失败让玩家重试，也不能提交一件客户端看不到结果的塑封
		// （2026-11-10 review P2：半成功故障）。
		ctx.Log.Warn("卡册塑封响应卡册树构造失败", "deck", deckGUID, "err", err)
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	// 来源码必须在开事务**之前**取：laminateGiftSource → tunableF 在缓存过期
	// （>30s）时会查 tunables 表，而连接池固定 MaxOpenConns(1)，事务已占用唯一
	// 连接，这条查询只能等到连接超时（2026-11-10 review P1 实测 2.0004s），
	// 而且失败后还会把空缓存写回去、配置继续回退默认值。
	giftSource := s.laminateGiftSource()
	tx, err := beginEconomyTx(c, s.DB)
	if err != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	defer tx.Rollback()
	// ── 事务内权威校验与写入（review P1：并发/重放防线）──
	deck, err := getOwnedItemTx(c, tx, uid, deckGUID)
	if err != nil || deck == nil || deck.WireClass != deckClass || deck.Container != tradingCardContainer ||
		deck.ItemID != deckDef.ItemID {
		ctx.ReplyFailure("System.InvalidOperationException", "卡册不存在或不属于当前玩家")
		return nil, true
	}
	fields, err := FieldsFromJSON(deck.FieldsJSON)
	if err != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	if deckSealed(fields) > 0 {
		// 重放/并发的第二发在此拦截（首个事务已提交密封）
		ctx.ReplyFailure("System.InvalidOperationException", "卡册已塑封")
		return nil, true
	}
	cards, err := cardsInDeck(deck)
	if err != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	if !deckComplete(cards, wantNumeric) {
		ctx.ReplyFailure("System.InvalidOperationException", "卡册尚未集齐")
		return nil, true
	}
	fields[1].Num = ptrU64(sealTs)
	if err := updateItemFieldsTx(c, s.DB, tx, deck, fields); err != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	// 塑封奖励之一是「1 个免费技能点」（kDlg_TradingCards_DeckStatusCompleted：
	// 「集齐后可以塑封，获得一个免费技能点和一尊纪念雕像」）。按卡册**实例**
	// 打 kv 标记，标记与加点同事务：重放、并发、解封再塑封都不会重复加。
	pointGranted := false
	if res, perr := tx.ExecContext(c, `INSERT OR IGNORE INTO kv(k,v) VALUES(?,?)`,
		"deck-laminate-point:"+deckGUID, "1"); perr != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	} else if n, aerr := res.RowsAffected(); aerr != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	} else if n > 0 {
		if _, aerr := s.DB.AddSkillBonusTx(c, tx, uid, 1); aerr != nil {
			ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
			return nil, true
		}
		pointGranted = true
	}
	// 奖座幂等补发：是否已有以事务内查询为准，只插缺的
	trees := proto.Args(proto.S("61"))
	guids := make([]string, 0, len(plan.statuettes))
	// 已有奖座的 guid：提交后照样推一次 4/22，客户端按这次塑封把奖座标成「到手」，
	// 不至于因为「本次没有新插入」而始终不刷新（玩家视角就是「奖杯没到账」）。
	existingGUIDs := make([]string, 0, len(plan.statuettes))
	granted := 0
	for _, st := range plan.statuettes {
		existing, terr := scanTxItem(tx.QueryRowContext(c, `SELECT `+txItemCols+
			` FROM item WHERE uid=? AND item_id=? ORDER BY created_ts,guid LIMIT 1`, uid, st))
		if terr == nil {
			tree := plan.trees[existing.GUID]
			if tree == nil || plan.containers[existing.GUID] != existing.Container {
				// 事务外预生成的树必须与事务内实际命中的行一致，避免 ACK 幽灵 GUID。
				ctx.Log.Warn("卡册奖座在事务外准备后状态变化", "item", st, "guid", existing.GUID)
				ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
				return nil, true
			}
			guids = append(guids, existing.GUID)
			existingGUIDs = append(existingGUIDs, existing.GUID)
			trees.Items = append(trees.Items, tree)
			continue
		} else if terr != sql.ErrNoRows {
			ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
			return nil, true
		}
		prepared := plan.prepared[st]
		if prepared == nil {
			// 事务外已拥有但事务内查无：行被并发移动/删除，拒绝重试
			ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
			return nil, true
		}
		row, _, gerr := grantPreparedItemTx(c, tx, prepared, false)
		if gerr != nil {
			ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
			return nil, true
		}
		tree := plan.trees[row.GUID]
		if tree == nil {
			ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
			return nil, true
		}
		granted++
		s.log.Info("卡册塑封发奖座", "uid", uid, "deck", deck.ItemID, "item", st)
		guids = append(guids, row.GUID)
		trees.Items = append(trees.Items, tree)
	}
	// 响应在提交前构造完毕：提交后不再有可失败步骤（review 次要项）。
	//
	// 四段字段序由客户端异常日志反推（Player.log，2026-11-10 实机）：
	//   f0 奖励描述数组（plhpmhpadcg[] = 811 礼物行 args 173）→ 弹窗标题/礼物徽章
	//   f1 物品实例数组（onobdmohahp[]，args 61）→ **卡册本体** + 奖座
	//   f2 第二个物品数组（args 61）→ 卡册本体 + 奖座
	//   f3 Guid[] → 卡册 + 同批奖座 GUID
	// 血泪一：f1 发 U64(0) 实测抛 `InvalidCastException: Unable to cast object of
	// type 'Int64' to type 'onobdmohahp[]'`（gglppphenoi.plphdfehkjl），整条响应
	// 作废、请求序号 60=>62 断号、客户端当场断线重连。
	// 血泪二：f0 给 null 时解码正常但一张卡都不画（重登才看到奖座）——弹窗是 f0 驱动。
	// 血泪三：f0 补上礼物行后弹窗弹出，只剩标题一片空白，客户端报
	// `IndexOutOfRangeException at RF4.Client.UI.GameResult.RUIGameResultWidget.hknalfnfjeg`
	// ——控件在物品数组里找**卡册**画正中那块卡面，找不到就 `[0]` 越界。卡册树
	// 现在排在数组首位：之前只放奖座，排在首位也不顶用，必须是卡册本体。
	itemsVal := func() *proto.Value {
		v := proto.Args(proto.S("61"))
		v.Items = append(v.Items, deckTree)
		v.Items = append(v.Items, trees.Items...)
		return v
	}
	giftItems, giftResults := laminateGiftParts(trees)
	guidArr := make([]string, 0, len(guids)+1)
	guidArr = append(guidArr, deckGUID)
	guidArr = append(guidArr, guids...)
	body, err := proto.EncodeValue(wiregen.MustMakeObj("gglppphenoi", map[int]*proto.Value{
		0: s.laminateGiftRows(giftItems, giftResults, giftSource),
		1: itemsVal(),
		2: itemsVal(),
		3: proto.GuidArr(guidArr),
	}), s.Table())
	if err != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	if err := tx.Commit(); err != nil {
		ctx.ReplyFailure("System.IO.IOException", "卡册塑封失败，请重试")
		return nil, true
	}
	// 会话内刷新：卡册 f1 已变、新奖座进背包。技能点用 2/6 推一次技能树——
	// 客户端按前后可用点差值展示「+1 免费技能点」。
	if pointGranted {
		ctx.AfterResponse(func() { ctx.Sess.Push2_6(1) })
	}
	s.resyncTradingCardsTwice(ctx.Sess, uid)
	for _, prepared := range plan.prepared {
		if row, gerr := s.DB.GetItemByGUID(c, prepared.GUID); gerr == nil && row != nil {
			s.pushItemUpdate(uid, row)
		}
	}
	for _, g := range existingGUIDs {
		if row, gerr := s.DB.GetItemByGUID(c, g); gerr == nil && row != nil {
			s.pushItemUpdate(uid, row)
		}
	}
	ctx.Log.Info("卡册塑封完成", "uid", uid, "deck", deck.ItemID, "rewards", len(trees.Items),
		"granted", granted, "skillPoint", pointGranted,
		"giftSource", giftSource,
		"giftItems", len(giftItems.Items), "giftResults", len(giftResults.Items))
	return body, true
}

// buildSealedDeckTree 事务外构造卡册的**密封态**物品树：把 f1 密封时间戳
// 换成即将落库的 sealTs，再按正式链路 ItemToTree 出树。
//
// 失败**不再降级**为 nil：这条响应靠卡册树画弹窗正中的卡面，缺了它就是本 PR
// 要修的那个故障（IndexOutOfRange → 正文全空）。调用方在开事务前据此
// ReplyFailure，连塑封都不提交。
func (s *Server) buildSealedDeckTree(ctx context.Context, uid int64, deckGUID string, sealTs uint64) (*proto.Value, error) {
	row, err := s.DB.GetItemByGUID(ctx, deckGUID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.UID != uid || row.Container != tradingCardContainer {
		return nil, fmt.Errorf("卡册不存在或不属于当前玩家: guid=%s", deckGUID)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return nil, err
	}
	if len(fields) < 2 {
		return nil, fmt.Errorf("卡册字段不足: guid=%s fields=%d", deckGUID, len(fields))
	}
	fields[1].Num = ptrU64(sealTs)
	// 与 updateItemFieldsTx 同样的收尾（创建刻度前移），保证树和落库行一致。
	bumpCreatedTicks(fields, row.WireClass)
	fj, err := FieldsToJSON(fields)
	if err != nil {
		return nil, err
	}
	sealed := *row
	sealed.FieldsJSON = fj
	tree, err := s.ItemToTree(ctx, &sealed)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, fmt.Errorf("卡册树为空: guid=%s", deckGUID)
	}
	return tree, nil
}

// ── 809.f0 的礼物行与物品槽位 ──
//
// 809 里能放物品的槽位有四个：f1、f2（onobdmohahp[]，画正中那块卡面与陈列台）和
// 811 礼物行的 f2（礼物物品格）、f3（结果数组）。实机教训：
//   1. f1/f2 必须能找出**卡册本体**，否则控件取 [0] 越界、整窗只余标题；
//   2. 811.f3 留空同样整窗空白（2026-11-10 19:01 实证），它也不是可有可无的；
//   3. 811.f2 与 f3 里都放奖座时，礼物详情格子里会出现**两尊**奖座（官方只有一尊），
//      所以 f2 恒空、f3 只放奖座（见 laminateGiftParts 的定型说明）。

// laminateGiftParts 组出 811 礼物行的 f2（礼物物品格）与 f3（结果数组）。
//
// 定型形状（2026-11-10 实机收敛）：**f2 恒空、f3 只放奖座**。理由链：
//   - 礼物详情格 = f2 ∪ f3，每个元素画一格；两段都放奖座会画成两尊（官方只有一尊）；
//   - f3 留空会让控件对 results 取 [0] 越界（IndexOutOfRangeException at
//     RUIGameResultWidget.hknalfnfjeg），整窗只剩标题；
//
// 所以「空的只能是 f2」。
func laminateGiftParts(trees *proto.Value) (items, results *proto.Value) {
	items = proto.Args(proto.S("61"))
	results = proto.Args(proto.S("61"))
	if trees != nil {
		results.Items = append(results.Items, trees.Items...)
	}
	return items, results
}

// laminateGiftRows 构造 809.f0 的奖励描述数组——就是 801.f4/807.f1 同款 811
// 礼物行（外层 args code 173，元素 bkmnmjcfbic/plhpmhpadcg）。塑封的弹窗是
// 礼物卡片驱动：f0 为 null 时客户端不画任何卡，所以哪怕奖座早就发过也要把
// 这行带上（物品取本次 ACK 里的奖座树）。
//
// giftSource 由调用方在**事务外**取好传入（tunableF 会查库，见 hTradingCardLaminate
// 里的 P1 注释），这里不再调用 laminateGiftSource。
//
// 恒为**单行**：多行时客户端切到「徽章横排」的紧凑版式，中间那块卡面陈列会消失。
func (s *Server) laminateGiftRows(items, results *proto.Value, giftSource int) *proto.Value {
	rows := proto.Args(proto.S("173"))
	row := wiregen.MustMakeObj("bkmnmjcfbic", map[int]*proto.Value{
		0: proto.U32(uint32(giftSource)), // fjfejmomhed=来源码
		1: proto.U32(0),                  // kbobbfmmibp=来源参数（塑封无参）
		2: items,                         // nbkhfhenbag=礼物物品
		3: results,                       // fmpbnollnmf=结果物品数组（红线：不发 null）
	})
	rows.Items = append(rows.Items, row)
	return rows
}

// laminateGiftSourceDefault 是 809（卡册塑封结果弹窗）里 f0 礼物行的来源码。
//
// 码值表**不是** Localization.csv 的行序（新级别/游戏活动/卡片册/数条/集齐），
// 客户端的枚举把「数条稀有鱼」排在了卡片册前面：
//
//	1 新级别（要等级参数）、2 游戏活动、3 数条稀有鱼（要数量参数）、
//	4 卡片册、5 集齐该水域
//
// 依据（全部实机，缺一不可）：
//   - 单发 2 → 徽章「为纪念此次游戏活动」（19:50）；
//   - 单发 3 → **一个徽章都不画**（20:14）。3 要 f1 = 稀有鱼数量，参数 0 时整枚跳过，
//     所以它并不是卡片册；
//   - 扫频 1,2,3,4,5（19:33）画出三枚并按**码值降序**排列：集齐(5) → 塑封卡牌合集(4)
//     → 游戏活动(2)；不画的是 1 和 3——两者都要参数、参数为 0。3 缺席反证了 4 = 卡片册；
//   - 单发 6 → 「捕获了该水域的所有稀有鱼」：码表只有 5 档，6 被钳到最后一档；
//     再大的 8 是空号（19:13 只有「+1 免费技能点」）。
//
// 所以塑封要的「塑封卡牌合集」= 4。
const laminateGiftSourceDefault = 4

// laminateGiftSource 取 809.f0 的来源码。留 tunable 覆盖，文案串号时改库 30 秒生效、
// 不必重编。
const laminateGiftSourceKey = "tc_laminate_gift_source"

func (s *Server) laminateGiftSource() int {
	v := s.tunableF(laminateGiftSourceKey, float64(laminateGiftSourceDefault))
	if v <= 0 || v > 255 {
		return laminateGiftSourceDefault
	}
	return int(v)
}

// deckIDByGUIDLoose 事务外按卡册实例 guid 粗取 def id（仅用于静态库预读；
// 权威归属校验在事务内）。
func deckIDByGUIDLoose(ctx context.Context, s *Server, uid int64, deckGUID string) string {
	row, err := s.DB.GetItemByGUID(ctx, deckGUID)
	if err != nil || row == nil || row.UID != uid {
		return deckGUID // 查无此行时原样返回，让 def 校验拒绝
	}
	return row.ItemID
}

// ── 塑封的「免费技能点」 ──
//
// 官方文案（kDlg_TradingCards_DeckStatusCompleted / DeckStatusLaminated）写明：
// 集齐后塑封会得到**一个免费技能点**和一尊纪念雕像。技能点按卡册**实例**记在
// kv `deck-laminate-point:<卡册 guid>` 上，与加点同事务——重放、并发、解封再塑封
// 都不会重复加。修复前塑封过的卡册没有标记，登录时按实例补一次。

// deckLaminatePointKey 是某本卡册实例的「已发过塑封技能点」标记。
func deckLaminatePointKey(deckGUID string) string { return "deck-laminate-point:" + deckGUID }

// grantDeckLaminatePoint 事务内抢标记并加 1 点；抢到返回 true。
func (s *Server) grantDeckLaminatePoint(ctx context.Context, uid int64, deckGUID string) (bool, error) {
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kv(k,v) VALUES(?,?)`,
		deckLaminatePointKey(deckGUID), "1")
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if _, err := s.DB.AddSkillBonusTx(ctx, tx, uid, 1); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ensureDeckLaminatePoints 补发历史塑封的技能点。登录 3/1 时调用，幂等。
func (s *Server) ensureDeckLaminatePoints(ctx context.Context, uid int64) int {
	if s == nil || s.DB == nil || uid == 0 {
		return 0
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `SELECT guid, fields_json FROM item
		WHERE uid=? AND container=? AND item_id LIKE 'tcdeck%'`, uid, tradingCardContainer)
	if err != nil {
		return 0
	}
	var sealed []string
	for rows.Next() {
		var guid, fj string
		if err := rows.Scan(&guid, &fj); err != nil {
			rows.Close()
			return 0
		}
		fields, ferr := FieldsFromJSON(fj)
		if ferr != nil || deckSealed(fields) == 0 {
			continue
		}
		sealed = append(sealed, guid)
	}
	rows.Close()
	granted := 0
	for _, guid := range sealed {
		ok, gerr := s.grantDeckLaminatePoint(ctx, uid, guid)
		if gerr != nil {
			s.log.Warn("卡册塑封技能点补发失败", "uid", uid, "deck", guid, "err", gerr)
			continue
		}
		if ok {
			granted++
		}
	}
	if granted > 0 {
		s.log.Info("补发卡册塑封技能点", "uid", uid, "count", granted)
	}
	return granted
}

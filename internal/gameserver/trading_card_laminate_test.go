package gameserver

import (
	"fmt"
	"sync"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// 27/4 塑封：集齐才可塑封、f1=FILETIME 密封时间戳、奖座幂等补发、
// 塑封后入册/取回被拒；ACK 为 llgjfoldbcc/obj-809。
func TestTradingCardLaminate(t *testing.T) {
	s, db := newProgressSrv(t)
	seedTradingCards(t, db)
	// 补奖座定义与属性
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(60004,'statuette_test',72,'{"jnkpgolkkdj":4,"fjfejmomhed":21}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=json_set(attrs_json,'$.pnaapkplkfo',json('["statuette_test"]'))
		WHERE item_id='tcdeck_test'`); err != nil {
		t.Fatal(err)
	}
	c := t.Context()
	perch, err := s.CreateItem(c, 7, "card_perch", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	crucian, err := s.CreateItem(c, 7, "card_crucian", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	addCtx := tradingAlbumRequest(t, s, 2, 700, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(perch.GUID))
	runTradingAlbumRequest(t, s, addCtx, false)
	deckGUID := deckRowGUID(t, db, 7, "tcdeck_test")
	// 未集齐：塑封被拒
	lamCtx := tradingAlbumRequest(t, s, 4, 701, proto.Guid(deckGUID))
	runTradingAlbumRequest(t, s, lamCtx, true)
	// 集齐第二张 → 塑封成功
	addCtx = tradingAlbumRequest(t, s, 2, 702, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(crucian.GUID))
	runTradingAlbumRequest(t, s, addCtx, false)
	lamCtx = tradingAlbumRequest(t, s, 4, 703, proto.Guid(deckGUID))
	body := runLaminateRequest(t, s, lamCtx)
	v, _, err := proto.DecodeValue(body, 0, s.Table())
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != proto.KindObj || v.TypeID != 809 || len(v.Fields) != 4 {
		t.Fatalf("塑封 ACK 应为 llgjfoldbcc(obj-809, 4 字段): %+v", v)
	}
	// 四段字段序（2026-11-10 实机异常日志反推）：
	//   f0 = 811 礼物行数组（args 173）——弹窗靠它画「礼物 / 塑封卡牌合集」卡
	//   f1 = 物品实例数组（args 61）——**卡册本体**（密封态）+ 奖座
	//   f2 = 同 f1（第二个物品数组）  f3 = Guid[]（卡册 + 同批奖座）
	f0 := v.Fields[0].Val
	if f0.Kind != proto.KindArgs || f0.Code == nil || *f0.Code != "173" || len(f0.Items) != 1 {
		t.Fatalf("f0 应为 1 条 811 礼物行(args 173): %+v", f0)
	}
	row := f0.Items[0]
	if row.Kind != proto.KindObj || row.TypeID != 811 || len(row.Fields) != 4 {
		t.Fatalf("f0 元素应为 811 礼物行 obj: %+v", row)
	}
	// 809 与 801.f4 是同一张 5 档码表（1 新级别/2 活动/3 数条稀有鱼/4 卡片册/5 集齐水域），
	// 塑封对应「塑封卡牌合集」= 4。扫频 1..5 实机只画出 2/4/5 三枚，3 因参数为 0 缺席。
	if src := u64Of(row.Fields[0].Num); src != uint64(laminateGiftSourceDefault) {
		t.Fatalf("811.f0 来源码应为 %d(809 上下文=塑封卡牌合集): %d", laminateGiftSourceDefault, src)
	}
	// 811.f2（礼物物品格）留空：详情格 = f2 ∪ f3，官方实拍只有一格，奖座走 f3。
	if gifts := row.Fields[2].Val; gifts.Kind != proto.KindArgs || gifts.Code == nil ||
		*gifts.Code != "61" || len(gifts.Items) != 0 {
		t.Fatalf("811.f2 应为空物品数组: %+v", gifts)
	}
	// 811.f3（结果数组）不能留空：留空时控件取 [0] 越界、整窗只剩标题。
	if res := row.Fields[3].Val; res.Kind != proto.KindArgs || res.Code == nil ||
		*res.Code != "61" || len(res.Items) != 1 {
		t.Fatalf("811.f3 应含 1 件奖座树: %+v", res)
	}
	// 物品数组首位必须是**卡册本体**（密封态）：控件拿它画弹窗正中那块卡面，
	// 找不到卡册就 IndexOutOfRange、正文全空。奖座排在它后面。
	deck, err := db.GetItemByGUID(c, deckGUID)
	if err != nil {
		t.Fatal(err)
	}
	f1 := v.Fields[1].Val
	if f1.Kind != proto.KindArgs || f1.Code == nil || *f1.Code != "61" || len(f1.Items) != 2 {
		t.Fatalf("f1 应含 [卡册, 奖座] 两棵树: %+v", f1)
	}
	if head := f1.Items[0]; head.Kind != proto.KindObj || head.TypeID != uint32(deck.TypeID) {
		t.Fatalf("f1 首元素应为卡册树(typeID=%d): %+v", deck.TypeID, head)
	}
	if f2 := v.Fields[2].Val; f2.Kind != proto.KindArgs || f2.Code == nil ||
		*f2.Code != "61" || len(f2.Items) != 2 {
		t.Fatalf("f2 应含 [卡册, 奖座] 两棵树: %+v", f2)
	}
	f3 := v.Fields[3].Val
	if f3.Kind != proto.KindGuidArr || len(f3.Items) != 2 {
		t.Fatalf("f3 应带卡册 + 奖座 GUID: %+v", f3)
	}
	if got := f3.Items[0].GuidHex(); got != deckGUID {
		t.Fatalf("f3 首元素应为卡册 GUID %s，实得 %s", deckGUID, got)
	}
	// 塑封奖励含 1 个免费技能点（kv skill_bonus 偏移 +1）。
	if bonus, _, _ := db.KVGet(c, fmt.Sprintf("skill_bonus:%d", 7)); bonus != "1" {
		t.Fatalf("塑封应加 1 个免费技能点，实得 skill_bonus=%q", bonus)
	}
	// f1 时间戳 = FILETIME（约 1.3e17 量级；.NET Ticks 是 6.4e17 量级，
	// 发错单位客户端 FromFileTime 会渲染成 +1600 年）
	deck, err = db.GetItemByGUID(c, deckGUID)
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := FieldsFromJSON(deck.FieldsJSON)
	if ts := deckSealed(fields); ts < 130_000_000_000_000_000 {
		t.Fatalf("密封时间戳应为 FILETIME: %d", ts)
	}
	// 奖座已发一座且不重复
	if n := countRows(t, db, 7, "statuette_test"); n != 1 {
		t.Fatalf("奖座应恰一座: %d", n)
	}
	// 塑封后：取回被拒、再入册被拒、重复塑封被拒
	rmCtx := tradingAlbumRequest(t, s, 3, 704, proto.Guid(perch.GUID))
	runTradingAlbumRequest(t, s, rmCtx, true)
	another, err := s.CreateItem(c, 7, "card_perch", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	addCtx = tradingAlbumRequest(t, s, 2, 705, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(another.GUID))
	runTradingAlbumRequest(t, s, addCtx, true)
	lamCtx = tradingAlbumRequest(t, s, 4, 706, proto.Guid(deckGUID))
	runTradingAlbumRequest(t, s, lamCtx, true)
	if n := countRows(t, db, 7, "statuette_test"); n != 1 {
		t.Fatalf("重复塑封不得再发奖座: %d", n)
	}
	// 解封重塑不会重复加点：技能点按卡册实例打标记。
	if bonus, _, _ := db.KVGet(c, fmt.Sprintf("skill_bonus:%d", 7)); bonus != "1" {
		t.Fatalf("重复塑封不得重复加技能点，实得 skill_bonus=%q", bonus)
	}
}

// 修复前塑封过的卡册（已密封、没有 kv 标记）登录时应补 1 点，且只补一次。
func TestEnsureDeckLaminatePointsBackfillsSealedDeck(t *testing.T) {
	s, db := newProgressSrv(t)
	seedTradingCards(t, db)
	c := t.Context()
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(60004,'statuette_test',72,'{"jnkpgolkkdj":4,"fjfejmomhed":21}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=json_set(attrs_json,'$.pnaapkplkfo',json('["statuette_test"]'))
		WHERE item_id='tcdeck_test'`); err != nil {
		t.Fatal(err)
	}
	deck, err := s.CreateItem(c, 7, "tcdeck_test", tradingCardContainer, 1)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := FieldsFromJSON(deck.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	fields[1].Num = ptrU64(uint64(proto.WindowsFiletimeNow()))
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item SET fields_json=? WHERE guid=?`, fj, deck.GUID); err != nil {
		t.Fatal(err)
	}
	if n := s.ensureDeckLaminatePoints(c, 7); n != 1 {
		t.Fatalf("登录补发应为 1 本卡册，实得 %d", n)
	}
	if bonus, _, _ := db.KVGet(c, "skill_bonus:7"); bonus != "1" {
		t.Fatalf("补发后 skill_bonus 应为 1，实得 %q", bonus)
	}
	if n := s.ensureDeckLaminatePoints(c, 7); n != 0 {
		t.Fatalf("补发必须幂等，第二次实得 %d", n)
	}
	if bonus, _, _ := db.KVGet(c, "skill_bonus:7"); bonus != "1" {
		t.Fatalf("重复补发不得重复加点，实得 skill_bonus=%q", bonus)
	}
}

func deckRowGUID(t *testing.T, db *store.DB, uid int64, itemID string) string {
	t.Helper()
	rows, err := db.ListItemsByContainer(t.Context(), uid, tradingCardContainer)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ItemID == itemID {
			return r.GUID
		}
	}
	t.Fatalf("卡册行缺失: %s", itemID)
	return ""
}

func countRows(t *testing.T, db *store.DB, uid int64, itemID string) int {
	t.Helper()
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, itemID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// runLaminateRequest 执行 27/4 并断言成功响应（809 形状由具体用例校验）。
func runLaminateRequest(t *testing.T, s *Server, ctx *Ctx) []byte {
	t.Helper()
	result := s.dispatch.executeRequest(ctx, true)
	if result.errorText != "" {
		if fv, _, derr := proto.DecodeValue(result.body, 0, s.Table()); derr == nil {
			var parts []string
			for _, f := range fv.Fields {
				if f.Str != nil {
					parts = append(parts, *f.Str)
				}
			}
			t.Fatalf("laminate error: %s parts=%v", result.errorText, parts)
		}
		t.Fatalf("laminate error: %s native=%v", result.errorText, result.nativeFailure)
	}
	if !result.hasResponse || result.failed {
		t.Fatalf("laminate response: %+v", result)
	}
	return result.body
}

// 奖座可被 4/6 移到房屋等非背包容器；塑封 ACK 必须返回实际已拥有行的 GUID，
// 不能返回事务外预构造但最终未落库的幽灵奖励。
func TestTradingCardLaminateExistingStatuetteOutsideBackpack(t *testing.T) {
	s, db := newProgressSrv(t)
	seedTradingCards(t, db)
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(60004,'statuette_test',72,'{"jnkpgolkkdj":4,"fjfejmomhed":21}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=json_set(attrs_json,'$.pnaapkplkfo',json('["statuette_test"]'))
		WHERE item_id='tcdeck_test'`); err != nil {
		t.Fatal(err)
	}
	c := t.Context()
	perch, err := s.CreateItem(c, 7, "card_perch", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	crucian, err := s.CreateItem(c, 7, "card_crucian", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	addCtx := tradingAlbumRequest(t, s, 2, 710, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(perch.GUID))
	runTradingAlbumRequest(t, s, addCtx, false)
	addCtx = tradingAlbumRequest(t, s, 2, 711, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(crucian.GUID))
	runTradingAlbumRequest(t, s, addCtx, false)
	deckGUID := deckRowGUID(t, db, 7, "tcdeck_test")
	trophy, err := s.CreateItem(c, 7, "statuette_test", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item SET container='house' WHERE guid=?`, trophy.GUID); err != nil {
		t.Fatal(err)
	}
	body := runLaminateRequest(t, s, tradingAlbumRequest(t, s, 4, 712, proto.Guid(deckGUID)))
	v, _, err := proto.DecodeValue(body, 0, s.Table())
	if err != nil {
		t.Fatal(err)
	}
	// f1 首元素是卡册本体，奖座排在后面：按成员判定，不看首位。
	found := false
	for _, it := range v.Fields[1].Val.Items {
		if findTradingTreeGUID(it) == trophy.GUID {
			found = true
		}
	}
	if !found {
		t.Fatalf("塑封 ACK 未带已拥有的奖座 %s", trophy.GUID)
	}
	if n := countRows(t, db, 7, "statuette_test"); n != 1 {
		t.Fatalf("已有奖座不得重发: %d", n)
	}
	row, err := db.GetItemByGUID(c, trophy.GUID)
	if err != nil || row.Container != "house" {
		t.Fatalf("已有奖座容器被改变: %v %+v", err, row)
	}
}

func findTradingTreeGUID(v *proto.Value) string {
	if v == nil {
		return ""
	}
	if v.Kind == proto.KindObj {
		if idx := fieldIdxByCS(v.Class, csGUID); idx >= 0 && idx < len(v.Fields) {
			return v.Fields[idx].Hex
		}
		for _, f := range v.Fields {
			if guid := findTradingTreeGUID(f.Val); guid != "" {
				return guid
			}
		}
	}
	for _, item := range v.Items {
		if guid := findTradingTreeGUID(item); guid != "" {
			return guid
		}
	}
	return ""
}

// 不同 call_id 的并发塑封必须只有一个请求能发奖座；事务内复查是幂等防线。
func TestTradingCardLaminateConcurrent(t *testing.T) {
	s, db := newProgressSrv(t)
	seedTradingCards(t, db)
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(60004,'statuette_test',72,'{"jnkpgolkkdj":4,"fjfejmomhed":21}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=json_set(attrs_json,'$.pnaapkplkfo',json('["statuette_test"]'))
		WHERE item_id='tcdeck_test'`); err != nil {
		t.Fatal(err)
	}
	c := t.Context()
	for i := 0; i < 20; i++ {
		if _, err := db.SQL.Exec(`DELETE FROM item WHERE uid=7`); err != nil {
			t.Fatal(err)
		}
		perch, err := s.CreateItem(c, 7, "card_perch", "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		crucian, err := s.CreateItem(c, 7, "card_crucian", "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		addCtx := tradingAlbumRequest(t, s, 2, uint32(720+i*10), proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(perch.GUID))
		runTradingAlbumRequest(t, s, addCtx, false)
		addCtx = tradingAlbumRequest(t, s, 2, uint32(721+i*10), proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(crucian.GUID))
		runTradingAlbumRequest(t, s, addCtx, false)
		deckGUID := deckRowGUID(t, db, 7, "tcdeck_test")
		start := make(chan struct{})
		var wg sync.WaitGroup
		for j := 0; j < 2; j++ {
			ctx := tradingAlbumRequest(t, s, 4, uint32(722+i*10+j), proto.Guid(deckGUID))
			wg.Add(1)
			go func(ctx *Ctx) {
				defer wg.Done()
				<-start
				_ = s.dispatch.executeRequest(ctx, true)
			}(ctx)
		}
		close(start)
		wg.Wait()
		if n := countRows(t, db, 7, "statuette_test"); n != 1 {
			t.Fatalf("iteration %d: concurrent laminate granted %d statuettes", i, n)
		}
	}
}

// 卡册树构造失败时不能「提交了塑封、返回缺卡册的响应」——那正是本 PR 要修的
// 半成功故障（客户端在物品数组里找卡册画正中卡面，找不到就 IndexOutOfRange、
// 整窗只剩标题）。2026-11-10 review P2。
//
// 构造失败最直接的注入点是卡册实例的 fields_json 坏掉：buildSealedDeckTree
// 在事务外解不出字段，必须在开事务前就 ReplyFailure，塑封时间戳、技能点、
// 奖座一样都不能落库。
func TestTradingCardLaminateRejectsUnbuildableDeckTree(t *testing.T) {
	s, db := newProgressSrv(t)
	seedTradingCards(t, db)
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(60004,'statuette_test',72,'{"jnkpgolkkdj":4,"fjfejmomhed":21}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=json_set(attrs_json,'$.pnaapkplkfo',json('["statuette_test"]'))
		WHERE item_id='tcdeck_test'`); err != nil {
		t.Fatal(err)
	}
	c := t.Context()
	perch, err := s.CreateItem(c, 7, "card_perch", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	crucian, err := s.CreateItem(c, 7, "card_crucian", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	addCtx := tradingAlbumRequest(t, s, 2, 760, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(perch.GUID))
	runTradingAlbumRequest(t, s, addCtx, false)
	addCtx = tradingAlbumRequest(t, s, 2, 761, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(crucian.GUID))
	runTradingAlbumRequest(t, s, addCtx, false)
	deckGUID := deckRowGUID(t, db, 7, "tcdeck_test")
	// 把卡册实例字段弄成不可解析，模拟「树构不出来」。
	if _, err := db.SQL.Exec(`UPDATE item SET fields_json='{' WHERE guid=?`, deckGUID); err != nil {
		t.Fatal(err)
	}
	lamCtx := tradingAlbumRequest(t, s, 4, 762, proto.Guid(deckGUID))
	runTradingAlbumRequest(t, s, lamCtx, true)
	// 整单拒绝：字段没被改写、技能点没加、奖座没发、幂等标记没写。
	var fieldsJSON string
	if err := db.SQL.QueryRow(`SELECT fields_json FROM item WHERE guid=?`, deckGUID).Scan(&fieldsJSON); err != nil {
		t.Fatal(err)
	}
	if fieldsJSON != "{" {
		t.Fatalf("卡册树构造失败时不应改写卡册字段，实得 %q", fieldsJSON)
	}
	if bonus, _, _ := db.KVGet(c, "skill_bonus:7"); bonus != "" {
		t.Fatalf("卡册树构造失败时不应加技能点，实得 %q", bonus)
	}
	if _, _, err := db.KVGet(c, deckLaminatePointKey(deckGUID)); err == nil {
		t.Fatal("卡册树构造失败时不应写塑封技能点幂等标记")
	}
	if n := countRows(t, db, 7, "statuette_test"); n != 0 {
		t.Fatalf("卡册树构造失败时不应发奖座，实得 %d 尊", n)
	}
}

// buildSealedDeckTree 的契约：构不出卡册树必须返回 error，不能返回 nil 让调用方
// 悄悄降级成「只有奖座」的响应（那正是客户端 IndexOutOfRange 的成因）。
func TestBuildSealedDeckTreeRequiresDeck(t *testing.T) {
	s, db := newProgressSrv(t)
	seedTradingCards(t, db)
	c := t.Context()
	perch, err := s.CreateItem(c, 7, "card_perch", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	addCtx := tradingAlbumRequest(t, s, 2, 780, proto.Str(proto.S("tcdeck_test")), proto.Guid(zeroGUID), proto.Guid(perch.GUID))
	runTradingAlbumRequest(t, s, addCtx, false)
	deckGUID := deckRowGUID(t, db, 7, "tcdeck_test")
	// 健康卡册：树必须构得出来（否则下面几条断言就成了「永远失败」）。
	if tree, err := s.buildSealedDeckTree(c, 7, deckGUID, 1); err != nil || tree == nil {
		t.Fatalf("健康卡册应能构造密封态树: tree=%v err=%v", tree, err)
	}
	// 字段坏掉 → error，不是 nil。
	if _, err := db.SQL.Exec(`UPDATE item SET fields_json='{' WHERE guid=?`, deckGUID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.buildSealedDeckTree(c, 7, deckGUID, 1); err == nil {
		t.Fatal("fields_json 损坏时 buildSealedDeckTree 必须返回 error（不得降级为 nil）")
	}
	// 字段被清空（只有 f0 都没有）→ error，不能让事务里按 fields[1] 取密封位。
	if _, err := db.SQL.Exec(`UPDATE item SET fields_json='[]' WHERE guid=?`, deckGUID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.buildSealedDeckTree(c, 7, deckGUID, 1); err == nil {
		t.Fatal("卡册字段为空时 buildSealedDeckTree 必须返回 error")
	}
	// 归属不符 → error。
	if _, err := s.buildSealedDeckTree(c, 8, deckGUID, 1); err == nil {
		t.Fatal("非本人卡册时 buildSealedDeckTree 必须返回 error")
	}
}

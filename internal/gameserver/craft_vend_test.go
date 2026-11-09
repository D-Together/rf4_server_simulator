package gameserver

// 贩卖机配方（*_vend）付费制作：机器按货架最低价卖料+搅拌一步完成，
// 玩家不需要持有原料；家用版配方保持"必须持有材料"不变。
// 期望总额用手算字面量：鲫鱼窝料配料实抓货架价 mix_groundpeas 290 +
// feedadds_flymaggots 120 + feedadds_corn 50 + aroma_vanila 170 = 630。

import (
	"context"
	"encoding/json"
	"testing"

	"rf4server/internal/proto"
)

func silverOf(t *testing.T, srv *Server, uid int64) int64 {
	t.Helper()
	var n int64
	if err := srv.DB.SQL.QueryRow(`SELECT silver FROM profile WHERE uid=?`, uid).Scan(&n); err != nil {
		t.Fatalf("读取余额失败: %v", err)
	}
	return n
}

// vendOK 用忠实还原实抓报文的树执行一次 9/1（craftOK 的槽 f2 恒为空，
// 而贩卖机通用槽定价恰恰依赖 f2 的具体 item id），返回 803.f0。
func vendOK(t *testing.T, srv *Server, uid int64, recipe string, slots []craftSlot, qty uint64) bool {
	t.Helper()
	body, ok := hCraft(economyTestCtx(srv, uid, vendCraftTree(recipe, slots, qty)))
	if !ok || len(body) == 0 {
		t.Fatalf("9/1 未处理: ok=%v body=%x", ok, body)
	}
	resp := decodeTreeValue(t, srv, body)
	if len(resp.Fields) == 0 || resp.Fields[0].Num == nil {
		t.Fatalf("803 缺少 f0: %+v", resp)
	}
	return u64Of(resp.Fields[0].Num) != 0
}

// vendCarpTree 构造鲫鱼窝料贩卖机请求：槽 GUID 是客户端临时值（库里不存在，
// 服务端按需求 id 的显式 item_id 定价），f2 携带具体 item id 同实抓报文。
func vendCarpTree(recipe string, qty uint64) *proto.Value {
	slots := []craftSlot{
		{ItemID: "craft_feed_mix_goroh", GUID: "f240a0e29ed5d148bd1574391f593faa", Species: "mix_groundpeas"},
		{ItemID: "craft_feed_add_oparishu", GUID: "9f75588faa9c4e8a20b6b8316f02aa13", Species: "feedadds_flymaggots"},
		{ItemID: "craft_feed_add_kukuruza", GUID: "36847c9bc869994e9a9789657d263854", Species: "feedadds_corn"},
		{ItemID: "craft_feed_aroma_vanil", GUID: "c4e2b1ec8f7a53428b38fbe221c5a911", Species: "aroma_vanila"},
	}
	return vendCraftTree(recipe, slots, qty)
}

// vendCraftTree 同 economyCraftTree，但槽 f2（实抓里的具体 item id）可填值。
func vendCraftTree(recipe string, slots []craftSlot, qty uint64) *proto.Value {
	arg := proto.Args(proto.S("11"))
	for _, sl := range slots {
		arg.Items = append(arg.Items, proto.Obj("bhfikaplhmh", 9,
			proto.FStr("STR", proto.S(sl.ItemID)),
			proto.FGuid(sl.GUID),
			proto.FStr("STR", proto.S(sl.Species)),
		))
	}
	return proto.Args524(proto.Obj("dhkphmjinmj", 8,
		proto.FStr("STR", proto.S(recipe)),
		proto.FVal("VAL", proto.Args(proto.S("11"))),
		proto.FVal("VAL", arg),
		proto.FOpt(0),
		proto.FStr("U16STR", nil),
		proto.FNum("U32", qty),
	))
}

func TestVendCraftChargesSilverAndSkipsMaterials(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx0 := context.Background()
	const uid = int64(4201)
	if _, err := db.EnsureProfile(ctx0, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=1000 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}

	// 槽 GUID 全是客户端临时值（craftOK 的树里甚至没有材料槽），
	// 背包里没有任何原料，制作应一步成功。
	if !craftOK(t, srv, uid, "craft_feed_carp_vend", nil, 1) {
		t.Fatal("贩卖机配方应一步成功（803.f0=1）")
	}
	if got := silverOf(t, srv, uid); got != 1000-630 {
		t.Fatalf("应扣 630 银币，余额应为 370，实得 %d", got)
	}
	if got := countOfItem(t, srv, ctx0, uid, "feed_karp"); got != 60 {
		t.Fatalf("应产出 60 份鲫鱼窝料（品类 46 每套 60），实得 %d", got)
	}
	if got := countOfItem(t, srv, ctx0, uid, "mix_groundpeas"); got != 0 {
		t.Fatalf("背包不应出现原料 mix_groundpeas，实得 %d", got)
	}
	// 自制窝料实例用料记录：4 种配料各 1 份（groundbait_recipe.mats_json）。
	var matsJSON string
	if err := srv.DB.SQL.QueryRow(
		`SELECT mats_json FROM groundbait_recipe ORDER BY created_ts DESC LIMIT 1`).Scan(&matsJSON); err != nil {
		t.Fatalf("读取窝料用料记录失败: %v", err)
	}
	var mats []string
	if err := json.Unmarshal([]byte(matsJSON), &mats); err != nil {
		t.Fatalf("用料记录不是 JSON 数组: %v", err)
	}
	want := map[string]bool{
		"mix_groundpeas": true, "feedadds_flymaggots": true,
		"feedadds_corn": true, "aroma_vanila": true,
	}
	if len(mats) != len(want) {
		t.Fatalf("实例用料应记 4 条，实得 %d: %v", len(mats), mats)
	}
	for _, m := range mats {
		if !want[m] {
			t.Fatalf("用料记录出现意外物品 %s: %v", m, mats)
		}
		delete(want, m)
	}
}

func TestVendCraftInsufficientSilverFailsAtomic(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx0 := context.Background()
	const uid = int64(4203)
	if _, err := db.EnsureProfile(ctx0, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=100 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	if craftOK(t, srv, uid, "craft_feed_carp_vend", nil, 1) {
		t.Fatal("余额不足时贩卖机制作必须失败")
	}
	if got := silverOf(t, srv, uid); got != 100 {
		t.Fatalf("失败不应扣款，余额应为 100，实得 %d", got)
	}
	if got := countOfItem(t, srv, ctx0, uid, "feed_karp"); got != 0 {
		t.Fatalf("失败不应产出，实得 %d", got)
	}
}

func TestVendCraftQuotePrice(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	const uid = int64(4204)
	if _, err := db.EnsureProfile(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	quote := func(qty uint64) (int64, int64) {
		t.Helper()
		body, ok := hCraftCost(economyTestCtx(srv, uid, vendCarpTree("craft_feed_carp_vend", qty)))
		if !ok || len(body) == 0 {
			t.Fatalf("9/4 未处理: ok=%v body=%x", ok, body)
		}
		resp := decodeTreeValue(t, srv, body)
		if len(resp.Fields) < 4 || resp.Fields[1].Num == nil || resp.Fields[3].Num == nil {
			t.Fatalf("501 缺少 f1/f3: %+v", resp)
		}
		return int64(u64Of(resp.Fields[1].Num)), int64(u64Of(resp.Fields[3].Num))
	}
	if silver, gotQty := quote(3); silver != 630*3 || gotQty != 3 {
		t.Fatalf("qty=3 报价应为 1890/3，实得 %d/%d", silver, gotQty)
	}
	// 超上限（窝料单次 60 套）报价与实际扣款口径一致。
	if silver, gotQty := quote(1000); silver != 630*60 || gotQty != 60 {
		t.Fatalf("qty=1000 应收敛到 60 套报价 37800，实得 %d/%d", silver, gotQty)
	}
}

func TestVendCraftGenericDemandsTakeSlotItems(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx0 := context.Background()
	const uid = int64(4205)
	if _, err := db.EnsureProfile(ctx0, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=10000 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	// universal_vend 的 6 条需求全部没有显式 item_id：混合料按类别 18 校验，
	// 4 个添加剂按标签组（无法反查、按槽序取），香精按类别 20 校验。
	// 手算总价 = 290 + 120 + 50 + 110 + 100 + 170 = 840。
	slots := []craftSlot{
		{ItemID: "craft_feed_generic_mix_1", GUID: "aa000000000000000000000000000001", Species: "mix_groundpeas"},
		{ItemID: "craft_feed_generic_add_1", GUID: "aa000000000000000000000000000002", Species: "feedadds_flymaggots"},
		{ItemID: "craft_feed_generic_add_2", GUID: "aa000000000000000000000000000003", Species: "feedadds_corn"},
		{ItemID: "craft_feed_generic_add_3", GUID: "aa000000000000000000000000000004", Species: "feedadds_bloodworms"},
		{ItemID: "craft_feed_pva_add_4", GUID: "aa000000000000000000000000000005", Species: "feedadds_peas"},
		{ItemID: "craft_feed_generic_aroma_1", GUID: "aa000000000000000000000000000006", Species: "aroma_vanila"},
	}
	if !vendOK(t, srv, uid, "craft_feed_universal_vend", slots, 1) {
		t.Fatal("通用槽配方应按槽内具体物品定价并成功")
	}
	if got := silverOf(t, srv, uid); got != 10000-840 {
		t.Fatalf("应扣 840 银币，余额应为 9160，实得 %d", got)
	}
}

func TestHomeCarpCraftStillRequiresMaterials(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx0 := context.Background()
	const uid = int64(4206)
	if _, err := db.EnsureProfile(ctx0, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=100000 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	// 家用版语义不变：没有原料时即使有钱也失败。
	if craftOK(t, srv, uid, "craft_feed_carp", nil, 1) {
		t.Fatal("家用配方无材料必须失败（语义未变）")
	}
	if got := countOfItem(t, srv, ctx0, uid, "feed_karp"); got != 0 {
		t.Fatalf("家用失败不应产出，实得 %d", got)
	}
}

package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/proto"
)

const rewardTestCreatedTS = 1700000000

// assertRewardFloat 断言 U32 槽里承载的是 IEEE754 float 位模式，且解析值等于 want。
// 客户端 onobdmohahp 把工艺/耐久声明为 float，用 proto.U32 送整数会读成接近 0。
func assertRewardFloat(t *testing.T, f proto.Field, want float32, name string) {
	t.Helper()
	if f.Num == nil {
		t.Fatalf("%s: Num 为空", name)
	}
	bits := uint32(u64Of(f.Num))
	if got := proto.F32FromBits(bits); got != want {
		t.Fatalf("%s=%v (位模式 0x%08x), 期望 %v", name, got, bits, want)
	}
}

// TestPlayerRewards2015ReturnsOwnedMedal 验证 3/16 奖赏列表：
// 返回 proto.Args("185", enikeammfen...)，包含玩家 house 里的奖牌物品，
// 且 f0(名次)/f1(可见性)/f6/f7(float 工艺耐久)/f8/f9(物品时间) 正确。
func TestPlayerRewards2015ReturnsOwnedMedal(t *testing.T) {
	srv, db := newRigTestServer(t)
	registerAchievementHandlers(srv.dispatch)
	c := dynamicTestCtx(srv, nil)
	uid := c.Sess.Userid

	// 必须是 32 位 hex 的合法 guid，否则 proto.Guid 会归一化为全 0。
	medalGUID := "0123456789abcdef0123456789abcdef"
	_, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO item (guid, uid, item_id, numeric_id, wire_class, type_id, container, slot, qty, durability, workmanship, created_ts)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		medalGUID, uid, "ft_big_bear_cup_medal_2", 6808, "enikeammfen", 2015, "house", 0, 1, 1.0, 1.0, rewardTestCreatedTS)
	if err != nil {
		t.Fatalf("插入测试奖牌失败: %v", err)
	}

	body, ok := hPlayerRewards2015(c)
	if !ok {
		t.Fatal("3/16 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if dec.Code == nil || *dec.Code != "185" {
		t.Fatalf("3/16 响应 code=%v, 期望 185", dec.Code)
	}
	var found *proto.Value
	for _, it := range dec.Items {
		if it.Kind == proto.KindObj && it.Class == "enikeammfen" && it.TypeID == 2015 {
			f2 := it.Field(2)
			if f2.GuidHex() == medalGUID {
				found = it
				break
			}
		}
	}
	if found == nil {
		t.Fatalf("3/16 未返回测试奖牌 %s", medalGUID)
	}
	if got := u64Of(found.Fields[5].Num); got != 6808 {
		t.Fatalf("3/16 奖牌 f5=%d, 期望 6808", got)
	}
	if got := u64Of(found.Fields[0].Num); got != 2 {
		t.Fatalf("3/16 奖牌 f0(名次)=%d, 期望 2", got)
	}
	if got := u64Of(found.Fields[1].Num); got != 0 {
		t.Fatalf("3/16 奖牌 f1(可见性)=%d, 期望 0(可见)", got)
	}
	assertRewardFloat(t, found.Field(6), 1.0, "3/16 f6(工艺)")
	assertRewardFloat(t, found.Field(7), 1.0, "3/16 f7(耐久)")
	if want := uint64(netTicksFromUnix(rewardTestCreatedTS)); u64Of(found.Fields[8].Num) != want {
		t.Fatalf("3/16 奖牌 f8(创建 .NET ticks)=%d, 期望 %d", u64Of(found.Fields[8].Num), want)
	}
	if want := uint64(proto.UnixToFiletime(rewardTestCreatedTS)); u64Of(found.Fields[9].Num) != want {
		t.Fatalf("3/16 奖牌 f9(更新 FILETIME)=%d, 期望 %d", u64Of(found.Fields[9].Num), want)
	}
}

// TestPlayerRewards2015RejectsMismatchedRows 验证 wire 类与 type_id 必须同时匹配：
// 只满足其中一个条件的异常行不得进入 2015 响应。
func TestPlayerRewards2015RejectsMismatchedRows(t *testing.T) {
	srv, db := newRigTestServer(t)
	registerAchievementHandlers(srv.dispatch)
	c := dynamicTestCtx(srv, nil)
	uid := c.Sess.Userid

	rows := []struct {
		guid, wireClass string
		typeID          int
	}{
		{"11111111111111111111111111111111", "enikeammfen", 305},  // 类对、type 错
		{"22222222222222222222222222222222", "otherclass", 2015},  // type 对、类错
		{"33333333333333333333333333333333", "enikeammfen", 2015}, // 两者都对
	}
	for _, r := range rows {
		if _, err := db.SQL.ExecContext(context.Background(),
			`INSERT INTO item (guid, uid, item_id, numeric_id, wire_class, type_id, container, slot, qty, durability, workmanship, created_ts)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.guid, uid, "ft_test_medal", 6807, r.wireClass, r.typeID, "house", 0, 1, 1.0, 1.0, rewardTestCreatedTS); err != nil {
			t.Fatalf("插入测试行 %s 失败: %v", r.guid, err)
		}
	}

	body, ok := hPlayerRewards2015(c)
	if !ok {
		t.Fatal("3/16 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Items) != 1 {
		t.Fatalf("3/16 返回 %d 条, 期望只返回 1 条同时匹配的行", len(dec.Items))
	}
	f2 := dec.Items[0].Field(2)
	if got := f2.GuidHex(); got != "33333333333333333333333333333333" {
		t.Fatalf("3/16 返回了异常行 guid=%s", got)
	}
}

// TestDynamicRewardEntryUsesItemFields 验证 3/40 详情命中真实物品时
// 与 3/16 列表口径一致：f6/f7 取物品工艺/耐久，f8/f9 取物品 created_ts。
func TestDynamicRewardEntryUsesItemFields(t *testing.T) {
	srv, db := newRigTestServer(t)
	c := dynamicTestCtx(srv, nil)
	uid := c.Sess.Userid

	guid := "fedcba9876543210fedcba9876543210"
	if _, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO item (guid, uid, item_id, numeric_id, wire_class, type_id, container, slot, qty, durability, workmanship, created_ts)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		guid, uid, "ft_big_bear_cup_medal_1", 6807, "enikeammfen", 2015, "house", 0, 3, 0.5, 0.75, rewardTestCreatedTS); err != nil {
		t.Fatalf("插入测试奖牌失败: %v", err)
	}

	c.Tree = rigRequestTree(t, proto.Guid(guid), proto.Bool(true))
	body, ok := hDynamicRewardEntry(c)
	if !ok {
		t.Fatal("3/40 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if dec.Class != "enikeammfen" || dec.TypeID != 2015 {
		t.Fatalf("3/40 响应类型=%s/%d", dec.Class, dec.TypeID)
	}
	if got := u64Of(dec.Field(13).Num); got != 3 {
		t.Fatalf("3/40 f13(数量)=%d, 期望 3", got)
	}
	assertRewardFloat(t, dec.Field(6), 0.75, "3/40 f6(工艺)")
	assertRewardFloat(t, dec.Field(7), 0.5, "3/40 f7(耐久)")
	if want := uint64(netTicksFromUnix(rewardTestCreatedTS)); u64Of(dec.Field(8).Num) != want {
		t.Fatalf("3/40 f8(创建 .NET ticks)=%d, 期望 %d", u64Of(dec.Field(8).Num), want)
	}
	if want := uint64(proto.UnixToFiletime(rewardTestCreatedTS)); u64Of(dec.Field(9).Num) != want {
		t.Fatalf("3/40 f9(更新 FILETIME)=%d, 期望 %d", u64Of(dec.Field(9).Num), want)
	}
}

func TestPlaceOfItem(t *testing.T) {
	cases := map[string]uint32{
		"ft_big_bear_cup_medal_1": 1,
		"ft_big_bear_cup_medal_2": 2,
		"ft_big_bear_cup_medal_3": 3,
		"weekly_medal_1":          1,
		"weekly_medal_3":          3,
		"ft_5th_anniversary_2":    2,
		"ann_rec_1":               1,
		"ann_rec_5":               5,
		"ann_rec_bl_2":            2,
		"ann_rec_sea_4":           4,
		"ann_rec_ul_5":            5,
		// 非名次类：计数/等级后缀不得误判
		"statuette_fisherman_10":   0,
		"statuette_supertrophy_50": 0,
		"trophy_level_001_lake":    0,
		"ny_2024_cup":              0,
		"qual_carp_cup":            0,
		"ft_extremal":              0,
	}
	for id, want := range cases {
		if got := placeOfItem(id); got != want {
			t.Errorf("placeOfItem(%q)=%d, 期望 %d", id, got, want)
		}
	}
}

func TestDynamicRewardVisibilityPersistsToList(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := t.Context()
	const uid = int64(1000001)
	if err := srv.EnsurePlayer(ctx, uid); err != nil {
		t.Fatal(err)
	}
	item, err := srv.CreateItem(ctx, uid, "ft_big_bear_cup_medal_1", "house", 1)
	if err != nil {
		t.Fatal(err)
	}

	c := dynamicTestCtx(srv, rigRequestTree(t, proto.Guid(item.GUID), proto.Bool(true)))
	body, ok := hDynamicRewardEntry(c)
	if !ok {
		t.Fatal("3/40 隐藏请求未处理")
	}
	detail, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	visibility := detail.Field(1)
	if got := visibility.U32(); got != 1 {
		t.Fatalf("3/40 隐藏后 f1=%d，期望 1", got)
	}

	fresh := mustItem(t, db, item.GUID)
	fields, err := FieldsFromJSON(fresh.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	visibility = fields[1]
	if got := visibility.U32(); got != 1 {
		t.Fatalf("隐藏状态未写入物品字段: %d", got)
	}

	c = dynamicTestCtx(srv, nil)
	body, ok = hPlayerRewards2015(c)
	if !ok {
		t.Fatal("3/16 列表未处理")
	}
	list, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	var found *proto.Value
	for _, entry := range list.Items {
		guidField := entry.Field(2)
		if guidField.GuidHex() == item.GUID {
			found = entry
			break
		}
	}
	if found == nil {
		t.Fatal("3/16 未返回奖赏")
	}
	visibility = found.Field(1)
	if got := visibility.U32(); got != 1 {
		t.Fatalf("3/16 列表未反映隐藏状态: %d", got)
	}

	c = dynamicTestCtx(srv, rigRequestTree(t, proto.Guid(item.GUID), proto.Bool(false)))
	body, ok = hDynamicRewardEntry(c)
	if !ok {
		t.Fatal("3/40 显示请求未处理")
	}
	detail, _, err = proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	visibility = detail.Field(1)
	if got := visibility.U32(); got != 0 {
		t.Fatalf("3/40 显示后 f1=%d，期望 0", got)
	}
}

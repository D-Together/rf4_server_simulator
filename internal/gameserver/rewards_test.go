package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/proto"
)

// TestPlayerRewards2015ReturnsOwnedMedal 验证 3/16 奖赏列表：
// 返回 proto.Args("185", enikeammfen...)，包含玩家 house 里的奖牌物品，
// 且 f0(名次)/f1(可见性) 正确。
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
		medalGUID, uid, "ft_big_bear_cup_medal_2", 6808, "enikeammfen", 2015, "house", 0, 1, 1.0, 1.0, 1700000000)
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
}

func TestPlaceOfItem(t *testing.T) {
	cases := map[string]uint32{
		"ft_big_bear_cup_medal_1": 1,
		"ft_big_bear_cup_medal_2": 2,
		"ft_big_bear_cup_medal_3": 3,
		"weekly_medal_1":          1,
		"weekly_medal_3":          3,
		"ft_5th_anniversary_2":    2,
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

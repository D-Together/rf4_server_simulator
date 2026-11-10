package gameserver

import (
	"fmt"
	"strings"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// waterwayGiftRow 在 801.f4 的 811 数组里找来源码等于 want 的那一行。
func waterwayGiftRow(t *testing.T, v *proto.Value, want uint64) *proto.Value {
	t.Helper()
	if v == nil || v.Kind != proto.KindArgs {
		t.Fatalf("801.f4 应为 args(173) 礼物行数组，实得 kind=%s", v.Kind)
	}
	for _, line := range v.Items {
		if line == nil || line.Kind != proto.KindObj || line.TypeID != 811 {
			continue
		}
		if num := line.Field(0).Num; num != nil && *num == want {
			return line
		}
	}
	return nil
}

// insertTrophyCatch 插一条「已上星」的历史记录（入护口径，与真实 catch_log 同形）。
func insertTrophyCatch(t *testing.T, s *Server, uid int64, water, fishKey, nameZh string, seq int, grade int64) {
	t.Helper()
	if _, err := s.DB.SQL.Exec(`INSERT INTO catch_log
		(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
		VALUES(1,'loc',0,?,?,1000,0.4,'keep','worm',?,0,0,?,?,?)`,
		fishKey, nameZh, grade, water, uid, fmt.Sprintf("%032d", seq)); err != nil {
		t.Fatalf("插入 catch_log 失败: %v", err)
	}
}

// 集齐该水域「渔夫稀有鱼」面板的最后一条鱼时，结算界面必须当场带礼物：
// 801.f4 里除了升级礼物，还要有一条来源码 = 集齐稀有鱼的 811 行，f2 装着该水域奖杯；
// 奖杯同一时刻进背包（不等下次登录巡检），重放结算不重复发。
func TestCatchCompletingWhitelistCarriesTrophyGift(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}

	const water = "level_001_lake"
	cells := trophyFishWhitelist[water]
	if len(cells) < 2 {
		t.Fatalf("测试依赖 %s 的面板清单，实得 %d 格", water, len(cells))
	}
	// 先补齐除最后一种以外的所有格子。
	for i, c := range cells[:len(cells)-1] {
		insertTrophyCatch(t, s, uid, water, c.key, c.zh, i, recordTrophyGrade)
	}
	last := cells[len(cells)-1]
	fish := &RolledFish{FishGUID: strings.Repeat("d", 32), Species: last.key, Grade: recordTrophyGrade,
		WeightG: 2000, LengthM: 0.5, LocLevel: water}
	st := s.stateFor(uid)
	st.mu.Lock()
	st.Pending = fish
	st.Hooked = true
	st.mu.Unlock()

	body, handled := s.fishingEndSettle(&Ctx{Sess: sess, CallID: 1, Main: 14, Sub: 4,
		ExpectResponse: true, Log: logging.Discard()})
	if !handled || len(body) == 0 {
		t.Fatal("结算未正常返回")
	}
	dec, _, err := proto.DecodeValue(body, 0, sess.Table())
	if err != nil || dec.Kind != proto.KindObj || dec.TypeID != 801 {
		t.Fatalf("801 响应不可解码或类型不符: %v", err)
	}
	line := waterwayGiftRow(t, dec.Fields[4].Val, uint64(s.waterwayTrophyGiftSource()))
	if line == nil {
		t.Fatalf("集齐最后一条鱼时 801.f4 应带来源码 %d 的礼物行（捕获了该水域的所有稀有鱼）",
			s.waterwayTrophyGiftSource())
	}
	items := line.Field(2).Val
	if items == nil || items.Kind != proto.KindArgs || len(items.Items) != 1 {
		t.Fatalf("礼物行 f2 应装 1 件奖杯物品，实得 %+v", items)
	}

	// 奖杯必须真的在背包里：礼物卡片里的物品和物品栏是同一件。
	itemID := waterwayTrophyItemPrefix + strings.TrimPrefix(water, "level_")
	var inBag int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=? AND container=?`,
		uid, itemID, waterwayTrophyContainer).Scan(&inBag); err != nil {
		t.Fatal(err)
	}
	if inBag != 1 {
		t.Fatalf("奖杯 %s 应已进背包 1 枚，实得 %d", itemID, inBag)
	}

	// 同一尾鱼重放结算：礼物行不重复（801.f4 回 null），背包里也只有一枚。
	body2, handled2 := s.fishingEndSettle(&Ctx{Sess: sess, CallID: 2, Main: 14, Sub: 4,
		ExpectResponse: true, Log: logging.Discard()})
	if !handled2 || len(body2) == 0 {
		t.Fatal("重放结算未正常返回")
	}
	dec2, _, err := proto.DecodeValue(body2, 0, sess.Table())
	if err != nil || dec2.Kind != proto.KindObj || dec2.TypeID != 801 {
		t.Fatalf("重放 801 响应不可解码: %v", err)
	}
	if f4 := dec2.Fields[4].Val; f4 == nil || f4.Kind != proto.KindNull {
		t.Fatalf("重放结算 801.f4 应为 null, 实得 kind=%s", f4.Kind)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, itemID).Scan(&inBag); err != nil {
		t.Fatal(err)
	}
	if inBag != 1 {
		t.Fatalf("重放后奖杯不应重复发放，实得 %d 枚", inBag)
	}
}

// 只补齐了前面的格子、这尾鱼不是最后一种时，不该发奖杯、也不该带礼物行。
func TestCatchNotCompletingWhitelistHasNoTrophyGift(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	// 关掉升级礼物，免得升级把 801.f4 也填上（本用例只看集齐礼物）。
	if err := db.TunableSet(t.Context(), "level_gifts_enabled", "0", "test"); err != nil {
		t.Fatal(err)
	}

	const water = "level_001_lake"
	cells := trophyFishWhitelist[water]
	if len(cells) < 3 {
		t.Fatalf("测试依赖 %s 的面板清单，实得 %d 格", water, len(cells))
	}
	// 差两种：这尾鱼补上后仍然不齐。
	for i, c := range cells[:len(cells)-2] {
		insertTrophyCatch(t, s, uid, water, c.key, c.zh, i, recordTrophyGrade)
	}
	fish := &RolledFish{FishGUID: strings.Repeat("e", 32), Species: cells[len(cells)-2].key,
		Grade: recordTrophyGrade, WeightG: 2000, LengthM: 0.5, LocLevel: water}
	st := s.stateFor(uid)
	st.mu.Lock()
	st.Pending = fish
	st.Hooked = true
	st.mu.Unlock()

	body, handled := s.fishingEndSettle(&Ctx{Sess: sess, CallID: 1, Main: 14, Sub: 4,
		ExpectResponse: true, Log: logging.Discard()})
	if !handled || len(body) == 0 {
		t.Fatal("结算未正常返回")
	}
	dec, _, err := proto.DecodeValue(body, 0, sess.Table())
	if err != nil || dec.Kind != proto.KindObj || dec.TypeID != 801 {
		t.Fatalf("801 响应不可解码: %v", err)
	}
	// 没集齐：不该有「集齐该水域」那张卡。注意 f4 非空不代表出错——上星鱼种数
	// 跨档时还会挂一条来源码 5 的「捕获了数条稀有鱼」，那与水域进度无关。
	want := uint64(s.waterwayTrophyGiftSource())
	if f4 := dec.Fields[4].Val; f4 != nil && f4.Kind == proto.KindArgs {
		for _, line := range f4.Items {
			if line != nil && line.Kind == proto.KindObj && line.TypeID == 811 &&
				line.Field(0).Num != nil && *line.Field(0).Num == want {
				t.Fatalf("没集齐时不该有来源码 %d 的礼物行", want)
			}
		}
	}
	itemID := waterwayTrophyItemPrefix + strings.TrimPrefix(water, "level_")
	var n int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, itemID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("没集齐不该发奖杯，实得 %d 枚", n)
	}
}

// 811.f0 的来源码是实机扫频标定的（1=新级别、4=集齐水域、5=数条稀有鱼、6=卡片册），
// 不是按本地化文案顺序推出来的。锁住 4 这个值：改错会让集齐提示变成别的礼物文案
// （曾经推成 5，实机弹的是「钓到了数条稀有鱼」）。
func TestWaterwayTrophyGiftSourceIsVerifiedTableCollected(t *testing.T) {
	t.Parallel()
	s, _ := newDetachTestServer(t)
	if got := s.waterwayTrophyGiftSource(); got != 4 {
		t.Fatalf("集齐水域的 811.f0 应为实机验证过的 4，实得 %d", got)
	}
}

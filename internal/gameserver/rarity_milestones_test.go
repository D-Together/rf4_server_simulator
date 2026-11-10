package gameserver

import (
	"fmt"
	"strings"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// insertTrophyCatch 直接往 catch_log 插一条渔获：奖座发放只看这张表的鱼种与档位，
// 不走结算就不需要造整套钓鱼上下文。
func insertTrophyCatch(t *testing.T, s *Server, uid int64, water, fishKey, nameZh string, seq int, grade int64) {
	t.Helper()
	if _, err := s.DB.SQL.Exec(`INSERT INTO catch_log
		(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
		VALUES(1,'loc',0,?,?,1000,0.4,'keep','worm',?,0,0,?,?,?)`,
		fishKey, nameZh, grade, water, uid, fmt.Sprintf("%032d", seq)); err != nil {
		t.Fatalf("插入 catch_log 失败: %v", err)
	}
}

// giftRowOf 在 801.f4 的 811 数组里找来源码等于 want 的行；f4 为 null（没有礼物）
// 时返回 nil 而不是失败——「不该有礼物」的断言也要用它。
func giftRowOf(v *proto.Value, want uint64) *proto.Value {
	if v == nil || v.Kind != proto.KindArgs {
		return nil
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

// countOwnedItem 数玩家背包里某个物品有几件。
func countOwnedItem(t *testing.T, s *Server, uid int64, itemID string) int64 {
	t.Helper()
	var n int64
	if err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`,
		uid, itemID).Scan(&n); err != nil {
		t.Fatalf("统计 %s 失败: %v", itemID, err)
	}
	return n
}

// settleWithFish 让 rig 里的这尾鱼走一次 14/4 结算，返回解码后的 801。
func settleWithFish(t *testing.T, s *Server, sess *Session, fish *RolledFish, call int64) *proto.Value {
	t.Helper()
	st := s.stateFor(fishOwner(sess))
	st.mu.Lock()
	st.Pending = fish
	st.Hooked = true
	st.mu.Unlock()
	body, handled := s.fishingEndSettle(&Ctx{Sess: sess, CallID: uint32(call), Main: 14, Sub: 4,
		ExpectResponse: true, Log: logging.Discard()})
	if !handled || len(body) == 0 {
		t.Fatal("结算未正常返回")
	}
	dec, _, err := proto.DecodeValue(body, 0, sess.Table())
	if err != nil || dec.Kind != proto.KindObj || dec.TypeID != 801 {
		t.Fatalf("801 响应不可解码或类型不符: %v", err)
	}
	return dec
}

func fishOwner(sess *Session) int64 { return sess.Userid }

// keepWithFish 让 pending 里的这尾鱼走一次 14/5 入护，返回 800。
// 奖座在入护后才算账，所以断言奖座必须走完整条 14/4 结算 -> 14/5 入护。
func keepWithFish(t *testing.T, s *Server, sess *Session, fish *RolledFish, call int64) *proto.Value {
	t.Helper()
	tree := proto.Args524(proto.Guid(strings.Repeat("0", 32)), proto.Guid(fish.FishGUID))
	body, handled := s.fishingKeep(&Ctx{Sess: sess, CallID: uint32(call), Main: 14, Sub: 5,
		Tree: tree, DecodeOK: true, ExpectResponse: true, Log: logging.Discard()})
	if !handled || len(body) == 0 {
		t.Fatal("14/5 入护未正常返回")
	}
	dec, _, err := proto.DecodeValue(body, 0, sess.Table())
	if err != nil || dec.Kind != proto.KindObj || dec.TypeID != 800 {
		t.Fatalf("800 响应不可解码或类型不符: %v", err)
	}
	return dec
}

// releaseWithFish 让 pending 里的这尾鱼走一次 14/6 放生，返回 807。
func releaseWithFish(t *testing.T, s *Server, sess *Session, fish *RolledFish, call int64) *proto.Value {
	t.Helper()
	tree := proto.Args524(proto.Guid(strings.Repeat("0", 32)), proto.Guid(fish.FishGUID))
	body, handled := s.fishingRelease(&Ctx{Sess: sess, CallID: uint32(call), Main: 14, Sub: 6,
		Tree: tree, DecodeOK: true, ExpectResponse: true, Log: logging.Discard()})
	if !handled || len(body) == 0 {
		t.Fatal("14/6 放生未正常返回")
	}
	dec, _, err := proto.DecodeValue(body, 0, sess.Table())
	if err != nil || dec.Kind != proto.KindObj || dec.TypeID != 807 {
		t.Fatalf("807 响应不可解码或类型不符: %v", err)
	}
	return dec
}

// 第 3 种稀有鱼上星并**入护**后，背包里应该出现 statuette_fisherman_3（官方名「3条稀有鱼」）。
// 结算阶段（14/4）刻意不发：图鉴口径是 catch_log，而 catch_log 要到 14/5 才写。
func TestRarityMilestoneCrossingGrantsStatuetteOnKeep(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	if err := db.TunableSet(t.Context(), "level_gifts_enabled", "0", "test"); err != nil {
		t.Fatal(err)
	}
	// 已经有两种上过星。
	insertTrophyCatch(t, s, uid, "level_001_lake", "perch", "河鲈", 1, recordTrophyGrade)
	insertTrophyCatch(t, s, uid, "level_001_lake", "pike", "白斑狗鱼", 2, recordTrophyGrade)

	fish := &RolledFish{FishGUID: strings.Repeat("a", 32), Species: "zander", Grade: recordTrophyGrade,
		WeightG: 2000, LengthM: 0.6, LocLevel: "level_001_lake"}
	settleWithFish(t, s, sess, fish, 1)
	// 结算只出 801，不入护就不该有奖座。
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 0 {
		t.Fatalf("结算阶段不该发奖座（图鉴还没记这尾鱼），实得 %d 枚", n)
	}

	keepWithFish(t, s, sess, fish, 2)
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 1 {
		t.Fatalf("入护后 statuette_fisherman_3 应为 1 枚，实得 %d", n)
	}
	var logged int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM catch_log WHERE uid=? AND fish_key='zander'`,
		uid).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Fatalf("入护后 catch_log 应有 1 行 zander，实得 %d", logged)
	}

	// 重复入护（重放）：不能重复发。
	keepWithFish(t, s, sess, fish, 3)
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 1 {
		t.Fatalf("重放入护不应重复发奖座，实得 %d 枚", n)
	}
}

// 放生的鱼不进 catch_log，也就不该发奖座——否则会出现「背包有奖座、图鉴不计数」。
func TestRarityMilestoneReleaseDoesNotGrant(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	if err := db.TunableSet(t.Context(), "level_gifts_enabled", "0", "test"); err != nil {
		t.Fatal(err)
	}
	insertTrophyCatch(t, s, uid, "level_001_lake", "perch", "河鲈", 1, recordTrophyGrade)
	insertTrophyCatch(t, s, uid, "level_001_lake", "pike", "白斑狗鱼", 2, recordTrophyGrade)

	fish := &RolledFish{FishGUID: strings.Repeat("d", 32), Species: "zander", Grade: recordTrophyGrade,
		WeightG: 2000, LengthM: 0.6, LocLevel: "level_001_lake"}
	settleWithFish(t, s, sess, fish, 1)
	releaseWithFish(t, s, sess, fish, 2)

	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 0 {
		t.Fatalf("放生的鱼不该计入鱼种数、也不该发奖座，实得 %d 枚", n)
	}
	var logged int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM catch_log WHERE uid=? AND fish_key='zander'`,
		uid).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 0 {
		t.Fatalf("放生不该写 catch_log，实得 %d 行", logged)
	}
	// 放生后再入护同一条鱼也不行：catch_log 与奖座都必须保持不计数。
	keepWithFish(t, s, sess, fish, 3)
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 0 {
		t.Fatalf("已放生的鱼再入护不该发奖座，实得 %d 枚", n)
	}
}

// 同一种鱼重复上星只算一个：鱼种数还是 2，不该跨过 3 那一档。
func TestRarityMilestoneSameSpeciesDoesNotAdvance(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	if err := db.TunableSet(t.Context(), "level_gifts_enabled", "0", "test"); err != nil {
		t.Fatal(err)
	}
	insertTrophyCatch(t, s, uid, "level_001_lake", "perch", "河鲈", 1, recordTrophyGrade)
	insertTrophyCatch(t, s, uid, "level_001_lake", "pike", "白斑狗鱼", 2, recordTrophyGrade)

	// 河鲈第二次上星（更大的个体），入护。
	fish := &RolledFish{FishGUID: strings.Repeat("b", 32), Species: "perch", Grade: recordTrophyGrade,
		WeightG: 3000, LengthM: 0.5, LocLevel: "level_001_lake"}
	settleWithFish(t, s, sess, fish, 1)
	keepWithFish(t, s, sess, fish, 2)

	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 0 {
		t.Fatalf("同鱼种重复上星不该推进鱼种数，不该发 statuette_fisherman_3，实得 %d 枚", n)
	}
}

// 蓝冠（超级稀有）另有自己的一套档位，首冠入护后发 statuette_supertrophy_1；
// 而稀有鱼档位最矮是 3，单冠时还差两种，不能顺手也发一枚。
func TestSuperRareMilestoneFirstCrown(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	if err := db.TunableSet(t.Context(), "level_gifts_enabled", "0", "test"); err != nil {
		t.Fatal(err)
	}
	fish := &RolledFish{FishGUID: strings.Repeat("c", 32), Species: "sturgeon", Grade: superTrophyGrade,
		WeightG: 9000, LengthM: 1.4, LocLevel: "level_001_lake"}
	settleWithFish(t, s, sess, fish, 1)
	keepWithFish(t, s, sess, fish, 2)

	if n := countOwnedItem(t, s, uid, "statuette_supertrophy_1"); n != 1 {
		t.Fatalf("首冠应发 statuette_supertrophy_1，实得 %d 枚", n)
	}
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 0 {
		t.Fatalf("只有 1 种鱼达标时不该发 statuette_fisherman_3，实得 %d 枚", n)
	}
}

// 登录巡检按当前鱼种数一次性补齐所有跨过的档位（可跨多档），重复调用不再发。
func TestEnsureRarityMilestonesBackfillsCrossedTiers(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	uid := int64(777001)
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		insertTrophyCatch(t, s, uid, "level_001_lake", "fish_"+strings.Repeat("x", i+1), "鱼", i, recordTrophyGrade)
	}
	got := s.ensureRarityMilestones(t.Context(), uid)
	if len(got) != 3 {
		t.Fatalf("11 种稀有待补 3/5/10 三档，实得 %d 枚", len(got))
	}
	for _, id := range []string{"statuette_fisherman_3", "statuette_fisherman_5", "statuette_fisherman_10"} {
		if n := countOwnedItem(t, s, uid, id); n != 1 {
			t.Fatalf("%s 应已补发 1 枚，实得 %d", id, n)
		}
	}
	if again := s.ensureRarityMilestones(t.Context(), uid); len(again) != 0 {
		t.Fatalf("巡检应幂等，第二次实得 %d 枚", len(again))
	}
}

// 档位表必须和物品定义对齐：数量少一档或多一档，玩家就会拿到不存在的奖座，
// 或者永远卡在某个档位不发。
func TestRarityMilestoneLaddersMatchItemDefs(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	wantRare := []int64{3, 5, 10, 15, 30, 50, 75, 100}
	wantSuper := []int64{1, 3, 5, 7, 10, 15, 20, 30, 50}
	if len(rareSpeciesMilestones) != len(wantRare) {
		t.Fatalf("稀有鱼档位应为 %v，实得 %d 档", wantRare, len(rareSpeciesMilestones))
	}
	for i, m := range rareSpeciesMilestones {
		if m.Species != wantRare[i] {
			t.Fatalf("稀有鱼第 %d 档应为 %d，实得 %d", i, wantRare[i], m.Species)
		}
		if n := countItemDef(t, s, m.ItemID); n == 0 {
			t.Fatalf("档位物品 %s 在 item_def 里不存在", m.ItemID)
		}
	}
	if len(superRareSpeciesMilestones) != len(wantSuper) {
		t.Fatalf("超级稀有鱼档位应为 %v，实得 %d 档", wantSuper, len(superRareSpeciesMilestones))
	}
	for i, m := range superRareSpeciesMilestones {
		if m.Species != wantSuper[i] {
			t.Fatalf("超级稀有鱼第 %d 档应为 %d，实得 %d", i, wantSuper[i], m.Species)
		}
		if n := countItemDef(t, s, m.ItemID); n == 0 {
			t.Fatalf("档位物品 %s 在 item_def 里不存在", m.ItemID)
		}
	}
}

func countItemDef(t *testing.T, s *Server, itemID string) int64 {
	t.Helper()
	var n int64
	if err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM item_def WHERE item_id=?`, itemID).Scan(&n); err != nil {
		t.Fatalf("查 item_def 失败: %v", err)
	}
	return n
}

package gameserver

import (
	"fmt"
	"strings"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// insertTrophyCatch 直接往 catch_log 插一条渔获，并同步补一行 catch_seen。
// 奖座只看 catch_seen（钓到过），图鉴/入护类断言看 catch_log；两个都插才等价于
// 「这尾鱼当时既结算过也入了护」，正好对应历史存档的形状。
func insertTrophyCatch(t *testing.T, s *Server, uid int64, water, fishKey, nameZh string, seq int, grade int64) {
	t.Helper()
	if _, err := s.DB.SQL.Exec(`INSERT INTO catch_log
		(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
		VALUES(1,'loc',0,?,?,1000,0.4,'keep','worm',?,0,0,?,?,?)`,
		fishKey, nameZh, grade, water, uid, fmt.Sprintf("%032d", seq)); err != nil {
		t.Fatalf("插入 catch_log 失败: %v", err)
	}
	if grade >= recordTrophyGrade {
		if _, err := s.DB.SQL.Exec(`INSERT INTO catch_seen(uid,loc_level,fish_key,best_grade,first_ts) VALUES(?,?,?,?,1)
			ON CONFLICT(uid,loc_level,fish_key) DO UPDATE SET best_grade=MAX(catch_seen.best_grade, excluded.best_grade)`,
			uid, water, fishKey, grade); err != nil {
			t.Fatalf("插入 catch_seen 失败: %v", err)
		}
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

// 第 3 种稀有鱼在**结算（14/4）**当场就该发 statuette_fisherman_3，
// 并在 801.f4 挂来源码 3 的礼物行（条数 3）——官方结算界面在玩家选择入护/放生
// 之前就把奖座卡画出来了，这是 2026-11-11 修回的回归点。
func TestRarityMilestoneGrantsOnSettleWithGiftCard(t *testing.T) {
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
	dec := settleWithFish(t, s, sess, fish, 1)
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 1 {
		t.Fatalf("结算跨档应立即发 statuette_fisherman_3，实得 %d 枚", n)
	}
	line := giftRowOf(dec.Field(4).Val, giftSourceTrophyCountReached)
	if line == nil {
		t.Fatal("801.f4 应带来源码 3 的「捕获了数条稀有鱼」礼物行")
	}
	if p := line.Field(1).Num; p == nil || *p != 3 {
		t.Fatalf("礼物行的条数参数应为 3（鱼种数），实得 %v", p)
	}
	if items := line.Field(2).Val; items == nil || items.Kind != proto.KindArgs || len(items.Items) != 1 {
		t.Fatalf("礼物行应含 1 件奖座，实得 %#v", items)
	}
	// 801.f8 奖励列表也要有它，结算界面的物品列表才显示。
	if grants := dec.Field(8).Val; grants == nil || grants.Kind != proto.KindArgs || len(grants.Items) != 1 {
		t.Fatalf("801.f8 应含 1 件奖励，实得 %#v", grants)
	}

	// 同一条鱼重放（客户端重发 14/4）：不重复发、也不重复挂礼物。
	settleWithFish(t, s, sess, fish, 2)
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 1 {
		t.Fatalf("重放结算不应重复发奖座，实得 %d 枚", n)
	}
}

// 放生不影响奖座，也不影响图鉴：官方是结算时弹卡、玩家随后才决定放生，
// 收集口径就是「钓到过」。catch_log 在结算事务里已写下这一尾（source='catch'），
// 放生不回收——3/17 面板与 307.f16 星号因此和奖座同步。
func TestRarityMilestoneCountsReleasedCatch(t *testing.T) {
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
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 1 {
		t.Fatalf("结算跨档应发奖座，实得 %d 枚", n)
	}
	releaseWithFish(t, s, sess, fish, 2)
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 1 {
		t.Fatalf("放生不该回收已发的奖座，实得 %d 枚", n)
	}
	var logged int
	var source string
	if err := db.SQL.QueryRow(`SELECT COUNT(*), COALESCE(MAX(source),'') FROM catch_log
		WHERE uid=? AND fish_key='zander'`, uid).Scan(&logged, &source); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Fatalf("放生的鱼也应留下一条钓获记录（图鉴点亮），实得 %d 行", logged)
	}
	if source != "catch" {
		t.Fatalf("放生后 catch_log.source 应为 catch，实得 %q", source)
	}
	// 307.f16（奖杯鱼种数）与 24/19.f5 共用这个口径：3 种都该点亮。
	if n, err := db.RareSpeciesCount(t.Context(), uid, recordTrophyGrade); err != nil {
		t.Fatal(err)
	} else if n != 3 {
		t.Fatalf("放生后奖杯鱼种数应为 3，实得 %d", n)
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

	// 河鲈第二次上星（更大的个体），结算。
	fish := &RolledFish{FishGUID: strings.Repeat("b", 32), Species: "perch", Grade: recordTrophyGrade,
		WeightG: 3000, LengthM: 0.5, LocLevel: "level_001_lake"}
	settleWithFish(t, s, sess, fish, 1)

	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 0 {
		t.Fatalf("同鱼种重复上星不该推进鱼种数，不该发 statuette_fisherman_3，实得 %d 枚", n)
	}
}

// 蓝冠（超级稀有）另有自己的一套档位：首冠结算即发 statuette_supertrophy_1，
// 礼物行的条数参数是 1；稀有鱼档位最矮是 3，单冠时还差两种，不能顺手也发一枚。
func TestSuperRareMilestoneFirstCrownOnSettle(t *testing.T) {
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
	dec := settleWithFish(t, s, sess, fish, 1)

	if n := countOwnedItem(t, s, uid, "statuette_supertrophy_1"); n != 1 {
		t.Fatalf("首冠结算应发 statuette_supertrophy_1，实得 %d 枚", n)
	}
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 0 {
		t.Fatalf("只有 1 种鱼达标时不该发 statuette_fisherman_3，实得 %d 枚", n)
	}
	line := giftRowOf(dec.Field(4).Val, giftSourceTrophyCountReached)
	if line == nil {
		t.Fatal("蓝冠结算也应带来源码 3 的礼物行")
	}
	if p := line.Field(1).Num; p == nil || *p != 1 {
		t.Fatalf("超级稀有鱼礼物行的条数参数应为 1，实得 %v", p)
	}
}

// 同一次结算两族各跨一档时，801.f4 要出**两行**同来源码的礼物行，
// 各自带自己的条数参数（合并成一行会让第二族显示成另一族的进度）。
func TestRarityMilestoneTwoFamiliesTwoGiftRows(t *testing.T) {
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

	// 本尾蓝冠：稀有鱼族第 3 种 + 超级稀有鱼族第 1 种，同时跨档。
	fish := &RolledFish{FishGUID: strings.Repeat("e", 32), Species: "sturgeon", Grade: superTrophyGrade,
		WeightG: 9000, LengthM: 1.4, LocLevel: "level_001_lake"}
	dec := settleWithFish(t, s, sess, fish, 1)

	rows, ok := dec.Field(4).Val, false
	if rows != nil && rows.Kind == proto.KindArgs {
		var params []uint64
		for _, line := range rows.Items {
			if line != nil && line.Kind == proto.KindObj && line.TypeID == 811 {
				if num := line.Field(0).Num; num != nil && *num == giftSourceTrophyCountReached {
					ok = true
					if p := line.Field(1).Num; p != nil {
						params = append(params, *p)
					}
				}
			}
		}
		if !ok || len(params) != 2 {
			t.Fatalf("应有 2 行来源码 %d 的礼物行，实得 %v", giftSourceTrophyCountReached, params)
		}
		if !(params[0] == 3 && params[1] == 1) {
			t.Fatalf("两行条数参数应分别为 3 与 1，实得 %v", params)
		}
	} else {
		t.Fatalf("801.f4 应为礼物行数组，实得 %#v", rows)
	}
	if n := countOwnedItem(t, s, uid, "statuette_fisherman_3"); n != 1 {
		t.Fatalf("稀有鱼族第 3 种应发 1 枚，实得 %d", n)
	}
	if n := countOwnedItem(t, s, uid, "statuette_supertrophy_1"); n != 1 {
		t.Fatalf("超级稀有鱼族首冠应发 1 枚，实得 %d", n)
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

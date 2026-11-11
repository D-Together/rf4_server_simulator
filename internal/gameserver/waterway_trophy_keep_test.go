package gameserver

import (
	"fmt"
	"strings"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// 水域奖杯的实际判定时机是 14/5 入护之后，所以回归必须走完整链路：
// 14/4 结算 → 14/6 放生（不发）/ 14/5 入护（发）。

// wlSettle 让这尾鱼走一次 14/4 结算，返回解码后的 801。
func wlSettle(t *testing.T, s *Server, sess *Session, fish *RolledFish, call int64) *proto.Value {
	t.Helper()
	st := s.stateFor(sess.Userid)
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

// wlKeep 让 pending 里的这尾鱼走一次 14/5 入护，返回 800。
func wlKeep(t *testing.T, s *Server, sess *Session, fish *RolledFish, call int64) *proto.Value {
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

// wlRelease 让 pending 里的这尾鱼走一次 14/6 放生，返回 807。
func wlRelease(t *testing.T, s *Server, sess *Session, fish *RolledFish, call int64) *proto.Value {
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

// 水域奖杯与稀有鱼奖座同一时机：**14/4 结算当场判定并发放**。
//
// 官方结算界面（801）在玩家选择入护或放生之前就画出「捕获了该水域的所有稀有鱼」
// 这张卡，说明官方的水域进度在「钓到」那一刻就成立。判定数据源是 catch_seen
// （钓到即记、带水域），catch_log 也在同一个结算事务里落库（source='catch'），
// 所以图鉴、307.f16 与这块面板和奖杯永远同步：放生也点亮。
func TestWaterwayTrophyIssuedOnSettleBeforeKeep(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	// 面板口径用白名单（level_001_lake 有 8 格）：先补齐前 7 格。
	const water = "level_001_lake"
	cells := trophyFishWhitelistSet(water)
	if len(cells) < 2 {
		t.Fatalf("测试依赖 %s 的白名单", water)
	}
	keys := make([]string, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	// 最后捕到的那一种留出来，用真实结算/放生/入护链路补上。
	last := keys[0]
	insertWL := func(key string, seq int) {
		t.Helper()
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,?,?,1000,0.4,'keep','worm',3,0,0,?,?,?)`,
			key, key, water, uid, fmt.Sprintf("%032d", seq)); err != nil {
			t.Fatal(err)
		}
		seedTrophySeen(t, db, uid, water, key, 3)
	}
	for i, k := range keys[1:] {
		insertWL(k, i+1)
	}
	trophyItemID := "trophy_level_001_lake"
	owned := func() int64 {
		t.Helper()
		var n int64
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`,
			uid, trophyItemID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 1) 结算当场发：这一尾补上最后一格，801 里要带来源码 6 的礼物行。
	fish := &RolledFish{FishGUID: strings.Repeat("b", 32), Species: last, Grade: recordTrophyGrade,
		WeightG: 1500, LengthM: 0.45, LocLevel: water}
	dec := wlSettle(t, s, sess, fish, 1)
	if n := owned(); n != 1 {
		t.Fatalf("结算当场应发 1 枚水域奖杯，实得 %d 枚", n)
	}
	if line := giftRowOf(dec.Field(4).Val, giftSourceTrophyTableCollected); line == nil {
		t.Fatalf("801.f4 没有来源码 %d 的礼物行：%+v", giftSourceTrophyTableCollected, dec.Field(4).Val)
	}

	// 2) 放生不回退：奖杯是纪念品，官方顺序是先发卡后选择。
	// 图鉴同口径——这一尾在结算时已落 catch_log，放生不回收，面板仍是满格。
	wlRelease(t, s, sess, fish, 2)
	if n := owned(); n != 1 {
		t.Fatalf("放生不该回收奖杯，实得 %d 枚", n)
	}
	var logged int64
	var source string
	if err := db.SQL.QueryRow(`SELECT COUNT(*), COALESCE(MAX(source),'') FROM catch_log
		WHERE uid=? AND fish_key=? AND loc_level=?`, uid, last, water).Scan(&logged, &source); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Fatalf("放生的鱼也该留下一条钓获记录（图鉴点亮），实得 %d 行", logged)
	}
	if source != "catch" {
		t.Fatalf("放生后 catch_log.source 应为 catch，实得 %q", source)
	}
	if p, err := s.waterwayTrophyProgressFor(t.Context(), uid, water); err != nil {
		t.Fatal(err)
	} else if p.Done != p.Total {
		t.Fatalf("放生后水域面板应为满格 %d/%d，实得 %d/%d", p.Total, p.Total, p.Done, p.Total)
	}

	// 3) 同一种鱼改成入护：入护补写 catch_log，但奖杯已在结算时发过，不重复发。
	again := &RolledFish{FishGUID: strings.Repeat("c", 32), Species: last, Grade: recordTrophyGrade,
		WeightG: 1600, LengthM: 0.47, LocLevel: water}
	wlSettle(t, s, sess, again, 3)
	wlKeep(t, s, sess, again, 4)
	if n := owned(); n != 1 {
		t.Fatalf("重复结算/入护不应再发第二枚，实得 %d 枚", n)
	}
	var kv int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM kv WHERE k=?`,
		fmt.Sprintf(waterwayTrophyKeyTemplate, uid, water)).Scan(&kv); err != nil {
		t.Fatal(err)
	}
	if kv != 1 {
		t.Fatalf("奖杯幂等标记应有 1 行，实得 %d", kv)
	}

	// 4) 重复入护（重放）不再发第二枚。
	wlKeep(t, s, sess, again, 5)
	if n := owned(); n != 1 {
		t.Fatalf("重放入护不应再发奖杯，实得 %d 枚", n)
	}
}

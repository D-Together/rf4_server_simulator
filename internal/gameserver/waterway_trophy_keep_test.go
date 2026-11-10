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

// 放生最后一尾时不能先发奖杯：`catch_log` 只在入护时写，放生的鱼不进图鉴，
// 若在结算（14/4）就发放，会出现「背包有奖杯、3/17 面板还差一格」（review P2）。
func TestWaterwayTrophyIssuedOnlyAfterKeep(t *testing.T) {
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

	// 1) 结算阶段不发：结算界面早于入护，此时 catch_log 还没有这尾鱼。
	fish := &RolledFish{FishGUID: strings.Repeat("b", 32), Species: last, Grade: recordTrophyGrade,
		WeightG: 1500, LengthM: 0.45, LocLevel: water}
	wlSettle(t, s, sess, fish, 1)
	if n := owned(); n != 0 {
		t.Fatalf("结算阶段不该发水域奖杯，实得 %d 枚", n)
	}

	// 2) 放生不发：面板仍是 7/8，奖杯一旦发出就再也无法自洽。
	wlRelease(t, s, sess, fish, 2)
	if n := owned(); n != 0 {
		t.Fatalf("放生的鱼不该计入面板、也不该发奖杯，实得 %d 枚", n)
	}
	var logged int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM catch_log WHERE uid=? AND fish_key=? AND loc_level=?`,
		uid, last, water).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 0 {
		t.Fatalf("放生不该写 catch_log，实得 %d 行", logged)
	}
	p, err := s.waterwayTrophyProgressFor(t.Context(), uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if p.Done != p.Total-1 {
		t.Fatalf("放生后面板应仍是 %d/%d，实得 %d/%d", p.Total-1, p.Total, p.Done, p.Total)
	}

	// 3) 同一种鱼改成入护：入护写 catch_log，面板填满，奖杯当场入包。
	again := &RolledFish{FishGUID: strings.Repeat("c", 32), Species: last, Grade: recordTrophyGrade,
		WeightG: 1600, LengthM: 0.47, LocLevel: water}
	wlSettle(t, s, sess, again, 3)
	wlKeep(t, s, sess, again, 4)
	if n := owned(); n != 1 {
		t.Fatalf("入护补上最后一格后应有 1 枚奖杯，实得 %d 枚", n)
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

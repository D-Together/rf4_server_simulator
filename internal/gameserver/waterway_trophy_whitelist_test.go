package gameserver

import (
	"context"
	"fmt"
	"testing"
)

// 面板口径：分母只能是「渔夫稀有鱼」面板收录的那几种，loc_fish 里多出来的鱼种
// （这张图能钓到、但官方没编进图鉴的）即使上了星也不参与判定。
func TestWaterwayTrophyUsesPanelWhitelistAsDenominator(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	const water = "level_001_lake"
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_alias(alias,loc,note)
		VALUES(?,?,'test')`, water, "测试湖"); err != nil {
		t.Fatal(err)
	}
	cells := trophyFishWhitelist[water]
	if len(cells) < 2 {
		t.Fatalf("测试依赖 level_001_lake 的白名单，实得 %d 条", len(cells))
	}
	wl := make([]string, 0, len(cells))
	for _, c := range cells {
		wl = append(wl, c.key)
	}
	// loc_fish 故意多塞两种「能钓到但不在面板上」的鱼：它们上星也不该影响判定。
	all := append([]string{"w_catfish", "n.pike"}, wl...)
	for _, key := range all {
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches)
			VALUES(?,?,?,5)`, "测试湖", key, key); err != nil {
			t.Fatal(err)
		}
	}
	logCatch := func(key string, grade int64) {
		t.Helper()
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,?,?,1000,0.4,'rod','worm',?,0,0,?,?,?)`,
			key, key, grade, water, uid, "g-"+key); err != nil {
			t.Fatal(err)
		}
	}
	// 分母外的两种先上星——若实现退化成 loc_fish 口径，这里会误判成快满。
	logCatch("w_catfish", 4)
	logCatch("n.pike", 4)
	// 白名单差最后一种。
	for _, key := range wl[:len(wl)-1] {
		logCatch(key, 3)
	}
	p, err := s.waterwayTrophyProgressFor(ctx, uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != int64(len(wl)) || p.Done != int64(len(wl)-1) {
		t.Fatalf("进度应为 %d/%d，实得 %d/%d", len(wl)-1, len(wl), p.Done, p.Total)
	}
	if it, err := s.grantWaterwayTrophy(ctx, uid, water); err != nil {
		t.Fatal(err)
	} else if it != nil {
		t.Fatalf("面板没填满却发了奖杯: %+v", it)
	}
	// 补齐最后一种 → 发一枚。
	logCatch(wl[len(wl)-1], 4)
	it, err := s.grantWaterwayTrophy(ctx, uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if it == nil {
		t.Fatal("面板集齐后没有发放奖杯")
	}
	if it.ItemID != "trophy_level_001_lake" {
		t.Fatalf("奖杯 item_id=%q", it.ItemID)
	}
}

// 3/17 面板下发时也要按白名单过滤：清单外的鱼种不该出现在面板里，否则面板看着
// 有卡、奖杯判定却不认它，玩家会以为「明明钓到了却没发」。
func TestBuildFishTrophiesFiltersByPanelWhitelist(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	const water = "level_002_kirzah_river"
	cells := trophyFishWhitelist[water]
	if len(cells) == 0 {
		t.Fatal("测试依赖 level_002_kirzah_river 的白名单")
	}
	wl := make([]string, 0, len(cells))
	for _, c := range cells {
		wl = append(wl, c.key)
	}
	keys := append([]string{"asp", "bream"}, wl...)
	for i, key := range keys {
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,?,?,1000,0.4,'rod','worm',3,0,0,?,?,?)`,
			key, key, water, uid, fmt.Sprintf("%032d", i)); err != nil {
			t.Fatal(err)
		}
	}
	v, err := s.buildFishTrophies323(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, g := range v.Items {
		if *g.Field(0).Str != water {
			continue
		}
		for _, c := range g.Field(1).Val.Items {
			got[*c.Field(0).Str] = true
		}
	}
	for _, key := range wl {
		if !got[key] {
			t.Fatalf("白名单鱼种 %s 没出现在 3/17 面板里：%v", key, got)
		}
	}
	for _, key := range []string{"asp", "bream"} {
		if got[key] {
			t.Fatalf("清单外鱼种 %s 被下发了，面板与奖杯判定口径不一致", key)
		}
	}
}

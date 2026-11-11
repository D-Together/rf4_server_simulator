package gameserver

import (
	"context"
	"fmt"
	"sort"
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
		seedTrophySeen(t, db, uid, water, key, grade)
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
		seedTrophySeen(t, db, uid, water, key, 3)
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

// 白名单是反推数据，最容易出的错是「拼错 key / 同一格写两遍 / 把家园写进去」——这类
// 错误不会让编译失败，只会让某个水域永远集不齐。这里把结构约束钉住。
func TestTrophyFishWhitelistStructure(t *testing.T) {
	t.Parallel()
	for water, cells := range trophyFishWhitelist {
		if water == waterwayTrophyHome {
			t.Fatalf("家园不该出现在面板收录清单里: %s", water)
		}
		if len(cells) == 0 {
			t.Fatalf("%s 的清单是空的，应直接不建条目", water)
		}
		seen := make(map[string]bool, len(cells))
		for _, c := range cells {
			if c.key == "" || c.zh == "" {
				t.Fatalf("%s 有空白格子: %+v", water, c)
			}
			if seen[c.key] {
				t.Fatalf("%s 的 key %q 重复出现", water, c.key)
			}
			seen[c.key] = true
		}
	}
	// unspawnable 里的 key 必须真的在该水域清单里，否则是拼错了 key、白白少一个分母。
	for water, keys := range trophyFishWhitelistUnspawnable {
		if len(trophyFishWhitelist[water]) == 0 {
			t.Fatalf("%s 没有清单却记了不可生成鱼种", water)
		}
		present := map[string]bool{}
		for _, c := range trophyFishWhitelist[water] {
			present[c.key] = true
		}
		for _, key := range keys {
			if !present[key] {
				t.Fatalf("%s 的 unspawnable key %q 不在清单里（拼错了？）", water, key)
			}
		}
	}
	// missing / unspawnable 都是数据缺口，只允许出现在有清单的水域。
	for water := range trophyFishWhitelistMissing {
		if len(trophyFishWhitelist[water]) == 0 {
			t.Fatalf("%s 没有清单却记了缺格", water)
		}
	}
}

// 苏拉河面板 16 格全部都能在本图派出来。这一条钉住两件事：
//
//  1. 原先按显示名认成「鲑鱼」k_salmon 的那一格，实际是「鲢鱼」s_carp——苏拉河 37 种
//     鱼里没有任何鲑科鱼，留着旧 key 就会被当成「派不出的格子」永远差一格；
//  2. 分母必须等于 16，15 格上星不发奖杯，16 格上星才发。
func TestWaterwayTrophySuraAllPanelCellsReachable(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	const water = "level_008_sura_river"
	wl := trophyFishWhitelistSet(water)
	if len(wl) != 16 {
		t.Fatalf("苏拉河面板应为 16 格可生成鱼种，实得 %d", len(wl))
	}
	if _, ok := wl["s_carp"]; !ok {
		t.Fatal("苏拉河面板那一格应是鲢鱼 s_carp")
	}
	if _, bad := wl["k_salmon"]; bad {
		t.Fatal("苏拉河派不出 k_salmon，不该留在判定集合里")
	}
	if total := trophyFishWhitelistTotal(water); total != 16 {
		t.Fatalf("苏拉河分母应为 16，实得 %d", total)
	}
	logCatch := func(key string) {
		t.Helper()
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,?,?,1000,0.4,'rod','worm',3,0,0,?,?,?)`,
			key, key, water, uid, "sura-"+key); err != nil {
			t.Fatal(err)
		}
		seedTrophySeen(t, db, uid, water, key, 3)
	}
	keys := make([]string, 0, len(wl))
	for key := range wl {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys[:len(keys)-1] {
		logCatch(key)
	}
	p, err := s.waterwayTrophyProgressFor(ctx, uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if p.Done != 15 || p.Total != 16 {
		t.Fatalf("差一格时进度应为 15/16，实得 %d/%d", p.Done, p.Total)
	}
	if it, err := s.grantWaterwayTrophy(ctx, uid, water); err != nil {
		t.Fatal(err)
	} else if it != nil {
		t.Fatalf("还差一格却发了奖杯: %+v", it)
	}
	logCatch(keys[len(keys)-1])
	p, err = s.waterwayTrophyProgressFor(ctx, uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if p.Done != 16 || p.Total != 16 {
		t.Fatalf("全部上星后进度应为 16/16，实得 %d/%d", p.Done, p.Total)
	}
	if it, err := s.grantWaterwayTrophy(ctx, uid, water); err != nil {
		t.Fatal(err)
	} else if it == nil {
		t.Fatal("16 格全部上星后没有发放奖杯")
	}
}

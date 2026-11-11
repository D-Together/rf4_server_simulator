package gameserver

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"rf4server/internal/store"
)

// seedTrophySeen 直接往 catch_seen 写一条「钓到过」记录（带水域与档位）。
//
// 水域奖杯与稀有鱼奖座都从这张表判定（钓到即算，不等入护），所以测试造历史进度
// 必须写这里；只写 catch_log 现在不推进任何进度。grade < recordTrophyGrade 的鱼
// 不落行，与生产写入一致。
func seedTrophySeen(t *testing.T, db *store.DB, uid int64, water, key string, grade int64) {
	t.Helper()
	if grade < recordTrophyGrade {
		return
	}
	if _, err := db.SQL.Exec(`INSERT INTO catch_seen(uid,loc_level,fish_key,best_grade,first_ts) VALUES(?,?,?,?,1)
		ON CONFLICT(uid,loc_level,fish_key) DO UPDATE SET best_grade=MAX(catch_seen.best_grade, excluded.best_grade)`,
		uid, water, key, grade); err != nil {
		t.Fatalf("插入 catch_seen 失败: %v", err)
	}
}

// whitelistKeysSorted 取某水域面板清单里的 key 并排序，供测试稳定遍历。
func whitelistKeysSorted(t *testing.T, water string) []string {
	t.Helper()
	wl := trophyFishWhitelistSet(water)
	if len(wl) == 0 {
		t.Fatalf("%s 没有面板白名单", water)
	}
	keys := make([]string, 0, len(wl))
	for key := range wl {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// 水域奖杯的准入与发放：面板清单里每一格都要有上星/蓝冠记录才发一枚
// trophy_level_<水域>，发过就不再发（卖掉/删掉也不补）。
func TestWaterwayTrophyGrantedOnlyWhenAllTrophyFishCollected(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// 用有面板白名单的水域（埃尔克湖 17 格）：清单外的鱼种上星不算，差一格也不发。
	const water = "level_019_american_pond"
	keys := whitelistKeysSorted(t, water)
	logCatch := func(key string, grade int64, guid string) {
		t.Helper()
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,?,?,1000,0.4,'rod','worm',?,0,0,?,?,?)`,
			key, key, grade, water, uid, guid); err != nil {
			t.Fatal(err)
		}
		seedTrophySeen(t, db, uid, water, key, grade)
	}
	guid := func(i int) string { return fmt.Sprintf("%032d", i) }
	for i, key := range keys[:len(keys)-1] {
		logCatch(key, 3, guid(i))
	}
	// 最后一格只到「合格」（grade 2），不到上星门槛：面板没填满，不该发。
	logCatch(keys[len(keys)-1], 2, guid(len(keys)))

	if it, err := s.grantWaterwayTrophy(ctx, uid, water); err != nil {
		t.Fatal(err)
	} else if it != nil {
		t.Fatalf("未集齐却发了奖杯: %+v", it)
	}
	var count int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`,
		uid, "trophy_level_019_american_pond").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("未集齐却写入了 %d 枚奖杯", count)
	}

	// 补上最后一格的上星记录 → 发一枚。
	logCatch(keys[len(keys)-1], 3, guid(len(keys)+1))
	it, err := s.grantWaterwayTrophy(ctx, uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if it == nil {
		t.Fatal("集齐后没有发放奖杯")
	}
	if it.ItemID != "trophy_level_019_american_pond" {
		t.Fatalf("奖杯 item_id=%q", it.ItemID)
	}
	if it.Container != "backpack" {
		t.Fatalf("奖杯应落在背包，实得容器 %q", it.Container)
	}
	// 3/16 奖励面板只认 enikeammfen / 2015。
	if it.WireClass != "enikeammfen" || it.TypeID != 2015 {
		t.Fatalf("奖杯 wire=%q tid=%d，3/16 面板会看不到", it.WireClass, it.TypeID)
	}
	// 必须真的写进 item 表：prepareNewItem 只构造对象，漏了 grantPreparedItemTx
	// 就会出现「日志说发了、背包里没有」。
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item
		WHERE uid=? AND item_id=? AND container='backpack' AND wire_class='enikeammfen'`,
		uid, it.ItemID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("奖杯没有落库，item 表里 %d 行", count)
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM kv WHERE k=?`,
		fmt.Sprintf(waterwayTrophyKeyTemplate, uid, water)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("奖杯幂等标记缺失，kv 里 %d 行", count)
	}

	// 第二次调用不再发。
	if again, err := s.grantWaterwayTrophy(ctx, uid, water); err != nil {
		t.Fatal(err)
	} else if again != nil {
		t.Fatalf("重复发放奖杯: %+v", again)
	}

	// 玩家把奖杯卖掉/丢掉后也不补发：这是纪念品，不是可重复领取的奖励。
	if _, err := db.SQL.Exec(`DELETE FROM item WHERE uid=? AND item_id=?`, uid, it.ItemID); err != nil {
		t.Fatal(err)
	}
	if again, err := s.grantWaterwayTrophy(ctx, uid, water); err != nil {
		t.Fatal(err)
	} else if again != nil {
		t.Fatalf("奖杯已被移除却又补发了一枚: %+v", again)
	}
}

// ensureWaterwayTrophies 只处理有奖杯定义的水域，家园与无定义水域直接跳过；
// 同时把历史上已经集齐的水域补出来。
func TestEnsureWaterwayTrophiesBackfillsCompletedWater(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	logCatch := func(water, key, guid string) {
		t.Helper()
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,?,?,1000,0.4,'rod','worm',3,0,0,?,?,?)`,
			key, key, water, uid, guid); err != nil {
			t.Fatal(err)
		}
		seedTrophySeen(t, db, uid, water, key, 3)
	}
	// 家园：没有奖杯定义、也没有面板清单，巡检必须跳过——哪怕 loc_fish 只有一种鱼、
	// 按兜底口径已经「集齐」。
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_alias(alias,loc,note)
		VALUES('level_000_home','家园','test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches)
		VALUES('家园','perch','鲈鱼',10)`); err != nil {
		t.Fatal(err)
	}
	logCatch("level_000_home", "perch", "guid-home")
	// 埃尔克湖：17 格全部上星的历史进度，登录巡检要补发一枚。
	const water = "level_019_american_pond"
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_alias(alias,loc,note)
		VALUES(?,?,'test')`, water, "测试湖"); err != nil {
		t.Fatal(err)
	}
	for i, key := range whitelistKeysSorted(t, water) {
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches)
			VALUES(?,?,?,10)`, "测试湖", key, key); err != nil {
			t.Fatal(err)
		}
		logCatch(water, key, fmt.Sprintf("%032d", i+100))
	}
	got := s.ensureWaterwayTrophies(ctx, uid)
	if len(got) != 1 {
		t.Fatalf("应补发 1 枚水域奖杯，实得 %d：%+v", len(got), got)
	}
	if got[0].ItemID != "trophy_level_019_american_pond" {
		t.Fatalf("补发的水域不对: %q", got[0].ItemID)
	}
	var rows int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`,
		uid, got[0].ItemID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("巡检补发的奖杯没落库，item 表里 %d 行", rows)
	}
	// 再跑一次是幂等的。
	if again := s.ensureWaterwayTrophies(ctx, uid); len(again) != 0 {
		t.Fatalf("巡检不幂等，又发了 %d 枚", len(again))
	}
}

// 18 个水域现在都录了面板清单，但「没有清单就退回 loc_fish 全集」这条兜底必须留着：
// 以后新增水域在补清单之前只能靠它，口径偏大只会晚发、不会早发。
func TestWaterwayTrophyFallsBackToLocFishWhenNoWhitelist(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	const water = "level_099_no_panel"
	if wl := trophyFishWhitelistSet(water); wl != nil {
		t.Fatalf("%s 不该有面板清单，用例前提被破坏了", water)
	}
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_alias(alias,loc,note)
		VALUES(?,?,'test')`, water, "无面板湖"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"perch", "pike", "zander"} {
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches)
			VALUES(?,?,?,10)`, "无面板湖", key, key); err != nil {
			t.Fatal(err)
		}
	}
	seedTrophySeen(t, db, uid, water, "perch", 3)
	seedTrophySeen(t, db, uid, water, "pike", 4)
	p, err := s.waterwayTrophyProgressFor(ctx, uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 3 || p.Done != 2 {
		t.Fatalf("兜底口径应为 2/3，实得 %d/%d", p.Done, p.Total)
	}
	seedTrophySeen(t, db, uid, water, "zander", 3)
	p, err = s.waterwayTrophyProgressFor(ctx, uid, water)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 3 || p.Done != 3 {
		t.Fatalf("兜底口径集齐后应为 3/3，实得 %d/%d", p.Done, p.Total)
	}
	// 该水域没有奖杯物品定义：集齐也不发东西，且不该报错。
	if it, err := s.grantWaterwayTrophy(ctx, uid, water); err != nil {
		t.Fatal(err)
	} else if it != nil {
		t.Fatalf("没有奖杯定义的水域却发出了物品: %+v", it)
	}
}

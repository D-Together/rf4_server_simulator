package gameserver

import (
	"context"
	"fmt"
	"testing"
)

// 水域奖杯的准入与发放：该水域地图鱼种列表里每一种都要有上星/蓝冠记录才发一枚
// trophy_level_<水域>，发过就不再发（卖掉/删掉也不补）。
func TestWaterwayTrophyGrantedOnlyWhenAllTrophyFishCollected(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// 用没有面板白名单的水域（埃尔克湖还没反推出清单），走的是「loc_fish 全集」兜底
	// 口径；白名单口径见 TestWaterwayTrophyUsesPanelWhitelistAsDenominator。
	const water = "level_019_american_pond"
	// 兜底分母取 loc_fish（该水域实际有捕获记录的鱼种），不是 map_fish_roster。
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_alias(alias,loc,note)
		VALUES(?,?,'test')`, water, "测试湖"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"perch", "pike", "zander"} {
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches)
			VALUES(?,?,?,10)`, "测试湖", key, key); err != nil {
			t.Fatal(err)
		}
	}
	logCatch := func(key string, grade int64, guid string) {
		t.Helper()
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,?,?,1000,0.4,'rod','worm',?,0,0,?,?,?)`,
			key, key, grade, water, uid, guid); err != nil {
			t.Fatal(err)
		}
	}
	logCatch("perch", 3, "00000000000000000000000000000001")
	logCatch("pike", 4, "00000000000000000000000000000002")
	logCatch("zander", 2, "00000000000000000000000000000003")

	// 还有一种是合格而不是奖杯：面板没填满，不该发。
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

	// 补上最后一种的奖杯记录 → 发一枚。
	logCatch("zander", 3, "00000000000000000000000000000004")
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
	for _, water := range []string{"level_019_american_pond", "level_000_home"} {
		zh := "测试河"
		if water == "level_000_home" {
			zh = "家园"
		}
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_alias(alias,loc,note)
			VALUES(?,?,'test')`, water, zh); err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches)
			VALUES(?,'perch','鲈鱼',10)`, zh); err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts,loc,spot,fish_key,name_zh,weight_g,length_m,source,bait,grade,xp,silver,loc_level,uid,catch_guid)
			VALUES(1,'loc',0,'perch','perch',1000,0.4,'rod','worm',3,0,0,?,?,?)`,
			water, uid, "guid-"+water); err != nil {
			t.Fatal(err)
		}
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

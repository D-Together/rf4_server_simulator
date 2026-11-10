package gameserver

import (
	"context"
	"fmt"
	"testing"
)

// 3/17「渔夫稀有鱼」面板只陈列上星（grade 3）与蓝冠（grade 4）的渔获。
// 之前的实现按「每个鱼种在该水域的最重一条」入列，合格（grade 2）的鱼也会一直
// 挂在面板上，整片水域看着像全收集。这里逐水域逐档位验证过滤结果。
func TestFishTrophies323OnlyListsTrophyAndBlue(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid int64 = 1000001
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}

	type catch struct {
		loc   string
		key   string
		w     int64
		grade int64
	}
	for i, c := range []catch{
		// 水域 A：perch 只钓过合格，另有更大的合格；ruffe 上了星。
		{"level_001_lake", "perch", 900, 2},
		{"level_001_lake", "perch", 1200, 2},
		{"level_001_lake", "ruffe", 300, 3},
		{"level_001_lake", "ruffe", 200, 3},
		// 水域 B：pike 上过蓝冠，也有更早的合格记录。
		{"level_002_river", "pike", 5000, 2},
		{"level_002_river", "pike", 8000, 4},
		// 水域 C：只有合格，整片水域不该出现在面板里。
		{"level_003_lake", "zander", 2000, 2},
	} {
		if _, err := db.SQL.Exec(`INSERT INTO catch_log
			(ts, loc, spot, fish_key, name_zh, weight_g, length_m, source, bait, grade, xp, silver, loc_level, uid, catch_guid)
			VALUES (?,?,0,?,?,?,?,'rod','worm',?,0,0,?,?,?)`,
			float64(1700000000), c.loc, c.key, c.key, c.w, 0.5, c.grade, c.loc, uid,
			fmt.Sprintf("%032d", i)); err != nil {
			t.Fatalf("插入渔获: %v", err)
		}
	}

	v, err := s.buildFishTrophies323(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]int64{}
	for _, g := range v.Items {
		place := *g.Field(0).Str
		if got[place] == nil {
			got[place] = map[string]int64{}
		}
		for _, c := range g.Field(1).Val.Items {
			got[place][*c.Field(0).Str] = int64(*c.Field(2).Num)
		}
	}

	if _, ok := got["level_003_lake"]; ok {
		t.Fatalf("只有合格鱼的水域不应出现：%v", got)
	}
	if _, ok := got["level_001_lake"]["perch"]; ok {
		t.Fatalf("grade 2 的 perch 不应入列：%v", got)
	}
	if w := got["level_001_lake"]["ruffe"]; w != 300 {
		t.Fatalf("上星鱼应取最重一条 300g，得到 %d：%v", w, got)
	}
	if w := got["level_002_river"]["pike"]; w != 8000 {
		t.Fatalf("蓝冠鱼应取最重一条 8000g，得到 %d：%v", w, got)
	}
	if len(got["level_001_lake"]) != 1 || len(got["level_002_river"]) != 1 {
		t.Fatalf("每水域只应剩奖杯鱼：%v", got)
	}
}

package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFishBaitOverrideRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "bait-override.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	want := FishBaitOverride{
		FishZh:     "常见拟鲤",
		Allow:      []string{"蚯蚓", "红虫"},
		Deny:       []string{"拟饵"},
		BottomOnly: []string{"蚯蚓"},
		Condition:  "出现 jig 阶梯",
		Groups:     [][]string{{"蚯蚓"}, {"红虫", "玉米"}},
		RotateSec:  600,
		RotateMode: "strict",
		Enabled:    true,
	}
	if err := db.SaveFishBaitOverride(ctx, want, "tester"); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListFishBaitOverrides(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("rows=%d", len(list))
	}
	got := list[0]
	if got.FishZh != want.FishZh || got.Condition != want.Condition || got.RotateSec != want.RotateSec {
		t.Fatalf("scalar mismatch: %+v", got)
	}
	if got.RotateMode != "strict" || !got.Enabled || got.UpdatedBy != "tester" {
		t.Fatalf("meta mismatch: %+v", got)
	}
	if len(got.Allow) != 2 || got.Allow[0] != "蚯蚓" || got.Allow[1] != "红虫" {
		t.Fatalf("allow=%v", got.Allow)
	}
	if len(got.Groups) != 2 || len(got.Groups[1]) != 2 || got.Groups[1][1] != "玉米" {
		t.Fatalf("groups=%v", got.Groups)
	}

	// 覆盖写入：同一条鱼再次保存应更新而不是新增。
	want.Allow = []string{"玉米"}
	want.RotateMode = "soft"
	if err := db.SaveFishBaitOverride(ctx, want, "tester2"); err != nil {
		t.Fatal(err)
	}
	list, _ = db.ListFishBaitOverrides(ctx)
	if len(list) != 1 || len(list[0].Allow) != 1 || list[0].Allow[0] != "玉米" || list[0].RotateMode != "soft" {
		t.Fatalf("upsert mismatch: %+v", list)
	}

	// 删除即恢复默认。
	if n, err := db.DeleteFishBaitOverride(ctx, "常见拟鲤"); err != nil || n != 1 {
		t.Fatalf("delete n=%d err=%v", n, err)
	}
	if list, _ = db.ListFishBaitOverrides(ctx); len(list) != 0 {
		t.Fatalf("rows after delete=%d", len(list))
	}

	// 整表清空。
	for _, name := range []string{"A", "B", "C"} {
		o := FishBaitOverride{FishZh: name, Allow: []string{"蚯蚓"}, Enabled: true}
		if err := db.SaveFishBaitOverride(ctx, o, "tester"); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := db.DeleteAllFishBaitOverrides(ctx); err != nil || n != 3 {
		t.Fatalf("delete all n=%d err=%v", n, err)
	}
	if list, _ = db.ListFishBaitOverrides(ctx); len(list) != 0 {
		t.Fatalf("rows after delete all=%d", len(list))
	}
}

func TestFishBaitOverrideSanitizesInput(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "bait-override-sanitize.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// 空串、未知模式、负数间隔都应被规范化，避免脏数据进判鱼路径。
	o := FishBaitOverride{
		FishZh:     "青蛙",
		Allow:      []string{"", "  ", "青蛙"},
		Groups:     [][]string{{"", " "}, {"青蛙"}},
		RotateSec:  -5,
		RotateMode: "weird",
		Enabled:    false,
	}
	if err := db.SaveFishBaitOverride(ctx, o, "tester"); err != nil {
		t.Fatal(err)
	}
	list, _ := db.ListFishBaitOverrides(ctx)
	if len(list) != 1 {
		t.Fatalf("rows=%d", len(list))
	}
	got := list[0]
	if len(got.Allow) != 1 || got.Allow[0] != "青蛙" {
		t.Fatalf("allow not cleaned: %v", got.Allow)
	}
	if got.RotateMode != "soft" || got.RotateSec != 0 || got.Enabled {
		t.Fatalf("mode/sec/enabled not normalized: %+v", got)
	}
	if len(got.Groups) != 1 || len(got.Groups[0]) != 1 {
		t.Fatalf("empty groups not dropped: %v", got.Groups)
	}
}

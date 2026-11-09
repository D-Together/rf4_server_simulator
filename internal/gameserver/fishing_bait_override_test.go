package gameserver

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/store"
)

// 轮换用时间桶计算：同桶不变、跨桶轮转、未配置或间隔过小不启用。
func TestExportedRotationState(t *testing.T) {
	groups := [][]string{{"甲"}, {"乙"}, {"丙"}}
	base := time.Unix(1_800_000_000, 0)
	st := ExportedRotationState(groups, 3600, "soft", base)
	if !st.Enabled || st.Total != 3 || st.Active < 0 || len(st.ActiveBaits) != 1 {
		t.Fatalf("state=%+v", st)
	}
	if st.NextSwitch <= base.Unix() || st.NextSwitch > base.Unix()+3600 {
		t.Fatalf("next_switch=%d not in current bucket", st.NextSwitch)
	}
	same := ExportedRotationState(groups, 3600, "soft", base.Add(3599*time.Second))
	if same.Active != st.Active || same.NextSwitch != st.NextSwitch {
		t.Fatalf("同一时间桶应保持不变: %+v vs %+v", same, st)
	}
	next := ExportedRotationState(groups, 3600, "soft", base.Add(3600*time.Second))
	if want := (st.Active + 1) % 3; next.Active != want {
		t.Fatalf("下一桶应轮转到 %d，实得 %d", want, next.Active)
	}
	// 转满一圈回到原组。
	circle := ExportedRotationState(groups, 3600, "soft", base.Add(3*3600*time.Second))
	if circle.Active != st.Active {
		t.Fatalf("满一轮应回到 %d，实得 %d", st.Active, circle.Active)
	}
	// 关闭与非法输入。
	if d := ExportedRotationState(groups, 30, "soft", base); d.Enabled {
		t.Fatal("间隔小于 60 秒不应启用")
	}
	if d := ExportedRotationState(nil, 3600, "soft", base); d.Enabled {
		t.Fatal("没有组不应启用")
	}
	if d := ExportedRotationState([][]string{{"甲"}}, 3600, "strict", base); d.Enabled || d.Active != -1 || d.NextSwitch != 0 || len(d.ActiveBaits) != 0 {
		t.Fatalf("只有一组时不应启用轮换: %+v", d)
	}
	if d := ExportedRotationState(groups, 3600, "weird", base); d.Mode != "soft" {
		t.Fatalf("未知模式应归一化为 soft，实得 %q", d.Mode)
	}
}

// 后台覆盖：strict 只认当前组，soft 两组都能咬但当前组权重更高。
func TestBaitRotationStrictAndSoft(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	setBaitRuleSwitch(t, s, "1")
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(999811,'rot_a',27,'{"fopbhlmkcpe":6}'),(999812,'rot_b',27,'{"fopbhlmkcpe":6}')`,
		`INSERT INTO item_catalog(item_id,name_zh) VALUES('rot_a','轮换饵甲'),('rot_b','轮换饵乙')`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	groups := [][]string{{"轮换饵甲"}, {"轮换饵乙"}}
	save := func(mode string) {
		t.Helper()
		ov := store.FishBaitOverride{
			FishZh: "常见拟鲤", Allow: []string{"轮换饵甲", "轮换饵乙"},
			Groups: groups, RotateSec: 600, RotateMode: mode, Enabled: true,
		}
		if err := db.SaveFishBaitOverride(ctx, ov, "test"); err != nil {
			t.Fatal(err)
		}
		s.invalidateBaitOverrides()
	}
	keys := []string{"c.roach"}
	roll := func(bait string, at time.Time) ([]string, []float64) {
		return s.filterFishBaitSet("rules-test", []string{bait}, keys, []float64{100}, at)
	}
	// 选一个落在组 0（甲）的时间点。
	at := time.Unix(1_800_000_000, 0)
	for (at.Unix()/600)%2 != 0 {
		at = at.Add(600 * time.Second)
	}

	save("strict")
	if got, _ := roll("rot_a", at); len(got) != 1 {
		t.Fatalf("strict 当前组应可咬，实得 %v", got)
	}
	if got, _ := roll("rot_b", at); len(got) != 0 {
		t.Fatalf("strict 组外不应咬，实得 %v", got)
	}
	next := at.Add(600 * time.Second)
	if got, _ := roll("rot_b", next); len(got) != 1 {
		t.Fatalf("strict 下一组应可咬，实得 %v", got)
	}
	if got, _ := roll("rot_a", next); len(got) != 0 {
		t.Fatalf("strict 上一组不应再咬，实得 %v", got)
	}

	save("soft")
	_, wA := roll("rot_a", at)
	_, wB := roll("rot_b", at)
	if len(wA) != 1 || len(wB) != 1 {
		t.Fatalf("soft 两组都应可咬: A=%v B=%v", wA, wB)
	}
	if wA[0] <= wB[0] {
		t.Fatalf("soft 当前组权重应更高: A=%f B=%f", wA[0], wB[0])
	}
}

// 覆盖可以整组替换内置规则；删掉覆盖后回到内置规则。
func TestBaitOverrideReplacesDefaultAndResets(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	setBaitRuleSwitch(t, s, "1")
	ctx := context.Background()
	keys := []string{"c.roach"}
	at := time.Unix(1_800_000_000, 0)
	roll := func(bait string) ([]string, []float64) {
		return s.filterFishBaitSet("rules-test", []string{bait}, keys, []float64{100}, at)
	}
	// 内置规则：常见拟鲤 不吃拟饵。
	if got, _ := roll("rule_lure"); len(got) != 0 {
		t.Fatalf("内置规则应拒绝拟饵，实得 %v", got)
	}
	// 覆盖成允许 Hunter 1-001（拟饵类名）。
	ov := store.FishBaitOverride{FishZh: "常见拟鲤", Allow: []string{"Hunter 1-001"}, Enabled: true}
	if err := db.SaveFishBaitOverride(ctx, ov, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	if got, _ := roll("rule_lure"); len(got) != 1 {
		t.Fatalf("覆盖后应允许 Hunter 1-001，实得 %v", got)
	}
	// 恢复默认。
	if _, err := db.DeleteFishBaitOverride(ctx, "常见拟鲤"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	if got, _ := roll("rule_lure"); len(got) != 0 {
		t.Fatalf("恢复默认后应再次拒绝拟饵，实得 %v", got)
	}
}

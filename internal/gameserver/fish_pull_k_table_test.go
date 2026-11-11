package gameserver

import (
	"math"
	"testing"
	"time"
)

// 官服 14/14 抓包 A 级锚点（2026-09-18 真值表 9 样本 + 2026-11-10/11 新抓 12+8 样本，
// 见 specs/changes/fish-pull-k-table/spec.md「Further Notes」样本表）。
// 只落数值锚点，不含任何抓包原始数据（CONTRIBUTING 禁令不受影响）。
var officialPullAnchors = []struct {
	species string
	weightG int64
	pullN   float64 // 官方 f8（牛）
	tolPct  float64 // 允许偏差
}{
	{"crucian", 611, 9.0919, 2},
	{"crucian", 293, 4.28627, 2},
	{"crucian", 743, 11.1182, 2},
	{"crucian", 193, 2.7937, 2},
	{"c.roach", 680, 11.0735, 2},
	{"c.roach", 150, 2.3899, 2},
	{"c.roach", 713, 11.6109, 2},
	{"perch", 221, 3.74761, 2},
	{"perch", 93, 1.55752, 3},
	{"perch", 1070, 18.9743, 3},
	{"perch", 119, 1.99803, 3},
	{"perch", 342, 5.86736, 3},
	{"perch", 642, 11.1688, 3},
	{"g_carp", 21742, 423.798, 2},
	{"g_carp", 4880, 93.4397, 2},
	{"g_carp", 5484, 105.073, 2},
	{"g_carp", 5716, 109.545, 2},
	{"g_carp", 10307, 198.496, 2},
	{"b_carp", 7828, 146.519, 2},
	{"carp", 1063, 17.5527, 2},
	{"taran", 611, 11.3884, 2},
	{"piksha", 2975, 55.451, 2},
	{"merlang", 252, 5.19145, 2},
	{"b_gill", 113, 2.21706, 2},
	{"n.pike", 4305, 76.6636, 2},
	{"din_l_carp", 1715, 29.3737, 2},
	{"c.carp", 655, 8.9112, 2},
	{"c.carp", 140, 1.8186, 3},
	{"s.bream", 359, 4.5193, 2},
}

// TestFishPullMatchesOfficialAnchors 防回归：已实测鱼种的 f8 必须落在官服
// A 级样本 ±tol% 内。这是 P1 修复的核心验收。
func TestFishPullMatchesOfficialAnchors(t *testing.T) {
	srv := &Server{}
	for _, a := range officialPullAnchors {
		f := &RolledFish{Species: a.species, WeightG: a.weightG}
		got := srv.fishFightPullFor(f)
		tol := a.pullN * a.tolPct / 100
		if math.Abs(got-a.pullN) > tol {
			t.Errorf("%s %dg f8=%.4fN，官服=%.4fN（±%v%%）", a.species, a.weightG, got, a.pullN, a.tolPct)
		}
	}
}

// TestFishPullGroupFallback 验证未实测鱼种走习性组表与 fallback，量级为官方域。
func TestFishPullGroupFallback(t *testing.T) {
	srv := &Server{}
	cases := []struct {
		species string
		group   string
		wantK   float64
	}{
		{"riv_rb_trout", "salmonid", 18.0},
		{"lm_b_bass", "predator", 17.5},
		{"coalfish", "marine_gadid", 19.7},
		{"karasekarp", "still_carp", 15.0},
		{"not_in_calib", "fallback", 17.5},
	}
	for _, c := range cases {
		if got := srv.fishPullK0For(c.species); math.Abs(got-c.wantK) > 1e-9 {
			t.Errorf("%s K0=%f，期望 %s 组 %f", c.species, got, c.group, c.wantK)
		}
		// 100g 锚定斜率后不得越出官方 k 域 [12.6, 20.6] 太远（100g 时 ln 项 −1.6）。
		pull := srv.fishFightPullFor(&RolledFish{Species: c.species, WeightG: 1000})
		k := pull // w=1kg 时 k 即 pull
		if k < 12.0 || k > 21.5 {
			t.Errorf("%s 1kg 等效 k=%f 越出官方域", c.species, k)
		}
	}
}

// TestFishPullLnSlopeTunable 验证斜率与 fallback 的 tunable 应急覆盖。
func TestFishPullLnSlopeTunable(t *testing.T) {
	srv := &Server{}
	srv.tunCache = map[string]float64{"fish_pull_ln_slope": 0}
	srv.tunAt = time.Now()
	if got := srv.fishFightPullFor(&RolledFish{Species: "perch", WeightG: 1000}); math.Abs(got-17.6) > 1e-6 {
		t.Fatalf("斜率置 0 后 1kg perch 应恰为 K0=17.6，实得 %f", got)
	}
	if got := srv.fishPullK0For("not_in_calib"); got != 17.5 {
		t.Fatalf("fallback K0 应为 17.5，实得 %f", got)
	}
}

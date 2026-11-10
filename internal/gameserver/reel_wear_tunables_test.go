package gameserver

// 摩擦片磨损参数的 tunable 化验证。见 specs/changes/reel-wear-tunables。
//
// 这组测试分两半：
//  1. 无 DB 的零值 Server：验证「未配置时行为与硬编码逐字节一致」；
//  2. 带 DB 的 Server：验证 tunable 覆盖与非法值回落。
//
// 打滑/烧蚀的物理手算样例在 reel_fight_wear_test.go 里已有覆盖，这里只钉参数通路。

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestReelSlipWearDefaultMatchesConstants(t *testing.T) {
	t.Parallel()
	srv := &Server{} // 无 DB：tunableF 一律回落默认
	cases := []struct {
		setting int64
		want    float64
	}{
		{0, reelSlipWearLow},
		{24, reelSlipWearLow},
		{25, 0.000025},
		{26, 0.00005},
		{27, 0.000075},
		{28, 0.000095},
		{29, reelSlipWearMax},
	}
	for _, tc := range cases {
		if got := srv.reelSlipWearForSetting(tc.setting); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("档位 %d 默认磨损率=%.9f，期望 %.9f", tc.setting, got, tc.want)
		}
		if got := reelSlipWearForSettingDefault(tc.setting); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("档位 %d 包级默认口径=%.9f，期望 %.9f", tc.setting, got, tc.want)
		}
	}
}

func TestReelSlipCalibrationAndBurnDefaults(t *testing.T) {
	t.Parallel()
	srv := &Server{}
	if got := srv.reelSlipCalibrationSeconds(); math.Abs(got-reelSlipCalibrationSeconds) > 1e-12 {
		t.Errorf("默认校准秒数=%.4f，期望 %.4f", got, reelSlipCalibrationSeconds)
	}
	if got := srv.reelBurnCalibrationDrag(); math.Abs(got-reelBurnCalibrationDragKg) > 1e-12 {
		t.Errorf("默认校准制动力=%.4f，期望 %.4f", got, reelBurnCalibrationDragKg)
	}
}

// 打滑磨损量 = 档位率 / 校准秒数 x tick x 油系数。校准秒数翻倍 → 磨损减半。
func TestReelSlipWearHalvesWithDoubledCalibration(t *testing.T) {
	t.Parallel()
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()

	w := newFightWearForTest(7.7, 5, 9, 7.5, 1, 1, false)
	w.setting = 29
	const tick = 2.0

	_, base, _, _, _ := srv.fightReelTickWear(w, tick)
	if base <= 0 {
		t.Fatalf("基准磨损应大于 0，实得 %.9f", base)
	}
	// 手算：0.0001 / 1.8 * 2 * 1 * 1
	wantBase := defaultReelSlipWearMax / reelSlipCalibrationSeconds * tick
	if math.Abs(base-wantBase) > 1e-12 {
		t.Fatalf("29 档打滑磨损=%.12f，手算 %.12f", base, wantBase)
	}

	if err := db.TunableSet(ctx, "reel_slip_calibration_seconds", "3.6", "test"); err != nil {
		t.Fatal(err)
	}
	srv.tunAt = time.Time{}
	_, halved, _, _, _ := srv.fightReelTickWear(w, tick)

	if math.Abs(halved-base/2) > 1e-12 {
		t.Fatalf("校准秒数翻倍后磨损应减半：base=%.12f halved=%.12f", base, halved)
	}
}

// 档位率本身可调：把 29 档改小一半，磨损同步减半。
func TestReelSlipWearHonorsSettingTunable(t *testing.T) {
	t.Parallel()
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()

	if err := db.TunableSet(ctx, "reel_slip_wear_max", "0.00005", "test"); err != nil {
		t.Fatal(err)
	}
	srv.tunAt = time.Time{}
	if got := srv.reelSlipWearForSetting(29); math.Abs(got-0.00005) > 1e-12 {
		t.Fatalf("29 档应读 tunable 0.00005，实得 %.9f", got)
	}
	// 未单独配置的档位仍用各自默认
	if got := srv.reelSlipWearForSetting(25); math.Abs(got-defaultReelSlipWear25) > 1e-12 {
		t.Fatalf("25 档应保持默认 %.9f，实得 %.9f", defaultReelSlipWear25, got)
	}
}

// 非法值（0 / 负 / 非数值）必须回落到代码默认，不能把模型算歪。
func TestReelSlipWearFallsBackOnInvalidTunable(t *testing.T) {
	t.Parallel()
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()

	for _, bad := range []string{"0", "-1", "abc"} {
		if err := db.TunableSet(ctx, "reel_slip_wear_max", bad, "test"); err != nil {
			t.Fatalf("写入 %q 失败: %v", bad, err)
		}
		srv.tunAt = time.Time{}
		if got := srv.reelSlipWearForSetting(29); math.Abs(got-defaultReelSlipWearMax) > 1e-12 {
			t.Fatalf("%q 应回落到默认 %.9f，实得 %.9f", bad, defaultReelSlipWearMax, got)
		}
	}
}

// 烧穿时长 T = base x (rated/校准值) x 油 / 过载。校准值翻倍 → T 减半。
func TestReelBurnSecondsShrinkWithCalibrationDrag(t *testing.T) {
	t.Parallel()
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()

	// rated=15.4 → strength = 15.4/7.7 = 2；过载 pull/locked-1+0.35 = 1.35
	w := newFightWearForTest(15.4, 5, 15.4, 20, 1, 1, true)
	base := srv.reelBurnSecondsFor(w)
	if base <= 0 {
		t.Fatalf("基准烧穿时长应大于 0，实得 %.4f", base)
	}

	if err := db.TunableSet(ctx, "reel_burn_calibration_drag_kg", "15.4", "test"); err != nil {
		t.Fatal(err)
	}
	srv.tunAt = time.Time{}
	shrunk := srv.reelBurnSecondsFor(w)

	if math.Abs(shrunk-base/2) > 1e-9 {
		t.Fatalf("校准制动力翻倍后烧穿时长应减半：base=%.4f shrunk=%.4f", base, shrunk)
	}
}

// 每竿基准与泡水走消耗矩阵，不受打滑参数影响。
func TestReelPartWearPerRodUnaffectedBySlipTunable(t *testing.T) {
	t.Parallel()
	counts := reelPartCounts{Rods: 1, Soaks: 1}
	got := reelPartWearFor(reelPartCatGear, 100, counts, reelSlipCalibrationSeconds)
	want := 0.030 / 100
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("齿轮每竿基准=%.9f，期望 %.9f", got, want)
	}
}

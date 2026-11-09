package gameserver

import (
	"math"
	"testing"
)

func TestReelOilCompatibilityTiers(t *testing.T) {
	tests := []struct {
		name   string
		reelID string
		brand  string
		oilID  string
		want   float64
	}{
		{"beluga dedicated", "spin_beluga_venga_01", "beluga", "blg_reel_oil", reelOilMatchDedicated},
		{"zemex dedicated", "spin_5354", "zmn_spreel_jsxs", "zmn_reel_oil", reelOilMatchDedicated},
		{"ef dedicated", "spin_5820", "ef_spark_2", "ef_reel_oil", reelOilMatchDedicated},
		{"high universal", "spin_beluga_venga_01", "beluga", "sb_b_reel_oil", reelOilMatchUniversalHigh},
		{"standard universal", "spin_beluga_venga_01", "beluga", "oil_aquatica", reelOilMatchUniversal},
		{"mismatch", "spin_beluga_venga_01", "beluga", "zmn_reel_oil", reelOilMatchMismatch},
		{"unknown reel universal", "spin_custom_01", "", "sb_b_reel_oil", reelOilMatchUniversalHigh},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reelOilMatchFactor(tc.reelID, tc.brand, tc.oilID); math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("适配倍率=%f，期望 %f", got, tc.want)
			}
		})
	}
}

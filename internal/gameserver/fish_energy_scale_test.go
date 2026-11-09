package gameserver

import "testing"

// 精力对鱼体力池的放大：精力满不放大，精力见底放大到 (1+penalty) 倍。
func TestScaleFishStaminaForEnergy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		base    float64
		ratio   float64
		penalty float64
		want    float64
	}{
		{"full energy no scale", 100, 1.0, 1.0, 100},
		{"half energy x1.5", 100, 0.5, 1.0, 150},
		{"empty energy x2", 100, 0.0, 1.0, 200},
		{"penalty disabled", 100, 0.0, 0, 100},
		{"ratio above one clamps", 100, 1.2, 1.0, 100},
		{"zero energy floor", 100, -0.3, 1.0, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scaleFishStaminaForEnergy(c.base, c.ratio, c.penalty)
			if got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

// 精力比例必须按“有效上限”归一化：装备把上限抬到 1.0 时，0.85 的精力量
// 只能算 85%，不能当满精力（否则玩家凭空白少 15% 惩罚）。
func TestEnergyRatioForCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		primary float64
		cap     float64
		want    float64
	}{
		{"base cap full", 0.85, 0.85, 1.0},
		{"gear cap not full", 0.85, 1.0, 0.85},
		{"gear cap half", 0.5, 1.0, 0.5},
		{"over cap clamps", 1.2, 1.0, 1.0},
		{"negative floors", -0.1, 0.85, 0},
		{"zero cap treated as full", 0.4, 0, 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := energyRatioForCap(c.primary, c.cap); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

// 装备加成场景：同一精力量在基础上限与加成上限下，惩罚不同。
func TestFishStaminaEnergyPenaltyWithGearCap(t *testing.T) {
	t.Parallel()
	const primary = 0.85
	const penalty = 1.0
	baseRatio := energyRatioForCap(primary, 0.85) // 满精力，不放大
	gearRatio := energyRatioForCap(primary, 1.0)  // 只有 85%，仍要惩罚
	if got := scaleFishStaminaForEnergy(1000, baseRatio, penalty); got != 1000 {
		t.Fatalf("基础上限满精力应不放大，实得 %v", got)
	}
	if got := scaleFishStaminaForEnergy(1000, gearRatio, penalty); got <= 1000 {
		t.Fatalf("加成上限下 0.85 不是满精力，应放大，实得 %v", got)
	}
}

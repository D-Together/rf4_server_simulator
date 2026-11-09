package gameserver

import "time"

// drinkRule 是本地设定的饮酒经验规则。
type drinkRule struct {
	Category string
	Peak     float64
	Duration time.Duration
}

func drinkRuleForCategory(category string) (drinkRule, bool) {
	switch category {
	case "beer":
		return drinkRule{Category: category, Peak: .05, Duration: 5 * time.Minute}, true
	case "wine":
		return drinkRule{Category: category, Peak: .10, Duration: 10 * time.Minute}, true
	case "spirit":
		return drinkRule{Category: category, Peak: .15, Duration: 15 * time.Minute}, true
	case "mead":
		return drinkRule{Category: category, Peak: .20, Duration: 20 * time.Minute}, true
	default:
		return drinkRule{}, false
	}
}

// gameHourReal 世界时钟 24 倍速：1 游戏小时 = 150 现实秒（world_clock.go）。
const gameHourReal = 150 * time.Second

// officialDrinkBonuses 官服单瓶经验加成（gamekee 官服酒表 2026-11-09
// 存档，drink-bonus-official.docx，28 款有实测值 / 30 个 item_id）。
// 峰值为单瓶经验加成小数，时长为游戏小时（按 gameHourReal 折算）。
// 官服表空白（—）或待定（？）的单品不在表中，回退本地类别档位；
// TUNGUS 伏特加总表 10% 与地图页 12% 冲突，取总表值。
var officialDrinkBonuses = map[string]struct {
	peak  float64
	hours int
}{
	"rd_ice_vine":       {.25, 12},
	"AV_shamp_tsr":      {.20, 12},
	"rd_white_vine":     {.19, 12},
	"rd_red_vine":       {.19, 12},
	"XXIVA_bottle_vdka": {.16, 12},
	"kala_vodka":        {.14, 10},
	"hooch_corn":        {.10, 9},
	"tungus_vodka":      {.10, 9},
	"gorilka":           {.085, 9},
	"zdn_bottle_cogn5":  {.08, 9},
	"boyar_bottle_vdka": {.08, 9},
	"hooch_potato":      {.07, 9},
	"pervach":           {.065, 7},
	"AV_shamp_1l":       {.06, 12},
	"AV_shamp_p_6l":     {.06, 12},
	"zdn_bottle_cogn3":  {.059, 6},
	"pm_medovuha":       {.059, 8},
	"vodka_glass":       {.055, 6},
	"thermos_gwein":     {.05, 6},
	"kr_pennik":         {.05, 6},
	"zdn_bottle_vdka":   {.046, 6},
	"AV_shamp_1l_8":     {.04, 7},
	"AV_shamp_1lg":      {.04, 7},
	"apple_sidr":        {.04, 6},
	"vine_bottle_em":    {.04, 6},
	"lb_bottle_krep":    {.039, 5},
	"lb_bottle_krep_ny": {.039, 6},
	"lb_bottle_trad_ny": {.035, 5},
	"wood_can_dark":     {.033, 5},
	"lb_bottle_eks_ny":  {.029, 6},
}

// 白名单按 4.0.25034 饮品定义和名称分类，不包含微量含酒食物或无酒精替代品。
// 官服有实测值的单品用 officialDrinkBonuses 覆盖类别档位；其余回退类别档。
func drinkRuleForItem(itemID string) (drinkRule, bool) {
	var category string
	switch itemID {
	case "kvas_bottle", "kvas_glass",
		"lb_bottle_ch_ale_ny", "lb_bottle_eks", "lb_bottle_eks_ny",
		"lb_bottle_krep", "lb_bottle_krep_ny", "lb_bottle_trad", "lb_bottle_trad_ny",
		"wood_bottle_cask", "wood_bottle_dark", "wood_bottle_lager",
		"wood_can_cask", "wood_can_dark", "wood_can_lager":
		category = "beer"
	case "AV_shamp_1l", "AV_shamp_1l_8", "AV_shamp_1lg", "AV_shamp_p_6l", "AV_shamp_tsr",
		"apple_sidr", "g_vine_bottle",
		"rd_ice_vine", "rd_ice_vine25", "rd_ice_vine26",
		"rd_red_vine", "rd_red_vine_2017", "rd_red_vine_8", "rd_white_vine",
		"rot_bottle_porto", "thermos_gwein", "vine_bottle_em":
		category = "wine"
	case "XXIVA_bottle_vdka", "boyar_bottle_vdka", "gorilka", "hooch_corn", "hooch_potato",
		"kala_vodka", "kr_pennik", "pervach", "tungus_vodka", "vodka_glass",
		"zdn_bottle_cogn3", "zdn_bottle_cogn5", "zdn_bottle_vdka":
		category = "spirit"
	case "barrel_honey", "pm_medovuha", "pm_medovuha_g", "pm_medovuha_g_8", "sbiten":
		category = "mead"
	default:
		return drinkRule{}, false
	}
	if b, ok := officialDrinkBonuses[itemID]; ok {
		return drinkRule{Category: category, Peak: b.peak,
			Duration: time.Duration(b.hours) * gameHourReal}, true
	}
	return drinkRuleForCategory(category)
}

// consumableRestoreForItem 食品/热饮/药品的恢复分类（4/8 使用通道）：
// 咖啡→精力、茶→舒适度、可可→饱食+少量精力、药品→健康、其余食品→饱食度。
// 分类映射来自维护者实测经验与官方游戏提示（热茶提高舒适度等），
// 见 specs/changes/player-condition-core Further Notes；恢复量为 tunables
// 标定值，非官服常量。白名单只收可直接食用的成品；烹饪材料
// （面粉/糖/油/盐/调料/生米面/香精等）与酒类（走经验加成规则）不进表。
func consumableRestoreForItem(itemID string) (string, bool) {
	switch itemID {
	// 药品：静态库里品类 26 且 fopbhlmkcpe=3 的只有 Sorbimax 一种
	//（`pill_pack`，154 食物 / 92 饮品 / 1 药品），按品类标记收录而非按名称猜。
	case "pill_pack":
		return "medicine", true
	// 咖啡：现磨杯/罐装/瓶装/保温瓶。
	case "big_coffee", "big_coffee_can", "big_coffee_can25", "big_coffee_can26",
		"big_coffee_can_g", "fcb_coffee_can", "fcb_coffee_can2", "brand_coffe",
		"small_coffee", "thermos_coffee":
		return "coffee", true
	// 茶：罐装/保温瓶。
	case "ab_tea_can", "ab_tea_can_g", "ab_tea_can2", "thermos_tea":
		return "tea", true
	// 可可：冲饮/保温瓶。
	case "food_cacao", "thermos_cacao":
		return "cocoa", true
	// 可直接食用的食品：蔬果/蛋奶/主食/菜肴/肉鱼/罐头/甜品/软饮。
	case "apples", "carrot", "cucumbers", "cucumbers_mc", "melon", "plum", "tangerin",
		"wmelon_ast", "onion", "potatoes", "grilled_apple", "vinegret",
		"boiled_egg", "food_egg", "farm_milk", "ff_pack_milk", "ff_pack_kefir",
		"food_sweetmilk", "farm_cheese", "ff_proc_cheese",
		"baked_potato", "bowl_potato", "fried_potato", "fish_potato",
		"buckwheat_porridge", "borshch", "fish_soup_nal", "fish_soup_red", "fish_soup_tr",
		"rye_bread_r", "loaf_01", "kalach", "bublik", "r_pie", "pancakes",
		"christmas_pancakes", "pelmeni", "sandwich_cafe", "salmon_steak", "pot_roast",
		"beef", "pork", "lard", "hm_lard", "cutlet_p", "pike_cutlets", "shashlyk_p",
		"fried_bychki", "fried_carp", "fried_karasy", "fried_nalim", "fried_sudak",
		"sm_pikeperch", "sm_trout", "smoked_bream", "smoked_catfish", "smoked_salmon",
		"boiled_crayfish", "dried_chekhon", "dried_plotva", "dried_taran", "dried_vobla",
		"caviar_black", "caviar_pike", "caviar_red",
		"ff_krak_sausage", "grilled_sausage", "ff_pork_canned", "ff_saira_canned",
		"cf_codliver", "cf_crab", "cf_halibut", "cf_oyster", "cf_seabass", "cf_squid",
		"rf_codliver", "rf_crab", "rf_halibut", "rf_oyster", "rf_seabass",
		"cake_6years", "cake_rf4", "candy_murka_1", "candy_murka_2",
		"candystick_1", "candystick_2", "candystick_3",
		"chocofish_1", "chocofish_2", "chocofish_3", "chocolate_santa25",
		"pb_d_choc", "pb_d_choc25", "pb_d_choc26", "pb_d_choc_8", "pb_d_choc_g", "pb_d_chocg",
		"pb_m_choc", "pb_m_choc25", "pb_m_choc26", "pb_m_choc_8", "pb_m_choc_g", "pb_m_chocg",
		"sd_shoc_bar", "pryanik_ny26", "pack_marshm", "grilled_marshm",
		"christmas_dish", "fishcookie",
		"mc_bottle_cola", "mc_can_cola", "brand_softdrink", "mors_bottle_hm", "kisel_jug",
		// 瓶装无酒精成品饮料：柠檬汽水/节日柠檬水、无酒精香槟（_a0）、
		// 无酒精蜜酒/麦芽（alcfree/_a0）。原料（ff_black_tea 等）与酒类
		// 不进表——见 drink_rules_test 的排除清单。
		"lb_bottle_lim", "lb_bottle_lim_ny", "lim_ny25_bottle",
		"AV_shamp_p_a0", "AV_shamp_tsr_a0",
		"pm_medovuha_alcfree", "pm_medovuha_alcfree_g", "lb_bottle_ale_ny_a0":
		return "food", true
	}
	return "", false
}

// drinkRate 前 75% 时间为峰值，最后 25% 时间线性降至零。
func drinkRate(category string, remaining time.Duration) float64 {
	rule, ok := drinkRuleForCategory(category)
	if !ok {
		return 0
	}
	return drinkRateFor(rule.Peak, rule.Duration, remaining)
}

// drinkRateFor 按效果记录里的单瓶峰值和自身时长算当前倍率。
func drinkRateFor(peak float64, duration, remaining time.Duration) float64 {
	if peak <= 0 || duration <= 0 || remaining <= 0 {
		return 0
	}
	tail := duration / 4
	if remaining >= tail {
		return peak
	}
	return peak * float64(remaining) / float64(tail)
}

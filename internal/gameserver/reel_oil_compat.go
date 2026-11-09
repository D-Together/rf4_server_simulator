package gameserver

import "strings"

// 轮油适配是独立于品质的乘数：品牌专用油最省，高端通用油次之，
// 普通通用油再次之，不匹配油不提供额外保护。
const (
	reelOilMatchDedicated     = 0.50
	reelOilMatchUniversalHigh = 2.0 / 3.0
	reelOilMatchUniversal     = 0.75
	reelOilMatchMismatch      = 1.0
)

var reelOilFamilyByPrefix = map[string]string{
	"blg":  "beluga",
	"zmn":  "zemex",
	"reef": "reef",
	"sat":  "sato",
	"ef":   "ef",
	"syb":  "syberia",
	"spk":  "spark",
	"trd":  "trident",
}

var reelFamilyAliases = map[string]string{
	"beluga":  "beluga",
	"blg":     "beluga",
	"zmn":     "zemex",
	"zemex":   "zemex",
	"zm":      "zemex",
	"reef":    "reef",
	"sat":     "sato",
	"sato":    "sato",
	"ef":      "ef",
	"syb":     "syberia",
	"syberia": "syberia",
	"spk":     "spark",
	"spark":   "spark",
	"trd":     "trident",
	"trident": "trident",
}

func lowerItemToken(itemID string) string {
	return strings.ToLower(strings.TrimSpace(itemID))
}

// reelOilFamily 返回品牌专用油对应的轮子家族和通用油的基础适配倍率。
func reelOilFamily(itemID string) (string, float64) {
	id := lowerItemToken(itemID)
	switch {
	case strings.HasPrefix(id, "c_zg_"):
		return "", reelOilMatchUniversalHigh
	case strings.HasPrefix(id, "m_au_"):
		return "", reelOilMatchUniversalHigh
	case strings.HasPrefix(id, "sb_"), strings.HasPrefix(id, "sl_"):
		return "", reelOilMatchUniversalHigh
	case strings.HasPrefix(id, "m_g_"), strings.HasPrefix(id, "msh_"),
		strings.HasPrefix(id, "tama_"), strings.HasPrefix(id, "oil_"):
		return "", reelOilMatchUniversal
	}
	prefix := id
	if i := strings.IndexAny(prefix, "_-"); i >= 0 {
		prefix = prefix[:i]
	}
	if family, ok := reelOilFamilyByPrefix[prefix]; ok {
		return family, reelOilMatchDedicated
	}
	return "", reelOilMatchUniversal
}

// reelFamilyFromItemID 从渔轮实例 ID 识别品牌家族。兼容表里的实例 ID
// 已经带有 spin_beluga、bclp_zmn 这类稳定品牌段。
func reelFamilyFromItemID(itemID, brand string) string {
	id := lowerItemToken(itemID)
	brand = lowerItemToken(brand)
	for alias, family := range reelFamilyAliases {
		if strings.HasPrefix(brand, alias+"_") ||
			brand == alias ||
			strings.HasPrefix(id, alias+"_") ||
			strings.Contains(id, "_"+alias+"_") ||
			strings.Contains(id, "-"+alias+"-") ||
			strings.HasSuffix(id, "_"+alias) ||
			strings.HasSuffix(id, "-"+alias) {
			return family
		}
	}
	return ""
}

// reelOilMatchFactor 返回轮油对当前渔轮的磨损频率倍率。
// 未知轮子按普通通用油处理，不把缺少品牌信息的历史轮子误判成不匹配。
func reelOilMatchFactor(reelItemID, reelBrand, oilItemID string) float64 {
	oilFamily, fallback := reelOilFamily(oilItemID)
	reelFamily := reelFamilyFromItemID(reelItemID, reelBrand)
	if oilFamily != "" && reelFamily != "" && oilFamily == reelFamily {
		return reelOilMatchDedicated
	}
	if reelFamily == "" {
		if fallback == reelOilMatchDedicated {
			return reelOilMatchUniversal
		}
		return fallback
	}
	if oilFamily == "" {
		return fallback
	}
	return reelOilMatchMismatch
}

package gameserver

// 轮根规格表：assets/reel_strength.json（来源 轮表.xlsx"精确锁轮表"列，按 ItemID 对齐）。
// root_spec 是出厂满耐久轮根——渔轮实例 nlmnpekipjc(csStrength) 的标称值；
// 官方 4/22 实抓验证：实例轮根 = root_spec × 轴承子件耐久
// （Turion SW 30000: 127.94×0.98779≈126.35；Tagara II 6000: 45.89×0.9756≈44.77）。
// 表里没有的 ItemID 回退旧口径（摩擦片值），canon 时打一次性警告。
//
// 注意区分：root_spec 不是摩擦片额定（base_drag，官方语义里两者独立，比值 2.2~3.6×）。

import (
	_ "embed"
	"encoding/json"
	"sync"

	"rf4server/internal/store"
)

//go:embed assets/reel_strength.json
var reelStrengthJSON []byte

var (
	reelStrengthOnce  sync.Once
	reelStrengthTable map[string]reelStrengthEntry
	reelStrengthMu    sync.Mutex
	// reelStrengthMissed 记录已告警过的未收录 ItemID，避免每次 canon 都刷日志。
	reelStrengthMissed = map[string]bool{}
)

type reelStrengthEntry struct {
	Model    string  `json:"model"`
	RootSpec float64 `json:"root_spec"`
	BaseDrag float64 `json:"base_drag"`
	Tier     string  `json:"tier"` // exact=精确锁轮表；estimate=沿用旧估算
}

// loadReelStrengthTable 解析编译期嵌入的规格表。使用 go:embed 而不是读取工作目录，
// 保证源码运行、测试和发布包看到同一份数据，也避免发布时漏带资源后静默回退旧口径。
func loadReelStrengthTable() {
	reelStrengthOnce.Do(func() {
		var doc struct {
			Reels map[string]reelStrengthEntry `json:"reels"`
		}
		if err := json.Unmarshal(reelStrengthJSON, &doc); err != nil {
			return
		}
		reelStrengthTable = doc.Reels
	})
}

// reelRootSpec 返回该轮子的出厂满耐久轮根规格。
// found=false 表示表里没有该 ItemID，调用方回退旧口径。
func reelRootSpec(itemID string) (float64, bool) {
	loadReelStrengthTable()
	e, ok := reelStrengthTable[itemID]
	if !ok || e.RootSpec <= 0 {
		return 0, false
	}
	return e.RootSpec, true
}

// reelMechStrengthKg 按定义取机构强度（出厂满耐久锁轮承力标称值）。
// 规格表未收录时返回 0，调用方回退旧公式（基础制动力 × 耐久倍率）。
func reelMechStrengthKg(def *store.ItemDef) float64 {
	if def == nil {
		return 0
	}
	spec, ok := reelRootSpec(def.ItemID)
	if !ok {
		return 0
	}
	return spec
}

// reelRootMissOnce 对未收录的 ItemID 只打一次警告。
func reelRootMissOnce(logf func(msg string, keys ...any), itemID string) {
	reelStrengthMu.Lock()
	defer reelStrengthMu.Unlock()
	if reelStrengthMissed[itemID] {
		return
	}
	reelStrengthMissed[itemID] = true
	logf("轮根规格表未收录该轮子，回退摩擦片值口径", "item_id", itemID)
}

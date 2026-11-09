package gameserver

import (
	"context"
	"strings"
	"time"
)

// 轮换参数：当前生效组的饵加权，其余组的饵降权（soft）；strict 时只认当前组。
const (
	baitRotationBoost   = 3.0
	baitRotationPenalty = 0.5
	minBaitRotateSec    = 60
	baitOverrideTTL     = 15 * time.Second
)

// baitRotation 描述一个鱼种的饵料轮换配置。
type baitRotation struct {
	groups    [][]string
	rotateSec int64
	mode      string // soft | strict
}

// baitOverrideEntry 是运行时生效的覆盖：规则本体 + 轮换配置。
type baitOverrideEntry struct {
	rule fishBaitRule
	rot  baitRotation
}

// RotationState 是轮换在某一时刻的可读状态，供后台展示与判鱼共用。
type RotationState struct {
	Enabled     bool     `json:"enabled"`
	Mode        string   `json:"mode"`
	Total       int      `json:"total"`
	Active      int      `json:"active"`
	ActiveBaits []string `json:"active_baits"`
	IntervalSec int64    `json:"interval_sec"`
	NextSwitch  int64    `json:"next_switch"`
}

// ExportedRotationState 供管理后台展示用的轮换状态（与判鱼同一套计算）。
func ExportedRotationState(groups [][]string, rotateSec int64, mode string, now time.Time) RotationState {
	return rotationState(baitRotation{groups: groups, rotateSec: rotateSec, mode: mode}, now)
}

// rotationState 按时间桶确定当前生效组：idx = floor(now/interval) % 组数。
// 用时间桶而不是定时器，重启不丢状态，后台展示与判鱼天然一致。
func rotationState(rot baitRotation, now time.Time) RotationState {
	state := RotationState{Mode: rot.mode, Total: len(rot.groups), Active: -1}
	if state.Mode != "strict" {
		state.Mode = "soft"
	}
	state.IntervalSec = rot.rotateSec
	if len(rot.groups) <= 1 || rot.rotateSec < minBaitRotateSec {
		return state
	}
	bucket := now.Unix() / rot.rotateSec
	idx := int(bucket % int64(len(rot.groups)))
	state.Enabled = true
	state.Active = idx
	state.ActiveBaits = rot.groups[idx]
	state.NextSwitch = (bucket + 1) * rot.rotateSec
	return state
}

// activeBaitGroup 返回当前生效组；未启用轮换返回 nil。
func activeBaitGroup(rot baitRotation, now time.Time) []string {
	if state := rotationState(rot, now); state.Enabled {
		return state.ActiveBaits
	}
	return nil
}

// resolveBaitRule 取该鱼种当前生效的规则与轮换配置：
// 有后台覆盖用覆盖，否则回落到编译期内置规则表。
func (s *Server) resolveBaitRule(ctx context.Context, fishZh string) (fishBaitRule, baitRotation) {
	key := ruleName(fishZh)
	if ov, ok := s.baitOverrides(ctx)[key]; ok {
		return ov.rule, ov.rot
	}
	r, ok := fishBaitRules[key]
	if !ok {
		return fishBaitRule{}, baitRotation{}
	}
	return r, baitRotation{}
}

// baitOverrides 读取后台覆盖并缓存；写操作会主动清缓存，这里再兜一层 TTL。
func (s *Server) baitOverrides(ctx context.Context) map[string]baitOverrideEntry {
	s.baitOvMu.RLock()
	if s.baitOvCache != nil && time.Since(s.baitOvAt) < baitOverrideTTL {
		m := s.baitOvCache
		s.baitOvMu.RUnlock()
		return m
	}
	s.baitOvMu.RUnlock()

	s.baitOvMu.Lock()
	defer s.baitOvMu.Unlock()
	if s.baitOvCache != nil && time.Since(s.baitOvAt) < baitOverrideTTL {
		return s.baitOvCache
	}
	m := map[string]baitOverrideEntry{}
	if s.DB != nil {
		rows, err := s.DB.ListFishBaitOverrides(ctx)
		if err != nil {
			s.log.Warn("读取鱼饵规则覆盖失败", "err", err)
		}
		for _, o := range rows {
			if !o.Enabled {
				continue
			}
			rule := fishBaitRule{
				allow:      cleanRuleTerms(o.Allow),
				deny:       cleanRuleTerms(o.Deny),
				bottomOnly: cleanRuleTerms(o.BottomOnly),
				condition:  o.Condition,
			}
			rule.deriveConditionFlags()
			m[ruleName(o.FishZh)] = baitOverrideEntry{
				rule: rule,
				rot: baitRotation{
					groups:    o.Groups,
					rotateSec: o.RotateSec,
					mode:      o.RotateMode,
				},
			}
		}
	}
	s.baitOvCache = m
	s.baitOvAt = time.Now()
	return m
}

// invalidateBaitOverrides 在后台保存/重置后清缓存，让改动立即生效。
func (s *Server) invalidateBaitOverrides() {
	s.baitOvMu.Lock()
	s.baitOvCache = nil
	s.baitOvAt = time.Time{}
	s.baitOvMu.Unlock()
}

// cleanRuleTerms 去掉空白项，避免后台保存的空串影响匹配。
func cleanRuleTerms(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// rotationScoreMultiplier 返回该饵在本轮的权重系数：
// 组内 ×3；启用轮换但不在组内 ×0.5；未启用轮换 ×1。
func rotationScoreMultiplier(active []string, b ruleBait) float64 {
	if active == nil {
		return 1
	}
	if b.matchesAny(active) {
		return baitRotationBoost
	}
	return baitRotationPenalty
}

// rotationKeepsBait 判断 strict 模式下该饵是否还允许使用。
func rotationKeepsBait(active []string, strict bool, b ruleBait) bool {
	if active == nil || !strict {
		return true
	}
	return b.matchesAny(active)
}

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"rf4server/internal/gameserver"
	"rf4server/internal/store"
)

// 覆盖写入上限：避免一次提交过大把判鱼路径拖慢。
const (
	maxBaitRuleTerms  = 40
	maxBaitRuleGroups = 20
	maxBaitGroupTerms = 20
	minBaitRotateSec  = 60
)

type baitRuleSaveReq struct {
	FishZh     string     `json:"fish_zh"`
	Allow      []string   `json:"allow"`
	Deny       []string   `json:"deny"`
	BottomOnly []string   `json:"bottom_only"`
	Condition  string     `json:"condition"`
	Groups     [][]string `json:"groups"`
	RotateSec  int64      `json:"rotate_sec"`
	RotateMode string     `json:"rotate_mode"`
	Enabled    bool       `json:"enabled"`
}

// handleBaitRuleSave 保存一个鱼种的饵料规则覆盖（含多组轮换配置）。
func (s *Server) handleBaitRuleSave(w http.ResponseWriter, r *http.Request) {
	var req baitRuleSaveReq
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "参数错误")
		return
	}
	fishZh := strings.TrimSpace(req.FishZh)
	if fishZh == "" {
		httpError(w, 400, "缺少鱼种中文名")
		return
	}
	if len([]rune(fishZh)) > 40 {
		httpError(w, 400, "鱼种名过长")
		return
	}
	mode := strings.TrimSpace(req.RotateMode)
	if mode == "" {
		mode = "soft"
	}
	if mode != "soft" && mode != "strict" {
		httpError(w, 400, "轮换模式只能是 soft 或 strict")
		return
	}
	groups := cleanGroups(req.Groups)
	if len(groups) > maxBaitRuleGroups {
		httpError(w, 400, "轮换组最多 "+strconv.Itoa(maxBaitRuleGroups)+" 组")
		return
	}
	if len(groups) > 0 {
		if req.RotateSec <= 0 {
			httpError(w, 400, "配置了轮换组时必须填写轮换间隔")
			return
		}
		if req.RotateSec < minBaitRotateSec {
			httpError(w, 400, "轮换间隔至少 60 秒")
			return
		}
	}
	ov := store.FishBaitOverride{
		FishZh:     fishZh,
		Allow:      cleanTerms(req.Allow),
		Deny:       cleanTerms(req.Deny),
		BottomOnly: cleanTerms(req.BottomOnly),
		Condition:  strings.TrimSpace(req.Condition),
		Groups:     groups,
		RotateSec:  req.RotateSec,
		RotateMode: mode,
		Enabled:    req.Enabled,
	}
	if len(ov.Allow) > maxBaitRuleTerms || len(ov.Deny) > maxBaitRuleTerms || len(ov.BottomOnly) > maxBaitRuleTerms {
		httpError(w, 400, "单个列表最多 "+strconv.Itoa(maxBaitRuleTerms)+" 项")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.db.SaveFishBaitOverride(ctx, ov, adminOf(r)); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(ctx, adminOf(r), "bait_rule_save", fishZh,
		"allow="+strconv.Itoa(len(ov.Allow))+" deny="+strconv.Itoa(len(ov.Deny))+
			" groups="+strconv.Itoa(len(groups))+" interval="+strconv.FormatInt(ov.RotateSec, 10)+
			" mode="+mode+" enabled="+strconv.FormatBool(ov.Enabled))
	_ = s.db.QueuePush(ctx, mustJSON(map[string]any{"op": "reload_bait_rules"}))
	writeJSON(w, map[string]any{"ok": true})
}

// handleBaitRuleReset 恢复默认：body 带 fish_zh 删单条，带 all=true 清整表。
func (s *Server) handleBaitRuleReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FishZh string `json:"fish_zh"`
		All    bool   `json:"all"`
	}
	if err := readJSON(r, &req); err != nil {
		req = struct {
			FishZh string `json:"fish_zh"`
			All    bool   `json:"all"`
		}{}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var n int64
	var err error
	target := strings.TrimSpace(req.FishZh)
	if req.All {
		n, err = s.db.DeleteAllFishBaitOverrides(ctx)
		target = "*"
	} else {
		if target == "" {
			httpError(w, 400, "缺少鱼种中文名")
			return
		}
		n, err = s.db.DeleteFishBaitOverride(ctx, target)
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(ctx, adminOf(r), "bait_rule_reset", target,
		"rows="+strconv.FormatInt(n, 10))
	_ = s.db.QueuePush(ctx, mustJSON(map[string]any{"op": "reload_bait_rules"}))
	writeJSON(w, map[string]any{"ok": true, "removed": n})
}

// 饵料/拟饵的二级分类：直接取客户端枚举（饵料 baitType、拟饵 lureType），
// 中文名按静态库里该枚举的代表物品归纳，用于后台「先选分类再选饵」。
var baitTypeLabels = map[int64]string{
	1: "香煮饵 / 贻贝", 2: "虫类", 3: "蛙类", 4: "活饵 / 饵鱼",
	6: "蚯蚓类", 7: "昆虫类", 8: "Pop-up 浮水饵", 9: "鱼子 / 虾类",
	10: "水果 / 蟹类", 11: "坚果", 12: "钩虾", 13: "面团类",
	14: "鱼片", 15: "死饵鱼", 16: "海钓饵", 17: "Zig 浮水球",
}

var lureTypeLabels = map[int64]string{
	1: "旋转亮片", 2: "莫斯科路亚", 3: "泡沫 / 软饵", 4: "Crank 摇摆",
	5: "Popper 波扒", 6: "Pilker 铁板", 7: "Jerk 抽停", 8: "Tube 软管",
	9: "W-Stick 软虫", 10: "Mouse 鼠型", 11: "Fly 飞蝇", 12: "大号软饵",
	13: "Gummi 软鳗", 14: "Octopus 软饵", 15: "虾型", 16: "Flying worm 飞虫",
	17: "鱼卵珠", 18: "Jig 铅头", 19: "Flip Jig 铁板", 20: "Vibra 震动",
}

func baitSubLabel(category, sub int64) string {
	switch category {
	case 27:
		if s, ok := baitTypeLabels[sub]; ok {
			return s
		}
	case 17:
		if s, ok := lureTypeLabels[sub]; ok {
			return s
		}
	}
	return "其他"
}

func baitCategoryLabel(category int64) string {
	if category == 17 {
		return "拟饵"
	}
	return "饵料"
}

type baitCatalogEntry struct {
	ItemID   string `json:"item_id"`
	NameZh   string `json:"name_zh"`
	Category int64  `json:"category"`
	Sub      int64  `json:"sub"`
	SubLabel string `json:"sub_label"`
}

// loadBaitCatalog 读取全部饵料/拟饵及其二级分类，供选择器与分类树使用。
func (s *Server) loadBaitCatalog(ctx context.Context) ([]baitCatalogEntry, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT d.item_id, COALESCE(c.name_zh,c.name,''), COALESCE(d.category,0), COALESCE(d.attrs_json,'{}')
		FROM item_def d LEFT JOIN item_catalog c ON c.item_id=d.item_id
		WHERE d.category IN (17,27)
		GROUP BY d.item_id, d.category
		ORDER BY COALESCE(c.name_zh,c.name,''), d.item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type attrs struct {
		BaitType int64 `json:"fopbhlmkcpe"`
		LureType int64 `json:"mbemakijdeh"`
	}
	out := make([]baitCatalogEntry, 0, 4096)
	for rows.Next() {
		var e baitCatalogEntry
		var raw string
		if err := rows.Scan(&e.ItemID, &e.NameZh, &e.Category, &raw); err != nil {
			return nil, err
		}
		var a attrs
		_ = json.Unmarshal([]byte(raw), &a)
		if e.Category == 17 {
			e.Sub = a.LureType
		} else {
			e.Sub = a.BaitType
		}
		e.SubLabel = baitSubLabel(e.Category, e.Sub)
		out = append(out, e)
	}
	return out, rows.Err()
}

// handleBaitCategories 返回「大类 → 二级分类」树，带每种分类的数量。
func (s *Server) handleBaitCategories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	all, err := s.loadBaitCatalog(ctx)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	type child struct {
		Sub   int64  `json:"sub"`
		Label string `json:"label"`
		Count int    `json:"count"`
	}
	type node struct {
		Key      int64   `json:"key"`
		Label    string  `json:"label"`
		Count    int     `json:"count"`
		Children []child `json:"children"`
	}
	counts := map[int64]map[int64]int{}
	totals := map[int64]int{}
	for _, e := range all {
		if counts[e.Category] == nil {
			counts[e.Category] = map[int64]int{}
		}
		counts[e.Category][e.Sub]++
		totals[e.Category]++
	}
	nodes := make([]node, 0, 2)
	for _, cat := range []int64{27, 17} {
		n := node{Key: cat, Label: baitCategoryLabel(cat), Count: totals[cat]}
		subs := make([]int64, 0, len(counts[cat]))
		for sub := range counts[cat] {
			subs = append(subs, sub)
		}
		sort.Slice(subs, func(i, j int) bool { return subs[i] < subs[j] })
		for _, sub := range subs {
			n.Children = append(n.Children, child{Sub: sub, Label: baitSubLabel(cat, sub), Count: counts[cat][sub]})
		}
		nodes = append(nodes, n)
	}
	writeJSON(w, map[string]any{"categories": nodes})
}

// handleBaitItems 给规则编辑器提供可选饵料清单（只读），支持按大类/二级分类/关键字过滤。
func (s *Server) handleBaitItems(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	cat, _ := strconv.ParseInt(r.URL.Query().Get("cat"), 10, 64)
	sub, _ := strconv.ParseInt(r.URL.Query().Get("sub"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	all, err := s.loadBaitCatalog(ctx)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	lower := strings.ToLower(q)
	out := make([]baitCatalogEntry, 0, limit)
	for _, e := range all {
		if cat != 0 && e.Category != cat {
			continue
		}
		if sub != 0 && e.Sub != sub {
			continue
		}
		if lower != "" &&
			!strings.Contains(strings.ToLower(e.NameZh), lower) &&
			!strings.Contains(strings.ToLower(e.ItemID), lower) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, map[string]any{"rows": out, "total": len(all)})
}

// baitRulesEnabled 规则开关是否打开：关闭时规则不参与判鱼，后台需要提示。
func (s *Server) baitRulesEnabled(ctx context.Context) bool {
	var v string
	if err := s.db.SQL.QueryRowContext(ctx, `SELECT value FROM tunables WHERE key='fish_bait_rules_enabled'`).Scan(&v); err != nil {
		return false
	}
	return strings.TrimSpace(v) == "1"
}

// rotationView 是后台展示用的轮换状态，与判鱼共用同一套时间桶算法。
func rotationView(o store.FishBaitOverride, now time.Time) gameserver.RotationState {
	return gameserver.ExportedRotationState(o.Groups, o.RotateSec, o.RotateMode, now)
}

func cleanTerms(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func cleanGroups(in [][]string) [][]string {
	out := make([][]string, 0, len(in))
	for _, g := range in {
		row := cleanTerms(g)
		if len(row) > maxBaitGroupTerms {
			row = row[:maxBaitGroupTerms]
		}
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}

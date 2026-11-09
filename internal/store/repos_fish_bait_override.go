package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// FishBaitOverride 是管理后台对某个鱼种饵料规则的覆盖。
// 存在且 enabled=1 时整体覆盖编译期内置规则；删除记录即恢复默认。
type FishBaitOverride struct {
	FishZh     string     `json:"fish_zh"`
	Allow      []string   `json:"allow"`
	Deny       []string   `json:"deny"`
	BottomOnly []string   `json:"bottom_only"`
	Condition  string     `json:"condition"`
	Groups     [][]string `json:"groups"`
	RotateSec  int64      `json:"rotate_sec"`
	RotateMode string     `json:"rotate_mode"`
	Enabled    bool       `json:"enabled"`
	UpdatedBy  string     `json:"updated_by"`
	UpdatedTs  float64    `json:"updated_ts"`
}

// ListFishBaitOverrides 读取全部覆盖记录，供后台与判鱼缓存使用。
func (d *DB) ListFishBaitOverrides(ctx context.Context) ([]FishBaitOverride, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT fish_zh,allow_json,deny_json,bottom_json,condition,
		       groups_json,rotate_sec,rotate_mode,enabled,updated_by,updated_ts
		FROM fish_bait_override ORDER BY fish_zh`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FishBaitOverride, 0, 64)
	for rows.Next() {
		var o FishBaitOverride
		var allow, deny, bottom, groups string
		var enabled int
		if err := rows.Scan(&o.FishZh, &allow, &deny, &bottom, &o.Condition,
			&groups, &o.RotateSec, &o.RotateMode, &enabled, &o.UpdatedBy, &o.UpdatedTs); err != nil {
			return nil, err
		}
		o.Allow = decodeStringList(allow)
		o.Deny = decodeStringList(deny)
		o.BottomOnly = decodeStringList(bottom)
		o.Groups = decodeStringGroups(groups)
		o.Enabled = enabled != 0
		out = append(out, o)
	}
	return out, rows.Err()
}

// SaveFishBaitOverride 写入或替换一个鱼种的覆盖。
func (d *DB) SaveFishBaitOverride(ctx context.Context, o FishBaitOverride, by string) error {
	mode := strings.TrimSpace(o.RotateMode)
	if mode != "strict" {
		mode = "soft"
	}
	if o.RotateSec < 0 {
		o.RotateSec = 0
	}
	enabled := int64(0)
	if o.Enabled {
		enabled = 1
	}
	_, err := d.SQL.ExecContext(ctx, `
		INSERT INTO fish_bait_override(fish_zh,allow_json,deny_json,bottom_json,condition,
			groups_json,rotate_sec,rotate_mode,enabled,updated_by,updated_ts)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(fish_zh) DO UPDATE SET
			allow_json=excluded.allow_json, deny_json=excluded.deny_json,
			bottom_json=excluded.bottom_json, condition=excluded.condition,
			groups_json=excluded.groups_json, rotate_sec=excluded.rotate_sec,
			rotate_mode=excluded.rotate_mode, enabled=excluded.enabled,
			updated_by=excluded.updated_by, updated_ts=excluded.updated_ts`,
		o.FishZh,
		encodeStringList(o.Allow), encodeStringList(o.Deny), encodeStringList(o.BottomOnly),
		strings.TrimSpace(o.Condition),
		encodeStringGroups(o.Groups), o.RotateSec, mode, enabled, by, float64(time.Now().Unix()))
	return err
}

// DeleteFishBaitOverride 删除一个鱼种的覆盖（恢复默认）。
func (d *DB) DeleteFishBaitOverride(ctx context.Context, fishZh string) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `DELETE FROM fish_bait_override WHERE fish_zh=?`, fishZh)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteAllFishBaitOverrides 删除全部覆盖（整表恢复默认）。
func (d *DB) DeleteAllFishBaitOverrides(ctx context.Context) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `DELETE FROM fish_bait_override`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func encodeStringList(v []string) string {
	clean := make([]string, 0, len(v))
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

func decodeStringList(raw string) []string {
	out := []string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func encodeStringGroups(v [][]string) string {
	clean := make([][]string, 0, len(v))
	for _, group := range v {
		row := make([]string, 0, len(group))
		for _, s := range group {
			if s = strings.TrimSpace(s); s != "" {
				row = append(row, s)
			}
		}
		if len(row) > 0 {
			clean = append(clean, row)
		}
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

func decodeStringGroups(raw string) [][]string {
	out := [][]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = [][]string{}
	}
	return out
}

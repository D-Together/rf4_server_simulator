package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

// 后台规则覆盖：保存 → 列表反映覆盖与轮换状态 → 恢复默认回到内置规则。
func TestBaitRuleSaveAndReset(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "admin123"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh,in_map_roster,in_habitat) VALUES('铜湖','c.roach','常见拟鲤',1,1)`,
		`INSERT INTO tunables(key,value) VALUES('fish_bait_rules_enabled','1')`,
		`INSERT INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(999901,'rule_test_worm',27,'{"fopbhlmkcpe":6}')`,
		`INSERT INTO item_catalog(item_id,name_zh) VALUES('rule_test_worm','测试蚯蚓')`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	cfg := config.Admin{HTTPAddr: "127.0.0.1:0", JWTSecret: "test"}
	srv := NewServer(cfg, db, logging.Discard(), WebFS())
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	base := "http://" + srv.Addr()
	client := &http.Client{Timeout: 5 * time.Second}

	do := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		r, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	code, login := do("POST", "/api/login", `{"username":"admin","password":"admin123"}`)
	if code != 200 || login["token"] == nil {
		t.Fatalf("登录失败: %d %v", code, login)
	}
	auth := login["token"].(string)
	doAuth := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		r, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+auth)
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	findRow := func(out map[string]any) map[string]any {
		rows, _ := out["rows"].([]any)
		for _, it := range rows {
			m := it.(map[string]any)
			if m["name_zh"] == "常见拟鲤" {
				return m
			}
		}
		t.Fatalf("列表里没有目标鱼: %v", out)
		return nil
	}

	// 保存一条覆盖 + 两组轮换（strict）。
	payload := `{"fish_zh":"常见拟鲤","allow":["测试蚯蚓"],"deny":["拟饵"],
		"bottom_only":[],"condition":"","groups":[["测试蚯蚓"],["红虫"]],
		"rotate_sec":600,"rotate_mode":"strict","enabled":true}`
	if code, out := doAuth("PUT", "/api/bait/rules", payload); code != 200 {
		t.Fatalf("保存失败: %d %v", code, out)
	}

	code, list := doAuth("GET", "/api/bait/prefs?q=c.roach", "")
	if code != 200 {
		t.Fatalf("列表失败: %d", code)
	}
	if list["rules_enabled"] != true {
		t.Fatalf("rules_enabled 应为 true: %v", list["rules_enabled"])
	}
	row := findRow(list)
	if row["source"] != "override" {
		t.Fatalf("source=%v", row["source"])
	}
	if allow, _ := row["allow"].([]any); len(allow) != 1 || allow[0] != "测试蚯蚓" {
		t.Fatalf("allow=%v", row["allow"])
	}
	rot, _ := row["rotation"].(map[string]any)
	if rot == nil || rot["enabled"] != true || rot["mode"] != "strict" {
		t.Fatalf("rotation=%v", row["rotation"])
	}
	if active, _ := rot["active_baits"].([]any); len(active) != 1 {
		t.Fatalf("当前组应有 1 个饵: %v", rot["active_baits"])
	}

	// 非法间隔应被拒绝。
	bad := `{"fish_zh":"常见拟鲤","allow":["测试蚯蚓"],"groups":[["测试蚯蚓"]],"rotate_sec":30,"rotate_mode":"soft","enabled":true}`
	if code, _ := doAuth("PUT", "/api/bait/rules", bad); code != 400 {
		t.Fatalf("间隔 30 秒应返回 400，实得 %d", code)
	}

	// 恢复默认。
	if code, out := doAuth("POST", "/api/bait/rules/reset", `{"fish_zh":"常见拟鲤"}`); code != 200 {
		t.Fatalf("恢复默认失败: %d %v", code, out)
	}
	_, list = doAuth("GET", "/api/bait/prefs?q=c.roach", "")
	row = findRow(list)
	if row["source"] != "default" {
		t.Fatalf("恢复后 source=%v", row["source"])
	}
	allow, _ := row["allow"].([]any)
	if len(allow) < 2 {
		t.Fatalf("恢复后应回到内置允许列表，实得 %v", row["allow"])
	}
	rot, _ = row["rotation"].(map[string]any)
	if rot != nil && rot["enabled"] == true {
		t.Fatalf("恢复后不应再有轮换: %v", rot)
	}
}

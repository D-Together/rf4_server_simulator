package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

func TestAdminExtendedAPI(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "admin123"); err != nil {
		t.Fatal(err)
	}
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(9001,'worm_01',27,'bait','','{}')`)
	mustExec(`INSERT INTO item_catalog(item_id,name,name_zh,kind,numeric_id) VALUES('worm_01','Worm','红虫','bait',9001)`)
	mustExec(`INSERT INTO item(guid,uid,item_id,numeric_id,wire_class,type_id,container,slot,qty,fields_json,durability,workmanship) VALUES('item-a',?,'worm_01',9001,'kbnbcppjkaf',100,'backpack',1,8,'[]',1,1)`, uid)
	mustExec(`INSERT INTO keepnet_items(echo_guid,uid,species,name_zh,weight_g,length_m,cat,keep_loc,keep_spot,keep_bait,keep_level,ts) VALUES('fish-a',?,'c.roach','蟑螂',400,0.2,2,'loc_test',1,'worm_01','level_test',1)`, uid)
	mustExec(`INSERT INTO skill_cat_def(cat_id,ord,title_key,category_kind) VALUES(1,0,'',1)`)
	mustExec(`INSERT INTO skill(uid,skill_id,points) VALUES(?,1,12.5)`, uid)
	mustExec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('store_test','worm_01',100,0,50)`)
	mustExec(`INSERT INTO species_weight(fish_key,min_g,max_g,trophy_g,length_k,length_p) VALUES('c.roach',50,1000,300,0.0355,0.3333)`)
	mustExec(`INSERT INTO loc_fish(loc,fish_key,name_zh,catches) VALUES('loc_test','c.roach','蟑螂',80)`)
	mustExec(`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh,in_map_roster,in_habitat) VALUES('loc_test','c.roach','蟑螂',1,1)`)

	cfg := config.Admin{HTTPAddr: "127.0.0.1:0", JWTSecret: "test"}
	srv := NewServer(cfg, db, logging.Discard(), WebFS())
	errCh, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	base := "http://" + srv.Addr()
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path, token, body string) (int, string) {
		t.Helper()
		r, _ := http.NewRequest(method, base+path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out bytes.Buffer
		_, _ = out.ReadFrom(resp.Body)
		return resp.StatusCode, out.String()
	}
	code, body := call("POST", "/api/login", "", `{"username":"admin","password":"admin123"}`)
	if code != 200 {
		t.Fatalf("login: %d %s", code, body)
	}
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal([]byte(body), &login)

	code, body = call("GET", "/api/players/detail?uid=1000001", login.Token, "")
	if code != 200 || !strings.Contains(body, `"item_count":1`) || !strings.Contains(body, `"fish-a"`) || !strings.Contains(body, `"skills"`) {
		t.Fatalf("detail: %d %s", code, body)
	}

	if code, body = call("POST", "/api/players/skills/update", login.Token,
		`{"uid":1000001,"skill_id":1,"points":177.5}`); code != 200 {
		t.Fatalf("skill: %d %s", code, body)
	}
	pts, _ := db.GetSkillPoints(ctx, uid, 1)
	if pts != 100 {
		t.Fatalf("skill points=%v", pts)
	}
	assertQueueContains(t, db, "refresh_skills")

	code, body = call("GET", "/api/stores", login.Token, "")
	if code != 200 || !strings.Contains(body, "store_test") {
		t.Fatalf("stores: %d %s", code, body)
	}
	if code, body = call("POST", "/api/stores/items/update", login.Token,
		`{"store_id":"store_test","item_id":"worm_01","silver":120,"gold":2,"stock":7}`); code != 200 {
		t.Fatalf("store update: %d %s", code, body)
	}
	price, err := db.ShopItemGet(ctx, "store_test", "worm_01")
	if err != nil || price.Stock != 7 || price.Silver != 120 || price.Gold != 2 {
		t.Fatalf("effective price=%+v err=%v", price, err)
	}
	if code, body = call("POST", "/api/stores/items/reset", login.Token,
		`{"store_id":"store_test","item_id":"worm_01"}`); code != 200 {
		t.Fatalf("store reset: %d %s", code, body)
	}
	price, _ = db.ShopItemGet(ctx, "store_test", "worm_01")
	if price.Stock != 50 || price.Silver != 100 || price.Gold != 0 {
		t.Fatalf("静态货架恢复失败: %+v", price)
	}

	code, body = call("GET", "/api/fish/config?loc=loc_test", login.Token, "")
	if code != 200 || !strings.Contains(body, "c.roach") {
		t.Fatalf("fish config: %d %s", code, body)
	}
	saveFish := `{"area":"loc_test","setting":{"bite_probability":0.25,"bite_delay_fixed":1.5,"bite_delay_scale":0.8,"bite_delay_max":9,"bite_delay_lure_max":4},"fish":[{"fish_key":"c.roach","name_zh":"蟑螂","base_weight":80,"weight":12,"enabled":true}]}`
	if code, body = call("POST", "/api/fish/config", login.Token, saveFish); code != 200 {
		t.Fatalf("fish save: %d %s", code, body)
	}
	setting, err := db.GetFishBiteSetting(ctx, "loc_test")
	if err != nil || !setting.HasOverride || setting.BiteProbability != 0.25 || setting.BiteDelayFixed != 1.5 {
		t.Fatalf("setting=%+v err=%v", setting, err)
	}
	assertQueueContains(t, db, "reload_tunables")
	if code, body = call("POST", "/api/fish/config/reset", login.Token, `{"area":"loc_test"}`); code != 200 {
		t.Fatalf("fish reset: %d %s", code, body)
	}
	setting, _ = db.GetFishBiteSetting(ctx, "loc_test")
	if setting.HasOverride {
		t.Fatalf("鱼口覆盖未清除: %+v", setting)
	}
	assertQueueContains(t, db, "reload_tunables")

	if code, body = call("POST", "/api/players/items/delete", login.Token,
		`{"uid":1000001,"guids":["item-a"]}`); code != 200 {
		t.Fatalf("item delete: %d %s", code, body)
	}
	assertQueueContains(t, db, "delete_item")
	if code, body = call("POST", "/api/players/keepnet/add", login.Token,
		`{"uid":1000001,"species":"c.roach","weight_g":700,"cat":3,"keep_loc":"loc_test","keep_level":"level_test","keep_bait":"worm_01"}`); code != 200 {
		t.Fatalf("keepnet add: %d %s", code, body)
	}
	assertQueueContains(t, db, "add_keepnet")
	if code, body = call("POST", "/api/players/keepnet/delete", login.Token,
		`{"uid":1000001,"guids":["fish-a"]}`); code != 200 {
		t.Fatalf("keepnet delete: %d %s", code, body)
	}
	assertQueueContains(t, db, "delete_keepnet")
	if code, body = call("POST", "/api/players/mail/delete", login.Token,
		`{"uid":1000001,"ids":[2]}`); code != 200 {
		t.Fatalf("mail delete: %d %s", code, body)
	}
	assertQueueContains(t, db, "delete_mail")
	if code, body = call("POST", "/api/mail/send", login.Token,
		`{"uid":1000001,"text":"补偿","items":[{"item_id":"worm_01","qty":3}]}`); code != 200 {
		t.Fatalf("mail send: %d %s", code, body)
	}
	assertQueueContains(t, db, "send_mail")

	code, body = call("GET", "/api/audit?limit=30", login.Token, "")
	if code != 200 || !strings.Contains(body, "skill_proficiency") || !strings.Contains(body, "fish_config") {
		t.Fatalf("audit: %d %s", code, body)
	}
	select {
	case err := <-errCh:
		t.Fatalf("server exited: %v", err)
	default:
	}
}

func assertQueueContains(t *testing.T, db interface {
	QueuePop(context.Context) (string, bool, error)
}, needle string) {
	t.Helper()
	raw, ok, err := db.QueuePop(context.Background())
	if err != nil || !ok || !strings.Contains(raw, needle) {
		t.Fatalf("queue needle=%q got=%q ok=%v err=%v", needle, raw, ok, err)
	}
}

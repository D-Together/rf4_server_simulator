package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

// mailTestAPI 建一套最小可发件环境，并返回一个已登录的请求函数。
// 礼盒用真实静态库同形的 attrs（ieopbeejfmc=true + 内含物），
// 因为校验判据就是 item_def 的真实定义，不能靠假数据蒙混。
func mailTestAPI(t *testing.T) (srv *Server, token func() string, call func(method, path, body string) (int, string)) {
	t.Helper()
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "admin123"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
		t.Fatal(err)
	}
	seed := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json)
		VALUES(9001,'worm_01',27,'bait','','{}')`)
	seed(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json)
		VALUES(9002,'AV_shamp1_pack_6',55,'bundle','',
		'{"ieopbeejfmc":true,"nbkhfhenbag":[{"cemjmlpafmc":"AV_shamp_1l","ljnnpokapig":6}]}')`)
	seed(`INSERT OR REPLACE INTO item_catalog(item_id,name,name_zh,kind,numeric_id)
		VALUES('worm_01','Worm','红虫','bait',9001)`)
	seed(`INSERT OR REPLACE INTO item_catalog(item_id,name,name_zh,kind,numeric_id)
		VALUES('AV_shamp1_pack_6','Champagne box','香槟礼盒','bundle',9002)`)

	cfg := config.Admin{HTTPAddr: "127.0.0.1:0", JWTSecret: "test"}
	srv = NewServer(cfg, db, logging.Discard(), WebFS())

	call = func(method, path, body string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		r.Header.Set("Content-Type", "application/json")
		if token != nil {
			if tok := token(); tok != "" {
				r.Header.Set("Authorization", "Bearer "+tok)
			}
		}
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	code, body := call("POST", "/api/login", `{"username":"admin","password":"admin123"}`)
	if code != 200 {
		t.Fatalf("登录失败: %d %s", code, body)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &login); err != nil {
		t.Fatal(err)
	}
	token = func() string { return login.Token }

	// 清空队列，确保后面的断言只看本次请求投递了什么。
	for {
		_, ok, _ := db.QueuePop(ctx)
		if !ok {
			break
		}
	}
	return srv, token, call
}

func (s *Server) queueEmptyForTest() bool {
	_, ok, _ := s.db.QueuePop(context.Background())
	return !ok
}

// mailSend 发一封邮件并返回状态码与响应体。
func mailSend(t *testing.T, call func(method, path, body string) (int, string), body string) (int, string) {
	t.Helper()
	return call("POST", "/api/mail/send", body)
}

// TestMailSendRejectsOverCapBundle 复现缺陷现场的发送侧：
// AV_shamp1_pack_6 × 99999 必须在入队前被拒，而不是"已下发"后玩家永远领不出来。
func TestMailSendRejectsOverCapBundle(t *testing.T) {
	srv, _, call := mailTestAPI(t)
	code, body := mailSend(t, call,
		`{"uid":1000001,"text":"活动奖励","items":[{"item_id":"AV_shamp1_pack_6","qty":99999}]}`)
	if code != 400 {
		t.Fatalf("超上限包数应 400, 实得 %d: %s", code, body)
	}
	if !strings.Contains(body, "超过单次领取上限") {
		t.Fatalf("错误信息应说明超上限，实得: %s", body)
	}
	if queued(srv) {
		t.Fatal("被拒绝的邮件不应入队")
	}
}

// TestMailSendRejectsUnknownItem 物品不存在时要当场失败，
// 否则会生成一封永远领不出来的邮件。
func TestMailSendRejectsUnknownItem(t *testing.T) {
	srv, _, call := mailTestAPI(t)
	code, body := mailSend(t, call,
		`{"uid":1000001,"text":"测试","items":[{"item_id":"no_such_item","qty":1}]}`)
	if code != 400 {
		t.Fatalf("未知物品应 400, 实得 %d: %s", code, body)
	}
	if !strings.Contains(body, "物品不存在") {
		t.Fatalf("错误信息应说明物品不存在，实得: %s", body)
	}
	if queued(srv) {
		t.Fatal("被拒绝的邮件不应入队")
	}
}

// TestMailSendRejectsBrokenBundleDefinition 组合包定义损坏时也要拦，
// 因为领取路径展开必然失败。
func TestMailSendRejectsBrokenBundleDefinition(t *testing.T) {
	srv, _, call := mailTestAPI(t)
	if _, err := srv.db.SQL.Exec(`UPDATE item_def SET attrs_json='{oops'
		WHERE item_id='AV_shamp1_pack_6'`); err != nil {
		t.Fatal(err)
	}
	code, body := mailSend(t, call,
		`{"uid":1000001,"text":"测试","items":[{"item_id":"AV_shamp1_pack_6","qty":1}]}`)
	if code != 400 {
		t.Fatalf("损坏的组合包定义应 400, 实得 %d: %s", code, body)
	}
	if queued(srv) {
		t.Fatal("被拒绝的邮件不应入队")
	}
}

// TestMailSendAcceptsValidBundle 合法礼盒必须照常发得出去，
// 防止校验把正常发奖链路也拦掉。
func TestMailSendAcceptsValidBundle(t *testing.T) {
	srv, _, call := mailTestAPI(t)
	code, body := mailSend(t, call,
		`{"uid":1000001,"text":"活动奖励","items":[{"item_id":"AV_shamp1_pack_6","qty":1}]}`)
	if code != 200 {
		t.Fatalf("合法礼盒应 200, 实得 %d: %s", code, body)
	}
	raw, ok, err := srv.db.QueuePop(context.Background())
	if err != nil || !ok || !strings.Contains(raw, "send_mail") {
		t.Fatalf("合法邮件应入队: raw=%q ok=%v err=%v", raw, ok, err)
	}
}

// queued 报告队列里是否还有待投递指令。
func queued(srv *Server) bool {
	_, ok, _ := srv.db.QueuePop(context.Background())
	return ok
}

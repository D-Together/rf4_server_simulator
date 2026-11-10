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

// TestMailSendRejectsMissingBundleInnerItem 审查 P2 复现：
// 组合包内含物指向不存在的 item_id 时，发件必须 400。
// 曾经的实现只校验"内含物 ID 非空"，因此这种包能发出去、玩家领取必然失败。
func TestMailSendRejectsMissingBundleInnerItem(t *testing.T) {
	srv, _, call := mailTestAPI(t)
	if _, err := srv.db.SQL.Exec(`UPDATE item_def SET attrs_json =
		'{"ieopbeejfmc":false,"nbkhfhenbag":[{"cemjmlpafmc":"missing_inner","ljnnpokapig":1}]}'
		WHERE item_id='AV_shamp1_pack_6'`); err != nil {
		t.Fatal(err)
	}
	code, body := mailSend(t, call,
		`{"uid":1000001,"text":"测试","items":[{"item_id":"AV_shamp1_pack_6","qty":1}]}`)
	if code != 400 {
		t.Fatalf("内含物不存在应 400, 实得 %d: %s", code, body)
	}
	if !strings.Contains(body, "内含物物品不存在") {
		t.Fatalf("错误信息应指出内含物不存在，实得: %s", body)
	}
	if queued(srv) {
		t.Fatal("被拒绝的邮件不应入队")
	}
}

// TestMailSendRejectsNestedBundle 内含物仍是组合包时，领取路径会拒绝，
// 发件侧也应提前拦下。
func TestMailSendRejectsNestedBundle(t *testing.T) {
	srv, _, call := mailTestAPI(t)
	if _, err := srv.db.SQL.Exec(`UPDATE item_def SET attrs_json =
		'{"ieopbeejfmc":false,"nbkhfhenbag":[{"cemjmlpafmc":"AV_shamp1_pack_6","ljnnpokapig":1}]}'
		WHERE item_id='AV_shamp1_pack_6'`); err != nil {
		t.Fatal(err)
	}
	code, body := mailSend(t, call,
		`{"uid":1000001,"text":"测试","items":[{"item_id":"AV_shamp1_pack_6","qty":1}]}`)
	if code != 400 || !strings.Contains(body, "嵌套") {
		t.Fatalf("嵌套组合包应 400: %d %s", code, body)
	}
	if queued(srv) {
		t.Fatal("被拒绝的邮件不应入队")
	}
}

// TestMailSendRejectsUnclaimableCountableBundle 覆盖领取准备阶段最后几类
// 可计件内含物校验。每一条都曾在发件侧漏检：邮件返回 200 入队，但玩家领取时
// prepareBundleItems 才报数量槽溢出或单位换算错误。
func TestMailSendRejectsUnclaimableCountableBundle(t *testing.T) {
	tests := []struct {
		name       string
		childAttrs string
		bundleArgs string
		want       string
	}{
		{
			name:       "unit_conversion_overflow",
			childAttrs: `{"ljnnpokapig":2147483648}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":2}]`,
			want:       "单位换算溢出",
		},
		{
			name:       "absolute_quantity_slot_limit",
			childAttrs: `{"ljnnpokapig":30}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":4294967296,"mlapkoadfnh":true}]`,
			want:       "数量槽上限",
		},
		{
			name:       "invalid_child_unit_quantity",
			childAttrs: `{"ljnnpokapig":0}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":1}]`,
			want:       "单位量非法",
		},
		{
			name:       "same_item_cumulative_overflow",
			childAttrs: `{"ljnnpokapig":30}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":3000000000,"mlapkoadfnh":true},{"cemjmlpafmc":"worm_01","ljnnpokapig":3000000000,"mlapkoadfnh":true}]`,
			want:       "总量溢出",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, call := mailTestAPI(t)
			if _, err := srv.db.SQL.Exec(`UPDATE item_def SET attrs_json=? WHERE item_id='worm_01'`, tc.childAttrs); err != nil {
				t.Fatal(err)
			}
			bundleAttrs := `{"ieopbeejfmc":false,"nbkhfhenbag":` + tc.bundleArgs + `}`
			if _, err := srv.db.SQL.Exec(`UPDATE item_def SET attrs_json=? WHERE item_id='AV_shamp1_pack_6'`, bundleAttrs); err != nil {
				t.Fatal(err)
			}
			code, body := mailSend(t, call,
				`{"uid":1000001,"text":"测试","items":[{"item_id":"AV_shamp1_pack_6","qty":1}]}`)
			if code != 400 {
				t.Fatalf("不可领取组合包应 400, 实得 %d: %s", code, body)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("错误信息应包含 %q, 实得: %s", tc.want, body)
			}
			if queued(srv) {
				t.Fatal("被拒绝的邮件不应入队")
			}
		})
	}
}

// TestMailSendAcceptsAbsoluteCountableBundle 确保 mlapkoadfnh=true 的合法
// 绝对数量条目不会被默认单位量换算误拒。
func TestMailSendAcceptsAbsoluteCountableBundle(t *testing.T) {
	srv, _, call := mailTestAPI(t)
	if _, err := srv.db.SQL.Exec(`UPDATE item_def SET attrs_json='{"ljnnpokapig":30}' WHERE item_id='worm_01'`); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.SQL.Exec(`UPDATE item_def SET attrs_json='{"ieopbeejfmc":false,"nbkhfhenbag":[{"cemjmlpafmc":"worm_01","ljnnpokapig":100,"mlapkoadfnh":true}]}' WHERE item_id='AV_shamp1_pack_6'`); err != nil {
		t.Fatal(err)
	}
	code, body := mailSend(t, call,
		`{"uid":1000001,"text":"测试","items":[{"item_id":"AV_shamp1_pack_6","qty":1}]}`)
	if code != 200 {
		t.Fatalf("合法绝对数量组合包应 200, 实得 %d: %s", code, body)
	}
	if !queued(srv) {
		t.Fatal("合法邮件应入队")
	}
}

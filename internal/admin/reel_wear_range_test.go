package admin

// 摩擦片磨损参数的区间校验：下限低于 float64 精度门槛的值必须被拒绝，
// 否则参数会静默失效（看着生效、实际一分不磨）。见 specs/changes/reel-wear-tunables。

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

func TestReelWearTunableRangeValidation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "reel-range-test"); err != nil {
		t.Fatal(err)
	}
	s := NewServer(config.Admin{JWTSecret: "reel-range-test"}, db, logging.Discard(), WebFS())
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}

	var login struct {
		Token string `json:"token"`
	}
	w := call("POST", "/api/login", "", `{"username":"admin","password":"reel-range-test"}`)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil || login.Token == "" {
		t.Fatalf("login=%s", w.Body)
	}

	// 小于精度门槛的值必须被拒。1e-21 是第一轮实测能写进去、但实际不磨的值；
	// 1e-13 仍低于 1e-12 下限，一并挡掉。
	for _, value := range []string{"0", "-1", "0.000000000000000000001", "1e-21", "1e-13", "2", "NaN", ""} {
		got := call("POST", "/api/tunables", login.Token, `{"key":"reel_slip_wear_low","value":"`+value+`"}`).Code
		if got != 400 {
			t.Fatalf("磨损率 %q 应被拒绝，实得 %d", value, got)
		}
	}

	// 区间内的值（含默认值和下限本身）必须接受。
	for _, value := range []string{"0.000001", "1e-12", "1"} {
		w = call("POST", "/api/tunables", login.Token, `{"key":"reel_slip_wear_low","value":"`+value+`"}`)
		if w.Code != 200 {
			t.Fatalf("磨损率 %q 应被接受，实得 %d %s", value, w.Code, w.Body)
		}
		if got, ok, err := db.TunableGet(ctx, "reel_slip_wear_low"); err != nil || !ok || got != value {
			t.Fatalf("保存 %q 读到 %q err=%v", value, got, err)
		}
	}

	// 其它磨损参数的边界：校准秒数下限 0.01、上限 3600。
	for _, value := range []string{"0.001", "3601"} {
		if got := call("POST", "/api/tunables", login.Token, `{"key":"reel_slip_calibration_seconds","value":"`+value+`"}`).Code; got != 400 {
			t.Fatalf("校准秒数 %q 应被拒绝，实得 %d", value, got)
		}
	}
	if got := call("POST", "/api/tunables", login.Token, `{"key":"reel_slip_calibration_seconds","value":"0.01"}`).Code; got != 200 {
		t.Fatalf("校准秒数下限 0.01 应被接受，实得 %d", got)
	}

	// 曲线指数下限 0.1：0.05 应被拒。
	if got := call("POST", "/api/tunables", login.Token, `{"key":"reel_locked_burn_curve","value":"0.05"}`).Code; got != 400 {
		t.Fatalf("曲线指数 0.05 应被拒绝，实得 %d", got)
	}

	// 未受区间约束的键不受影响（回归保护：别把校验扩散到别的参数）。
	if got := call("POST", "/api/tunables", login.Token, `{"key":"xp_gain","value":"0.0001"}`).Code; got != 200 {
		t.Fatalf("xp_gain 不在磨损区间表内，应正常保存，实得 %d", got)
	}
}

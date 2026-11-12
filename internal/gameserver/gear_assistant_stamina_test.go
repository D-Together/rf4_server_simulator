package gameserver

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"rf4server/internal/tunnel"
)

// 14/8 完整帧长 138 字节：剩余体力在偏移 45，钓组最弱环强度在偏移 49，都是
// F32 小端。偏移与量纲来自 2026-11-07~12 的 4671 帧实机日志（len=138 未截断）；
// 帧内容在测试里用合成数据填充，不携带抓包原始字节。
const loadFrameLength = 138

func loadFrameWithStamina(t *testing.T, value float32) []byte {
	t.Helper()
	return loadFrame(t, value, 11.0)
}

// loadFrame 合成一帧 14/8：stamina 是剩余体力比例，rigLimit 是钓组最弱环 kgf。
func loadFrame(t *testing.T, stamina, rigLimit float32) []byte {
	t.Helper()
	body := make([]byte, loadFrameLength)
	binary.LittleEndian.PutUint32(body[staminaLoadOffset:], math.Float32bits(stamina))
	binary.LittleEndian.PutUint32(body[staminaRigLimitOffset:], math.Float32bits(rigLimit))
	return body
}

func TestFishStaminaFromLoadReadsOffset45(t *testing.T) {
	start := loadFrameWithStamina(t, 0.99992126)
	got, ok := fishStaminaFromLoad(start)
	if !ok || math.Abs(got-0.99992126) > 1e-6 {
		t.Fatalf("start frame stamina=%v ok=%v", got, ok)
	}
	end := loadFrameWithStamina(t, 0.37131304)
	got, ok = fishStaminaFromLoad(end)
	if !ok || math.Abs(got-0.37131304) > 1e-6 {
		t.Fatalf("end frame stamina=%v ok=%v", got, ok)
	}
	if _, ok := fishStaminaFromLoad(start[:staminaLoadMinBytes-1]); ok {
		t.Fatal("short frame was accepted")
	}
	invalid := append([]byte(nil), start...)
	binary.LittleEndian.PutUint32(invalid[45:49], math.Float32bits(2.0))
	if _, ok := fishStaminaFromLoad(invalid); ok {
		t.Fatal("out-of-range value was accepted")
	}
	binary.LittleEndian.PutUint32(invalid[45:49], math.Float32bits(float32(math.NaN())))
	if _, ok := fishStaminaFromLoad(invalid); ok {
		t.Fatal("NaN was accepted")
	}
}

func TestFishRigLimitFromLoadReadsOffset49(t *testing.T) {
	body := loadFrame(t, 0.5, 11.2176)
	if got, ok := fishRigLimitFromLoad(body); !ok || math.Abs(got-11.2176) > 1e-4 {
		t.Fatalf("rig limit=%v ok=%v", got, ok)
	}
	if _, ok := fishRigLimitFromLoad(body[:staminaRigLimitMinBytes-1]); ok {
		t.Fatal("short frame was accepted")
	}
	for _, bad := range []float32{float32(math.NaN()), 0, -1, 5000} {
		invalid := loadFrame(t, 0.5, bad)
		if _, ok := fishRigLimitFromLoad(invalid); ok {
			t.Fatalf("out-of-range %v was accepted", bad)
		}
	}
}

// 原型：小鱼满体力也拉得动（0%），大鱼出正数，体力耗尽后归零。
func TestFishOverloadPct(t *testing.T) {
	s, _ := newRigTestServer(t)
	limit := 11.0
	cases := []struct {
		name    string
		weightG int64
		stamina float64
		want    string // 期望文本，空表示只判断方向
		zero    bool
	}{
		{"small fish at full stamina is free", 400, 1.0, "", true},
		{"small fish never overloads", 400, 0.3, "", true},
		{"20kg perch at half stamina overloads", 20000, 0.5, "", false},
		{"20kg perch when exhausted is free", 20000, 0.001, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fish := &RolledFish{FishGUID: "f", Species: "perch", WeightG: c.weightG}
			pull := s.fishFightPullKgfFor(fish)
			got, ok := fishOverloadPct(limit, pull, c.stamina)
			if !ok {
				t.Fatal("overload unavailable for a known species")
			}
			if c.zero && got != 0 {
				t.Fatalf("want 0%%, got %.1f%%", got)
			}
			if !c.zero && got <= 0 {
				t.Fatalf("want a positive overload, got %.1f%%", got)
			}
			// 与公式直算一致：体力归零时分子的确归零。
			want := math.Max(0, (c.stamina*pull-limit)/limit*100)
			if math.Abs(got-want) > 1e-6 {
				t.Fatalf("got %.4f, want %.4f", got, want)
			}
		})
	}
	// 分母缺失（未上报钓组最弱环）时不出超载，避免拿 0 当分母。
	if _, ok := fishOverloadPct(0, 20, 1.0); ok {
		t.Fatal("zero rig limit must not produce an overload value")
	}
	// 分子缺失（鱼种无标定拉力）同样不出数。
	if _, ok := fishOverloadPct(11, 0, 1.0); ok {
		t.Fatal("missing pull must not produce an overload value")
	}
}

func TestStaminaReportCommandsAndPersistence(t *testing.T) {
	env := newDLineTestEnv(t, 1000001)
	if _, err := env.db.EnsureProfile(t.Context(), env.sess.Userid); err != nil {
		t.Fatal(err)
	}
	uid := env.sess.Userid
	staminaOn := func() bool {
		t.Helper()
		on, err := readKVFlag(t.Context(), env.db.SQL, staminaReportKey(uid))
		if err != nil {
			t.Fatal(err)
		}
		return on
	}
	lastReply := func() string {
		t.Helper()
		mails, err := env.db.MailList(t.Context(), uid, false)
		if err != nil || len(mails) == 0 || mails[0].Sender != gearAssistantUID {
			t.Fatalf("no assistant reply: %+v %v", mails, err)
		}
		return mails[0].Text
	}
	if staminaOn() {
		t.Fatal("stamina report must default off")
	}
	// 一级菜单同时列出两个开关。
	sendGearAssistantForTest(t, env.srv, env.sess, "菜单")
	if reply := lastReply(); !strings.Contains(reply, "【耐力播报】") || !strings.Contains(reply, "【满红不爆】") {
		t.Fatalf("menu does not list both switches: %q", reply)
	}
	// 发 2 进入二级：显示当前状态，不改设置。
	sendGearAssistantForTest(t, env.srv, env.sess, "2")
	if reply := lastReply(); !strings.Contains(reply, "【当前状态】：关闭") || staminaOn() {
		t.Fatalf("level two menu: %q", reply)
	}
	// 二级 y 开启。
	sendGearAssistantForTest(t, env.srv, env.sess, "y")
	if reply := lastReply(); !staminaOn() || !strings.Contains(reply, "已开启") {
		t.Fatalf("y did not enable: %q", reply)
	}
	// 重复开启幂等。
	sendGearAssistantForTest(t, env.srv, env.sess, "y")
	if reply := lastReply(); !strings.Contains(reply, "已经开启") {
		t.Fatalf("repeat on must be idempotent: %q", reply)
	}
	// h 显示说明，不改设置也不离开二级。
	sendGearAssistantForTest(t, env.srv, env.sess, "h")
	if reply := lastReply(); !strings.Contains(reply, "每 5 秒在聊天框显示当前鱼的耐力与超载") || !staminaOn() {
		t.Fatalf("h lost the description or the setting: %q", reply)
	}
	// n 关闭，重复关闭幂等。
	sendGearAssistantForTest(t, env.srv, env.sess, "n")
	if reply := lastReply(); staminaOn() || !strings.Contains(reply, "已关闭") {
		t.Fatalf("n did not disable: %q", reply)
	}
	sendGearAssistantForTest(t, env.srv, env.sess, "n")
	if reply := lastReply(); !strings.Contains(reply, "已经关闭") {
		t.Fatalf("repeat off must be idempotent: %q", reply)
	}
	// 返回一级后仍支持「序号 子命令」一次性写法。
	sendGearAssistantForTest(t, env.srv, env.sess, "b")
	sendGearAssistantForTest(t, env.srv, env.sess, "2 开")
	if !staminaOn() {
		t.Fatal("level one one-shot command failed")
	}
	if on, err := readKVFlag(t.Context(), env.db.SQL, staminaReportKey(uid+1)); err != nil || on {
		t.Fatalf("setting leaked to another player: %v %v", on, err)
	}
	// 两个开关互不影响：改满红不爆不能顺带改耐力播报。
	sendGearAssistantForTest(t, env.srv, env.sess, "b")
	sendGearAssistantForTest(t, env.srv, env.sess, "1 开")
	if on, err := readOverloadProtection(t.Context(), env.db.SQL, uid); err != nil || !on {
		t.Fatalf("overload switch broken: %v %v", on, err)
	}
	if !staminaOn() {
		t.Fatal("overload command changed stamina setting")
	}
	// 新会话读取已保存设置。
	sendGearAssistantForTest(t, env.srv, env.sess, "b")
	reconnected := &Session{ID: 2, Userid: uid, Srv: env.srv, log: env.sess.log}
	sendGearAssistantForTest(t, env.srv, reconnected, "2")
	if reply := lastReply(); !strings.Contains(reply, "【当前状态】：开启") {
		t.Fatalf("reconnected session lost setting: %q", reply)
	}
}

func TestStaminaReportGatingAndThrottle(t *testing.T) {
	s, _ := newRigTestServer(t)
	sess, peer := newTunnelProbe(t, s)
	ch := collectHookedChat(peer)
	uid := sess.Userid
	rodA, err := s.CreateItem(t.Context(), uid, "spin_10001_S60L", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	rodB, err := s.CreateItem(t.Context(), uid, "spin_10001_S60L", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	stA := s.selectFishingRod(uid, rodA.GUID)
	stB := s.selectFishingRod(uid, rodB.GUID)
	s.selectFishingRod(uid, rodA.GUID) // rodA is the held rod again.
	fish := &RolledFish{FishGUID: newFishGUID(), Species: "perch", WeightG: 400, GearGUID: rodA.GUID}
	stA.mu.Lock()
	stA.GearGuid, stA.Pending, stA.CastActive, stA.InWater, stA.Hooked = rodA.GUID, fish, true, true, true
	stA.mu.Unlock()
	stB.mu.Lock()
	stB.GearGuid, stB.Pending, stB.CastActive, stB.InWater, stB.Hooked = rodB.GUID, fish, true, true, true
	stB.mu.Unlock()

	body := loadFrame(t, 0.99992126, 11.0)
	makeCtx := func(raw []byte) *Ctx {
		c := miscCtx(sess, 14, 8, nil)
		c.Body = raw
		return c
	}
	base := time.Now()
	step := 0
	collect := func(want int, label string) []string {
		t.Helper()
		texts := finishHookedChat(t, sess, ch)
		if len(texts) != want {
			t.Fatalf("%s: got %d chat pushes, want %d: %v", label, len(texts), want, texts)
		}
		for _, text := range texts {
			if !strings.HasPrefix(text, "[耐力] ") {
				t.Fatalf("%s: unexpected push %q", label, text)
			}
		}
		return texts
	}
	advance := func() time.Time {
		step += 6
		return base.Add(time.Duration(step) * time.Second)
	}

	// Switch off: no push even with a valid fighting state and frame.
	s.maybeReportStamina(makeCtx(body), stA, advance())
	collect(0, "switch off")
	sendGearAssistantForTest(t, s, sess, "2 开")
	collect(0, "switch command uses mail, not chat")

	// 0.4kg perch against an 11kgf rig: pullable, so the overload reads 0%.
	s.maybeReportStamina(makeCtx(body), stA, advance())
	if texts := collect(1, "first report"); !strings.Contains(texts[0], "[耐力] 100.0%") ||
		!strings.Contains(texts[0], "[超载] 0%") {
		t.Fatalf("small fish must report a zero overload: %q", texts[0])
	}
	// Same fish within the 5s window: throttled.
	s.maybeReportStamina(makeCtx(loadFrame(t, 0.99992126, 11.0)), stA,
		base.Add(time.Duration(step)*time.Second+time.Second))
	collect(0, "throttled")
	// Same small fish after the window: stamina drops, overload stays at 0.
	s.maybeReportStamina(makeCtx(loadFrame(t, 0.37131304, 11.0)), stA, advance())
	if texts := collect(1, "second report"); !strings.Contains(texts[0], "[耐力] 37.1%") ||
		!strings.Contains(texts[0], "[超载] 0%") {
		t.Fatalf("small fish at 37%% stamina is still free: %q", texts[0])
	}
	// 20kg perch on the same rig: the fish now out-pulls it, so the overload is
	// positive. Expected value is derived from the same calibration the server uses.
	bigFish := &RolledFish{FishGUID: newFishGUID(), Species: "perch", WeightG: 20000}
	wantOver := (0.99992126*s.fishFightPullKgfFor(bigFish) - 11.0) / 11.0 * 100
	stA.mu.Lock()
	stA.Pending = bigFish
	stA.mu.Unlock()
	s.maybeReportStamina(makeCtx(body), stA, base.Add(time.Duration(step)*time.Second+time.Second))
	if texts := collect(1, "big fish"); !strings.Contains(texts[0], fmt.Sprintf("[超载] %.0f%%", wantOver)) {
		t.Fatalf("big fish should overload by %.0f%%: %q", wantOver, texts[0])
	}

	// Non-held rod, non-fighting state and malformed frames are all silent.
	s.maybeReportStamina(makeCtx(body), stB, advance())
	collect(0, "non-held rod")
	stA.mu.Lock()
	stA.Hooked = false
	stA.mu.Unlock()
	s.maybeReportStamina(makeCtx(body), stA, advance())
	collect(0, "not fighting")
	stA.mu.Lock()
	stA.Hooked = true
	stA.mu.Unlock()
	s.maybeReportStamina(makeCtx(body[:40]), stA, advance())
	collect(0, "short frame")

	// 帧里读不到钓组最弱环（50 字节）时只报耐力，不拿 0 当分母。
	s.maybeReportStamina(makeCtx(body[:50]), stA, advance())
	if texts := collect(1, "no rig limit"); strings.Contains(texts[0], "[超载]") {
		t.Fatalf("missing rig limit must suppress the overload column: %q", texts[0])
	}

	// The switch stays off for other players and the report never touches mail.
	if on, err := readKVFlag(t.Context(), s.DB.SQL, staminaReportKey(uid+1)); err != nil || on {
		t.Fatalf("stamina switch leaked: %v %v", on, err)
	}
	mails, err := s.DB.MailList(t.Context(), uid, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mails {
		if strings.HasPrefix(m.Text, "[耐力]") {
			t.Fatal("report was written into the personal mail table")
		}
	}
}

// staminaReportFixture 造一套「手持竿正在搏鱼」的最小环境。
func staminaReportFixture(t *testing.T) (*Server, *Session, *tunnel.Conn, *FishState, string) {
	t.Helper()
	s, _ := newRigTestServer(t)
	sess, peer := newTunnelProbe(t, s)
	uid := sess.Userid
	rod, err := s.CreateItem(t.Context(), uid, "spin_10001_S60L", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	st := s.selectFishingRod(uid, rod.GUID)
	fish := &RolledFish{FishGUID: newFishGUID(), Species: "perch", WeightG: 400, GearGUID: rod.GUID}
	st.mu.Lock()
	st.GearGuid, st.Pending, st.CastActive, st.InWater, st.Hooked = rod.GUID, fish, true, true, true
	st.mu.Unlock()
	return s, sess, peer, st, rod.GUID
}

// 开关缓存：默认关时读一次库就缓存，菜单改设置后立刻失效——关掉即刻停止，
// 不必等到下一个 5 秒窗口，也不会因为缓存而把旧状态沿用下去。
func TestStaminaReportSwitchCacheInvalidation(t *testing.T) {
	s, sess, peer, st, _ := staminaReportFixture(t)
	ch := collectHookedChat(peer)
	body := loadFrame(t, 0.5, 11.0)
	makeCtx := func(ss *Session) *Ctx {
		c := miscCtx(ss, 14, 8, nil)
		c.Body = body
		return c
	}
	now := time.Now()

	s.maybeReportStamina(makeCtx(sess), st, now)
	st.mu.Lock()
	cached, on := st.staminaFlagCached, st.staminaFlagOn
	st.mu.Unlock()
	if !cached || on {
		t.Fatalf("switch off must be cached as off: cached=%v on=%v", cached, on)
	}
	if texts := finishHookedChat(t, sess, ch); len(texts) != 0 {
		t.Fatalf("switch off pushed %v", texts)
	}

	sendGearAssistantForTest(t, s, sess, "2 开")
	s.maybeReportStamina(makeCtx(sess), st, now)
	if texts := finishHookedChat(t, sess, ch); len(texts) != 1 {
		t.Fatalf("enabling did not invalidate the cache: %v", texts)
	}

	sendGearAssistantForTest(t, s, sess, "b")
	sendGearAssistantForTest(t, s, sess, "2 关")
	s.maybeReportStamina(makeCtx(sess), st, now.Add(10*time.Second))
	if texts := finishHookedChat(t, sess, ch); len(texts) != 0 {
		t.Fatalf("disabling did not invalidate the cache: %v", texts)
	}
}

// 重连不重复播报：竿状态与节流标记都挂在服务端按 uid 的 FishState 上，
// 新会话复用同一对象，同一尾鱼不会因为重连再播一次。
func TestStaminaReportSurvivesReconnect(t *testing.T) {
	s, sess, peer, st, _ := staminaReportFixture(t)
	ch := collectHookedChat(peer)
	body := loadFrame(t, 0.5, 11.0)
	makeCtx := func(ss *Session) *Ctx {
		c := miscCtx(ss, 14, 8, nil)
		c.Body = body
		return c
	}
	now := time.Now()
	sendGearAssistantForTest(t, s, sess, "2 开")
	s.maybeReportStamina(makeCtx(sess), st, now)
	if texts := finishHookedChat(t, sess, ch); len(texts) != 1 {
		t.Fatalf("first fight frame must report: %v", texts)
	}

	reconnected := &Session{ID: 2, Userid: sess.Userid, Srv: s, log: sess.log, tunnel: sess.tunnel}
	if got := s.fishingState(sess.Userid); got != st {
		t.Fatal("reconnect rebuilt the rod state instead of reusing it")
	}
	s.maybeReportStamina(makeCtx(reconnected), st, now.Add(time.Second))
	if texts := finishHookedChat(t, sess, ch); len(texts) != 0 {
		t.Fatalf("reconnect repeated the report for the same fish: %v", texts)
	}
	// 过了窗口按正常节拍继续，属于节流后的下一拍，不是重连重复。
	s.maybeReportStamina(makeCtx(reconnected), st, now.Add(6*time.Second))
	if texts := finishHookedChat(t, sess, ch); len(texts) != 1 {
		t.Fatalf("next window after reconnect was lost: %v", texts)
	}
}

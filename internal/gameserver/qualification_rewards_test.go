package gameserver

import (
	"context"
	"strings"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// completeQualificationTasks 把指定任务直接写成已完成（测试预填进度）。
func completeQualificationTasks(t *testing.T, db *store.DB, uid int64, ids []int64) {
	t.Helper()
	for _, id := range ids {
		if err := db.UpsertQualificationState(context.Background(), uid,
			store.QualificationState{RuleID: id, CompletedTS: 1794000000}); err != nil {
			t.Fatalf("预填任务 %d 失败: %v", id, err)
		}
	}
}

func qualificationBranchRuleIDs(branch int64, skip func(int64) bool) []int64 {
	var ids []int64
	for _, r := range qualificationRules {
		if r.ID/1000 == branch && (skip == nil || !skip(r.ID)) {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func countPlayerCups(t *testing.T, db *store.DB, uid int64, itemID string) int {
	t.Helper()
	var n int
	if err := db.SQL.QueryRow(
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=? AND container='house' AND wire_class='enikeammfen' AND type_id=2015`,
		uid, itemID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 分支 1 只差 1002 时，一尾 ≥3kg 纺车竿鱼达标 1002 → 整分支完成发
// qual_spin_cup；再次巡检/重复结算不重发（kv 幂等）。
func TestQualificationCupOnBranchComplete(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	c := context.Background()

	ids := qualificationBranchRuleIDs(1, nil)
	if len(ids) != 21 {
		t.Fatalf("分支 1 应有 21 条任务，实得 %d", len(ids))
	}
	var rest []int64
	for _, id := range ids {
		if id != 1002 {
			rest = append(rest, id)
		}
	}
	completeQualificationTasks(t, db, uid, rest)

	// 差一条时巡检不发杯。
	if cups := srv.ensureQualificationCups(c, uid); len(cups) != 0 {
		t.Fatalf("分支未集齐不应发杯，实得 %d", len(cups))
	}

	ev := qualificationEvent{Kind: qevFishLanded, Species: "perch", WeightG: 3500, LengthM: .4, RodType: rodTypeSpinning, CatchGUID: "0123456789abcdef0123456789abcdef"}
	changed := srv.evaluateQualifications(ctx, ev)
	if changed[1002].CompletedTS == 0 {
		t.Fatalf("1002 应达标完成，实得 %v", changed[1002])
	}
	cups := srv.ensureQualificationCups(c, uid)
	if len(cups) != 1 || cups[0].ItemID != "qual_spin_cup" {
		t.Fatalf("应发 qual_spin_cup 一杯，实得 %v", cups)
	}
	if n := countPlayerCups(t, db, uid, "qual_spin_cup"); n != 1 {
		t.Fatalf("house 里应有 1 个 qual_spin_cup，实得 %d", n)
	}
	// 幂等：再巡检、再结算同尾鱼都不重发。
	if cups := srv.ensureQualificationCups(c, uid); len(cups) != 0 {
		t.Fatalf("重复巡检不应重发，实得 %d", len(cups))
	}
	srv.evaluateQualifications(ctx, ev)
	if cups := srv.ensureQualificationCups(c, uid); len(cups) != 0 {
		t.Fatalf("重复结算后不应重发，实得 %d", len(cups))
	}
	if n := countPlayerCups(t, db, uid, "qual_spin_cup"); n != 1 {
		t.Fatalf("幂等后仍应只有 1 杯，实得 %d", n)
	}
}

// 8 个永久分支全部完成 → 8 个分支杯 + qual_total_cup 总杯。
func TestQualificationTotalCup(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := int64(1000001)
	c := context.Background()

	var all []int64
	for branch := int64(1); branch <= qualificationBranches; branch++ {
		all = append(all, qualificationBranchRuleIDs(branch, nil)...)
	}
	completeQualificationTasks(t, db, uid, all)
	cups := srv.ensureQualificationCups(c, uid)
	if len(cups) != qualificationBranches+1 {
		t.Fatalf("应发 8 分支杯 + 1 总杯，实得 %d", len(cups))
	}
	if n := countPlayerCups(t, db, uid, "qual_total_cup"); n != 1 {
		t.Fatalf("应有 qual_total_cup，实得 %d", n)
	}
	if again := srv.ensureQualificationCups(c, uid); len(again) != 0 {
		t.Fatalf("重复巡检不应重发，实得 %d", len(again))
	}
}

// 含缺口任务的活动分支：缺口任务不下发，按非缺口任务全完成即可发杯。
// 分支 19（IX 九周年）有 19101/19103/19202/19301/19603 五条缺口。
func TestQualificationCupSkipsGapTasks(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := int64(1000001)
	c := context.Background()

	ids := qualificationBranchRuleIDs(19, func(id int64) bool { return eventGapRuleIDs[id] })
	if len(ids) == 0 {
		t.Fatal("分支 19 应有非缺口任务")
	}
	completeQualificationTasks(t, db, uid, ids)
	cups := srv.ensureQualificationCups(c, uid)
	if len(cups) != 1 || cups[0].ItemID != "reward_bday26" {
		t.Fatalf("应发 reward_bday26，实得 %v", cups)
	}
}

// 奖杯映射表与静态 item_def 逐条对齐：item_id 必须存在且 category=72。
func TestQualificationCupItemsMatchItemDefs(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	c := context.Background()
	cups := make([]qualificationCup, 0, len(qualificationCupByBranch)+1)
	for _, cup := range qualificationCupByBranch {
		cups = append(cups, cup)
	}
	cups = append(cups, qualificationTotalCup)
	for _, cup := range cups {
		def, err := srv.DB.GetItemDefByItemID(c, cup.ItemID)
		if err != nil || def == nil {
			t.Errorf("奖杯物品不存在: %s", cup.ItemID)
			continue
		}
		if def.Category != 72 {
			t.Errorf("%s category=%d，期望 72（奖赏）", cup.ItemID, def.Category)
		}
	}
}

// 奖赏 wire 树的容器枚举 f4 必须投影成奖赏专用分组 14（不是 house 的 10），
// 客户端物品管理器才把它挂进奖赏面板读的分组，实时推送才能刷新面板。
func TestAwardItemTreeProjectsRewardsGroup(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := int64(1000001)
	c := context.Background()
	completeQualificationTasks(t, db, uid, qualificationBranchRuleIDs(1, nil))
	cups := srv.ensureQualificationCups(c, uid)
	if len(cups) != 1 {
		t.Fatalf("应发 1 杯，实得 %d", len(cups))
	}
	tree, err := srv.awardItemTree(c, cups[0])
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(cups[0].WireClass, csSlotEnum)
	if idx < 0 || idx >= len(tree.Fields) {
		t.Fatalf("容器枚举槽缺失 idx=%d", idx)
	}
	if got := u64Of(tree.Fields[idx].Num); got != containerRewards {
		t.Fatalf("奖赏 f4=%d，期望 %d（奖赏分组）", got, containerRewards)
	}
}

// 重放：同一进度连续两次巡检，第二次不返回新杯、奖赏物品总行数不变。
func TestQualificationCupReplayIdempotent(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := int64(1000001)
	c := context.Background()
	completeQualificationTasks(t, db, uid, qualificationBranchRuleIDs(1, nil))

	countRows := func() int {
		var n int
		if err := db.SQL.QueryRow(
			`SELECT COUNT(*) FROM item WHERE uid=? AND wire_class='enikeammfen' AND type_id=2015`,
			uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if cups := srv.ensureQualificationCups(c, uid); len(cups) != 1 {
		t.Fatalf("首次应发 1 杯，实得 %d", len(cups))
	}
	before := countRows()
	if again := srv.ensureQualificationCups(c, uid); len(again) != 0 {
		t.Fatalf("重放不应返回新杯，实得 %d", len(again))
	}
	if after := countRows(); after != before {
		t.Fatalf("重放后奖赏物品行数应不变，before=%d after=%d", before, after)
	}
}

// 合并播报：多枚奖杯只拼一条系统消息，名字数量与传入一致、无换行。
func TestQualificationCupsBroadcastText(t *testing.T) {
	if got := qualificationCupsBroadcastText(nil); got != "" {
		t.Fatalf("空输入应返回空串，实得 %q", got)
	}
	if got := qualificationCupsBroadcastText([]string{"纺车竿高手"}); got != "获得奖赏：「纺车竿高手」" {
		t.Fatalf("单枚文案不符: %q", got)
	}
	multi := qualificationCupsBroadcastText([]string{"纺车竿高手", "超级高手"})
	if want := "获得奖赏：「纺车竿高手」「超级高手」"; multi != want {
		t.Fatalf("多枚文案不符: %q，期望 %q", multi, want)
	}
	if strings.Count(multi, "「") != 2 || strings.ContainsAny(multi, "\r\n") {
		t.Fatalf("多枚应合并为一行且含 2 个名字: %q", multi)
	}
}

// append811Row 把裸 811 行并入 801.f4 的 args(173)：空容器新建，非空原地追加且保序。
// 这是 #407 的 gift811Line 行与本 PR 的资格杯行共存的接缝。
func TestAppend811Row(t *testing.T) {
	levelLine := proto.U32(1) // 占位载荷：本函数只做容器拼接，不解析内容
	qualLine := proto.U32(2)

	got := append811Row(proto.Null(), qualLine)
	if got.Kind != proto.KindArgs || len(got.Items) != 1 || got.Items[0] != qualLine {
		t.Fatalf("空容器应新建 args(173) 并含该行")
	}
	base := proto.Args(proto.S("173"), levelLine)
	got = append811Row(base, qualLine)
	if got != base || len(got.Items) != 2 || got.Items[0] != levelLine || got.Items[1] != qualLine {
		t.Fatalf("非空容器应原地追加并保序，实得 %d 行", len(got.Items))
	}
}

package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 活动分支 9..20 的 273 条任务必须全部注册、编号不重复、静态分母 > 0，
// 且与 eventTaskCounts 推导的编号阶梯逐条吻合。
func TestEventQualificationRulesCovered(t *testing.T) {
	seen := make(map[int64]bool)
	for _, r := range eventQualificationRules {
		if seen[r.ID] {
			t.Fatalf("活动规则 %d 重复注册", r.ID)
		}
		seen[r.ID] = true
		if r.Event != qevFishLanded {
			t.Fatalf("活动规则 %d 事件类型异常", r.ID)
		}
		if r.Counted == nil {
			t.Fatalf("活动规则 %d 缺少计数函数", r.ID)
		}
		if qualificationTarget(uint64(r.ID)) == 0 {
			t.Fatalf("活动规则 %d 缺少静态目标值", r.ID)
		}
	}
	want := 0
	for branch, counts := range eventTaskCounts {
		for si, count := range counts {
			for task := uint64(1); task <= count; task++ {
				id := int64(branch*1000 + uint64(si)*100 + task)
				if !seen[id] {
					t.Fatalf("活动分支 %d 规则 %d 未注册", branch, id)
				}
				want++
			}
		}
	}
	if want != 273 || len(seen) != 273 {
		t.Fatalf("活动任务总数应为 273，实得 want=%d seen=%d", want, len(seen))
	}
	// 并入总表后不破坏永久分支编号。
	for _, r := range qualificationRules {
		if r.ID >= 9001 && r.ID <= 20604 && !seen[r.ID] {
			t.Fatalf("总表含未登记活动规则 %d", r.ID)
		}
	}
}

// 缺口任务恒返回空键（不造假进度）；实装任务对合规事件返回非空键。
func TestEventGapRulesNeverCount(t *testing.T) {
	gaps := []int64{
		12401, 12403, 12603, 13502, 13602, 13603, 14703, 14901, 15603,
		16002, 16103, 16401, 17202, 17302, 17501, 17502, 17602, 17603,
		18302, 18703, 18901, 19101, 19103, 19202, 19301, 19603,
		20002, 20101, 20102, 20104, 20203, 20401,
	}
	byID := make(map[int64]qualificationRuleDef)
	for _, r := range eventQualificationRules {
		byID[r.ID] = r
	}
	ev := qualificationEvent{
		Kind: qevFishLanded, Species: "perch", WeightG: 99999, Rarity: 2,
		CatchGUID: "x", LocLevel: evLocKuori, Bait: "muckworm", BaitCat: 27,
		BaitClass: 6, RodType: 9, LineStrengthKG: 1,
	}
	for _, id := range gaps {
		r, ok := byID[id]
		if !ok {
			t.Fatalf("缺口规则 %d 未注册", id)
		}
		if key := r.Counted(ev); key != "" {
			t.Fatalf("缺口规则 %d 不应计入任何事件，实得键 %q", id, key)
		}
	}
}

// 活动条件过滤：线强度、蠕虫饵类、浮钓/底钓钓组、单尾二选一、中轻硬度。
func TestEventQualificationFilters(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("ev-" + string(rune('a'+seq%26)) + string(rune('a'+seq/26%26)))
		srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 19401 廓里湖 + 线强度 <=5kg + 5 种鱼：6kg 线不计，5kg 计入。
	catch(qualificationEvent{Species: "s1", LocLevel: evLocKuori, LineStrengthKG: 6})
	if n, _ := state(19401); n != 0 {
		t.Fatalf("6kg 线不应计入 19401，实得 %d", n)
	}
	for i := 0; i < 5; i++ {
		catch(qualificationEvent{Species: "sp" + string(rune('a'+i)), LocLevel: evLocKuori, LineStrengthKG: 5})
	}
	if n, ts := state(19401); n != 5 || ts == 0 {
		t.Fatalf("5kg 线五种鱼应达标 19401，实得 %d/%g", n, ts)
	}

	// 15503 苏拉河 + 蠕虫类饵(6) + 11 种：昆虫饵(2) 不计。
	catch(qualificationEvent{Species: "w0", LocLevel: evLocSura, Bait: "maggot", BaitCat: 27, BaitClass: 2})
	if n, _ := state(15503); n != 0 {
		t.Fatalf("昆虫饵不应计入蠕虫类 15503，实得 %d", n)
	}
	catch(qualificationEvent{Species: "w1", LocLevel: evLocSura, Bait: "worm_01", BaitCat: 27, BaitClass: 6})
	if n, _ := state(15503); n != 1 {
		t.Fatalf("蠕虫饵应计入 15503，实得 %d", n)
	}

	// 9003 浮子钓具：路亚钓组(5) 不计，浮钓(1) 计入。
	catch(qualificationEvent{Species: "f0", LocLevel: evLocKomarino, RigType: 5})
	if n, _ := state(9003); n != 0 {
		t.Fatalf("路亚钓组不应计入浮子钓具 9003，实得 %d", n)
	}
	catch(qualificationEvent{Species: "f1", LocLevel: evLocKomarino, RigType: 1})
	if n, _ := state(9003); n != 1 {
		t.Fatalf("浮钓钓组应计入 9003，实得 %d", n)
	}

	// 19501 单尾二选一：塞凡湖鳟 >=3.5kg 或廓里湖红点鲑 >=5kg。
	catch(qualificationEvent{Species: "sevan_trout", WeightG: 3499, LocLevel: evLocKuori})
	catch(qualificationEvent{Species: "chartus_kuoriius", WeightG: 4999, LocLevel: evLocKuori})
	if n, _ := state(19501); n != 0 {
		t.Fatalf("不足重不应计入 19501，实得 %d", n)
	}
	catch(qualificationEvent{Species: "chartus_kuoriius", WeightG: 5000, LocLevel: evLocKuori})
	if n, ts := state(19501); n != 1 || ts == 0 {
		t.Fatalf("5kg 廓里湖红点鲑应达标 19501，实得 %d/%g", n, ts)
	}

	// 20003 中轻/轻/超轻硬度：中型(M) 不计，中轻(ML) 计入。
	catch(qualificationEvent{Species: "riv_brown_trout", LocLevel: evLocKuori, RodPower: "M"})
	if n, _ := state(20003); n != 0 {
		t.Fatalf("中型竿不应计入 20003，实得 %d", n)
	}
	catch(qualificationEvent{Species: "riv_brown_trout", LocLevel: evLocKuori, RodPower: "ML"})
	if n, _ := state(20003); n != 1 {
		t.Fatalf("中轻型竿应计入 20003，实得 %d", n)
	}

	// 20302 计数型：5 尾 >=5kg（按 CatchGUID 去重），同种重复仍计数。
	for i := 0; i < 5; i++ {
		catch(qualificationEvent{Species: "perch", WeightG: 5000, LocLevel: evLocSura})
	}
	if n, ts := state(20302); n != 5 || ts == 0 {
		t.Fatalf("5 尾 5kg 应达标 20302，实得 %d/%g", n, ts)
	}
}

// 活动「稀有鱼（星）」按个体档位、「罕见鱼类」按物种稀有度：常见鱼种的奖杯
// 个体必须计入个体任务；稀有鱼种未达奖杯档不计；鱼种任务反之。
func TestEventRarityUsesIndividualGrade(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	uid := sess.Userid
	seq := 0
	catch := func(ev qualificationEvent) {
		t.Helper()
		seq++
		ev.Kind = qevFishLanded
		ev.CatchGUID = qualificationCountedGUID("evr-" + string(rune('a'+seq)))
		srv.evaluateQualifications(ctx, ev)
	}
	state := func(id int64) (int, float64) {
		t.Helper()
		states, err := db.QualificationStates(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[id]
		return len(st.Counted), st.CompletedTS
	}

	// 12203「三水域稀有鱼（星）」：perch 物种稀有度 0；合格档不计，奖杯档计。
	catch(qualificationEvent{Species: "perch", LocLevel: evLocKomarino, Grade: 2, Rarity: 1})
	if n, _ := state(12203); n != 0 {
		t.Fatalf("合格档不应计入 12203，实得 %d", n)
	}
	catch(qualificationEvent{Species: "perch", LocLevel: evLocKomarino, Grade: 3, Rarity: 0})
	if n, ts := state(12203); n != 1 || ts == 0 {
		t.Fatalf("常见鱼种奖杯个体应达标 12203，实得 %d/%g", n, ts)
	}

	// 16603「铜湖/琥珀湖 3 种罕见鱼类」：物种稀有度 >=1；常见鱼种奖杯个体不计。
	catch(qualificationEvent{Species: "s0", LocLevel: evLocCopper, Grade: 4, Rarity: 0})
	if n, _ := state(16603); n != 0 {
		t.Fatalf("常见鱼种不应计入 16603，实得 %d", n)
	}
	for i, rar := range []int64{1, 2, 1} {
		catch(qualificationEvent{Species: "s" + string(rune('a'+i)), LocLevel: evLocCopper, Rarity: rar})
	}
	if n, ts := state(16603); n != 3 || ts == 0 {
		t.Fatalf("3 种稀有鱼种应达标 16603，实得 %d/%g", n, ts)
	}
}

// 缺口任务不下发到面板：32 条缺口被跳过，全分支实际下发 241 条可完成任务。
func TestEventGapTasksNotEmitted(t *testing.T) {
	if len(eventGapRuleIDs) != 32 {
		t.Fatalf("缺口任务应为 32 条，实得 %d", len(eventGapRuleIDs))
	}
	byID := make(map[int64]bool)
	for _, r := range eventQualificationRules {
		byID[r.ID] = true
	}
	for id := range eventGapRuleIDs {
		if !byID[id] {
			t.Fatalf("缺口规则 %d 未在规则表注册", id)
		}
	}
	emitted := 0
	for branch, counts := range eventTaskCounts {
		for si, count := range counts {
			stage := uint64(si + 1)
			list := eventTaskList(branch, stage, count)
			if uint64(len(list.Items)) != eventEmittableCount(branch, stage, count) {
				t.Fatalf("分支 %d 阶段 %d 下发数与辅助函数不符", branch, stage)
			}
			for _, task := range list.Items {
				rule := task.Fields[1].Val.Items[0]
				id := int64(*rule.Fields[0].Num)
				if eventGapRuleIDs[id] {
					t.Fatalf("缺口任务 %d 不应下发到面板", id)
				}
			}
			emitted += len(list.Items)
		}
	}
	if emitted != 241 {
		t.Fatalf("实际下发应为 241 条可完成任务，实得 %d", emitted)
	}
}

// 线强度上限过滤：未知（0）拒绝、含等号、超限拒绝。与注释口径一致。
func TestLineStrengthAtMostBoundary(t *testing.T) {
	f := qLineStrengthAtMost(5)
	if f(qualificationEvent{LineStrengthKG: 0}) {
		t.Fatal("线强度未知（0）不应计入上限任务")
	}
	if !f(qualificationEvent{LineStrengthKG: 5}) {
		t.Fatal("线强度恰为上限应计入（含等号）")
	}
	if !f(qualificationEvent{LineStrengthKG: 4.9}) {
		t.Fatal("低于上限应计入")
	}
	if f(qualificationEvent{LineStrengthKG: 5.1}) {
		t.Fatal("超过上限不应计入")
	}
}

// 活动任务 5007 条件字段：竿型/硬度/钓组大类应下发到客户端「鱼竿/条件」文案。
func TestEventRule5007Conditions(t *testing.T) {
	decode := func(ruleID uint64) *proto.Value {
		t.Helper()
		body, err := proto.EncodeValue(qualificationRule(ruleID), wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		root, _, err := proto.DecodeValue(body, 0, wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	u32s := func(v *proto.Value) []uint32 {
		t.Helper()
		if v == nil || v.Kind != proto.KindU32Arr {
			t.Fatalf("应为 u32 数组，实得 %v", v)
		}
		out := make([]uint32, 0, len(v.Items))
		for _, it := range v.Items {
			out = append(out, uint32(it.Num))
		}
		return out
	}
	at := func(root *proto.Value, idx int) uint32 {
		t.Helper()
		if idx >= len(root.Fields) || root.Fields[idx].Num == nil {
			t.Fatalf("字段 %d 缺失或非数值", idx)
		}
		return uint32(*root.Fields[idx].Num)
	}

	// 20103 手竿 → 竿型 [3]。
	if got := u32s(decode(20103).Fields[5].Val); len(got) != 1 || got[0] != 3 {
		t.Fatalf("20103 竿型应为 [3]，实得 %v", got)
	}
	// 20003 中轻/轻/超轻 → 硬度 [1,2,3]。
	if got := u32s(decode(20003).Fields[6].Val); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("20003 硬度应为 [1,2,3]，实得 %v", got)
	}
	// 19001 浮子钓具 → 钓组大类 1。
	if got := at(decode(19001), 4); got != 1 {
		t.Fatalf("19001 钓组大类应为 1，实得 %d", got)
	}
	// 19203 底钓渔具 → 钓组大类 3。
	if got := at(decode(19203), 4); got != 3 {
		t.Fatalf("19203 钓组大类应为 3，实得 %d", got)
	}
	// 12201 飞德竿 → 竿型 [10]。
	if got := u32s(decode(12201).Fields[5].Val); len(got) != 1 || got[0] != 10 {
		t.Fatalf("12201 竿型应为 [10]，实得 %v", got)
	}
	// 10102 顺流位 → Idx27=64。
	if got := at(decode(10102), 27); got != 64 {
		t.Fatalf("10102 顺流位应为 64，实得 %d", got)
	}
}

// 12501 文案为「超过 1.5 公斤」，1500g 不应计入，1501g 才计入。
func TestEventStrictWeightBoundary(t *testing.T) {
	var rule qualificationRuleDef
	for _, r := range eventQualificationRules {
		if r.ID == 12501 {
			rule = r
			break
		}
	}
	if rule.Counted == nil {
		t.Fatal("未找到 12501 规则")
	}
	ev := qualificationEvent{Species: "fish", WeightG: 1500, LocLevel: evLocLadoga, RigType: 10}
	if key := rule.Counted(ev); key != "" {
		t.Fatalf("1500g 不应计入 12501，实得键 %q", key)
	}
	ev.WeightG = 1501
	if key := rule.Counted(ev); key == "" {
		t.Fatal("1501g 应计入 12501")
	}
}

// 活动节点开启后，28/1 在 8 个永久节点之外追加活动节点；活动任务分母取静态目标值。
func TestEventNodesUseRealTargets(t *testing.T) {
	srv := eventTestServer(t, true)
	nodes := srv.eventNodes()
	if len(nodes) == 0 {
		t.Fatal("周年开关开启后应下发活动节点")
	}
	for _, node := range nodes {
		if got := *node.Fields[1].Num; got != uint64(eventStageAnniversary) {
			t.Fatalf("活动节点 Idx1 应为 6，实得 %d", got)
		}
		stages := node.Fields[3].Val
		for _, stage := range stages.Items {
			for _, task := range stage.Fields[1].Val.Items {
				rule := task.Fields[1].Val.Items[0]
				id := *rule.Fields[0].Num
				if got := *rule.Fields[3].Num; got != uint64(qualificationTarget(id)) || got == 0 {
					t.Fatalf("活动任务 %d 分母应为 %d，实得 %d", id, qualificationTarget(id), got)
				}
			}
		}
	}
}

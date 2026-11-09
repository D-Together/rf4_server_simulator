package gameserver

import (
	"math"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// lureTestHooks 读落库拟饵的出厂钩 GUID 列表。
func lureTestHooks(t *testing.T, s *Server, lureGUID string) []string {
	t.Helper()
	return lureHookItemGUIDs(lureTestHookSlots(t, s, lureGUID))
}

// lureTestHookSlots 读落库拟饵的原始槽位数组。
func lureTestHookSlots(t *testing.T, s *Server, lureGUID string) []*proto.Value {
	t.Helper()
	row, err := s.DB.GetItemByGUID(t.Context(), lureGUID)
	if err != nil || row == nil {
		t.Fatalf("读取拟饵 %s: %v", lureGUID, err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatalf("解析拟饵字段: %v", err)
	}
	idx := lureHookFieldIndex(row.WireClass)
	if idx < 0 || idx >= len(fields) || fields[idx].Val == nil {
		t.Fatalf("拟饵缺少钩数组字段")
	}
	return fields[idx].Val.Items
}

// 15/1 换钩必须改拟饵实例 f0：钩要挂上去、旧钩让位、钩本体离开背包。
// 旧实现把钩写进竿式 f5/rig_component，拟饵 f0 不变，客户端看不到换钩效果。
func TestLureHookSwapRewritesFactoryHookList(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	lure, err := s.CreateItem(t.Context(), 1000001, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(t.Context(), 1000001, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}

	before := lureTestHooks(t, s, lure.GUID)
	if len(before) == 0 {
		t.Fatal("测试拟饵没有出厂钩")
	}

	res := s.rigSetComponent(t.Context(), 1000001, lure.GUID, catHook, hook.GUID, 250,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
	if res.Rejected || res.MountedGUID != hook.GUID {
		t.Fatalf("拟饵换钩被拒: %+v", res)
	}

	after := lureTestHooks(t, s, lure.GUID)
	if len(after) != len(before) {
		t.Fatalf("换钩不应改变钩位数量: before=%d after=%d", len(before), len(after))
	}
	if after[0] != hook.GUID {
		t.Fatalf("钩位 0 应为新钩 %s，实得 %s", hook.GUID, after[0])
	}
	if before[0] == hook.GUID {
		t.Fatal("新钩未替换原钩")
	}

	row, err := s.DB.GetItemByGUID(t.Context(), hook.GUID)
	if err != nil || row == nil {
		t.Fatalf("读取钩实例: %v", err)
	}
	if row.Container != "rig" {
		t.Fatalf("钩应转为已装配(rig)，实得 %q", row.Container)
	}
	if got := s.lureActiveHookGUID(t.Context(), lure.GUID); got != hook.GUID {
		t.Fatalf("活动钩应指向新钩 %s，实得 %s", hook.GUID, got)
	}
	if len(res.UpdatedTrees) == 0 {
		t.Fatal("换钩后应把拟饵/钩的更新树放进 800 快照")
	}
}

// 15/2 拆钩要把钩从 f0 摘掉并退回背包；元素从数组移除（官方短缩语义，
// 文件头）——全卸空时数组为空，客户端状态行报「钩未安装」。
func TestLureHookRemoveReturnsHookToBackpack(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	lure, err := s.CreateItem(t.Context(), 1000001, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(t.Context(), 1000001, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	before := lureTestHookSlots(t, s, lure.GUID)
	if res := s.rigSetComponent(t.Context(), 1000001, lure.GUID, catHook, hook.GUID, 250,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("拟饵换钩被拒: %+v", res)
	}

	det := s.rigRemoveComponentInstance(t.Context(), 1000001, lure.GUID, catHook, hook.GUID, 250)
	if det == nil || det.CompGUID != hook.GUID {
		t.Fatalf("拟饵拆钩失败: %+v", det)
	}
	after := lureTestHookSlots(t, s, lure.GUID)
	if len(after) != len(before)-1 {
		t.Fatalf("拆钩后槽数应为 %d（元素移除），实得 %d", len(before)-1, len(after))
	}
	for _, g := range lureHookItemGUIDs(after) {
		if g == hook.GUID {
			t.Fatal("拆下的钩仍在拟饵 f0 里")
		}
	}
	row, err := s.DB.GetItemByGUID(t.Context(), hook.GUID)
	if err != nil || row == nil {
		t.Fatalf("读取钩实例: %v", err)
	}
	if row.Container != "backpack" {
		t.Fatalf("拆下的钩应回到背包，实得容器 %q", row.Container)
	}
	if det.ReturnedTree == nil {
		t.Fatal("拆钩应返回钩的更新树")
	}
}

// 卸下原装出厂钩：物化独立背包实例退包一次（f0 新增段）、元素移除。
// 短缩语义下防刷是结构性的：卸钩不回填，原装钩离开槽位只有这一次；
// 空数组再装钩走追加。
func TestLureHookRemoveRefundsFactoryHookOnce(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) == 0 {
		t.Fatal("测试拟饵没有出厂钩")
	}
	target := len(slots) - 1
	targetGUID := lureHookItemGUID(slots, target)
	if lureOriginalFactorySlot(lure.GUID, targetGUID) < 0 {
		t.Fatalf("槽 %d 应是原装出厂钩，实得 %s", target, targetGUID)
	}
	targetObj := slots[target]
	wantNumeric := u64Of(targetObj.Fields[fieldIdxByCS(targetObj.Class, csNumericID)].Num)
	before := lureBackpackHookQty(t, s, uid, int64(wantNumeric))

	// 卸下原装钩：退包一次，元素移除。
	det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, targetGUID, 0)
	if det == nil || det.CompGUID != targetGUID {
		t.Fatalf("拟饵拆出厂钩失败: %+v", det)
	}
	if det.RefundTree == nil {
		t.Fatal("卸下原装出厂钩应物化退包（f0 新增段）")
	}
	rootGUID := treeRootGUID(det.RefundTree)
	row, err := s.DB.GetItemByGUID(t.Context(), rootGUID)
	if err != nil || row == nil {
		t.Fatalf("出厂钩退包实例未落库: %v", err)
	}
	if row.UID != uid || row.Container != "backpack" || row.NumericID != int64(wantNumeric) {
		t.Fatalf("退包实例应归原玩家、在背包、numeric=%d，实得 uid=%d container=%q numeric=%d",
			wantNumeric, row.UID, row.Container, row.NumericID)
	}
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+1 {
		t.Fatalf("退包后背包钩数量应 +1: before=%d after=%d", before, got)
	}
	after := lureTestHookSlots(t, s, lure.GUID)
	if len(after) != len(slots)-1 {
		t.Fatalf("卸下出厂钩后槽数应为 %d，实得 %d", len(slots)-1, len(after))
	}

	// 空数组再装钩（追加）：不触发退包（槽里没有出厂钩可退）。
	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hook.GUID, 0,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("空饵装钩被拒: %+v", res)
	}
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+1 {
		t.Fatalf("空饵装钩不应退包: want=%d got=%d", before+1, got)
	}
	remounted := lureTestHookSlots(t, s, lure.GUID)
	if len(remounted) != 1 || lureHookItemGUID(remounted, 0) != hook.GUID {
		t.Fatalf("空饵装钩应追加为新元素: 实得 %d 个", len(remounted))
	}
}

// lureBackpackHookQty 统计玩家背包中指定 numeric_id 钩的可堆数量。
func lureBackpackHookQty(t *testing.T, s *Server, uid, numericID int64) int64 {
	t.Helper()
	var n int64
	if err := s.DB.SQL.QueryRow(`SELECT COALESCE(SUM(qty),0) FROM item
		WHERE uid=? AND container='backpack' AND numeric_id=?`, uid, numericID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 存量损坏（null 元素/空数组）：启动迁移按定义槽数重建，损坏槽回填
// 幻影空槽钩、健康元素原位保留；二次执行靠 KV 标记跳过。
// v3~v7 实验残留（垫底钩/幻影空槽）迁移剥离：:r/:e 命名空间元素从数组
// 移除，健康元素原位保留；二次执行靠 KV 标记跳过。
func TestRepairLureHookSlotsStripsLegacyArtifacts(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.DB.GetItemByGUID(t.Context(), lure.GUID)
	if err != nil || row == nil {
		t.Fatal(err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	idx := lureHookFieldIndex(row.WireClass)
	if idx < 0 || idx >= len(fields) || fields[idx].Val == nil || fields[idx].Val.Kind != proto.KindArgs {
		t.Fatal("拟饵缺少钩数组字段")
	}
	if len(fields[idx].Val.Items) < 1 {
		t.Skip("测试拟饵没有出厂钩")
	}
	// 模拟 v3~v7 遗留：槽 0 换成 :r 垫底钩，其余保留。
	def, err := s.DB.GetItemDef(t.Context(), row.NumericID)
	if err != nil || def == nil {
		t.Fatal(err)
	}
	keepGUID := lureHookItemGUID(fields[idx].Val.Items, len(fields[idx].Val.Items)-1)
	fields[idx].Val.Items[0] = s.lureHookAt(t.Context(), defAttrs(def), lure.GUID, 0,
		lureRestoredHookKey(0), row.Workmanship, row.Durability)
	if fields[idx].Val.Items[0] == nil {
		t.Skip("定义槽 0 无出厂钩配置")
	}
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.UpdateItemFields(t.Context(), lure.GUID, fj, row.Qty, row.Durability, row.Workmanship, row.RigGUID); err != nil {
		t.Fatal(err)
	}

	fixed, err := s.repairLureHookSlots(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if fixed != 1 {
		t.Fatalf("应清理 1 个拟饵，实得 %d", fixed)
	}
	after, err := s.DB.GetItemByGUID(t.Context(), lure.GUID)
	if err != nil || after == nil {
		t.Fatal(err)
	}
	afterFields, err := FieldsFromJSON(after.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	got := afterFields[idx].Val.Items
	if len(got) != len(fields[idx].Val.Items)-1 {
		t.Fatalf("剥离后元素应为 %d，实得 %d", len(fields[idx].Val.Items)-1, len(got))
	}
	for i, g := range lureHookItemGUIDs(got) {
		if g == uuidLike(lure.GUID, lureRestoredHookKey(0)) {
			t.Fatalf("残留 :r 元素仍在槽 %d", i)
		}
		if i == len(got)-1 && g != keepGUID {
			t.Fatalf("健康元素应原位保留 %s，实得 %s", keepGUID, g)
		}
	}
	if again, err := s.repairLureHookSlots(t.Context()); err != nil || again != 0 {
		t.Fatalf("迁移应只执行一次: fixed=%d err=%v", again, err)
	}
}

// 拟饵钩位不应在 rig_component 留下行：旧实现残留的幽灵行必须被清掉。
func TestLureHookSwapLeavesNoRigComponentRow(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	lure, err := s.CreateItem(t.Context(), 1000001, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(t.Context(), 1000001, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertRigComponentAt(t.Context(), lure.GUID, catHook, 0, NewItemGUID(), 250); err != nil {
		t.Fatal(err)
	}
	if res := s.rigSetComponent(t.Context(), 1000001, lure.GUID, catHook, hook.GUID, 250,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("拟饵换钩被拒: %+v", res)
	}
	rows, err := db.ListRigComponents(t.Context(), lure.GUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rc := range rows {
		if rc.Slot == catHook {
			t.Fatalf("拟饵钩位仍在 rig_component 留行: ordinal=%d guid=%s", rc.Ordinal, rc.GUID)
		}
	}
}

// 拟饵与鱼钩都必须属于当前玩家；否则可直接操作别人的物品。
func TestLureHookRejectsForeignItems(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const owner, stranger = int64(1000001), int64(1000002)

	lure, err := s.CreateItem(t.Context(), owner, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	foreignHook, err := s.CreateItem(t.Context(), stranger, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	res := s.rigSetComponent(t.Context(), owner, lure.GUID, catHook, foreignHook.GUID, 250,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
	if !res.Rejected {
		t.Fatalf("不应允许装配其他玩家的鱼钩: %+v", res)
	}

	foreignLure, err := s.CreateItem(t.Context(), stranger, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	ownHook, err := s.CreateItem(t.Context(), owner, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	res = s.rigSetComponent(t.Context(), owner, foreignLure.GUID, catHook, ownHook.GUID, 250,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
	if !res.Rejected {
		t.Fatalf("不应允许修改其他玩家的拟饵: %+v", res)
	}
	if det := s.rigRemoveComponentInstance(t.Context(), owner, foreignLure.GUID, catHook, "", 250); det != nil {
		t.Fatalf("不应允许拆其他玩家拟饵的钩: %+v", det)
	}
}

// 15/1 槽位下标在请求尾部 int（dump 实证：详情面板按数组 Length 循环
// 创建槽控件并把循环下标发给服务端，0 基裸下标）。换钩必须落到玩家点
// 的那个槽；顶替活动钩 0 是旧实现读错 int 的症状。
func TestLureHookMountTargetsRequestedSlot(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) < 2 {
		t.Skipf("测试拟饵只有 %d 个钩槽", len(slots))
	}
	factory0 := lureHookItemGUID(slots, 0)
	hookA, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hookB, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}

	// A 装第二槽（extra=1）：槽 0 的出厂钩必须原位保留。
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hookA.GUID, 1,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("拟饵换钩被拒: %+v", res)
	}
	got := lureTestHookSlots(t, s, lure.GUID)
	if g := lureHookItemGUID(got, 1); g != hookA.GUID {
		t.Fatalf("extra=1 应装到槽 1，实得 %s", g)
	}
	if g := lureHookItemGUID(got, 0); g != factory0 {
		t.Fatalf("装槽 1 不应动槽 0：原 %s 实得 %s", factory0, g)
	}

	// B 装第一槽（extra=0）。
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hookB.GUID, 0,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("拟饵换钩被拒: %+v", res)
	}
	got = lureTestHookSlots(t, s, lure.GUID)
	if g := lureHookItemGUID(got, 0); g != hookB.GUID {
		t.Fatalf("extra=0 应装到槽 0，实得 %s", g)
	}
	if g := lureHookItemGUID(got, 1); g != hookA.GUID {
		t.Fatalf("装槽 0 不应动槽 1：实得 %s", g)
	}

	// 拆 B（元素移除、数组缩短为 [A]）后按 250+0 规范索引重装：落槽 0
	// 并顶替 A（短缩语义下原槽 1 已不存在）。
	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, hookB.GUID, 0); det == nil {
		t.Fatal("拟饵拆钩失败")
	}
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hookB.GUID, 250,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("拟饵换钩被拒: %+v", res)
	}
	got = lureTestHookSlots(t, s, lure.GUID)
	if len(got) != 1 {
		t.Fatalf("拆 B 后重装应得 1 个元素，实得 %d", len(got))
	}
	if g := lureHookItemGUID(got, 0); g != hookB.GUID {
		t.Fatalf("extra=250 应装到槽 0，实得 %s", g)
	}
}

// 多槽追加语义（14:31~14:32 实测客户端行为）：数组缩短后行控件不重建，
// 装钩下标 ≥ 当前元素数 = 追加意图（追加到末尾、定义槽数封顶）；拆钩
// 命中陈旧行（GUID 不在数组）拒绝，绝不误拆第一颗。
func TestLureHookMultiSlotAppendAndStaleRowGuard(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) < 2 {
		t.Skipf("测试拟饵只有 %d 个钩槽", len(slots))
	}
	defSlots := len(slots)
	hookA, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hookB, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hookC, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}

	// 全卸空（官方短缩语义）。
	for _, g := range lureHookItemGUIDs(slots) {
		if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, g, 0); det == nil {
			t.Fatalf("拆出厂钩 %s 失败", g)
		}
	}
	if got := lureTestHookSlots(t, s, lure.GUID); len(got) != 0 {
		t.Fatalf("全卸空后数组应为空，实得 %d 个元素", len(got))
	}

	// 装 A（extra=0，空数组追加）→ [A]。
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hookA.GUID, 0,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("空饵装钩被拒: %+v", res)
	}
	// 装 B（extra=1 ≥ len=1：陈旧行号=追加意图）→ [A,B]，不得顶替 A。
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hookB.GUID, 1,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("第二槽装钩被拒: %+v", res)
	}
	got := lureTestHookSlots(t, s, lure.GUID)
	if len(got) != 2 {
		t.Fatalf("追加后应有 2 个元素，实得 %d", len(got))
	}
	if g0, g1 := lureHookItemGUID(got, 0), lureHookItemGUID(got, 1); g0 != hookA.GUID || g1 != hookB.GUID {
		t.Fatalf("追加不应顶替：实得 [%s, %s]，期望 [%s, %s]", g0, g1, hookA.GUID, hookB.GUID)
	}

	// 拆 A 后按陈旧行号再拆 A（GUID 已不在数组）：拒绝，B 必须原位保留。
	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, hookA.GUID, 0); det == nil {
		t.Fatal("拆 A 失败")
	}
	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, hookA.GUID, 0); det != nil {
		t.Fatalf("陈旧行 GUID 再拆应拒绝: %+v", det)
	}
	got = lureTestHookSlots(t, s, lure.GUID)
	if len(got) != 1 || lureHookItemGUID(got, 0) != hookB.GUID {
		t.Fatalf("拒绝陈旧行后 B 应原位保留，实得 %v", lureHookItemGUIDs(got))
	}

	// 槽数封顶：定义 %d 槽已装满后（先追加 B 满 2），再装 C 应被拒。
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hookA.GUID, 1,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("装回 A 被拒: %+v", res)
	}
	if defSlots < 3 {
		if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hookC.GUID, 2,
			rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); !res.Rejected {
			t.Fatalf("超过定义槽数 %d 的装钩应被拒", defSlots)
		}
	}
}

// 竿上拟饵的 bait_hook 关联以竿 GUID 为行、key=bait_hook:<拟饵GUID>；
// 换钩要改成新钩，拆钩要删除旧关联。
func TestLureHookSelectionTracksSwapAndRemove(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	before := lureTestHooks(t, s, lure.GUID)
	if len(before) == 0 {
		t.Fatal("测试拟饵没有出厂钩")
	}
	const rodGUID = "00112233445566778899aabbccddeeff"
	key := "bait_hook:" + lure.GUID
	if err := db.UpsertRodSetting(t.Context(), rodGUID, key, before[0]); err != nil {
		t.Fatal(err)
	}

	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hook.GUID, 250,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); res.Rejected {
		t.Fatalf("拟饵换钩被拒: %+v", res)
	}
	var selected string
	if err := db.SQL.QueryRow(`SELECT value FROM rod_setting WHERE guid=? AND key=?`, rodGUID, key).Scan(&selected); err != nil {
		t.Fatal(err)
	}
	if selected != hook.GUID {
		t.Fatalf("换钩后 bait_hook 应指向新钩 %s，实得 %s", hook.GUID, selected)
	}

	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, hook.GUID, 250); det == nil {
		t.Fatal("拟饵拆钩失败")
	}
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM rod_setting WHERE guid=? AND key=?`, rodGUID, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("拆钩后 bait_hook 关联应删除，实存 %d 行", n)
	}
}

// PR#339 第三轮评审 P2：装/拆非活动槽不能改动 rod_setting 的活动钩关联。
func TestLureHookNonActiveSlotPreservesBaitHookSelection(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) < 2 {
		t.Skip("测试拟饵需要至少两个钩槽")
	}
	active := lureHookItemGUID(slots, 0)
	const rodGUID = "00112233445566778899aabbccddeeff"
	key := "bait_hook:" + lure.GUID
	if err := db.UpsertRodSetting(t.Context(), rodGUID, key, active); err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hook.GUID, 1,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
	if res.Rejected {
		t.Fatalf("非活动槽装钩被拒: %+v", res)
	}
	if got := s.lureActiveHookGUID(t.Context(), lure.GUID); got != active {
		t.Fatalf("非活动槽装钩改变了活动钩: got=%s want=%s", got, active)
	}
	if got := lureTestRodSetting(t, db, rodGUID, key); got != active {
		t.Fatalf("非活动槽装钩后 bait_hook=%s，期望仍为 %s", got, active)
	}

	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, hook.GUID, 1); det == nil {
		t.Fatal("拆非活动槽钩失败")
	}
	if got := lureTestRodSetting(t, db, rodGUID, key); got != active {
		t.Fatalf("非活动槽拆钩后 bait_hook=%s，期望仍为 %s", got, active)
	}
}

// PR#339 第三轮评审 P2：Commit 必须使用事务内重读的钩行重建拟饵内嵌树，
// 不能把 Prepare 阶段的耐久/字段快照写回 f0。
func TestLureHookMountUsesCommitTimeHookState(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	plan := s.lureHookMountPrepare(t.Context(), uid, lure.GUID, hook.GUID, 250)
	if plan == nil {
		t.Fatal("拟饵装钩预检失败")
	}
	if err := s.DB.SetItemDurability(t.Context(), hook.GUID, 0.25); err != nil {
		t.Fatal(err)
	}
	res := s.lureHookMountCommit(t.Context(), plan)
	if res.Rejected {
		t.Fatalf("提交被拒: %+v", res)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) == 0 {
		t.Fatal("拟饵没有钩槽")
	}
	if got := lureTestHookDurability(t, slots[0]); math.Abs(got-0.25) > 0.001 {
		t.Fatalf("拟饵内嵌钩耐久=%v，期望事务内最新值 0.25", got)
	}
}

// 同上的损坏态复验：Prepare 后钩耐久归零，Commit 必须整体拒绝。
func TestLureHookMountRejectsBrokenCommitState(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	before := lureHookItemGUID(lureTestHookSlots(t, s, lure.GUID), 0)
	plan := s.lureHookMountPrepare(t.Context(), uid, lure.GUID, hook.GUID, 250)
	if plan == nil {
		t.Fatal("拟饵装钩预检失败")
	}
	if err := s.DB.SetItemDurability(t.Context(), hook.GUID, 0); err != nil {
		t.Fatal(err)
	}
	if res := s.lureHookMountCommit(t.Context(), plan); !res.Rejected {
		t.Fatalf("Prepare 后损坏的钩不得提交: %+v", res)
	}
	if got := lureHookItemGUID(lureTestHookSlots(t, s, lure.GUID), 0); got != before {
		t.Fatalf("拒绝后拟饵槽位被改动: got=%s want=%s", got, before)
	}
	if row, err := s.DB.GetItemByGUID(t.Context(), hook.GUID); err != nil || row == nil || row.Container != "backpack" {
		t.Fatalf("拒绝后新钩应留在背包: %+v %v", row, err)
	}
}

func lureTestRodSetting(t *testing.T, db *store.DB, rodGUID, key string) string {
	t.Helper()
	var value string
	if err := db.SQL.QueryRow(`SELECT value FROM rod_setting WHERE guid=? AND key=?`, rodGUID, key).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func lureTestHookDurability(t *testing.T, v *proto.Value) float64 {
	t.Helper()
	idx := fieldIdxByCS(v.Class, csDurability)
	if idx < 0 || idx >= len(v.Fields) {
		t.Fatal("钩缺少耐久字段")
	}
	if fieldIsFloat(v.Class, idx) {
		return v.Fields[idx].Float
	}
	return float64(math.Float32frombits(uint32(u64Of(v.Fields[idx].Num))))
}

// PR#339 评审 P2 回归：多槽拟饵先卸一槽后，剩余原装出厂钩的当前下标已
// 漂移（原槽 1 变当前下标 0）。替换它时必须仍按原装识别并退包。
func TestLureHookOriginalRefundAfterShortenDrift(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) < 2 {
		t.Skipf("测试拟饵只有 %d 个钩槽", len(slots))
	}
	f0 := slots[0]
	f1 := slots[1]
	if lureOriginalFactorySlot(lure.GUID, lureHookItemGUID(items1(f0), 0)) < 0 ||
		lureOriginalFactorySlot(lure.GUID, lureHookItemGUID(items1(f1), 0)) < 0 {
		t.Fatal("测试拟饵槽位不是原装出厂钩")
	}
	wantNumeric := u64Of(f1.Fields[fieldIdxByCS(f1.Class, csNumericID)].Num)
	before := lureBackpackHookQty(t, s, uid, int64(wantNumeric))

	// 卸掉槽 0：数组缩短，原槽 1 的原装钩漂移到当前下标 0。
	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook,
		lureHookItemGUID(items1(f0), 0), 0); det == nil {
		t.Fatal("卸槽 0 失败")
	}
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+1 {
		t.Fatalf("卸槽 0 应退包 +1（两槽同型号时按同 numeric 计）: before=%d got=%d", before, got)
	}

	// 替换漂移后剩余的原装钩（当前下标 0，原槽 1）：必须仍识别为原装并退包。
	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hook.GUID, 0,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
	if res.Rejected {
		t.Fatalf("替换剩余原装钩被拒: %+v", res)
	}
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+2 {
		t.Fatalf("短缩漂移后替换原装钩应再退包 +1（共 +2）: got=%d want=%d", got, before+2)
	}
}

// items1 把单个钩对象包装成单元素数组供 lureHookItemGUID 取 GUID。
func items1(v *proto.Value) []*proto.Value { return []*proto.Value{v} }

// PR#339 评审 P1 回归：拟饵 f0 写回失败（UPDATE 触发器 ABORT）时，换钩
// 整体回滚——不退包、不拆旧钩、数组不变，杜绝出厂钩复制。
func TestLureHookMountRollsBackOnWriteFailure(t *testing.T) {
	t.Parallel()
	s, db := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) == 0 {
		t.Skip("测试拟饵没有出厂钩")
	}
	f0 := slots[0]
	wantNumeric := u64Of(f0.Fields[fieldIdxByCS(f0.Class, csNumericID)].Num)
	before := lureBackpackHookQty(t, s, uid, int64(wantNumeric))
	arrayBefore := len(slots)

	// 触发器：拟饵行的 fields_json 更新强制失败（SQLite 触发器不支持
	// 绑定参数，GUID 为十六进制串可安全内联）。
	if _, err := db.SQL.ExecContext(t.Context(),
		`CREATE TRIGGER fail_lure_write BEFORE UPDATE OF fields_json ON item
		 WHEN NEW.guid='`+lure.GUID+`' BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hook.GUID, 0,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
	if !res.Rejected {
		t.Fatalf("写回失败必须整体拒绝: %+v", res)
	}
	// 退包未发生（无复制）、新钩仍在背包、数组不变。
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before {
		t.Fatalf("写回失败时不得退包: before=%d got=%d", before, got)
	}
	hookRow, err := s.DB.GetItemByGUID(t.Context(), hook.GUID)
	if err != nil || hookRow == nil || hookRow.Container != "backpack" {
		t.Fatalf("写回失败时新钩应留在背包: %+v %v", hookRow, err)
	}
	if got := lureTestHookSlots(t, s, lure.GUID); len(got) != arrayBefore {
		t.Fatalf("写回失败时拟饵数组不应变化: before=%d after=%d", arrayBefore, len(got))
	}
}

// lureTestActiveOrdinal 读落库拟饵的活动钩下标（f1，nkogmkobfng）。
func lureTestActiveOrdinal(t *testing.T, s *Server, lureGUID string) int {
	t.Helper()
	row, err := s.DB.GetItemByGUID(t.Context(), lureGUID)
	if err != nil || row == nil {
		t.Fatalf("读取拟饵 %s: %v", lureGUID, err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatalf("解析拟饵字段: %v", err)
	}
	k := fieldIdxByCS(row.WireClass, "nkogmkobfng")
	if k < 0 || k >= len(fields) || fields[k].Num == nil {
		return 0
	}
	return int(u64Of(fields[k].Num))
}

// setLureTestActiveOrdinal 直写活动钩下标（模拟客户端早已把 f1 指到
// 后面的槽）。
func setLureTestActiveOrdinal(t *testing.T, s *Server, lureGUID string, v int) {
	t.Helper()
	row, err := s.DB.GetItemByGUID(t.Context(), lureGUID)
	if err != nil || row == nil {
		t.Fatalf("读取拟饵 %s: %v", lureGUID, err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	k := fieldIdxByCS(row.WireClass, "nkogmkobfng")
	if k < 0 || k >= len(fields) {
		t.Fatal("拟饵缺活动钩下标字段")
	}
	fields[k].Num = ptrU64(uint64(v))
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.UpdateItemFields(t.Context(), lureGUID, fj, row.Qty, row.Durability, row.Workmanship, row.RigGUID); err != nil {
		t.Fatal(err)
	}
}

// PR#339 评审 P2 回归：数组缩短后活动钩下标必须按剥离映射随迁，不得
// 机械回落 0 把仍在的活动钩切走。3 槽拟饵 f1=2（第三颗活动）：
//   - 卸中间槽 1 → 活动钩仍在，随之前移到新下标 1（旧实现会越界回落 0）；
//   - 再卸活动钩本身 → 其后没有保留槽，收末位 0。
func TestLureHookRemoveRecalculatesActiveOrdinal(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "crankbaits_1702", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) < 3 {
		t.Skipf("测试拟饵只有 %d 个钩槽", len(slots))
	}
	setLureTestActiveOrdinal(t, s, lure.GUID, 2)

	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook,
		lureHookItemGUID(slots, 1), 1); det == nil {
		t.Fatal("卸槽 1 失败")
	}
	if got := lureTestActiveOrdinal(t, s, lure.GUID); got != 1 {
		t.Fatalf("卸中间槽后活动钩应随迁到 1，实得 %d", got)
	}

	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook,
		lureHookItemGUID(slots, 2), 1); det == nil {
		t.Fatal("卸活动钩失败")
	}
	if got := lureTestActiveOrdinal(t, s, lure.GUID); got != 0 {
		t.Fatalf("卸活动钩后应收末位 0，实得 %d", got)
	}
}

// PR#339 评审 P2 回归：槽内元素 GUID 命中原装出厂钩命名空间、但退包准备
// 失败（元素树缺 NumericID 无法物化）时，装/拆必须整体拒绝——钩不得从
// 数组消失而无退包。
func TestLureHookRejectsWhenFactoryRefundPrepFails(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "spinnerbaits_1244", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.DB.GetItemByGUID(t.Context(), lure.GUID)
	if err != nil || row == nil {
		t.Fatal(err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	idx := lureHookFieldIndex(row.WireClass)
	if idx < 0 || idx >= len(fields) || fields[idx].Val == nil || len(fields[idx].Val.Items) == 0 {
		t.Skip("测试拟饵没有出厂钩")
	}
	factoryGUID := lureHookItemGUID(fields[idx].Val.Items, 0)
	if lureOriginalFactorySlot(lure.GUID, factoryGUID) < 0 {
		t.Fatalf("槽 0 应是原装出厂钩，实得 %s", factoryGUID)
	}
	// 槽 0 元素剥掉 NumericID 字段，模拟"识别为出厂钩但物化失败"。
	elem := fields[idx].Val.Items[0]
	if k := fieldIdxByCS(elem.Class, csNumericID); k >= 0 && k < len(elem.Fields) {
		elem.Fields = append(elem.Fields[:k], elem.Fields[k+1:]...)
	} else {
		t.Skip("出厂钩元素缺 NumericID 字段")
	}
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.UpdateItemFields(t.Context(), lure.GUID, fj, row.Qty, row.Durability, row.Workmanship, row.RigGUID); err != nil {
		t.Fatal(err)
	}

	// 拆被拒：数组原样。
	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, factoryGUID, 0); det != nil {
		t.Fatalf("退包准备失败必须拒绝拆钩: %+v", det)
	}
	if got := len(lureTestHookSlots(t, s, lure.GUID)); got != 1 {
		t.Fatalf("被拒后数组不应变化，实得 %d 个元素", got)
	}

	// 装到同槽（替换这颗退不出去的出厂钩）同样被拒，新钩留在背包。
	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res := s.rigSetComponent(t.Context(), uid, lure.GUID, catHook, hook.GUID, 0,
		rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true}); !res.Rejected {
		t.Fatalf("退包准备失败必须拒绝换钩: %+v", res)
	}
	if got := lureTestHookSlots(t, s, lure.GUID); lureHookItemGUID(got, 0) != factoryGUID {
		t.Fatalf("被拒后槽 0 应原样保留 %s，实得 %s", factoryGUID, lureHookItemGUID(got, 0))
	}
	if row2, err := s.DB.GetItemByGUID(t.Context(), hook.GUID); err != nil || row2 == nil || row2.Container != "backpack" {
		t.Fatalf("被拒后新钩应留在背包: %+v %v", row2, err)
	}
}

// PR#339 评审 P1 回归（并发交错的可测形态）：预检后、提交前钓组被另一个
// call_id 改动时，陈旧预检的提交必须整体拒绝，不得按陈旧快照退包。用
// 「Prepare → 完整走一遍公开 API → Commit 陈旧 plan」确定性复现交错。
func TestLureHookMountCommitRejectsStalePlan(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) < 2 {
		t.Skipf("测试拟饵只有 %d 个钩槽", len(slots))
	}
	factory1 := lureHookItemGUID(slots, 1)
	f1obj := slots[1]
	wantNumeric := u64Of(f1obj.Fields[fieldIdxByCS(f1obj.Class, csNumericID)].Num)
	before := lureBackpackHookQty(t, s, uid, int64(wantNumeric))

	hook, err := s.CreateItem(t.Context(), uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	// 预检：装 hook 到槽 1，替换出厂钩 factory1（快照记下要退包）。
	plan := s.lureHookMountPrepare(t.Context(), uid, lure.GUID, hook.GUID, 1)
	if plan == nil || plan.oldGUID != factory1 {
		t.Fatalf("预检应锁定槽 1 出厂钩，实得 %+v", plan)
	}
	// 交错：另一个请求先卸掉这颗出厂钩（退包 +1 已发生）。
	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, factory1, 1); det == nil {
		t.Fatal("交错拆钩失败")
	}
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+1 {
		t.Fatalf("交错拆钩应恰好退包 +1: got=%d want=%d", got, before+1)
	}
	// 陈旧 plan 提交必须被拒（槽内容与快照不一致）。
	if res := s.lureHookMountCommit(t.Context(), plan); !res.Rejected {
		t.Fatalf("陈旧预检提交必须拒绝: %+v", res)
	}
	// 退包恰好 +1（不得双退），新钩仍在背包。
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+1 {
		t.Fatalf("陈旧提交不得再退包: want=%d got=%d", before+1, got)
	}
	if row, err := s.DB.GetItemByGUID(t.Context(), hook.GUID); err != nil || row == nil || row.Container != "backpack" {
		t.Fatalf("陈旧提交被拒后新钩应留在背包: %+v %v", row, err)
	}
}

// 陈旧拆钩 plan 同理：预检锁定的出厂钩已被另一请求拆掉并退包后，陈旧
// 提交必须拒绝——剩余出厂钩不得再退一次（双退刷钩）。
func TestLureHookRemoveCommitRejectsStalePlan(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	slots := lureTestHookSlots(t, s, lure.GUID)
	if len(slots) < 2 {
		t.Skipf("测试拟饵只有 %d 个钩槽", len(slots))
	}
	factory0 := lureHookItemGUID(slots, 0)
	factory1 := lureHookItemGUID(slots, 1)
	wantNumeric := u64Of(slots[0].Fields[fieldIdxByCS(slots[0].Class, csNumericID)].Num)
	before := lureBackpackHookQty(t, s, uid, int64(wantNumeric))

	// 预检：拆槽 0 出厂钩。
	plan := s.lureHookRemovePrepare(t.Context(), uid, lure.GUID, factory0, 0)
	if plan == nil || plan.removedGUID != factory0 {
		t.Fatalf("预检应锁定槽 0，实得 %+v", plan)
	}
	// 交错：另一个请求先拆掉这颗钩（退包 +1 已发生）。
	if det := s.rigRemoveComponentInstance(t.Context(), uid, lure.GUID, catHook, factory0, 0); det == nil {
		t.Fatal("交错拆钩失败")
	}
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+1 {
		t.Fatalf("交错拆钩应恰好退包 +1: got=%d want=%d", got, before+1)
	}
	// 陈旧 plan 提交必须被拒，不得按快照再退一次。
	if det := s.lureHookRemoveCommit(t.Context(), plan); det != nil {
		t.Fatalf("陈旧预检提交必须拒绝: %+v", det)
	}
	if got := lureBackpackHookQty(t, s, uid, int64(wantNumeric)); got != before+1 {
		t.Fatalf("陈旧提交不得双退: want=%d got=%d", before+1, got)
	}
	got := lureTestHookSlots(t, s, lure.GUID)
	if len(got) != 1 || lureHookItemGUID(got, 0) != factory1 {
		t.Fatalf("陈旧提交被拒后剩余原装钩应原位保留，实得 %v", lureHookItemGUIDs(got))
	}
}

// PR#339 评审 P2 回归：迁移剥离垫底钩后，活动钩下标按剥离映射随迁——
// f1 指向的活动钩仍在数组时不得被机械切回第一颗。
func TestRepairLureHookSlotsAdjustsActiveOrdinal(t *testing.T) {
	t.Parallel()
	s, _ := newRigTestServer(t)
	const uid = int64(1000001)
	lure, err := s.CreateItem(t.Context(), uid, "crankbaits_1702", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.DB.GetItemByGUID(t.Context(), lure.GUID)
	if err != nil || row == nil {
		t.Fatal(err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	idx := lureHookFieldIndex(row.WireClass)
	if idx < 0 || idx >= len(fields) || fields[idx].Val == nil || fields[idx].Val.Kind != proto.KindArgs {
		t.Fatal("拟饵缺少钩数组字段")
	}
	if len(fields[idx].Val.Items) < 1 {
		t.Skip("测试拟饵没有出厂钩")
	}
	def, err := s.DB.GetItemDef(t.Context(), row.NumericID)
	if err != nil || def == nil {
		t.Fatal(err)
	}
	// 模拟 v3~v7 遗留：槽 0 是 :r 垫底钩，活动钩 f1=2（原下标 2）。剥离
	// 后数组 [F1,F2]，活动钩应随迁到 1，不得机械回落 0 切走活动钩。
	fields[idx].Val.Items[0] = s.lureHookAt(t.Context(), defAttrs(def), lure.GUID, 0,
		lureRestoredHookKey(0), row.Workmanship, row.Durability)
	if fields[idx].Val.Items[0] == nil {
		t.Skip("定义槽 0 无出厂钩配置")
	}
	if k := fieldIdxByCS(row.WireClass, "nkogmkobfng"); k >= 0 && k < len(fields) {
		fields[k].Num = ptrU64(2)
	}
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.UpdateItemFields(t.Context(), lure.GUID, fj, row.Qty, row.Durability, row.Workmanship, row.RigGUID); err != nil {
		t.Fatal(err)
	}

	fixed, err := s.repairLureHookSlots(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if fixed != 1 {
		t.Fatalf("应清理 1 个拟饵，实得 %d", fixed)
	}
	after, err := s.DB.GetItemByGUID(t.Context(), lure.GUID)
	if err != nil || after == nil {
		t.Fatal(err)
	}
	afterFields, err := FieldsFromJSON(after.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(afterFields[idx].Val.Items); n != 2 {
		t.Fatalf("剥离后应剩 2 个元素，实得 %d", n)
	}
	if k := fieldIdxByCS(row.WireClass, "nkogmkobfng"); k >= 0 && k < len(afterFields) {
		if got := int(u64Of(afterFields[k].Num)); got != 1 {
			t.Fatalf("剥离垫底钩后活动钩应随迁到 1，实得 %d", got)
		}
	}
}

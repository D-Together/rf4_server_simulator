package gameserver

// 海钓倒钓臂多钩回归：钩位数必须按运行时实际钩数算，不能只看静态模板。
//
// 证据（logs/gameserver.log，杆 90e95cf8 / 3c0d7f44，2026-11-09 10:26:54–10:28:59）：
//
//	装配 parent=90e95cf8 slot=12 ordinal=0 item=071993e7 extra=250
//	装配 parent=90e95cf8 slot=12 ordinal=1 item=544b8300 extra=1001
//	装配 parent=90e95cf8 slot=12 ordinal=2 item=8d1bd556 extra=1005
//	装配 parent=90e95cf8 slot=12 ordinal=3 item=d9e98f70 extra=1009
//
// 四条 catHook 组件同时存在于 f5 真源。客户端 RigMarineBase.faocjcaiac 从 1009
// 起每次 −4 遍历到 1001 构造钩组件，1001/1005/1009 是客户端真实使用的钩位。
//
// 服务端没有也不需要单独的“倒钓臂已展开”开关事件：钩组件本身就是运行时状态。
// 因此钩位数 = max(rigSlotTemplates 声明, f5 实际 catHook 组件数)。修复前
// hookPositionsFor 只读模板（rig 50/51 声明 1 个钩位），多钩被当成单钩：
// 所有饵并进第一枚钩，第二枚起的钩永远不判鱼、不吃饵、不算断件。

import "testing"

// mountArmHook 在指定客户端钩位索引上装一枚钩。extra>=1000 是客户端倒钓臂
// 展开后的钩位命名空间，rigSetTarget 必须按该索引原样落库。
func mountArmHook(t *testing.T, s *Server, rod string, extra int64) string {
	t.Helper()
	hook, err := s.CreateItem(t.Context(), 1000001, "mhook_20924", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	res := s.rigSetComponent(t.Context(), 1000001, rod, catHook, hook.GUID, extra,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true})
	if res.Rejected || res.MountedGUID == "" {
		t.Fatalf("装钩(extra=%d) 被拒: %+v", extra, res)
	}
	return res.MountedGUID
}

// newArmMultiHookRod 建一根海钓铁板竿（rig 50，模板只声明 1 个钩位）。
func newArmMultiHookRod(t *testing.T, s *Server) string {
	t.Helper()
	rod, err := s.CreateItem(t.Context(), 1000001, "pilkr_10212_90340", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !s.rigSetType(t.Context(), 1000001, rod.GUID, 50) {
		t.Fatal("rigSetType(50) 失败")
	}
	return rod.GUID
}

// TestMarineArmHooksFormSeparateBaitGroups 锁定多钩判鱼入口：
// 两枚钩各挂一枚饵必须分成两组。修复前按单钩处理，两枚饵并成第一枚钩上的
// 双饵组合，第二枚钩永远不参与判鱼，也不吃饵。
func TestMarineArmHooksFormSeparateBaitGroups(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := t.Context()
	rod := newArmMultiHookRod(t, s)

	mainHook := mountArmHook(t, s, rod, 250)
	armHook := mountArmHook(t, s, rod, 1001)
	_, baitMain := baitTestMount(t, s, rod, "worm_01", 1, 350)
	_, baitArm := baitTestMount(t, s, rod, "muckworm", 1, 351)

	groups := s.baitedHookSets(ctx, rod)
	if len(groups) != 2 {
		t.Fatalf("两枚钩各一枚饵的分组数 = %d，期望 2 组（单钩处理会把饵并到第一枚钩上）: %+v",
			len(groups), groups)
	}
	got := map[string][]string{}
	for _, g := range groups {
		got[g.hookGUID] = g.baitGUIDs
	}
	if len(got) != 2 {
		t.Fatalf("分组落到同一枚钩上: %+v", groups)
	}
	if b := got[mainHook]; len(b) != 1 || b[0] != baitMain {
		t.Fatalf("250 钩位分组 = %v，期望仅 %s", b, shortGUID(baitMain))
	}
	if b := got[armHook]; len(b) != 1 || b[0] != baitArm {
		t.Fatalf("1001 钩位分组 = %v，期望仅 %s", b, shortGUID(baitArm))
	}
}

// TestMarineArmFourHooksAllKept 锁定倒钓臂三个钩位 + 主钩位全部算数：
// f5 里四条 catHook 组件都必须被算进钩位数，两个饵只能占用其中两枚钩，
// 另外两枚钩保持空钩（不判鱼、不被并组）。
func TestMarineArmFourHooksAllKept(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := t.Context()
	rod := newArmMultiHookRod(t, s)

	for _, extra := range []int64{250, 1001, 1005, 1009} {
		mountArmHook(t, s, rod, extra)
	}
	if n := len(rigCompKeysOfCategory(s.storedRigComponents(ctx, rod), catHook)); n != 4 {
		t.Fatalf("f5 钩组件 = %d 个，期望 4 个", n)
	}
	_, bait0 := baitTestMount(t, s, rod, "worm_01", 1, 350)
	_, bait1 := baitTestMount(t, s, rod, "muckworm", 1, 351)

	groups := s.baitedHookSets(ctx, rod)
	if len(groups) != 2 {
		t.Fatalf("4 钩 2 饵的分组数 = %d，期望 2 组（只判挂了饵的钩）: %+v", len(groups), groups)
	}
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.hookGUID] = true
	}
	if !seen[groups[0].hookGUID] || len(seen) != 2 {
		t.Fatalf("两个分组必须落在不同钩上: %+v", groups)
	}
	if groups[0].baitGUIDs[0] != bait0 && groups[1].baitGUIDs[0] != bait0 {
		t.Fatalf("第一枚饵 %s 未参与分组: %+v", shortGUID(bait0), groups)
	}
	if groups[0].baitGUIDs[0] != bait1 && groups[1].baitGUIDs[0] != bait1 {
		t.Fatalf("第二枚饵 %s 未参与分组: %+v", shortGUID(bait1), groups)
	}
}

// TestMarineArmRebaitCoversAllHookOrdinals 锁定补饵遍历上限：
// 多钩钓组按钩序号写 rebait:<钩序号>。饵挂在第 3 枚钩（钩序号 2）上时，
// 遍历上限若仍取模板的 2，这条补饵请求永远不会被读到，该钩断饵后不再补。
func TestMarineArmRebaitCoversAllHookOrdinals(t *testing.T) {
	s, db := newRigTestServer(t)
	ctx := t.Context()
	rod := newArmMultiHookRod(t, s)

	for _, extra := range []int64{250, 1001, 1005, 1009} {
		mountArmHook(t, s, rod, extra)
	}
	// 第三枚钩（1005，钩序号 2）挂饵：客户端的饵位只有 350/351 两个，
	// 钩与饵的绑定由装配时写入的 bait_hook 映射决定，这里直接落该映射。
	hookThird := s.storedRigComponents(ctx, rod)[rigCompKey(catHook, 2)].GUID
	source, bait := baitTestMount(t, s, rod, "worm_01", 5, 350)
	if err := db.UpsertRodSetting(ctx, rod, "bait_hook:"+bait, hookThird); err != nil {
		t.Fatal(err)
	}
	groups := s.baitedHookSets(ctx, rod)
	if len(groups) != 1 || groups[0].hookGUID != hookThird {
		t.Fatalf("第三枚钩的挂饵分组 = %+v，期望 1 组且钩 = %s", groups, shortGUID(hookThird))
	}
	if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "arm-bite", groups[0].baitGUIDs, false); err != nil {
		t.Fatal(err)
	}
	// rebait 键必须落在钩序号上，补饵遍历才能命中。
	var key string
	if err := db.SQL.QueryRow(`SELECT key FROM rod_setting WHERE guid=? AND key LIKE 'rebait:%'`, rod).Scan(&key); err != nil {
		t.Fatalf("断饵后未写 rebait 记录: %v", err)
	}
	if key != "rebait:2" {
		t.Fatalf("rebait 键 = %q，期望 rebait:2（按钩序号记录）", key)
	}
	if _, err := s.reloadBaitForCast(ctx, 1000001, rod); err != nil {
		t.Fatal(err)
	}
	reloaded := s.baitedHookSets(ctx, rod)
	if len(reloaded) != 1 || reloaded[0].hookGUID != hookThird {
		t.Fatalf("补饵后分组 = %+v，期望钩 %s 重新挂上饵（遍历上限未覆盖钩序号 2 时补不上）",
			reloaded, shortGUID(hookThird))
	}
	if row, err := db.GetItemByGUID(ctx, source); err != nil || row == nil || baitQuantity(row) != 3 {
		t.Fatalf("补饵后来源堆 = %+v，期望剩 3 份（挂 1 吃 1 补 1）", row)
	}
}

// TestMarineArmBreakOneHookKeepsOthers 锁定断件下游丢失的边界：
// 单枚钩被拉断只销毁它自己，其余钩位（含 1000+ 索引）必须原样留在 f5。
// 丢失集合按品类整删是断线/断主线的口径；断单钩走的是 breakComponentTx，
// 两者不能混。多钩上线后这条边界必须继续保持。
func TestMarineArmBreakOneHookKeepsOthers(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := t.Context()
	rod := newArmMultiHookRod(t, s)

	byIndex := map[int64]string{}
	for _, extra := range []int64{250, 1001, 1005, 1009} {
		g := mountArmHook(t, s, rod, extra)
		byIndex[extra] = g
	}
	broken := byIndex[1001]
	out, err := s.breakComponentTx(ctx, 1000001, rod, broken, catHook)
	if err != nil {
		t.Fatalf("断钩失败: %v", err)
	}
	if len(out.RemovedGUIDs) != 1 || out.RemovedGUIDs[0] != broken {
		t.Fatalf("断 1 枚钩的移除集合 = %v，期望只含 %s", out.RemovedGUIDs, shortGUID(broken))
	}
	after := s.storedRigComponents(ctx, rod)
	if n := len(rigCompKeysOfCategory(after, catHook)); n != 3 {
		t.Fatalf("剩余钩 = %d，期望 3", n)
	}
	remaining := map[int64]bool{}
	for _, key := range rigCompKeysOfCategory(after, catHook) {
		remaining[after[key].Extra] = true
	}
	for _, extra := range []int64{250, 1005, 1009} {
		if !remaining[extra] {
			t.Fatalf("钩位 %d 意外丢失，剩余索引 = %v", extra, remaining)
		}
	}
}

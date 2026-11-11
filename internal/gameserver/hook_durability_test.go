package gameserver

import (
	"context"
	"math"
	"testing"
)

// 出厂拟饵钩没有独立 item 行，它随宿主拟饵一起磨损；
// mountedHookDef 必须读宿主拟饵行的耐久，不能读钩对象里不会更新的旧投影。
func TestMountedHookDefReadsEmbeddedLureHookDurability(t *testing.T) {
	s, db := newStaticRigTestServer(t)
	ctx := context.Background()
	uid := int64(1000001)

	rod, err := s.CreateItem(ctx, uid, "spin_10001_S60MH", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	baitTestMount(t, s, rod.GUID, "spinnerbaits_1244", 1, 300)

	lureGUID := s.mountedLureGUID(ctx, rod.GUID)
	if lureGUID == "" {
		t.Fatal("拟饵未挂到竿上")
	}
	lure := mustItem(t, db, lureGUID)
	// 走生产磨损路径：出厂钩没有独立行，随宿主拟饵一起磨损，
	// 变化的是拟饵行耐久；钩对象里的耐久只是首次物化的旧投影。
	if err := db.SetItemDurability(ctx, lure.GUID, 0.4); err != nil {
		t.Fatal(err)
	}

	def, dur := s.mountedHookDef(ctx, rod.GUID)
	if def == nil {
		t.Fatal("mountedHookDef 没有返回钩定义")
	}
	if math.Abs(dur-0.4) > 1e-6 {
		t.Fatalf("内嵌钩耐久 = %v，期望 0.4", dur)
	}
	ids := s.rigGearIDs(ctx, uid, rod.GUID)
	spec := s.loadGearSpecForRig(ctx, ids, rod.GUID)
	want := itemDefF32(def, gearAttrMaxLoad) * 0.4
	if math.Abs(spec.HookStrengthKg-want) > 1e-6 {
		t.Fatalf("HookStrengthKg = %v，期望 %v", spec.HookStrengthKg, want)
	}
}

// 换上的钩有独立 item 行：mountedHookDef 必须读行耐久，而不是树里的旧投影。
func TestMountedHookDefReadsSwappedLureHookRow(t *testing.T) {
	s, db := newStaticRigTestServer(t)
	ctx := context.Background()
	uid := int64(1000001)

	rod, err := s.CreateItem(ctx, uid, "spin_10001_S60MH", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	baitTestMount(t, s, rod.GUID, "spinnerbaits_1244", 1, 300)
	lureGUID := s.mountedLureGUID(ctx, rod.GUID)
	if lureGUID == "" {
		t.Fatal("拟饵未挂到竿上")
	}
	hook, err := s.CreateItem(ctx, uid, "triple_20569", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res := s.lureHookMount(ctx, uid, lureGUID, hook.GUID, 250); res == nil || res.Rejected {
		t.Fatalf("拟饵换钩失败: %+v", res)
	}
	if err := db.SetItemDurability(ctx, hook.GUID, 0.4); err != nil {
		t.Fatal(err)
	}

	def, dur := s.mountedHookDef(ctx, rod.GUID)
	if def == nil {
		t.Fatal("mountedHookDef 没有返回钩定义")
	}
	if math.Abs(dur-0.4) > 1e-6 {
		t.Fatalf("换上钩耐久 = %v，期望 0.4", dur)
	}
	ids := s.rigGearIDs(ctx, uid, rod.GUID)
	spec := s.loadGearSpecForRig(ctx, ids, rod.GUID)
	want := itemDefF32(def, gearAttrMaxLoad) * 0.4
	if math.Abs(spec.HookStrengthKg-want) > 1e-6 {
		t.Fatalf("HookStrengthKg = %v，期望 %v", spec.HookStrengthKg, want)
	}
}

package gameserver

// 机构强度统一口径（reel_strength.json）：规格表收录的轮子，锁轮承力 =
// 出厂规格 × 轴承子件耐久——与客户端轮根字段同源（官方 4/22 实抓：Turion SW
// 30000 满耐久 127.94，磨损后 126.35）；本体耐久归 0（轮子损坏）承力归零；
// 摩擦磨耗只作用于 1~29 档泄力（BaseDragKg 不变）。未收录的轮子回退旧公式
// （基础制动力 × 耐久倍率），行为不变。
//
// 历史注：本文件最初按"机构强度存 def attrs kngaaefkpka、忽略磨损"设计
// （TDD 未完成）；2026-11-11 统一到 reel_strength.json 数据源与官方实测的
// 磨损衰减口径，attrs 导入方案废弃。

import (
	"testing"
)

func TestReelLockedDragUsesOfficialStrength(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := t.Context()
	reel, err := srv.CreateItem(ctx, reviewWearUID, "spin_5274", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	row, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || row == nil {
		t.Fatalf("读轮失败: %v", err)
	}
	// 满轴承耐久：锁轮承力恒等于出厂规格，不吃轴承/齿轮/耐久倍率。
	if got := srv.reelLockedDragFromRow(ctx, row); got != 127.94 {
		t.Fatalf("spin_5274 锁轮承力 = %v, want 127.94", got)
	}
	perf, err := srv.syncReelPerformance(ctx, row)
	if err != nil {
		t.Fatalf("性能表失败: %v", err)
	}
	if perf.LockedDragKg != 127.94 {
		t.Fatalf("性能表 LockedDragKg = %v, want 127.94", perf.LockedDragKg)
	}
	// 摩擦片基准制动仍是定义拖力 36.3（f32 存储有 1e-5 级噪声），供 1~29 档使用。
	if diff := perf.BaseDragKg - 36.3; diff > 0.01 || diff < -0.01 {
		t.Fatalf("BaseDragKg = %v, want 36.3", perf.BaseDragKg)
	}
	// 本体耐久归 0（轮子损坏）承力归零。
	row.Durability = 0
	if got := srv.reelLockedDragFromRow(ctx, row); got != 0 {
		t.Fatalf("损坏轮锁轮承力 = %v, want 0", got)
	}
}

// 官方实抓锚点：同一颗轮磨损后锁轮承力随轴承耐久衰减
// （Turion SW 30000：127.94 × 0.98779 ≈ 126.35；底钓/路亚两颗 Tagara II 6000 同证）。
func TestReelLockedDragScalesWithBearingWear(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := t.Context()
	reel, err := srv.CreateItem(ctx, reviewWearUID, "spin_5274", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	fields, err := FieldsFromJSON(reel.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	bearing, _, _, ok := srv.selectedReelPart(ctx, fields, reelPartCatBearing)
	if !ok || bearing == nil {
		t.Fatal("轮子缺少轴承子件")
	}
	if !setReelChildDurability(bearing, 0.98779) {
		t.Fatal("写轴承耐久失败")
	}
	// 落库：锁轮承力读取的是持久化状态。
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateItemFields(ctx, reel.GUID, fj, reel.Qty, reel.Durability, reel.Workmanship, reel.RigGUID); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || row == nil {
		t.Fatalf("读轮失败: %v", err)
	}
	got := srv.reelLockedDragFromRow(ctx, row)
	if diff := got - 127.94*0.98779; diff > 0.01 || diff < -0.01 {
		t.Fatalf("轴承 0.98779 时锁轮承力 = %v, want %v（官方实抓 126.35）", got, 127.94*0.98779)
	}
}

func TestReelLockedDragDefFallbackWithoutStrength(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := t.Context()
	// 规格表未收录的定义走旧口径：基准 x 耐久倍率（spin_5820 已收录，不能再用）。
	def, err := srv.DB.GetItemDefByItemID(ctx, "old_spinreel")
	if err != nil || def == nil {
		t.Fatalf("读定义失败: %v", err)
	}
	if reelMechStrengthKg(def) > 0 {
		t.Fatalf("old_spinreel 不应带机构强度，得到 %v", reelMechStrengthKg(def))
	}
	ids := rigGearIDs{Reel: def.ItemID, ReelDurability: 0.8}
	base := reelDragMaxKg(def)
	want := base * reelLockMultiplier(0.8)
	if want <= 0 {
		t.Fatalf("回退口径基准异常: base=%v", base)
	}
	if got := srv.reelLockedDragForSpec(ctx, ids, def); got != want {
		t.Fatalf("无机构强度回退 = %v, want %v", got, want)
	}
}

// 规格表收录、但实例行缺失时走定义回退：机构强度 × 本体耐久
// （拿不到轴承子件耐久，本体耐久作代理）。
func TestReelLockedDragDefFallbackUsesStrength(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := t.Context()
	def, err := srv.DB.GetItemDefByItemID(ctx, "spin_5274")
	if err != nil || def == nil {
		t.Fatalf("读定义失败: %v", err)
	}
	ids := rigGearIDs{Reel: def.ItemID, ReelDurability: 0.98779}
	want := 127.94 * 0.98779
	if got := srv.reelLockedDragForSpec(ctx, ids, def); got != want {
		t.Fatalf("机构强度回退 = %v, want %v", got, want)
	}
	ids.ReelDurability = 0
	if got := srv.reelLockedDragForSpec(ctx, ids, def); got != 0 {
		t.Fatalf("损坏轮锁轮承力 = %v, want 0", got)
	}
}

// 规格表数值抽查：锚点 + 周年版的小数位（128.5）原值还原。
func TestReelMechStrengthValues(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := t.Context()
	for itemID, want := range map[string]float64{
		"spin_5274": 127.94, // Turion SW 30000
		"spin_6008": 128.5,  // Turion SW 30000 7-Years
		"spin_6043": 71.2,   // Venga II 6000
	} {
		def, err := srv.DB.GetItemDefByItemID(ctx, itemID)
		if err != nil || def == nil {
			t.Fatalf("读定义 %s 失败: %v", itemID, err)
		}
		if got := reelMechStrengthKg(def); got != want {
			t.Fatalf("%s 机构强度 = %v, want %v", itemID, got, want)
		}
	}
}

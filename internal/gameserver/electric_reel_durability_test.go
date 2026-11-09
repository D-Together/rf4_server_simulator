package gameserver

import (
	"context"
	"math"
	"testing"

	"rf4server/internal/store"
)

func electricReelRow(t *testing.T, srv *Server, db *store.DB, ctx context.Context, itemID string) *store.Item {
	t.Helper()
	reel, err := srv.CreateItem(ctx, wearTestUID, itemID, "backpack", 1)
	if err != nil || reel == nil {
		t.Fatalf("造电轮失败: %v", err)
	}
	return wearReelRow(t, db, ctx, reel.GUID)
}

func electricMechanicalLockForTest(t *testing.T, srv *Server, ctx context.Context, row *store.Item) float64 {
	t.Helper()
	_, bearingQuality, bearingDurability := srv.reelPartStateFromRow(ctx, row, reelPartCatBearing)
	_, gearQuality, gearDurability := srv.reelPartStateFromRow(ctx, row, reelPartCatGear)
	return reelLockedDragKg(
		srv.reelBaseDragFromRow(ctx, row),
		row.Durability,
		reelBearingFactor(bearingQuality, bearingDurability),
		reelGearFactor(gearQuality, gearDurability),
	)
}

func setElectricWasherDragForTest(t *testing.T, srv *Server, db *store.DB, ctx context.Context,
	reelGUID string, dragKg float64) {
	t.Helper()
	row := wearReelRow(t, db, ctx, reelGUID)
	fields := snapshotFields(t, row.FieldsJSON)
	washer, _, _, ok := srv.selectedReelPart(ctx, fields, reelPartCatWasher)
	if !ok {
		t.Fatal("电轮缺少摩擦片")
	}
	idx := fieldIdxByCS(washer.Class, csDragForce)
	if idx < 0 || !setWireFloat(washer.Fields, washer.Class, idx, dragKg) {
		t.Fatal("写摩擦片制动力失败")
	}
	if err := db.UpdateItemFields(ctx, row.GUID, snapshotJSON(t, fields), row.Qty,
		row.Durability, row.Workmanship, row.RigGUID); err != nil {
		t.Fatalf("保存摩擦片制动力失败: %v", err)
	}
}

func replaceElectricMotorForTest(t *testing.T, srv *Server, db *store.DB, ctx context.Context,
	reelGUID, motorItemID string) {
	t.Helper()
	row := wearReelRow(t, db, ctx, reelGUID)
	fields := snapshotFields(t, row.FieldsJSON)
	idx := fieldIdxByCS(row.WireClass, "dfjpnambjcj")
	if idx < 0 || idx >= len(fields) {
		t.Fatal("电轮缺少 f14 电机槽")
	}
	motor, err := srv.buildPartInstance(ctx, row, motorItemID, "dfjpnambjcj")
	if err != nil || motor == nil {
		t.Fatalf("构造电机失败: %v", err)
	}
	setReelChildDurability(motor, 1)
	fields[idx].Val = motor
	if err := db.UpdateItemFields(ctx, row.GUID, snapshotJSON(t, fields), row.Qty,
		row.Durability, row.Workmanship, row.RigGUID); err != nil {
		t.Fatalf("保存电机失败: %v", err)
	}
}

func TestElectricReelLockedDragUsesLiveBrakeAndMotor(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	row := electricReelRow(t, srv, db, ctx, "elcon_6003")

	perf, err := srv.syncReelPerformance(ctx, row)
	if err != nil || perf == nil {
		t.Fatalf("同步电轮性能失败: %v", err)
	}
	fields := snapshotFields(t, row.FieldsJSON)
	mechanical := electricMechanicalLockForTest(t, srv, ctx, row)
	_, _, bearingDurability := srv.reelPartStateFromRow(ctx, row, reelPartCatBearing)
	_, bearingQuality, _ := srv.reelPartStateFromRow(ctx, row, reelPartCatBearing)
	_, gearQuality, gearDurability := srv.reelPartStateFromRow(ctx, row, reelPartCatGear)
	motor, _, found := srv.reelMotorCapacityInFields(ctx, row, fields,
		reelBearingFactor(bearingQuality, bearingDurability),
		reelGearFactor(gearQuality, gearDurability))
	if !found || motor <= 0 {
		t.Fatal("电轮应有有效电机承力")
	}
	want := srv.electricLockedMultiplier() * math.Min(mechanical, motor)
	if math.Abs(perf.LockedDragKg-want) > 1e-6 {
		t.Fatalf("电轮锁轮值 %.6f，期望 3×min(机械 %.6f, 电机 %.6f)=%.6f",
			perf.LockedDragKg, mechanical, motor, want)
	}
}

func TestElectricReelLockedDragTracksPartChangesWithoutCompounding(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	row := electricReelRow(t, srv, db, ctx, "elcon_5968")
	reelGUID := row.GUID

	// 把摩擦片数值抬高，让电机先成为限制项。
	setElectricWasherDragForTest(t, srv, db, ctx, reelGUID, 1000)
	first, err := srv.syncReelPerformance(ctx, wearReelRow(t, db, ctx, reelGUID))
	if err != nil || first == nil {
		t.Fatalf("首次同步失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, serr := srv.syncReelPerformance(ctx, wearReelRow(t, db, ctx, reelGUID))
		if serr != nil || again == nil {
			t.Fatalf("第 %d 次重复同步失败: %v", i+2, serr)
		}
		if math.Abs(again.LockedDragKg-first.LockedDragKg) > 1e-9 {
			t.Fatalf("重复同步发生复利：%.6f -> %.6f", first.LockedDragKg, again.LockedDragKg)
		}
	}

	replaceElectricMotorForTest(t, srv, db, ctx, reelGUID, "rengine_2")
	upgraded, err := srv.syncReelPerformance(ctx, wearReelRow(t, db, ctx, reelGUID))
	if err != nil || upgraded == nil {
		t.Fatalf("换电机后同步失败: %v", err)
	}
	if upgraded.LockedDragKg <= first.LockedDragKg+1e-6 {
		t.Fatalf("换更强电机后锁轮值应上升：%.6f -> %.6f",
			first.LockedDragKg, upgraded.LockedDragKg)
	}

	// 机械承力成为限制项时，修改摩擦片也必须实时反映到 3 倍后的锁轮值。
	setElectricWasherDragForTest(t, srv, db, ctx, reelGUID, 10)
	lowBrake, err := srv.syncReelPerformance(ctx, wearReelRow(t, db, ctx, reelGUID))
	if err != nil || lowBrake == nil {
		t.Fatalf("低制动同步失败: %v", err)
	}
	setElectricWasherDragForTest(t, srv, db, ctx, reelGUID, 20)
	highBrake, err := srv.syncReelPerformance(ctx, wearReelRow(t, db, ctx, reelGUID))
	if err != nil || highBrake == nil {
		t.Fatalf("高制动同步失败: %v", err)
	}
	if highBrake.LockedDragKg <= lowBrake.LockedDragKg+1e-6 {
		t.Fatalf("加强摩擦片后锁轮值应上升：%.6f -> %.6f",
			lowBrake.LockedDragKg, highBrake.LockedDragKg)
	}
}

func TestElectricReelMotorDurabilityAggregatesWithoutFuse(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	row := electricReelRow(t, srv, db, ctx, "elcon_6003")

	fields := snapshotFields(t, row.FieldsJSON)
	before, ok := srv.reelBodyDurabilityInFields(ctx, row, fields)
	if !ok || before <= 0 {
		t.Fatal("电轮应能聚合本体耐久")
	}
	motor, _, _, found := srv.selectedReelPart(ctx, fields, reelPartCatMotor)
	if !found {
		t.Fatal("电轮缺少电机")
	}
	setReelChildDurability(motor, 0.2)
	after, ok := srv.reelBodyDurabilityInFields(ctx, row, fields)
	if !ok || after >= before {
		t.Fatalf("电机磨损后本体耐久应下降：%.6f -> %.6f", before, after)
	}
	if after <= 0 {
		t.Fatalf("电机归零前不应作为熔断件：%.6f", after)
	}

	setReelChildDurability(motor, 0)
	zero, ok := srv.reelBodyDurabilityInFields(ctx, row, fields)
	if !ok || zero <= 0 {
		t.Fatalf("电机归零不能直接把整轮判死，实得 %.6f", zero)
	}
	_, bearingQuality, bearingDurability := srv.reelPartStateFromRow(ctx, row, reelPartCatBearing)
	_, gearQuality, gearDurability := srv.reelPartStateFromRow(ctx, row, reelPartCatGear)
	mechanical := electricMechanicalLockForTest(t, srv, ctx, row)
	locked := srv.reelEffectiveLockedDragInFields(ctx, row, fields, srv.reelBaseDragFromRow(ctx, row),
		row.Durability,
		reelBearingFactor(bearingQuality, bearingDurability),
		reelGearFactor(gearQuality, gearDurability))
	if math.Abs(locked-mechanical) > 1e-9 {
		t.Fatalf("电机归零后应退回机械锁轮：%.6f vs %.6f", locked, mechanical)
	}
}

func TestElectricReelMotorWearNeedsLoadAndRepairsToFull(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	row := electricReelRow(t, srv, db, ctx, "elcon_6003")
	fields := snapshotFields(t, row.FieldsJSON)

	if worn := srv.wearReelPartsInFields(ctx, fields, reelPartCounts{Rods: 1}); len(worn) > 0 {
		if _, hasMotor := worn["rengine_2"]; hasMotor {
			t.Fatal("普通抛投基准不应消耗电机")
		}
	}
	motor, _, _, ok := srv.selectedReelPart(ctx, fields, reelPartCatMotor)
	if !ok {
		t.Fatal("电轮缺少电机")
	}
	motorBefore := reelChildDurability(motor)
	worn := srv.wearReelPartsInFields(ctx, fields, reelPartCounts{OverloadSecs: 10, SlipTicks: 5})
	if worn["rengine_2"] <= 0 {
		t.Fatalf("锁轮过载与打滑应消耗电机，磨损表=%v", worn)
	}
	if motorAfter := reelChildDurability(motor); motorAfter >= motorBefore {
		t.Fatalf("电机耐久应下降：%.6f -> %.6f", motorBefore, motorAfter)
	}

	if changed, err := srv.restoreReelInternalsInFields(ctx, row, fields); err != nil || !changed {
		t.Fatalf("整机修理应恢复电机，changed=%v err=%v", changed, err)
	}
	if got := reelChildDurability(motor); got != 1 {
		t.Fatalf("修理后电机耐久应回满，实得 %.6f", got)
	}
}

func TestNormalReelLockedDragDoesNotGetElectricMultiplier(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	reel, err := srv.CreateItem(ctx, wearTestUID, "spin_5820", "backpack", 1)
	if err != nil || reel == nil {
		t.Fatalf("造普通轮失败: %v", err)
	}
	row := wearReelRow(t, db, ctx, reel.GUID)
	mechanical := electricMechanicalLockForTest(t, srv, ctx, row)
	locked := srv.reelLockedDragFromRow(ctx, row)
	if math.Abs(locked-mechanical) > 1e-9 {
		t.Fatalf("普通轮不应吃电轮倍率：锁轮 %.6f 机械 %.6f", locked, mechanical)
	}
}

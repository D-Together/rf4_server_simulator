package gameserver

// 工坊诊断（21/2）对摩擦片的完好度必须以制动力为准。
//
// 摩擦片的权威状态是制动力 csDragForce，csDurability 只是它的显示镜像
// （见 degradeFrictionWasherFields）。磨穿的摩擦片制动力为 0，但镜像耐久
// 可能仍停在 1.0；若诊断直接读镜像，f1 会报"100% 完好"，与 f3 报价和
// 客户端显示的"损耗"自相矛盾。

import (
	"math"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

func TestWorkshopDiagWasherConditionFollowsDragForce(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := t.Context()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}

	// 造一只"制动力已烧穿、但镜像耐久还是 1.0"的轮子——真实坏档就是这个形态。
	reel, _ := seedWornWorkshopReel(t, srv, db, 1, 0, 1, 1)
	fields := snapshotFields(t, reel.FieldsJSON)
	washer, washerDef, _, ok := srv.selectedReelPart(ctx, fields, reelPartCatWasher)
	if !ok || washerDef == nil {
		t.Fatal("轮内缺少摩擦片")
	}
	setWireFloat(washer.Fields, washer.Class, fieldIdxByCS(washer.Class, csDragForce), 0)
	setReelChildDurability(washer, 1) // 镜像故意留在 1.0
	if err := db.UpdateItemFields(ctx, reel.GUID, snapshotJSON(t, fields),
		reel.Qty, reel.Durability, reel.Workmanship, reel.RigGUID); err != nil {
		t.Fatal(err)
	}

	diagBody, ok := hWorkshopDiag(&Ctx{
		Sess: newProbeSess(srv),
		Tree: proto.Args524(proto.Guid(reel.GUID)),
		Log:  logging.Discard(),
	})
	if !ok || len(diagBody) == 0 {
		t.Fatal("21/2 未返回诊断")
	}
	diag, _, err := proto.DecodeValue(diagBody, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	comps := diag.Field(1).Val
	if comps == nil || comps.ArgCount() == 0 {
		t.Fatal("21/2 缺少内件清单")
	}

	washerGUID := frictionWasherGUID(t, srv, reel.GUID)
	for _, item := range comps.Items {
		guidField := item.Field(0)
		if guidField.GuidHex() != washerGUID {
			continue
		}
		condField := item.Field(1)
		got := proto.F32FromBits(condField.U32())
		// 制动力 0 / 额定 → 完好度 0。旧实现读镜像会报 1.0。
		if math.Abs(float64(got)-0) > 1e-3 {
			t.Fatalf("摩擦片完好度 f1=%v，期望 0（按制动力报告）", got)
		}
		// 报价仍由 21/2 的整机保养口径覆盖，与服务端一致，不受本改动影响。
		price := item.Field(3).Val
		if price == nil {
			t.Fatal("摩擦片缺少价格对象")
		}
		quoteField := price.Field(0)
		if quoteField.U64() == 0 {
			t.Fatal("摩擦片报价应非 0")
		}
		return
	}
	t.Fatal("21/2 内件清单中没有摩擦片")
}

// 对照：未磨损的摩擦片仍报完好度 1.0，本改动不会把正常件报成磨损。
func TestWorkshopDiagWasherConditionHealthyUnchanged(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := t.Context()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	reel, _ := seedWornWorkshopReel(t, srv, db, 1, 1, 1, 1)

	diagBody, ok := hWorkshopDiag(&Ctx{
		Sess: newProbeSess(srv),
		Tree: proto.Args524(proto.Guid(reel.GUID)),
		Log:  logging.Discard(),
	})
	if !ok || len(diagBody) == 0 {
		t.Fatal("21/2 未返回诊断")
	}
	diag, _, err := proto.DecodeValue(diagBody, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	comps := diag.Field(1).Val
	washerGUID := frictionWasherGUID(t, srv, reel.GUID)
	for _, item := range comps.Items {
		guidField := item.Field(0)
		if guidField.GuidHex() != washerGUID {
			continue
		}
		condField := item.Field(1)
		got := proto.F32FromBits(condField.U32())
		if math.Abs(float64(got)-1) > 1e-3 {
			t.Fatalf("完好摩擦片 f1=%v，期望 1", got)
		}
		return
	}
	t.Fatal("21/2 内件清单中没有摩擦片")
}

// 回归：额定制动力必须乘母轮工艺质量。否则工艺 0.8 的正常摩擦片会被
// 误报成 80% 完好，而磨损账本实际以 rated * Workmanship 为满值。
func TestWorkshopDiagWasherConditionHonorsReelWorkmanship(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := t.Context()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	reel, _ := seedWornWorkshopReel(t, srv, db, 1, .8, 1, 1)
	reel.Workmanship = .8
	if err := db.UpdateItemFields(ctx, reel.GUID, reel.FieldsJSON,
		reel.Qty, reel.Durability, reel.Workmanship, reel.RigGUID); err != nil {
		t.Fatal(err)
	}

	diagBody, ok := hWorkshopDiag(&Ctx{
		Sess: newProbeSess(srv),
		Tree: proto.Args524(proto.Guid(reel.GUID)),
		Log:  logging.Discard(),
	})
	if !ok || len(diagBody) == 0 {
		t.Fatal("21/2 未返回诊断")
	}
	diag, _, err := proto.DecodeValue(diagBody, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	washerGUID := frictionWasherGUID(t, srv, reel.GUID)
	for _, item := range diag.Field(1).Val.Items {
		guidField := item.Field(0)
		if guidField.GuidHex() != washerGUID {
			continue
		}
		condField := item.Field(1)
		got := proto.F32FromBits(condField.U32())
		if math.Abs(float64(got)-1) > 1e-3 {
			t.Fatalf("工艺 0.8 的正常摩擦片 f1=%v，期望 1", got)
		}
		return
	}
	t.Fatal("21/2 内件清单中没有摩擦片")
}

// frictionWasherGUID 取轮内摩擦片子实例的 guid。
func frictionWasherGUID(t *testing.T, srv *Server, reelGUID string) string {
	t.Helper()
	row := mustItem(t, srv.DB, reelGUID)
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	child, _, _, ok := srv.selectedReelPart(t.Context(), fields, reelPartCatWasher)
	if !ok || child == nil {
		t.Fatal("缺少摩擦片")
	}
	idx := fieldIdxByCS(child.Class, csGUID)
	if idx < 0 || idx >= child.FieldCount() {
		t.Fatal("摩擦片缺少 guid 字段")
	}
	return child.Fields[idx].Hex
}

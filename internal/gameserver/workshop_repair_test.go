package gameserver

import (
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func snapshotWorkshopRequestPart(guid string, category uint32, systemID string) *proto.Value {
	return proto.Args524(proto.StrVal("workshop_01"), wiregen.MustMakeObj("nfmkdcbnkhp", map[int]*proto.Value{
		0: proto.Guid(guid),
		1: proto.Args(proto.S("503"), wiregen.MustMakeObj("gcllbdkgkna", map[int]*proto.Value{
			0: proto.U32(category),
			1: proto.StrVal(systemID),
		})),
		2: proto.U32(0),
		3: proto.U64(100),
	}))
}

func TestWorkshopSameModelRepairRestoresItemDurability(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	ctx := t.Context()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	fields := snapshotFields(t, rod.FieldsJSON)
	part, partDef := snapshotPart(t, srv, fields, 4)
	if part == nil || partDef == nil {
		t.Fatal("竿柄部件缺失")
	}
	setWireFloat(part.Fields, part.Class, fieldIdxByCS(part.Class, csDurability), .4)
	raw := snapshotJSON(t, fields)
	rod.FieldsJSON = raw
	if err := db.UpdateItemFields(ctx, rod.GUID, raw, rod.Qty, .5, rod.Workmanship, rod.RigGUID); err != nil {
		t.Fatalf("预置竿状态失败: %v", err)
	}

	c := &Ctx{
		Sess: newProbeSess(srv), Main: 21, Sub: 3,
		Tree: snapshotWorkshopRequestPart(rod.GUID, 4, partDef.ItemID),
		Log:  logging.Discard(),
	}
	body, ok := hWorkshopOrder(c)
	if !ok || len(body) == 0 {
		t.Fatal("同型号竿柄修理失败")
	}
	fresh := mustItem(t, db, rod.GUID)
	if fresh.Durability != 1 {
		t.Fatalf("同型号修理后物品本体耐久应回 1，实得 %v", fresh.Durability)
	}
	freshFields := snapshotFields(t, fresh.FieldsJSON)
	repairedPart, _ := snapshotPart(t, srv, freshFields, 4)
	if got := snapshotFloat(repairedPart.Fields, repairedPart.Class, csDurability); got != 1 {
		t.Fatalf("同型号修理后竿柄耐久应回 1，实得 %v", got)
	}
}

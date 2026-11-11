package gameserver

import (
	"context"
	"math"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// 竿架 f1 的线上形状：[0x03]["369"][u16 n] + n×([0x01][u32 11502] + 7 字节)。
func rodPodSlotWire(enabled, color, sound uint64, volumeBits uint32) []byte {
	out := []byte{0x01, 0xEE, 0x2C, 0x00, 0x00}
	out = append(out, byte(enabled), byte(color), byte(sound))
	return append(out,
		byte(volumeBits), byte(volumeBits>>8), byte(volumeBits>>16), byte(volumeBits>>24))
}

func TestRodPodDefaultAlarmSlots(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := context.Background()
	row, err := srv.CreateItem(ctx, 1000001, "rod_pod_03r", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := srv.ItemToTree(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(rodPodWireClass, rodPodSlotsCS)
	if idx < 0 {
		t.Fatal("abmokghdbhf 缺少 cfpajgilkng")
	}
	arr := tree.Fields[idx].Val
	if arr == nil || arr.Kind != proto.KindTypedArr || arr.TypeName != rodPodSlotName || len(arr.Items) != 3 {
		t.Fatalf("竿位设置不是 3 个元素：%+v", arr)
	}
	for i, it := range arr.Items {
		if it.Kind != proto.KindObj || it.TypeID != rodPodSlotTypeID || len(it.Fields) != 4 {
			t.Fatalf("第 %d 个竿位设置形状不对：%+v", i, it)
		}
	}
	raw, err := proto.EncodeValue(arr, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x03, byte(len(rodPodSlotName))}
	want = append(want, []byte(rodPodSlotName)...)
	want = append(want, 0x03, 0x00)
	for i := 0; i < 3; i++ {
		want = append(want, rodPodSlotWire(1, 0, 0, math.Float32bits(0.5))...)
	}
	if len(raw) != len(want) {
		t.Fatalf("f1 长度 %d，期望 %d", len(raw), len(want))
	}
	for i := range want {
		if raw[i] != want[i] {
			t.Fatalf("f1 第 %d 字节 0x%02x，期望 0x%02x", i, raw[i], want[i])
		}
	}
	// 服务端解码器把 typed array 读成 args(类型名)，元素仍是 4 字段对象。
	decoded, n, err := proto.DecodeValue(raw, 0, srv.Table())
	if err != nil || n != len(raw) {
		t.Fatalf("f1 回读失败: %v", err)
	}
	if decoded.Kind != proto.KindArgs || decoded.Code == nil || *decoded.Code != rodPodSlotName || len(decoded.Items) != 3 {
		t.Fatalf("f1 回读形状不对：%+v", decoded)
	}
	if decoded.Items[0].Kind != proto.KindObj || decoded.Items[0].TypeID != rodPodSlotTypeID {
		t.Fatalf("元素不是竿位设置对象：%+v", decoded.Items[0])
	}
}

func TestRodPodSlotCountByModel(t *testing.T) {
	cases := map[string]int{
		"rod_pod":     1,
		"rod_pod_2":   1,
		"rod_pod_02":  2,
		"rod_pod_03r": 3,
		"rod_pod_03g": 3,
	}
	for itemID, want := range cases {
		if got := rodPodSlotCount(itemID, 0); got != want {
			t.Fatalf("%s 竿位数 %d，期望 %d", itemID, got, want)
		}
	}
}

func TestRodPodSetSlotPersists(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	row, err := srv.CreateItem(ctx, 1000001, "rod_pod_03r", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	settings := proto.Obj(rodPodSlotCodec, rodPodSlotTypeID,
		proto.FNum("OPT", 1),
		proto.FNum("BYTE", 3),
		proto.FNum("BYTE", 7),
		proto.FNum("U32", uint64(math.Float32bits(0.9))),
	)
	tree := proto.Args524(proto.U32(0), proto.Guid(row.GUID), proto.U32(1), settings)
	if _, ok := hRodPodSetSlot(&Ctx{Sess: newProbeSess(srv), Tree: tree, Log: logging.Discard()}); !ok {
		t.Fatal("18/1 未处理")
	}
	stored, err := db.GetItemByGUID(ctx, row.GUID)
	if err != nil || stored == nil {
		t.Fatal("竿架读回失败")
	}
	fields, err := FieldsFromJSON(stored.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	arr := fields[fieldIdxByCS(row.WireClass, rodPodSlotsCS)].Val
	if arr == nil || arr.Kind != proto.KindTypedArr || len(arr.Items) != 3 {
		t.Fatalf("落库的 f1 不对：%+v", arr)
	}
	got := arr.Items[1]
	if got.Fields[0].Num == nil || *got.Fields[0].Num != 1 ||
		*got.Fields[1].Num != 3 || *got.Fields[2].Num != 7 ||
		*got.Fields[3].Num != uint64(math.Float32bits(0.9)) {
		t.Fatalf("竿位 1 设置没落库：%+v", got.Fields)
	}
	if arr.Items[0].Kind != proto.KindObj || *arr.Items[0].Fields[1].Num != 0 {
		t.Fatalf("其他竿位被改动：%+v", arr.Items[0])
	}
	// 重新装载物品树仍然是 3 个竿位，且第 1 位是刚写入的设置。
	tree2, err := srv.ItemToTree(ctx, stored)
	if err != nil {
		t.Fatal(err)
	}
	arr2 := tree2.Fields[fieldIdxByCS(row.WireClass, rodPodSlotsCS)].Val
	if arr2.Kind != proto.KindTypedArr || len(arr2.Items) != 3 || *arr2.Items[1].Fields[1].Num != 3 {
		t.Fatalf("重新装载后竿位设置丢失：%+v", arr2)
	}
}

func TestRodPodSetMasterPersists(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	row, err := srv.CreateItem(ctx, 1000001, "rod_pod_03r", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	tree := proto.Args524(proto.U32(0), proto.Guid(row.GUID), proto.U32(4))
	if _, ok := hRodPodSetMaster(&Ctx{Sess: newProbeSess(srv), Tree: tree, Log: logging.Discard()}); !ok {
		t.Fatal("18/2 未处理")
	}
	stored, err := db.GetItemByGUID(ctx, row.GUID)
	if err != nil || stored == nil {
		t.Fatal("竿架读回失败")
	}
	fields, err := FieldsFromJSON(stored.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	f0 := fields[fieldIdxByCS(row.WireClass, rodPodMasterCS)].Num
	if f0 == nil || *f0 != 4 {
		t.Fatalf("f0 没落库：%+v", f0)
	}
}

func TestRodPodSetSlotRejectsForeignItem(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	row, err := srv.CreateItem(ctx, 2000002, "rod_pod_03r", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	before := mustItem(t, db, row.GUID).FieldsJSON
	settings := proto.Obj(rodPodSlotCodec, rodPodSlotTypeID,
		proto.FNum("OPT", 1), proto.FNum("BYTE", 1), proto.FNum("BYTE", 1),
		proto.FNum("U32", 0))
	tree := proto.Args524(proto.U32(0), proto.Guid(row.GUID), proto.U32(0), settings)
	hRodPodSetSlot(&Ctx{Sess: newProbeSess(srv), Tree: tree, Log: logging.Discard()})
	if after := mustItem(t, db, row.GUID).FieldsJSON; after != before {
		t.Fatal("改了别人竿架的设置")
	}
}

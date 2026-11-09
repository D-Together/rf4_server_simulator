package gameserver

import (
	"context"
	"testing"
)

// 拟饵出厂钩没有独立 item 行，17/9 上报的是拟饵树里的内嵌 hook GUID。
// 这种断件必须通过竿归属校验，并落到“断整个拟饵”的既有终端件处理。
func TestEmbeddedLureHookBreakIsAccepted(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	uid := int64(7247281)
	sess := newProbeSess(srv)
	sess.Userid = uid

	rod, err := srv.CreateItem(ctx, uid, "spin_10158_M100HH", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	reel, err := srv.CreateItem(ctx, uid, "spin_5691", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "braid_40898", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	leader, err := srv.CreateItem(ctx, uid, "leader_crafted_60", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	lure, err := srv.CreateItem(ctx, uid, "plug_w_2370", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, itemGUID := range []string{reel.GUID, line.GUID, leader.GUID, lure.GUID} {
		mountGearForTest(t, srv, sess, rod.GUID, itemGUID)
	}

	lureRow := mustItem(t, db, lure.GUID)
	tree, err := srv.ItemToTree(ctx, lureRow)
	if err != nil {
		t.Fatal(err)
	}
	embeddedHook := ""
	for _, guid := range deepGUIDs(tree) {
		if guid == "" || guid == zeroGUID || guid == lure.GUID {
			continue
		}
		if row, _ := db.GetItemByGUID(ctx, guid); row == nil {
			embeddedHook = guid
			break
		}
	}
	if embeddedHook == "" {
		t.Fatal("拟饵树里没有找到内嵌钩 GUID")
	}
	report, err := srv.validateBreakReport(ctx, uid, rod.GUID, embeddedHook)
	if err != nil {
		t.Fatalf("内嵌拟饵钩应通过断件归属校验: %v", err)
	}
	if !report.embedded {
		t.Fatalf("内嵌钩应标记 embedded: %+v", report)
	}

	outcome, err := srv.breakLineOrRod(ctx, uid, rod.GUID, embeddedHook)
	if err != nil {
		t.Fatalf("内嵌拟饵钩断件处理失败: %v", err)
	}
	if outcome == nil || !outcome.ComponentBroken || outcome.BreakPart != "component" {
		t.Fatalf("内嵌钩应断开拟饵终端件: %+v", outcome)
	}
	if row, _ := db.GetItemByGUID(ctx, lure.GUID); row != nil {
		t.Fatal("拟饵出厂钩断裂后拟饵仍存在")
	}
	if row, _ := db.GetItemByGUID(ctx, rod.GUID); row == nil || row.Durability <= 0 {
		t.Fatalf("断拟饵不应损坏鱼竿: %+v", row)
	}
}

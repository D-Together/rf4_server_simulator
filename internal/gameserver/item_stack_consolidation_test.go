package gameserver

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

func TestConsolidateStacksRedirectsAliasOnlyShortcuts(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%v", rollback), func(t *testing.T) {
			srv, db := newEconomyTxTestServer(t)
			const uid = int64(4631)
			seedEconomyItemDef(t, db, 994031, "alias_food", 26)
			keep := economyTestItem(t, srv, uid, "alias_food", "backpack", 2)
			drop := economyTestItem(t, srv, uid, "alias_food", "backpack", 3)
			other := economyTestItem(t, srv, uid+1, "alias_food", "backpack", 1)
			setStackOrder(t, db, keep, drop)
			key := fmt.Sprintf("shortcut-aliases:%d", uid)
			raw, err := json.Marshal(map[string]string{"4": drop.GUID, "5": drop.GUID, "6": keep.GUID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`INSERT INTO kv(k,v) VALUES(?,?),(?,?)`, key, string(raw), fmt.Sprintf("shortcut-aliases:%d", uid+1), fmt.Sprintf(`{"4":"%s"}`, other.GUID)); err != nil {
				t.Fatal(err)
			}
			if rollback {
				if _, err := db.SQL.Exec(`CREATE TRIGGER fail_alias_stack_delete BEFORE DELETE ON item BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
					t.Fatal(err)
				}
			}
			removed, err := srv.ConsolidateNonReturnableItemStacks(t.Context())
			if rollback {
				if err == nil || removed != 0 {
					t.Fatalf("未回滚: removed=%d err=%v", removed, err)
				}
			} else if err != nil || removed != 1 {
				t.Fatalf("整理失败: removed=%d err=%v", removed, err)
			}
			var gotRaw string
			if err := db.SQL.QueryRow(`SELECT v FROM kv WHERE k=?`, key).Scan(&gotRaw); err != nil {
				t.Fatal(err)
			}
			var got map[string]string
			if err := json.Unmarshal([]byte(gotRaw), &got); err != nil {
				t.Fatal(err)
			}
			want := keep.GUID
			if rollback {
				want = drop.GUID
			}
			if len(got) != 3 || got["4"] != want || got["5"] != want || got["6"] != keep.GUID {
				t.Fatalf("别名未保留: %v", got)
			}
			if err := db.SQL.QueryRow(`SELECT v FROM kv WHERE k=?`, fmt.Sprintf("shortcut-aliases:%d", uid+1)).Scan(&gotRaw); err != nil {
				t.Fatal(err)
			}
			if gotRaw != fmt.Sprintf(`{"4":"%s"}`, other.GUID) {
				t.Fatalf("改动其他玩家别名: %s", gotRaw)
			}
			if !rollback {
				if n, err := srv.ConsolidateNonReturnableItemStacks(t.Context()); err != nil || n != 0 {
					t.Fatalf("不幂等: %d %v", n, err)
				}
			}
		})
	}
}

func markLegacyStackPurchase(t *testing.T, srv *Server, uid int64, row *store.Item) {
	t.Helper()
	tx, err := beginEconomyTx(context.Background(), srv.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := srv.saveStorePurchaseTx(context.Background(), tx, uid, "legacy_store", row.ItemID, 10, false, []*store.Item{row}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func setStackOrder(t *testing.T, db *store.DB, ordered ...*store.Item) {
	t.Helper()
	for slot, row := range ordered {
		if _, err := db.SQL.Exec(`UPDATE item SET slot=? WHERE guid=?`, slot, row.GUID); err != nil {
			t.Fatal(err)
		}
		row.Slot = int64(slot)
	}
}

func TestConsolidateNonReturnableItemStacksPreservesBoundariesAndReferences(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	const (
		uid    = int64(4620)
		foodID = "legacy_food"
		baitID = "legacy_bait"
		hookID = "legacy_hook"
	)
	seedEconomyItemDef(t, db, 994001, foodID, 26)
	seedEconomyItemDef(t, db, 994002, baitID, 27)
	seedEconomyItemDef(t, db, 994003, hookID, 14)

	foodKeep := economyTestItem(t, srv, uid, foodID, "backpack", 2)
	foodDrop := economyTestItem(t, srv, uid, foodID, "backpack", 3)
	baitKeep := economyTestItem(t, srv, uid, baitID, "backpack", 4)
	baitDrop := economyTestItem(t, srv, uid, baitID, "backpack", 5)
	houseKeep := economyTestItem(t, srv, uid, foodID, "house", 7)
	houseDrop := economyTestItem(t, srv, uid, foodID, "house", 11)
	hookA := economyTestItem(t, srv, uid, hookID, "backpack", 2)
	hookB := economyTestItem(t, srv, uid, hookID, "backpack", 3)
	mountedA := economyTestItem(t, srv, uid, baitID, "backpack", 1)
	mountedB := economyTestItem(t, srv, uid, baitID, "backpack", 1)
	setStackOrder(t, db, foodKeep, foodDrop)
	setStackOrder(t, db, baitKeep, baitDrop)
	setStackOrder(t, db, houseKeep, houseDrop)
	if _, err := db.SQL.Exec(`UPDATE item SET container='rig',rig_guid=? WHERE guid=?`, "rod-a", mountedA.GUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE item SET container='rig',rig_guid=? WHERE guid=?`, "rod-b", mountedB.GUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO container_slot(uid,container,slot,guid) VALUES(?,0,7,?)`, uid, foodDrop.GUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO drink_bonus_effect(uid,category,peak,remaining_millis,duration_millis,source_guid)
		VALUES(?,'food',0.2,1000,1000,?)`, uid, foodDrop.GUID); err != nil {
		t.Fatal(err)
	}
	markLegacyStackPurchase(t, srv, uid, foodDrop)

	removed, err := srv.ConsolidateNonReturnableItemStacks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 3 {
		t.Fatalf("删除重复堆=%d，期望 3", removed)
	}
	for _, tc := range []struct {
		item, container, guid string
		rows, qty             int64
	}{
		{foodID, "backpack", foodKeep.GUID, 1, 5},
		{baitID, "backpack", baitKeep.GUID, 1, 9},
		{foodID, "house", houseKeep.GUID, 1, 18},
		{hookID, "backpack", "", 2, hookA.Qty + hookB.Qty},
		{baitID, "rig", "", 2, mountedA.Qty + mountedB.Qty},
	} {
		rows, qty, guid := stackingTestStats(t, db, uid, tc.item, tc.container)
		if rows != tc.rows || qty != tc.qty || tc.guid != "" && guid != tc.guid {
			t.Errorf("%s/%s: rows=%d qty=%d guid=%s，期望 %d/%d/%s", tc.item, tc.container, rows, qty, guid, tc.rows, tc.qty, tc.guid)
		}
	}
	var shortcut, source string
	if err := db.SQL.QueryRow(`SELECT guid FROM container_slot WHERE uid=? AND container=0 AND slot=7`, uid).Scan(&shortcut); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT source_guid FROM drink_bonus_effect WHERE uid=? AND category='food'`, uid).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if shortcut != foodKeep.GUID || source != foodKeep.GUID {
		t.Fatalf("存量引用未重定向: shortcut=%s source=%s want=%s", shortcut, source, foodKeep.GUID)
	}
	kept, err := db.GetItemByGUID(context.Background(), foodKeep.GUID)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := FieldsFromJSON(kept.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	origin := fieldIdxByCS(kept.WireClass, csOrigin)
	if kept.BuyPrice != 0 || origin >= 0 && origin < len(fields) && fields[origin].Val != nil && fields[origin].Val.Kind != proto.KindNull {
		t.Fatalf("合并堆仍携带失效退货元数据: buy_price=%d", kept.BuyPrice)
	}
	var receipts int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM kv WHERE k LIKE ?`, fmt.Sprintf("store-purchase:%d:%%", uid)).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("旧退货凭据未清理: %d", receipts)
	}
	if _, err := db.SQL.Exec(`CREATE TRIGGER reject_clean_stack_rewrite BEFORE DELETE ON container_slot
		BEGIN SELECT RAISE(ABORT,'clean stack rewritten'); END`); err != nil {
		t.Fatal(err)
	}
	if again, err := srv.ConsolidateNonReturnableItemStacks(context.Background()); err != nil || again != 0 {
		t.Fatalf("重复整理不幂等: removed=%d err=%v", again, err)
	}
}

func TestConsolidateNonReturnableItemStacksPreservesRecipeBackedGroundbaitInstances(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	const (
		uid    = int64(4622)
		feedID = "recipe_feedcompleted"
		pvaID  = "recipe_pva_stick_s"
	)
	seedEconomyItemDef(t, db, 994021, feedID, 46)
	seedEconomyItemDef(t, db, 994022, pvaID, 30)

	feedAnimal := economyTestItem(t, srv, uid, feedID, "backpack", 60)
	feedPlant := economyTestItem(t, srv, uid, feedID, "backpack", 60)
	pvaAnimal := economyTestItem(t, srv, uid, pvaID, "backpack", 10)
	pvaPlant := economyTestItem(t, srv, uid, pvaID, "backpack", 10)
	setStackOrder(t, db, feedAnimal, feedPlant)
	setStackOrder(t, db, pvaAnimal, pvaPlant)

	recipes := map[string]string{
		feedAnimal.GUID: `["feedadds_bloodworms"]`,
		feedPlant.GUID:  `["feedadds_corn"]`,
		pvaAnimal.GUID:  `["feedadds_casters"]`,
		pvaPlant.GUID:   `["feedadds_pelets"]`,
	}
	for guid, mats := range recipes {
		if _, err := db.SQL.Exec(`INSERT INTO groundbait_recipe(instance_guid,mats_json,created_ts) VALUES(?,?,1)`, guid, mats); err != nil {
			t.Fatal(err)
		}
	}
	for _, itemID := range []string{feedID, pvaID} {
		rows, _, _ := stackingTestStats(t, db, uid, itemID, "backpack")
		if rows != 2 {
			t.Fatalf("测试前置错误：%s 实例数=%d，期望 2", itemID, rows)
		}
	}
	var recipeRows int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM groundbait_recipe`).Scan(&recipeRows); err != nil || recipeRows != 4 {
		t.Fatalf("测试前置错误：配方行=%d err=%v", recipeRows, err)
	}

	removed, err := srv.ConsolidateNonReturnableItemStacks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("带逐实例配方的窝料/PVA 不得被整理删除: removed=%d", removed)
	}
	for _, tc := range []struct {
		item string
		qty  int64
	}{
		{feedID, 120},
		{pvaID, 20},
	} {
		rows, qty, _ := stackingTestStats(t, db, uid, tc.item, "backpack")
		if rows != 2 || qty != tc.qty {
			t.Errorf("%s 的逐实例堆被改变: rows=%d qty=%d", tc.item, rows, qty)
		}
	}
	for guid, want := range recipes {
		var got string
		if err := db.SQL.QueryRow(`
			SELECT r.mats_json FROM groundbait_recipe r
			JOIN item i ON i.guid=r.instance_guid
			WHERE r.instance_guid=? AND i.uid=?`, guid, uid).Scan(&got); err != nil {
			t.Errorf("实例 %s 的配方丢失或成为孤儿: %v", guid, err)
		} else if got != want {
			t.Errorf("实例 %s 的配方改变: got=%s want=%s", guid, got, want)
		}
	}
}

func TestConsolidateNonReturnableItemStacksRollsBackOnDeleteFailure(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	const (
		uid    = int64(4621)
		foodID = "rollback_food"
	)
	seedEconomyItemDef(t, db, 994011, foodID, 26)
	keep := economyTestItem(t, srv, uid, foodID, "backpack", 2)
	drop := economyTestItem(t, srv, uid, foodID, "backpack", 3)
	setStackOrder(t, db, keep, drop)
	if _, err := db.SQL.Exec(`INSERT INTO container_slot(uid,container,slot,guid) VALUES(?,0,8,?)`, uid, drop.GUID); err != nil {
		t.Fatal(err)
	}
	markLegacyStackPurchase(t, srv, uid, drop)
	if _, err := db.SQL.Exec(fmt.Sprintf(`CREATE TRIGGER fail_stack_delete BEFORE DELETE ON item
		WHEN OLD.guid='%s' BEGIN SELECT RAISE(ABORT,'test delete failure'); END`, drop.GUID)); err != nil {
		t.Fatal(err)
	}

	if removed, err := srv.ConsolidateNonReturnableItemStacks(context.Background()); err == nil || removed != 0 {
		t.Fatalf("注入失败未整体回滚: removed=%d err=%v", removed, err)
	}
	rows, qty, _ := stackingTestStats(t, db, uid, foodID, "backpack")
	if rows != 2 || qty != 5 {
		t.Fatalf("失败整理改变库存: rows=%d qty=%d", rows, qty)
	}
	var shortcut string
	if err := db.SQL.QueryRow(`SELECT guid FROM container_slot WHERE uid=? AND container=0 AND slot=8`, uid).Scan(&shortcut); err != nil || shortcut != drop.GUID {
		t.Fatalf("失败整理改变快捷栏: guid=%s err=%v", shortcut, err)
	}
	var receipt string
	if err := db.SQL.QueryRow(`SELECT v FROM kv WHERE k=?`, storePurchaseKey(uid, drop.GUID)).Scan(&receipt); err != nil || receipt == "" {
		t.Fatalf("失败整理清除了退货凭据: err=%v", err)
	}
	current, err := db.GetItemByGUID(context.Background(), drop.GUID)
	if err != nil || current.BuyPrice != 10 {
		t.Fatalf("失败整理改变重复堆元数据: row=%+v err=%v", current, err)
	}
}

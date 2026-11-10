package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

func buyStackingTestItem(t *testing.T, srv *Server, uid int64, shop, itemID string) {
	t.Helper()
	c := economyTestCtx(srv, uid, proto.Args524(
		proto.StrVal(shop), proto.StrVal(itemID), proto.U64(0),
	))
	c.ExpectResponse = true
	c.replyCapture = &ctxReplyCapture{}
	if body, ok := hStoreBuy(false)(c); !ok || len(body) == 0 {
		t.Fatalf("购买 %s 失败: ok=%v body=%x", itemID, ok, body)
	}
}

func stackingTestStats(t *testing.T, db *store.DB, uid int64, itemID, container string) (rows, qty int64, guid string) {
	t.Helper()
	if err := db.SQL.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(qty),0), COALESCE(MIN(guid),'')
		FROM item WHERE uid=? AND item_id=? AND container=?`,
		uid, itemID, container).Scan(&rows, &qty, &guid); err != nil {
		t.Fatal(err)
	}
	return rows, qty, guid
}

func TestStorePurchasePreservesRecipeInstances(t *testing.T) {
	for _, id := range []string{"feedcompleted", "pva_stick_s"} {
		t.Run(id, func(t *testing.T) {
			srv, db := newEconomyTxTestServer(t)
			const uid = int64(4630)
			if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('recipe_store',?,10,0,100)`, id); err != nil {
				t.Fatal(err)
			}
			old := economyTestItem(t, srv, uid, id, "backpack", 60)
			const recipe = `["feedadds_corn"]`
			if _, err := db.SQL.Exec(`INSERT INTO groundbait_recipe(instance_guid,mats_json) VALUES(?,?)`, old.GUID, recipe); err != nil {
				t.Fatal(err)
			}
			buyStackingTestItem(t, srv, uid, "recipe_store", id)
			buyStackingTestItem(t, srv, uid, "recipe_store", id)
			rows, qty, _ := stackingTestStats(t, db, uid, id, "backpack")
			if rows != 3 || qty != 62 {
				t.Errorf("配方产出必须独立: rows=%d qty=%d want=3/62", rows, qty)
			}
			var oldQty int64
			var mats string
			if err := db.SQL.QueryRow(`SELECT i.qty,r.mats_json FROM item i JOIN groundbait_recipe r ON r.instance_guid=i.guid WHERE i.guid=?`, old.GUID).Scan(&oldQty, &mats); err != nil {
				t.Fatal(err)
			}
			if oldQty != 60 || mats != recipe {
				t.Fatalf("原配方实例被改变: qty=%d mats=%s", oldQty, mats)
			}
		})
	}
}

func TestStorePurchaseMergesNonReturnableStacksByExactItemID(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	const (
		uid    = int64(4610)
		shopID = "stack_store"
		foodID = "stack_food"
		baitID = "stack_bait"
	)
	seedEconomyItemDef(t, db, 993001, foodID, 26)
	seedEconomyItemDef(t, db, 993002, baitID, 27)
	if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES
		(?,?,10,0,100),(?,?,10,0,100)`, shopID, foodID, shopID, baitID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureProfile(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=1000 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	food := economyTestItem(t, srv, uid, foodID, "backpack", 2)
	bait := economyTestItem(t, srv, uid, baitID, "backpack", 3)

	for range 2 {
		buyStackingTestItem(t, srv, uid, shopID, foodID)
		buyStackingTestItem(t, srv, uid, shopID, baitID)
	}

	foodRows, foodQty, foodGUID := stackingTestStats(t, db, uid, foodID, "backpack")
	baitRows, baitQty, baitGUID := stackingTestStats(t, db, uid, baitID, "backpack")
	if foodRows != 1 || foodQty != 4 || foodGUID != food.GUID {
		t.Fatalf("食物未并入原堆: rows=%d qty=%d guid=%s want=%s", foodRows, foodQty, foodGUID, food.GUID)
	}
	if baitRows != 1 || baitQty != 5 || baitGUID != bait.GUID {
		t.Fatalf("饵料未并入原堆: rows=%d qty=%d guid=%s want=%s", baitRows, baitQty, baitGUID, bait.GUID)
	}
	if foodGUID == baitGUID {
		t.Fatal("食物与饵料错误地共用了同一实例")
	}
	var receipts, priced int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM kv WHERE k LIKE ?`, "store-purchase:"+"4610"+":%").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id IN (?,?) AND buy_price<>0`, uid, foodID, baitID).Scan(&priced); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || priced != 0 {
		t.Fatalf("不可退计件物品不应保存退货凭据: kv=%d priced=%d", receipts, priced)
	}
}

func TestStorePurchaseMergesAutoExpandedFoodAndBaitBoxes(t *testing.T) {
	for _, tc := range []struct {
		name, boxID, contentID string
		uid, initial, wantQty  int64
	}{
		{"food", "box_food_egg_10", "food_egg", 4611, 1, 21},
		{"bait", "box_worm_01_10", "worm_01", 4612, 1, 630},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newEconomyTxTestServer(t)
			shopID := tc.name + "_store"
			if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES(?,?,10,0,100)`, shopID, tc.boxID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.EnsureProfile(context.Background(), tc.uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`UPDATE profile SET silver=1000 WHERE uid=?`, tc.uid); err != nil {
				t.Fatal(err)
			}
			existing := economyTestItem(t, srv, tc.uid, tc.contentID, "backpack", tc.initial)

			buyStackingTestItem(t, srv, tc.uid, shopID, tc.boxID)
			buyStackingTestItem(t, srv, tc.uid, shopID, tc.boxID)

			rows, qty, guid := stackingTestStats(t, db, tc.uid, tc.contentID, "backpack")
			if rows != 1 || qty != tc.wantQty || guid != existing.GUID {
				t.Fatalf("盒装内容未并入原堆: rows=%d qty=%d guid=%s want=%s", rows, qty, guid, existing.GUID)
			}
			var boxes int64
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, tc.uid, tc.boxID).Scan(&boxes); err != nil {
				t.Fatal(err)
			}
			if boxes != 0 {
				t.Fatalf("购买即展开的盒子不应入库本体: %d", boxes)
			}
		})
	}
}

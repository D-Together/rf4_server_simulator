package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStaticSpecialtyShopShelvesReferenceKnownItems(t *testing.T) {
	db, err := OpenWithStatic(filepath.Join(t.TempDir(), "specialty-shops.db"),
		filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	stores := []string{
		"level_004_kvas", "kala_store_003", "milkstore_viiveri", "sawmill_02",
		"foodstore_garage", "bakery_013", "vakula_013", "mill_013", "paseka_sura",
		"arbuz_store_009", "tungus_015", "craft_baits_012", "varyag_012",
	}
	rows, err := db.SQL.QueryContext(context.Background(), `
		SELECT s.store_id, s.item_id
		FROM static.shop_item s
		LEFT JOIN static.item_def d ON d.item_id = s.item_id
		LEFT JOIN static.item_catalog c ON c.item_id = s.item_id
		WHERE s.store_id IN (
			'level_004_kvas','kala_store_003','milkstore_viiveri','sawmill_02',
			'foodstore_garage','bakery_013','vakula_013','mill_013','paseka_sura',
			'arbuz_store_009','tungus_015','craft_baits_012','varyag_012'
		)
		AND (d.item_id IS NULL OR c.item_id IS NULL)
		ORDER BY s.store_id, s.item_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var missing []string
	for rows.Next() {
		var storeID, itemID string
		if err := rows.Scan(&storeID, &itemID); err != nil {
			t.Fatal(err)
		}
		missing = append(missing, storeID+"/"+itemID)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("特色商店存在无 item_def/item_catalog 的货架商品: %v", missing)
	}

	var craftRows int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM static.shop_item WHERE store_id='craft_baits_012'`).Scan(&craftRows); err != nil {
		t.Fatal(err)
	}
	if craftRows != 8254 {
		t.Fatalf("craft_baits_012 基础货架=%d，期望 8254（%d 个目标商店）", craftRows, len(stores))
	}
}

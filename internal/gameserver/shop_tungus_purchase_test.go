package gameserver

import (
	"bytes"
	"path/filepath"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

const tungusShopTestUID int64 = 1000001

func newTungusShopTestServer(t *testing.T) (*Server, *store.DB) {
	t.Helper()
	db, err := store.OpenWithStatic(filepath.Join(t.TempDir(), "tungus-shop.db"),
		filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureProfile(t.Context(), tungusShopTestUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=5000,gold=1000 WHERE uid=?`, tungusShopTestUID); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "tungus-shop-test"}}, logging.Discard(), nil)
	srv.SetDB(db)
	return srv, db
}

func tungusShopBuyRequest(srv *Server, itemID string, sub int) *Ctx {
	request := economyTestCtx(srv, tungusShopTestUID, proto.Args524(
		proto.StrVal("tungus_015"), proto.StrVal(itemID), proto.U64(1)))
	request.Main, request.Sub, request.ExpectResponse = 22, sub, true
	return request
}

func assertTungusShopBalances(t *testing.T, db *store.DB, gold, buys, items int64) {
	t.Helper()
	var silver, gotGold, gotBuys, gotItems int64
	if err := db.SQL.QueryRow(`SELECT silver,gold FROM profile WHERE uid=?`, tungusShopTestUID).Scan(&silver, &gotGold); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='buy'`, tungusShopTestUID).Scan(&gotBuys); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=?`, tungusShopTestUID).Scan(&gotItems); err != nil {
		t.Fatal(err)
	}
	if silver != 5000 || gotGold != gold || gotBuys != buys || gotItems != items {
		t.Fatalf("购买账实不符: silver=%d gold=%d buys=%d items=%d; want gold=%d buys=%d items=%d",
			silver, gotGold, gotBuys, gotItems, gold, buys, items)
	}
}

func TestTungusShopShelfUsesPricedLocalGoods(t *testing.T) {
	srv, db := newTungusShopTestServer(t)
	request := economyTestCtx(srv, tungusShopTestUID, proto.Args524(proto.StrVal("tungus_015")))
	body, ok := hStoreList(request)
	if !ok {
		t.Fatal("22/1 未返回货架")
	}
	shelf, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if shelf.ArgCodeStr() != "140" || shelf.ArgCount() != 95 {
		t.Fatalf("特产店应保留上游93件商品并补齐两件有价特产: code=%q count=%d", shelf.ArgCodeStr(), shelf.ArgCount())
	}
	want := make(map[string]store.ShopPrice, 95)
	rows, err := db.SQL.Query(`SELECT item_id,silver,gold,stock FROM static.shop_item WHERE store_id='tungus_015'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p store.ShopPrice
		if err := rows.Scan(&p.ItemID, &p.Silver, &p.Gold, &p.Stock); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		want[p.ItemID] = p
	}
	rowErr := rows.Err()
	_ = rows.Close()
	if rowErr != nil {
		t.Fatal(rowErr)
	}
	if len(want) != 93 {
		t.Fatalf("上游基础货架行数=%d want=93", len(want))
	}
	for _, id := range []string{"tungus_bot", "tungus_12pack"} {
		if _, exists := want[id]; exists {
			t.Fatalf("上游已提供%s，应重新核对本地补齐是否仍有必要", id)
		}
		p, err := db.ItemPriceGet(t.Context(), id)
		if err != nil || p.Silver != 0 || p.Gold <= 0 || p.Stock != 1 {
			t.Fatalf("特产必须具有明确静态价目: %+v %v", p, err)
		}
		want[id] = *p
	}
	for i := 0; i < shelf.ArgCount(); i++ {
		row := shelf.ArgItem(i)
		itemID, silver, price, stock := row.Field(0), row.Field(1), row.Field(2), row.Field(3)
		id := itemID.StrOr()
		p, exists := want[id]
		if !exists || silver.U64() != uint64(p.Silver) || price.U64() != uint64(p.Gold) || stock.U64() != uint64(p.Stock) {
			t.Fatalf("货架不应漏项、重复、改动实录或估价特产: %s silver=%d gold=%d stock=%d",
				id, silver.U64(), price.U64(), stock.U64())
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("货架缺少上游或补齐商品: %v", want)
	}
	if _, err := db.ShopItemGet(t.Context(), "farmstore_002", "tungus_bot"); err == nil {
		t.Fatal("特产商品不应扩散到农场商店")
	}
	if farm, err := db.ShopItemGet(t.Context(), "farmstore_002", "mix_combi"); err != nil || farm.Silver != 50 {
		t.Fatalf("农场原有货架应保持不变: %+v %v", farm, err)
	}
}

func TestTungusShopPurchaseGrantsDrinkAndGift(t *testing.T) {
	for _, tc := range []struct {
		itemID   string
		gold     int64
		qty      int64
		field    int
		buyPrice int64
		receipts int
	}{
		{"tungus_bot", 22, 2, 1, 0, 0},
		{"tungus_12pack", 129, 1, 2, 129, 1},
	} {
		t.Run(tc.itemID, func(t *testing.T) {
			srv, db := newTungusShopTestServer(t)
			result := srv.dispatch.executeRequest(tungusShopBuyRequest(srv, tc.itemID, 3), true)
			if result.failed || result.nativeFailure || !result.hasResponse {
				t.Fatalf("22/3 特产购买应成功: %+v", result)
			}
			if reason := srv.dispatch.checkRegisteredResponse(22, 3, result.body, srv.Table()); reason != "" {
				t.Fatalf("购买响应不满足协议契约: %s", reason)
			}
			response, _, err := proto.DecodeValue(result.body, 0, srv.Table())
			if err != nil || response.TypeID != 804 {
				t.Fatalf("购买响应应为对应商品的804: %+v %v", response, err)
			}
			itemID := response.Field(0)
			if itemID.StrOr() != tc.itemID {
				t.Fatalf("购买响应商品=%s want=%s", itemID.StrOr(), tc.itemID)
			}
			goods := response.Field(tc.field).Val
			if goods == nil || goods.ArgCodeStr() != "61" || goods.ArgCount() != 1 {
				t.Fatalf("购买响应f%d应包含一件商品，礼盒购买时不展开: %+v", tc.field, goods)
			}
			items, err := db.ListItemsByContainer(t.Context(), tungusShopTestUID, "backpack")
			if err != nil || len(items) != 1 || items[0].ItemID != tc.itemID || items[0].Qty != tc.qty || items[0].BuyPrice != tc.buyPrice {
				t.Fatalf("购买后物品应与扣款一致: %+v %v", items, err)
			}
			tree, err := srv.ItemToTree(t.Context(), items[0])
			if err != nil || tree == nil || tree.TypeID != goods.ArgItem(0).TypeID {
				t.Fatalf("入库物品必须可构造物品树: %+v %v", tree, err)
			}
			if _, err := proto.EncodeValue(tree, srv.Table()); err != nil {
				t.Fatalf("入库物品树必须可编码: %v", err)
			}
			var receipts int
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM kv WHERE k=?`, storePurchaseKey(tungusShopTestUID, items[0].GUID)).Scan(&receipts); err != nil || receipts != tc.receipts {
				t.Fatalf("退货凭据数量错误: count=%d want=%d err=%v", receipts, tc.receipts, err)
			}
			assertTungusShopBalances(t, db, 1000-tc.gold, 1, 1)
		})
	}
}

func TestTungusShopRejectsInvalidPurchases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		itemID string
		sub    int
		gold   int64
	}{
		{"余额不足", "tungus_bot", 3, 21},
		{"没有银币价格", "tungus_bot", 2, 1000},
		{"其他店的有价饮品", "kvas_bottle", 3, 1000},
		{"其他店的有价商品", "wallet_50", 3, 1000},
		{"无价旧款不估价", "tungus_vodka", 3, 1000},
		{"无价旧礼盒不估价", "box_tungus_vodka", 3, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTungusShopTestServer(t)
			if _, err := db.SQL.Exec(`UPDATE profile SET gold=? WHERE uid=?`, tc.gold, tungusShopTestUID); err != nil {
				t.Fatal(err)
			}
			result := srv.dispatch.executeRequest(tungusShopBuyRequest(srv, tc.itemID, tc.sub), true)
			if !result.nativeFailure || !result.hasResponse {
				t.Fatalf("无效购买必须明确拒绝: %+v", result)
			}
			assertTungusShopBalances(t, db, tc.gold, 0, 0)
		})
	}
}

func TestTungusShopKeepsOverridesAndAutomaticRestock(t *testing.T) {
	srv, db := newTungusShopTestServer(t)
	if err := db.SetAdminShopOverride(t.Context(), "tungus_015", "tungus_bot", 0, 30, 0, "test"); err != nil {
		t.Fatal(err)
	}
	price, err := db.ShopItemGet(t.Context(), "tungus_015", "tungus_bot")
	if err != nil || price.Gold != 30 || price.Stock != 1000 {
		t.Fatalf("零库存应沿用既有自动补货，覆盖价应生效: %+v %v", price, err)
	}
	result := srv.dispatch.executeRequest(tungusShopBuyRequest(srv, "tungus_bot", 3), true)
	if result.failed || result.nativeFailure || !result.hasResponse {
		t.Fatalf("自动补货后应可购买: %+v", result)
	}
	assertTungusShopBalances(t, db, 970, 1, 1)
}

func TestTungusShopPurchaseRollbackOnGrantFailure(t *testing.T) {
	srv, db := newTungusShopTestServer(t)
	if _, err := db.SQL.Exec(`CREATE TRIGGER reject_tungus BEFORE INSERT ON item
		WHEN NEW.item_id='tungus_12pack' BEGIN SELECT RAISE(ABORT,'test grant failure'); END`); err != nil {
		t.Fatal(err)
	}
	result := srv.dispatch.executeRequest(tungusShopBuyRequest(srv, "tungus_12pack", 3), true)
	if !result.nativeFailure {
		t.Fatalf("发货失败应明确拒绝: %+v", result)
	}
	assertTungusShopBalances(t, db, 1000, 0, 0)
}

func TestTungusShopPurchaseReceiptReplay(t *testing.T) {
	srv, db := newTungusShopTestServer(t)
	const token = "1|2|1000001|tungus-shop-replay"
	first := newReceiptTestSession(t, srv, 1, token)
	request, err := proto.EncodeValue(proto.Args524(
		proto.StrVal("tungus_015"), proto.StrVal("tungus_12pack"), proto.U64(1)), srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	first.send(22, 3, 77, request, 0x00)
	response := waitReceiptResponse(t, first)
	if response.Marker != -2 {
		t.Fatalf("首次购买应成功: marker=%d", response.Marker)
	}
	first.send(22, 3, 77, request, 0x00)
	if replay := waitReceiptResponse(t, first); !bytes.Equal(replay.Raw, response.Raw) {
		t.Fatal("相同call_id应原样重放购买响应")
	}
	reconnected := newReceiptTestSession(t, srv, 2, token)
	reconnected.send(22, 3, 77, request, 0x00)
	if replay := waitReceiptResponse(t, reconnected); !bytes.Equal(replay.Raw, response.Raw) {
		t.Fatal("重连后应原样重放购买响应")
	}
	assertTungusShopBalances(t, db, 871, 1, 1)
}

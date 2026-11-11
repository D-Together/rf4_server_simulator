package gameserver

import (
	"path/filepath"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// 欧泊咖啡馆（铜湖 cafestore_18）售卖 cafe_ukleyka 产品线。它的明细只落在
// item_def.kind 与静态 item_price 上，没有逐店 shop_item 实录，因此必须在
// 商店目录里按店登记货架，否则 22/1 会回退到 kind 1 的食品模板，客户端点咖啡
// 时 22/3 查不到价目，最终报“发生内部错误”。

const cafeShopTestUID int64 = 1000001

func newCafeShopTestServer(t *testing.T) (*Server, *store.DB) {
	t.Helper()
	db, err := store.OpenWithStatic(filepath.Join(t.TempDir(), "cafe-shop.db"),
		filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureProfile(t.Context(), cafeShopTestUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=5000,gold=1000 WHERE uid=?`, cafeShopTestUID); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "cafe-shop-test"}}, logging.Discard(), nil)
	srv.SetDB(db)
	return srv, db
}

func cafeShopBuyRequest(srv *Server, itemID string, sub int) *Ctx {
	request := economyTestCtx(srv, cafeShopTestUID, proto.Args524(
		proto.StrVal("cafestore_18"), proto.StrVal(itemID), proto.U64(1)))
	request.Main, request.Sub, request.ExpectResponse = 22, sub, true
	return request
}

func assertCafeShopBalances(t *testing.T, db *store.DB, gold, buys, items int64) {
	t.Helper()
	var silver, gotGold, gotBuys, gotItems int64
	if err := db.SQL.QueryRow(`SELECT silver,gold FROM profile WHERE uid=?`, cafeShopTestUID).Scan(&silver, &gotGold); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='buy'`, cafeShopTestUID).Scan(&gotBuys); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=?`, cafeShopTestUID).Scan(&gotItems); err != nil {
		t.Fatal(err)
	}
	if silver != 5000 || gotGold != gold || gotBuys != buys || gotItems != items {
		t.Fatalf("购买账实不符: silver=%d gold=%d buys=%d items=%d; want gold=%d buys=%d items=%d",
			silver, gotGold, gotBuys, gotItems, gold, buys, items)
	}
}

// 咖啡馆货架必须只含 cafe_ukleyka 产品线，价格取自静态 item_price（内部整数单位，
// 4/6/1 对应界面 0.04/0.06/0.01 金币）。旧行为回退到 kind 1 食品模板，会把面包、
// 猪肉这些食品厂商品摆进咖啡馆。
func TestCafeShopShelfUsesCafeProductLine(t *testing.T) {
	srv, _ := newCafeShopTestServer(t)
	request := economyTestCtx(srv, cafeShopTestUID, proto.Args524(proto.StrVal("cafestore_18")))
	body, ok := hStoreList(request)
	if !ok {
		t.Fatal("22/1 未返回货架")
	}
	shelf, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if shelf.ArgCodeStr() != "140" {
		t.Fatalf("22/1 货架 args code=%q want=140", shelf.ArgCodeStr())
	}
	want := map[string][3]uint64{
		"small_coffee": {0, 4, 1},
		"big_coffee":   {0, 6, 1},
		"hd_box":       {0, 1, 1},
	}
	if shelf.ArgCount() != len(want) {
		var got []string
		for i := 0; i < shelf.ArgCount(); i++ {
			row := shelf.ArgItem(i)
			idField := row.Field(0)
			got = append(got, idField.StrOr())
		}
		t.Fatalf("咖啡馆货架应只含 %d 件本店商品，实际 %d 件: %v", len(want), shelf.ArgCount(), got)
	}
	for i := 0; i < shelf.ArgCount(); i++ {
		row := shelf.ArgItem(i)
		idField := row.Field(0)
		id := idField.StrOr()
		w, exists := want[id]
		if !exists {
			t.Fatalf("咖啡馆不应上架非本店商品: %s", id)
		}
		silver, gold, stock := row.Field(1), row.Field(2), row.Field(3)
		if silver.U64() != w[0] || gold.U64() != w[1] || stock.U64() != w[2] {
			t.Fatalf("%s 货架价不符: silver=%d gold=%d stock=%d want=%v",
				id, silver.U64(), gold.U64(), stock.U64(), w)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("货架缺少本店商品: %v", want)
	}
}

// 用户在铜湖欧泊咖啡馆点“小杯咖啡”走 22/3 金币通道：必须扣 0.04 金币并入库，
// 而不是返回业务失败（客户端会显示成“发生内部错误”）。
func TestCafeShopPurchaseCoffeeWithGold(t *testing.T) {
	for _, tc := range []struct {
		itemID string
		gold   int64
		qty    int64
	}{
		{"small_coffee", 4, 1},
		{"big_coffee", 6, 2},
	} {
		t.Run(tc.itemID, func(t *testing.T) {
			srv, db := newCafeShopTestServer(t)
			result := srv.dispatch.executeRequest(cafeShopBuyRequest(srv, tc.itemID, 3), true)
			if result.failed || result.nativeFailure || !result.hasResponse {
				t.Fatalf("22/3 咖啡馆购买应成功: %+v", result)
			}
			if reason := srv.dispatch.checkRegisteredResponse(22, 3, result.body, srv.Table()); reason != "" {
				t.Fatalf("购买响应不满足协议契约: %s", reason)
			}
			response, _, err := proto.DecodeValue(result.body, 0, srv.Table())
			if err != nil || response.TypeID != 804 {
				t.Fatalf("购买响应应为 804: %+v %v", response, err)
			}
			itemField := response.Field(0)
			if got := itemField.StrOr(); got != tc.itemID {
				t.Fatalf("购买响应商品=%s want=%s", got, tc.itemID)
			}
			goods := response.Field(1).Val
			if goods == nil || goods.ArgCodeStr() != "61" || goods.ArgCount() != 1 {
				t.Fatalf("购买响应 f1 应包含一件非套盒商品: %+v", goods)
			}
			items, err := db.ListItemsByContainer(t.Context(), cafeShopTestUID, "backpack")
			// 单份产出数量取自 item_def 的 ljnnpokapig：小杯 1、大杯 2。
			if err != nil || len(items) != 1 || items[0].ItemID != tc.itemID || items[0].Qty != tc.qty {
				t.Fatalf("购买后物品应与扣款一致: %+v %v", items, err)
			}
			assertCafeShopBalances(t, db, 1000-tc.gold, 1, 1)
		})
	}
}

// 咖啡没有银币价，22/2 必须明确拒绝且不动账；不在货架上的商品同样拒绝。
func TestCafeShopRejectsInvalidPurchases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		itemID string
		sub    int
	}{
		{"咖啡没有银币价", "small_coffee", 2},
		{"非本店商品", "loaf_01", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newCafeShopTestServer(t)
			result := srv.dispatch.executeRequest(cafeShopBuyRequest(srv, tc.itemID, tc.sub), true)
			if !result.nativeFailure || !result.hasResponse {
				t.Fatalf("无效购买必须明确拒绝: %+v", result)
			}
			assertCafeShopBalances(t, db, 1000, 0, 0)
		})
	}
}

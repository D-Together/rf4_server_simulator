package gameserver

// 消耗品无效果时不得白扣：状态条已在上限时使用该消耗品，物品必须保留。
//
// 期望值全部按 tunables 缺省值手算，不用被测代码回算：
// 咖啡 +0.15 精力、茶 +0.30 舒适、药品 -0.50 健康（f4）。

import (
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// drinkUseTestGone 断言最后一份用完后实例行已被删除（数量归零即移除，不留在背包里）。
func drinkUseTestGone(t *testing.T, s *Server, guid string) {
	t.Helper()
	if _, err := s.DB.GetItemByGUID(t.Context(), guid); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("物品应已被移除: guid=%s err=%v", guid, err)
	}
}

// setConditionForUse 覆盖测试玩家的状态行；updated_ts 推到未来，
// 避免读取路径顺手推进状态、污染用例期望值。
func setConditionForUse(t *testing.T, s *Server, c store.PlayerCondition) {
	t.Helper()
	c.UID = 1000001
	c.UpdatedTS = float64(time.Now().Unix()) + 3600
	if err := s.DB.UpsertPlayerCondition(t.Context(), c); err != nil {
		t.Fatal(err)
	}
}

// 精力已在上限时喝咖啡：落回通用 handler，罐子保留。
func TestCoffeeAtEnergyCapIsNotConsumed(t *testing.T) {
	t.Parallel()
	s := newDrinkUseTestServer(t)
	// 精力上限缺省 0.85：这里正好顶满，+0.15 会被钳回去。
	setConditionForUse(t, s, store.PlayerCondition{Primary: 0.85, Secondary: 0.30, Comfort: 0.10})
	coffee := drinkUseTestItem(t, s, "big_coffee", 1, 0)
	ctx := drinkUseTestCtx(t, s, 70, coffee.GUID, 0)
	if body, handled := s.useAlcoholDrink(ctx); handled || body != nil {
		t.Fatalf("精力满时咖啡应落回通用 handler: handled=%v body=%d", handled, len(body))
	}
	drinkUseTestQty(t, s, coffee.GUID, 1, 0)
}

// 精力未满时喝咖啡：正常生效并扣 1，精力 0.50+0.15=0.65。
func TestCoffeeBelowEnergyCapStillConsumes(t *testing.T) {
	t.Parallel()
	s := newDrinkUseTestServer(t)
	setConditionForUse(t, s, store.PlayerCondition{Primary: 0.50, Secondary: 0.30, Comfort: 0.10})
	coffee := drinkUseTestItem(t, s, "big_coffee", 1, 0)
	if body, handled := s.useAlcoholDrink(drinkUseTestCtx(t, s, 71, coffee.GUID, 0)); !handled || len(body) == 0 {
		t.Fatalf("精力未满时咖啡应被消耗通道处理: handled=%v body=%d", handled, len(body))
	}
	drinkUseTestGone(t, s, coffee.GUID)
	row, err := s.DB.EnsurePlayerCondition(t.Context(), 1000001)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(row.Primary-0.65) > 1e-9 {
		t.Fatalf("咖啡应恢复精力 0.50+0.15: %v", row.Primary)
	}
	if row.Secondary != 0.30 || row.Comfort != 0.10 {
		t.Fatalf("咖啡不应动饱食/舒适: %+v", row)
	}
}

// 饱食度满时吃药：药品不碰饱食度，不能被「已吃饱」拦住；f4 0.60-0.50=0.10。
func TestMedicineNotBlockedByFullSatiety(t *testing.T) {
	t.Parallel()
	s := newDrinkUseTestServer(t)
	if _, err := s.DB.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(900100,'pill_pack',26,'{"fopbhlmkcpe":3,"aekjeoifnkk":1}')`); err != nil {
		t.Fatal(err)
	}
	setConditionForUse(t, s, store.PlayerCondition{Primary: 0.85, Secondary: 1.0, Comfort: 1.0, LuckPenalty: 0.60})
	pill := drinkUseTestItem(t, s, "pill_pack", 2, 0)
	if body, handled := s.useAlcoholDrink(drinkUseTestCtx(t, s, 72, pill.GUID, 0)); !handled || len(body) == 0 {
		t.Fatalf("饱食满时药品仍应生效: handled=%v body=%d", handled, len(body))
	}
	drinkUseTestQty(t, s, pill.GUID, 1, 0)
	row, err := s.DB.EnsurePlayerCondition(t.Context(), 1000001)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(row.LuckPenalty-0.10) > 1e-9 {
		t.Fatalf("药品应回健康 0.60-0.50: %v", row.LuckPenalty)
	}
	if row.Secondary != 1.0 {
		t.Fatalf("药品不应动饱食度: %+v", row)
	}
}

// 饱食度满时喝茶：茶只碰舒适度，同样不能被「已吃饱」拦住；f3 0.10+0.30=0.40。
func TestTeaNotBlockedByFullSatiety(t *testing.T) {
	t.Parallel()
	s := newDrinkUseTestServer(t)
	setConditionForUse(t, s, store.PlayerCondition{Primary: 0.85, Secondary: 1.0, Comfort: 0.10})
	tea := drinkUseTestItem(t, s, "ab_tea_can", 1, 0)
	if body, handled := s.useAlcoholDrink(drinkUseTestCtx(t, s, 73, tea.GUID, 0)); !handled || len(body) == 0 {
		t.Fatalf("饱食满时茶仍应生效: handled=%v body=%d", handled, len(body))
	}
	drinkUseTestGone(t, s, tea.GUID)
	row, err := s.DB.EnsurePlayerCondition(t.Context(), 1000001)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(row.Comfort-0.40) > 1e-9 {
		t.Fatalf("茶应恢复舒适度 0.10+0.30: %v", row.Comfort)
	}
	if row.Secondary != 1.0 || row.Primary != 0.85 {
		t.Fatalf("茶不应动饱食/精力: %+v", row)
	}
}

// 饱食度满时喝可可：饱食分量推不动，但精力分量仍生效，属于「有变化」，
// 所以照常扣物并回精力（精力 0.50+0.05=0.55）。
func TestCocoaStillEffectiveViaEnergyWhenSatietyFull(t *testing.T) {
	t.Parallel()
	s := newDrinkUseTestServer(t)
	setConditionForUse(t, s, store.PlayerCondition{Primary: 0.50, Secondary: 1.0, Comfort: 0.10})
	cocoa := drinkUseTestItem(t, s, "food_cacao", 1, 0)
	if body, handled := s.useAlcoholDrink(drinkUseTestCtx(t, s, 74, cocoa.GUID, 0)); !handled || len(body) == 0 {
		t.Fatalf("饱食满但精力未满时可可应生效: handled=%v body=%d", handled, len(body))
	}
	drinkUseTestGone(t, s, cocoa.GUID)
	row, err := s.DB.EnsurePlayerCondition(t.Context(), 1000001)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(row.Primary-0.55) > 1e-9 {
		t.Fatalf("可可应恢复精力 0.50+0.05: %v", row.Primary)
	}
	if row.Secondary != 1.0 {
		t.Fatalf("饱食已满时不应越界: %+v", row)
	}
}

// hItemUse 层：无效果时落回通用 handler 后仍要回出合法包体，
// 且客户端能正常解码（走的是所有普通物品同一条 args(61)+800 快照路径）。
func TestItemUseFallsBackWithValidBodyWhenNoEffect(t *testing.T) {
	t.Parallel()
	s := newDrinkUseTestServer(t)
	setConditionForUse(t, s, store.PlayerCondition{Primary: 0.85, Secondary: 0.30, Comfort: 0.10})
	coffee := drinkUseTestItem(t, s, "big_coffee", 1, 0)
	ctx := drinkUseTestCtx(t, s, 75, coffee.GUID, 0)
	body, handled := hItemUse(ctx)
	if !handled {
		t.Fatal("4/8 必须被处理（落回通用 handler）")
	}
	if len(body) == 0 {
		t.Fatal("落回通用 handler 也必须回出包体，不能空回")
	}
	if _, present, _, failure := ctx.replyCapture.snapshot(); present || failure {
		t.Fatalf("无效果回落不是错误，不应回失败信封: present=%v failure=%v", present, failure)
	}
	// 包体必须能被同一张表解码回 800 快照，否则客户端强转会失败。
	if _, _, err := proto.DecodeValue(body, 0, s.Table()); err != nil {
		t.Fatalf("通用 handler 包体解码失败: %v", err)
	}
	drinkUseTestQty(t, s, coffee.GUID, 1, 0)
}

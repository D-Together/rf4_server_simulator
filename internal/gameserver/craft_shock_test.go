package gameserver

import (
	"context"
	"math"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

func TestCraftShockLeaderUsesMainLineSlot(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid = int64(2000043)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "mono_15065", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.alignItemRowQty(ctx, line)
	sess := &Session{ID: 1, Userid: uid, Srv: srv, log: logging.Discard()}
	hctx := &Ctx{Sess: sess, Main: 9, Sub: 1, CallID: 1, DecodeOK: true,
		Tree: proto.Args524(craftObj8("craft_leader_shock_200", []craftSlot{{ItemID: "craft_mat_line", GUID: line.GUID}}, 1)), Log: logging.Discard()}
	body, ok := hCraft(hctx)
	if !ok || u64Of(decodeTreeValue(t, srv, body).Fields[0].Num) != 1 {
		t.Fatal("Super Line 7lb 制作减震引线应成功")
	}
	rows, err := db.ListItemsByContainers(ctx, uid, []string{"backpack"})
	if err != nil {
		t.Fatal(err)
	}
	var crafted *store.Item
	for _, row := range rows {
		if row.ItemID == "leader_crafted_shock_200" {
			crafted = row
		}
	}
	if crafted == nil {
		t.Fatal("缺少减震引线成品")
	}
	check := func() {
		t.Helper()
		row, err := db.GetItemByGUID(ctx, crafted.GUID)
		if err != nil || row == nil || row.Container != "backpack" {
			t.Fatalf("成品应保留在背包: %+v, %v", row, err)
		}
		tree, err := srv.ItemToTree(ctx, row)
		if err != nil {
			t.Fatal(err)
		}
		inner := tree.Fields[fieldIdxByCS(tree.Class, craftedMainLineKey)].Val
		if inner == nil || inner.Kind != proto.KindObj || inner.Class != categoryWireClass[gearCatLine].Class {
			t.Fatalf("f3 应使用普通主线实例: %+v", inner)
		}
		if got := u64Of(inner.Fields[fieldIdxByCS(inner.Class, csNumericID)].Num); got != 9201 {
			t.Fatalf("主线模型=%d，期望 9201", got)
		}
		if got := u64Of(inner.Fields[fieldIdxByCS(inner.Class, gearAttrLineLen)].Num); got != 200 {
			t.Fatalf("主线长度=%d，期望 200cm", got)
		}
		if got := wireFloatForTest(t, tree, csStrength); math.Abs(got-3.1) > 0.001 {
			t.Fatalf("成品强度=%f，期望 3.1kg", got)
		}
		for _, key := range []string{gearAttrLeaderIn, "lbadjgllglo"} {
			v := tree.Fields[fieldIdxByCS(tree.Class, key)].Val
			if v != nil && v.Kind != proto.KindNull {
				t.Fatalf("减震引线 %s 应为空: %+v", key, v)
			}
		}
	}
	check()
	if err := srv.RepairItemPhysics(ctx); err != nil {
		t.Fatal(err)
	}
	check()
	after, err := db.GetItemByGUID(ctx, line.GUID)
	if err != nil || after == nil || after.Qty != 118 {
		t.Fatalf("应只消耗 2m 主线: %+v, %v", after, err)
	}
}

// 减震引线内嵌线的两种形态与修复扫描的对应处置（2026-11-09 放开前导线）：
//   - 现行格式：前导线（cat15）在 f1 —— 正确位置，不得迁动。迁去 f3 会把
//     cat15 物品塞进 cat11 外壳，客户端渲染成错误身份。
//   - 旧格式：普通主线（cat11）留在 f1 —— 迁到 f3 并清空 f1。
func TestRepairShockLeaderSlotFormats(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid = int64(2000053)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}

	currentGUID := makeLegacyShockLeaderWithF1Line(t, srv, db, uid, "wrl_sim_monorig_7.5LB", gearAttrLeaderIn, 0.9, 0.95)
	legacyGUID := makeLegacyShockLeaderWithF1Line(t, srv, db, uid, "mono_15247", gearAttrLeaderIn, 0.8, 0.9)

	if err := srv.RepairItemPhysics(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("现行格式前导线在f1不动", func(t *testing.T) {
		row, err := db.GetItemByGUID(ctx, currentGUID)
		if err != nil || row == nil {
			t.Fatalf("读取减震引线失败: %v", err)
		}
		if row.Container == craftedLeaderQuarantineContainer {
			t.Fatal("前导线减震引线不得被隔离")
		}
		fields, err := FieldsFromJSON(row.FieldsJSON)
		if err != nil {
			t.Fatal(err)
		}
		f1 := fields[fieldIdxByCS(row.WireClass, gearAttrLeaderIn)].Val
		if f1 == nil || f1.Kind != proto.KindObj || f1.Class != categoryWireClass[gearCatLeaderWire].Class {
			t.Fatalf("现行格式 f1 前导线对象不得被迁动: %+v", f1)
		}
		lineDef, err := db.GetItemDefByItemID(ctx, "wrl_sim_monorig_7.5LB")
		if err != nil || lineDef == nil {
			t.Fatalf("读取材料定义失败: %v", err)
		}
		if got := u64Of(f1.Fields[fieldIdxByCS(f1.Class, csNumericID)].Num); got != uint64(lineDef.NumericID) {
			t.Fatalf("f1 numeric_id=%d，期望保留 %d", got, lineDef.NumericID)
		}
	})

	t.Run("旧格式f1主线迁到f3", func(t *testing.T) {
		row, err := db.GetItemByGUID(ctx, legacyGUID)
		if err != nil || row == nil {
			t.Fatalf("读取旧减震引线失败: %v", err)
		}
		fields, err := FieldsFromJSON(row.FieldsJSON)
		if err != nil {
			t.Fatal(err)
		}
		mainLine := fields[fieldIdxByCS(row.WireClass, craftedMainLineKey)].Val
		if mainLine == nil || mainLine.Kind != proto.KindObj || mainLine.Class != categoryWireClass[gearCatLine].Class {
			t.Fatalf("旧减震引线未迁移到 f3 普通主线对象: %+v", mainLine)
		}
		lineDef, err := db.GetItemDefByItemID(ctx, "mono_15247")
		if err != nil || lineDef == nil {
			t.Fatalf("读取材料定义失败: %v", err)
		}
		if got := u64Of(mainLine.Fields[fieldIdxByCS(mainLine.Class, csNumericID)].Num); got != uint64(lineDef.NumericID) {
			t.Fatalf("迁移后 numeric_id=%d，期望保留 %d", got, lineDef.NumericID)
		}
		if got := u64Of(mainLine.Fields[fieldIdxByCS(mainLine.Class, gearAttrLineLen)].Num); got != 200 {
			t.Fatalf("迁移后长度=%d，期望 200cm", got)
		}
		const workmanship, durability = 0.8, 0.9
		if got := wireFloatForTest(t, mainLine, csWorkmanship); math.Abs(got-workmanship) > 0.001 {
			t.Fatalf("迁移后工艺=%f，期望 %f", got, workmanship)
		}
		if got := wireFloatForTest(t, mainLine, csDurability); math.Abs(got-durability) > 0.001 {
			t.Fatalf("迁移后耐久=%f，期望 %f", got, durability)
		}
		wantStrength := itemDefF32(lineDef, gearAttrMaxLoad) * workmanship * durability
		if got := wireFloatForTest(t, mainLine, csStrength); math.Abs(got-wantStrength) > 0.001 {
			t.Fatalf("迁移后内线强度=%f，期望 %f", got, wantStrength)
		}
		wantGUID := uuidLike(row.GUID, "legacy-line")
		if got := mainLine.Fields[fieldIdxByCS(mainLine.Class, csGUID)].Hex; got != wantGUID {
			t.Fatalf("迁移后内线 GUID=%s，期望保留 %s", got, wantGUID)
		}
		if old := fields[fieldIdxByCS(row.WireClass, gearAttrLeaderIn)].Val; old != nil && old.Kind != proto.KindNull {
			t.Fatalf("迁移后 f1 应清空: %+v", old)
		}
	})
}

func makeLegacyShockLeaderWithF1Line(
	t *testing.T,
	srv *Server,
	db *store.DB,
	uid int64,
	lineItemID string,
	sourceKey string,
	workmanship float64,
	durability float64,
) string {
	t.Helper()
	ctx := context.Background()
	row, err := srv.prepareNewItem(ctx, uid, "leader_crafted_shock_200", "backpack", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	lineDef, err := db.GetItemDefByItemID(ctx, lineItemID)
	if err != nil {
		t.Fatal(err)
	}
	material := craftedMaterial{
		row: &store.Item{GUID: uuidLike(row.GUID, "legacy-line"), Workmanship: workmanship, Durability: durability},
		def: lineDef,
	}
	line, err := srv.craftedMaterialObject(row, material, sourceKey, 200)
	if err != nil {
		t.Fatal(err)
	}
	if idx := fieldIdxByCS(line.Class, csGUID); idx >= 0 {
		// craftedMaterialObject 会按槽位派生嵌套 GUID；这里固定成旧对象自己的
		// GUID，确保迁移测试验证的是保留原对象身份，而不是重新生成一个。
		line.Fields[idx].Hex = material.row.GUID
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	fields[fieldIdxByCS(row.WireClass, gearAttrLeaderIn)].Val = line
	setWireFloat(fields, row.WireClass, fieldIdxByCS(row.WireClass, csStrength),
		itemDefF32(lineDef, gearAttrMaxLoad)*workmanship*durability)
	row.FieldsJSON, err = FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertItem(ctx, row); err != nil {
		t.Fatal(err)
	}
	return row.GUID
}

// 回归（放开减震引线收前导线，2026-11-09）：配方数据本就是官方的
// [11,15]，b0ac5d3 的"减震引线需要普通主线"拒收无实锚证据——同一
// wire class/DTO 渲染路径相同，普通引线的 cat15→f1 早已实机验证。
// 现在选 Atami Leader Nylon 400LB（mn_lm_40809）做减震引线应成功：
// 内嵌线落 f1（cat15 外壳、numeric 9197），强度按真实线材 181kg。
func TestCraftShockLeaderAcceptsLeaderWireMaterial(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid = int64(2000046)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	wire, err := srv.CreateItem(ctx, uid, "mn_lm_40809", "backpack", 2)
	if err != nil {
		t.Fatal(err)
	}
	srv.alignItemRowQty(ctx, wire)
	sess := &Session{ID: 1, Userid: uid, Srv: srv, log: logging.Discard()}
	hctx := &Ctx{Sess: sess, Main: 9, Sub: 1, CallID: 1, DecodeOK: true,
		Tree: proto.Args524(craftObj8("craft_leader_shock_200", []craftSlot{{ItemID: "craft_mat_line", GUID: wire.GUID}}, 1)), Log: logging.Discard()}
	body, ok := hCraft(hctx)
	if !ok || u64Of(decodeTreeValue(t, srv, body).Fields[0].Num) != 1 {
		t.Fatalf("前导线做减震引线应成功: ok=%v", ok)
	}
	rows, err := db.ListItemsByContainers(ctx, uid, []string{"backpack"})
	if err != nil {
		t.Fatal(err)
	}
	var out *store.Item
	for _, row := range rows {
		if row.ItemID == "leader_crafted_shock_200" {
			out = row
		}
	}
	if out == nil {
		t.Fatal("应产出减震引线成品")
	}
	fields, err := FieldsFromJSON(out.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	// 内嵌线在 f1（mbnambppbom），外壳必须是 cat15 类，numeric 用真实前导线。
	sub := fields[1].Val
	if sub == nil || sub.Kind != proto.KindObj {
		t.Fatalf("减震引线 f1 应有内嵌线对象: %v", sub)
	}
	if sub.Class != "cjigljdikhe" {
		t.Fatalf("f1 外壳应为 cat15 类 cjigljdikhe，实得 %s", sub.Class)
	}
	if idx := fieldIdxByCS(sub.Class, csNumericID); idx < 0 || idx >= len(sub.Fields) ||
		u64Of(sub.Fields[idx].Num) != 9197 {
		t.Fatalf("f1 内嵌线 numeric_id 应为 9197 (mn_lm_40809)")
	}
	// 顶层强度 = 400LB × 工艺 1.0 × 完好 1.0 = 181kg。
	sIdx := fieldIdxByCS(out.WireClass, csStrength)
	if sIdx < 0 || sIdx >= len(fields) || fields[sIdx].Num == nil {
		t.Fatalf("成品没有强度字段")
	}
	got := float64(proto.F32FromBits(uint32(u64Of(fields[sIdx].Num))))
	if math.Abs(got-181.0) > 0.01 {
		t.Fatalf("减震引线强度应为 181kg (400LB)，实得 %v", got)
	}
}

// 回归（明确选料不得静默换料，2026-11-09 事故）：玩家在槽位里明确指定
// 类目不合格的物品（鱼饵 topwater_1928）做减震引线时，服务端不得跳过
// 它再去扫背包换料。必须整体拒绝，鱼饵和合格主线都原样保留。
func TestCraftShockLeaderExplicitWrongMaterialNotSubstituted(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid = int64(2000044)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	wire, err := srv.CreateItem(ctx, uid, "topwater_1928", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.alignItemRowQty(ctx, wire)
	line, err := srv.CreateItem(ctx, uid, "mono_15065", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.alignItemRowQty(ctx, line)
	wireBefore := itemQtyByGUID(t, db, wire.GUID)
	lineBefore := itemQtyByGUID(t, db, line.GUID)
	sess := &Session{ID: 1, Userid: uid, Srv: srv, log: logging.Discard()}
	hctx := &Ctx{Sess: sess, Main: 9, Sub: 1, CallID: 1, DecodeOK: true,
		Tree: proto.Args524(craftObj8("craft_leader_shock_200", []craftSlot{{ItemID: "craft_mat_line", GUID: wire.GUID}}, 1)), Log: logging.Discard()}
	body, ok := hCraft(hctx)
	if !ok || u64Of(decodeTreeValue(t, srv, body).Fields[0].Num) == 1 {
		t.Fatalf("明确指定鱼饵做减震引线应被拒绝: ok=%v", ok)
	}
	if got := itemQtyByGUID(t, db, wire.GUID); got != wireBefore {
		t.Fatalf("鱼饵不得被消耗: before=%d after=%d", wireBefore, got)
	}
	if got := itemQtyByGUID(t, db, line.GUID); got != lineBefore {
		t.Fatalf("不得静默换料扣掉合格主线: before=%d after=%d", lineBefore, got)
	}
	rows, err := db.ListItemsByContainers(ctx, uid, []string{"backpack"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ItemID == "leader_crafted_shock_200" {
			t.Fatal("不应产出减震引线成品")
		}
	}
}

// 回归（修复扫描不得迁动 f1 前导线）：减震引线放开后，用前导线做出来的
// 成品把内嵌线放在 f1（cat15 外壳）。修复扫描若沿用旧规则"减震引线的线
// 必须在 f3"，会把它迁进 f3 并强制换成 cat11 外壳——cat15 物品配 cat11
// 外壳，客户端渲染成错误身份。扫描必须原样放行。
func TestRepairShockLeaderKeepsLeaderWireInF1(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid = int64(2000047)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	wire, err := srv.CreateItem(ctx, uid, "mn_lm_40809", "backpack", 2)
	if err != nil {
		t.Fatal(err)
	}
	srv.alignItemRowQty(ctx, wire)
	sess := &Session{ID: 1, Userid: uid, Srv: srv, log: logging.Discard()}
	hctx := &Ctx{Sess: sess, Main: 9, Sub: 1, CallID: 1, DecodeOK: true,
		Tree: proto.Args524(craftObj8("craft_leader_shock_200", []craftSlot{{ItemID: "craft_mat_line", GUID: wire.GUID}}, 1)), Log: logging.Discard()}
	if body, ok := hCraft(hctx); !ok || u64Of(decodeTreeValue(t, srv, body).Fields[0].Num) != 1 {
		t.Fatalf("前导线做减震引线应成功")
	}
	rows, err := db.ListItemsByContainers(ctx, uid, []string{"backpack"})
	if err != nil {
		t.Fatal(err)
	}
	var out *store.Item
	for _, row := range rows {
		if row.ItemID == "leader_crafted_shock_200" {
			out = row
		}
	}
	if out == nil {
		t.Fatal("应产出减震引线成品")
	}
	fields, err := FieldsFromJSON(out.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	changed, invalid, err := srv.repairCraftedLeaderWire(ctx, out, fields)
	if err != nil {
		t.Fatal(err)
	}
	if invalid {
		t.Fatal("前导线减震引线不得被判定无效隔离")
	}
	if changed {
		t.Fatal("修复扫描不得迁动 f1 里的前导线")
	}
}

// 兼容锁：客户端提交库里查不到的临时 GUID 时，补扣扫描仍按类目从背包
// 选取合格线材正常出货，不得被"明确选料拒绝"误伤。
func TestCraftShockLeaderTempGUIDFallsBackToScan(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid = int64(2000045)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "mono_15065", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.alignItemRowQty(ctx, line)
	sess := &Session{ID: 1, Userid: uid, Srv: srv, log: logging.Discard()}
	hctx := &Ctx{Sess: sess, Main: 9, Sub: 1, CallID: 1, DecodeOK: true,
		Tree: proto.Args524(craftObj8("craft_leader_shock_200", []craftSlot{{ItemID: "craft_mat_line", GUID: "temp-guid-not-in-db"}}, 1)), Log: logging.Discard()}
	body, ok := hCraft(hctx)
	if !ok || u64Of(decodeTreeValue(t, srv, body).Fields[0].Num) != 1 {
		t.Fatalf("临时 GUID 应按类目补扣正常出货: ok=%v", ok)
	}
	found := false
	rows, err := db.ListItemsByContainers(ctx, uid, []string{"backpack"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ItemID == "leader_crafted_shock_200" {
			found = true
		}
	}
	if !found {
		t.Fatal("补扣扫描应产出减震引线成品")
	}
}

func itemQtyByGUID(t *testing.T, db *store.DB, guid string) int64 {
	t.Helper()
	row, err := db.GetItemByGUID(context.Background(), guid)
	if err != nil || row == nil {
		t.Fatalf("读取 %s: %v", guid, err)
	}
	return row.Qty
}

package gameserver

// 轮根规格表回归：轮根（nlmnpekipjc）官方语义 = 出厂满耐久轮身强度，按
// root_spec × 轴承子件耐久 下发；摩擦片磨损只写摩擦片字段。
// 官方 4/22 实抓锚点（详见 reel_strength_data.go 注释）：
//   Turion SW 30000: 127.94 × 0.98779 ≈ 126.35
//   Tagara II 6000:  45.89 × 0.9756  ≈ 44.77 / 44.42（两颗）

import (
	"context"
	"math"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// reelRootStrengthForTest 读轮根 csStrength 字段（FLOAT/DOUBLE 或 U32 位型）。
func reelRootStrengthForTest(t *testing.T, fields []proto.Field, wireClass string) float64 {
	t.Helper()
	idx := fieldIdxByCS(wireClass, csStrength)
	if idx < 0 || idx >= len(fields) {
		t.Fatalf("轮根字段缺失 idx=%d", idx)
	}
	switch fieldOpAt(wireClass, idx) {
	case "FLOAT", "DOUBLE":
		return fields[idx].Float
	case "U32":
		if fields[idx].Num == nil {
			return 0
		}
		return float64(proto.F32FromBits(uint32(*fields[idx].Num)))
	default:
		t.Fatalf("轮根字段不是浮点位型: %s", fieldOpAt(wireClass, idx))
		return 0
	}
}

func mustCreateReel(t *testing.T, srv *Server, itemID string) *store.Item {
	t.Helper()
	reel, err := srv.CreateItem(context.Background(), 1000001, itemID, "backpack", 1)
	if err != nil {
		t.Fatalf("CreateItem %s: %v", itemID, err)
	}
	return reel
}

func reloadReel(t *testing.T, db *store.DB, guid string) *store.Item {
	t.Helper()
	row, err := db.GetItemByGUID(context.Background(), guid)
	if err != nil || row == nil {
		t.Fatalf("GetItemByGUID %s: %v", guid, err)
	}
	return row
}

// TestReelStrengthTableIsEmbedded 确保规格表随二进制编译，不依赖运行目录或发布包文件白名单。
func TestReelStrengthTableIsEmbedded(t *testing.T) {
	t.Parallel()
	if len(reelStrengthJSON) == 0 {
		t.Fatal("轮根规格表未嵌入二进制")
	}
	loadReelStrengthTable()
	if len(reelStrengthTable) < 500 {
		t.Fatalf("轮根规格表加载条目过少: %d", len(reelStrengthTable))
	}
}

// TestReelStrengthAnchors 锁定规格表锚点值（防转录手误/文件缺失）。
func TestReelStrengthAnchors(t *testing.T) {
	t.Parallel()
	for itemID, want := range map[string]float64{
		"spin_5274": 127.94, // Turion SW 30000，官方实抓轮根 126.35
		"spin_5677": 45.89,  // Tagara II 6000，官方实抓 44.77/44.42
		"spin_6043": 71.2,   // Venga II 6000
	} {
		got, ok := reelRootSpec(itemID)
		if !ok {
			t.Fatalf("规格表缺锚点 %s", itemID)
		}
		if math.Abs(got-want) > 0.01 {
			t.Fatalf("%s root_spec=%f 期望 %f", itemID, got, want)
		}
	}
}

// TestReelRootUsesStrengthSpec 规格表命中的轮子：轮根 = root_spec（轴承满耐久），
// 不再是摩擦片值（旧口径 22.7）。
func TestReelRootUsesStrengthSpec(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	reel := mustCreateReel(t, srv, "spin_6043")
	row := reloadReel(t, db, reel.GUID)
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	root := reelRootStrengthForTest(t, fields, row.WireClass)
	if math.Abs(root-71.2) > 0.02 {
		t.Fatalf("轮根=%f，期望规格 71.2（旧口径摩擦片值 22.7）", root)
	}
}

// TestReelRootScalesWithBearingDurability 轴承耐久下降时轮根按比例下降。
// 官方锚点：0.98779 → 126.35（Turion SW 30000）。
func TestReelRootScalesWithBearingDurability(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	reel := mustCreateReel(t, srv, "spin_6043")
	fields, err := FieldsFromJSON(reel.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	bearing, _, _, ok := srv.selectedReelPart(context.Background(), fields, reelPartCatBearing)
	if !ok || bearing == nil {
		t.Fatal("轮子缺少轴承子件")
	}
	if !setReelChildDurability(bearing, 0.5) {
		t.Fatal("写轴承耐久失败")
	}
	// 落库后再走 canon，保证规格×轴承耐久读取的是持久化状态。
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateItemFields(context.Background(), reel.GUID, fj, reel.Qty, reel.Durability, reel.Workmanship, reel.RigGUID); err != nil {
		t.Fatal(err)
	}
	row := reloadReel(t, db, reel.GUID)
	fields, err = FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !srv.canonicalizeReelStrength(context.Background(), row, fields) {
		t.Fatal("canonicalizeReelStrength 未生效")
	}
	root := reelRootStrengthForTest(t, fields, row.WireClass)
	if math.Abs(root-71.2*0.5) > 0.02 {
		t.Fatalf("轴承 0.5 时轮根=%f，期望 %f", root, 71.2*0.5)
	}
}

// TestReelRootNotCollapsedByWornWasher 摩擦片磨穿（拖力=0）不得把轮根一起写塌：
// 旧口径轮根=摩擦片当前值，磨穿即 0，客户端负载条分母归零直接爆红。
func TestReelRootNotCollapsedByWornWasher(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	reel := mustCreateReel(t, srv, "spin_6043")
	fields, err := FieldsFromJSON(reel.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	wornOut, found := srv.degradeFrictionWasherFields(context.Background(), reel, fields, 1)
	if !found || !wornOut {
		t.Fatalf("磨穿摩擦片: wornOut=%v found=%v", wornOut, found)
	}
	row := reloadReel(t, db, reel.GUID)
	srv.canonicalizeReelStrength(context.Background(), row, fields)
	root := reelRootStrengthForTest(t, fields, row.WireClass)
	if math.Abs(root-71.2) > 0.02 {
		t.Fatalf("摩擦片磨穿后轮根=%f，期望保持规格 71.2", root)
	}
}

// TestReelRootFallbackForUnlistedReel 规格表未收录的轮子回退旧口径（摩擦片值），
// 行为与改动前一致。
func TestReelRootFallbackForUnlistedReel(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	reel := mustCreateReel(t, srv, "old_spinreel")
	row := reloadReel(t, db, reel.GUID)
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reelRootSpec(row.ItemID); ok {
		t.Skipf("%s 已进入规格表，回退用例失效", row.ItemID)
	}
	want := srv.reelBaseDragFromRow(context.Background(), row)
	if want <= 0 {
		t.Fatal("回退轮缺少可信摩擦片值")
	}
	root := reelRootStrengthForTest(t, fields, row.WireClass)
	if math.Abs(root-want) > 0.02 {
		t.Fatalf("未收录轮根=%f，期望旧口径 %f", root, want)
	}
}

// TestReelLockedDragMatchesRootSpec 统一后的锁轮承力口径：规格表收录的轮子
// 锁轮承力 = 规格 × 轴承耐久，与客户端轮根字段同源（机构强度设计，见
// reel_mech_strength_test.go）；摩擦片基准制动力（1~29 档泄力）不变。
func TestReelLockedDragMatchesRootSpec(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	reel := mustCreateReel(t, srv, "spin_6043")
	row := reloadReel(t, db, reel.GUID)
	perf, err := srv.syncReelPerformance(context.Background(), row)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(perf.BaseDragKg-22.7) > 0.05 {
		t.Fatalf("基础制动力=%f，期望摩擦片额定 22.7", perf.BaseDragKg)
	}
	if math.Abs(perf.LockedDragKg-71.2) > 0.02 {
		t.Fatalf("锁轮承力=%f，期望机构强度 71.2（旧口径 22.7×1.4=31.8 已废弃）", perf.LockedDragKg)
	}
}

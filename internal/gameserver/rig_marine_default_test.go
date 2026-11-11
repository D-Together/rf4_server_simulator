package gameserver

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMarineRodDefaultRigType 验证海钓竿购买后按竿型选择正确默认钓组：
// 船竿 16 必须是经典海钓沉底，而不是回退到浮钓简易钓组。
func TestMarineRodDefaultRigType(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	cases := []struct {
		itemID   string
		wantType int64
		wantName string
	}{
		{"boatr_10213_050", 55, "rig_MarineBottomClassic"}, // WTA 70-050 船竿
		{"pilkr_10208_f88", 50, "rig_MarinePilker"},        // Pilker 竿
	}
	for _, tc := range cases {
		t.Run(tc.itemID, func(t *testing.T) {
			rod, err := srv.CreateItem(ctx, uid, tc.itemID, "backpack", 1)
			if err != nil {
				t.Fatalf("造竿失败: %v", err)
			}
			srv.ensureRig(ctx, uid, rod.GUID)

			row, err := db.GetItemByGUID(ctx, rod.GUID)
			if err != nil || row == nil {
				t.Fatalf("读取竿失败: row=%v err=%v", row, err)
			}
			f5 := rigStoredF5(t, row)
			if got := f5.Fields[0].StrOr(); got != tc.wantName {
				t.Fatalf("默认钓组名 = %q，期望 %q", got, tc.wantName)
			}
			if got := int64(u64Of(f5.Fields[1].Num)); got != tc.wantType {
				t.Fatalf("默认钓组类型 = %d，期望 %d", got, tc.wantType)
			}
			if got, found, err := db.GetRigType(ctx, rod.GUID); err != nil || !found || got != tc.wantType {
				t.Fatalf("rig 表类型 = %d found=%v err=%v，期望 %d", got, found, err, tc.wantType)
			}
		})
	}
}

// TestExistingMarineRodFloatRigIsCorrected 验证已被错误写成浮钓的存量海竿
// 会在 ensureRig 的兼容性矫正中收敛到经典海钓沉底。
func TestExistingMarineRodFloatRigIsCorrected(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "boatr_10213_050", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	// 模拟旧版本已落库的不兼容浮钓类型。17/1 现在会正确拒绝这类
	// 新请求，因此遗留数据必须直接写入，才能覆盖登录/投影矫正路径。
	row, err := db.GetItemByGUID(ctx, rod.GUID)
	if err != nil || row == nil {
		t.Fatalf("读取竿失败: row=%v err=%v", row, err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(row.WireClass, csRigObject)
	if idx < 0 {
		t.Fatal("竿实例缺少钓组字段")
	}
	fields[idx].Val = buildRig153Like(fields[idx].Val, "rig_FloatSimple", 1, rigCompsOf(fields[idx].Val), 100, 0)
	srv.saveItemFields(ctx, row, fields)
	if err := db.UpsertRig(ctx, rod.GUID, uid, 1, "", "{}"); err != nil {
		t.Fatal(err)
	}
	srv.ensureRig(ctx, uid, rod.GUID)

	row, err = db.GetItemByGUID(ctx, rod.GUID)
	if err != nil || row == nil {
		t.Fatalf("读取竿失败: row=%v err=%v", row, err)
	}
	f5 := rigStoredF5(t, row)
	if got := f5.Fields[0].StrOr(); got != "rig_MarineBottomClassic" {
		t.Fatalf("矫正后的钓组名 = %q，期望 rig_MarineBottomClassic", got)
	}
	if got := int64(u64Of(f5.Fields[1].Num)); got != 55 {
		t.Fatalf("矫正后的钓组类型 = %d，期望 55", got)
	}
	if got, found, err := db.GetRigType(ctx, rod.GUID); err != nil || !found || got != 55 {
		t.Fatalf("矫正后 rig 表类型 = %d found=%v err=%v，期望 55", got, found, err)
	}
}

// TestAllRodTypesResolveMappedRigType 防止后续新增竿型或允许钓组时，
// 再次因缺少 rig_* 名称映射而静默回退到固定浮标钓组。
func TestAllRodTypesResolveMappedRigType(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()

	rows, err := db.SQL.Query(`SELECT item_id, attrs_json FROM item_def WHERE category=3`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	firstByType := map[int64]string{}
	for rows.Next() {
		var itemID, attrsJSON string
		if err := rows.Scan(&itemID, &attrsJSON); err != nil {
			t.Fatal(err)
		}
		var attrs map[string]any
		if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
			t.Fatalf("%s attrs_json 解析失败: %v", itemID, err)
		}
		rodType, ok := attrs["fopbhlmkcpe"].(float64)
		if !ok {
			t.Fatalf("%s 缺少竿型 fopbhlmkcpe", itemID)
		}
		if _, seen := firstByType[int64(rodType)]; !seen {
			firstByType[int64(rodType)] = itemID
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for rodType, itemID := range firstByType {
		got := srv.defaultRigTypeForRod(ctx, itemID)
		if rigTypeNames[got] == "" {
			t.Errorf("竿型 %d（%s）默认钓组 %d 没有 rig_* 名称映射", rodType, itemID, got)
		}
		if allowed, ok := rodRigAllowed[rodType]; ok && !allowedContains(allowed, got) {
			t.Errorf("竿型 %d（%s）默认钓组 %d 不在允许矩阵 %v 内", rodType, itemID, got, allowed)
		}
	}
}

// TestRodRigAllowedTypesHaveNames 锁定允许矩阵与资源名映射的一致性。
func TestRodRigAllowedTypesHaveNames(t *testing.T) {
	for rodType, allowed := range rodRigAllowed {
		if len(allowed) == 0 {
			t.Errorf("竿型 %d 的允许钓组为空", rodType)
		}
		for _, rigType := range allowed {
			if rigTypeNames[rigType] == "" {
				t.Errorf("竿型 %d 允许钓组 %d 没有 rig_* 名称映射", rodType, rigType)
			}
		}
	}
}

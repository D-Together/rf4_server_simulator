package gameserver

import (
	"context"
	"testing"
)

// TestOtherBottomRigsMountLeaderKeepExplicitType 锁住其他沉底钓组的装配关系：
// 玩家选好钓组后首次安装、重复安装同一根引线，都不能改掉钓组身份或复制投影。
func TestOtherBottomRigsMountLeaderKeepExplicitType(t *testing.T) {
	cases := []struct {
		name    string
		rigType int64
		rigName string
	}{
		{"Paternoster", 13, "rig_BottomPaternoster"},
		{"Inline", 14, "rig_BottomInline"},
		{"Loop", 15, "rig_BottomLoop"},
		{"Method", 16, "rig_BottomMethod"},
		{"LiveFish", 17, "rig_BottomLiveFish"},
		{"Hair", 24, "rig_BottomHair"},
		{"Snowman", 25, "rig_BottomSnowman"},
		{"Popup", 26, "rig_BottomPopup"},
		{"HingedStiff", 27, "rig_BottomHingedStiff"},
		{"Chod", 28, "rig_BottomChod"},
		{"Maggot", 29, "rig_BottomMaggot"},
		{"MethodPopup", 32, "rig_BottomMethodPopUp"},
		{"FixedZig", 34, "rig_BottomFixedZigRig"},
	}

	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
			if err != nil {
				t.Fatal(err)
			}
			if !srv.rigSetType(ctx, uid, rod.GUID, tc.rigType) {
				t.Fatalf("选择钓组 %d 失败", tc.rigType)
			}
			leader, err := srv.CreateItem(ctx, uid, "sh_leader_40815", "backpack", 1)
			if err != nil {
				t.Fatal(err)
			}
			beforeQty := leader.Qty
			sess := newProbeSess(srv)
			runRodSet(t, srv, sess, rod.GUID, leader.GUID, 0)
			runRodSet(t, srv, sess, rod.GUID, leader.GUID, 0)

			row, err := db.GetItemByGUID(ctx, rod.GUID)
			if err != nil {
				t.Fatal(err)
			}
			name, rigType, ok := rigIdentity(rigStoredF5(t, row))
			if !ok || name != tc.rigName || rigType != tc.rigType {
				t.Fatalf("重复装引线后钓组身份=%q/%d/%v，期望 %q/%d/true",
					name, rigType, ok, tc.rigName, tc.rigType)
			}
			if projected, found, err := db.GetRigType(ctx, rod.GUID); err != nil || !found || projected != tc.rigType {
				t.Fatalf("rig_type=%d found=%v err=%v，期望 %d", projected, found, err, tc.rigType)
			}
			mounted, err := db.GetItemByGUID(ctx, leader.GUID)
			if err != nil || mounted.Container != "rig" || mounted.RigGUID != rod.GUID || mounted.Qty != beforeQty {
				t.Fatalf("引线数量或归属错误: row=%+v err=%v", mounted, err)
			}
			rows, err := db.ListRigComponents(ctx, rod.GUID)
			if err != nil {
				t.Fatal(err)
			}
			leaders := 0
			for _, comp := range rows {
				if comp.Slot != catLead {
					continue
				}
				leaders++
				if comp.Ordinal != 0 || comp.GUID != leader.GUID || comp.Extra != 150 {
					t.Fatalf("引线投影错误: %+v", comp)
				}
			}
			if leaders != 1 {
				t.Fatalf("引线投影数量=%d，期望 1: %+v", leaders, rows)
			}
		})
	}
}

// TestLeaderBreakKeepsLiteralUpstreamCategoriesAcrossRigs 用业务字面预期
// 校验物理链，而不是用模板自身计算期望，避免模板顺序写错时测试同步写错。
func TestLeaderBreakKeepsLiteralUpstreamCategoriesAcrossRigs(t *testing.T) {
	cases := []struct {
		name    string
		rigType int64
		keep    []int64
	}{
		{"FloatSimple", 1, []int64{28}},
		{"FloatBolognese", 2, []int64{28}},
		{"FloatSlider", 3, []int64{28}},
		{"FloatLiveFish", 4, []int64{28}},
		{"Carolina", 9, []int64{29, 33}},
		{"ThreeWay", 10, []int64{29}},
		{"BottomSimple", 12, []int64{47, 29, 31}},
		{"BottomPaternoster", 13, []int64{47}},
		{"BottomInline", 14, []int64{47}},
		{"BottomLoop", 15, []int64{47}},
		{"BottomMethod", 16, []int64{47, 57, 31}},
		{"BottomLiveFish", 17, []int64{47, 29}},
		{"SbirolinoLure", 19, []int64{44}},
		{"SbirolinoSoftPlastic", 20, []int64{44}},
		{"FloatWaggler", 21, []int64{28}},
		{"BottomMarker", 23, []int64{28, 29}},
		{"BottomHair", 24, []int64{47, 57, 29}},
		{"BottomSnowman", 25, []int64{47, 57, 29}},
		{"BottomPopup", 26, []int64{47, 57, 29}},
		{"BottomHingedStiff", 27, []int64{47, 57, 29}},
		{"BottomChod", 28, []int64{47, 57, 29}},
		{"BottomMaggot", 29, []int64{47, 57, 29}},
		{"BottomMethodPopup", 32, []int64{47, 57, 31}},
		{"BottomFixedZig", 34, []int64{47, 57, 29}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lost := downstreamLossCategories(tc.rigType, catLead)
			if !lost[catLead] || !lost[catHook] {
				t.Fatalf("断引线必须丢失引线和下游钩: lost=%v", lost)
			}
			for _, category := range tc.keep {
				if lost[category] {
					t.Fatalf("断引线误删上游品类 %d: lost=%v", category, lost)
				}
			}
		})
	}
}

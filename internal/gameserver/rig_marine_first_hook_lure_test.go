package gameserver

// 海钓首钩位装拟饵的槽位索引回归（2026-11-09 实机，杆 3c0d7f44 rig_MarinePilker/50）：
//
//	20:57:35 装配 parent=3c0d7f44 slot=12 ordinal=1 item=f5926c07 extra=1001   ← 首钩位加钩
//	20:57:37 装配 parent=3c0d7f44 slot=17 ordinal=0 item=54b5d2cc extra=1002   ← 首钩位装泡沫橡胶鱼拟饵
//	20:58:04 装配 parent=3c0d7f44 slot=17 ordinal=0 item=37a1ea5f extra=300    ← 柳叶亮片被顶回
//
// 历史日志里服务端自己打过告警，直接点名这次改写：
//
//	11:50:27 诊断 槽位索引改写 parent=3c0d7f44 rig_type=50 cat=17 ordinal=0 \
//	         client_extra=1002 stored_extra=300 item=48975541
//
// 落库实况（data/rf4server.db，rig 3c0d7f44）：
//
//	f5  slot=17 guid=54b5d2cc(hm_foamr_80red_hq 泡沫橡胶鱼) extra=300
//	    slot=17 guid=3ffc4b9b(color_bead)                  extra=325
//	    slot=17 guid=15062137(fire_tube)                   extra=326
//	item 37a1ea5f(pilkerlures_2630 柳叶亮片) container=backpack rig=""  ← 掉回背包
//
// 根因：canonicalIndexForOrdinal 的 1000+ 原样保留分支只认 catHook。
// 客户端对「钩位子槽」用的是 1000 基址 + 4×钩序 + 子槽：
// 1001=首钩、1002=首钩拟饵、1005/1006=第二钩、1009/1010=第三钩
// （与维护者给的 faocjcaiac 从 1009 起每次 −4 遍历到 1001 一致）。
// catLure 的 1002 因此被当成非法索引，按 ordinal 改写成模板首索引 300，
// 与柳叶亮片的 300 撞车：亮片被去重退回背包，客户端在自己的 1002 槽位
// 找不到组件，整组判无效（钩子/鱼片/吸引成分变黑）。

import "testing"

// TestMarineFirstHookLureKeepsClientIndex 断言客户端给拟饵上报 1000+ 钩位
// 子槽索引时必须原样保留。修复前 catLure 走模板规范化，1002 → 300。
func TestMarineFirstHookLureKeepsClientIndex(t *testing.T) {
	for _, tc := range []struct {
		rigType     int64
		clientExtra int64
	}{
		{50, 1002},
		{51, 1002},
		{50, 1006},
		{50, 1010},
	} {
		got := canonicalIndexForOrdinal(catLure, tc.clientExtra, tc.rigType, 0)
		if got != tc.clientExtra {
			t.Fatalf("rig %d：客户端上报拟饵钩位索引 %d，被改写成 %d；"+
				"改写后与模板拟饵索引撞车，会让亮片被挤出、整组判无效",
				tc.rigType, tc.clientExtra, got)
		}
	}
}

// TestMarineFirstHookLureDoesNotEvictPilker 复现实机四个症状的服务端形态：
// 首钩位（250 钩 + 1001 钩）装上泡沫橡胶鱼（1002）后，
// 柳叶亮片（300）、基本吸引成分（325）、钓钩吸引成分（326）必须全部还在原位。
func TestMarineFirstHookLureDoesNotEvictPilker(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := t.Context()
	const uid = int64(1000001)

	rod, err := s.CreateItem(ctx, uid, "boatr_10222_SBH80_100", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !s.rigSetType(ctx, uid, rod.GUID, 50) {
		t.Fatal("rigSetType(50) 失败")
	}

	mount := func(cat, extra int64, itemID string) string {
		t.Helper()
		item, err := s.CreateItem(ctx, uid, itemID, "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		res := s.rigSetComponent(ctx, uid, rod.GUID, cat, item.GUID, extra,
			rigSetOpts{ParentType: familyRod, EnforceCategory: true})
		if res.Rejected || res.MountedGUID == "" {
			t.Fatalf("装 %s(extra=%d) 被拒: %+v", itemID, extra, res)
		}
		return res.MountedGUID
	}

	pilker := mount(catLure, 300, "pilkerlures_2630") // 柳叶亮片
	bead := mount(catLure, 325, "color_bead_009")     // 基本吸引成分
	tube := mount(catLure, 326, "fire_tube_006")      // 钓钩吸引成分
	mount(catHook, 250, "mhook_20924")                // 首钩位主钩
	mount(catHook, 1001, "mhook_20894")               // 首钩位加钩
	foam := mount(catLure, 1002, "hm_foamr_80red_hq") // 首钩位泡沫橡胶鱼拟饵

	comps := s.storedRigComponents(ctx, rod.GUID)
	byGUID := map[string]rigComp{}
	for _, c := range comps {
		byGUID[c.GUID] = c
	}
	// 四件拟饵族必须各占一个互不相同的索引，一个都不能被挤掉。
	want := map[string]int64{
		pilker: 300,
		bead:   325,
		tube:   326,
		foam:   1002,
	}
	seen := map[int64]string{}
	for guid, extra := range want {
		c, ok := byGUID[guid]
		if !ok {
			t.Fatalf("组件 %s 被挤出钓组（实机症状：柳叶亮片掉落）", shortGUID(guid))
		}
		if c.Extra != extra {
			t.Fatalf("组件 %s 索引 = %d，期望 %d（1000+ 客户端钩位子槽被改写会让索引撞车）",
				shortGUID(guid), c.Extra, extra)
		}
		if prev, dup := seen[c.Extra]; dup {
			t.Fatalf("索引 %d 被两条组件占用: %s / %s（客户端按索引渲染，重复即整组变黑）",
				c.Extra, prev, shortGUID(guid))
		}
		seen[c.Extra] = guid
	}
	if n := len(rigCompKeysOfCategory(comps, catLure)); n != 4 {
		t.Fatalf("拟饵族组件 = %d 个，期望 4 个", n)
	}
}

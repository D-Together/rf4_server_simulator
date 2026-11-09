package gameserver

import (
	"math/rand"
	"net"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/tunnel"
)

func randIntnFromSeed(seed int64) func(int) int {
	rng := rand.New(rand.NewSource(seed))
	return rng.Intn
}

func TestRollBaitDigLootKinds(t *testing.T) {
	// 熟练度 100：铲子取 80 档表，最多 7 种可选，一次最多出 3 种且不重复。
	table := baitLootTableFor(baitShovelItemID, 100)
	allowed := map[string]bool{}
	for _, entry := range table {
		allowed[entry.Key] = true
	}
	multi := 0
	for seed := int64(1); seed <= 500; seed++ {
		rng := randIntnFromSeed(seed)
		loot := rollBaitDigLoot(baitShovelItemID, 100, rng)
		if len(loot) < 1 || len(loot) > 3 {
			t.Fatalf("seed=%d 种类=%d, want 1..3", seed, len(loot))
		}
		if len(loot) > 1 {
			multi++
		}
		seen := map[string]bool{}
		for _, drop := range loot {
			if !allowed[drop.Key] {
				t.Fatalf("seed=%d 未授权饵 %s", seed, drop.Key)
			}
			if seen[drop.Key] {
				t.Fatalf("seed=%d 饵重复: %+v", seed, loot)
			}
			seen[drop.Key] = true
			if drop.Qty < 1 || drop.Qty > 3 {
				t.Fatalf("seed=%d %s 数量=%d, want 1..3", seed, drop.Key, drop.Qty)
			}
		}
	}
	if multi < 100 {
		t.Fatalf("500 次只有 %d 次出多饵，随机分布异常", multi)
	}
}

func TestBaitDigKindCountMasteryGate(t *testing.T) {
	// 熟练度不足时不开第三种；概率边界用固定 rng 验证。
	always := func(int) int { return 0 }
	if got := baitDigKindCount(0, always); got != 2 {
		t.Fatalf("低熟练度 kindCount=%d, want 2（第 2 种必出，第 3 种未解锁）", got)
	}
	if got := baitDigKindCount(baitThirdKindMastery, always); got != 3 {
		t.Fatalf("达标熟练度 kindCount=%d, want 3", got)
	}
	never := func(n int) int { return n - 1 }
	if got := baitDigKindCount(100, never); got != 1 {
		t.Fatalf("rng 拉满时 kindCount=%d, want 1", got)
	}
}

func TestBaitDigReportsGainAndKeepsStackTotal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tool    string
		initial int64
		mastery float64
	}{
		{name: "first_and_repeated_gain", tool: baitShovelItemID},
		{name: "merged_shovel", tool: baitShovelItemID, initial: 100},
		{name: "merged_mastered_shovel", tool: baitShovelItemID, initial: 100, mastery: 100},
		{name: "merged_scoop", tool: baitScoopItemID, initial: 100},
		{name: "merged_new_year_shovel", tool: "shovel_ny25", initial: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newDetachTestServer(t)
			ctx := t.Context()
			const uid = int64(1000001)
			tool, err := srv.CreateItem(ctx, uid, tc.tool, "backpack", 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.ExecContext(ctx, `INSERT OR REPLACE INTO skill(uid, skill_id, points) VALUES(?,?,?)`,
				uid, captureBaitSkillID, tc.mastery); err != nil {
				t.Fatal(err)
			}
			before := map[string]int64{}
			guids := map[string]string{}
			for _, bands := range baitDigBands {
				for _, band := range bands {
					for _, entry := range band.Table {
						if _, seen := before[entry.Key]; seen {
							continue
						}
						before[entry.Key] = tc.initial
						if tc.initial == 0 {
							continue
						}
						row, err := srv.prepareNewItem(ctx, uid, entry.Key, "backpack", 1, "")
						if err != nil {
							t.Fatal(err)
						}
						if err := setPreparedItemQuantity(row, tc.initial); err != nil {
							t.Fatal(err)
						}
						if err := db.InsertItem(ctx, row); err != nil {
							t.Fatal(err)
						}
						guids[entry.Key] = row.GUID
					}
				}
			}
			for dig := 1; dig <= 2; dig++ {
				hctx := &Ctx{Sess: newProbeSess(srv), Main: 9, Sub: 3,
					Tree: proto.Args524(proto.Guid(tool.GUID)), DecodeOK: true, Log: logging.Discard()}
				body, handled := hCraftMine2(hctx)
				if !handled || len(body) == 0 {
					t.Fatalf("dig %d was not handled", dig)
				}
				result := decodeTreeValue(t, srv, body)
				if result.TypeID != 803 || u64Of(result.Field(0).Num) != 1 {
					t.Fatalf("dig %d did not return successful 803: %+v", dig, result)
				}
				produced := baitDigItems(t, result.Field(1).Val)
				if len(produced) < 1 || len(produced) > 3 {
					t.Fatalf("dig %d 产出种类=%d, want 1..3", dig, len(produced))
				}
				type gained struct {
					guid  string
					qty   int64
					stack int64
				}
				records := make([]gained, 0, len(produced))
				seen := map[string]bool{}
				for _, item := range produced {
					guid := fieldGuidHex(item, fieldIdxByCS(item.Class, csGUID))
					row, err := db.GetItemByGUID(ctx, guid)
					if err != nil || row == nil || row.UID != uid || row.Container != "backpack" {
						t.Fatalf("dig %d result does not identify the backpack stack: row=%+v err=%v", dig, row, err)
					}
					if seen[row.ItemID] {
						t.Fatalf("dig %d 同一种饵出现两次: %s", dig, row.ItemID)
					}
					seen[row.ItemID] = true
					prior, allowed := before[row.ItemID]
					if !allowed {
						t.Fatalf("unexpected bait %q", row.ItemID)
					}
					gain := row.Qty - prior
					maxGain := int64(1)
					if tc.mastery == 100 {
						maxGain = 3
					}
					if gain < 1 || gain > maxGain {
						t.Fatalf("dig %d %s gain=%d, want 1..%d", dig, row.ItemID, gain, maxGain)
					}
					if got := baitDigTreeQuantity(t, item); got != gain {
						t.Fatalf("dig %d 803.f1 %s gain=%d, want %d (prior=%d, backpack total=%d)",
							dig, row.ItemID, got, gain, prior, row.Qty)
					}
					if old := guids[row.ItemID]; old != "" && old != guid {
						t.Fatalf("merge changed stack GUID: %s -> %s", old, guid)
					}
					storedTree, err := srv.ItemToTree(ctx, row)
					if err != nil {
						t.Fatal(err)
					}
					if got := baitDigTreeQuantity(t, storedTree); got != row.Qty {
						t.Fatalf("persisted fields quantity=%d, row qty=%d", got, row.Qty)
					}
					var stacks int
					if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item WHERE uid=? AND item_id=? AND container='backpack'`,
						uid, row.ItemID).Scan(&stacks); err != nil {
						t.Fatal(err)
					}
					if stacks != 1 {
						t.Fatalf("bait %s has %d stacks, want 1", row.ItemID, stacks)
					}
					before[row.ItemID], guids[row.ItemID] = row.Qty, guid
					records = append(records, gained{guid: guid, qty: gain, stack: row.Qty})
				}
				for _, idx := range []int{6, 7} {
					inventory := baitDigItems(t, result.Field(idx).Val)
					if len(inventory) != len(records) {
						t.Fatalf("dig %d 803.f%d 件数=%d, want %d", dig, idx, len(inventory), len(records))
					}
					for i, item := range inventory {
						if got := baitDigTreeQuantity(t, item); got != records[i].stack {
							t.Fatalf("dig %d 803.f%d[%d] inventory total=%d, want %d",
								dig, idx, i, got, records[i].stack)
						}
						if got := fieldGuidHex(item, fieldIdxByCS(item.Class, csGUID)); got != records[i].guid {
							t.Fatalf("803.f%d[%d] stack GUID=%s, want %s", idx, i, got, records[i].guid)
						}
					}
				}
			}
			if kept, err := db.GetItemByGUID(ctx, tool.GUID); err != nil || kept == nil || kept.ItemID != tc.tool {
				t.Fatalf("digging removed the tool: row=%+v err=%v", kept, err)
			}
		})
	}
}

func TestBaitDigReportsAchievementForEveryProducedKind(t *testing.T) {
	srv, db := newDetachTestServer(t)
	ctx := t.Context()
	const uid = int64(1000001)
	tool, err := srv.CreateItem(ctx, uid, baitScoopItemID, "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hctx := &Ctx{Sess: newProbeSess(srv), Main: 9, Sub: 3,
		Tree: proto.Args524(proto.Guid(tool.GUID)), DecodeOK: true, Log: logging.Discard()}
	body, handled := hCraftMine2(hctx)
	if !handled || len(body) == 0 {
		t.Fatal("9/3 挖饵未处理")
	}
	result := decodeTreeValue(t, srv, body)
	produced := baitDigItems(t, result.Field(1).Val)

	achievementByBait := map[string]int64{
		"worm_01":         250,
		"bloodworm":       258,
		"caddisfly":       259,
		"naiad":           261,
		"unibeetle_larva": 272,
	}
	states, err := db.PlayerAchievementStates(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, item := range produced {
		guid := fieldGuidHex(item, fieldIdxByCS(item.Class, csGUID))
		row, err := db.GetItemByGUID(ctx, guid)
		if err != nil || row == nil {
			t.Fatalf("产出的饵没有落库: guid=%s err=%v", guid, err)
		}
		achID, ok := achievementByBait[row.ItemID]
		if !ok {
			continue
		}
		checked++
		st := states[achID]
		if !st.Unlocked || !st.Claimed {
			t.Fatalf("产出 %s 后成就 %d 未解锁: %+v", row.ItemID, achID, st)
		}
	}
	if checked == 0 {
		t.Fatalf("本次产出 %v 没有覆盖任何挖饵成就", produced)
	}
}

func TestBaitDigInventoryPushKeepsStackTotal(t *testing.T) {
	srv, db := newDetachTestServer(t)
	ctx := t.Context()
	const uid = int64(1000001)
	tool, err := srv.CreateItem(ctx, uid, baitShovelItemID, "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"worm_01", "maggot"} {
		row, err := srv.prepareNewItem(ctx, uid, key, "backpack", 1, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := setPreparedItemQuantity(row, 100); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertItem(ctx, row); err != nil {
			t.Fatal(err)
		}
	}

	serverNet, peerNet := net.Pipe()
	t.Cleanup(func() { _ = serverNet.Close(); _ = peerNet.Close() })
	if err := serverNet.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := peerNet.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ready := make(chan *tunnel.Conn, 1)
	handshakeErr := make(chan error, 1)
	go func() {
		conn, err := tunnel.HandshakeServer(serverNet, srv.cfg.Tunnel.PSK)
		if err != nil {
			handshakeErr <- err
			return
		}
		ready <- conn
	}()
	peer, err := tunnel.HandshakeClient(peerNet, srv.cfg.Tunnel.PSK)
	if err != nil {
		t.Fatal(err)
	}
	var conn *tunnel.Conn
	select {
	case conn = <-ready:
	case err := <-handshakeErr:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	hctx := &Ctx{Sess: newSession(srv, 1, "baitdig-push-test", uid, conn), Main: 9, Sub: 3,
		Tree: proto.Args524(proto.Guid(tool.GUID)), DecodeOK: true, Log: logging.Discard()}
	body, handled := hCraftMine2(hctx)
	// 挖饵成功后先推技能树，再按产出种类各推一条背包增量；这里只要求
	// 至少有一条回调，不断言回调总数（成就解锁也可能追加）。
	if !handled || len(body) == 0 || len(hctx.afterResponse) < 1 {
		t.Fatalf("digging did not register inventory push: handled=%t callbacks=%d", handled, len(hctx.afterResponse))
	}
	result := decodeTreeValue(t, srv, body)
	kinds := result.Field(6).Val.ArgCount()
	if kinds < 1 || kinds > 3 {
		t.Fatalf("803.f6 种类=%d, want 1..3", kinds)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		hctx.afterResponse[0]()
	}()
	// 第一帧是 2/4 技能树，随后每种饵各一条 4/22。
	for i := 0; i < kinds+1; i++ {
		kind, sid, payload, err := peer.ReadRecord()
		if err != nil {
			t.Fatal(err)
		}
		env, ok := proto.ParseEnvelope(payload)
		wantMain, wantSub := uint8(2), uint8(4)
		if i > 0 {
			wantMain, wantSub = 4, 22
		}
		if kind != tunnel.TypeData || sid != 1 || !ok || env.Main != wantMain || env.Sub != wantSub {
			t.Fatalf("expected %d/%d push, got kind=%v sid=%d envelope=%+v", wantMain, wantSub, kind, sid, env)
		}
		if i == 0 {
			continue
		}
		inventory := baitDigItems(t, decodeTreeValue(t, srv, env.Body))
		if len(inventory) != 1 {
			t.Fatalf("4/22 应只带一件物品，实际 %d", len(inventory))
		}
		item := inventory[0]
		guid := fieldGuidHex(item, fieldIdxByCS(item.Class, csGUID))
		row, err := db.GetItemByGUID(ctx, guid)
		if err != nil || row == nil {
			t.Fatalf("4/22 does not match persisted stack: row=%+v err=%v", row, err)
		}
		if row.Qty < 101 || row.Qty > 103 {
			t.Fatalf("4/22 stack total=%d, want 101..103", row.Qty)
		}
		if got := baitDigTreeQuantity(t, item); got != row.Qty {
			t.Fatalf("4/22 inventory total=%d, want %d (not acquisition gain)", got, row.Qty)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("after-response callback did not finish")
	}
}

// baitDigItems 解出 803.f1/f6/f7 的物品数组。
func baitDigItems(t *testing.T, list *proto.Value) []*proto.Value {
	t.Helper()
	if list == nil || list.Kind != proto.KindArgs || list.ArgCount() < 1 {
		t.Fatalf("expected bait item list, got %+v", list)
	}
	out := make([]*proto.Value, 0, list.ArgCount())
	for i := 0; i < list.ArgCount(); i++ {
		item := list.ArgItem(i)
		if item.Kind != proto.KindObj {
			t.Fatalf("expected bait item object, got %+v", item)
		}
		out = append(out, item)
	}
	return out
}

func baitDigTreeQuantity(t *testing.T, tree *proto.Value) int64 {
	t.Helper()
	idx := fieldIdxByCS(tree.Class, csQty)
	if idx < 0 || tree.Field(idx).Num == nil {
		t.Fatalf("bait tree has no quantity field: %+v", tree)
	}
	return int64(u64Of(tree.Field(idx).Num))
}

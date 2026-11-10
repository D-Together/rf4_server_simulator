package gameserver

import (
	"fmt"
	"testing"
	"time"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/store"
)

// newVariantTestServer 造一个只带 tunables 缓存的 Server（不连库、不启网络）。
// 变体族名推导只读 tunables + 档位线缓存，用不到完整服务端。
func newVariantTestServer(t *testing.T, tun map[string]float64) *Server {
	t.Helper()
	srv := NewServer(config.Game{}, logging.Discard(), nil)
	srv.tunMu.Lock()
	srv.tunCache = tun
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	return srv
}

// 大西洋鲑的档位线：4000 入档 / 30000 奖杯 / 38000 蓝冠（与线上一致）。
func seedSalmonLines(t *testing.T) *store.DB {
	t.Helper()
	db := openItemTestDB(t)
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO fish_grade_lines
		(fish_key, qualified_g, trophy_g, blue_trophy_g, source, checked_at)
		VALUES ('a_salmon', 4000, 30000, 38000, 'test', '2026-01-01')`); err != nil {
		t.Fatalf("写入档位线: %v", err)
	}
	return db
}

func TestPickFishModelVariantStages(t *testing.T) {
	const q, b = 4000, 38000
	cases := []struct {
		name    string
		species string
		weight  int64
		length  float64
		male    bool
		want    string
	}{
		{"幼鱼 parr", "a_salmon", 800, 0.18, true, "a_salmon_parr"},
		{"降海幼鱼 smolt", "a_salmon", 3000, 0.30, false, "a_salmon_smolt"},
		// 关键回归：7.7kg / 0.95m 已经过了鱼苗尺度，必须走成鱼族，
		// 否则渔护列表缩略图会空白（鱼苗模型塞进成鱼尺寸槽）。
		{"7.7kg 成鱼体型不得用鱼苗族", "a_salmon", 7724, 0.95, false, "a_salmon_f1"},
		{"成鱼第一期(雌)", "a_salmon", 12000, 1.09, false, "a_salmon_f1"},
		{"成鱼第二期(雄)", "a_salmon", 20000, 1.30, true, "a_salmon_m2"},
		{"蓝冠线附近(雌)", "a_salmon", 37000, 1.61, false, "a_salmon_f3"},
		{"超蓝冠也封顶", "a_salmon", 200000, 2.60, true, "a_salmon_m3"},
		{"大鳞麻哈鱼 5 期", "chavycha", 59000, 1.80, false, "chavycha_f_5"},
		{"大马哈鱼 2 期", "keta", 37000, 1.50, true, "keta_m_2"},
		{"无变体族鱼种", "perch", 500, 0.20, true, ""},
	}
	for _, tc := range cases {
		got := pickFishModelVariant(tc.species, tc.weight, q, b, tc.male, tc.length,
			defaultFishVariantParrMaxG, defaultFishVariantSmoltMaxG, defaultFishVariantJuvenileMaxM)
		if got != tc.want {
			t.Fatalf("%s：族名=%q want=%q", tc.name, got, tc.want)
		}
	}
}

// 体长缺失（老数据）时不允许用鱼苗族：成鱼尺寸槽取不到对应模型会渲染成空白。
func TestFishModelVariantJuvenileNeedsLength(t *testing.T) {
	if got := pickFishModelVariant("a_salmon", 800, 4000, 38000, true, 0,
		defaultFishVariantParrMaxG, defaultFishVariantSmoltMaxG, defaultFishVariantJuvenileMaxM); got == "a_salmon_parr" {
		t.Fatalf("体长缺失时不应判成幼鱼族，得到 %q", got)
	}
	// 体长在阈值内才放行。
	if got := pickFishModelVariant("a_salmon", 800, 4000, 38000, true, 0.20,
		defaultFishVariantParrMaxG, defaultFishVariantSmoltMaxG, defaultFishVariantJuvenileMaxM); got != "a_salmon_parr" {
		t.Fatalf("0.20m 的 800g 幼鱼应走 parr，得到 %q", got)
	}
}

// 同一尾鱼（GUID）必须永远得到同一个族名，否则溜鱼/结算/渔护会各显示一套模型。
func TestFishModelVariantStablePerFish(t *testing.T) {
	db := seedSalmonLines(t)
	srv := newVariantTestServer(t, map[string]float64{})
	srv.SetDB(db)

	const guid = "9f2c1d4e6a8b0c1d2e3f405162738495"
	first := srv.fishModelVariantForFish("a_salmon", 20000, 1.30, 2, guid)
	if first == "" {
		t.Fatal("大西洋鲑应当有变体族")
	}
	for i := 0; i < 20; i++ {
		if got := srv.fishModelVariantForFish("a_salmon", 20000, 1.30, 2, guid); got != first {
			t.Fatalf("同一尾鱼第 %d 次族名=%q，首次=%q", i, got, first)
		}
	}
	if got := srv.fishModelVariantForFish("perch", 20000, 0.30, 2, guid); got != "" {
		t.Fatalf("无变体族鱼种应留空，得到 %q", got)
	}
}

// 雌雄由 GUID 哈希决定：多次不同 GUID 必须雌雄都出现，且六套成鱼族都能命中。
func TestFishModelVariantCoversEveryFamily(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 600; i++ {
		guid := fmt.Sprintf("%032x", i*2654435761)
		// 期数只跟体重走（入档线 4000、蓝冠线 38000 三等分），
		// 这里挑三档中点把三期全扫一遍。
		for _, w := range []int64{12000, 21000, 36000} {
			if v := pickFishModelVariant("a_salmon", w, 4000, 38000, fishVariantMale(guid), 1.30,
				defaultFishVariantParrMaxG, defaultFishVariantSmoltMaxG, defaultFishVariantJuvenileMaxM); v != "" {
				seen[v] = true
			}
		}
	}
	for _, want := range []string{"a_salmon_f1", "a_salmon_f2", "a_salmon_f3",
		"a_salmon_m1", "a_salmon_m2", "a_salmon_m3"} {
		if !seen[want] {
			t.Fatalf("族 %s 从未被选中：%v", want, seen)
		}
	}
}

func TestFishModelVariantTunableOff(t *testing.T) {
	db := seedSalmonLines(t)
	srv := newVariantTestServer(t, map[string]float64{"fish_model_variant": 0})
	srv.SetDB(db)
	if got := srv.fishModelVariantForFish("a_salmon", 20000, 1.30, 2, "abc"); got != "" {
		t.Fatalf("开关关闭时应留空，得到 %q", got)
	}
}

// 溜鱼（27.f22）、渔护/结算（25.f11）必须下发同一个族名；
// 命名鱼的名字要让位到 25.f12，不能被族名挤掉。
func TestFishModelKeySharedByEntityAndKeepnet(t *testing.T) {
	db := seedSalmonLines(t)
	srv := newVariantTestServer(t, map[string]float64{})
	srv.SetDB(db)

	f := &RolledFish{FishGUID: "0e1d2c3b4a5968778695a4b3c2d1e0f1", Species: "a_salmon",
		Grade: 2, WeightG: 20000, LengthM: 1.05}
	want := srv.fishModelVariantFor(f)
	if want == "" {
		t.Fatal("大西洋鲑应当有族名")
	}
	tree27 := srv.buildFish27(f, "", "", 24)
	if tree27.Fields[22].Str == nil || *tree27.Fields[22].Str != want {
		t.Fatalf("27.f22 = %v，want %q", tree27.Fields[22].Str, want)
	}

	entry := &store.KeepnetEntry{EchoGUID: f.FishGUID, Species: f.Species,
		WeightG: f.WeightG, LengthM: f.LengthM, Cat: f.Grade}
	srv.fillKeepnetVariant(entry)
	if entry.ModelVariant != want {
		t.Fatalf("渔护条目族名=%q，溜鱼时=%q", entry.ModelVariant, want)
	}
	tree25 := buildKeepnet25(entry, "", 0, entry.ModelVariant)
	if tree25.Fields[11].Str == nil || *tree25.Fields[11].Str != want {
		t.Fatalf("25.f11 = %v，want %q", tree25.Fields[11].Str, want)
	}
	if tree25.Fields[12].Str != nil {
		t.Fatalf("未命名鱼不应写 25.f12，得到 %q", *tree25.Fields[12].Str)
	}

	named := *entry
	named.NameZh = "鲍里斯"
	tree25 = buildKeepnet25(&named, "", 0, named.ModelVariant)
	if tree25.Fields[11].Str == nil || *tree25.Fields[11].Str != want {
		t.Fatalf("命名鱼 25.f11 应仍是族名，得到 %v", tree25.Fields[11].Str)
	}
	if tree25.Fields[12].Str == nil || *tree25.Fields[12].Str != "鲍里斯" {
		t.Fatalf("命名鱼的名字应写进 25.f12，得到 %v", tree25.Fields[12].Str)
	}

	// 没有变体族的鱼种：f11 仍然写名字，行为不变。
	plain := &store.KeepnetEntry{EchoGUID: "aa", Species: "perch", WeightG: 300, Cat: 1, NameZh: "小鲈"}
	srv.fillKeepnetVariant(plain)
	tree25 = buildKeepnet25(plain, "", 0, plain.ModelVariant)
	if plain.ModelVariant != "" {
		t.Fatalf("鲈鱼不应有族名，得到 %q", plain.ModelVariant)
	}
	if tree25.Fields[11].Str == nil || *tree25.Fields[11].Str != "小鲈" || tree25.Fields[12].Str != nil {
		t.Fatal("无变体族鱼的 25.f11 应保持写名字")
	}
}

// 关掉开关后 27.f22 回到命名鱼名字/null，不会残留族名。
func TestFishModelVariantOffKeepsOldWire(t *testing.T) {
	db := seedSalmonLines(t)
	srv := newVariantTestServer(t, map[string]float64{"fish_model_variant": 0})
	srv.SetDB(db)
	f := &RolledFish{FishGUID: "11223344556677889900112233445566", Species: "a_salmon",
		Grade: 2, WeightG: 20000, LengthM: 1.05, Name: "老鲑"}
	tree27 := srv.buildFish27(f, "", "", 24)
	if tree27.Fields[22].Str == nil || *tree27.Fields[22].Str != "老鲑" {
		t.Fatalf("关闭开关后 27.f22 应为命名鱼名字，得到 %v", tree27.Fields[22].Str)
	}
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fish_model_variant": 1}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	f.ModelVariant = ""
	if got := srv.fishModelVariantFor(f); got == "" {
		t.Fatal("重新打开开关后应当有族名")
	}
}

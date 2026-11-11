package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// TestFishStats321FromCatchLog 验证 3/2 统计由玩家 catch_log 实时生成。
func TestFishStats321FromCatchLog(t *testing.T) {
	t.Parallel()
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	insert := func(loc string, spot int64, key string, weight, grade, xp, silver int64, bait string, length float64) {
		t.Helper()
		if err := db.CatchLogInsert(ctx, uid, loc, spot, key, "", weight, length, "catch",
			bait, grade, xp, silver, "level_001_lake"); err != nil {
			t.Fatal(err)
		}
	}
	insert("loc1_02", 1, "c.roach", 186, 1, 35, 0, "worm_01", 0.2)
	insert("loc1_02", 2, "bream", 900, 3, 260, 42, "worm_01", 0.9)
	insert("loc1_02", 3, "c.roach", 500, 2, 100, 10, "plug_m_1833", 0.3)
	insert("loc1_02", 4, "perch", 700, 4, 300, 100, "worm_01", 0.5)

	sess := newProbeSess(srv)
	body, ok := hFishStats(&Ctx{Sess: sess, Main: 3, Sub: 2, DecodeOK: true, Log: logging.Discard()})
	if !ok || len(body) == 0 {
		t.Fatal("3/2 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatalf("3/2 响应不可解码: %v", err)
	}
	if dec.Class != "jahbacemfof" {
		t.Fatalf("3/2 应为 jahbacemfof，实得 %s", dec.Class)
	}
	f0 := dec.Field(0)
	if got := f0.U32(); got != 4 {
		t.Fatalf("f0 总钓获=%d，期望4", got)
	}
	f1 := dec.Field(1)
	if got := f1.U32(); got != 2 {
		t.Fatalf("f1 稀有=%d，期望2（grade>=3）", got)
	}
	f2 := dec.Field(2)
	if got := f2.U32(); got != 1 {
		t.Fatalf("f2 蓝冠=%d，期望1（grade>=4）", got)
	}
	f4 := dec.Field(4)
	if got := f4.U32(); got != 4 {
		t.Fatalf("f4 单日数量纪录=%d，期望4", got)
	}
	// f5 客户端 DTO cpenllldpnk 是 float（kg），必须按 F32 位型解码。
	f5 := dec.Field(5)
	if got := float64(proto.F32FromBits(f5.U32())); got < 2.2859 || got > 2.2861 {
		t.Fatalf("f5 单日重量纪录=%f kg，期望2.286", got)
	}
	// f6 鱼列表：每鱼种最重一张 320 卡，按重量降序（bream 900 / perch 700 / c.roach 500）。
	fishList := dec.Field(6).Val
	if got := fishList.ArgCount(); got != 3 {
		t.Fatalf("f6 鱼列表条数=%d，期望3（每鱼种一张卡）", got)
	}
	if got := fishList.Items[0].Field(0).Str; got == nil || *got != "bream" {
		t.Fatalf("f6 首卡鱼种=%v，期望 bream（900g 最重）", got)
	}
	if got := fishList.Items[0].Field(2).Num; got == nil || *got != 900 {
		t.Fatalf("f6 首卡重量=%v，期望900", got)
	}
	if got := fishList.Items[2].Field(0).Str; got == nil || *got != "c.roach" {
		t.Fatalf("f6 末卡鱼种=%v，期望 c.roach（500g 最轻）", got)
	}
	counts := []struct {
		name                       string
		total, trophy, superTrophy int64
	}{
		{"bream", 1, 1, 0},
		{"perch", 1, 1, 1},
		{"c.roach", 2, 0, 0},
	}
	for i, want := range counts {
		card := fishList.Items[i]
		nameField := card.Field(0)
		if got := nameField.StrOr(); got != want.name {
			t.Fatalf("f6[%d] 鱼种=%q，期望 %q", i, got, want.name)
		}
		totalField := card.Field(9)
		if got := int64(totalField.U32()); got != want.total {
			t.Fatalf("f6[%d] %s 总钓获=%d，期望 %d", i, want.name, got, want.total)
		}
		trophyField := card.Field(10)
		if got := int64(trophyField.U32()); got != want.trophy {
			t.Fatalf("f6[%d] %s 奖杯数=%d，期望 %d", i, want.name, got, want.trophy)
		}
		superTrophyField := card.Field(11)
		if got := int64(superTrophyField.U32()); got != want.superTrophy {
			t.Fatalf("f6[%d] %s 超稀有数=%d，期望 %d", i, want.name, got, want.superTrophy)
		}
	}
	card := dec.Field(7).Val
	if card == nil || card.Class != "ippcfiphgfl" {
		t.Fatalf("f7 最大渔获卡缺失: %#v", card)
	}
	cardFish := card.Field(0)
	if got := cardFish.StrOr(); got != "bream" {
		t.Fatalf("记录卡鱼种=%q，期望 bream", got)
	}
	cardWeight := card.Field(2)
	if got := cardWeight.U32(); got != 900 {
		t.Fatalf("记录卡重量=%d，期望900g", got)
	}
	f14 := dec.Field(14).Val.Field(2)
	if got := f14.U32(); got != 900 {
		t.Fatalf("f14 非拟饵最大=%d，期望900g", got)
	}
	f15 := dec.Field(15).Val.Field(2)
	if got := f15.U32(); got != 500 {
		t.Fatalf("f15 路亚最大=%d，期望500g", got)
	}
	// f8 最优竞争力：按搏鱼拉力维度（鱼种曲线×重量），不再与最重同卡。
	pullOf := func(key string, weight int64) float64 {
		f := &RolledFish{Species: key, WeightG: weight}
		return srv.fishFightPullFor(f)
	}
	type catchRow struct {
		key    string
		weight int64
	}
	all := []catchRow{{"c.roach", 186}, {"bream", 900}, {"c.roach", 500}, {"perch", 700}}
	wantStrongest := all[0]
	for _, r := range all[1:] {
		if pullOf(r.key, r.weight) > pullOf(wantStrongest.key, wantStrongest.weight) {
			wantStrongest = r
		}
	}
	f8 := dec.Field(8).Val
	f8Fish, f8Weight := f8.Field(0), f8.Field(2)
	if got := f8Fish.StrOr(); got != wantStrongest.key || f8Weight.U32() != uint32(wantStrongest.weight) {
		t.Fatalf("f8 最优竞争力=%v %d，期望按拉力维度 %v %d",
			f8Fish.StrOr(), f8Weight.U32(), wantStrongest.key, wantStrongest.weight)
	}
	// f9=冒险鱼；f16=底钓最大鱼，现在按钓法快照取底钓族，本夹具无底钓渔获 → 空卡。
	f9, f16 := dec.Field(9).Val, dec.Field(16).Val
	if f9 == nil || f9.Kind != proto.KindNull {
		t.Fatalf("f9 应保持 null，实得 %#v", f9)
	}
	if f16 != nil && f16.Kind != proto.KindNull {
		t.Fatalf("无底钓渔获时 f16 应为 null，实得 %#v", f16)
	}
	if got := dec.Field(17).Val.ArgCount(); got != 2 {
		t.Fatalf("f17 竿类统计项=%d，期望2（浮钓/纺车）", got)
	}
	// f19 饵料统计（天然饵，318 gconafdfchk）与 f21 拟饵统计（319 keoejejkenf）
	// 是两个不同字段，不能像过去那样把全部钓饵都塞进 f21。
	// 夹具 4 条：worm_01×3、plug_m_1833×1 → 天然饵 2 个（worm_01 计 3）、拟饵 1 个。
	if got := dec.Field(19).Val.ArgCount(); got != 1 {
		t.Fatalf("f19 饵料统计项=%d，期望1（只有 worm_01）", got)
	}
	if got := dec.Field(19).Val.Items[0].Field(0); got.StrOr() != "worm_01" {
		t.Fatalf("f19 饵料 systemID=%q，期望 worm_01", got.StrOr())
	}
	if got := dec.Field(21).Val.ArgCount(); got != 1 {
		t.Fatalf("f21 拟饵统计项=%d，期望1（只有 plug_m_1833）", got)
	}
	if got := dec.Field(21).Val.Items[0].Field(1); got.StrOr() != "plug_m_1833" {
		t.Fatalf("f21 拟饵 systemID=%q，期望 plug_m_1833", got.StrOr())
	}
	// f11/f13 语义未定，服务端恒发 0。
	f11, f13 := dec.Field(11), dec.Field(13)
	if got := f11.U64(); got != 0 {
		t.Fatalf("f11=%d，期望恒 0（语义未定）", got)
	}
	if got := f13.U64(); got != 0 {
		t.Fatalf("f13=%d，期望恒 0（语义未定）", got)
	}
}

// TestFishStats321TackleFamily 验证浮钓/路亚/底钓三张纪录卡严格按钓法族取鱼：
// 海钓渔获（挪威海绿青鳕走海钓饵）不得出现在浮钓卡里。
func TestFishStats321TackleFamily(t *testing.T) {
	t.Parallel()
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	insert := func(key string, weight int64, bait string, cat int64) {
		t.Helper()
		if err := db.CatchLogInsertTackle(ctx, uid, "loc", 1, key, "", weight, 0.5, "keep",
			bait, 1, 10, 1, "level_001_lake", cat); err != nil {
			t.Fatal(err)
		}
	}
	// 浮钓 1.044kg；路亚 32.807kg；底钓 6.5kg；海钓 20.781kg（绿青鳕，海钓饵）。
	insert("wa_bream", 1044, "maggot_caster", tackleCatFloat)
	insert("a_salmon", 32807, "crank_s_1910", tackleCatSpinning)
	insert("carp", 6500, "boilie_01", tackleCatBottom)
	insert("coalfish", 20781, "fb_bf_saida", tackleCatMarine)

	sess := newProbeSess(srv)
	body, ok := hFishStats(&Ctx{Sess: sess, Main: 3, Sub: 2, DecodeOK: true, Log: logging.Discard()})
	if !ok || len(body) == 0 {
		t.Fatal("3/2 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatalf("3/2 响应不可解码: %v", err)
	}
	cardFishAndWeight := func(field int) (string, int64) {
		t.Helper()
		card := dec.Field(field).Val
		if card == nil || card.Kind == proto.KindNull {
			return "", 0
		}
		nameField, weightField := card.Field(0), card.Field(2)
		return nameField.StrOr(), int64(weightField.U32())
	}
	if fish, w := cardFishAndWeight(14); fish != "wa_bream" || w != 1044 {
		t.Fatalf("f14 浮钓纪录=%s/%dg，期望 wa_bream/1044g（海钓绿青鳕不得进入）", fish, w)
	}
	if fish, w := cardFishAndWeight(15); fish != "a_salmon" || w != 32807 {
		t.Fatalf("f15 路亚纪录=%s/%dg，期望 a_salmon/32807g", fish, w)
	}
	if fish, w := cardFishAndWeight(16); fish != "carp" || w != 6500 {
		t.Fatalf("f16 底钓纪录=%s/%dg，期望 carp/6500g", fish, w)
	}
	// 全场最重仍是海钓那条（f7 不按钓法过滤）。
	if fish, w := cardFishAndWeight(7); fish != "a_salmon" || w != 32807 {
		t.Fatalf("f7 最大渔获=%s/%dg，期望 a_salmon/32807g", fish, w)
	}
}

// TestFishStats321LegacyFamilyFallback 验证历史行（tackle_cat=0）仍能出卡：
// 回退「拟饵→路亚、其余→浮钓」旧口径，不能让老档玩家看到空卡。
func TestFishStats321LegacyFamilyFallback(t *testing.T) {
	t.Parallel()
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	insert := func(key string, weight int64, bait string) {
		t.Helper()
		if err := db.CatchLogInsert(ctx, uid, "loc", 1, key, "", weight, 0.5, "keep",
			bait, 1, 10, 1, "level_001_lake"); err != nil {
			t.Fatal(err)
		}
	}
	insert("bream", 900, "worm_01")
	insert("c.roach", 500, "plug_m_1833")

	sess := newProbeSess(srv)
	body, ok := hFishStats(&Ctx{Sess: sess, Main: 3, Sub: 2, DecodeOK: true, Log: logging.Discard()})
	if !ok || len(body) == 0 {
		t.Fatal("3/2 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatalf("3/2 响应不可解码: %v", err)
	}
	f14Name, f15Name := dec.Field(14).Val.Field(0), dec.Field(15).Val.Field(0)
	if got := f14Name.StrOr(); got != "bream" {
		t.Fatalf("旧档 f14 浮钓纪录=%q，期望 bream（非拟饵回退浮钓）", got)
	}
	if got := f15Name.StrOr(); got != "c.roach" {
		t.Fatalf("旧档 f15 路亚纪录=%q，期望 c.roach（拟饵回退路亚）", got)
	}
}

func TestRecordCardDefaultsAndCategory81(t *testing.T) {
	t.Parallel()
	// category 81 只有 rengine_1/rengine_2（渔轮电机）。渔轮 DTO 里 f14
	// dfjpnambjcj 声明的类型是 odlknjlbooh，对应 wire 类 kbpmmclfipg/1470；
	// 原来的 dbjjakbbbfc/12601 会让客户端解出未知类型，电机槽无法使用。
	if got := categoryWireClass[81]; got.Class != "kbpmmclfipg" || got.TypeID != 1470 {
		t.Fatalf("category 81 映射=%+v，期望 kbpmmclfipg/1470", got)
	}
	card := (&Server{}).recordCard320(catchStatRow{
		TS: 1.25, FishKey: "roach", Grade: 0, WeightG: 100,
		LengthM: 0.2, Bait: "worm", LocLevel: "level",
	}, true, func(string) bool { return false })
	if card == nil {
		t.Fatal("recordCard320 不应返回 nil")
	}
	gradeField := card.Field(1)
	if got := gradeField.U32(); got != 1 {
		t.Fatalf("grade=0 时 f1=%d，期望默认值 1", got)
	}
	categoryField := card.Field(12)
	if got := categoryField.U32(); got != 1 {
		t.Fatalf("grade=0 时 f12=%d，期望默认值 1", got)
	}
}

// TestTackleCatForRig 验证钓组类型到钓法快照的映射边界。
//
// 快照会落库且不可回溯，因此未知钓组必须写 0（未知），不能沿用技能页
// 「未知按浮钓计」的兼容行为——否则历史异常或未来新增的钓组会永久污染
// 浮钓纪录卡。
func TestTackleCatForRig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rig  int64
		want int64
	}{
		{"浮钓族", 1, tackleCatFloat},
		{"底钓族", 12, tackleCatBottom},
		{"路亚族", 5, tackleCatSpinning},
		{"海钓族", 50, tackleCatMarine},
		{"零值", 0, tackleCatUnknown},
		{"负值", -1, tackleCatUnknown},
		{"未知正数", 999, tackleCatUnknown},
		{"未登记正数", 300, tackleCatUnknown},
	}
	for _, c := range cases {
		if got := tackleCatForRig(c.rig); got != c.want {
			t.Fatalf("%s rigType=%d → %d，期望 %d", c.name, c.rig, got, c.want)
		}
	}
	// 与技能大类的差异必须有意识：技能页把未知正数归浮钓，统计快照归未知。
	if got := fishingSkillCatForRig(999); got != tackleCatFloat {
		t.Skipf("技能大类对未知钓组的默认值已变（%d），需复查 tackleCatForRig 的注释", got)
	}
}

// TestFishStats321RodRigAggregate 锁定 f17/f18 的族级聚合分布。
//
// 当前 f17/f18 输出的是每个钓法族的**代表**竿型/钓组，而不是真实竿型：
// catch_log 只记录 tackle_cat，没有记录当次 rod_type/rig_type。因此本测试
// 锁定的是现状（同族合并），不是"按真实竿类统计"。若将来补了真实竿型快照，
// 应同时更新本用例的期望值。
func TestFishStats321RodRigAggregate(t *testing.T) {
	t.Parallel()
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	insert := func(key string, cat int64) {
		t.Helper()
		if err := db.CatchLogInsertTackle(ctx, uid, "loc", 1, key, "", 1000, 0.5, "keep",
			"worm", 1, 10, 1, "level_001_lake", cat); err != nil {
			t.Fatal(err)
		}
	}
	// 浮钓 2 条、路亚 3 条、底钓 1 条、海钓 4 条。
	insert("wa_bream", tackleCatFloat)
	insert("gudrus", tackleCatFloat)
	insert("a_salmon", tackleCatSpinning)
	insert("c.roach", tackleCatSpinning)
	insert("n.pike", tackleCatSpinning)
	insert("carp", tackleCatBottom)
	for _, k := range []string{"coalfish", "meagre", "cod", "herring"} {
		insert(k, tackleCatMarine)
	}

	sess := newProbeSess(srv)
	body, ok := hFishStats(&Ctx{Sess: sess, Main: 3, Sub: 2, DecodeOK: true, Log: logging.Discard()})
	if !ok || len(body) == 0 {
		t.Fatal("3/2 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatalf("3/2 响应不可解码: %v", err)
	}
	type agg struct{ cat, count int64 }
	collect := func(field int) map[int64]int64 {
		t.Helper()
		out := map[int64]int64{}
		for _, it := range dec.Field(field).Val.Items {
			a, b := it.Field(0), it.Field(1)
			out[int64(a.U32())] = int64(b.U32())
		}
		return out
	}
	// 竿型代表值：浮钓→3(手竿)、路亚→1(纺车)、底钓→10(Feeder)、海钓→15。
	wantRod := map[int64]int64{3: 2, 1: 3, 10: 1, 15: 4}
	gotRod := collect(17)
	if len(gotRod) != len(wantRod) {
		t.Fatalf("f17 竿类项数=%d，期望 %d（%v）", len(gotRod), len(wantRod), gotRod)
	}
	for cat, want := range wantRod {
		if gotRod[cat] != want {
			t.Fatalf("f17 竿型 %d 计数=%d，期望 %d（族级代表值）", cat, gotRod[cat], want)
		}
	}
	// 钓组代表值：浮钓→1、路亚→5、底钓→12、海钓→50。
	wantRig := map[int64]int64{1: 2, 5: 3, 12: 1, 50: 4}
	gotRig := collect(18)
	for cat, want := range wantRig {
		if gotRig[cat] != want {
			t.Fatalf("f18 钓组 %d 计数=%d，期望 %d（族级代表值）", cat, gotRig[cat], want)
		}
	}
}

package gameserver

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// lineBreakBodyWithLineOut 按 17/9 的真实请求布局造 body：
// 放线米数是以 little-endian float32 存在偏移 0x3f 的。
func lineBreakBodyWithLineOut(meters float32) []byte {
	body := make([]byte, 0x43)
	binary.LittleEndian.PutUint32(body[0x3f:], math.Float32bits(meters))
	return body
}

// mountMonoFifty 造 tele 竿 + mono 主线并挂到位，把主线长度固定成 50 米。
func mountMonoFifty(t *testing.T, srv *Server) (*Session, string, string) {
	t.Helper()
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "tele_10180_4", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "mono_15095", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, rod.GUID, line.GUID)

	lineRow, _ := srv.DB.GetItemByGUID(ctx, line.GUID)
	fields, _ := FieldsFromJSON(lineRow.FieldsJSON)
	if !setItemLengthField(lineRow, fields, 50) {
		t.Fatal("设置主线长度失败")
	}
	fj, _ := FieldsToJSON(fields)
	if err := srv.DB.UpdateItemFields(ctx, line.GUID, fj, lineRow.Qty, lineRow.Durability,
		lineRow.Workmanship, lineRow.RigGUID); err != nil {
		t.Fatal(err)
	}
	return sess, rod.GUID, line.GUID
}

// rodBreakLineOutM 的解析边界：过短、0、负数、超上限都视为未上报。
func TestRodBreakLineOutM(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body []byte
		want float64
	}{
		{"nil body", nil, 0},
		{"body shorter than offset", make([]byte, 0x3f), 0},
		{"body one byte short", make([]byte, 0x42), 0},
		{"zero meters", lineBreakBodyWithLineOut(0), 0},
		{"negative meters", lineBreakBodyWithLineOut(-3), 0},
		{"over cap", lineBreakBodyWithLineOut(6000), 0},
		{"NaN", lineBreakBodyWithLineOut(float32(math.NaN())), 0},
		{"minimal valid", lineBreakBodyWithLineOut(0.5), 0.5},
		{"normal", lineBreakBodyWithLineOut(12.5), 12.5},
		{"at cap", lineBreakBodyWithLineOut(5000), 5000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rodBreakLineOutM(c.body); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

// 客户端上报 5 米：只扣 5 米，主线物品保留。
func TestMainLineBreakLosesReportedLineOutMeters(t *testing.T) {
	t.Parallel()
	srv, _ := newRigTestServer(t)
	sess, rodGUID, lineGUID := mountMonoFifty(t, srv)

	tree := rigRequestTree(t, proto.Guid(rodGUID), proto.Guid(lineGUID), proto.U32(1), proto.Null())
	body, ok := hDynamicRodBreak(&Ctx{
		Sess: sess, Main: 17, Sub: 9, Tree: tree, Body: lineBreakBodyWithLineOut(5),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard(),
	})
	if !ok || len(body) == 0 {
		t.Fatal("17/9 断主线未处理")
	}
	if got := lineLengthOfTestItem(t, srv, lineGUID); got != 45 {
		t.Fatalf("应只扣上报的 5 米（剩 45），实得剩余 %d 米", got)
	}
}

// 上报米数超过剩余线长：封顶扣光，主线按耗尽删除。
func TestMainLineBreakReportedMetersClampToLineLength(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	sess, rodGUID, lineGUID := mountMonoFifty(t, srv)

	tree := rigRequestTree(t, proto.Guid(rodGUID), proto.Guid(lineGUID), proto.U32(1), proto.Null())
	body, ok := hDynamicRodBreak(&Ctx{
		Sess: sess, Main: 17, Sub: 9, Tree: tree, Body: lineBreakBodyWithLineOut(200),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard(),
	})
	if !ok || len(body) == 0 {
		t.Fatal("17/9 断主线未处理")
	}
	if row, _ := db.GetItemByGUID(context.Background(), lineGUID); row != nil {
		t.Fatalf("上报 200 米 > 线长 50 米，应封顶扣光并删除主线，实得 %+v", row)
	}
}

// 无客户端上报时退回 tunable：line_break_loss_m=20 → 剩 30 米。
func TestMainLineBreakFallsBackToTunableMeters(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	if err := db.TunableSet(t.Context(), "line_break_loss_m", "20", "test"); err != nil {
		t.Fatal(err)
	}
	// tunableF 有 30s 缓存，写入后必须让缓存失效，否则读到的还是旧值。
	srv.tunMu.Lock()
	srv.tunCache = nil
	srv.tunMu.Unlock()
	if got := srv.tunableF("line_break_loss_m", -1); got != 20 {
		t.Fatalf("tunable 未生效：读回 %v（期望 20）", got)
	}
	sess, rodGUID, lineGUID := mountMonoFifty(t, srv)

	tree := rigRequestTree(t, proto.Guid(rodGUID), proto.Guid(lineGUID), proto.U32(1), proto.Null())
	body, ok := hDynamicRodBreak(&Ctx{
		Sess: sess, Main: 17, Sub: 9, Tree: tree,
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard(),
	})
	if !ok || len(body) == 0 {
		t.Fatal("17/9 断主线未处理")
	}
	if got := lineLengthOfTestItem(t, srv, lineGUID); got != 30 {
		t.Fatalf("tunable 20 米应扣掉 20（剩 30），实得剩余 %d 米", got)
	}
}

// 上报不足 1 米：四舍五入会变成 0，必须钳到至少扣 1 米，
// 否则一个 0.01 的上报就能把"断线"变成"零损耗"。
func TestMainLineBreakSubMeterReportCostsOneMeter(t *testing.T) {
	t.Parallel()
	srv, _ := newRigTestServer(t)
	sess, rodGUID, lineGUID := mountMonoFifty(t, srv)

	tree := rigRequestTree(t, proto.Guid(rodGUID), proto.Guid(lineGUID), proto.U32(1), proto.Null())
	body, ok := hDynamicRodBreak(&Ctx{
		Sess: sess, Main: 17, Sub: 9, Tree: tree, Body: lineBreakBodyWithLineOut(0.3),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard(),
	})
	if !ok || len(body) == 0 {
		t.Fatal("17/9 断主线未处理")
	}
	if got := lineLengthOfTestItem(t, srv, lineGUID); got != 49 {
		t.Fatalf("上报 0.3 米应钳到扣 1 米（剩 49），实得剩余 %d 米", got)
	}
}

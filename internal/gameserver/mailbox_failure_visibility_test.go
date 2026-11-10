package gameserver

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// 邮件附件领取失败必须对玩家可见：19/3、19/4 不能再回 null 成功。
//
// 缺陷现场（specs/changes/mail-claim-visible-failure）：管理员发了一封
// AV_shamp1_pack_6 × 99999 的礼盒邮件，玩家点领取后邮件仍未读、背包为空、
// 客户端无任何提示，日志里只有一条 WARN。根因是 acceptMails 失败时回 0，
// 而 handler 无条件回 null。
//
// 2026-11-10 实机验证又推翻了一版实现：kOperationResult_* 枚举里没有
// "邮件附件发不出来"这一语义，码 65 在客户端显示成"没有足够的技能点来
// 升级能力"，把玩家引向错误方向。因此失败改为普通异常 + 中文 message
// （与 campfire/econ/fish_query 等既有路径一致，客户端直接显示 message）。

// mailAcceptFailureEnvelope 断言 19/3、19/4 失败回包的精确字节形状。
func mailAcceptFailureEnvelope(callID uint32, message string) []byte {
	return proto.BuildFailureEnvelope(callID, "System.InvalidOperationException", message)
}

// overCapClaimMessage 是超限礼盒附件在测试库里的完整失败文案：
// qty = maxBundlePreparedItems+1，item = giftBoxID。
func overCapClaimMessage() string {
	return fmt.Sprintf("邮件附件领取失败：礼盒附件数量 %d 超过上限 %d: %s。邮件已保留，请联系服主处理。",
		maxBundlePreparedItems+1, maxBundlePreparedItems, giftBoxID)
}

// TestMailAcceptRejectsOverCapAttachmentWithError 复现缺陷现场：
// qty 超过 maxBundlePreparedItems 的礼盒附件，点击领取必须回玩家可读的错误，
// 且邮件保持未读、附件原样保留、背包不新增物品。
func TestMailAcceptRejectsOverCapAttachmentWithError(t *testing.T) {
	t.Parallel()
	srv, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(4301)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// 与线上坏邮件同形：礼盒类目 + 无实例快照的基础附件。
	id, err := db.MailInsert(ctx, uid, 0, 0, "活动奖励", []store.MailItem{{
		ItemID: giftBoxID, Qty: maxBundlePreparedItems + 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	c := giftBoxTestCtx(t, srv, uid, proto.Args524(proto.Guid(mailIDToGUID(id))))
	c.CallID = 77
	c.replyCapture = &ctxReplyCapture{}

	body, ok := hMailAccept(c)
	captured, present, _, failure := c.replyCapture.snapshot()
	if !ok || body != nil || !present || !failure {
		t.Fatalf("领取失败必须回业务错误: ok=%v body=%x present=%v failure=%v",
			ok, body, present, failure)
	}
	if want := mailAcceptFailureEnvelope(c.CallID, overCapClaimMessage()); !bytes.Equal(captured, want) {
		t.Fatalf("失败信封不符:\n got=%q\nwant=%q", captured, want)
	}

	// 邮件未被吞掉，服主修好数据后玩家还能再领。
	mail, err := db.MailGet(ctx, uid, id)
	if err != nil {
		t.Fatal(err)
	}
	if mail.Read != 0 || mail.ItemsJSON == "" {
		t.Fatalf("失败后邮件应保持未读且保留附件: read=%d items=%q", mail.Read, mail.ItemsJSON)
	}
	if rows := giftBoxRows(t, srv, uid, giftBoxID); len(rows) != 0 {
		t.Fatalf("失败不应入库: %d 行", len(rows))
	}
}

// TestMailAcceptAllReportsErrorWhenAnyAttachmentFails 一键领取混合场景：
// 能领的照常入包并置读，领不出来的保持未读，同时回可读错误让玩家知道有事发生。
func TestMailAcceptAllReportsErrorWhenAnyAttachmentFails(t *testing.T) {
	t.Parallel()
	srv, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(4302)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	goodID, err := db.MailInsert(ctx, uid, 0, 0, "正常奖励",
		[]store.MailItem{{ItemID: "AV_shamp_1lg", Qty: 1}})
	if err != nil {
		t.Fatal(err)
	}
	badID, err := db.MailInsert(ctx, uid, 0, 0, "坏奖励", []store.MailItem{{
		ItemID: giftBoxID, Qty: maxBundlePreparedItems + 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	c := giftBoxTestCtx(t, srv, uid, proto.Args524())
	c.CallID = 88
	c.replyCapture = &ctxReplyCapture{}

	body, ok := hMailAcceptAll(c)
	captured, present, _, failure := c.replyCapture.snapshot()
	if !ok || body != nil || !present || !failure {
		t.Fatalf("混有坏附件时 19/4 必须回可读错误: ok=%v body=%x present=%v failure=%v",
			ok, body, present, failure)
	}
	if want := mailAcceptFailureEnvelope(c.CallID, overCapClaimMessage()); !bytes.Equal(captured, want) {
		t.Fatalf("19/4 失败信封不符:\n got=%q\nwant=%q", captured, want)
	}

	good, err := db.MailGet(ctx, uid, goodID)
	if err != nil {
		t.Fatal(err)
	}
	if good.Read != 1 || good.ItemsJSON != "" {
		t.Fatalf("能领的邮件应已置读并清空附件: %+v", good)
	}
	bad, err := db.MailGet(ctx, uid, badID)
	if err != nil {
		t.Fatal(err)
	}
	if bad.Read != 0 || bad.ItemsJSON == "" {
		t.Fatalf("领不出来的邮件应保持未读: read=%d items=%q", bad.Read, bad.ItemsJSON)
	}
}

// TestMailAcceptSucceedsWithoutFalseError 正常领取仍应回 null，
// 防止"为了可见失败"把成功路径也报错。
func TestMailAcceptSucceedsWithoutFalseError(t *testing.T) {
	t.Parallel()
	srv, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(4303)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	id, err := db.MailInsert(ctx, uid, 0, 0, "正常奖励",
		[]store.MailItem{{ItemID: "AV_shamp_1lg", Qty: 1}})
	if err != nil {
		t.Fatal(err)
	}
	c := giftBoxTestCtx(t, srv, uid, proto.Args524(proto.Guid(mailIDToGUID(id))))
	c.replyCapture = &ctxReplyCapture{}

	body, ok := hMailAccept(c)
	if !ok || len(body) != 1 || body[0] != 0x00 {
		t.Fatalf("正常领取应回 null: ok=%v body=%x", ok, body)
	}
	if _, present, _, failure := c.replyCapture.snapshot(); present || failure {
		t.Fatal("正常领取不应回错误信封")
	}
	mail, err := db.MailGet(ctx, uid, id)
	if err != nil {
		t.Fatal(err)
	}
	if mail.Read != 1 || mail.ItemsJSON != "" {
		t.Fatalf("领取成功后邮件状态不符: %+v", mail)
	}
}

// TestMailAcceptAllReportsErrorOnListFailure 审查 P2：
// 读未读列表失败时不能回 null 成功，否则玩家以为"已经全部领完"。
func TestMailAcceptAllReportsErrorOnListFailure(t *testing.T) {
	t.Parallel()
	srv, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(4401)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// 用触发器让 mail 表的读取失败（SQLite 无 RAISE on SELECT，改用破坏表的方式）。
	if _, err := db.SQL.Exec(`DROP TABLE mail`); err != nil {
		t.Fatal(err)
	}
	c := giftBoxTestCtx(t, srv, uid, proto.Args524())
	c.CallID = 99
	c.replyCapture = &ctxReplyCapture{}

	body, ok := hMailAcceptAll(c)
	captured, present, _, failure := c.replyCapture.snapshot()
	if !ok || body != nil || !present || !failure {
		t.Fatalf("读取邮箱失败必须回业务错误: ok=%v body=%x present=%v failure=%v",
			ok, body, present, failure)
	}
	env, parseOK := proto.ParseEnvelope(captured)
	if !parseOK || env.Marker != -3 {
		t.Fatalf("应为 -3 失败信封: %x", captured)
	}
}

package gameserver

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/tunnel"
)

type mailTestSession struct {
	sess      *Session
	envelopes chan *proto.Envelope
	peer      *tunnel.Conn
	closes    chan uint64
}

func newMailTestSession(t *testing.T, srv *Server, sid uint64, uid int64) *mailTestSession {
	t.Helper()
	serverNet, peerNet := net.Pipe()
	t.Cleanup(func() {
		_ = serverNet.Close()
		_ = peerNet.Close()
	})
	type handshakeResult struct {
		conn *tunnel.Conn
		err  error
	}
	ready := make(chan handshakeResult, 1)
	go func() {
		conn, err := tunnel.HandshakeServer(serverNet, srv.cfg.Tunnel.PSK)
		ready <- handshakeResult{conn: conn, err: err}
	}()
	peer, err := tunnel.HandshakeClient(peerNet, srv.cfg.Tunnel.PSK)
	if err != nil {
		t.Fatal(err)
	}
	var serverConn *tunnel.Conn
	select {
	case result := <-ready:
		if result.err != nil {
			t.Fatal(result.err)
		}
		serverConn = result.conn
	case <-time.After(2 * time.Second):
		t.Fatal("测试隧道握手超时")
	}
	h := &mailTestSession{
		sess:      newSession(srv, sid, "", uid, serverConn),
		envelopes: make(chan *proto.Envelope, 16),
		peer:      peer,
		closes:    make(chan uint64, 4),
	}
	srv.sessions[sid] = h.sess
	go func() {
		defer close(h.envelopes)
		for {
			rt, sid, payload, err := peer.ReadRecord()
			if err != nil {
				return
			}
			if rt == tunnel.TypeClose {
				h.closes <- sid
			}
			if rt == tunnel.TypeData {
				if env, ok := proto.ParseEnvelope(payload); ok {
					h.envelopes <- env
				}
			}
		}
	}()
	return h
}

func nextMailTestPush(t *testing.T, h *mailTestSession, main, sub uint8) *proto.Value {
	t.Helper()
	select {
	case env, ok := <-h.envelopes:
		if !ok {
			t.Fatal("测试隧道已关闭")
		}
		if env.Marker != -1 || env.Main != main || env.Sub != sub || env.Expect == nil || *env.Expect != 1 {
			t.Fatalf("推送信封不符: %+v", env)
		}
		tree, n, err := proto.DecodeValue(env.Body, 0, h.sess.Table())
		if err != nil || n != len(env.Body) {
			t.Fatalf("推送 %d/%d 不可完整解码: n=%d len=%d err=%v", main, sub, n, len(env.Body), err)
		}
		if reason := checkPushArgsStrict(main, sub, tree); reason != "" {
			t.Fatalf("推送 %d/%d 合同不符: %s", main, sub, reason)
		}
		return tree
	case <-time.After(2 * time.Second):
		t.Fatalf("未收到邮件相关推送 %d/%d", main, sub)
		return nil
	}
}

func assertNoMailTestPush(t *testing.T, h *mailTestSession) {
	t.Helper()
	select {
	case env := <-h.envelopes:
		t.Fatalf("不应收到推送: %+v", env)
	case <-time.After(100 * time.Millisecond):
	}
}

func assertMailTestNotification(t *testing.T, h *mailTestSession, mail *store.MailRow) {
	t.Helper()
	push := nextMailTestPush(t, h, 19, 10)
	if len(push.Items) != 1 || push.Items[0].TypeID != 11062 {
		t.Fatalf("在线通知必须携带完整邮件对象: %+v", push)
	}
	message := push.Items[0]
	if message.Fields[0].StrOr() != mail.Text || message.Fields[2].Hex != mailIDToGUID(mail.ID) || message.Fields[4].U32() != uint32(mail.Sender) {
		t.Fatalf("在线通知内容与已提交邮件不符: %+v", message)
	}
	if mail.ItemsJSON != "" && len(message.Fields[1].Val.Items) == 0 {
		t.Fatal("在线通知丢失附件展示数据")
	}
}

func TestAdminMailAttachmentListAndClaim(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	const itemID = "AV_shamp_1lg"
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	first := newMailTestSession(t, srv, 1, uid)
	srv.execAdminCmd(&AdminCommand{
		Op: "send_mail", UID: uid, Text: "管理员补偿",
		Items: []store.MailItem{{ItemID: itemID, Qty: 1}},
	})
	mails, err := db.MailList(ctx, uid, true)
	if err != nil || len(mails) != 1 {
		t.Fatalf("投递后应有一封未读邮件: mails=%+v err=%v", mails, err)
	}
	mail := mails[0]
	var count int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, itemID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 || mail.AttachmentSnapshotVersion != 0 {
		t.Fatalf("管理员基础附件应留在邮箱等待领取: count=%d mail=%+v", count, mail)
	}
	assertMailTestNotification(t, first, mail)

	call := func(sub uint8, args *proto.Value) *proto.Value {
		t.Helper()
		request, err := proto.EncodeValue(args, srv.Table())
		if err != nil {
			t.Fatal(err)
		}
		tree, _, err := proto.DecodeValue(request, 0, srv.Table())
		if err != nil {
			t.Fatal(err)
		}
		handler := srv.dispatch.lookup(19, sub)
		if handler == nil {
			t.Fatalf("邮件路由 19/%d 未注册", sub)
		}
		body, ok := handler(&Ctx{Sess: first.sess, Main: 19, Sub: int(sub), Tree: tree, DecodeOK: true, Log: srv.log})
		if !ok {
			t.Fatalf("邮件路由 19/%d 未处理", sub)
		}
		response, n, err := proto.DecodeValue(body, 0, srv.Table())
		if err != nil || n != len(body) {
			t.Fatalf("19/%d 响应不可完整解码: n=%d len=%d err=%v", sub, n, len(body), err)
		}
		return response
	}
	info := call(5, proto.Args524())
	if info.TypeID != 133 || info.Fields[0].U32() != 1 {
		t.Fatalf("19/5 未读数不符: %+v", info)
	}
	list := call(6, proto.Args524())
	if list.Kind != proto.KindGuidArr || len(list.Items) != 1 || list.Items[0].Hex != mailIDToGUID(mail.ID) {
		t.Fatalf("19/6 未返回新邮件 GUID: %+v", list)
	}
	get := call(7, proto.Args524(proto.Guid(list.Items[0].Hex)))
	if get.Kind != proto.KindArgs || len(get.Items) != 1 || get.Items[0].TypeID != 11062 {
		t.Fatalf("19/7 邮件详情形状不符: %+v", get)
	}
	attachments := get.Items[0].Fields[1].Val
	if attachments == nil || len(attachments.Items) != 1 || attachments.Items[0].TypeID != 11063 {
		t.Fatalf("19/7 附件形状不符: %+v", attachments)
	}
	attachment := attachments.Items[0]
	if attachment.Fields[2].StrOr() != itemID || attachment.Fields[6].U32() != 1 {
		t.Fatalf("19/7 附件物品或数量不符: %+v", attachment)
	}
	if response := call(3, proto.Args524(proto.Guid(mailIDToGUID(mail.ID)))); response.Kind != proto.KindNull {
		t.Fatalf("19/3 应返回 null: %+v", response)
	}
	push := nextMailTestPush(t, first, 4, 22)
	if len(push.Items) != 1 || push.Items[0].Kind != proto.KindObj {
		t.Fatalf("领取后背包推送不含物品树: %+v", push)
	}
	rows, err := db.ListItemsByContainer(ctx, uid, "backpack")
	if err != nil || len(rows) != 1 || rows[0].ItemID != itemID || rows[0].Qty != 1 {
		t.Fatalf("领取后附件未完整入包: rows=%+v err=%v", rows, err)
	}
	mail, err = db.MailGet(ctx, uid, mail.ID)
	if err != nil || mail.Read != 1 || mail.ItemsJSON != "" {
		t.Fatalf("领取后邮件状态不符: mail=%+v err=%v", mail, err)
	}
	call(3, proto.Args524(proto.Guid(mailIDToGUID(mail.ID))))
	assertNoMailTestPush(t, first)
}

func TestAdminMailInsertFailureDoesNotNotify(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	const uid = int64(1000001)
	h := newMailTestSession(t, srv, 1, uid)
	if _, err := db.SQL.Exec(`CREATE TRIGGER mail_notification_fail BEFORE INSERT ON mail
		BEGIN SELECT RAISE(ABORT,'mail insert failed'); END`); err != nil {
		t.Fatal(err)
	}
	srv.execAdminCmd(&AdminCommand{Op: "send_mail", UID: uid, Text: "failed"})
	mails, err := db.MailList(context.Background(), uid, false)
	if err != nil || len(mails) != 0 {
		t.Fatalf("投递失败不应留下邮件: mails=%+v err=%v", mails, err)
	}
	assertNoMailTestPush(t, h)
}

func TestPlayerMailNotifiesOnlineRecipients(t *testing.T) {
	for _, attachment := range []bool{false, true} {
		name := "message"
		if attachment {
			name = "attachment"
		}
		t.Run(name, func(t *testing.T) {
			srv, db := newEconomyTxTestServer(t)
			ctx := context.Background()
			const sender, recipient, stranger int64 = 2401, 2402, 2403
			for _, uid := range []int64{sender, recipient, stranger} {
				if _, err := db.EnsureProfile(ctx, uid); err != nil {
					t.Fatal(err)
				}
			}
			first := newMailTestSession(t, srv, 1, recipient)
			second := newMailTestSession(t, srv, 2, recipient)
			other := newMailTestSession(t, srv, 3, stranger)
			var body []byte
			var ok bool
			if attachment {
				seedEconomyItemDef(t, db, 9451, "online_mail_item", 27)
				item := economyTestItem(t, srv, sender, "online_mail_item", "backpack", 2)
				body, ok = hMailSendItem(economyTestCtx(srv, sender, proto.Args524(
					proto.U32(uint32(recipient)), proto.U32(1), proto.Guid(item.GUID))))
			} else {
				body, ok = hMailSendMessage(economyTestCtx(srv, sender, proto.Args524(
					proto.U32(uint32(recipient)), proto.StrVal("在线文字消息"))))
			}
			if !ok || len(body) == 0 || body[0] == 0x0f {
				t.Fatalf("寄件失败: ok=%v body=%x", ok, body)
			}
			mails, err := db.MailList(ctx, recipient, true)
			if err != nil || len(mails) != 1 || mails[0].Sender != sender {
				t.Fatalf("寄件未提交: mails=%+v err=%v", mails, err)
			}
			assertMailTestNotification(t, first, mails[0])
			assertMailTestNotification(t, second, mails[0])
			assertNoMailTestPush(t, other)
		})
	}
}

func TestMailNotificationContractRejectsEmptyOrWrongPayload(t *testing.T) {
	for _, body := range []*proto.Value{
		proto.Args524(),
		proto.Args524(proto.Guid(mailIDToGUID(1))),
		proto.Args524(proto.U32(1)),
	} {
		if reason := checkPushArgsStrict(19, 10, body); reason == "" {
			t.Fatalf("邮箱合同不应接受缺失邮件对象的通知: %+v", body)
		}
	}
}

func TestPlayerMailInsertFailureDoesNotNotify(t *testing.T) {
	for _, attachment := range []bool{false, true} {
		name := "message"
		if attachment {
			name = "attachment"
		}
		t.Run(name, func(t *testing.T) {
			srv, db := newEconomyTxTestServer(t)
			ctx := context.Background()
			const sender, recipient int64 = 2601, 2602
			for _, uid := range []int64{sender, recipient} {
				if _, err := db.EnsureProfile(ctx, uid); err != nil {
					t.Fatal(err)
				}
			}
			session := newMailTestSession(t, srv, 1, recipient)
			if _, err := db.SQL.Exec(`CREATE TRIGGER player_mail_fail BEFORE INSERT ON mail
				BEGIN SELECT RAISE(ABORT,'mail insert failed'); END`); err != nil {
				t.Fatal(err)
			}
			if attachment {
				seedEconomyItemDef(t, db, 9461, "failed_online_mail_item", 27)
				item := economyTestItem(t, srv, sender, "failed_online_mail_item", "backpack", 2)
				hMailSendItem(economyTestCtx(srv, sender, proto.Args524(
					proto.U32(uint32(recipient)), proto.U32(1), proto.Guid(item.GUID))))
				left, err := db.GetItemByGUID(ctx, item.GUID)
				if err != nil || left.Qty != 2 {
					t.Fatalf("寄件失败应回滚扣件: item=%+v err=%v", left, err)
				}
			} else {
				hMailSendMessage(economyTestCtx(srv, sender, proto.Args524(
					proto.U32(uint32(recipient)), proto.StrVal("投递失败测试"))))
			}
			mails, err := db.MailList(ctx, recipient, false)
			if err != nil || len(mails) != 0 {
				t.Fatalf("投递失败不应留下邮件: mails=%+v err=%v", mails, err)
			}
			assertNoMailTestPush(t, session)
		})
	}
}

// TestAdminDeleteMailRemovesOnlyTargetedMail 服主清理坏邮件：
// 只删指定 id，其它邮件与其它玩家的邮件不受影响。
func TestAdminDeleteMailRemovesOnlyTargetedMail(t *testing.T) {
	t.Parallel()
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()
	const uid, other = int64(2701), int64(2702)
	for _, u := range []int64{uid, other} {
		if _, err := db.EnsureProfile(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	badID, err := db.MailInsert(ctx, uid, 0, 0, "坏奖励", []store.MailItem{{
		ItemID: "AV_shamp1_pack_6", Qty: 99999,
	}})
	if err != nil {
		t.Fatal(err)
	}
	keepID, err := db.MailInsert(ctx, uid, 0, 0, "保留", nil)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := db.MailInsert(ctx, other, 0, 0, "别人的", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 后台队列传的是十进制字符串 id（见 handlePlayerMailDelete）。
	srv.execAdminCmd(&AdminCommand{
		Op: "delete_mail", UID: uid, MailIDs: []string{strconv.FormatInt(badID, 10)},
	})

	if m, err := db.MailGet(ctx, uid, badID); err == nil && m != nil {
		t.Fatalf("目标邮件应已删除: %+v", m)
	}
	if m, err := db.MailGet(ctx, uid, keepID); err != nil || m == nil {
		t.Fatalf("同玩家的其它邮件不应被删: %+v err=%v", m, err)
	}
	if m, err := db.MailGet(ctx, other, otherID); err != nil || m == nil {
		t.Fatalf("其它玩家的邮件不应被删: %+v err=%v", m, err)
	}
}

// TestDeleteMailDecimalIDNotParsedAsHex 审查 P1：后台按十进制入队 id，
// 消费端不能用十六进制解析，否则 id=10 会被读成 16、删错别人的邮件。
func TestDeleteMailDecimalIDNotParsedAsHex(t *testing.T) {
	t.Parallel()
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()
	const uid = int64(2801)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// 造出 id 恰好为 10 与 16 的两封邮件（十六进制 "10" == 十进制 16）。
	id10, id16 := int64(0), int64(0)
	for i := 0; i < 40 && (id10 == 0 || id16 == 0); i++ {
		id, err := db.MailInsert(ctx, uid, 0, 0, "filler", nil)
		if err != nil {
			t.Fatal(err)
		}
		switch id {
		case 10:
			id10 = id
		case 16:
			id16 = id
		}
	}
	if id10 == 0 || id16 == 0 {
		t.Skipf("测试库 mail 自增未到达 10/16（id10=%d id16=%d），跳过", id10, id16)
	}

	// 后台传十进制 "10"，必须只删 10，不能误删 16。
	srv.execAdminCmd(&AdminCommand{Op: "delete_mail", UID: uid, MailIDs: []string{"10"}})

	if m, err := db.MailGet(ctx, uid, id10); err == nil && m != nil {
		t.Fatalf("十进制 id=10 的邮件应被删除: %+v", m)
	}
	if m, err := db.MailGet(ctx, uid, id16); err != nil || m == nil {
		t.Fatal("十进制 10 不能删掉 id=16 的邮件（十六进制误解析）")
	}
}

// TestParseMailIDArgDistinguishesGUIDAndDecimal 直接锁住解析函数的分支。
func TestParseMailIDArgDistinguishesGUIDAndDecimal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"10", 10},                               // 十进制
		{"16", 16},                               // 十进制
		{"0000000000000000000000000000000a", 10}, // 32 位十六进制 GUID
		{"00000000000000000000000000000010", 16}, // 同上
		{"", 0},                                  // 空
		{"abc", 0},                               // 非法十进制
	}
	for _, c := range cases {
		if got := parseMailIDArg(c.in); got != c.want {
			t.Errorf("parseMailIDArg(%q)=%d，期望 %d", c.in, got, c.want)
		}
	}
}

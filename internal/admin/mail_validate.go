package admin

import (
	"context"
	"fmt"
	"strings"

	"rf4server/internal/gameserver"
	"rf4server/internal/store"
)

// 邮件发件前置校验。
//
// 背景（specs/changes/mail-claim-visible-failure）：后台曾发出一封
// AV_shamp1_pack_6 × 99999 的礼盒邮件，领取路径失败但当时领取侧静默回 null，
// 结果玩家点了没反应、物品也不进包。
//
// 关键教训：校验不能只看字符串形式。曾经的实现只检查"内含物 ID 非空"，
// 于是把内含物改成不存在的 item_id 仍能发出去，玩家的领取必然失败。
// 因此这里不再自己维护一套规则，而是直接调用领取路径共用的
// gameserver.ValidateMailAttachment —— 两边同源，才不会漂移。
func (s *Server) validateMailAttachments(ctx context.Context, items []store.MailItem) error {
	if len(items) == 0 {
		return nil
	}
	var problems []string
	for _, att := range items {
		if err := gameserver.ValidateMailAttachment(ctx, s.db, att); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(problems, "；"))
}

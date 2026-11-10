package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"rf4server/internal/store"
)

// 邮件发件前置校验。
//
// 背景（specs/changes/mail-claim-visible-failure）：后台曾发出一封
// AV_shamp1_pack_6 × 99999 的礼盒邮件，领取路径因包数超过
// maxBundlePreparedItems 而失败，但那时领取侧还是静默回 null，
// 结果玩家点了没反应、物品也不进包。
//
// 领取侧已经改成"领不到就报错"，这里补另一半：**发不出来的邮件不要发出去**。
// 判据取自 item_def 的真实定义，不写死 SKU 白名单；只拦"注定领不出来"的情况。
const (
	// maxMailBundleUnits 与 gameserver.maxBundlePreparedItems 同值：
	// 单个附件在一次领取中最多能发放的实例数。
	maxMailBundleUnits = 10000
	// mailBundleCategory 是组合包（含礼盒）的品类号，与 gameserver.bundleCategory 同值。
	mailBundleCategory = 55
)

// validateMailAttachments 在入队前对附件做一次干跑校验。
// 返回的 error 汇总了所有不合格附件的原因，直接回给后台操作者。
func (s *Server) validateMailAttachments(ctx context.Context, items []store.MailItem) error {
	if len(items) == 0 {
		return nil
	}
	var problems []string
	for _, att := range items {
		itemID := strings.TrimSpace(att.ItemID)
		if itemID == "" {
			problems = append(problems, "附件缺少物品 ID")
			continue
		}
		if att.Qty <= 0 {
			problems = append(problems, fmt.Sprintf("附件 %s 数量必须大于 0", itemID))
			continue
		}
		def, err := s.db.GetItemDefByItemID(ctx, itemID)
		if errors.Is(err, sql.ErrNoRows) {
			problems = append(problems, fmt.Sprintf("附件 %s 物品不存在", itemID))
			continue
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("附件 %s 查询物品定义失败: %v", itemID, err))
			continue
		}
		if def == nil {
			problems = append(problems, fmt.Sprintf("附件 %s 物品不存在", itemID))
			continue
		}
		if def.Category != mailBundleCategory {
			continue
		}
		// 组合包：礼盒按包数逐件发放，饵料盒按内含物展开，两者都受实例数上限约束。
		if att.Qty > maxMailBundleUnits {
			problems = append(problems, fmt.Sprintf(
				"附件 %s 数量 %d 超过单次领取上限 %d，玩家无法领取",
				itemID, att.Qty, maxMailBundleUnits))
			continue
		}
		if reason := bundleDefinitionProblem(def); reason != "" {
			problems = append(problems, fmt.Sprintf("附件 %s %s", itemID, reason))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(problems, "；"))
}

// bundleDefinitionProblem 检查组合包定义本身能否被领取路径展开。
// 只判断"展开必然失败"的结构问题，具体实例构造留给领取路径。
func bundleDefinitionProblem(def *store.ItemDef) string {
	raw := strings.TrimSpace(def.AttrsJSON)
	if raw == "" || !json.Valid([]byte(raw)) {
		return "组合包定义属性损坏，内含物无法解析"
	}
	var attrs struct {
		Contents []struct {
			ItemID string `json:"cemjmlpafmc"`
		} `json:"nbkhfhenbag"`
	}
	if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
		return "组合包定义属性损坏，内含物无法解析"
	}
	if len(attrs.Contents) == 0 {
		return "组合包没有内含物"
	}
	for _, c := range attrs.Contents {
		if strings.TrimSpace(c.ItemID) == "" {
			return "组合包内含物缺少物品 ID"
		}
	}
	return ""
}

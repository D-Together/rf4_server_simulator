package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/store"
)

// TestMailValidationMatchesBundlePreparation 锁住发件侧校验与领取准备侧的
// 可计件组合包规则一致：只验“两边是否同时拒绝”而不复制实现细节。
func TestMailValidationMatchesBundlePreparation(t *testing.T) {
	tests := []struct {
		name       string
		childAttrs string
		bundleArgs string
		wantReject bool
	}{
		{
			name:       "unit_conversion_overflow",
			childAttrs: `{"ljnnpokapig":2147483648}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":2}]`,
			wantReject: true,
		},
		{
			name:       "absolute_quantity_slot_limit",
			childAttrs: `{"ljnnpokapig":30}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":4294967296,"mlapkoadfnh":true}]`,
			wantReject: true,
		},
		{
			name:       "invalid_child_unit_quantity",
			childAttrs: `{"ljnnpokapig":0}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":1}]`,
			wantReject: true,
		},
		{
			name:       "same_item_cumulative_overflow",
			childAttrs: `{"ljnnpokapig":30}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":3000000000,"mlapkoadfnh":true},{"cemjmlpafmc":"worm_01","ljnnpokapig":3000000000,"mlapkoadfnh":true}]`,
			wantReject: true,
		},
		{
			name:       "absolute_quantity_accepts",
			childAttrs: `{"ljnnpokapig":30}`,
			bundleArgs: `[{"cemjmlpafmc":"worm_01","ljnnpokapig":100,"mlapkoadfnh":true}]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newRigTestServer(t)
			ctx := context.Background()
			if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=? WHERE item_id='worm_01'`, tc.childAttrs); err != nil {
				t.Fatal(err)
			}
			bundleAttrs := `{"ieopbeejfmc":false,"nbkhfhenbag":` + tc.bundleArgs + `}`
			if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=? WHERE item_id='AV_shamp1_pack_6'`, bundleAttrs); err != nil {
				t.Fatal(err)
			}
			att := store.MailItem{ItemID: "AV_shamp1_pack_6", Qty: 1}
			validateErr := ValidateMailAttachment(ctx, db, att)
			_, prepareErr := srv.prepareBundleItems(ctx, 1000001, att.ItemID, "backpack", att.Qty)
			if (validateErr != nil) != tc.wantReject || (prepareErr != nil) != tc.wantReject {
				t.Fatalf("发件校验与领取准备应同为 reject=%v: validate=%v prepare=%v", tc.wantReject, validateErr, prepareErr)
			}
		})
	}
}

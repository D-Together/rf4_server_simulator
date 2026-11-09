package gameserver

// 拟饵钩位。
//
// 拟饵实例的钩子不在竿的 f5 组件表里：它们存放在拟饵自身 f0 的
// gpoaffnjbdo 字段（args(60)，元素即钩类 agedjipcnpe/flgcebbcehb）。
// 因此 15/1、15/2 必须改实例字段；按竿的装配路径写 f5/rig_component
// 只会把钩从背包摘走，拟饵仍渲染出厂钩——表现为"拟饵更换不了钓钩"。
//
// 槽位语义（2026-11-09 八轮实机实验定版，官方语义）：卸钩 = 元素从
// gpoaffnjbdo 数组移除（缩短）。客户端实证：jdpjgahlapp 状态行在
// kcgjlnmdbol()<=0（= 数组 Length==0）时报「钩未安装」；详情面板按
// Length 逐元素建槽行，空数组无行可崩。保长度的三种空槽表示全部
// 实锤崩客户端：缩短外的 0x00 null 元素（数组解析不一致→槽位行 NPE）、
// 0xFF elem_null（登录快照打死客户端）、幻影对象（客户端渲染成真钩，
// 卸载按钮仍激活）。
//
// 多槽（14:31~14:32 实测）：客户端行控件在数组缩短后不重建，仍按旧行
// 号发请求。装钩下标 ≥ 当前元素数 = 追加意图，追加到末尾并以定义出厂
// 钩槽数封顶（lureHookMountOrdinal）；拆钩只接受真实存在的下标/GUID，
// 命中陈旧行（GUID 不在数组）一律拒绝（lureHookRemoveOrdinal）——绝不
// 兜底到活动钩 0，那是「装钩总顶第一颗、卸载总拆第一颗」的事故根源。
//
// 出厂钩（内嵌子对象，GUID 由 uuidLike 合成、无独立物品行）被卸下/替换
// 时按子对象 NumericID 物化独立背包实例退回（2026-11-09 拍板）。短缩
// 语义下防刷是结构性的：卸钩不回填任何钩，原装出厂钩（lureFactoryHook
// Key 命名空间）离开槽位只会发生一次。

import (
	"context"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

const (
	// lureWireClass 是拟饵（品类 17）的 wire 类。
	lureWireClass = "ioombppcnlh"
	// lureHooksAttr 是拟饵 f0 的出厂钩数组字段名。
	lureHooksAttr = "gpoaffnjbdo"
	// lureHookArgsCode 是出厂钩数组容器的 args code。
	lureHookArgsCode = "60"
	// lureHookIndexBase 是钩位规范索引起点（与竿钩位 250+ 序号同族）。
	lureHookIndexBase = 250
)

// lureFactoryHookKey 第 i 槽原装出厂钩的 GUID 派生 key。
// 卸下/替换原装钩时按拍板物化独立背包实例退包（每槽只退这一次）。
func lureFactoryHookKey(i int) string {
	return lureHooksAttr + itoa(i)
}

// lureRestoredHookKey 卸钩后回填槽位的恢复出厂钩 GUID 派生 key。
// 与原装 key 区分：恢复钩再被卸下/替换时不退包，否则「卸钩→装钩→再卸钩」
// 循环可无限刷出厂钩。
func lureRestoredHookKey(i int) string {
	return lureFactoryHookKey(i) + ":r"
}

// lurePhantomHookKey 空槽幻影钩的 GUID 派生 key（2026-11-09 用户拍板
// 「空槽」语义）。空槽的前两种编码是解析层毒药（0x00 截断数组、0xFF
// 未知标记打死客户端），但空物品句柄本身可渲染——背包空格就是这个
// 视图族渲染的。幻影钩 = 结构完整的钩对象 + 唯一确定性 GUID + 任何
// 物品表都不存在：客户端按 GUID 查句柄得空 → 槽位显示「钩未安装」。
func lurePhantomHookKey(i int) string {
	return lureFactoryHookKey(i) + ":e"
}

// isLurePhantomSlotGUID 判断 guid 是否为第 i 槽的幻影空槽。
func isLurePhantomSlotGUID(lureGUID, guid string, i int) bool {
	return guid != "" && guid == uuidLike(lureGUID, lurePhantomHookKey(i))
}

// lureIsOriginalFactoryHook 判断 guid 是否为第 ordinal 槽原装出厂钩
// （唯一可退包的出厂钩形态）。
func lureIsOriginalFactoryHook(lureGUID, hookGUID string, ordinal int) bool {
	return hookGUID != "" && ordinal >= 0 &&
		hookGUID == uuidLike(lureGUID, lureFactoryHookKey(ordinal))
}

// isLureRow 判断物品行是否为拟饵实例。
func isLureRow(row *store.Item) bool {
	return row != nil && row.WireClass == lureWireClass
}

// lureHookFieldIndex 返回拟饵钩数组字段下标。
func lureHookFieldIndex(class string) int {
	return fieldIdxByCS(class, lureHooksAttr)
}

// lureHookItemGUID 取出厂钩数组第 i 项的 GUID。
func lureHookItemGUID(items []*proto.Value, i int) string {
	if i < 0 || i >= len(items) || items[i] == nil {
		return ""
	}
	for k := range items[i].Fields {
		if items[i].Fields[k].Op == "GUID16" && items[i].Fields[k].Hex != "" {
			return items[i].Fields[k].Hex
		}
	}
	return ""
}

// lureHookItemGUIDs 按顺序列出出厂钩 GUID。
func lureHookItemGUIDs(items []*proto.Value) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		if g := lureHookItemGUID(items, i); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// lureActiveOrdinal 返回拟饵当前选中的钩序号。
// f1 保存活动钩下标（lureActiveHookGUID 同口径），越界回退到 0。
func lureActiveOrdinal(fields []proto.Field, n int) int {
	if n <= 0 {
		return 0
	}
	if len(fields) > 1 && fields[1].Num != nil {
		if v := int(u64Of(fields[1].Num)); v >= 0 && v < n {
			return v
		}
	}
	return 0
}

// lureHookMountOrdinal 选出装钩目标槽。尾部 int 是 0 基槽位下标；
// 客户端行控件在数组缩短后不重建（仍发旧行号，2026-11-09 14:32 实测
// len=1 时收到 extra=1），下标落在当前元素数之外视为追加意图，返回
// len(items)，由调用方按定义出厂钩槽数封顶。绝不回落到活动钩——那会
// 把玩家点向新槽的钩顶替到第一颗。
func lureHookMountOrdinal(extra int64, n int) int {
	if n <= 0 {
		return 0
	}
	if extra >= lureHookIndexBase && extra < lureHookIndexBase+int64(n) {
		return int(extra - lureHookIndexBase)
	}
	if extra >= 0 && extra < int64(n) {
		return int(extra)
	}
	return n
}

// lureHookRemoveOrdinal 选出拆钩目标槽，只接受当前真实存在的下标；
// 越界（客户端陈旧行号）返回 -1，由调用方拒绝——兜底拆第一颗是
// 2026-11-09 14:31 实测的「卸载总是第一个槽」事故根源。
func lureHookRemoveOrdinal(extra int64, n int) int {
	if n <= 0 {
		return -1
	}
	if extra >= lureHookIndexBase && extra < lureHookIndexBase+int64(n) {
		return int(extra - lureHookIndexBase)
	}
	if extra >= 0 && extra < int64(n) {
		return int(extra)
	}
	return -1
}

// setLureChildRef 把钩对象写成拟饵子部件：父引用指向拟饵、容器枚举为 rig(4)。
func setLureChildRef(v *proto.Value, parent string) {
	if v == nil || v.Kind != proto.KindObj {
		return
	}
	if i := fieldIdxByCS(v.Class, csParent); i >= 0 && i < len(v.Fields) &&
		(v.Fields[i].Op == "OPTGUID" || v.Fields[i].Op == "GUID16") {
		v.Fields[i].Hex = parent
	}
	if i := fieldIdxByCS(v.Class, csSlotEnum); i >= 0 && i < len(v.Fields) && fieldOpAt(v.Class, i) == "BYTE" {
		v.Fields[i].Num = ptrU64(4)
	}
}

// dropLureHookRows 清掉拟饵钩位在 rig_component 里可能残留的行。
// 拟饵钩的真源是实例 f0；索引行只由旧装配路径写入，留着会变成幽灵组件。
func (s *Server) dropLureHookRows(ctx context.Context, lureGUID string) {
	rows, err := s.DB.ListRigComponents(ctx, lureGUID)
	if err != nil {
		return
	}
	for _, rc := range rows {
		if rc.Slot == catHook {
			_ = s.DB.DeleteRigComponentAt(ctx, lureGUID, rc.Slot, rc.Ordinal)
		}
	}
}

// lureHookMount 把背包里的钩装到拟饵的某个钩位上（15/1）。
// 旧钩返还规则见文件头：实例行退背包（f1）、原装出厂钩物化退包（f0）、
// 恢复版出厂钩不退。
func (s *Server) lureHookMount(ctx context.Context, uid int64, lureGUID, hookGUID string, extra int64) *rigSetResult {
	res := &rigSetResult{UpdatedTrees: []*proto.Value{}}
	lureRow, err := s.DB.GetItemByGUID(ctx, lureGUID)
	if err != nil || !isLureRow(lureRow) || lureRow.UID != uid {
		s.log.Warn("拟饵换钩被拒：拟饵不属于当前玩家", "lure", shortGUID(lureGUID), "uid", uid)
		res.Rejected = true
		return res
	}
	hookRow, err := s.DB.GetItemByGUID(ctx, hookGUID)
	if err != nil || hookRow == nil || hookRow.UID != uid || hookRow.Container != "backpack" || hookRow.RigGUID != "" {
		s.log.Warn("拟饵换钩被拒：鱼钩不属于当前玩家或不在背包", "hook", shortGUID(hookGUID), "uid", uid)
		res.Rejected = true
		return res
	}
	if cat, ok := s.itemCategoryOf(ctx, hookRow); !ok || cat != catHook {
		s.log.Warn("拟饵换钩被拒：不是鱼钩", "item", shortGUID(hookGUID), "cat", cat)
		res.Rejected = true
		return res
	}
	if s.itemBrokenForMount(ctx, hookRow) {
		s.log.Warn("拟饵换钩被拒：钩已损坏", "item", shortGUID(hookGUID))
		res.Rejected = true
		return res
	}

	fields, err := FieldsFromJSON(lureRow.FieldsJSON)
	if err != nil {
		res.Rejected = true
		return res
	}
	idx := lureHookFieldIndex(lureRow.WireClass)
	if idx < 0 || idx >= len(fields) {
		res.Rejected = true
		return res
	}
	list := fields[idx].Val
	if list == nil || list.Kind != proto.KindArgs {
		list = proto.Args(proto.S(lureHookArgsCode))
	}
	items := append([]*proto.Value(nil), list.Items...)
	ordinal := lureHookMountOrdinal(extra, len(items))
	if ordinal >= len(items) {
		// 追加（空饵重装/客户端陈旧行号）：以定义出厂钩槽数为上限。
		def, derr := s.DB.GetItemDef(ctx, lureRow.NumericID)
		if derr != nil || def == nil {
			res.Rejected = true
			return res
		}
		cfgs, _ := defAttrs(def)["gpoaffnjbdo"].([]any)
		if len(items) >= len(cfgs) {
			s.log.Warn("拟饵装钩被拒：钩槽已满", "lure", shortGUID(lureGUID), "slots", len(cfgs))
			res.Rejected = true
			return res
		}
	}

	tree, terr := s.itemTree(ctx, hookRow)
	if terr != nil || tree == nil || tree.Kind != proto.KindObj {
		res.Rejected = true
		return res
	}
	setLureChildRef(tree, lureGUID)

	oldGUID := ""
	var oldTree *proto.Value
	if ordinal < len(items) {
		oldGUID = lureHookItemGUID(items, ordinal)
		oldTree = items[ordinal]
		items[ordinal] = tree
	} else {
		items = append(items, tree)
		ordinal = len(items) - 1
	}
	list.Items = items
	fields[idx] = proto.Field{Op: "VAL", Val: list}
	s.saveItemFields(ctx, lureRow, fields)

	// 钩本体转为已装配：父引用指向拟饵，背包不再列出。新装钩树进 f0
	// 新增段（与 17/2 装配件响应同构），重建客户端的父子归属。
	_ = s.DB.SetItemRigLink(ctx, hookGUID, lureGUID, catHook)
	if fresh, ferr := s.DB.GetItemByGUID(ctx, hookGUID); ferr == nil && fresh != nil {
		s.setWireParent(ctx, fresh, lureGUID)
		if t, terr2 := s.itemTree(ctx, fresh); terr2 == nil {
			res.AddedTrees = append(res.AddedTrees, t)
		}
	}

	// 旧钩返还三分支：带独立物品行的实例退回背包；原装出厂钩（合成
	// GUID 无行）物化退包（新 guid → f0 新增段）；恢复版出厂钩不退
	// （原装已退过一次，再退可循环刷钩）。
	if oldGUID != "" && oldGUID != hookGUID {
		res.ReplacedGUID = oldGUID
		if prev, perr := s.DB.GetItemByGUID(ctx, oldGUID); perr == nil && prev != nil {
			s.returnToBackpack(ctx, prev)
			if fresh, ferr := s.DB.GetItemByGUID(ctx, oldGUID); ferr == nil && fresh != nil {
				if t, terr2 := s.itemTree(ctx, fresh); terr2 == nil {
					res.UpdatedTrees = append(res.UpdatedTrees, t)
				}
			}
		} else if lureIsOriginalFactoryHook(lureGUID, oldGUID, ordinal) {
			if t := s.refundLureFactoryHook(ctx, uid, oldTree); t != nil {
				res.AddedTrees = append(res.AddedTrees, t)
			}
		}
	}

	s.dropLureHookRows(ctx, lureGUID)
	s.setLureHookSelection(ctx, lureGUID, hookGUID)
	if lure, lerr := s.DB.GetItemByGUID(ctx, lureGUID); lerr == nil && lure != nil {
		if t, terr2 := s.itemTree(ctx, lure); terr2 == nil {
			res.UpdatedTrees = append(res.UpdatedTrees, t)
		}
	}
	res.MountedGUID = hookGUID
	s.log.Info("拟饵换钩", "lure", shortGUID(lureGUID), "hook", shortGUID(hookGUID), "ordinal", ordinal)
	return res
}

// lureHookRemove 从拟饵钩位移除一颗钩（15/2）。
// hookGUID 非空时按 GUID 定位，否则按 extra/活动钩定位。
func (s *Server) lureHookRemove(ctx context.Context, uid int64, lureGUID, hookGUID string, extra int64) *rigDetachResult {
	lureRow, err := s.DB.GetItemByGUID(ctx, lureGUID)
	if err != nil || !isLureRow(lureRow) || lureRow.UID != uid {
		s.log.Warn("拟饵拆钩被拒：拟饵不属于当前玩家", "lure", shortGUID(lureGUID), "uid", uid)
		return nil
	}
	fields, err := FieldsFromJSON(lureRow.FieldsJSON)
	if err != nil {
		return nil
	}
	idx := lureHookFieldIndex(lureRow.WireClass)
	if idx < 0 || idx >= len(fields) {
		return nil
	}
	list := fields[idx].Val
	if list == nil || list.Kind != proto.KindArgs || len(list.Items) == 0 {
		return nil
	}
	items := list.Items
	ordinal := -1
	if hookGUID != "" {
		for i := range items {
			if lureHookItemGUID(items, i) == hookGUID {
				ordinal = i
				break
			}
		}
		if ordinal < 0 {
			// 钩不在拟饵上（客户端陈旧行控件最常见）：拒绝，绝不兜底
			// 拆第一颗（2026-11-09 14:31 实测「卸载总是第一个槽」）。
			s.log.Warn("拟饵拆钩被拒：钩不在该拟饵上", "lure", shortGUID(lureGUID), "hook", shortGUID(hookGUID))
			return nil
		}
	}
	if ordinal < 0 {
		ordinal = lureHookRemoveOrdinal(extra, len(items))
	}
	if ordinal < 0 || ordinal >= len(items) {
		return nil
	}

	removedGUID := lureHookItemGUID(items, ordinal)
	removedTree := items[ordinal]
	// 官方语义（文件头）：卸钩 = 元素从数组移除（缩短）。绝不置
	// null/幻影保长度——三种保长空槽表示均实锤崩客户端。
	items = append(items[:ordinal], items[ordinal+1:]...)
	list.Items = items
	fields[idx] = proto.Field{Op: "VAL", Val: list}
	s.saveItemFields(ctx, lureRow, fields)

	res := &rigDetachResult{CompGUID: removedGUID}
	if removedGUID != "" {
		if row, rerr := s.DB.GetItemByGUID(ctx, removedGUID); rerr == nil && row != nil && row.UID == uid {
			s.returnToBackpack(ctx, row)
			if fresh, ferr := s.DB.GetItemByGUID(ctx, removedGUID); ferr == nil && fresh != nil {
				if t, terr := s.itemTree(ctx, fresh); terr == nil {
					res.ReturnedTree = t
				}
			}
		} else if lureIsOriginalFactoryHook(lureGUID, removedGUID, ordinal) {
			// 原装出厂钩只有子对象（GUID 合成、无独立物品行）：按拍板物化
			// 独立背包实例退回（新 guid → f0 新增段）。
			if t := s.refundLureFactoryHook(ctx, uid, removedTree); t != nil {
				res.RefundTree = t
			}
		}
	}
	s.dropLureHookRows(ctx, lureGUID)
	// 选中钩关联指向旧钩时必须让位，否则上鱼判定仍按旧钩（甚至是不存在的 GUID）。
	s.setLureHookSelection(ctx, lureGUID, "")
	// 活动钩下标 f1 是槽位下标：缩短后可能越界，回落 0。
	if len(items) > 0 {
		if len(fields) > 1 && fields[1].Num != nil {
			if k := fieldIdxByCS(lureRow.WireClass, "nkogmkobfng"); k >= 0 && k < len(fields) &&
				u64Of(fields[1].Num) >= uint64(len(items)) {
				fields[k].Num = ptrU64(0)
				s.saveItemFields(ctx, lureRow, fields)
			}
		}
	}
	s.log.Info("拟饵拆钩", "lure", shortGUID(lureGUID), "hook", shortGUID(removedGUID), "ordinal", ordinal)
	return res
}

// refundLureFactoryHook 把出厂钩子对象物化为独立背包实例退回玩家背包。
// 返回退包实例的完整树；定义查询在事务外（静态库隔离纪律），入库走短事务。
// 任何一步失败只记日志返回 nil——拆钩本身已生效，不因退包失败回滚槽位变更。
func (s *Server) refundLureFactoryHook(ctx context.Context, uid int64, hookTree *proto.Value) *proto.Value {
	if hookTree == nil || hookTree.Kind != proto.KindObj {
		return nil
	}
	var numID int64
	if i := fieldIdxByCS(hookTree.Class, csNumericID); i >= 0 && i < len(hookTree.Fields) && hookTree.Fields[i].Num != nil {
		numID = int64(u64Of(hookTree.Fields[i].Num))
	}
	if numID <= 0 {
		s.log.Warn("出厂钩退包失败：子对象缺 NumericID", "class", hookTree.Class)
		return nil
	}
	def, err := s.DB.GetItemDef(ctx, numID)
	if err != nil || def == nil {
		s.log.Warn("出厂钩退包失败：物品定义不存在", "numeric", numID, "err", err)
		return nil
	}
	prepared, err := s.prepareNewItem(ctx, uid, def.ItemID, "backpack", 1, "")
	if err != nil {
		s.log.Warn("出厂钩退包失败：构造背包实例失败", "item", def.ItemID, "err", err)
		return nil
	}
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		s.log.Warn("出厂钩退包失败：开事务失败", "err", err)
		return nil
	}
	defer tx.Rollback()
	granted, _, err := grantPreparedItemTx(ctx, tx, prepared, true)
	if err != nil {
		s.log.Warn("出厂钩退包失败：入库失败", "item", def.ItemID, "err", err)
		return nil
	}
	if err := tx.Commit(); err != nil {
		s.log.Warn("出厂钩退包失败：提交失败", "err", err)
		return nil
	}
	if t, terr := s.itemTree(ctx, granted); terr == nil {
		s.log.Info("出厂钩退包", "uid", uid, "item", def.ItemID, "guid", shortGUID(granted.GUID))
		return t
	}
	return nil
}

// setLureHookSelection 同步竿上拟饵的 bait_hook 关联。
// 关联行以竿 GUID 为主键、key=bait_hook:<拟饵GUID>、value=当前钩 GUID；
// hookGUID 为空时删除关联，由后续 baitHookAssignments 按新活动钩重建。
func (s *Server) setLureHookSelection(ctx context.Context, lureGUID, hookGUID string) {
	key := "bait_hook:" + lureGUID
	var err error
	if hookGUID == "" {
		_, err = s.DB.SQL.ExecContext(ctx, `DELETE FROM rod_setting WHERE key=?`, key)
	} else {
		_, err = s.DB.SQL.ExecContext(ctx, `UPDATE rod_setting SET value=? WHERE key=?`, hookGUID, key)
	}
	if err != nil {
		s.log.Warn("拟饵钩位关联同步失败", "lure", shortGUID(lureGUID), "hook", shortGUID(hookGUID), "err", err)
	}
}

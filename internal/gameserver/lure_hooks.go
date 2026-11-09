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
//
// 并发纪律（PR#339 评审第 2 轮 P1）：装/拆拆成 Prepare（池读快照：静态
// 定义裁决 + 退包只读准备，静态库纪律要求 item_def 查询在事务外）与
// Commit（事务内以 GetItemByGUIDTx 重读全部可变状态复验）两段。预检
// 快照与提交时最新状态不一致（不同 call_id 并发装/拆）一律整体拒绝；
// 新钩归属用条件 UPDATE + RowsAffected 把关。request_receipts 只串行化
// 相同 call_id，挡不住这类交错。

import (
	"context"
	"database/sql"
	"fmt"

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

// maxLureHookSlots 出厂钩 key 空间上界（静态目录品类 17 最多 3 钩，留余量）。
const maxLureHookSlots = 8

// lureOriginalFactorySlot 返回钩 GUID 对应的原装出厂钩槽位（-1 = 非原装）。
// 与当前数组下标无关：短缩后原槽位号会漂移（先卸槽 0，原槽 1 的原装钩
// 变成当前下标 0），按下标单点匹配会把原装钩误判成普通钩导致替换时
// 漏退（2026-11-09 PR#339 评审 P2）。key 空间小，全量扫描。
func lureOriginalFactorySlot(lureGUID, hookGUID string) int {
	if hookGUID == "" {
		return -1
	}
	for i := 0; i < maxLureHookSlots; i++ {
		if hookGUID == uuidLike(lureGUID, lureFactoryHookKey(i)) {
			return i
		}
	}
	return -1
}

// isLureLegacyArtifactGUID 判断 guid 是否为 v3~v7 实验残留（:r 垫底钩 /
// :e 幻影空槽）——迁移剥离用，同样与下标无关（短缩后下标漂移）。
func isLureLegacyArtifactGUID(lureGUID, guid string) bool {
	for i := 0; i < maxLureHookSlots; i++ {
		if guid != "" && (guid == uuidLike(lureGUID, lureRestoredHookKey(i)) ||
			guid == uuidLike(lureGUID, lurePhantomHookKey(i))) {
			return true
		}
	}
	return false
}

// setWireParentFields 修改物品字段的父引用与容器枚举（纯字段操作，
// 落库由调用方负责）。setWireParent 的无副作用内核，供事务路径复用。
func setWireParentFields(fields []proto.Field, wireClass, parentGUID string) {
	p := fieldIdxByCS(wireClass, csParent)
	if p < 0 || p >= len(fields) || (fields[p].Op != "OPTGUID" && fields[p].Op != "GUID16") {
		return // 字段不存在或类型不符时不修改
	}
	fields[p].Hex = parentGUID
	if sidx := fieldIdxByCS(wireClass, csSlotEnum); sidx >= 0 && fieldOpAt(wireClass, sidx) == "BYTE" {
		if parentGUID == "" {
			fields[sidx].Num = ptrU64(0) // 背包
		} else {
			fields[sidx].Num = ptrU64(4) // rig
		}
	}
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

// lureActiveOrdinalAfterStrip 按剥离映射计算新的活动钩下标。
// keptOld[k] 是剥离后第 k 个保留元素的原始下标（元素紧凑前移，新下标即
// k）。语义（PR#339 评审 P2）：活动元素仍保留时随之前移到新位置；活动
// 元素本身被剥离时落到其后第一个保留槽（其后没有则收末位）；数组清空
// 返回 0。绝不因数组缩短把仍存在的活动钩错误切回第一颗。
func lureActiveOrdinalAfterStrip(active int, keptOld []int) int {
	for k, old := range keptOld {
		if old == active {
			return k
		}
	}
	for k, old := range keptOld {
		if old > active {
			return k
		}
	}
	if len(keptOld) > 0 {
		return len(keptOld) - 1
	}
	return 0
}

// applyLureActiveOrdinal 把活动钩下标写回拟饵字段（f1，nkogmkobfng）。
// f1 字段缺失或不是数值时不写。
func applyLureActiveOrdinal(class string, fields []proto.Field, active int) {
	if len(fields) <= 1 || fields[1].Num == nil || active < 0 {
		return
	}
	k := fieldIdxByCS(class, "nkogmkobfng")
	if k < 0 || k >= len(fields) {
		return
	}
	fields[k].Num = ptrU64(uint64(active))
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

// lureHookMountPlan 装钩预检产物。全部字段来自池读快照与静态定义；
// 事务内以最新数据库状态复验后才生效。
type lureHookMountPlan struct {
	uid          int64
	lureGUID     string
	hookGUID     string
	extra        int64
	lureDefSlots int         // 拟饵定义出厂钩槽数（静态值，追加封顶用）
	oldGUID      string      // 预检数组中将被替换槽的钩 GUID（"" = 追加）
	oldRowGUID   string      // 旧钩有物品行实例时的行 GUID（事务内重读复核）
	oldRow       *store.Item // 预检读到的旧实例行（仅响应树构建用，归属以事务内重读为准）
	ordinal      int         // 事务内复验后的目标槽下标
	refundPrep   *store.Item // 原装出厂钩退包实例（事务内 grantPreparedItemTx 入库）
}

// lureHookMount 把背包里的钩装到拟饵的某个钩位上（15/1）。
// 旧钩返还规则见文件头：实例行退背包（f1）、原装出厂钩物化退包（f0）、
// 恢复版出厂钩不退。
func (s *Server) lureHookMount(ctx context.Context, uid int64, lureGUID, hookGUID string, extra int64) *rigSetResult {
	plan := s.lureHookMountPrepare(ctx, uid, lureGUID, hookGUID, extra)
	if plan == nil {
		return &rigSetResult{Rejected: true, UpdatedTrees: []*proto.Value{}}
	}
	return s.lureHookMountCommit(ctx, plan)
}

// lureHookMountPrepare 预检：池读快照上的归属/品类/损坏裁决，静态定义
// 查询（钩槽封顶）与退包只读准备。任何裁决失败返回 nil。可变状态（钩行
// 归属、拟饵数组）在此之后由 Commit 在事务内重验。
func (s *Server) lureHookMountPrepare(ctx context.Context, uid int64, lureGUID, hookGUID string, extra int64) *lureHookMountPlan {
	lureRow, err := s.DB.GetItemByGUID(ctx, lureGUID)
	if err != nil || !isLureRow(lureRow) || lureRow.UID != uid {
		s.log.Warn("拟饵换钩被拒：拟饵不属于当前玩家", "lure", shortGUID(lureGUID), "uid", uid)
		return nil
	}
	hookRow, err := s.DB.GetItemByGUID(ctx, hookGUID)
	if err != nil || hookRow == nil || hookRow.UID != uid || hookRow.Container != "backpack" || hookRow.RigGUID != "" {
		s.log.Warn("拟饵换钩被拒：鱼钩不属于当前玩家或不在背包", "hook", shortGUID(hookGUID), "uid", uid)
		return nil
	}
	if cat, ok := s.itemCategoryOf(ctx, hookRow); !ok || cat != catHook {
		s.log.Warn("拟饵换钩被拒：不是鱼钩", "item", shortGUID(hookGUID), "cat", cat)
		return nil
	}
	if s.itemBrokenForMount(ctx, hookRow) {
		s.log.Warn("拟饵换钩被拒：钩已损坏", "item", shortGUID(hookGUID))
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
	if list == nil || list.Kind != proto.KindArgs {
		list = proto.Args(proto.S(lureHookArgsCode))
	}
	items := append([]*proto.Value(nil), list.Items...)
	ordinal := lureHookMountOrdinal(extra, len(items))

	// 追加（空饵重装/客户端陈旧行号）时以定义出厂钩槽数为上限。
	// 槽数是静态定义值，事务内复验数组长度时直接复用。
	lureDefSlots := 0
	if ordinal >= len(items) {
		def, derr := s.DB.GetItemDef(ctx, lureRow.NumericID)
		if derr != nil || def == nil {
			return nil
		}
		cfgs, _ := defAttrs(def)["gpoaffnjbdo"].([]any)
		if len(items) >= len(cfgs) {
			s.log.Warn("拟饵装钩被拒：钩槽已满", "lure", shortGUID(lureGUID), "slots", len(cfgs))
			return nil
		}
		lureDefSlots = len(cfgs)
	}

	// 预检只确认钩能物化；真正写入拟饵的树必须在事务内按最新钩行重建，
	// 避免 Prepare 后耐久/字段变化时把旧快照写进 f0。
	tree, terr := s.itemTree(ctx, hookRow)
	if terr != nil || tree == nil || tree.Kind != proto.KindObj {
		return nil
	}

	plan := &lureHookMountPlan{
		uid: uid, lureGUID: lureGUID, hookGUID: hookGUID, extra: extra,
		lureDefSlots: lureDefSlots,
	}
	// 旧钩预备（读侧）：有物品行的实例 vs 原装出厂钩（全 key 空间匹配，
	// 与当前下标无关——PR#339 评审 P2：短缩后原槽位号漂移）。
	if ordinal < len(items) {
		plan.oldGUID = lureHookItemGUID(items, ordinal)
		if prev, perr := s.DB.GetItemByGUID(ctx, plan.oldGUID); perr == nil && prev != nil {
			plan.oldRowGUID = prev.GUID
			plan.oldRow = prev
		} else if lureOriginalFactorySlot(lureGUID, plan.oldGUID) >= 0 {
			prepared, ferr := s.prepareLureFactoryRefund(ctx, uid, items[ordinal])
			if ferr != nil || prepared == nil {
				// PR#339 评审 P2：识别为原装出厂钩但退包准备失败 → 整体
				// 拒绝，钩不得从数组消失而无退包。
				s.log.Warn("拟饵换钩被拒：出厂钩退包准备失败", "lure", shortGUID(lureGUID), "hook", shortGUID(plan.oldGUID))
				return nil
			}
			plan.refundPrep = prepared
		}
	}
	return plan
}

// lureHookMountCommit 事务段：全部可变状态以 GetItemByGUIDTx 重读复验后
// 才写入；预检快照与最新状态不一致（不同 call_id 并发装/拆）一律拒绝。
// 任何一步失败整体回滚，退包绝不脱离拟饵写回单独提交。
func (s *Server) lureHookMountCommit(ctx context.Context, plan *lureHookMountPlan) *rigSetResult {
	res := &rigSetResult{UpdatedTrees: []*proto.Value{}}
	tx, txErr := beginEconomyTx(ctx, s.DB)
	if txErr != nil {
		res.Rejected = true
		return res
	}
	defer tx.Rollback()
	reject := func(reason string) *rigSetResult {
		s.log.Warn("拟饵换钩被拒", "lure", shortGUID(plan.lureGUID), "hook", shortGUID(plan.hookGUID), "reason", reason)
		res.Rejected = true
		return res
	}
	rejectErr := func(stage string, err error) *rigSetResult {
		s.log.Warn("拟饵换钩被拒：事务失败", "lure", shortGUID(plan.lureGUID), "stage", stage, "err", err)
		res.Rejected = true
		return res
	}

	lureRow, err := s.DB.GetItemByGUIDTx(ctx, tx, plan.lureGUID)
	if err != nil || !isLureRow(lureRow) || lureRow.UID != plan.uid {
		return reject("拟饵状态已变化")
	}
	fields, err := FieldsFromJSON(lureRow.FieldsJSON)
	if err != nil {
		return rejectErr("解析拟饵字段", err)
	}
	idx := lureHookFieldIndex(lureRow.WireClass)
	if idx < 0 || idx >= len(fields) {
		return reject("拟饵字段结构已变化")
	}
	list := fields[idx].Val
	if list == nil || list.Kind != proto.KindArgs {
		list = proto.Args(proto.S(lureHookArgsCode))
	}
	items := append([]*proto.Value(nil), list.Items...)
	ordinal := lureHookMountOrdinal(plan.extra, len(items))
	if ordinal >= len(items) && (plan.lureDefSlots <= 0 || len(items) >= plan.lureDefSlots) {
		return reject("钩槽已满")
	}
	hookRow, err := s.DB.GetItemByGUIDTx(ctx, tx, plan.hookGUID)
	if err != nil || hookRow == nil || hookRow.UID != plan.uid {
		return reject("新钩归属已变化")
	}
	if hookRow.Durability <= 0 {
		return reject("新钩已损坏")
	}
	hookFields, hfErr := FieldsFromJSON(hookRow.FieldsJSON)
	if hfErr != nil {
		return rejectErr("解析新钩字段", hfErr)
	}
	hookFields = alignFieldsToOps(hookRow, hookFields)
	// 事务内按最新钩行重建写入拟饵的树，不能复用 Prepare 快照：耐久、
	// 工艺或字段可能在预检后变化。钩是简单终端件，这里直接投影可变字段。
	setWireFloat(hookFields, hookRow.WireClass, fieldIdxByCS(hookRow.WireClass, csWorkmanship), hookRow.Workmanship)
	setWireFloat(hookFields, hookRow.WireClass, fieldIdxByCS(hookRow.WireClass, csDurability), hookRow.Durability)
	setWireParentFields(hookFields, hookRow.WireClass, plan.lureGUID)
	hookTree := proto.Obj(hookRow.WireClass, uint32(hookRow.TypeID), hookFields...)

	// 旧钩槽一致性：预检与提交必须看到同一颗被替换钩（不同 call_id 的
	// 并发装/拆在两次预检之间改动了数组 → 拒绝，绝不基于陈旧快照退包）。
	oldGUID := ""
	if ordinal < len(items) {
		oldGUID = lureHookItemGUID(items, ordinal)
	}
	if oldGUID != plan.oldGUID {
		return reject("钓组已被其他操作修改，请重试")
	}
	plan.ordinal = ordinal
	if ordinal < len(items) {
		items[ordinal] = hookTree
	} else {
		items = append(items, hookTree)
	}
	list.Items = items
	fields[idx] = proto.Field{Op: "VAL", Val: list}

	if err := updateItemFieldsTx(ctx, s.DB, tx, lureRow, fields); err != nil {
		return rejectErr("拟饵写回", err)
	}
	// 新钩归属：条件 UPDATE 把关——仍须在背包且未被任何钓组持有，
	// RowsAffected != 1 即已被并发操作抢先。
	hookRes, err := tx.ExecContext(ctx,
		`UPDATE item SET rig_guid=?, slot=?, container='rig'
		 WHERE guid=? AND container='backpack' AND COALESCE(rig_guid,'')=''`,
		plan.lureGUID, catHook, plan.hookGUID)
	if err != nil {
		return rejectErr("新钩归属变更", err)
	}
	if n, nerr := hookRes.RowsAffected(); nerr != nil || n != 1 {
		return reject("新钩已被其他操作占用")
	}
	hookRow.RigGUID = plan.lureGUID
	if err := updateItemFieldsTx(ctx, s.DB, tx, hookRow, hookFields); err != nil {
		return rejectErr("新钩字段写回", err)
	}
	// 旧钩返还：带物品行的实例退回背包（行 + 字段清理）；原装出厂钩
	// 物化退包入库（新 guid → f0 新增段）。恢复/幻影命名空间不退。
	var refundGranted *store.Item
	if oldGUID != "" && oldGUID != plan.hookGUID {
		res.ReplacedGUID = oldGUID
		if plan.oldRowGUID != "" {
			oldRow, oerr := s.DB.GetItemByGUIDTx(ctx, tx, oldGUID)
			if oerr != nil || oldRow == nil || oldRow.UID != plan.uid {
				return reject("旧钩实例已被其他操作处理")
			}
			if err := s.DB.DetachReturnToBackpackTx(ctx, tx, oldGUID); err != nil {
				return rejectErr("旧钩退回背包", err)
			}
			oldRow.RigGUID = ""
			oldFields, ofErr := FieldsFromJSON(oldRow.FieldsJSON)
			if ofErr != nil {
				return rejectErr("解析旧钩字段", ofErr)
			}
			clearRigFieldsForBackpack(oldFields, oldRow.WireClass)
			if err := updateItemFieldsTx(ctx, s.DB, tx, oldRow, oldFields); err != nil {
				return rejectErr("旧钩字段写回", err)
			}
		} else if plan.refundPrep != nil {
			granted, _, err := grantPreparedItemTx(ctx, tx, plan.refundPrep, true)
			if err != nil {
				return rejectErr("出厂钩退包入库", err)
			}
			refundGranted = granted
		}
	}
	// 幽灵组件行与 bait_hook 关联同事务清理。
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM rig_component WHERE rig_guid=? AND slot=?`, plan.lureGUID, catHook); err != nil {
		return rejectErr("幽灵组件行清理", err)
	}
	// bait_hook 关联必须跟随最新活动钩，而不是本次装上的槽。装到非活动槽
	// 时保持原关联；替换活动槽时才改为新钩。关联行由竿侧流程创建，这里只
	// 更新已有行，不建行。
	activeGUID := lureHookItemGUID(items, lureActiveOrdinal(fields, len(items)))
	if err := syncLureBaitHookTx(ctx, tx, plan.lureGUID, activeGUID); err != nil {
		return rejectErr("bait_hook 关联更新", err)
	}
	if err := tx.Commit(); err != nil {
		return rejectErr("提交", err)
	}

	// 提交后读 fresh 构建响应树：新装钩树进 f0 新增段（与 17/2 同构）。
	if fresh, ferr := s.DB.GetItemByGUID(ctx, plan.hookGUID); ferr == nil && fresh != nil {
		if t, terr2 := s.itemTree(ctx, fresh); terr2 == nil {
			res.AddedTrees = append(res.AddedTrees, t)
		}
	}
	if refundGranted != nil {
		if t, terr2 := s.itemTree(ctx, refundGranted); terr2 == nil {
			res.AddedTrees = append(res.AddedTrees, t)
		}
	}
	if plan.oldRow != nil {
		if fresh, ferr := s.DB.GetItemByGUID(ctx, oldGUID); ferr == nil && fresh != nil {
			if t, terr2 := s.itemTree(ctx, fresh); terr2 == nil {
				res.UpdatedTrees = append(res.UpdatedTrees, t)
			}
		}
	}
	if lure, lerr := s.DB.GetItemByGUID(ctx, plan.lureGUID); lerr == nil && lure != nil {
		if t, terr2 := s.itemTree(ctx, lure); terr2 == nil {
			res.UpdatedTrees = append(res.UpdatedTrees, t)
		}
	}
	res.MountedGUID = plan.hookGUID
	s.log.Info("拟饵换钩", "lure", shortGUID(plan.lureGUID), "hook", shortGUID(plan.hookGUID), "ordinal", plan.ordinal)
	return res
}

// lureHookRemovePlan 拆钩预检产物。数组快照中的 removedGUID 用于事务内
// 一致性复验（不同 call_id 并发装/拆在预检后改动了数组 → 拒绝）。
type lureHookRemovePlan struct {
	uid         int64
	lureGUID    string
	hookGUID    string
	extra       int64
	removedGUID string       // 预检时该槽的钩 GUID
	removedTree *proto.Value // 预检时该槽的树（出厂钩退包物化用）
	removedRow  *store.Item  // 预检读到的实例行（归属以事务内重读为准）
	refundPrep  *store.Item  // 原装出厂钩退包实例（事务内 grantPreparedItemTx 入库）
}

// lureHookRemove 从拟饵钩位移除一颗钩（15/2）。
// hookGUID 非空时按 GUID 定位，否则按 extra/活动钩定位。
func (s *Server) lureHookRemove(ctx context.Context, uid int64, lureGUID, hookGUID string, extra int64) *rigDetachResult {
	plan := s.lureHookRemovePrepare(ctx, uid, lureGUID, hookGUID, extra)
	if plan == nil {
		return nil
	}
	return s.lureHookRemoveCommit(ctx, plan)
}

// lureHookRemovePrepare 预检：池读快照上的归属裁决与退包只读准备。
// 定位失败、钩不在拟饵上、退包准备失败一律返回 nil（拒绝）。
func (s *Server) lureHookRemovePrepare(ctx context.Context, uid int64, lureGUID, hookGUID string, extra int64) *lureHookRemovePlan {
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

	plan := &lureHookRemovePlan{
		uid: uid, lureGUID: lureGUID, hookGUID: hookGUID, extra: extra,
		removedGUID: lureHookItemGUID(items, ordinal),
		removedTree: items[ordinal],
	}
	// 卸下钩预备（读侧）：有物品行的实例 vs 原装出厂钩（全 key 空间
	// 匹配，与当前下标无关——PR#339 评审 P2）。
	if plan.removedGUID != "" {
		if row, rerr := s.DB.GetItemByGUID(ctx, plan.removedGUID); rerr == nil && row != nil && row.UID == uid {
			plan.removedRow = row
		} else if lureOriginalFactorySlot(lureGUID, plan.removedGUID) >= 0 {
			prepared, ferr := s.prepareLureFactoryRefund(ctx, uid, plan.removedTree)
			if ferr != nil || prepared == nil {
				// PR#339 评审 P2：识别为原装出厂钩但退包准备失败 → 整体
				// 拒绝，钩不得从数组消失而无退包。
				s.log.Warn("拟饵拆钩被拒：出厂钩退包准备失败", "lure", shortGUID(lureGUID), "hook", shortGUID(plan.removedGUID))
				return nil
			}
			plan.refundPrep = prepared
		}
	}
	return plan
}

// lureHookRemoveCommit 事务段：可变状态全部以 GetItemByGUIDTx 重读复验，
// 槽内容与预检不一致一律拒绝；任何一步失败整体回滚（写回失败绝不退包）。
func (s *Server) lureHookRemoveCommit(ctx context.Context, plan *lureHookRemovePlan) *rigDetachResult {
	tx, txErr := beginEconomyTx(ctx, s.DB)
	if txErr != nil {
		return nil
	}
	defer tx.Rollback()
	abort := func(reason string) *rigDetachResult {
		s.log.Warn("拟饵拆钩被拒", "lure", shortGUID(plan.lureGUID), "hook", shortGUID(plan.removedGUID), "reason", reason)
		return nil
	}
	abortErr := func(stage string, err error) *rigDetachResult {
		s.log.Warn("拟饵拆钩失败：事务失败", "lure", shortGUID(plan.lureGUID), "stage", stage, "err", err)
		return nil
	}
	lureRow, err := s.DB.GetItemByGUIDTx(ctx, tx, plan.lureGUID)
	if err != nil || !isLureRow(lureRow) || lureRow.UID != plan.uid {
		return abort("拟饵状态已变化")
	}
	fields, err := FieldsFromJSON(lureRow.FieldsJSON)
	if err != nil {
		return abortErr("解析拟饵字段", err)
	}
	idx := lureHookFieldIndex(lureRow.WireClass)
	if idx < 0 || idx >= len(fields) {
		return abort("拟饵字段结构已变化")
	}
	list := fields[idx].Val
	if list == nil || list.Kind != proto.KindArgs || len(list.Items) == 0 {
		return abort("钩槽数组已变化")
	}
	items := append([]*proto.Value(nil), list.Items...)
	// 用与预检同一定位规则在最新数组上复定位；槽内容不一致即并发改动。
	ordinal := -1
	if plan.hookGUID != "" {
		for i := range items {
			if lureHookItemGUID(items, i) == plan.hookGUID {
				ordinal = i
				break
			}
		}
	}
	if ordinal < 0 {
		ordinal = lureHookRemoveOrdinal(plan.extra, len(items))
	}
	if ordinal < 0 || ordinal >= len(items) || lureHookItemGUID(items, ordinal) != plan.removedGUID {
		return abort("钓组已被其他操作修改，请重试")
	}

	// 官方语义（文件头）：卸钩 = 元素从数组移除（缩短）。绝不发任何
	// 保长空槽表示（三种均实锤崩客户端）。活动钩下标按剥离映射随迁
	// （PR#339 评审 P2：不再机械回落 0，把仍在的活动钩切到第一颗）。
	activeBefore := lureActiveOrdinal(fields, len(items))
	keptOld := make([]int, 0, len(items)-1)
	for i := range items {
		if i != ordinal {
			keptOld = append(keptOld, i)
		}
	}
	items = append(items[:ordinal], items[ordinal+1:]...)
	newActive := lureActiveOrdinalAfterStrip(activeBefore, keptOld)
	list.Items = items
	fields[idx] = proto.Field{Op: "VAL", Val: list}
	applyLureActiveOrdinal(lureRow.WireClass, fields, newActive)

	if err := updateItemFieldsTx(ctx, s.DB, tx, lureRow, fields); err != nil {
		return abortErr("拟饵写回", err)
	}
	var refundGranted *store.Item
	if plan.removedGUID != "" {
		if plan.removedRow != nil {
			removedRow, rerr := s.DB.GetItemByGUIDTx(ctx, tx, plan.removedGUID)
			if rerr != nil || removedRow == nil || removedRow.UID != plan.uid {
				return abort("卸下钩实例已被其他操作处理")
			}
			if err := s.DB.DetachReturnToBackpackTx(ctx, tx, plan.removedGUID); err != nil {
				return abortErr("卸下钩退回背包", err)
			}
			removedRow.RigGUID = ""
			removedFields, rfErr := FieldsFromJSON(removedRow.FieldsJSON)
			if rfErr != nil {
				return abortErr("解析卸下钩字段", rfErr)
			}
			clearRigFieldsForBackpack(removedFields, removedRow.WireClass)
			if err := updateItemFieldsTx(ctx, s.DB, tx, removedRow, removedFields); err != nil {
				return abortErr("卸下钩字段写回", err)
			}
		} else if plan.refundPrep != nil {
			granted, _, err := grantPreparedItemTx(ctx, tx, plan.refundPrep, true)
			if err != nil {
				return abortErr("出厂钩退包入库", err)
			}
			refundGranted = granted
		}
	}
	// 幽灵组件行与 bait_hook 关联同事务清理（选中钩让位，由
	// baitHookAssignments 按新活动钩重建）。
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM rig_component WHERE rig_guid=? AND slot=?`, plan.lureGUID, catHook); err != nil {
		return abortErr("幽灵组件行清理", err)
	}
	// 拆非活动钩不能清掉活动钩关联；活动钩变化时才改为新活动钩，
	// 数组清空则删除关联。
	activeGUID := lureHookItemGUID(items, newActive)
	if err := syncLureBaitHookTx(ctx, tx, plan.lureGUID, activeGUID); err != nil {
		return abortErr("bait_hook 关联更新", err)
	}
	if err := tx.Commit(); err != nil {
		return abortErr("提交", err)
	}

	res := &rigDetachResult{CompGUID: plan.removedGUID}
	if plan.removedRow != nil {
		if fresh, ferr := s.DB.GetItemByGUID(ctx, plan.removedGUID); ferr == nil && fresh != nil {
			if t, terr := s.itemTree(ctx, fresh); terr == nil {
				res.ReturnedTree = t
			}
		}
	}
	if refundGranted != nil {
		if t, terr := s.itemTree(ctx, refundGranted); terr == nil {
			res.RefundTree = t
		}
	}
	s.log.Info("拟饵拆钩", "lure", shortGUID(plan.lureGUID), "hook", shortGUID(plan.removedGUID), "ordinal", ordinal, "active", newActive)
	return res
}

// syncLureBaitHookTx 把竿上的 bait_hook:<lure> 关联同步到当前活动钩。
// 非活动槽装/拆不改活动钩；数组为空时删除关联。
func syncLureBaitHookTx(ctx context.Context, tx *sql.Tx, lureGUID, activeGUID string) error {
	key := "bait_hook:" + lureGUID
	if activeGUID == "" {
		_, err := tx.ExecContext(ctx, `DELETE FROM rod_setting WHERE key=?`, key)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE rod_setting SET value=? WHERE key=?`, activeGUID, key)
	return err
}

// prepareLureFactoryRefund 构造出厂钩退包实例（读库准备，不落库）。
// 定义查询在事务外（静态库隔离纪律），落库由调用方经 grantPreparedItemTx
// 与拟饵 f0 写回放进同一事务（PR#339 评审 P1）。准备失败返回错误，调用方
// 整体拒绝——钩不得从数组消失而无退包（PR#339 评审 P2）。
func (s *Server) prepareLureFactoryRefund(ctx context.Context, uid int64, hookTree *proto.Value) (*store.Item, error) {
	if hookTree == nil || hookTree.Kind != proto.KindObj {
		return nil, fmt.Errorf("出厂钩退包：子对象为空")
	}
	var numID int64
	if i := fieldIdxByCS(hookTree.Class, csNumericID); i >= 0 && i < len(hookTree.Fields) && hookTree.Fields[i].Num != nil {
		numID = int64(u64Of(hookTree.Fields[i].Num))
	}
	if numID <= 0 {
		return nil, fmt.Errorf("出厂钩退包：子对象缺 NumericID class=%s", hookTree.Class)
	}
	def, err := s.DB.GetItemDef(ctx, numID)
	if err != nil {
		return nil, fmt.Errorf("出厂钩退包：物品定义查询失败 numeric=%d: %w", numID, err)
	}
	if def == nil {
		return nil, fmt.Errorf("出厂钩退包：物品定义不存在 numeric=%d", numID)
	}
	prepared, err := s.prepareNewItem(ctx, uid, def.ItemID, "backpack", 1, "")
	if err != nil {
		return nil, fmt.Errorf("出厂钩退包：构造背包实例失败 item=%s: %w", def.ItemID, err)
	}
	return prepared, nil
}

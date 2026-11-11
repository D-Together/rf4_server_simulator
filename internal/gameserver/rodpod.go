package gameserver

import (
	"encoding/hex"
	"math"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// 竿架与竿位电子报警器设置。
//
// 竿架（wire 类 abmokghdbhf，typeID 11500）的 f1 字段（cfpajgilkng）是各竿位的
// 报警器设置数组，客户端按数组元素个数建出每个竿位的开关/颜色/声音/音量选项。
// 服务端不下发 f1 时客户端建不出竿位设置，电子报警器不生效。
//
// f1 编码（客户端版本 4.0.25034）：typedarr 头 [0x03][短串类型名][u16 n]，
// 类型名为客户端类型注册表序号 "369"；每个元素为 [0x01][u32 11502] + 7 字节
// 载荷（enabled/color/sound 各 1 字节，音量 float32 小端 4 字节）。
//
// 客户端改设置时按 18/1（单个竿位）与 18/2（f0 主设置）上报，这里夹紧到合法
// 范围后落库到物品 fields_json。

const (
	rodPodWireClass = "abmokghdbhf"
	rodPodSlotsCS   = "cfpajgilkng"
	rodPodMasterCS  = "miecmmbpolk"

	// 元素对象 codec nldiapcifdp、类型号 11502；typedarr 头的类型名是客户端
	// 类型注册表序号字符串 "369"（与鱼群数组 "135"、卡集数组 "362" 同惯例）。
	// 类型名查不到时客户端解析抛异常，会卡在载入个人资料。
	rodPodSlotCodec  = "nldiapcifdp"
	rodPodSlotTypeID = 11502
	rodPodSlotName   = "369"

	// rodPodMaxSlots 防止畸形包把数组撑爆。
	rodPodMaxSlots = 8

	// rodPodInjectSlots 控制是否在 4/5 物品树下发 f1（竿位报警器设置）。
	rodPodInjectSlots = true
)

// rodPodSlotCount 竿架竿位数。只有 03 系列（Kingfisher RDIII 等）带电子报警器。
func rodPodSlotCount(itemID string, numericID int64) int {
	switch itemID {
	case "rod_pod_03al", "rod_pod_03b", "rod_pod_03bm", "rod_pod_03c",
		"rod_pod_03dg", "rod_pod_03g", "rod_pod_03r":
		return 3
	case "rod_pod_02":
		return 2
	case "rod_pod", "rod_pod_2":
		return 1
	}
	switch numericID {
	case 11664, 11665, 11666, 11667, 11668, 11669, 11670:
		return 3
	case 11663:
		return 2
	}
	return 1
}

// encodeRodPodSlot 把单个竿位报警器设置编码成元素对象。线上形状为
// [0x01][u32 11502] + 载荷，载荷按 codec nldiapcifdp 布局：
// 启用 1B / 颜色 1B / 声音 1B / 音量 float32 小端 4B。
func encodeRodPodSlot(enabled, color, sound uint64, volume float32) *proto.Value {
	return proto.Obj(rodPodSlotCodec, rodPodSlotTypeID,
		proto.FNum("OPT", enabled),
		proto.FNum("BYTE", color),
		proto.FNum("BYTE", sound),
		proto.FNum("U32", uint64(math.Float32bits(volume))),
	)
}

// buildRodPodSlotsArray 拼出 f1 的 typedarr：[0x03]["369"][u16 n] + 元素。
func buildRodPodSlotsArray(items []*proto.Value) *proto.Value {
	return &proto.Value{Kind: proto.KindTypedArr, TypeName: rodPodSlotName, Items: items}
}

// defaultRodPodSlot 单个竿位的出厂报警器设置：开启、红色、默认音、音量 0.5。
func defaultRodPodSlot() *proto.Value {
	return encodeRodPodSlot(1, 0, 0, 0.5)
}

// defaultRodPodSlots 按竿架型号构造 f1：N 个默认竿位设置。
func defaultRodPodSlots(itemID string, numericID int64) *proto.Value {
	n := rodPodSlotCount(itemID, numericID)
	items := make([]*proto.Value, n)
	for i := range items {
		items[i] = defaultRodPodSlot()
	}
	return buildRodPodSlotsArray(items)
}

// slotNum 取竿位对象第 i 个字段的整数值。
func slotNum(v *proto.Value, i int) uint64 {
	if v == nil || i < 0 || i >= len(v.Fields) || v.Fields[i].Num == nil {
		return 0
	}
	return *v.Fields[i].Num
}

// decodeRodPodSlots 把 f1 的 typedarr 解回逐槽（启用/颜色/声音/音量）。
// 兼容旧数据里存的裸 7 字节元素（KindRaw）：按 codec 布局就地还原。
func decodeRodPodSlots(v *proto.Value) []*proto.Value {
	if v == nil || v.Kind != proto.KindTypedArr {
		return nil
	}
	out := make([]*proto.Value, 0, len(v.Items))
	for _, it := range v.Items {
		if it == nil {
			continue
		}
		switch it.Kind {
		case proto.KindObj:
			out = append(out, encodeRodPodSlot(slotNum(it, 0), slotNum(it, 1), slotNum(it, 2),
				math.Float32frombits(uint32(slotNum(it, 3)))))
		case proto.KindRaw:
			b, err := hex.DecodeString(it.Hex)
			if err != nil || len(b) != 7 {
				continue
			}
			out = append(out, encodeRodPodSlot(uint64(b[0]), uint64(b[1]), uint64(b[2]),
				math.Float32frombits(uint32(b[3])|uint32(b[4])<<8|uint32(b[5])<<16|uint32(b[6])<<24)))
		}
	}
	return out
}

// ensureRodPodSlots 给没有竿位设置的竿架补默认值（只补内存树，不改库里已有数据）。
func ensureRodPodSlots(it *store.Item, fields []proto.Field) {
	idx := fieldIdxByCS(rodPodWireClass, rodPodSlotsCS)
	if idx < 0 || idx >= len(fields) {
		return
	}
	if len(decodeRodPodSlots(fields[idx].Val)) > 0 {
		return
	}
	fields[idx] = proto.FVal("VAL", defaultRodPodSlots(it.ItemID, it.NumericID))
}

// normalizeRodPodSlot 把客户端上报的设置压成固定 4 字段对象（越界值夹紧）。
func normalizeRodPodSlot(src *proto.Value) *proto.Value {
	enabled := slotNum(src, 0)
	if enabled > 1 {
		enabled = 1
	}
	color := slotNum(src, 1)
	if color > 5 {
		color = 5
	}
	sound := slotNum(src, 2)
	if sound > 10 {
		sound = 10
	}
	return encodeRodPodSlot(enabled, color, sound, math.Float32frombits(uint32(slotNum(src, 3))))
}

// rodPodArgsOf 解析 18/x 的异构参数：竿架 guid、整数序列（首个通常是客户端
// 枚举）、以及设置结构体对象。
func rodPodArgsOf(tree *proto.Value) (guid string, ints []int64, obj *proto.Value) {
	if tree == nil || tree.Kind != proto.KindArgs {
		return
	}
	for _, it := range tree.Items {
		if it == nil {
			continue
		}
		switch it.Kind {
		case proto.KindGuid:
			if guid == "" && it.Hex != "" {
				guid = it.Hex
			}
		case proto.KindU32, proto.KindU64:
			ints = append(ints, int64(it.Num))
		case proto.KindObj:
			obj = it
		}
	}
	return
}

// rodPodRowFor 取本会话拥有、且确实是竿架的物品行。
func rodPodRowFor(ctx *Ctx, guid string) (*store.Item, []proto.Field, bool) {
	srv := ctx.Sess.Srv
	row, err := srv.DB.GetItemByGUID(ctx.Sess.pushCtx(), guid)
	if err != nil || row == nil || row.UID != ctx.Sess.Userid || row.WireClass != rodPodWireClass {
		return nil, nil, false
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return nil, nil, false
	}
	fields = alignFieldsToOps(row, fields)
	return row, fields, true
}

func init() {
	RegisterExtra(registerRodPod)
}

func registerRodPod(d *Dispatch) {
	d.Register(18, 1, hRodPodSetSlot)
	d.Register(18, 2, hRodPodSetMaster)
}

// hRodPodSetSlot 18/1（fire-and-forget）：单个竿位的报警器设置落库。
// 参数为 args[枚举, 竿架 guid, 竿位, 设置对象]。
func hRodPodSetSlot(ctx *Ctx) ([]byte, bool) {
	guid, ints, obj := rodPodArgsOf(ctx.Tree)
	if guid == "" || len(ints) == 0 || obj == nil {
		return nil, true
	}
	// 首个整数是客户端枚举，竿位序号取最后一个整数。
	slot := int(ints[len(ints)-1])
	if slot < 0 || slot >= rodPodMaxSlots {
		return nil, true
	}
	row, fields, ok := rodPodRowFor(ctx, guid)
	if !ok {
		ctx.Log.Info("18/1 竿架不存在或不属于本玩家", "竿架", shortGUID(guid))
		return nil, true
	}
	idx := fieldIdxByCS(row.WireClass, rodPodSlotsCS)
	if idx < 0 || idx >= len(fields) {
		return nil, true
	}
	items := decodeRodPodSlots(fields[idx].Val)
	if len(items) == 0 {
		// 存量竿架还没落过 f1：先按型号铺满默认竿位，避免把竿位数缩水。
		n := rodPodSlotCount(row.ItemID, row.NumericID)
		for i := 0; i < n; i++ {
			items = append(items, defaultRodPodSlot())
		}
	}
	for len(items) <= slot {
		items = append(items, defaultRodPodSlot())
	}
	items[slot] = normalizeRodPodSlot(obj)
	fields[idx] = proto.FVal("VAL", buildRodPodSlotsArray(items))
	ctx.Sess.Srv.saveItemFields(ctx.Sess.pushCtx(), row, fields)
	ctx.Log.Info("18/1 竿位报警器设置", "竿架", shortGUID(guid), "竿位", slot)
	return nil, true
}

// hRodPodSetMaster 18/2（fire-and-forget）：竿架 f0 主设置回写。
// 参数为 args[枚举, 竿架 guid, 值]。
func hRodPodSetMaster(ctx *Ctx) ([]byte, bool) {
	guid, ints, _ := rodPodArgsOf(ctx.Tree)
	if guid == "" || len(ints) == 0 {
		return nil, true
	}
	row, fields, ok := rodPodRowFor(ctx, guid)
	if !ok {
		return nil, true
	}
	idx := fieldIdxByCS(row.WireClass, rodPodMasterCS)
	if idx < 0 || idx >= len(fields) {
		return nil, true
	}
	value := uint64(ints[len(ints)-1])
	if fields[idx].Num != nil && *fields[idx].Num == value {
		return nil, true
	}
	fields[idx].Num = ptrU64(value)
	ctx.Sess.Srv.saveItemFields(ctx.Sess.pushCtx(), row, fields)
	ctx.Log.Info("18/2 竿架主设置", "竿架", shortGUID(guid), "值", value)
	return nil, true
}

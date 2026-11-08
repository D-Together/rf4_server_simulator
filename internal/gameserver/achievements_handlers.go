package gameserver

import (
	"strings"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func registerAchievementHandlers(d *Dispatch) {
	d.Register(3, 3, hAchievements305)
	d.Register(3, 16, hPlayerRewards2015)
	d.Register(3, 17, hFishTrophies323)
}

// hPlayerRewards2015 处理 3/16 奖赏列表。客户端 RPC 签名
// public iidloljandl[] cfbomeockod() 返回 2015 数组，容器名 "185"
// （argcode_table by_type_id[2015]=185 = by_cmd[3/16]）。
//
// 数据源：玩家 house/backpack/keepnet 里 wire 类 enikeammfen（物品类别 72 奖赏）
// 的物品——与官方把嘉奖当物品存储的方式一致。纯查询：不写库、不推送。
func hPlayerRewards2015(ctx *Ctx) ([]byte, bool) {
	if ctx.Sess == nil || ctx.Sess.Srv == nil || ctx.Sess.Srv.DB == nil {
		return encodeArgsCode(ctx, "185")
	}
	srv := ctx.Sess.Srv
	cctx := ctx.Sess.pushCtx()
	rows, err := srv.DB.ListItemsByContainers(cctx, ctx.Sess.Userid, []string{"house", "backpack", "keepnet"})
	if err != nil {
		ctx.Log.Error("3/16 奖赏查询失败", "err", err)
		return encodeArgsCode(ctx, "185")
	}
	items := make([]*proto.Value, 0, len(rows))
	for _, it := range rows {
		if it.WireClass != "enikeammfen" && it.TypeID != 2015 {
			continue
		}
		kind := uint32(0) // 0=背包/其它
		if it.Container == "house" {
			kind = 10 // 房/家（奖杯陈列位）
		}
		created := netTicksNow()
		updatedFT := proto.WindowsFiletimeNow()
		if it.CreatedTS > 0 {
			created = netTicksFromUnix(it.CreatedTS)
			updatedFT = proto.UnixToFiletime(int64(it.CreatedTS))
		}
		wm := it.Workmanship
		if wm <= 0 {
			wm = 1
		}
		du := it.Durability
		if du <= 0 {
			du = 1
		}
		obj, merr := wiregen.MakeObj("enikeammfen", map[int]*proto.Value{
			0:  proto.U32(placeOfItem(it.ItemID)),
			1:  proto.U32(0), // 0=对其他玩家可见、1=隐藏
			2:  proto.Guid(it.GUID),
			3:  proto.Guid(""),
			4:  proto.U32(kind),
			5:  proto.U32(uint32(it.NumericID)),
			6:  proto.U32(uint32(wm)),
			7:  proto.U32(uint32(du)),
			8:  proto.U64(uint64(created)),
			9:  proto.U64(uint64(updatedFT)),
			10: proto.U32(0),
			11: proto.Null(),
			12: proto.Null(),
			13: proto.U32(uint32(it.Qty)),
			14: proto.U32(0),
		})
		if merr != nil {
			ctx.Log.Error("3/16 奖赏条目编码失败", "err", merr, "guid", it.GUID)
			continue
		}
		items = append(items, obj)
	}
	body, err := proto.EncodeValue(proto.Args(proto.S("185"), items...), ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("3/16 奖赏响应编码失败", "err", err)
		return encodeArgsCode(ctx, "185")
	}
	return body, true
}

// placeOfItem 从奖牌 item_id 推「名次」（enikeammfen.f0 = ijahnpdddfc，
// 客户端本地化 kDlg_Rewards_Position = 第{0}名）。仅认竞赛类 _1/_2/_3 家族
// （ft_*、weekly_medal_*），刻意排除 statuette_fisherman_10 / trophy_level_* 这类
// 「计数/等级」后缀，避免把 10 当成第10名。非名次类返回 0。
func placeOfItem(itemID string) uint32 {
	if !strings.HasPrefix(itemID, "ft_") && !strings.HasPrefix(itemID, "weekly_medal_") {
		return 0
	}
	switch {
	case strings.HasSuffix(itemID, "_1"):
		return 1
	case strings.HasSuffix(itemID, "_2"):
		return 2
	case strings.HasSuffix(itemID, "_3"):
		return 3
	}
	return 0
}

func hAchievements305(ctx *Ctx) ([]byte, bool) {
	if ctx.Sess == nil || ctx.Sess.Srv == nil || ctx.Sess.Srv.DB == nil {
		return encodeArgsCode(ctx, "114")
	}
	v, err := ctx.Sess.Srv.buildAchievements305(ctx.Sess.Srv.dbCtx(), ctx.Sess.Userid)
	if err != nil {
		ctx.Log.Error("3/3 成就查询失败", "err", err)
		return encodeArgsCode(ctx, "114")
	}
	body, err := proto.EncodeValue(v, ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("3/3 成就编码失败", "err", err)
		return encodeArgsCode(ctx, "114")
	}
	// 纯查询：不判定、不写库、不推送。成就解锁与弹窗只发生在玩法事件后。
	return body, true
}

func hFishTrophies323(ctx *Ctx) ([]byte, bool) {
	if ctx.Sess == nil || ctx.Sess.Srv == nil || ctx.Sess.Srv.DB == nil {
		return encodeArgsCode(ctx, "132")
	}
	v, err := ctx.Sess.Srv.buildFishTrophies323(ctx.Sess.Srv.dbCtx(), ctx.Sess.Userid)
	if err != nil {
		ctx.Log.Error("3/17 鱼种奖杯查询失败", "err", err)
		return encodeArgsCode(ctx, "132")
	}
	body, err := proto.EncodeValue(v, ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("3/17 鱼种奖杯编码失败", "err", err)
		return encodeArgsCode(ctx, "132")
	}
	return body, true
}

func encodeArgsCode(ctx *Ctx, code string) ([]byte, bool) {
	body, err := proto.EncodeValue(proto.Args(proto.S(code)), ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("空参数列表编码失败", "code", code, "err", err)
		return emptyArgsBody(), true
	}
	return body, true
}

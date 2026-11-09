# 图内诱饵贩卖机（*_vend 配方）一键付费制作

Status: done

<!-- 状态取值：draft | ready | in-progress | done | cancelled
     完成时原地改这里，不要归档、不要搬家、不要另开第二份。 -->

## Problem Statement

玩家在图内诱饵搅拌机/贩卖机界面选好配料点搅拌，客户端一直弹「未成功，请再试一次」。
日志（2026-11-09 20:32，userid=1000001，level_002_kirzah_river）三次失败均为
`9/1 制作失败：材料不足 demand=craft_feed_mix_goroh missing=10`。
根因：贩卖机配方（`*_vend`）在官方语义里是"机器按货架价卖料 + 搅拌"一步完成，
玩家不需要持有原料；本服务端只实装了"扣自己背包里的料"这一种来源，
而玩家从未买过原料（txn 表零记录、背包零物品），制作必然失败。
雪上加霜的是 9/4 询价对贩卖机配方也回 0 银币，界面不显示任何价格，
玩家无从知道要先自己买料。

## Solution

`*_vend` 后缀的配方走"机器卖料+搅拌"：9/4 按货架价返回真实总价（数量随份数缩放，
按产出品类上限收敛）；9/1 在同一经济事务内按该总价扣银币并直接产出，
不再检查玩家背包/鱼护。家用版配方（无 `_vend` 后缀）保持"必须持有材料"不变。

## User Stories

1. 作为玩家，我在贩卖机界面选配料点搅拌，希望一步直接做出窝料并由机器扣银币，
   以便不用先去商品页逐样买料。
2. 边界：余额不足时制作失败（803(false)），不产出、不扣款、不留残料；
   配料没有货架价时制作失败并留服务端日志；
   数量超过单次上限（窝料 60 套等）按上限收费与产出，报价与扣款一致。

## Implementation Decisions

- 配方判定：`recipe_id` 以 `_vend` 结尾。静态库共 22 条（feed 14 + pva_stick 3 +
  pva_stinger 5），全部是贩卖机商品，后缀规则无歧义。
- 价格真源：新增 `store.DB.CraftIngredientSilverPrice(itemID)`：
  1. `shop_item` 基础货架与 `shop_item_override` 覆盖价中该商品的最低非零银价。
     9/1 载荷里没有门店 id，用全货架最低价保证"机器做不会比去店里买贵"；
     各 kind-0 货架同物同价（290/120/50/170 在 fishing_store_01/02 与 craft_baits_012 一致）。
  2. 仍无价（2026-11-10 实机：自制窝料添加槽可选到未上架成品饵 fplt_op_ccp7
     ——只有金币价目、cb_attr_cc 无任何货架行）按商店货架补齐同款口径估价：
     同类同级最贵 → 同类型中位价 → 同品类中位价 → 1000，再按 item_id 稳定
     上浮 0~10%（与 `augmentShopShelf` 同公式，同一商品估价恒定）。管理员可用
     `shop_item_override` 对偏估价配料精确调价（阶梯第一腿即读覆盖价）。
- 配料解析：需求带显式 `item_id` 直接采用（25 种配料全部有货架价，已核对）；
  类别/标签需求（craft_feed_universal/dry/pva_*_vend 的通用槽）取客户端材料槽
  f2 提交的具体物品（实抓报文 2026-11-09 20:32：槽 = {需求 id, 临时 GUID, 具体 item id}），
  槽序即需求序、不复用已认领槽；类别可校验则校验，标签沿用既有"无法反查、忽略"口径。
- 时序：配料解析与定价读静态库/覆盖价，必须在 `beginEconomyTx` 之前完成
  （SQLite 单连接，事务内不能走 `store.DB` 方法）。
- 扣款：事务内 `applyWalletTx(-total, 0, kind="buy", itemID=产出, note="vend recipe=...")`，
  复用商店购买的余额原子更新与流水；重放由 dispatch 回执表兜住（同 call_id 返回旧响应）。
- 自制窝料成分记账：扣款配料按 `usedMats` 写入实例用料（与背包扣料同口径），
  投进窝里后成分比例不变。
- 响应形状不变：803/501 字段结构照旧，只把 501.f1 从恒 0 改为真实银价；
  钱包 HUD 用既有 `Push2_1` 刷新。
- 数据与迁移：无表结构变更，静态库不动。回滚 = 还原 craft_ops.go / repos2.go 两处。

## Testing Decisions

- 层级：存储/行为层，走既有接缝 `openRecipeTestDB` + `newSkillsCraftServer` +
  `economyTestCtx`/`craftOK`（与 craft_keepnet_test.go 同层）。
- recipes 模板补导入 `shop_item` 静态表，期望值用手算字面量（真实货架价
  290+120+50+170=630，来自 server_data/static.sqlite3 2026-11-09 快照）。
- 用例：
  1. 付费制作成功：无原料、silver=1000 → 成功，silver=370，产出 feed_karp 60 份，
     背包无原料，实例用料记录 4 条。
  2. 余额不足：silver=100 → 失败，silver 不动、无产出（事务回滚不留痕）。
  3. 9/4 报价：qty=3 → f1=1890；qty=1000 → 收敛到上限 60，f1=37800、f3=60。
  4. 通用槽配料的 universal_vend：槽 f2 携带具体物品 → 总价 = 手算 840。
  5. 未上架配料的估价：ccp7/cat19、cb_attr_cc/cat20 在模板库（无 item_catalog
     数据，medianByKind 跳过空 kind）落品类中位价 590/690，稳定上浮后
     619/690，总价手算 1859（2026-11-10 追加）。
  6. 回归钉：家用 craft_feed_carp 无材料仍失败（语义不变）。
- 回滚后：用例 1/3/4 变红（贩卖机不再收费或报价），其余不受影响。

## Out of Scope

- 金币通道（贩卖机金币价）：客户端实抓只有银币流，金币价暂不实装。
- 贩卖机库存扣减：货架库存 5 万+，官方未见扣减证据，不做。
- 制作时长（duration_sec）：沿用现状即时出料。
- 技能熟练度/成就口径：沿用 9/1 既有判定（craft_feed_ 前缀 → 配置饵料 7 类）。

## Further Notes

- `[事实]` 失败日志：2026-11-09T20:32:11/16/33 `9/1 制作失败：材料不足
  demand=craft_feed_mix_goroh missing=10`，配方 craft_feed_carp_vend。
- `[事实]` 玩家无原料：item 表无 mix_%/feedadds_%/aroma_%，txn 表无购买记录。
- `[事实]` 9/4 报价 0：2026-11-09T20:32:07~09 六次询价回包 f1=0。
- `[事实]` 槽 f2 携带具体 item id：9/1 报文槽 {craft_feed_mix_goroh, GUID16,
  "mix_groundpeas"}。
- `[推断]` 官方贩卖机 = 卖料+搅拌一步完成：由配方成对出现（craft_feed_carp 家用 /
  _vend 机器版、材料完全一致）、机器界面无需持有即可装槽、9/4 存在报价语义综合推断；
  无官方服务端抓包直接证据。
- `[事实]` 2026-11-10 00:25 实机（新二进制）：craft_feed_lin_vend 制作成功
  （显式配料链路通）；craft_feed_universal_vend 三次失败于
  `配料 fplt_op_ccp7 没有货架价`——自制窝料添加槽可选中只有金币价目的成品饵。
- `[推断]` 估价阶梯取 augmentShopShelf 同款公式是"与现有估价机制一致"的最小选择；
  运行时 ccp7 落 seriesMax（自家金币价 3金×100=300+5%上浮=315 银，官方同款
  5/12mm 货架银价 680），cb_attr_cc 落 misc 类型中位价 3590（偏贵，可用
  shop_item_override 校准）。
- 未验证：金币购买通道的官方行为；官方贩卖机对未上架配料的计价方式。

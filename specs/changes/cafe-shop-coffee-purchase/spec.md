# 修复铜湖欧泊咖啡馆购买咖啡报内部错误

Status: done

## Problem Statement

玩家在铜湖（`level_018_moskitnoe`）的欧泊咖啡馆（`cafestore_18`）点“小杯咖啡/大杯咖啡/招牌热狗”并确认购买时，客户端弹出红色“错误 · 发生内部错误。如果问题仍然存在，请联系客服。”，商品不入库、金币不扣。

商品在界面上是可见的、价格也正确显示（0.04/0.06/0.01 金币），所以玩家不会预期失败——这是“能点、能确认、才炸”的体验断裂。

`cafestore_18` 在静态 `shop_item` 中没有逐店实录，解析回退到共享 kind 1（食品店）模板；而 `cafe_ukleyka` 产品线（小杯咖啡、大杯咖啡、招牌热狗）的品牌信息落在 `item_def.kind` 列，`brand` 列为空。货架补齐只按 `item_def.brand` 比对品牌，因此这三件商品永远补不进生效货架：`22/1` 展示的是食品模板，`22/3` 按 `(store_id, item_id)` 查生效货架拿不到价目，返回 `System.InvalidOperationException`，客户端落到通用内部错误文案。

## Solution

铜湖欧泊咖啡馆只上架本店 `cafe_ukleyka` 三件商品，价格与静态 `item_price` 一致（内部整数单位金币 4/6/1），`22/2` 银币购买因无银币价而明确拒绝、不动账，`22/3` 金币购买扣款并入库。其他食品店依旧走各自的共享模板，不因这次改动获得咖啡馆商品。

## User Stories

1. 作为在铜湖活动的玩家，我希望在欧泊咖啡馆用金币买到咖啡，以便恢复精力/饱食，而不是点完收到内部错误。
2. 边界：咖啡馆货架不含面包、猪肉等食品厂商品，避免出现“咖啡馆卖生猪肉”的荒谬陈列。
3. 边界：咖啡只有金币价，银币通道必须明确拒绝且不产生半扣款、不产生幽灵物品。
4. 边界：后台 `shop_item_override` 仍可单独改这三件的价格与库存；零库存照常自动补货到 1000。

## Implementation Decisions

- 走**快速路径**：只改内嵌商店目录数据 + 测试，不改解析代码。理由：`brandMatches` 的通用语义一旦改成同时匹配 `item_def.kind`，会波及 75 家品牌店、把 114 件无价商品（`ny_bazaar_*` 的 `box_bublik_*`）按中位价估价上架——这是数据事故，不是本次要解决的问题。
- `internal/store/assets/shop_kind_defs.json` 的 `cafestore_18` 增加逐店 `shelf`，登记 `small_coffee`（gold 4）、`big_coffee`（gold 6）、`hd_box`（gold 1），基础库存各 1，与静态 `item_price` 逐字段对齐。沿用 `tungus_015` 已验证的逐店 `shelf` 机制，不新增代码分支。
- 不改表结构、不写玩家库、不改迁移、不改协议形状：`22/1` 仍是 args code 140 的 `ifdefblccde` 数组，`22/2`/`22/3` 仍是 `804`/`jfafgmcfjkc`。
- 回滚方式：把 `cafestore_18` 的 `shelf` 删掉、恢复目录 hash 常量即可；玩家库无迁移，无脏数据。

## Testing Decisions

- 存储层（真实 SQLite + 静态库，`internal/store/shop_catalog_test.go`）：断言逐店 `shelf` 三件商品与静态 `item_price` 一致、生效货架只有这三件、解析来源是 `store_definition` 而非 kind 模板。
- 行为层（`internal/gameserver/shop_cafe_purchase_test.go`）：`22/1` 返回恰好 3 行且价格等于 0/4/1、0/6/1、0/1/1；`22/3` 购买成功、响应过 `804` 契约校验、入库数量与 `item_def.ljnnpokapig` 一致（小杯 1、大杯 2）、账实一致；`22/2` 与“非本店商品”必须明确拒绝且零副作用。
- 期望值来源：静态 `item_price` 的实测行（`small_coffee` 0/4/1、`big_coffee` 0/6/1、`hd_box` 0/1/1）与 `item_def` 的 `ljnnpokapig`（1/2/1），不是用同一段生产代码再算一遍。
- 回滚后变红：删掉 `cafestore_18.shelf` 时两个新增测试都会红（`22/1` 退回 185 行食品模板、`22/3` 返回业务失败）。

## Out of Scope

- 不修改 `augmentShopShelf` 的品牌匹配语义（`item_def.kind` 参与匹配的影响面是 75 家品牌店，需要单独评估）。
- 不补齐 `item_price` 中缺失价目的其他 cafe 商品（如 `sandwich_cafe`、`thermos_coffee` 无价目，按既有规则不估价上架）。
- 不改客户端 Unity 资源层的商品展示逻辑。

## Further Notes

- `[事实]` 服务端对这笔失败有可复现的拒绝记录，原文：`商店拒绝购买 ... store=cafestore_18 item=small_coffee gold=true err="商品不在该店生效货架: cafestore_18@small_coffee: sql: no rows in result set"`。
- `[事实]` `server_data/static.sqlite3`：`shop_item` 无 `cafestore_18` 行；`item_def` 中 `small_coffee`/`big_coffee`/`hd_box` 的 `brand=''`、`kind='cafe_ukleyka'`；`item_price` 为 `(0,4,1)`/`(0,6,1)`/`(0,1,1)`。
- `[事实]` 改前 `ShopItems("cafestore_18")` 返回 185 行 kind 1 食品模板；改后返回 3 行本店商品。
- `[推断]` 客户端“发生内部错误”文案对应服务端业务失败信封；测试中 `ReplyFailure` 产出的正是 `System.InvalidOperationException`，与上述拒绝路径同源。
- `[事实]` 实机验证：修复部署后客户端在铜湖欧泊咖啡馆以 0.04 金币买下小杯咖啡，`txn` 记录 `gold_delta=-4`、`item` 落库 `backpack` 一件、钱包余额与界面显示一致；此后该店不再产生上述拒绝记录。

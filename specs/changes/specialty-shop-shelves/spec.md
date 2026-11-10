# 补齐特色商店与通古斯卡缺项

Status: done

## Problem Statement

多家地图特色商店在 `server_data/static.sqlite3` 的 `shop_item` 中没有专属货架。客户端可以列出本店商品，但购买时服务端按生效货架查价失败，返回“商品不在该店生效货架”，特色商品无法购买。

2026-11-10 又发现 `tungus_015`（通古斯卡合作社）虽然已有 93 条逐店 `shop_item` 实录，但 `tungus_bot` 和 `tungus_12pack` 既没有逐店行、也没有品牌补齐可命中的 `item_def.brand`，导致客户端购买被拒绝。

## Solution

为 13 家特色商店补齐基础货架：已有本店实录继续作为价格和库存真源，模板商品沿用现有模板价格与库存。对 `tungus_015` 的两个有明确静态价目的缺项，在嵌入式逐店目录中登记补货 SKU；有同 ID 实录时实录优先，仅在缺项时追加目录行，不把同品牌/同类型模板重新混入本店。`craft_baits_012` 复制 kind 0 渔具模板时排除 25 个在 `item_def`、`item_catalog` 和客户端本地化中都不存在的失效模板 SKU，避免把不可构造的商品暴露为在售。

## User Stories

1. 作为玩家，我希望特色商店展示的商品可以按界面价格购买，以便正常获得地图专属商品。
2. 作为玩家，我希望 `tungus_015` 的瓶装特产和 12 件装礼物可以正常购买，而不是因为缺少货架行被拒绝。
3. 边界：没有物品定义或客户端资源的模板 SKU 不上架，不能出现“显示有货但购买失败”。
4. 边界：模板商品保留原有库存语义，不因补齐本店货架而被统一改成 9999；逐店实录同 ID 的价格和库存优先于逐店目录补齐。

## Implementation Decisions

- 静态基础行继续放在 `server_data/static.sqlite3` 的 `shop_item`；该库不新增迁移，也不写玩家库。
- `internal/store/assets/shop_kind_defs.json` 的商店定义增加可选 `shelf`，仅登记有明确静态价目依据的逐店缺项。
- 基础货架解析顺序为：逐店实录（并追加缺失的逐店 `shelf` 行）→ 逐店 `shelf`（无实录时）→ 同类型同品牌实录 → 共享 kind 模板。
- `tungus_015` 只补 `tungus_bot`（金币 22）和 `tungus_12pack`（金币 129），基础库存各 1；价格使用 `item_price` 的内部整数单位。
- 逐店目录返回新对象，不共享 `shop_item` 实录或内嵌目录的可变行；`shop_item_override` 仍在基础货架选择后应用，零库存自动补货语义不变。
- 覆盖 `level_004_kvas`、`kala_store_003`、`milkstore_viiveri`、`sawmill_02`、`foodstore_garage`、`bakery_013`、`vakula_013`、`mill_013`、`paseka_sura`、`arbuz_store_009`、`tungus_015`、`craft_baits_012`、`varyag_012`。
- `craft_baits_012` 的基础行数为 8254：kind 0 模板 8267 行中剔除 25 个失效 SKU，再加海沙蚕、海沙蠋、虾米及其 10/20/50 箱装共 12 行。
- `sawmill_02` 的 16 行与 PR #323 内容一致；本 PR 合入后由本 PR 覆盖该修复，#323 关闭。
- 回滚方式：恢复 `server_data/static.sqlite3`/`shop_kind_defs.json` 并重新运行 seed；不涉及玩家库迁移。

## Testing Decisions

- 存储层新增静态库货架完整性测试：13 家目标商店的每条 `shop_item` 都必须能关联到 `item_def` 和 `item_catalog`。
- 新增逐店目录优先级测试：逐店实录同 ID 优先；缺项补齐；目录/实录返回对象互不共享；重复 SKU 不重复追加。
- 新增 `tungus_015` 端到端行为测试：22/1 返回 93 条实录加 2 条缺项，22/2/22/3 购买成功，失败整体回滚，重复请求不重复发放。
- 新增管理后台与购买货架一致性测试：后台列表、`ShopItemGet` 和成交价格使用同一解析结果；`shop_item_override` 仍优先生效。
- 门禁：`go test ./internal/store ./internal/gameserver -run 'Test(EmbeddedShopCatalog|ShopCatalog|ShopSupplement|TungusShop)' -count=1`、`go test ./...`、`go vet ./...`、`go build ./...`。
- 实机验证：PR 描述记录银币 22/2 与金币 22/3 购买通道、`txn` 记录和界面价格对齐；`kala_store_003` 的 `sm_pikeperch` 客户端本地判定限制保持不变。

## Out of Scope

- 不修复客户端 Unity 资源对 `kala_store_003/sm_pikeperch` 的本地“无库存”判定。
- 不重写所有 kind 0 商店的历史模板数据；仅清理本 PR 新建的 `craft_baits_012` 精确货架。
- 不改变除显式逐店 `shelf` 之外的其他商店解析行为，不修改购买事务或后台覆盖协议。

## Further Notes

- `[事实]` `tungus_015` 的上游快照已有 93 条静态 `shop_item`，购买失败日志对应 `tungus_bot`/`tungus_12pack` 没有任何可命中的货架行。
- `[事实]` 两个缺项的 `item_price` 分别为金币 22 和 129，基础库存为 1。
- `[事实]` 25 个待排除 SKU 均为 `box_worm_{asurv,msor,orlen,sur,volh}` 的 `20/50/h_10/h_20/h_50` 变体；它们在 `item_def`、`item_catalog`、zh_CN/en_US/ru_RU 本地化及箱体资源中均不存在。
- `[事实]` 静态库 `PRAGMA integrity_check` 为 `ok`。

# 补齐特色商店货架

Status: done

## Problem Statement

多家地图特色商店在 `server_data/static.sqlite3` 的 `shop_item` 中没有专属货架。客户端可以列出本店商品，但购买时服务端按生效货架查价失败，返回“商品不在该店生效货架”，特色商品无法购买。

## Solution

为 13 家特色商店补齐 `shop_item` 基础货架：本店独占商品使用实机显示价，模板商品沿用现有模板价格与库存。`craft_baits_012` 复制 kind 0 渔具模板时排除 25 个在 `item_def`、`item_catalog` 和客户端本地化中都不存在的失效模板 SKU，避免把不可构造的商品暴露为在售。

## User Stories

1. 作为玩家，我希望特色商店展示的商品可以按界面价格购买，以便正常获得地图专属商品。
2. 边界：没有物品定义或客户端资源的模板 SKU 不上架，不能出现“显示有货但购买失败”。
3. 边界：模板商品保留原有库存语义，不因补齐本店货架而被统一改成 9999。

## Implementation Decisions

- 修改静态库 `server_data/static.sqlite3` 的 `shop_item`，不修改表结构或迁移。
- 覆盖 `level_004_kvas`、`kala_store_003`、`milkstore_viiveri`、`sawmill_02`、`foodstore_garage`、`bakery_013`、`vakula_013`、`mill_013`、`paseka_sura`、`arbuz_store_009`、`tungus_015`、`craft_baits_012`、`varyag_012`。
- `craft_baits_012` 的基础行数为 8254：kind 0 模板 8267 行中剔除 25 个失效 SKU，再加海沙蚕、海沙蠋、虾米及其 10/20/50 箱装共 12 行。
- `sawmill_02` 的 16 行与 PR #323 内容一致；本 PR 合入后由本 PR 覆盖该修复，#323 关闭。
- 回滚方式：恢复 `server_data/static.sqlite3` 并重新运行 seed；不涉及玩家库迁移。

## Testing Decisions

- 存储层新增静态库货架完整性测试：13 家目标商店的每条 `shop_item` 都必须能关联到 `item_def` 和 `item_catalog`。回滚到本 PR 前该测试会因 `craft_baits_012` 的 25 个失效 SKU 变红。
- 门禁：`go test ./...`、`go vet ./...`、`go build ./...`。
- 实机验证：PR 描述记录银币 22/2 与金币 22/3 购买通道、`txn` 记录和界面价格对齐；`kala_store_003` 的 `sm_pikeperch` 客户端本地判定限制保持不变。

## Out of Scope

- 不修复客户端 Unity 资源对 `kala_store_003/sm_pikeperch` 的本地“无库存”判定。
- 不重写所有 kind 0 商店的历史模板数据；仅清理本 PR 新建的 `craft_baits_012` 精确货架。
- 不调整商店解析优先级、后台覆盖或购买事务逻辑。

## Further Notes

- `[事实]` 本 PR 对比当前 main 只修改 `shop_item`，新增行按店统计。
- `[事实]` 25 个待排除 SKU 均为 `box_worm_{asurv,msor,orlen,sur,volh}` 的 `20/50/h_10/h_20/h_50` 变体；它们在 `item_def`、`item_catalog`、zh_CN/en_US/ru_RU 本地化及箱体资源中均不存在。
- `[事实]` 静态库 `PRAGMA integrity_check` 为 `ok`。

# 酒类官服单瓶加成与吃饱结果码

Status: done

## Problem Statement

旧酒类规则只按啤酒/葡萄酒/烈酒/蜜酒四档给固定峰值和时长，与官服单瓶数值差异较大。
4/8 饮用后写效果记录时又只保存类别和时长，钓获结算仍按类别默认峰值与衰减窗口计算；
同时部分瓶装无酒精饮料没有进入食品恢复表，使用后不扣数量也不恢复状态。
吃饱后继续进食走通用内部错误，客户端显示错误弹窗。

## Solution

有官服实测值的酒类按单瓶峰值和游戏小时时长写入效果记录，钓获 XP 结算读取记录中的峰值和自身时长计算衰减；
未收录酒类继续回退类别档。瓶装无酒精成品饮料归入食品恢复；吃饱拒绝改用官服结果码 55。

## User Stories

1. 作为玩家，我希望喝 `tungus_vodka` 得到 10% 加成，而不是烈酒类别默认 15%。
2. 作为玩家，我希望官服 12 游戏小时/9 游戏小时的时长也决定衰减窗口，而不只是到期时间。
3. 作为玩家，我希望无酒精香槟/柠檬汽水能正常饮用并扣数量。
4. 边界：吃饱后使用食品不扣物，客户端显示正规“已吃饱”结果，而不是内部错误。

## Implementation Decisions

- 模块：`internal/gameserver` 的 `drink_rules.go`、`drink_use.go`、`drink_bonus.go`；`internal/store/repos_drink_bonus.go`。
- 数据：`drink_bonus_effect` 新增 `peak` 列，默认 0；旧行峰值 0 时结算回退原类别默认值，避免旧档行为漂移。
- 写入：4/8 饮酒使用 `RefreshDrinkBonusTxWithPeak` 同时保存峰值、时长和来源 GUID。
- 结算：`drinkXPBonusTx` 优先用记录中的 `peak + duration_millis`；`peak=0` 的旧行回退类别规则。
- 兼容：客户端基线 4.0.25034；吃饱结果码 55 已由客户端枚举实证。
- 回滚：回退代码保留新列不影响旧逻辑；删除新 spec/迁移需按迁移回滚流程处理。

## Testing Decisions

- 单元：官服峰值抽查、空白回退档、30 个 item_id 覆盖数。
- 存储：真实 SQLite 验证 `peak` 持久化并随状态读取；迁移默认值保证旧行可用。
- 行为：4/8 饮用 `tungus_vodka` 后，在最后 25% 衰减区调用 XP 结算，验证使用 10% 峰值和 22.5 分钟衰减窗口，而非烈酒类别 15%/15 分钟。
- 行为：无酒精饮料分类、吃饱结果码 55、物品不扣和恢复状态。
- 回滚后：上述峰值/时长结算与结果码断言会变红。

## Out of Scope

- 不改酒类叠加、离线计时和衰减比例本身。
- 不新增官服未提供的酒类数值。
- 不改所有消耗品的恢复量曲线。

## Further Notes

- [事实] 官服酒表 2026-11-09 存档含 28 款实测值 / 30 个 item_id；TUNGUS 取总表 10%。
- [事实] 客户端枚举 `abadjfbfdjb.lpkkndpoicm` 中 55 = `kOperationResult_PlayerFull`。
- [事实] `drink_bonus_effect` 旧表只有 category/remaining/duration/source，结算无法区分同类别不同单瓶峰值。

# 恢复精力消耗饱食度

Status: done

## Problem Statement

`advancePlayerCondition` 里饱食度只按 `HungerDecayPerHour` 随时间下降，精力恢复（`Primary`）虽然按 `factor = HungerRegenFloor + (1-HungerRegenFloor)*Secondary` 受饱食度影响，但**恢复过程本身不吃饱食度**。结果是：饱食度满的玩家站着不动，精力一路回满，饱食度却只是匀速自然衰减，两者之间只有单向关系（饱食度高 → 回得快），没有代价。

## Solution

在精力恢复之后按实际回复量折算消耗饱食度，系数由 `condition_energy_regen_food_cost` 控制，默认 0.5；置 0 即退回原行为。

## User Stories

1. 作为玩家，我希望“靠吃饱撑体力”是有代价的：食物条高时精力回得快，但食物条也会肉眼可见地掉。
2. 作为维护者，我希望这个耦合可以单独关掉（tunable 设 0），不影响既有标定曲线。
3. 边界：精力已满（`Primary` 顶到 `EnergyMax`）时不产生消耗；饱食度为 0 时不会出现负值。

## Implementation Decisions

- `internal/gameserver/player_condition.go`：
  - 新增 tunable `condition_energy_regen_food_cost` 与默认值 `defEnergyRegenFoodCost = 0.5`；
  - `conditionParams` 增加 `EnergyRegenFoodCost` 字段并在 `conditionParams()` 中读取；
  - `advancePlayerCondition` 中记录回复前的精力值，用 `gained = out.Primary - prevEnergy` 作为实际回复量，再从 `out.Secondary` 扣 `gained * EnergyRegenFoodCost`（钳到 `[0,1]`）。
- 顺序：饱食度自然衰减 → 精力恢复 → 扣饱食度 → 健康压力判定。保证同一拍里 f1 与健康判定自洽（饿到阈值以下该掉血时，同一拍就掉）。
- 系数默认 0.5：满饱食度回满一管精力大约吃掉半管食物条，与“挖掘/搏鱼消耗靠吃饭补”的既有节奏一致。
- 不新增协议字段，不改 `2/2` 下发形状。

## Testing Decisions

- `testConditionParams()` 中把 `EnergyRegenFoodCost` 显式设为 0，既有手算用例（饱食度只按自然衰减）保持不变，用来锁“关闭时无额外消耗”。
- 新增 `TestAdvanceEnergyRegenConsumesFood`：系数 0.5、饱食度 1.0、精力 0.10 推进 0.5 小时，断言 `Secondary = 1 - 0.24*0.5 - gained*0.5`。
- 新增 `TestAdvanceEnergyRegenNoFoodCostWhenDisabled`：系数 0 时饱食度只按 `HungerDecayPerHour` 掉。
- 完成前运行 `go build ./...`、`go vet ./internal/gameserver`、`go test -run TestAdvance ./internal/gameserver`、`go test ./internal/gameserver`。

## Out of Scope

不改挖饵/搏鱼的体力消耗（沿用 `bait_dig_energy_cost`、`condition_cast_energy_cost` 等既有 tunable），不改健康动态，不改进食恢复量。

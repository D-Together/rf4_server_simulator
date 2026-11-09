# 满饱食度时快速回满精力

Status: done

## Problem Statement

`defEnergyRegenPerHour = 2.0`：满饱食度（factor = 1.0）下回满一管精力（上限 0.85）需要 `0.85 / 2.0 ≈ 0.43` 小时，约 25 分钟。而挖饵一次消耗 0.15、搏鱼持续消耗，玩家一管精力打完要站着等半小时，节奏完全断裂。

## Solution

把默认精力恢复速率从 2.0/h 提到 100/h：满饱食度时 `0.85 / 100 × 3600 ≈ 30.6 秒`回满一管。“吃饱 → 快速回精力 → 再消耗”形成闭环，同时因为 `condition_energy_regen_food_cost = 0.5`，回满一管会吃掉约 0.42 食物条，消耗端仍然成立。

## User Stories

1. 作为玩家，我希望体力打空后吃顿饱饭，半分钟内就能继续挖饵/搏鱼，而不是站着干等。
2. 作为维护者，我希望这个节奏仍由 tunable 控制，各服可以自己调回慢速。
3. 边界：饱食度为 0 时 `factor = HungerRegenFloor`（0.25），恢复速度随之降到 25/h，仍然比其他状态慢——“吃饱”依然是快速恢复的前提。

## Implementation Decisions

- `internal/gameserver/player_condition.go`：`defEnergyRegenPerHour` 由 2.0 改为 100.0，并补注释说明换算（0.85 / 100 × 3600 ≈ 30.6 秒）。
- 速率仍受 `factor = HungerRegenFloor + (1-HungerRegenFloor)*Secondary` 调制，饱食度掉下去恢复自然变慢，不需要额外分支。
- 不新增 tunable：`condition_energy_regen_per_hour` 已存在，覆盖默认值即可；本次只改标定默认。
- 不动 `defEnergyRegenFoodCost`（仍 0.5），保证“回得快”和“吃得多”同时成立。

## Testing Decisions

- 既有 `TestAdvance*` 用例使用 `testConditionParams()`（显式 `EnergyRegenPerHour = 0.5`），不依赖默认值，改默认后仍然通过。
- 手算校验：默认参数、饱食度 1.0、精力 0 推进 1 秒 → `Primary ≈ 100/3600 ≈ 0.02778`，与 30.6 秒回满一致。
- 完成前运行 `go build ./...`、`go vet ./internal/gameserver`、`go test -run 'TestAdvance|TestCondition' ./internal/gameserver`。

## Out of Scope

不改食物/咖啡/可可的即时恢复量，不改 `condition_energy_regen_food_cost`，不改挖饵/搏鱼的消耗标定。

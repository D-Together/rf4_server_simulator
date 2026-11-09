# 玩家精力影响鱼体力消耗速度

Status: done

## Problem Statement

搏鱼时鱼的体力（14/14 的 `f5` 体力池）完全由鱼种与体重决定，与玩家状态无关：玩家精力见底时，鱼还是按同样的池子消耗，拉起来和满精力时一样快。玩家精力只有“抬杆扣 0.005”这么一点存在感，两者之间没有体感上的关联。

## Solution

在 14/14 下发前按玩家当前精力放大鱼体力池：

```text
精力满（>=1）       不放大
精力 0              放大到 (1 + penalty) 倍
中间                线性
```

池子变大等价于客户端模拟时鱼“更耐拉、体力掉得更慢”，从而让“人累了、鱼更难拉”有实际反馈。

## User Stories

1. 作为玩家，我希望精力见底时搏鱼明显更吃力（鱼更耐拉），而不是毫无差别。
2. 作为玩家，我希望精力满时和以前完全一致，不影响既有手感。
3. 作为维护者，我希望这个耦合有开关与系数（tunable），可以关掉。

## Implementation Decisions

- `internal/gameserver/fishing.go` 新增 tunable `fish_stamina_energy_penalty`（默认 **1.0**，即精力见底时池子翻倍；设 0 关闭）与纯函数 `scaleFishStaminaForEnergy(base, energy, penalty)`。
- 接入点：`Push14_14` 计算 `stamina` 之后、写日志之前，用 `currentPlayerCondition(ctx, uid).Primary` 作为精力输入。精力 >= 1 或系数为 0 时原样返回；精力 < 0 按 0 处理。
- 只在状态机开关 `player_condition_enabled` 打开时生效，与既有状态条体系一致。
- 只放大 `f5`（体力池），不动 `f6` 初始体力比例与 `f8` 拉力，避免同时改变手感曲线导致不可预期。
- 不下发新字段，14/14 形状不变。

## Testing Decisions

- 新增 `fish_energy_scale_test.go` 覆盖纯函数六个分支：满精力不缩放、半精力 ×1.5、空精力 ×2、系数 0 关闭、精力 >1 钳制、精力 <0 钳制。
- 完成前运行 `go build ./...`、`go vet ./internal/gameserver`、`go test -run 'TestScaleFishStaminaForEnergy' ./internal/gameserver`。

## Out of Scope

不改鱼体力池的基线公式（`fishStaminaForProfile`）、不改拉力与冲刺倍率、不改 14/14 之外的下发路径，也不做“玩家精力影响收线速度”这类额外耦合。

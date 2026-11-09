# 断主线按实际放线量扣线

Status: done

## Problem Statement

`lineLossTx` 在“断主线”分支里无条件清杯：

```go
if policy.destroyLine {
    // 官服清杯口径：主线被拉断时整条丢失（含挂线配件）。
    lossLength = beforeLength     // 整卷
}
```

实战中客户端断主线时通常只放出十几米（贴脸崩断、锁轮瞬间断），整卷 1000+ 米的线一起判死，玩家要重新买整卷——惩罚与事实不符。

## Solution

新增 tunable `line_break_loss_m`（米）：

```text
> 0   断主线按该米数扣线，剩余长度保留在杯上
= 0   保持上游清杯口径（默认，行为不变）
```

实现位于 `lineLossTx` 的清杯分支内，只影响 `destroyLine=true`（17/9 断主线），剪线（17/10，按上报米数扣）与断子线不受影响。

## User Stories

1. 作为玩家，锁轮崩断主线时只损失实际放出的那段线，杯上剩余的线还能继续用。
2. 作为维护者，希望默认行为与上游完全一致，需要时用一个 tunable 打开。
3. 边界：扣减量仍受 `beforeLength` 上限约束（线不足时按线长封顶，不会出现负数长度）。

## Implementation Decisions

- `internal/gameserver/gear_damage.go`：`lineLossTx` 的清杯分支里读取 `line_break_loss_m`，命中则用固定米数替换 `beforeLength`。
- 仍走“主线长度字段更新 + 线耗尽删除”的既有路径：只有 `afterLength <= 0` 才删除主线并清 f3 引用，所以保留线长时主线物品继续存在。
- 终端件（子线、钩、饵）照旧随断点全部移除——这段逻辑按 `mainLineLossCategories` 走，与扣多少米无关。
- 不新增协议字段，不改客户端行为。

## Testing Decisions

- `go test -run 'TestBreak|TestGearBreak' ./internal/gameserver` 覆盖断线/断子线/断竿三条分支，确认 tunable 默认 0 时行为与上游一致。
- 手算校验：主线 1184 米、`line_break_loss_m=20` → `line_loss_m=20`、`afterLength=1164`，主线物品保留。
- 完成前跑 `go build ./...`、`go vet ./internal/gameserver`、`go test ./internal/gameserver ./internal/store ./internal/proto`。

## Out of Scope

不改剪线（17/10）的按米扣逻辑，不改断子线（不动主线长度），不改终端件丢失集合，也不引入“按客户端上报放线量精确扣线”（17/9 请求里没有该字段）。

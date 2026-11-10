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

两级取值，越具体越优先：

```text
17/9 body 偏移 0x3f 的 f32 > 0   按客户端上报的实际放线米数扣（取值上限 5000 米）
line_break_loss_m > 0           按固定米数扣
两者都没有                      保持上游清杯口径（默认，行为不变）
```

只影响 `destroyLine=true`（17/9 断主线）；剪线（17/10，按上报米数扣）与断子线不受影响。

## User Stories

1. 作为玩家，锁轮崩断主线时只损失实际放出的那段线，杯上剩余的线还能继续用。
2. 作为维护者，希望默认行为与上游完全一致，需要时用一个 tunable 打开。
3. 边界：扣减量受 `beforeLength` 上限约束——上报超过剩余线长时按线长封顶并走线耗尽删除，不会出现负数长度。

## Implementation Decisions

- 协议层 `protocol_dynamic_handlers.go` 新增 `rodBreakLineOutM(body []byte) float64`：读取 body 偏移 `0x3f` 的 little-endian float32，`<= 0`、`> 5000` 或 NaN 一律回 0（视为未上报）。
- 该偏移是实测结论（客户端 4.0.25034）：把真实 17/9 请求体按 little-endian 解出的 float32 与客户端显示的放线米数一致。
- 参数贯通：`breakLineOrRod(ctx, uid, rodGUID, reportedGUID, lineOutM)` → `breakLineTx(..., lineOutM)` → `lineLossPolicy.lineOutM`，只有断主线的清杯分支读它。
- `internal/gameserver/gear_damage.go`：清杯分支先看 `policy.lineOutM > 0`，否则读 tunable `line_break_loss_m`，两者都无效才保留 `beforeLength` 的整卷清杯。
- 仍走“主线长度字段更新 + 线耗尽删除”的既有路径：只有 `afterLength <= 0` 才删除主线并清 f3 引用，所以保留线长时主线物品继续存在。
- 终端件（子线、钩、饵）照旧随断点全部移除——这段逻辑按 `mainLineLossCategories` 走，与扣多少米无关。
- 不新增协议字段（只读请求体里已有的字节），不改客户端行为。

## Testing Decisions

- 新增 `line_break_loss_test.go`：
  - `TestRodBreakLineOutM` 覆盖解析边界：body 过短、0、负数、超 5000、正常值。
  - `TestMainLineBreakLosesReportedLineOutMeters`：50 米线 + 上报 5 米 → 剩 45 米，主线物品保留。
  - `TestMainLineBreakReportedMetersClampToLineLength`：50 米线 + 上报 200 米 → 封顶扣光，主线删除。
  - `TestMainLineBreakFallsBackToTunableMeters`：无上报 + `line_break_loss_m=20` → 剩 30 米。
- 既有 `TestMainLineBreakLosesExactlyFiveMeters` 覆盖默认清杯口径不变（无上报、无 tunable）。
- 手算校验：主线 1184 米、`line_break_loss_m=20` → `line_loss_m=20`、`afterLength=1164`，主线物品保留。
- 完成前跑 `go build ./...`、`go vet ./internal/gameserver`、`go test -run 'Break|Line|Gear|Reel' ./internal/gameserver`。

## Out of Scope

不改剪线（17/10）的按米扣逻辑，不改断子线（不动主线长度），不改终端件丢失集合，也不解析 17/9 body 里 `0x3f` 以外的字段。

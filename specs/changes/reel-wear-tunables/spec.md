# 摩擦片磨损参数后台可配

Status: done

## Problem Statement

泄力打滑与锁轮过载烧蚀是摩擦片寿命的两条唯一通道，但两者的关键系数在代码里是硬编码：

- `reel_performance.go:17-19` 的档位磨损率（`reelSlipWear` / `reelSlipWearLow` / `reelSlipWearMax`）与
  `reelSlipWearForSetting` 里 25~29 档的 5 个字面量；
- `reel_fight_wear.go:41` 的烧蚀校准点 `reelBurnCalibrationDragKg = 7.7`。

运维侧观察到实战轮摩擦片在约 67 条渔获、36 小时内磨到 18.8%，但无法在不改代码、不发版的前提下调整，
也无法验证「调大/调小会怎样」。已有的 `reel_load_tick_seconds`、`reel_locked_burn_base_s`、
`reel_locked_burn_curve`、`reel_cast_base_factor_lure` 四个 tunable 同样没有暴露到后台。

## Solution

把两条通道的全部系数收进既有 tunables 机制（`s.tunableF`，缓存 30s，后台保存后推送
`reload_tunables` 立即生效），并在后台「游戏参数」页新增「摩擦片磨损」分组。

新增 tunable（键名与默认值 = 现行硬编码值，默认行为完全不变）：

| 键 | 默认 | 含义 |
|---|---|---|
| `reel_slip_wear_low` | 0.000001 | ≤24 档单次打滑寿命损耗 |
| `reel_slip_wear_s25` | 0.000025 | 25 档 |
| `reel_slip_wear_s26` | 0.00005 | 26 档 |
| `reel_slip_wear_s27` | 0.000075 | 27 档 |
| `reel_slip_wear_s28` | 0.000095 | 28 档 |
| `reel_slip_wear_max` | 0.0001 | 29 档（锁轮边界与诊断口径） |
| `reel_slip_calibration_seconds` | 1.8 | 单次基准换算成每秒磨损率的校准秒数 |
| `reel_burn_calibration_drag_kg` | 7.7 | 烧蚀强度系数的校准制动力 |

同时把已有的四个 reel tunable 补进后台 UI（`reel_load_tick_seconds`、
`reel_load_max_tick_seconds`、`reel_locked_burn_base_s`、`reel_locked_burn_curve`、
`reel_cast_base_factor_lure`）。

## User Stories

1. 作为服主，我希望在后台直接调整摩擦片被磨掉的速度，以便把「一晚上磨穿一块片」调到可持续。
2. 作为服主，我希望分别控制「低档位几乎不磨」与「高档位明显磨损」的斜率，而不是只能整体缩放。
3. 作为服主，我希望改完最多 30 秒生效，不需要重启服务端或让玩家重登。
4. 边界：tunable 缺失、≤0 或非数值时回落到代码默认值，行为与改动前逐字节一致。
5. 边界：参数只影响后续结算，已落库的磨损进度不追溯。

## Implementation Decisions

- `reel_performance.go`：`reelSlipWearForSetting` 改为 `(s *Server)` 方法，逐档读 tunable；
  默认值常量化保留为 `defaultReelSlipWear*`。`reelSlipWear`（旧直接入口的基准）保留常量语义，
  但 29 档改为读 `reel_slip_wear_max`。
- `reel_fight_wear.go`：`reelSlipCalibrationSeconds`、`reelBurnCalibrationDragKg` 改为
  `default*` 常量 + 读取处走 tunable；`fightReelTickWear` 的 `slipRate` 同步改为读 tunable。
- 读取口径统一走已有 `s.tunableF`，不新增缓存层、不新增表、不新增迁移。
- 后台 `handleTunableSet` 为这些键加**区间校验** `[min, max]`（`reelWearTunableRanges`）。
  下限不是「>0 即可」：磨损率直接乘进制动力，而制动力是 38.1kg 量级，float64 在该量级的
  半数舍入门槛约 3.553e-15 kg；单次减量小于它时 `cur - wear` 仍等于 `cur`，参数看着生效、
  实际一分不磨，属于静默失效。反算理论最小档位率约 8.39e-17，取 `1e-12`（约 1.2 万倍余量）
  作为硬下限；其余参数按各自语义定下限（除数类 0.01、指数类 0.1、折扣类 0.001）。
- 前端 `TUNABLE_DEFS` 增加条目；参数按「谁在用、什么时候用」分成三个子页
  （玩法与钓组 / 收益与通知 / 限时活动），全量渲染 + Tab 切显隐，切换不重新请求。
- 默认值展示从输入框 placeholder 移到说明行：placeholder 只做空状态提示，
  默认值写进 placeholder 会在 112px 输入框里被截断（`0.000001` 显示成 `0.00000`）。
- 回滚方式：把所有键设回默认值即等效回滚；代码层 revert 本提交。

## Testing Decisions

- 单元测试：覆盖每个档位读取 tunable、tunable 缺失回落默认、≤0 回落默认、
  校准秒数与校准制动力生效、烧蚀曲线随参数变化。
- 期望值来自手算：例 29 档默认 `0.0001 / 1.8 = 0.005556%/s`；把
  `reel_slip_wear_max` 设成 `0.00005` 后同样时长磨损减半。
- 回滚后测试变红。

## Out of Scope

- 不改摩擦片的磨损模型本身（仍是「打滑秒数 × 档位率 + 锁轮过载曲线」）。
- 不改每竿基准与泡水的消耗矩阵（`reelPartWearMatrix`）。
- 不改客户端显示口径——客户端摩擦片制动力读本地静态表，服务端改数值不会反映到面板。
- 不做按玩家/按轮子的独立配置。

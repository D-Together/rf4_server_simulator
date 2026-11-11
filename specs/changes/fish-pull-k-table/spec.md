# 鱼拉力逐鱼种 K 表 + 搏鱼曲线纯 uniform 缩放

Status: in-progress

## Problem Statement

官服 14/14 抓包 21+8 条 A 级样本对拍证实模拟器搏鱼参数两处系统性错误
（完整样本表与反推见「Further Notes」，分析快照 2026-11-11）：

1. **27.f8 拉力全线偏低 14–49%**（均值 −28%，无一例外）。
   官方 `f8 = 体重kg × 逐鱼种 k`，k ∈ [12.6, 20.6] N/kg、种内随体重 +0.3~0.35·ln(w_kg)
   缓升、同种多样本稳定 ±3%。模拟器用常数 `verifiedForcePerGram = 0.00980665` N/g
   （fishing.go:595）× archetype `ForceScale`（fish_fight_profile.go 猜测系数 0.84–1.34），
   等效 k 仅 8.2–13.1。
2. **27.f21 曲线多了官方不存在的逐阶段乘数**（`fightPhaseTensionScale`：
   302 下潜×1.78、105 主搏×0.30、306 翻腾×0.42、101×1.66 等；
   `fightPhaseDurationScale` 同理）。决定性反证：官方每鱼曲线 = 鱼种池 × 单一
   uniform scale 逐行精确成立（perch221=池×0.19375 / crucian293=×1.47233 /
   crucian743=×3.8062 / c.roach680=×4.6066），张力列外的列不随体重缩放
   （duration 恒等于池值；amp 列另有独立因子 ~1.3，形式未定，本期不动）。
   自造层把主搏段压到官方 22%、下潜抬到 132%。

## Solution

### f8：逐鱼种 K 表（已测种精确 + 未测种习性组 v1）

`pull_N = w_kg × (K0(species) + slope·ln(w_kg))`，slope 默认 0.35（tunable
`fish_pull_ln_slope`）。K0 查表顺序：实测种 → 习性组 → fallback 17.5。

实测种 K0（=1kg 基值，由 29 条 A 级样本按 `k0 = k − 0.35·ln(w_kg)` 反推，
样本明细见「Further Notes」）：

| species | K0 | species | K0 |
|---|---|---|---|
| merlang | 21.1 | din_l_carp | 16.9 |
| b_gill | 20.4 | carp | 16.5 |
| taran | 18.8 | perch | 17.6 |
| g_carp | 18.5 | c.roach | 16.4 |
| piksha | 18.3 | crucian | 15.05 |
| b_carp | 18.0 | c.carp | 13.7 |
| n.pike | 17.3 | s.bream | 13.0 |

习性组 K0（未测种，纯推断 ±1.5）：鲑科 18.0 / 鲟科 17.0 / 鲶科 16.0 /
比目鱼 16.0；无组归属 fallback 17.5。鱼种→组映射覆盖鱼谱高频种；
`tunable fish_pull_fallback_k`（默认 17.5）兜底。

- `profile.ForceScale` 从 f8 路径移除（官方 k 无 archetype 形状）。
- `RolledFish.Force` 手动覆盖与 `fight_power_scale` tunable 语义保留。
- `verifiedForcePerGram` 常量保留但不再进入 f8 主路径（其 B 级来源与全部
  A 级样本矛盾，`newtonPerKgf = 9.80665` 恰为其 ×1000，数值不变）。

### f21：纯 uniform 缩放

`buildFightCurvesWithProfile` 只保留 `scale = pull / 池首行f8`：

- f8/f9（张力列）× scale；
- f2/f3（duration 列）不再乘任何系数（官方不随体重/阶段变）；
- 其余列维持池原值；f15–f18 int 列照旧；
- 删除 `fightPhaseTensionScale` / `fightPhaseDurationScale` 及
  profile 的 DurationScale/RestScale/Burst/ForceScale 字段（StaminaScale/SizeRatio
  保留，f5 仍用 archetype，其逐鱼种表属 P2）。

## User Stories

- 玩家溜同一条官方克重的鱼，杆弯量/泄力报警/线载显示与官服同量级（力 ±3%）。
- 主搏段（105）冲刺强度恢复官方量级（现被压到 22%），下潜（302）不再异常硬。
- 空竿时长不变（本 spec 不动咬钩/发鱼）。

## Implementation Decisions

- 模块：`internal/gameserver`（fishing.go 拉力口径、serverdb.go 曲线池装配、
  fish_fight_profile.go 删除自造缩放层、reel_fight_wear.go / statsbuild.go 结算随动）；
  `internal/admin/web/index.html` 登记新 tunable（「玩法手感」分组可直接改）。
- 接口/字段：`27.f8`（牛）与 `27.f21`（曲线行 19 列）的响应形状与字段 op 不变，
  只改取值口径；无新命令。
- 数据与迁移：无表结构变更。应急覆盖走既有 tunables：
  `fish_pull_ln_slope`（默认 0.35）、`fish_pull_fallback_k`（默认 17.5）、
  `fight_power_scale`（整体倍率，保留）。
- 兼容：客户端基线 `4.0.25034`；f8/f21 数值回归官方抓包口径（依据即本 spec
  「Further Notes」样本表），回归场景见 Tests。回滚 = 还原本提交，无状态残留。

## Non-Goals

- f5/f6 逐鱼种表（P2）；曲线池直配（P2）；amp 列独立缩放因子（待样本）；
  f16/f20（P3）；玩家耐力（独立能力项）。

## Tests

- `TestFishPullMatchesOfficialAnchors`（新增）：29 条 A 级样本钉死，全部落
  ±2~3%（样本表见「Further Notes」）。
- `TestFishPullScalesWithSpeciesAndWeight`：k 表单调性（同种体重大→拉力大、
  掠食者高于静水小鲤、海水鳕科最高）；未测种走组表/fallback 不为 0。
- `TestFishPullGroupFallback` / `TestFishPullLnSlopeTunable`（新增）：习性组与
  fallback 的 K0 取值；`fish_pull_ln_slope` / `fish_pull_fallback_k` 应急覆盖生效。
- `TestFish27UsesVerifiedFightFields` 重写：删 B 级 53g/63g 严格断言，改 k 表口径。
- `TestFightCurvesKeepSprintTensionStages`：uniform 后张力包络改为 crucian 池口径
  0.45×pull ≤ min、max ≤ 1.35×pull 且跨过 pull（官方 crucian 实测 min≈0.49×pull）；
  阶段存在性断言保留。
- `TestFightCurvesUniformEnvelope`（新增）：不同 pull 下逐行张力/pull 恒等，
  锁定纯 uniform 不变式。
- 既有 `reel_fight_wear` / `reel_wear_*` / `fishing_persist` 等引用 pull 的测试随
  新口径修正期望值。
- 回滚本提交后：以上新增/重写用例与全部锚点断言变红。

## Risks

- 力整体 +28%、105 段 ×4.5：实机可感知，属官方口径修复。需实机 A/B
  （同杆钓鲫鱼/草鱼：泄力报警时机、断件频率）。
- 结算侧 `fishFightPullKgfFor`（= pull / 9.80665）随 f8 上移，泄力比较基准同步
  变严——回归断线判定相关测试。
- f5 未动：鱼更强但体力池没调，搏鱼观感更费力，属预期（P2 修 f5）。

## Further Notes

- [事实] 29 条 A 级锚点样本（species / weight_g / 官方 f8），与
  `fish_pull_k_table_test.go` 的数值锚点一一对应；按
  `k0 = f8/w_kg − 0.35·ln(w_kg)` 反推，同种各样本 K0 落 ±3% 内，
  按种取均值即 Solution 中的 K0 表：

| species | weight_g | f8 (N) | 反推 K0 |
|---|---|---|---|
| crucian | 611 | 9.0919 | 15.05 |
| crucian | 293 | 4.2863 | 15.06 |
| crucian | 743 | 11.1182 | 15.07 |
| crucian | 193 | 2.7937 | 15.05 |
| c.roach | 680 | 11.0735 | 16.42 |
| c.roach | 150 | 2.3899 | 16.60 |
| c.roach | 713 | 11.6109 | 16.40 |
| perch | 221 | 3.7476 | 17.49 |
| perch | 93 | 1.5575 | 17.58 |
| perch | 1070 | 18.9743 | 17.71 |
| perch | 119 | 1.9980 | 17.54 |
| perch | 342 | 5.8674 | 17.53 |
| perch | 642 | 11.1688 | 17.55 |
| g_carp | 21742 | 423.7980 | 18.41 |
| g_carp | 4880 | 93.4397 | 18.59 |
| g_carp | 5484 | 105.0730 | 18.56 |
| g_carp | 5716 | 109.5450 | 18.55 |
| g_carp | 10307 | 198.4960 | 18.44 |
| b_carp | 7828 | 146.5190 | 18.00 |
| carp | 1063 | 17.5527 | 16.49 |
| taran | 611 | 11.3884 | 18.81 |
| piksha | 2975 | 55.4510 | 18.26 |
| merlang | 252 | 5.1914 | 21.08 |
| b_gill | 113 | 2.2171 | 20.38 |
| n.pike | 4305 | 76.6636 | 17.30 |
| din_l_carp | 1715 | 29.3737 | 16.94 |
| c.carp | 655 | 8.9112 | 13.75 |
| c.carp | 140 | 1.8186 | 13.68 |
| s.bream | 359 | 4.5193 | 12.95 |

- [事实] uniform 缩放：官方每鱼曲线 = 鱼种池 × 单一 scale 逐行精确成立
  （perch221=×0.19375 / crucian293=×1.47233 / crucian743=×3.8062 /
  c.roach680=×4.6066，张力列外的列不随体重缩放）。
- [推断] 未测种习性组 K0：按已测梯度（同科体型单调、海水/洄游高于静水）外推，
  纯估计组 ±1.5。
- [假设] slope=0.35 种内通用：perch/crucian/g_carp 多重量级样本钉死，其余种
  样本量不足以独立验证斜率。
- amp 列存在 ~1.3 独立因子，形式未定，未验证，本期不动。

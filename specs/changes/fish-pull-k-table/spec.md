# 鱼拉力逐鱼种 K 表 + 搏鱼曲线纯 uniform 缩放

Status: in-progress

## Problem Statement

官服 14/14 抓包 21+8 条 A 级样本对拍证实模拟器搏鱼参数两处系统性错误
（证据与推导见 `docs/根据实测包的鱼拉力修改后续建议.md`，分析快照 2026-11-11）：

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

实测种 K0（=1kg 基值，由 31 条 A 级样本按 `k0 = k − 0.35·ln(w_kg)` 反推）：

| species | K0 | species | K0 |
|---|---|---|---|
| merlang | 21.1 | din_l_carp | 16.9 |
| b_gill | 20.4 | carp | 16.5 |
| taran | 18.8 | perch | 17.6 |
| g_carp | 18.5 | c.roach | 16.4 |
| piksha | 18.3 | crucian | 15.05 |
| b_carp | 18.0 | c.carp | 13.7 |
| n.pike | 17.3 | s.bream | 13.0 |

习性组 K0（未测种，依据同上文档 §6）：鲑科 18.0 / 鲟科 17.0 / 鲶科 16.0 /
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

## Non-Goals

- f5/f6 逐鱼种表（P2）；曲线池直配（P2）；amp 列独立缩放因子（待样本）；
  f16/f20（P3）；玩家耐力（独立能力项）。

## Tests

- `TestFishPullKTableAgainstOfficial`（新增）：A 级样本钉死——crucian 611g→9.09N、
  perch 221g→3.75N、g_carp 21742g→423.8N、merlang 252g→5.19N（±2%）；
  同种体重大→拉力大；未测种走组表/fallback。
- `TestFish27UsesVerifiedFightFields` 重写：删 B 级 53g/63g 严格断言，改 k 表口径。
- `TestFightCurvesKeepSprintTensionStages`：uniform 后张力包络改为
  0.45×pull ≤ min、max ≥ 1.15×pull（官方 crucian 实测 min≈0.49×pull）；
  阶段存在性断言保留。
- `TestFishFightProfilesUseSpeciesAndWeight` 重写为 k 表单调性 + 曲线 uniform 包络。
- 既有 `reel_fight_wear` / `reel_wear_*` / `fishing_persist` 等引用 pull 的测试随
  新口径修正期望值。

## Risks

- 力整体 +28%、105 段 ×4.5：实机可感知，属官方口径修复。需实机 A/B
  （同杆钓鲫鱼/草鱼：泄力报警时机、断件频率）。
- 结算侧 `fishFightPullKgfFor`（= pull / 9.80665）随 f8 上移，泄力比较基准同步
  变严——回归断线判定相关测试。
- f5 未动：鱼更强但体力池没调，搏鱼观感更费力，属预期（P2 修 f5）。

# 发鱼模式三选一：新增"两档制"（底钓/浮钓 日常/打窝）

Status: in-progress

## Problem Statement

底钓和浮钓玩家在鱼猎路径下的一杆渔获由四阶段（日常/聚鱼/发窝/巨物发窝）权重表决定，
其中"日常"阶段 74.7% 都是未达标小鱼，奖杯只有 1%。管理员希望淡水（底钓/浮子）只有
两个档位节奏——**日常**（未达标 10%/合格 86.5%/奖杯 3%/蓝冠 0.5%）与**打窝**
（1%/92%/6%/1%）——并且百分比能在后台直接调，不用重编译；路亚、海钓的四阶段
行为保持不变。

## Solution

管理后台的档位判定改为**三选一模式开关**：档位比例（现有滑杆）、鱼猎四阶段（现有
鱼猎）、两档制（新增）。选中两档制时，底钓/浮钓按上面两组百分比出鱼，打窝进度
（窝料聚鱼度、守窝持续度、奖杯保底、脱钩补偿）沿用现有鱼猎状态机；路亚和海钓在
三种模式下都保持现有四阶段。两组百分比在后台用滑杆调整，保存时自动归一到 100%。

## User Stories

1. 作为管理员，我希望在后台三选一切换发鱼模式，以便不改代码就能在档位比例/鱼猎四阶段/两档制之间切换。
2. 作为管理员，我希望在两档制模式下用滑杆调整日常与打窝的四档百分比，保存后最多 30 秒生效。
3. 作为底钓/浮钓玩家，我希望日常阶段以合格鱼为主（86.5%），打窝后进一步压低小鱼并抬高奖杯与蓝冠。
4. 边界：两档制只作用于淡水底钓/浮钓分支（含飞钓、哈里跳这两个落在同分支的钓组）；海钓、路亚在任何模式下都是现有四阶段。
5. 边界：打窝判定沿用现有门槛（窝料聚鱼度 A<0.25、鱼口匹配 E<0.45、窝料匹配 G<0.25 任一不满足即日常）；昼夜门槛保留——白天钓夜行鱼仍是日常。
6. 边界：奖杯保底（打窝阶段 30 口未出奖杯 w3×1.5、50 口 ×2、60 口保底奖杯）与脱钩回访补偿照旧生效。
7. 边界：三模式互斥；选某一模式时后台原子写入三个开关。直改数据库把三个开关全设 0 会回到旧的体重采样路径（非本次功能，仅作说明）。

## Implementation Decisions

- 模式互斥由三个 tunable 表达：`grade_ratio_enabled`、`fish_hunt_enabled`、新增
  `two_stage_hunt_enabled`（0/1，默认 0）。两档制模式下 `fish_hunt_enabled=0`，因此
  鱼猎状态机入口用合并门 `huntStateEnabled() = fishHuntEnabled() || twoStageEnabled()`，
  覆盖 `internal/gameserver` 中所有 `fishHuntEnabled` 早退点（loadFishHuntState、
  recordHuntBite/Catch/Escape、huntAnchorGroundbait、rollFishFromDBWithRules 的鱼猎
  分支）。合并门只扩权不缩权，档位比例与鱼猎两模式的行为零变化。
- 权重读取：`fishHuntWeights` 增加 mode 参数（`fishHuntReadiness` 增加 Mode 字段，
  分派器按淡水分支填写）；淡水+两档制时 `stage==fishHuntDaily` 用日常表、其余阶段
  用打窝表；海钓/路亚与淡水鱼猎模式维持现有四张表。pity 倍率块（stage>daily）不动。
- 8 个权重 tunable：`two_stage_daily_below/qualified/trophy/super`（默认 10/86.5/3/0.5）、
  `two_stage_baited_below/qualified/trophy/super`（默认 1/92/6/1）；服务端读取后按
  非零档归一（沿用 grade_pct_ 的归一思路），负值按 0 处理。
- 后台新增 `GET/POST /api/fish/spawn_mode`：POST 单事务写三个开关（档位比例/鱼猎/
  两档制）并审计、通知主服刷新；GET 按三键现值推导当前模式（全 0 → legacy）。
  `fish_hunt_enabled`、`two_stage_hunt_enabled` 加入 0/1 白名单。
- 不改表结构、不改协议（RolledFish.HuntStage 只存在于服务端快照 JSON，不进 27 载荷）；
  旧档兼容：历史快照里的 Stage=2/3 在两档制下按 `>= fishHuntGather` 命中打窝表，无需迁移。
- 回滚：后台切回"档位比例"或"鱼猎四阶段"即恢复原行为，无需回滚代码。

## Testing Decisions

- 单元层：`fishHuntWeights` 纯函数直调——淡水两档表（tunable 默认值 + 自定义值归一）、
  海钓/路亚四表逐字面量断言（74.7/24/1/0.3 等原值）、twoStage 下 pity 倍率作用于打窝表、
  grade_ratio 开启时两档制不生效。
- API 层：spawn_mode POST 三键原子写入 + 非法 mode 拒绝 + 审计；0/1 白名单校验。
- 行为层：实机回归——两档制下浮钓不打窝（日志 stage=日常）、打窝后（stage=打窝）、
  海钓/路亚各一竿（stage=聚鱼等原名称）、三模式往返切换。
- 期望值来源：本 spec 里的百分比字面量与现有四表字面量，不是"用同一份代码再算一遍"。
- 回滚验证：把 `two_stage_hunt_enabled` 切 0、`fish_hunt_enabled` 切 1 后，
  TestFishHuntStageWeights 的四表断言（按模式参数）仍绿。

## Out of Scope

- 海钓、路亚的阶段判定与权重（任何模式下都不改）。
- 淡水 readiness 的阶段门槛（A/E/G 阈值）。
- 嘬饵/保持率、钩型软匹配的语义（钩型软匹配仍按 strength 作用于两档表）。
- 档位比例滑杆的机制（保留现状，仅在模式 UI 中作为选项之一）。
- 带日期的历史分析文档（analysis-*.md）不回改。

## Further Notes

- `[事实]` RolledFish.HuntStage 不进客户端协议，只入服务端 pending 快照
  （fishing_persist.go / repos_fishing_state.go JSON hunt_stage），改档位不影响客户端。
- `[事实]` 淡水分支 = 底钓∪浮子∪rig33 飞钓∪rig35 哈里跳（rigmeta.go 集合定义 +
  fishHuntReadiness 分派），这是"底钓和浮子"在代码里唯一干净的边界。
- `[假设]` 三 tunable 组合的互斥由后台 UI 保证；直改数据库造成的矛盾组合以
  grade_ratio > two_stage > fish_hunt 的优先级解释（grade_ratio 分支最先判断，
  twoStage 只在淡水权重分支生效，fish_hunt 保持其余路径）。
- 未验证：两档百分比在实机长周期下的手感，待管理员实测后再调滑杆。

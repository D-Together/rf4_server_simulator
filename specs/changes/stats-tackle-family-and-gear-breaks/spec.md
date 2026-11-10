# 统计页记录卡按钓法分族，并修正饵料/拟饵聚合错配

Status: done

## Problem Statement

玩家信息「统计」页存在三处与真实钓法不符的错误：

1. **最大渔获(浮子)错源。** f14 取「非拟饵中最重」，导致海钓渔获被算进浮钓纪录。
   例：挪威海用海钓饵钓上的绿青鳕（`fb_bf_saida`，物品品类 27）出现在「浮子钓具」卡上。
   浮子钓具不可能钓到该鱼。
2. **浮钓/路亚/底钓三卡无法区分。** `catch_log` 没有钓法快照，`statsbuild.go` 只能用
   「饵是不是拟饵（品类 17）」二选一，浮钓、底钓、海钓全部塌进「非拟饵」，
   连带 f17 竿类、f18 钓组聚合一起失真。
3. **饵料统计与拟饵统计串味。** 321 的 f19 是「饵料统计」、f21 是「拟饵统计」，两者元素类
   不同，但实现把全部钓饵都写进了 f21，天然饵（蚯蚓、蛆、鱼片）被当作拟饵显示。

## Solution

1. `catch_log` 增加 `tackle_cat`（钓法大类快照），取值与技能大类同源：
   1=浮钓 2=路亚 3=底钓 11=海钓，0=未知。入护与结算恢复时写入。
2. 迁移回填历史行：按 `kv` 中已存在的结算回执 `catch-settlement:<uid>:<catch_guid>`
   的 `SkillID` 回填；无回执的行保持 0，不猜测。
3. 321 的 f14/f15/f16 与 f17/f18 聚合按 `tackle_cat` 分族，四族互斥；
   `tackle_cat=0` 的历史行回退「拟饵→路亚、其余→浮钓」旧口径，保证老档仍有卡片可看。
4. f19 使用 318 `gconafdfchk`（`{systemID, 计数}`，容器 args 127）承载天然饵；
   f21 使用 319 `keoejejkenf`（`{品类, systemID, 字符串, 计数}`，容器 args 128）承载拟饵。

## User Stories

1. 作为玩家，浮钓纪录卡只应显示用浮子钓具钓到的鱼，海钓渔获不得混入。
2. 作为玩家，路亚与底钓应各自有独立的纪录卡，而不是共用一张。
3. 作为玩家，饵料统计里应看到天然饵，拟饵统计里应看到拟饵，两者不混。
4. 边界：历史存档没有钓法快照时回退旧口径，面板不出现空卡或报错。

## Implementation Decisions

- 迁移 `migrations/00055_catch_tackle_cat.sql`：新增 `tackle_cat INTEGER NOT NULL DEFAULT 0`
  与回填语句，`IF NOT EXISTS`/幂等，可重复执行。
- `store.KeepnetEntry.TackleCat` 随入护落库；`CatchLogInsertTackle` 与 `CatchLogInsert`
  并存，后者保持兼容（tackleCat=0）。
- `statsbuild.go`：`catchStatRow.TackleCat`；`tackleFamilyOf` 为唯一钓法归属入口
  （快照优先，历史行回退）；`tackleCatForRig` 复用 `fishingSkillCatForRig`，
  保证统计页与技能页口径一致。
- 海钓渔获只参与 biggest/strongest/max_xp/max_silver 等不分钓法的卡，不进三张钓法卡。
- 兼容：客户端基线 4.0.25034；不新增命令、不改 args code 之外的协议形状。

## Testing Decisions

- `TestFishStats321TackleFamily`：夹具含浮钓/路亚/底钓/海钓四族，断言 f14 只取浮钓、
  f15 只取路亚、f16 取底钓，海钓不进三卡。
- `TestFishStats321LegacyFamilyFallback`：`tackle_cat=0` 的行按旧口径归入浮钓/路亚。
- `TestFishStats321FromCatchLog`：断言 f19 只含天然饵、f21 只含拟饵且计数正确。
- `TestGearBreakCounts` / `TestCatchLogTackleCatBackfill`：损伤日志计数去重口径、
  回填值与幂等性。
- 回滚后：f14 重新取海钓绿青鳕、f19/f21 重新串味，上述测试变红。

## Out of Scope

- 不实现「损坏的钓竿/损坏的渔轮/完成咖啡订购/已参加赛事/已赢赛事」等计数卡。
  321 的 f11/f13/f22 语义未定，未用猜测值占用；详见 Further Notes。
- 不实现「冒险鱼」(f9) 与「制作统计」(f20)：前者无业务口径，后者无数据源表。
- 不改 `player_counters` 既有键的语义与成就口径。

## Further Notes

- [事实] `catch_log` 的历史行可按 `catch_guid` 关联到 `kv` 结算回执，回执 `SkillID`
  可 100% 覆盖，回填后分布为 {1:4, 2:73, 11:16}。
- [事实] `by_type_id`：318→args 127（`gconafdfchk`）、319→args 128（`keoejejkenf`）、
  320→129、321→（无，顶层对象）。容器码来自 `argcode_table.json`。
- [事实] `pilkr_*` 竿（rodtype 15）在客户端显示为「柳叶亮片竿」，即 Pilker 竿，
  与 `celffbjpmma` 枚举注释中 1~14 的列举不冲突（该枚举未穷举 15/16）。

### f11/f13 不是「损坏的钓竿/损坏的渔轮」

- [事实] 按该假设下发非零值后，客户端面板对应卡片仍显示 0，且响应被正常解析，
  说明这两个槽位不是那两张卡的来源。
- [事实] `buildStats321` 只被 `hFishStats`(3/2) 调用，无推送路径；打开统计面板时会话
  只请求 `3/2`，`3/3`(成就) 与 `3/17`(稀有鱼) 未被请求。
- [推断] 面板上恒 0 的那批计数卡，数据源不在 321 的 f11/f13；321 未定标量仅有
  f11/f13/f22 三个，不足以承载五张卡。
- [结论] 两个槽位保持常量 0，不以未证实语义占用。修这批卡前应先确定数据源命令，
  不要从 321 反推。

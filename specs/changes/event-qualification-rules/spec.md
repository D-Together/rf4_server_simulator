# 活动分支 9–20 任务判定（首期 241 条）

Status: done

## Problem Statement

限时活动面板（新年/周年，分支 9–20，共 273 条任务）此前只下发分支节点与静态进度
分母，没有任何服务端判定，玩家看到任务却永远停在 0/N，无法完成。永久资格分支
1–8 的判定链路（咬钩快照 → 结算事务内判定 → 5003/5004 增量下发）已就绪，活动分支
缺的只是逐条条件。

## Solution

为活动分支补齐条件判定，复用永久资格的判定/封顶/增量链路。首期实装 241 条可判定
任务并下发到面板；其余 32 条因条件信号缺失暂不下发（避免面板出现永久 0/N 的「可完成」
任务），待真实信号补齐后开放。玩家完成合规渔获时，活动任务与永久任务一样推进进度、
达标当尾弹一次完成窗。

## User Stories

1. 作为玩家，活动开启后在对应水域用合规钓具钓到目标鱼，活动任务进度增长，达标弹一次完成窗。
2. 作为玩家，面板里看到的活动任务都是当前可完成的；条件信号未实装的任务不出现在面板。
3. 边界：线强度未知（未装配/解析失败）时，「强度不超过 X 公斤」任务不计数，不造假进度。
4. 边界：断线重连/完整重登后，28/2 快照恢复活动进度；达标后封顶不再重复弹窗。

## Implementation Decisions

- `internal/gameserver/qualification_event_rules.go`：273 条规则（每条绑定
  `qevFishLanded` + 一个去重键函数），`init` 并入 `qualificationRules`，复用
  `evaluateQualificationsTx`。水域 key 取自 `loc_alias`、鱼种取自 `loc_wiki_fish`、
  饵类/味型/线强度取自 `item_def`。
- 竿型/硬度/钓组大类由 `eventRodTypes/eventRodPowers/eventRigClass` 三张表提供，
  `qualificationRodTypesFor/RodPowerFor/RigClassFor` 对分支 ≥9 查表，保证 5007.Idx4/5/6
  下发与服务端竿型门控同源。
- `qualificationEvent` 新增 `LineStrengthKG`（主线 cat11/前导线 cat15 的 `kngaaefkpka`
  取已装配最小值），在 `qualificationEventForCatch` 咬钩装配处读取。
- `qualification_classify.go` 新增浮钓/底钓/船上/中轻硬度/线强度上限/味型/严格小于/
  与或组合等纯函数过滤工厂。
- `qualification_targets.json` 追加 273 条静态分母（总 441）。
- **缺口处理口径**：32 条信号缺失任务以 `qGapCounted`（恒空键）注册但**不下发**——
  `eventGapRuleIDs` 列出这 32 个编号，`eventTaskList` 跳过它们。规则表保留注册，
  后续补齐信号时替换 `qGapCounted` 并从 `eventGapRuleIDs` 移除即可开放。缺口清单与
  原因由关联 issue 追踪（见 Further Notes）。
- 数据与迁移：不改表结构、不改 5003/5004/5005/5007 wire 形状与 args code，仅填充既有
  通道的活动数据。无迁移。
- 兼容：客户端基线 `4.0.25034`；活动节点仍按 `5009.Idx1`（5 新年/6 周年）过滤，
  永久资格树严格保持 8 分支不受影响。回滚即移除活动规则与查表分支。

## Testing Decisions

- 单元（纯逻辑）：`TestEventQualificationFilters` 覆盖线强度上限、蠕虫饵类、浮钓/底钓
  钓组、单尾二选一（19501）、中轻硬度（20003）、计数型（20302）；
  `TestLineStrengthAtMostBoundary` 锁定未知（0）拒绝、含等号、超限拒绝。
- 协议（编解码回环）：`TestEventRule5007Conditions` 断言活动任务竿型/硬度/钓组大类/
  顺流位下发到 5007 对应字段。
- 结构：`TestEventQualificationRulesCovered` 断言 273 编号阶梯无重复、分母 >0；
  `TestEventGapTasksNotEmitted` 断言 32 缺口不下发、全分支实际下发 241；
  `TestEventGapRulesNeverCount` 断言缺口对任意事件恒空键。
- 节点：`TestEnvironmentAnniversaryNodesFollowSwitch`/`TestEnvironmentNewYearNodesFollowSwitch`
  按过滤后的任务数与编号校验活动节点；`TestEventNodesUseRealTargets` 校验分母取静态目标值。
- 期望值来源：客户端 `qlfctn_t_<id>` 文案、`loc_alias`/`loc_wiki_fish`/`item_def` 静态表，
  非「用同样代码再算一遍」。
- 回滚变红：移除活动规则 → 覆盖测试缺号变红；恢复缺口下发 → `TestEventGapTasksNotEmitted`
  变红；线强度未知放行 → `TestLineStrengthAtMostBoundary` 变红。
- 实机回归项（**未验证**，合并前/后需客户端确认）：活动开启后钓一尾合规鱼（如 19002
  惟有诺克河 ≥80g 鮈鱼）确认增量与面板推进；达标弹一次完成窗；重登 28/2 恢复进度；
  资格面板仍 8 分支；缺口任务不出现在面板。

## Out of Scope

- 不补齐 32 条缺口任务的真实信号（水域/鱼种名单、假啮齿拟饵、累计总重计数器、渔轮尺寸）；
  这些由关联 issue 追踪，另行变更开放。
- 不改永久资格分支 1–8 的规则与分母。
- 不改协议 wire 形状、表结构、活动开关与赛季时段逻辑。

## Further Notes

证据等级（定义见 `docs/agents/domain.md`）：

- `[事实]` 水域/鱼种/饵类/味型/线强度字段映射取自本地静态库（`loc_alias`、
  `loc_wiki_fish`、`item_def`）；活动节点按 `5009.Idx1` 过滤、进度走 28/2+14/4 为既有
  逆向结论。
- `[事实]` 「稀有鱼（星）/trophy」= 个体档位 ≥3（官服 3/2 统计页稀有计数抓包，见
  statsbuild.go）；仅文案明确说「鱼类/鱼种」的 16603/20604 按物种稀有度 ≥1 判。
- `[推断]` 派克竿=Picker（竿型 9，文案明确写 Picker）；蠕虫类饵=饵类 6；昆虫饵=饵类 2/7；
  分支子线钓组=ThreeWay(10)（与永久 2103 同源）。
- `[假设]`（待实机核对，已在代码/JSON 标注）：9103 红虫香精=味型 16；10303「高白蛙」按
  高白鲑 peled；12301 夜晚 23:00–04:00 用现有 19:00–06:00 夜间位近似；12503 文案缺第三处
  水域按已知两湖判；14501 小型勺子按 spoon 族（无尺寸阈值）；14903 三种鲽鱼按鲽科鱼种
  去重；20602/20603「2 条稀有或两种罕见」按 2 尾 Grade≥3；「超过 X 公斤」按 ≥ X×1000+1 克。
- 缺口追踪 issue：glance522/rf4_server_simulator#379（32 条按「水域名单未知/鱼种名单未知/
  假啮齿拟饵/累计总重」分组列清单与开放流程）。
- 与 `qualification-progress` 的关系：该变更 Out of Scope 的「不引入活动分支判定」由本
  变更承接，已在其 spec 交叉引用本文件，消除冲突。
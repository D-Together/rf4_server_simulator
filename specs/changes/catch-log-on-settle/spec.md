# 钓获日志提前到结算：放生也点亮图鉴

Status: ready

## Problem Statement

稀有鱼奖座与水域奖杯在 14/4 结算界面当场弹卡之后，玩家发现图鉴与它们对不上：

- 钓到一条新的上星稀有鱼，结算界面弹出「N条稀有鱼」奖座、面板进度也够了，
  但把这条鱼**放生**之后，3/17「渔夫稀有鱼」面板仍然缺这一格、307.f16 星号也不涨。
- 拿到水域奖杯之后，面板还可能差一格 —— 因为奖杯按「钓到」算，图鉴按「入护」算。

根因是两套口径：`catch_log` 原先只在 14/5 入护时写入（`releaseCatchOnce` 完全不写），
而奖座/奖杯已经改成认 `catch_seen`（钓到即记）。官服的表现是结算界面先弹卡、
玩家随后才决定入护或放生 —— 说明官方的收集进度在「钓到」那一刻就成立，
图鉴不应该比奖座晚一步。

## Solution

把 `catch_log` 的写入时机从「入护」提前到「结算」：

- **14/4 结算事务内**写下这一尾（`insertCatchLogSettleTx`，`source='catch'`），
  与经验、熟练度、磨损、`catch_seen`、结算回执同一个事务。
- **14/5 入护**把同一行抬成 `source='keep'`，并补上入护当刻的钓法快照；
  `ts` 保留钓到时刻。没有结算行时（补丁前的旧 pending）才退化为 INSERT。
- **14/6 放生**不动这一行，`source` 保持 `'catch'`：钓到过就是钓到过。

于是 3/17 面板、307.f16 奖杯鱼种数、24/19 玩家卡、321 统计与水域奖杯、
稀有鱼奖座全部读同一份「钓到过」的记录，图鉴与奖座不再有先后差。

## User Stories

1. 作为玩家，我钓到一条新的上星稀有鱼后放生，图鉴面板与等级旁星号仍然点亮它。
2. 作为玩家，我拿到水域奖杯时，3/17 面板对应水域一定是满格，不会出现「有奖杯、缺一格」。
3. 作为玩家，我把鱼入护之后，钓获记录仍是同一条，不会在统计页里变成两条。
4. 边界：同一尾鱼重复结算、重复入护、放生后再入护都不会产生第二行。
5. 边界：结算写入失败会连同经验一起回滚，重试时重新判定。

## Implementation Decisions

- 写入：`internal/gameserver/catch_settlement.go`
  - `insertCatchLogSettleTx` 在 `settleCatchOnceWithQualification` 的事务里执行，
    字段与入护路径一致（`loc/spot/bait/grade/xp/silver/loc_level/tackle_cat`）。
  - `loc_level` 为空时用 `currentLevel` 兜底，避免面板归不到水域。
  - 唯一索引 `uq_catch_log_fish_identity(uid, source, catch_guid)` + `ON CONFLICT DO NOTHING`
    保证重放不重复；结算本身还有 `catch-settlement:` 回执兜底。
- 入护：`internal/store/repos_fishing_keep.go`
  - `consumed` 判定保持 `source='keep' AND catch_guid=?`（结算行是 `'catch'`，不算已处理）。
  - 落库顺序：`keepnet_items` INSERT → `catch_log` UPDATE 成 `'keep'`；`RowsAffected=0`
    才退回原来的 INSERT（兼容补丁上线前的 pending）。
- 放生：`releaseCatchOnce` 不改 `catch_log`，保持 `'catch'`。
- 图鉴与星号：`buildFishTrophies323`（3/17）、`RareSpeciesCount`（307.f16 / 24/19 f5）
  读 `catch_log` 的 SQL 不变，语义自动变成「钓到即点亮」。
- 成就：`distinctCatchBaits` 仍读 `catch_log`，现在会把放生用过的饵计入；
  触发事件仍是 14/5 `evFishKept`，所以不会提前解锁。

## 影响面：读 catch_log 的既有口径

`catch_log` 开始包含放生的鱼（`source='catch'` 常驻）后，所有读它的既有口径一并变化，
逐项列出：

| 消费方 | 位置 | 变化 |
|---|---|---|
| 3/17 图鉴面板 | `buildFishTrophies323` | 钓到即点亮（本 PR 的目标行为） |
| 307.f16 星号 / 24/19 玩家卡 | `store.RareSpeciesCount` | 鱼种数含放生的鱼，与奖座同步 |
| 321 统计（3/2） | `buildStats321` / `loadCatchStatRows` | 总条数、稀有/超稀有、按日条数与重量全部含放生 |
| 成就 906/907「20/50 种诱饵」 | `distinctCatchBaits`（`evFishKept` 触发） | 诱饵种类含放生用过的饵；判定时刻不变（14/5） |
| 成就 911「累计释放 10 条稀有鱼」 | 未实装 | 本改动后「grade≥3 且 source='catch'」即放生稀有鱼数，具备实现条件（本 PR 不实装） |
| 结算纪录判定 | `insertCatchLogSettleTx` 上方的 `MAX(weight_g)` | 历史最大含放生的鱼：破纪录弹「新纪录」按「钓到过」口径 |
| 最近钓获 / 社交动态 | `store`（`repos_social.go` 的最近钓获） | 放生的鱼也出现在最近钓获列表里 |
| `CatchLogTotals` | `store`（`repos2.go`） | 无调用方；其 `source='catch'` 过滤在 main 上恒为空集，本改动后变为「只统计放生」，仍无实际影响，留待后续清理 |

放生鱼进 321「钓获统计」是语义取向：官服口径「钓到即记」，统计页名为钓获而非入护，
包含放生是正确的；若后续竞赛要区分，需单独加 `source` 过滤（竞赛计分不读 `catch_log`，
暂无此需求）。

## Testing

- `internal/gameserver/waterway_trophy_keep_test.go`
  - `TestWaterwayTrophyIssuedOnSettleBeforeKeep`：结算当场发奖杯 + `801.f4` 来源码 6 →
    放生后 `catch_log` 保留 `source='catch'`、水域面板满格、奖杯不回收 →
    再结算/入护/重放都不重复发。
- `internal/gameserver/rarity_milestones_test.go`
  - `TestRarityMilestoneCountsReleasedCatch`：放生后图鉴记录 1 行、`source='catch'`、
    `RareSpeciesCount(grade>=3)` 计入这一种。
- `internal/gameserver/catch_recovery_test.go`
  - `TestCompletedCatchRecoveryFailurePreservesPendingAndRetries`：入护写库失败时
    pending 保留、护网为空、重试后正常入护（触发器等同时拦住 INSERT 与 UPDATE）。
- `internal/gameserver/fishing_semantics_test.go`
  - 入护后 `catch_log` 仍只有 1 行且 `source='keep'`（验证 UPDATE 而不是新增）。
  - `TestCatchLogReplayedSettleAfterKeptStaysSingleRow`：结算 → 入护 → 重放 14/4。
    唯一索引 `uq_catch_log_fish_identity` 只到 `(uid, source, catch_guid)`，入护把行
    抬成 `'keep'` 后 `'catch'` 槽位空出；重放必须命中 `catch-settlement:` kv 回执
    （`replayed=true`）而不二次入账——`catch_log` 仍 1 行、经验不变。

## Out of Scope

- 不给历史放生的鱼补 `catch_log` 行：那些记录从未落库，无法追溯。
- 不改 3/2 统计的读取方式（仍读 `catch_log` 全表），也不动 `catch_log` 的表结构。
- 不把 `catch_seen` 合并进 `catch_log`：前者仍是奖座/奖杯的聚合口径，两者同事务写入。

## Further Notes

- `[事实]` 官服结算界面在入护/放生之前就弹奖座与水城奖杯卡（2026-11-11 实机），
  说明图鉴同口径在「钓到」时成立。
- `[事实]` 本改动前 `releaseCatchOnce` 没有任何 `catch_log` 写入语句，放生的鱼只留
  `kv` 的 `catch-release:<uid>:<guid>` 标记。
- `[推断]` 321 统计与竞赛计分会开始包含放生的鱼。语义上「钓获统计」应当包含，
  但这是可见变化，若后续认为竞赛只算入护，需要给竞赛单独加 `source` 过滤。

# 水域奖杯与渔夫图鉴收录清单

Status: ready

## Problem Statement

玩家把某个水域的「渔夫稀有鱼」面板（3/17）全部钓到上星后，拿不到官方那枚水域奖杯
（`trophy_level_<水域号>`，物品类别 72，出现在 3/16 奖励面板）。日志上能看到的只有
「面板看着满了，可是没有奖杯」。

原因是分母取错了：服务端用 `loc_fish`（这张图能钓到什么）当分母，而面板只收录官方
编好的十来个鱼种。惟有诺克河 `loc_fish` 20 种、面板 10 格；白河 19 种、面板 10 格。
分母永远比面板大，判定永远差几种，奖杯就永远发不出来。

## Solution

按客户端面板截图反推出每个水域的**收录清单**（`trophyFishWhitelist`），判定分母只用
清单里的格子；清单里每一格都有 `grade >= 3`（上星）或 `grade = 4`（蓝冠）的
**钓到记录**即集齐，发一枚该水域奖杯，并在结算界面弹卡。

判定放在 **14/4 结算当场**（数据源 `catch_seen`，钓到即记、带 `loc_level`）。
官方结算界面（801）在玩家选择入护或放生**之前**就画出「捕获了该水域的所有稀有鱼」
这张卡，说明官方的进度在「钓到」那一刻就成立（2026-11-11 实机回归）。
`catch_log` 同步改为**结算即落库**（`source='catch'`，入护抬成 `'keep'`、放生保持
`'catch'`），所以图鉴、307.f16 星号、3/17 面板与奖杯永远同步 —— 放生也点亮。

## User Stories

1. 作为玩家，我希望把某水域面板上所有格子都钓到上星/蓝冠之后立刻拿到该水域奖杯，
   以便收齐 3/16 奖励面板里的纪念品。
2. 作为玩家，我放生了一条鱼之后，图鉴仍然点亮它（钓到即算收集），与奖杯、星号同步。
3. 边界：重放同一次入护、重登、连续打开面板都不会发第二枚奖杯。
4. 边界：奖杯卖掉/丢掉/搬进小屋后不会重发（纪念品）。
5. 边界：面板上出现本服派不出的鱼种时（认错显示名、或客户端独有），不应该把该水域
   卡成永远集不齐；机制保留（`trophyFishWhitelistUnspawnable`），当前没有条目。

## Implementation Decisions

- 判定与发放：`internal/gameserver/waterway_trophy.go`
  - `waterwayTrophyProgressFor`：有白名单走 `whitelistTrophyProgress`，否则退回
    `loc_fish ⨝ loc_alias` 左连 `catch_seen`（`loc_level` 精确匹配；只会晚发、不会早发）。
  - `grantWaterwayTrophy` / `issueWaterwayTrophy`：幂等键 `kv` 的
    `waterway-trophy:<uid>:<水域>`，与物品插入同一事务提交。
  - `ensureWaterwayTrophies`：登录 3/1 与打开 3/17 时巡检补发。
- 触发点：`internal/gameserver/catch_settlement.go` 的 14/4 结算事务提交之后，
  `waterwayTrophyAfterCatch` 只判**本尾鱼所在水域**（一尾鱼只可能在它被钓上来的那片水
  补上最后一格）。奖杯随 `801.f4` 的来源码 **6**（「捕获了该水域的所有稀有鱼」）下发，
  同时进 `801.f8`，响应后推 4/22。
  `econ.go` 的 `fishingKeep` 保留同一次调用，但只是**兜底**（结算发放失败时重试，幂等），
  那时已经没有结算界面，才改走 4/22 + `pushSystemChat` 播报。
- 收录清单：`internal/gameserver/fish_trophy_whitelist.go`，key 是水域 systemID，
  value 是面板格子（`catch_log.fish_key` + 客户端显示名）。18 个水域全部录入，共 324 格
  （埃尔克湖 17 格于 2026-11-11 按面板截图补齐：本图 19 种鱼里只有斑点雀鳝 `sgar`、
  鳄雀鳝 `agar` 不在面板上）。
  - `trophyFishWhitelistMissing`：面板上有、但服务端 252 个鱼种里认不出 key 的格子。
    计入分母、分子永不加（宁可晚发也不误发）。**当前为空**——原两格（下通古斯卡河
    「第三种鲟」、挪威海「鲣鱼」）于 2026-11-11 核对后确认是 `riv_brown_trout` 鳟鱼与
    `m_molva` 魣鳕，已按真 key 补进清单。
  - `trophyFishWhitelistUnspawnable`：认得出 key、但该水域 `loc_wiki_fish` 里没有这种鱼。
    分母一并扣掉、分子也不计，避免把该水域卡成永远集不齐。**当前为空**——原条目
    （苏拉河「鲑鱼」→`k_salmon`）在 2026-11-11 核对后确认是「鲢鱼」`s_carp`，本图能派鱼，
    已改回真 key，苏拉河分母恢复 16 格。
- 3/17 面板：`buildFishTrophies323` 同样按白名单过滤，保证「面板上看得见的格子」与
  「判定算的格子」是同一份。
- 数据与迁移：`00057_catch_seen_loc_level.sql` 给 `catch_seen` 补 `loc_level` 维度
  （重建表，按 `(uid, loc_level, fish_key)` 去重；从 `catch_log` 按水域回填）。
  `catch_log` 结构不动，但写入时机提前到 14/4 结算（`insertCatchLogSettleTx`），
  入护改为把同一行抬成 `'keep'`。历史已集齐的水域由 3/1 / 3/17 巡检补发。
- 兼容与回滚：客户端基线 4.0.25034。回滚只需去掉 `fishingKeep` 里的调用与巡检调用，
  已发奖杯与 `kv` 标记留在库里不会触发任何逻辑（不会重发第二次）。
- 协议：`801.f4` 复用 811 礼物行，来源码 **6**「捕获了该水域的所有稀有鱼」（`f1` 给非 0
  即可，文案没有占位符）。码表由反汇编 `GameAssembly 0x180D4BD40` 确认：跳转表 6 项中
  3 是空号，有效值 1/2/4/5/6；6 号分支对应 `kDlg_Catched_GiftsTrophyTableCollected`，
  并与 2026-11-11 实机弹卡表现一致。

## Testing Decisions

- 存储+行为层（真实 SQLite，`newDetachTestServer` / `newRigTestServer`）：
  - `TestWaterwayTrophyIssuedOnSettleBeforeKeep`：14/4 结算当场发 1 枚且 `801.f4` 带
    来源码 6 的礼物行 → 14/6 放生不回退（奖杯在包里、`catch_log` 保留 source='catch'
    且面板满格）→ 同种鱼再结算 + 入护不重复发 → 重放不再发。
  - `TestWaterwayTrophyUsesPanelWhitelistAsDenominator`：清单外的鱼上星不影响分母。
  - `TestWaterwayTrophySuraAllPanelCellsReachable`：苏拉河 16 格全部可生成、分母 16；
    15 格上星不发、16 格上星发一枚（旧实现把「鲢鱼」认成 `k_salmon` 并扣掉分母，
    只能靠 15 格拿奖杯）。
  - `TestWaterwayTrophyGrantedOnlyWhenAllTrophyFishCollected`：口径 17 格（埃尔克湖），
    差一格（最后一格只到 grade 2）不发、补上星发一枚、落库、`kv` 标记、卖掉不补发。
  - `TestWaterwayTrophyFallsBackToLocFishWhenNoWhitelist`：18 个水域都录了清单之后，
    「没有清单退回 `loc_fish`」这条兜底仍要覆盖，用一个没有面板也没有奖杯定义的
    合成水域（`level_099_no_panel`）跑 2/3 → 3/3，集齐也不发物品、不报错。
  - `TestEnsureWaterwayTrophiesBackfillsCompletedWater`：巡检只处理有奖杯定义的水域
    （家园有 loc_fish 记录也被跳过），已集齐的埃尔克湖补发一枚。
- 纯逻辑层：`TestTrophyFishWhitelistStructure` 钉住数据约束——无重复 key、无空白格、
  不含家园、`unspawnable` / `missing` 只出现在有清单的水域且 key 必须真的在清单里。
- 期望值来源：面板截图反推的格子清单 + 手写的 `catch_log`/`catch_seen` 行，
  不用被测代码再算一遍。
- 回滚后变红：把判定挪回 14/5 入护，`TestWaterwayTrophyIssuedOnSettleBeforeKeep` 第 1 步
  立刻失败；把苏拉河那一格改回 `k_salmon`，`TestWaterwayTrophySuraAllPanelCellsReachable`
  立刻失败（分母变 15、`s_carp` 缺失）。

## Out of Scope

- 不再有「面板有、本服没有」的格子：原 `trophyFishWhitelistMissing` 的两格已确认为
  本图真实鱼种（鳟鱼 `riv_brown_trout`、魣鳕 `m_molva`），机制保留但表为空。
- 不补苏拉河任何「派不出」的派鱼规则：该图 16 格都是本图真实鱼种，不需要例外；
  `loc_wiki_fish` 是从客户端静态数据反推的，人为加行会污染生态与查询链路。
- 不给历史放生的鱼补 `catch_log` 行：补丁上线前的放生记录没有落库，无法追溯。

## Further Notes

- `[事实]` `catch_log` 原先只在 14/5 入护写入；本改动把它提到 14/4 结算
  （`insertCatchLogSettleTx`），入护只更新 `source` 为 `'keep'`，放生保持 `'catch'`。
  3/17 面板、图鉴、307.f16 星号与 321 统计都读 `catch_log`，因此统一为「钓到即点亮」。
- `[事实]` `loc_wiki_fish` 里 `k_salmon` 只出现在「阿赫图巴河」；苏拉河 37 种鱼里没有它。
  2026-11-11 复核：苏拉河面板上那一格是「鲢鱼」`s_carp`（`loc_fish` 15 条记录、有完整
  `fish_ecology`），原先按显示名认成「鲑鱼」是反推错误，已改正。
- `[事实]` 白名单来自面板截图反推（2026-11-10 的 18 张，以及 2026-11-11 补的埃尔克湖），
  按「克重+体长」回查 `catch_log` 锁定 `fish_key`；客户端显示名与 `loc_fish.name_zh`
  并不总一致（常见鲃鱼=`c_barbel`、长吻梅花鲈=`ruffe_n`、湖鳟鱼=`gc_smelt`、
  魣鳕=`m_molva`……），不能只按名字猜。
- `[事实]` 埃尔克湖 19 种鱼、面板 17 格，未收录的是斑点雀鳝 `sgar` 与鳄雀鳝 `agar`；
  补上之后 18 个水域全部有清单，`loc_fish` 兜底只剩「将来新增水域」会用。
- `[推断]` 面板每格的门槛与 3/17 一致（`grade >= 3`）。依据是同一次上星既点亮面板格子，
  也让 307.f16 星号计入。
- `[事实]` 811 礼物行来源码表已定案（2026-11-11）：反汇编 `GameAssembly 0x180D4BD40`
  的 switch + 跳转表 `0x180D4C04C`，3 是空号（落点 = `ja` 越界目标），有效值 1/2/4/5/6；
  实机确认 2=游戏活动、5=数条稀有鱼、6=集齐水域。见 `docs/gameplay.md` 的码表段落。

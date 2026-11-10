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
`catch_log` 记录即集齐，发一枚该水域奖杯，背包 + 系统播报。

判定放在 **14/5 入护落库之后**：`catch_log` 只在入护时写，放生的鱼不进图鉴，
所以「面板填满」与「拿到奖杯」严格同时发生。

## User Stories

1. 作为玩家，我希望把某水域面板上所有格子都钓到上星/蓝冠之后立刻拿到该水域奖杯，
   以便收齐 3/16 奖励面板里的纪念品。
2. 作为玩家，我放生了一条鱼之后，不希望面板进度或奖杯判定被这尾鱼影响。
3. 边界：重放同一次入护、重登、连续打开面板都不会发第二枚奖杯。
4. 边界：奖杯卖掉/丢掉/搬进小屋后不会重发（纪念品）。
5. 边界：面板上存在本服派不出的鱼种时（苏拉河「鲑鱼」），不应该把该水域卡成永远集不齐。

## Implementation Decisions

- 判定与发放：`internal/gameserver/waterway_trophy.go`
  - `waterwayTrophyProgressFor`：有白名单走 `whitelistTrophyProgress`，否则退回
    `loc_fish ⨝ loc_alias` 左连 `catch_log`（只会晚发、不会早发）。
  - `grantWaterwayTrophy` / `issueWaterwayTrophy`：幂等键 `kv` 的
    `waterway-trophy:<uid>:<水域>`，与物品插入同一事务提交。
  - `ensureWaterwayTrophies`：登录 3/1 与打开 3/17 时巡检补发。
- 触发点：`internal/gameserver/econ.go` 的 `fishingKeep`，`keptInNet` 为真（本次真的写了
  `keepnet_items` + `catch_log`）时判定；到账走 4/22 背包增量 + `pushSystemChat` 播报。
  结算（14/4）与放生（14/6）都不判定。
- 收录清单：`internal/gameserver/fish_trophy_whitelist.go`，key 是水域 systemID，
  value 是面板格子（`catch_log.fish_key` + 客户端显示名）。
  - `trophyFishWhitelistMissing`：面板上有、但服务端 252 个鱼种里认不出 key 的格子。
    计入分母、分子永不加（宁可晚发也不误发）。
  - `trophyFishWhitelistUnspawnable`：认得出 key、但该水域 `loc_wiki_fish` 里没有这种鱼
    （苏拉河 `k_salmon` 只属于阿赫图巴河）。分母一并扣掉、分子也不计，避免把该水域
    卡成永远集不齐。
- 3/17 面板：`buildFishTrophies323` 同样按白名单过滤，保证「面板上看得见的格子」与
  「判定算的格子」是同一份。
- 数据与迁移：不改表、不改 `catch_log` 结构；纯读 + 新增 `item` 行与 `kv` 标记。
  历史已集齐的水域由 3/1 / 3/17 巡检补发。
- 兼容与回滚：客户端基线 4.0.25034。回滚只需去掉 `fishingKeep` 里的调用与巡检调用，
  已发奖杯与 `kv` 标记留在库里不会触发任何逻辑（不会重发第二次）。
- 协议：本改动**不**往 `801.f4`（811 礼物行）写自定义来源码。原先在结算界面弹「集齐
  水域」礼物卡的实现已删除：结算早于入护，那时 `catch_log` 还没有这尾鱼，必然要么提前
  发奖杯（放生后错位）、要么先展示一张还拿不到的卡。

## Testing Decisions

- 存储+行为层（真实 SQLite，`newDetachTestServer` / `newRigTestServer`）：
  - `TestWaterwayTrophyIssuedOnlyAfterKeep`：14/4 结算不发 → 14/6 放生不发且不写
    `catch_log`、面板仍是 7/8 → 同种鱼改成 14/5 入护后发一枚 → 重放不再发。
  - `TestWaterwayTrophyUsesPanelWhitelistAsDenominator`：清单外的鱼上星不影响分母。
  - `TestWaterwayTrophySuraUnspawnableCellDoesNotBlock`：苏拉河把 15 种可生成鱼种全部
    上星后进度即满（旧实现分母含 `k_salmon`，永远差一格）。
  - `TestWaterwayTrophyGrantedOnlyWhenAllTrophyFishCollected`：未集齐不发、集齐发一枚、
    落库、`kv` 标记、卖掉不补发。
  - `TestEnsureWaterwayTrophiesBackfillsCompletedWater`：巡检只处理有奖杯定义的水域。
- 纯逻辑层：`TestTrophyFishWhitelistStructure` 钉住数据约束——无重复 key、无空白格、
  不含家园、`unspawnable` / `missing` 只出现在有清单的水域且 key 必须真的在清单里。
- 期望值来源：面板截图反推的格子清单 + `catch_log` 手写行，不用被测代码再算一遍。
- 回滚后变红：把判定挪回 14/4，`TestWaterwayTrophyIssuedOnlyAfterKeep` 第 1、2 步
  立刻失败；把 `unspawnable` 去掉，苏拉河那条测试失败。

## Out of Scope

- 不补 `trophyFishWhitelistMissing` 里那两格（下通古斯卡河「鲟鱼」、挪威海「鲣鱼」）
  对应的鱼种——需要客户端独有的鱼种数据。
- 不给苏拉河 `k_salmon` 补派鱼规则：`loc_wiki_fish` 是从客户端静态数据反推的，
  人为加一行会污染生态与查询链路。
- 不做结算界面的集齐水域礼物卡（原因见 Implementation Decisions 最后一条）。

## Further Notes

- `[事实]` `catch_log` 只在 14/5 入护写入；14/6 放生（`releaseCatchOnce`）不写。
  3/17 面板、图鉴、307.f16 星号都只统计 `catch_log`。
- `[事实]` `loc_wiki_fish` 里 `k_salmon` 只出现在「阿赫图巴河」；苏拉河 37 种鱼里没有它。
- `[事实]` 白名单来自 18 张面板截图反推（2026-11-10），按「克重+体长」回查 `catch_log`
  锁定 `fish_key`；客户端显示名与 `loc_fish.name_zh` 并不总一致（常见鲃鱼=`c_barbel`、
  长吻梅花鲈=`ruffe_n`、湖鳟鱼=`gc_smelt`、魣鳕=`m_molva`……），不能只按名字猜。
- `[推断]` 面板每格的门槛与 3/17 一致（`grade >= 3`）。依据是同一次上星既点亮面板格子，
  也让 307.f16 星号计入。
- `[未验证]` 811 礼物行来源码表（`801.f4` / `809.f0`）本服两次实机扫频结论互相矛盾，
  见 `docs/gameplay.md` 的说明；本改动不依赖它。

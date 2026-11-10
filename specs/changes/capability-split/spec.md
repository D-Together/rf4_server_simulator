# 能力契约按域拆分

Status: in-progress

<!-- 验证：拆分与引用更新已完成；待下一次改动实际按新文件命中后改 done。 -->

## Problem Statement

`specs/capabilities/` 只有四份大文件，其中 `fishing-and-settlement.md`（102 行）同时覆盖
结算幂等、饵料消耗、鱼口与食性、嘬饵否决、派鱼档位、剪线断竿六个互不相干的改动面。后果：

- **改一处要读全文**：改轮子拉力的人得跳过结算与派鱼才能找到载荷条目。
- **对账表写不细**：`docs/agents/domain.md` 只能写"改钓鱼 → fishing-and-settlement"，
  粒度粗到等于没写。
- **缺口看不见**：管理端 tunables、经济与发奖、玩家状态条这三块根本没有契约，
  四份大文件"看起来都覆盖了"，实际是都没有。

## Solution

把能力契约拆成**一域一份、每份 ≤80 行**，共十三份；原有内容只搬家、不重写，新增六份补空白：

- 拆：`fishing-and-settlement.md` → `settlement-and-idempotency.md` + `bite-bait-and-spawn.md`
- 补：`admin-and-tunables.md`、`economy-and-rewards.md`、`player-condition.md`、
  `achievements.md`、`fish-query.md`、`runtime-and-launch.md`
- 留：`protocol-contract.md`、`gear-and-line-load.md`、`rig-assembly.md`、
  `store-and-idempotency.md`、`engineering-quality.md`

原 `fishing-and-settlement.md` 删除（不留第二份、不做指针文件），引用点全部改到新归属。

## User Stories

1. 作为改载荷的人，我希望只读 `gear-and-line-load.md` 就能覆盖本次改动的全部陷阱。
2. 作为评审者，我希望从文件名判断这次改动该对照哪份契约。
3. 作为新贡献者，我希望从目录就能看出"哪些域已经有契约、哪些还没有"。
4. 边界：拆分过程中任何一条既有 ✅/❌ 不得丢失或改写语义；只是改变所在文件。

## Implementation Decisions

- 归属划分：
  - `settlement-and-idempotency.md` ← 结算幂等、同一尾鱼、剪线/断竿/跑鱼、放生/入护、已结算待处理窗口
  - `bite-bait-and-spawn.md` ← 饵料消耗时机、选中钩与饵、鱼口与食性、嘬饵否决与 14/15、派鱼档位与发鱼模式、饵料轮换语义
  - `admin-and-tunables.md` ← tunables 通道与缓存、后台覆盖优先级、管理端与 gameserver 的进程边界、踢人/封禁延迟
  - `economy-and-rewards.md` ← 商店估价口径、鱼市地图口径、制作/咖啡厅事务、邮件快照、礼包触发点
  - `player-condition.md` ← 状态条推进与下发、离线冻结、精力门槛、消耗品恢复、装备加成
  - `achievements.md` ← 单路径判定、已解锁语义、解锁推送命令、列表只读
  - `fish-query.md` ← 复用产线权重链、查询只读、虚拟联系人、口径缺省、回复文案出口
  - `runtime-and-launch.md` ← 装配唯一入口、初始化四步、静态库只读、地址来自配置、启动期依赖
- 不复制的部分：轮换实现细节只在 `bite-bait-and-spawn.md` 写一次，后台侧只引用；
  发奖幂等只写在 `settlement-and-idempotency.md`，经济侧引用。
- 引用更新点：`CLAUDE.md` 事实入口表、`docs/agents/domain.md` 对账表、`docs/README.md` 契约清单、
  以及各契约之间的交叉引用。`docs/mechanism-map.md` 不变（它指代码，不指契约）。
- 不改任何 ✅/❌ 的语义、不改数值、不新增约束判定。

## Testing Decisions

- 测哪一层：文档门禁（`internal/doccheck`）+ 内容对账。
- 接缝：`go run ./tools/doccheck` 会同时校验"新契约文件已在 `docs/README.md` 登记"与
  "所有 `docs/*.md`、`specs/*.md` 引用存在"——删掉旧文件后若漏改引用会直接红，这是本次的主要验收。
- 期望值来源：拆分前统计的条目数（`###` 小节 9 个、✅ 条 27 条、❌ 条 21 条），拆分后逐项核对，
  允许的差异只有新增三份里的新条目。
- 回滚后哪些测试会变红：把旧文件恢复而不改引用时，`doccheck` 的 `ref` 规则会报新文件不存在；
  即回滚不会静默通过。

## Out of Scope

- 不重组 `CLAUDE.md` 的红线（那是另一件事，需单独评审）。
- 不把 `docs/gameplay.md` 按机制拆成多份（它现在是可用的总纲）。
- 不新增协议字段、不改任何运行时行为。

## Further Notes

- 依据 `[事实]`：`fishing-and-settlement.md` 拆分前 102 行、9 个小节，引用点 4 处
  （`CLAUDE.md`、`docs/agents/domain.md`、`gear-and-line-load.md`、`rig-assembly.md`）。
- 外部参照：能力契约按主题拆小、单份控制在数十行，是多仓库协作项目的通行做法；
  超过约 150 行后追加内容的收益递减，且更难判断"改这里要不要同步"。

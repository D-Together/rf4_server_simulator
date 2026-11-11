# 满级技能加成行不再展示（+0 = 100% 修复与熟练度 0.1 显示格量化）

Status: done

## Problem Statement

技能已满级（100%）的玩家钓获后，结算画面仍展示技能加成行「+0 = 100%」——增量显示为 0
却出现在加成列表里，且只在部分钓法出现（海钓不复现）。

复现数据 `[事实]`：存量库纺车熟练度 points = `99.99999999999996`（SQLite REAL 反复
累加 0.1 的二进制残差）。客户端按 0.1 格渲染，把它显示成 100%，但它仍小于上限 100；
结算钳位 `cap - old` 产生 **3.6e-14 的正残渣增量**，随 310.f2（非零钓法大类）+ f3≈0
下发，客户端四舍五入渲染出「+0 = 100%」。海钓存量恰为整数 100，gain 恒为 0，不复现。

## Solution

两条互补的改动：一条治数据（增量与存量在 0.1 显示格上归一），一条治展示（无熟练度
可报时不报钓法大类，客户端整行不渲染）。

1. **熟练度 0.1 显示格量化**：官服增量只有 0.1/0.2，客户端 UI 按 0.1 渲染——0.1 格
   就是熟练度的原生语义。写库（两处 UPSERT SQL）与结算读侧统一
   `ROUND(x*10)/10`，浮点尾差不再产生。
2. **无熟练度可报时 310.f2 = 0**：满级钳零或回执重放时，310 块不再携带钓法大类。
   官服抓包（n=18）中 7 行 f2=0（与 f0=0 行数一致），即官服「无可报→不报大类」的
   语义；客户端技能加成行按 f2 驱动，f2=0 整行不渲染。

## User Stories

1. 作为满级玩家，钓获结算不再出现「+0 = 100%」技能加成行；经验/加成行不受影响。
2. 边界：未满级技能照常按 0.1/0.2 增长并展示「+0.1/+0.2 = 当前%」。
3. 边界：回执重放（14/4 重发）不再重放熟练度行——原本会误弹旧的 +0.x = 100%。
4. 边界：801.f6 的 312 当前技能面板与档案技能值不受影响（f2 归零只作用于 310 块）。

## Implementation Decisions

- `internal/store`：新增 `QuantizeSkillPoints`；`AddSkillPoints`/`AddSkillPointsTx`
  两处 UPSERT SQL 改为 `MIN(ROUND((points+excluded.points)*10)/10, cap)`。
- `internal/gameserver`（catch_settlement.go）：读出 `oldSkill` 先量化（治愈存量脏
  数据），钳位后 gain 再量化（3.6e-14 残渣归零）；`gain > 1e-9` 收紧为 `gain > 0`。
- `internal/gameserver`（progress_ops.go buildSettle813）：`skillGain <= 0` 时 310.f2
  局部归零；801.f6 的 312 面板与 `st.LastSkill` 仍用原 skillID。
- 数据与迁移：无表结构改动；**存量尾差不做一次性清理迁移**——读侧量化已把它当
  上限处理，行隐藏且不再写入，后续任何真实增量（满级后恒 0）也不会触碰该行。
  本地部署曾用 `UPDATE skill SET points=ROUND(points*10)/10` 手工归一，属运维操作
  不入代码。
- 兼容：客户端基线 4.0.25034；310/813 协议形状无变化，仅字段取值语义；回滚方式为
  恢复两处 SQL 与 f2 赋值。

## Testing Decisions

- 测哪一层：结算行为层（真实 SQLite + settleCatchXP）+ 协议装配层（buildSettle813）。
- 期望值来源：复现用的具体残差字面量 `99.99999999999996` 直接写进用例（比抽象断言
  更能钉住回归）；官服抓包字段分布见 `tools/ida-docs/fields/kegeppimjbm.md`。
- 覆盖用例：`TestCatchSkillFloatResidueHidesSkillRow`（尾差 → gain=0、310.f2=0、
  库值不被改动）、表格用例 `float_residue`、`TestSettle813ReportsActualSkillGain`
  与 `TestSettle813SuppressesReplayedSkillGain` 的 f2 四象限断言（有增量/满级/重放/
  首次）。
- 回滚后哪些测试会变红：恢复 SQL 或 f2 赋值后上述用例全部变红。

## Out of Scope

- 不迁移/清理存量浮点尾差数据（读侧量化已语义归一）。
- 不改熟练度增长概率与增量公式（仍是 0.1/0.2 两档）。
- 不处理其它浮点累加场景（经验、银币为整数路径，无此问题）。

## Further Notes

- `[事实]` 存量残差实值：`SELECT points FROM skill` → 纺车 `99.99999999999996`、
  海钓 `100.0`（2026-11-11 实测，部署库）。
- `[事实]` 官服 310 字段分布：f2 ∈ {0×7, 1×10, 2×1}、f3 ∈ [0, 0.2]
  （tools/ida-docs/fields/kegeppimjbm.md，n=18）。
- `[推断]` 客户端 f2 门禁：RUIPlayerProgressStackItemSkill.OnInvalidate 无可见性
  判定（反编译逐行确认），行是否出现由列表装配按 f2 驱动；该装配点位于 IL2CPP
  共享泛型体内、dump 不可见。由排除法得出：f3 门禁与实机行为矛盾（f3=0 仍渲染
  「+0 = 100%」），协议内「无技能」唯一编码位就是 f2=0。
- 量化安全性前提：增量恒为 0.1/0.2 且 `profCap - quantized(old)` 是 0.1 的整数倍，
  正常路径下 `ROUND(x*10)/10` 是恒等变换；若未来引入非 0.1 网格增量或修改
  `profCap`，需重新评估四舍五入方向（最多向上多给 0.05）。

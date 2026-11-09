# 统计页 3/2 纪录卡维度修正

Status: done

## Problem Statement

统计页 321 的最大渔获、最优竞争力、浮钓最大/路亚最大存在维度重复或饵种分类错误：
最优竞争力直接复用全场最重；路亚只按 `spinner` 前缀识别，spoon/plug/topwater 等品类 17 拟饵被算进非拟饵。
另有两个客户端槽位 f9（冒险鱼）和 f16（底钓最大鱼）原先发 null。

## Solution

最优竞争力改为按搏鱼拉力排序；非拟饵/拟饵用物品定义品类 17 区分；f9/f16 在没有可靠业务口径时继续发 null，
不用“捕捞量/最长”等其他维度占位。

## User Stories

1. 作为玩家，我希望“最优竞争力”不再和“最大渔获”重复。
2. 作为玩家，我希望 spoon/plug/topwater/softplastic 等拟饵归入路亚纪录。
3. 边界：旧档只有 catch_log 时，拉力按鱼种曲线与重量确定性回溯，不新增表列。
4. 边界：catch_log 没有钓法/钓组快照，不能伪造“底钓最大”；f16 保持空卡。

## Implementation Decisions

- 模块：`internal/gameserver/statsbuild.go`。
- f8：`pullOf` 使用 `fishFightPullFor`，历史渔获构造 `RolledFish{Species,WeightG,Grade}` 后重算；先给每条渔获算一次拉力再排序，避免 comparator 反复查库。
- f9/f16：按客户端 UI 语义分别为 Risky Fish / Bottom Biggest Fish；当前数据不足以可靠计算，继续发送 `null`。
- f14/f15：按 `item_def.category==17` 判定拟饵，定义缺失时回退 `spinner` 前缀；f14 暂沿用“非拟饵”旧口径，避免引入未证实的底钓推断。
- f17/f18/f21：竿/钓组/诱饵聚合与卡片钩品字段同步使用同一 `isLure`。
- 兼容：客户端基线 4.0.25034；无协议形状/字段类型变化，无迁移。

## Testing Decisions

- handler 级：`TestFishStats321FromCatchLog` 使用 `plug_m_1833` 验证非 `spinner` 前缀拟饵进入 f15。
- f8 fixture 必须满足“最大重量鱼 ≠ 最大拉力鱼”，否则旧实现也会通过。
- 明确断言 f9/f16 为 `KindNull`。
- 性能：临时 10,000 条 `catch_log` 基准从约 5.59s 降至约 0.58s（仅 `buildStats321`）。
- 回滚后：f8 会重新等于最重鱼、f15 会漏掉 plug 类拟饵，测试变红。

## Out of Scope

- 不补 `catch_log` 的钓法/钓组快照，不实现底钓最大鱼。
- 不实现 Risky Fish 的真实业务判定。
- 不改统计页字段类型、args 容器或客户端协议。

## Further Notes

- [事实] `jahbacemfof.cmjcbaheigb` 中 f9 读取 DTO `+0x28`、f16 读取 `+0x98`；客户端 `RUIStatisticsMainPanel` 分别挂到 Risky Fish 与 Bottom Biggest Fish 行。
- [事实] `plug_m_1833` 在静态库为 category 17，旧 `spinner` 前缀逻辑无法识别。
- [推断] f14 暂沿用非拟饵口径；待增加可靠钓法快照后再拆浮钓/底钓。

# 钓组装配 / 高频陷阱清单

> 用法：改 13/15/16/17 命令族、拟饵钩位、多竿状态、装配投影时对照本页。
> 机制入口见 `docs/mechanism-map.md`；装配与结算指向同一尾鱼的红线见 `settlement-and-idempotency.md`。

## 1. 唯一写入口

- ✅ 13/15/16/17 全族的写入都走 `rigSetComponent`（参数走 `rigSetOpts`）；新增装配能力先在这里加分支。
  - ❌ 在 handler 里直接改 f5 或背包行 → 投影不同步，客户端渲染与背包内容对不上。

## 2. f5 是装配真源，`rig_component` 是单向投影

- ✅ 一致性靠投影重建（`reconcileRigProjection`、`BackfillRigProjections`），子件键是
  `rigCompKey(category, ordinal)`，子件 GUID 由稳定派生得到。
  - ❌ 双向互写，或另造一套"装配指纹" → 两套真源漂移。
    代码里**没有**名为 fingerprint 的实现，搜不到是正常的，不要因此新增。

## 3. 拟饵钩位不在竿上

- ✅ 拟饵的钩子存在拟饵自身 f0 的 `args(60)` 数组里（见 `lure_hooks.go` 文件头）；
  15/1、15/2 改的是实例字段，卸钩 = 数组缩短。
  - ❌ 按竿的 f5 / `rig_component` 路径改拟饵钩 → 钩从背包摘走了，拟饵仍显示出厂钩。
  - ❌ 用"保长度的空槽"表示未装钩 → 客户端槽位行崩；三种保长度写法都已实测崩过。
  - ✅ 出厂钩、恢复钩、幽灵槽位是三个不同来源（`lureFactoryHookKey` / `lureRestoredHookKey` /
    `lurePhantomHookKey`），改钩位要分清改的是哪一个。

## 4. 多竿状态按竿隔离

- ✅ 带竿 GUID 的请求一律用 `fishingStateForRod`；`fishingState(uid)` 只是手持竿兜底。
  - ❌ 用玩家级兜底状态处理带 GUID 的请求 → 后抛的竿覆盖前一根的计时器与待发鱼
    （`specs/changes/multiple-fishing-rods`）。
  - ✅ 放竿保留原鱼与计时器，只读鱼竿不推进物品版本。

## 5. 装配与耐久是耦合的，不是分层的

- ✅ 断件/磨损路径直接读写钓组行；改装配必跑 `rig_ordinal_durability_test.go` 与
  `rig_dup_index_test.go`。
  - ❌ 只断言"装配命令返回成功" → 断件后出现幽灵子件或重复序号，玩家装配面板才暴露。

## 提交前快速自检

- [ ] 写入只经过 `rigSetComponent`，没有旁路改 f5 / 背包
- [ ] 改到拟饵钩位时确认改的是实例字段，且卸载用了数组缩短
- [ ] 带 GUID 的路径没有落到玩家级兜底状态
- [ ] 跑过装配与耐久的两条耦合测试

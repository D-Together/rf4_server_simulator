# 装备载荷与断损 / 高频陷阱清单

> 用法：改轮子泄力、线载、断线扣米、搏鱼磨损、耐久与修理时对照本页。
> 机制入口见 `docs/mechanism-map.md`；断竿/跑鱼的终态清理见 `settlement-and-idempotency.md`。

## 1. 量纲：牛进、公斤比

- ✅ 客户端上报的鱼拉力是**牛**，服务端换算成公斤力后再与泄力、锁轮承力比较；
  这条边界由 `TestFightReelWearBoundaryUsesKgfNotNewton` 锁定。
  - ❌ 把牛数直接当公斤灌进磨损账 → 任何 ≥ 约 0.3kg 的鱼都判超限，全程打滑或全程过载
    （`specs/changes/reel-wear-pull-unit` 的实测根因）。

## 2. 断线按实际放线量扣

- ✅ 断主线、剪线都扣**实际放出米数**，扣线与终态清在同一次事务里
  （`breakLineOrRodWithLineOut`、`lineLossTx`、`cutLineTx`）。
  - ❌ 按线杯容量或配置上限扣 → 只放了 5m 却掉 100m（`specs/changes/line-break-loss-m`）。

## 3. 14/8 负载仍只观测

- ✅ 搏鱼期的 14/8 负载只做字段观测（`reel_load_probe.go`），不参与磨损与结算；
  关掉开关只是少几个诊断字段。
  - ❌ 拿字段语义尚未定案的负载去调结算 → 一次观测误差直接变成经济结果。

## 4. 轮体耐久是零件汇总

- ✅ 轮体耐久由零件寿命汇总（`reelPartWearFor` → `recalcReelBodyDurability`），修理按零件计价。
  - ❌ 直接加减轮体耐久 → 与零件表对不上，修理后出现"已修好但寿命仍坏"。

## 5. 满红不爆只挡断件

- ✅ 钓具助手开关只影响断件判定，不改变磨损累积、跑鱼判定与结算口径
  （见 `specs/changes/gear-assistant-protection`）。
  - ❌ 顺手接成"免磨损" → 玩家可以无限钓不修。

## 6. 承力上限的口径

- ✅ 竿、主线、前导、轮各自的承力上限各算各的，取最小值判定（`gearWeightCapKg`、
  `gearWeightCapForRig`、`fishWeightCapKg`）。
  - ❌ 只按竿或只按线算 → 换轮后爆线阈值不变，或反之。

## 提交前快速自检

- [ ] 新数值走公斤力口径，没有把牛直接当公斤
- [ ] 断线/剪线的扣米数与实际放线量一致，且与终态清在同一事务
- [ ] 改耐久只改零件汇总，没有单点写轮体
- [ ] 满红不爆改动没有牵连磨损与跑鱼

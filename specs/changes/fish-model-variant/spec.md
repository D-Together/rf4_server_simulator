# 鱼模型族下发（补齐多外观，同一尾鱼多处一致）

Status: ready

## Problem Statement

同一条鱼在客户端的不同界面显示成不同模型：溜鱼时是一个样子，收鱼结算弹窗和渔护里又是
另一套，稀有鱼面板（3/17）、统计面板（3/2）和任务/资格面板里的鱼干脆永远是同一个「老模型」。

原因不是资源缺失——客户端 `fishes` 资源包里每种鱼本来就有多个族（大西洋鲑有 `f1~f3` 雌、
`m1~m3` 雄、`parr`、`smolt`），每族都给了 small/medium/large 三套 body/mesh/skin。是服务端
从来没告诉客户端「用哪个族」：那几个字段一直是空的，客户端只能退回每个鱼种的**默认族**，
而默认族是历史遗留的混拼资源（`a_salmon` = `a_salmon_smolt_s` 皮肤 + `a_salmon_m3_m` 网格 +
`a_salmon_f3_b` 身体）。

## Solution

按鱼种选一个真实存在的族名下发：雌雄由这尾鱼的身份串推导，期数按体重在「入档线 → 蓝冠线」
之间的相对位置取。同一尾鱼在溜鱼、结算、渔护、鱼卡、3/17 面板、3/2 统计、24/22 社交、
资格鱼表里得到同一个族名。

## User Stories

1. 作为玩家，我希望同一尾鱼在溜鱼、结算、渔护和各个面板里长得一样，不要换个界面就变个样。
2. 作为玩家，我希望大西洋鲑这类分期的鱼能出现雌雄与不同成熟度的外观，而不是永远一个模型。
3. 边界：老存档里 `catch_guid` 为空的鱼，反复刷新面板也不会换性别。
4. 边界：运维把总开关置 `0` 之后，已经在线上的活跃鱼与已加载的渔护条目立刻回到默认族。
5. 边界：没有变体族的鱼种，字段保持 `null`（不能写空串，客户端对两者的兼容路径不同）。

## Implementation Decisions

- 映射与选择：`internal/gameserver/fish_variants.go`
  - `fishModelVariants`：鱼种 key → 可选族（雌/雄各一组，另加 `parr` / `smolt`）。
    注意美拟鲈的**鱼种 key 带点**（`a.sleeper`），资源族名才是 `a_sleeper`。
  - `fishVariantSeed`：取身份串。优先 `catch_guid`；为空时退回内容种子
    `鱼种|克重|体长毫米`；全缺时返回空串。
  - `fishVariantMale`：对身份串做 fnv64a 哈希取一位。空身份串**固定按雌鱼**，不再 `rand`。
  - `pickFishModelVariant`：`parr` / `smolt` 需同时满足体重阈值与体长 ≤ 0.35m
    （这两族三档尺寸槽全指向同一个 `*_s` 鱼苗模型，体长落到 medium/large 槽会让渔护列表
    缩略图渲染成空白）；其余按 `(weight - qualified) / (blue - qualified)` 分期。
    奖杯线不参与选族。
  - `fishModelVariantLines`：只缓存 `{入档线, 蓝冠线}`，渔护一次序列化上百条鱼不能每条查库。
- 下发点（都是同一入口 `fishModelVariantForFish`）：27.f22（溜鱼）、25.f11/f12（结算/渔护，
  命名鱼把玩家名字让到 f12）、320.f13（3/17 与 3/2 纪录卡）、5005.f5/f6（任务/资格鱼表）、
  24/22.f11（社交纪录）。
- 开关：`fish_model_variant`（默认 1）。关闭时 `fishModelVariantFor` 与 `fillKeepnetVariant`
  会把已缓存的 `ModelVariant` 清空，保证改库 30 秒内生效；渔护条目只有内存缓存，
  库里不持久化这个字段。
- 数据与迁移：不改表、不改 `catch_log` 结构，纯读 + 计算。
- 兼容与回滚：客户端基线 4.0.25034。回滚把 `fish_model_variant` 置 `0` 即可（不动代码、
  不重启），所有下发点当次请求即回到默认族。

## Testing Decisions

- 纯逻辑（`fish_variants_test.go` / `fish_record_model_test.go`）：
  - `TestPickFishModelVariantStages`：期数随体重单调后移，边界取端点。
  - `TestFishModelVariantJuvenileNeedsLength`：体重在阈值内但体长超限时不用鱼苗族。
  - `TestFishModelVariantStablePerFish`：同一 GUID 反复调用族名不变。
  - `TestFishVariantSeedFallbacks`：GUID 缺失走内容种子、全零 GUID 视为缺失、空种子固定雌鱼。
  - `TestFishModelVariantDottedSleeperKey`：`a.sleeper` 命中、`a_sleeper` 不命中。
- 协议/行为（真实 SQLite + 真实构造函数）：
  - `TestRecordCard320CarriesModelFamily` / `TestRecordCard320StableWithoutCatchGUID`：
    `catch_guid` 为空时 320.f13 反复读一致，且与 3/2 统计、渔护 25.f11、资格鱼表 5005.f5 逐字相同。
  - `TestFishModelKeySharedByEntityAndKeepnet`：27.f22 与 25.f11 同一个族名。
  - `TestFishModelVariantTunableOffClearsCache`：运行中置 0，活跃鱼与渔护条目的族名立刻清空。
  - `TestFishModelVariantOffKeepsOldWire`：关掉后 27.f22 / 25.f11 回到旧行为（名字）。
- 期望值来源：族名清单来自客户端资源包里的族名枚举 + 线上真实鱼（42.5kg 拉多加鲑等）；
  分期期望值用手算的 `(w - 入档) / (蓝冠 - 入档)` 区间。
- 回滚后变红：把 `fishVariantSeed` 改回「空 GUID → rand」，
  `TestRecordCard320StableWithoutCatchGUID` 会随机失败；把开关判断挪回缓存之后，
  `TestFishModelVariantTunableOffClearsCache` 失败。

## Out of Scope

- 不给客户端新增族名：只使用资源包里已存在的族（`bs_salmon` 两族保留条目，但物种表里没有
  用这个 key 的鱼种，现在不会命中）。
- 不做「按天气/季节/产卵期切换外观」这类玩法逻辑：期数只按体重区间取。
- 不往库里持久化 `ModelVariant`：渔护条目每次加载重算即可。

## Further Notes

- `[事实]` 客户端 `fishes` 资源包按族提供三套尺寸资源，取族字段在三个界面上同名
  （`oajeeklppia`），取不到就退回鱼种默认族。
- `[事实]` 默认族是历史混拼资源（`a_salmon` 那个组合），也就是玩家说的「淘汰的老模型」。
- `[事实]` 本服 `catch_log` 有大量 `catch_guid` 为空的旧行（`CatchRecord.GUID` 来自
  `COALESCE(catch_guid,'')`，没有兜底值）。
- `[推断]` 期数按体重线性分期与实际资源命名（`f1..f3` 体色逐渐变深）一致；没有逐族抓包
  验证过每个区间边界。
- `[未验证]` 25.f11 / 5005.f5 的语义迁移只有服务端断言与既有实机观察，没有逐帧比对客户端
  实际读取结果；若客户端优先读 25.f12，命名鱼会退回名称、非命名鱼仍取族名。

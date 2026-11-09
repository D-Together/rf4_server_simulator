# 拟饵钩槽官方短缩语义与装拆事务化

Status: done

## Problem Statement

玩家反馈拟饵钩槽三类问题：多槽拟饵换钩永远顶替第一颗、卸下的钩从背包列表消失、钩全部卸下后详情页没有钩槽无法再装。多轮实机排查后确认根因是服务端用错了两套客户端契约：15/1 请求的槽位下标读错位置（读到容器枚举），以及用「保数组长度 + 空槽占位」对抗客户端的短缩渲染模型（0x00 null 元素槽位行渲染即崩、0xFF elem_null 登录快照打死客户端、幻影对象被渲染成真钩）。此外旧实现的写库路径分步提交且吞错，存在出厂钩复制面；出厂钩身份按当前数组下标识别，短缩后下标漂移导致漏退。

## Solution

拟饵钩位改用官方短缩语义：卸钩 = 元素从 gpoaffnjbdo 数组移除，全卸空时客户端物品状态行自然显示「钩未安装」；装钩按请求尾部 int 的 0 基槽位下标定位，下标超出当前元素数视为追加（以定义出厂钩槽数封顶）。装/拆的全部写库（拟饵 f0 写回、钩行归属变更、出厂钩退包入库、幽灵组件行与 bait_hook 关联清理）合并为单事务，任何一步失败整体回滚。

## User Stories

1. 作为玩家，我希望往多槽拟饵的指定槽装钩/拆钩落到我点的那个槽，以便按需搭配钩组。
2. 作为玩家，我希望卸下的钩立即回到背包、原装出厂钩卸下时物化退包一次，以便回收利用。
3. 边界：客户端行控件在数组缩短后不重建仍发旧行号——装钩越界下标按追加处理、拆钩越界/GUID 不在数组一律拒绝，绝不兜底到第一颗。
4. 边界：卸钩不回填任何占位钩（三种保长空槽表示均崩客户端，已实测排除）；写库失败时整体拒绝，不产生部分状态。

## Implementation Decisions

- 模块：internal/gameserver 的 lure_hooks.go（15/1/15/2 的拟饵分支）、rigops.go（hLureSetComp/hRigRemoveCompFamily 的三段响应与窄推送）、materialize.go（单槽出厂钩物化 lureHookAt）、item_repair.go（启动迁移）、rigmeta.go（活动钩解析）。
- 协议：15/1 请求尾部 int 是 0 基槽位下标（客户端 dump：agfonbnkeoh.dihplofkffm 循环建槽控件、bbpmdcodcee.hbckkobipmd 尾参发送）；响应装=镜像 17/2（f0=新装钩+退包、f1=拟饵+旧钩）、拆=镜像 17/3（f1=退回钩+拟饵）；仅「带物品行的玩家钩从拟饵挪回背包」补 4/21+4/22 重建列表归属（f1 更新段不重建列表归属）。
- 出厂钩身份：原装命名空间（uuidLike(lureGUID, "gpoaffnjbdo"+i)）全 key 空间匹配（lureOriginalFactorySlot），与当前数组下标无关——短缩后下标漂移不漏退；每颗原装钩的合成 GUID 全局唯一，退包天然只发生一次，无循环刷钩面。
- 事务与回滚：拟饵 f0 写回（updateItemFieldsTx）、新钩/旧钩行归属（UPDATE item / DetachReturnToBackpackTx）、旧钩字段清理、出厂钩退包入库（grantPreparedItemTx，prepareNewItem 在事务外只读准备）、rig_component 幽灵行与 rod_setting bait_hook 关联清理，单事务提交；失败回滚后服务端拒绝（15/1 res.Rejected / 15/2 返回 nil），客户端状态不变。活动钩下标 f1 越界回落 0 合并进同一次写回。
- 提交快照：Commit 在事务内重读钩行并重新检查耐久；拟饵内嵌钩树按最新钩行字段重建，不复用 Prepare 快照，避免预检后耐久/字段变化导致 f0 与物品行不一致。
- 活动钩关联：装/拆后按最新 f1 重算 `bait_hook:<拟饵GUID>`；非活动槽装/拆保持原关联，活动钩变化才更新，数组清空才删除。
- 数据与迁移：不改表结构。KV 一次性迁移 migration:lure_hook_slots:v5 剥离 v3~v7 实验残留（:r 垫底钩/:e 幻影空槽，与下标无关的全空间匹配）；空数组/短数组为合法状态不补齐。
- 兼容：客户端版本基线 4.0.25034；旧档（保长空槽、缩短数组）由启动迁移收敛；回滚见下。

## Testing Decisions

- 行为层（真实 SQLite）：TestLureHookSwapRewritesFactoryHookList（换钩改 f0）、TestLureHookRemoveReturnsHookToBackpack（短缩+退回背包）、TestLureHookRemoveRefundsFactoryHookOnce（原装退包一次+空饵重装追加）、TestLureHookMountTargetsRequestedSlot（槽位下标定位）、TestLureHookMultiSlotAppendAndStaleRowGuard（追加/封顶/陈旧行拒绝）、TestLureHookOriginalRefundAfterShortenDrift（短缩漂移后替换原装钩仍退包）、TestLureHookMountRollsBackOnWriteFailure（UPDATE 触发器注入失败：不退包、不拆旧、数组不变）、TestLureHookNonActiveSlotPreservesBaitHookSelection（非活动槽不改选择）、TestLureHookMountUsesCommitTimeHookState（事务内最新耐久写回 f0）、TestLureHookMountRejectsBrokenCommitState（Prepare 后损坏拒绝）。
- 存储层：TestRepairLureHookSlotsStripsLegacyArtifacts（残留剥离幂等）。
- 期望值来自实机日志与客户端反编译证据（槽位下标来源、空槽三态崩溃记录、kcgjlnmdbol()==Length 判定「钩未安装」），不是代码自证。
- 回滚后变红：RollsBackOnWriteFailure（回到分步提交即失败）、OriginalRefundAfterShortenDrift（回到下标单点匹配即漏退）、MultiSlotAppend（回到活动钩兜底即顶替第一颗）。

## Out of Scope

- 不改竿/轮装配（17/x）与手持工具（13/x）路径的响应形状。
- 不实现客户端空槽渲染（受客户端视图能力限制，官方即为短缩模型）。
- 不恢复官服完整的出厂钩与物品行历史语义差异（出厂钩无独立行、退包为本地拍板的物化方案）。

## 回滚方案

二进制回滚：bin 目录保留按时间戳的 .bak（lurehook1~9 系列），拷回即回滚；回滚版本会把已缩短数组视为合法状态（短缩前版本会启动迁移把空数组重建为出厂钩，重登后自愈为可玩状态）。数据回滚：migration:lure_hook_slots:v5 只删除 :r/:e 合成 GUID 元素（无对应物品行），不触碰真实钩/退包行，无需逆向。已退包进背包的出厂钩实例是正常物品行，回滚不回收（一次性让利，不可循环）。

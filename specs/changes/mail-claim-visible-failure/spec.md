# 邮件附件领取失败必须可见：19/3 回业务错误 + 后台发件前置校验

Status: done

<!-- 状态取值：draft | ready | in-progress | done | cancelled
     完成时原地改这里，不要归档、不要搬家、不要另开第二份。 -->

## Problem Statement

管理员在后台用"邮件"给玩家（UID 1000001）发了一封活动奖励邮件，附件是 `AV_shamp1_pack_6`（香槟礼盒）数量 99999。玩家上线后能在邮箱里看到这封信，点击"领取"后界面无任何错误，但：

1. 邮件仍是**未读**，附件仍在信里；
2. 背包里**没有**这件奖品；
3. 后台玩家邮箱页也仍然显示"未读"。

玩家视角是"我点了、它没反应、东西也没到"，而且没有任何提示，属于静默失败——比直接报错更糟，因为玩家会反复点、反复没结果，最后只能来问服主。

服务端日志（`logs/gameserver.log:5477`，同一行在 2026-11-09 21:42、22:18、23:07 与 2026-11-10 12:49、13:47 共出现 5 次，对应玩家每次点击领取）：

```text
level=WARN msg="19/3 礼盒附件数量超过上限" userid=1000001 sid=1 cmd=19/3 call=38 \
  mail=2 item=AV_shamp1_pack_6 qty=99999 limit=10000
```

线上库 `data/rf4server.db` 的 `mail` 行原样未动（`id=2, read=0, items_json` 仍是 `[{"item_id":"AV_shamp1_pack_6","qty":99999}]`，`attachment_snapshot_version=0`），背包无该物品，印证"领取根本没发生"。

### 为什么现在挡不住

- **领取侧**：`acceptMails` 在准备阶段遇到超限附件时直接 `return 0, nil`；`hMailAccept` / `hMailAcceptAll` 无论返回值如何都回 `[]byte{0x00}`（null 成功）。客户端拿到"成功"，因此不弹错误、不刷新，表现成"点了没反应"。同一条路径上任何失败（附件定义不存在、快照损坏、入包失败、事务提交失败）都是同样的静默成功。
- **发件侧**：`POST /api/mail/send` 与 `execAdminCmd("send_mail")` 只校验"正文或附件非空"和 `qty <= 0 → 1`，不校验物品是否存在、是否是组合包、包数是否超过 `maxBundlePreparedItems`（10000），也不校验数量槽上限。于是后台能造出一封**永远领不出来**的邮件，且当场提示"已下发"。
- 两者叠加，缺陷在玩家侧才爆，服主侧完全无感。

## Solution

发得出、也领得到；领不到时必须当场告诉玩家错在哪，并且服主不可能再把这种邮件发出去。

1. 玩家点击领取时，只要有一件附件发不出去，就回业务错误信封（客户端弹 `kOperationResult_*` 文案），邮件保持未读、附件保留，可重试；不再回"假成功"。
2. 后台发件时先按领取路径同样的规则做一次干跑校验：物品不存在、组合包包数超上限、数量槽溢出，直接在后台报错并拒绝入队，不产生坏邮件。
3. 已经存在的坏邮件（例如本缺陷留下的 `mail id=2`）可由服主在后台直接删除，或按修正后的数量重发。

## User Stories

1. 作为玩家，我点击领取一封能领的邮件时，附件进背包、邮件变已读，和现在一样。
2. 作为玩家，我点击领取一封领不出来的邮件时，客户端弹出明确错误提示，邮件保持未读、附件不丢，我可以找服主处理。
3. 作为服主，我在后台发邮件时如果附件数量/物品非法，保存当即失败并告诉我原因，而不是"已下发"然后石沉大海。
4. 作为服主，我能删掉历史坏邮件并重发正确的一封，玩家不再被卡住。
5. 边界：一键领取（19/4）里若混有可领与不可领的邮件，可领的照常入包并置已读，不可领的保持未读，并回一次错误提示。
6. 边界：同一 `call_id` 重放不会二次发奖（沿用既有回执表）。

## Implementation Decisions

- 模块：`internal/gameserver`（领取链路）、`internal/admin`（发件校验）、`internal/admin/web`（前端错误提示与删除入口）。
- **领取侧改为返回错误**：把 `acceptMails` 的失败分支从 `return 0, nil` 改造成显式错误返回（保留"全部成功/部分成功/全失败"的区分），`hMailAccept`、`hMailAcceptAll` 在失败时回失败信封而不是 null。19/3、19/4 的响应合同本来就是 `null`，失败信封走 `-3` 通道，不改响应形状、不动 args code。
- **错误信封形态（经三轮实机验证后定稿）**：用 `System.InvalidOperationException` + 中文 message，
  **不用** `RF4.API.APIError` + 数字结果码。理由见 Further Notes 的实测记录。
- **发件侧干跑校验**：抽一个与领取路径共用规则的校验函数（组合包包数上限、内含物展开可行性、数量槽上限），在 `handleMailSend` 入队前执行；失败回 `400` + 中文原因，不写队列、不写审计"已下发"。
- **存量坏邮件**：后台玩家邮箱页增加删除单封邮件的能力（管理指令 `delete_mail`），让服主能自愈。
- 数据与迁移：不改表结构、不迁移。`attachment_snapshot_version` 语义不变。
- 兼容：客户端基线 4.0.25034；只把"假成功"变"真失败"，成功路径零改动。回滚 = revert 本分支，回滚后静默失败行为恢复。

## Testing Decisions

- 存储/行为（真实 SQLite，复用 `newEconomyTxTestServer` 与 `newBundleTestServer` 接缝）：
  - `19/3` 领取超上限礼盒附件 → handler 返回失败信封（`System.InvalidOperationException` + 中文 message，按字节精确比对），且 `mail.read` 仍为 0、`items_json` 未清空、背包无新行。
  - `19/3` 领取正常附件 → 仍回 null，`mail.read=1`、`items_json` 清空、背包有物品、收到 `4/22` 推送（既有测试保持绿）。
  - `19/4` 混合场景 → 可领的入包置读，不可领的保持未读，回一次错误。
- 管理端（`internal/admin`，复用 `features_test.go` 的 DB + `s.mux.ServeHTTP` 接缝）：
  - `POST /api/mail/send` 带超上限包数 → `400`，队列里没有 `send_mail`（沿用 `assertQueueContains` 的反向断言）。
  - `POST /api/mail/send` 带不存在的 `item_id` → `400`。
  - `POST /api/mail/send` 合法请求 → `200` 且队列有 `send_mail`（既有测试保持绿）。
- 期望值来源：超限阈值直接取 `maxBundlePreparedItems` 与实际 `mail` 行状态（`read`/`items_json`）——不是"用同样代码再算一遍"。
- 回滚后哪些测试会变红：`19/3`、`19/4` 的"失败必须回错误"两条，以及 `admin` 的两条拒绝入队断言。

## Out of Scope

- 邮件正文/附件展示协议改动（11062/11063 字段不动）。
- 玩家之间互寄（19/1、19/2）的既有语义。
- 礼盒开盒（4/8）流程。
- 附件数量上限本身是否该提高（`maxBundlePreparedItems` 保持 10000；本缺陷是"超限要报错"，不是"允许超限"）。
- 后台邮件页的整体重做，只补发件校验反馈与单封删除。

## Further Notes

- `[事实]` `logs/gameserver.log` 中 `19/3 礼盒附件数量超过上限` 共 5 条，横跨 2026-11-09 21:42 至 2026-11-10 13:47，`mail=2`、`qty=99999`、`limit=10000`（实机日志）。
- `[事实]` 线上库 `mail id=2`：`read=0`、`attachment_snapshot_version=0`、`items_json=[{"item_id":"AV_shamp1_pack_6","qty":99999}]`，背包无对应行（`server_data`/`data/rf4server.db` 只读查询）。
- `[事实]` `AV_shamp1_pack_6` 在静态库为 `category=55`、`attrs_json.ieopbeejfmc=true`（礼盒），内含 `AV_shamp_1l × 6`，因此走 `mailbox.go` 礼盒分支并被 `maxBundlePreparedItems` 拦截（`server_data/static.sqlite3`）。
- `[事实]` `hMailAccept`/`hMailAcceptAll` 原实现无条件 `return []byte{0x00}, true`（`internal/gameserver/mailbox.go`）。
- `[事实·实机]` **65 号不是 `kOperationResult_CreatingItemError`**：2026-11-10 15:21 实机点击
  （`logs/gameserver.log` 有 `19/3 领取附件失败`，客户端 `Player.log` 有
  `dchjlkidjdc: 没有足够的技能点来升级能力`）证明客户端把 65 解析成
  `AbilityPointsNotEnought`。`internal/wiregen/tables.go:3430` 的枚举注解与客户端真实取值不符，
  **该注解不能作为取码依据**；不要再用它推导任何 `abadjfbfdjb` 取值。
- `[事实·实机]` `System.InvalidOperationException` + 中文 message 的信封**能送达客户端**：
  15:40 实机点击后，客户端 `Player.log` 完整记录了
  `System.InvalidOperationException: 邮件附件领取失败：礼盒附件数量 99999 超过上限 10000…`。
  但 UI 弹窗只显示通用文案「发生内部错误。如果问题仍然存在，请联系客服。」
  —— 客户端不把 message 渲染进弹窗，只写日志。
- `[事实]` `campfire.go` / `econ.go` / `fish_query_contact.go` 等既有路径同样用
  `ReplyFailure("System.InvalidOperationException", 中文)`，因此它们的中文 message
  同样只落客户端日志、不显示在弹窗里；之前隐含的"能显示"假设不成立。
- `[结论]` 三轮实机对比后选定当前形态。弹窗文案「发生内部错误，请联系客服」虽不精确，
  但方向正确（引导玩家找服主），且不像 65 号那样把玩家引向"去点技能"的错误操作。
  在没有客户端源码/元数据（`global-metadata.dat` 不存在）的前提下，这是可达到的最好效果。
- 未验证：礼盒附件数量上限 10000 是否与官服一致（本缺陷不依赖该结论）。
- 未验证：客户端是否存在某个能透出 message 的异常类型（枚举表与客户端取值对不上，
  且缺少反编译源码，无法系统性穷举）。

## Review 修订（2026-11-10 21:10，PR #374 审查意见）

审查给出 3 条真实缺陷，均已修复并用变异测试证明测试有效：

- **P1 删除邮件 id 进制错配**：`handlePlayerMailDelete` 按十进制入队 `"10"`，
  消费端却用 `mailGUIDToID`（十六进制）解析，把 10 读成 16 → 删错邮件，
  后台仍回 `{"ok":true,"queued":true}`。修复：新增 `parseMailIDArg`，
  32 位纯十六进制按 GUID 解析、其余按十进制；`delete_mail` 改用它。
  变异测试：把消费端改回 `mailGUIDToID` 后
  `TestDeleteMailDecimalIDNotParsedAsHex` 变红并准确报出"十进制 id=10 的邮件应被删除"。
- **P2-a 发件校验只看形式**：旧 `bundleDefinitionProblem` 只检查内含物 ID 是否为空，
  把内含物改成不存在的 item_id 仍能 200 入队，玩家领取必然失败。
  修复：新增 `gameserver.ValidateMailAttachment`，按领取路径同源规则做只读 dry-run
  （内含物定义存在、非嵌套组合包、类别可映射、数量/实例数不越界）；
  admin 侧不再自己维护一套规则，直接调用它，避免两边漂移。
  变异测试：复刻旧实现后 `TestMailSendRejectsMissingBundleInnerItem` 变红，
  输出 `实得 200: {"ok":true,"queued":true}` —— 与审查复现完全一致。
- **P2-b 数据库错误被当成"没有邮件"**：`acceptMails` 的 `MailGet` 把
  `err != nil || m == nil` 一律 `continue`；`hMailAcceptAll` 的 `MailList` 失败
  直接回 `null`。这与"失败必须可见"自相矛盾。
  修复：`MailGet` 仅把 `sql.ErrNoRows`/`m == nil` 视为空操作，其余错误返回；
  `MailList` 失败回 `-3` 错误信封；新增 `TestMailAcceptAllReportsErrorOnListFailure`。

门禁：`go build` / `go vet` / `go test ./...` 全绿（gameserver 135.7s、admin 4.6s），
`gofmt -l` 无差异，行尾统一 LF 与仓库 blob 一致。

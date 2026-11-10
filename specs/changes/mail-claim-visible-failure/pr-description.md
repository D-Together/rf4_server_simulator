# 邮件附件领取失败不再静默回成功

## 问题

服主在后台给玩家（UID 1000001）发了一封活动奖励邮件，附件是 `AV_shamp1_pack_6`（香槟礼盒）× 99999。玩家上线后能在邮箱看到这封信，点击领取后：

1. 邮件仍是**未读**，附件仍在信里；
2. 背包**没有**该奖品；
3. 界面**没有任何提示**。

玩家视角是"我点了、它没反应、东西也没到"，因为没有任何反馈，玩家会反复点、反复没结果，最后只能来问服主。

### 根因

服务端日志（`logs/gameserver.log`，同一行在 2026-11-09 21:42 至 2026-11-10 13:47 共出现 5 次，对应玩家每次点击）：

```
level=WARN msg="19/3 礼盒附件数量超过上限" userid=1000001 cmd=19/3 mail=2 \
  item=AV_shamp1_pack_6 qty=99999 limit=10000
```

`acceptMails` 在准备阶段因包数超过 `maxBundlePreparedItems`（10000）放弃发放，**但 `hMailAccept` / `hMailAcceptAll` 无论返回值如何都回 `[]byte{0x00}`（null 成功）**。客户端拿到"成功"，因此不弹错误、不刷新，表现成"点了没反应"。

同一条路径上，附件定义缺失、快照损坏、入包失败、事务提交失败，全部是同类静默失败。

**发件侧同样没有防线**：`POST /api/mail/send` 只校验"正文或附件非空"和 `qty <= 0 → 1`，不校验物品是否存在、包数是否超上限，因此后台能造出一封**永远领不出来**的邮件，并且当场提示"已下发"。

## 改动

### 领取侧：失败必须可见

`acceptMails` 改为返回 `error`，不再把失败吞成 `0`。

失败分两级处理：

- **单封邮件自身的校验失败**（附件定义缺失、快照损坏、数量超限等）→ 只跳过这一封，其余邮件照常发放
- **事务 / 数据库级失败** → 整体回滚

> 为什么分级：如果把"单封坏邮件"也算作整体失败，一封坏邮件会让"一键领取"永久卡死，玩家连好邮件都领不出来——那是用一个新的缺陷换掉旧缺陷。

`19/3`、`19/4` 失败时回业务错误，玩家可见。

### 发件侧：从源头不再产生坏邮件

后台发件前做一次干跑校验，规则与领取路径同源：

- 物品不存在
- 组合包包数超过 `maxBundlePreparedItems`
- 组合包定义损坏 / 没有内含物

校验失败直接 `400` + 中文原因，**不入队、不写"已下发"审计**。

### 前端与运维

- `sendMail()` 补 `try/catch`：原先校验失败是一个未处理的 promise rejection，界面上什么都不发生
- 玩家邮箱页新增**单封删除** + `delete_mail` 管理指令，供服主清理存量坏邮件

## 错误信封形态（经三轮实机对比选定）

这一点值得单独说明，因为踩了坑。

| 方案 | 客户端弹窗文案 | 判定 |
|---|---|---|
| 静默回 null（原缺陷） | 无任何反应 | ❌ 玩家完全不知道 |
| `RF4.API.APIError` + 结果码 65 | **"没有足够的技能点来升级能力"** | ❌ 严重误导，玩家会去点技能 |
| `System.InvalidOperationException` + 中文 message | "发生内部错误。如果问题仍然存在，请联系客服。" | ✅ 方向正确，引导找服主 |

**关于 65 号**：`internal/wiregen/tables.go:3430` 把 65 注解为 `kOperationResult_CreatingItemError`，但实机验证客户端把它解析成 `AbilityPointsNotEnought`。该注解与客户端真实取值不符，**不能作为取码依据**。

**关于 message**：中文 message 完整送达客户端（`Player.log` 有完整记录），但客户端只写日志、不渲染进弹窗。这是客户端 UI 的行为，服务端无法改变。

## 验证

### 门禁

```
go build ./...   BUILD_OK
go vet ./...     VET_OK
go test ./...    全部 21 个包 ok（gameserver 78.8s、admin 4.4s）
gofmt            LF 归一化后无差异
```

### 新增回归测试（回滚后会变红）

| 测试 | 锁定的行为 |
|---|---|
| `TestMailAcceptRejectsOverCapAttachmentWithError` | 超限附件 → 按字节精确比对失败信封；邮件保持未读、`items_json` 未清空、背包不新增 |
| `TestMailAcceptAllReportsErrorWhenAnyAttachmentFails` | 混合场景：好邮件照常入包置读、坏邮件保持未读、回一次错误 |
| `TestMailAcceptSucceedsWithoutFalseError` | 成功路径仍回 null（防止"为了可见失败"误伤正常领取） |
| `TestMailSendRejectsOverCapBundle` | 超上限包数 → 400 且不入队 |
| `TestMailSendRejectsUnknownItem` | 物品不存在 → 400 且不入队 |
| `TestMailSendRejectsBrokenBundleDefinition` | 定义损坏 → 400 且不入队 |
| `TestMailSendAcceptsValidBundle` | 合法礼盒照常 200 并入队 |
| `TestAdminDeleteMailRemovesOnlyTargetedMail` | 只删指定 id，不误伤同玩家其它邮件与他人邮件 |

### 实机验证

| 项 | 结果 |
|---|---|
| 后台发 99999 礼盒 | `400` + "数量 99999 超过单次领取上限 10000，玩家无法领取" |
| 后台发不存在的物品 | `400` + "物品不存在" |
| 玩家点击超限邮件 | 服务端 `19/3 领取附件失败`；客户端弹"发生内部错误，请联系客服"；邮件保持未读 |
| 后台删除坏邮件 | 日志 `管理指令：删除邮件 uid=1000001 count=2`，DB 确认删除 |
| 重发 qty=1 | 玩家成功领取、礼盒入包、拆开得 12 瓶香槟 |

## Review 修订（依据 PR 审查意见）

审查发现 3 条真实缺陷，均已修复，并用**变异测试**证明测试确实能抓到旧实现：

| 编号 | 问题 | 修复 |
|---|---|---|
| P1 | 后台按十进制入队邮件 id `"10"`，消费端按十六进制解析成 16 → **删错邮件**，后台仍回 `queued:true` | 新增 `parseMailIDArg`：32 位纯十六进制按 GUID、其余按十进制；`delete_mail` 改用它 |
| P2-a | 发件校验只检查内含物 ID 是否为空，改成不存在的 item_id 仍能 200 入队，玩家领取必然失败 | 新增 `gameserver.ValidateMailAttachment`，按领取路径同源规则做只读 dry-run（内含物存在、非嵌套、类别可映射、数量不越界）；admin 直接调用，不再维护两套规则 |
| P2-b | `MailGet` / `MailList` 的数据库错误被当成"没有邮件"吞掉，`19/4` 失败直接回 null，与"失败必须可见"自相矛盾 | `MailGet` 仅 `ErrNoRows`/`nil` 视为空操作，其余返回错误；`MailList` 失败回 `-3` 错误信封 |

新增回归（4 条）：`TestDeleteMailDecimalIDNotParsedAsHex`、`TestParseMailIDArgDistinguishesGUIDAndDecimal`、`TestMailSendRejectsMissingBundleInnerItem`、`TestMailSendRejectsNestedBundle`、`TestMailAcceptAllReportsErrorOnListFailure`。

**变异测试结果**：把消费端改回十六进制解析 → `TestDeleteMailDecimalIDNotParsedAsHex` 变红，报"十进制 id=10 的邮件应被删除"；把发件校验换回旧实现 → `TestMailSendRejectsMissingBundleInnerItem` 变红，输出 `实得 200: {"ok":true,"queued":true}`，与审查复现完全一致。

## 已知限制

1. **客户端弹窗文案不可定制**：只能显示通用文案「发生内部错误。如果问题仍然存在，请联系客服。」，服务端传入的中文 message 只落在客户端日志里。`kOperationResult_*` 枚举中没有"邮件附件发不出来"这一语义，找不到精确匹配的码。

2. **`tables.go:3430` 的枚举注解错误未修**：那是 wiregen 生成的协议资产，按仓库约定未擅自改动，留待维护者订正。建议后续确认 65 的真实语义后修正该注解。

3. **未验证**：是否存在某个能透出 message 的异常类型。客户端 `global-metadata.dat` 不存在、无反编译源码，无法系统性穷举。

## 复现步骤

1. 后台给玩家发一封 `AV_shamp1_pack_6` × 99999 的邮件（改动前可发，改动后会被 400 拦截）
2. 玩家上线点击该邮件领取
3. 改动前：无任何提示，邮件未读，背包为空
4. 改动后：弹出"发生内部错误，请联系客服"，邮件保持未读，服务端日志有 `19/3 领取附件失败`

## 回滚

`revert` 本 PR 即可。回滚后静默失败行为恢复（不涉及表结构、迁移、协议形状改动）。

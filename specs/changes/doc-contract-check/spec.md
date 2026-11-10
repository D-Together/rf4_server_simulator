# 文档契约自动对账（doccheck）

Status: in-progress

<!-- 验证：校验器与单测已完成；待 CI 跑满一周无假红后改 done。 -->

## Problem Statement

约束工程的两类漂移只能靠人发现，实际已经发生：

- `docs/` 下有 21 份文档，其中 10 份从未登记进 `docs/README.md` 索引。新读者（人与 Agent）按索引找不到它们，等于不存在。
- `specs/changes/*/spec.md` 的 `Status:` 约定为 `draft | ready | in-progress | done | cancelled`，实际出现过自由文本（`done (implementation and automated checks); client validation pending`、`implemented; partial client validation`），后续无法用脚本统计"哪些变更在飞"。
- 文档互相引用的 `docs/*.md` / `specs/*.md` 路径没有校验，改名或删除后引用变成死链，评审看不出来。
- 能力契约没有体量上限：`fishing-and-settlement.md` 曾长到 102 行、跨四个域，改一处要读全文，
  评审也看不出"这份又长了一点"。

现有机制挡不住的原因：L2 门禁只跑 gofmt/build/vet/test，全部是代码层；文档层没有任何可执行检查，`docs/agents/domain.md` 的对账表只能要求人自觉。

## Solution

增加一个零依赖的文档契约校验器，把"索引完整性、状态枚举、相对链接、文档路径引用、能力契约体量"五件事变成可执行判定：

- 本地：`go run ./tools/doccheck`，任何一条不满足即非零退出；`.githooks/pre-commit` 调用它，提交前拦住。
- CI：`go test ./internal/doccheck` 覆盖仓库本体，随现有 Test job 一起跑，不新增 job、不改测试口径。

## User Stories

1. 作为维护者，我希望漏登记一份新文档时门禁立刻变红，以便索引不会随时间腐坏。
2. 作为维护者，我希望 spec 状态只能是五个值，以便能脚本统计在飞变更。
3. 作为 Agent，我希望改文档名后死链当场被拦，而不是等到下次有人按链接点空。
4. 边界：仓库外依赖（`E:\RF4\...` 反编译资料）与网络链接不参与校验，不因本机缺资料变红。

## Implementation Decisions

- 新包 `internal/doccheck`：`Run(root)` 返回 `[]Issue` + 统计；规则常量独立，便于以后增删。
- 校验范围：`docs/**/*.md`、`specs/**/*.md`，加根目录 `CLAUDE.md`、`CONTEXT.md`、`CONTRIBUTING.md`。
- 五条规则：
  1. `doc-index`：`docs/` 根目录每份 `.md`（除 `README.md`）必须在 `docs/README.md` 中出现文件名。
  2. `spec-status`：`specs/changes/*/spec.md` 首屏 `Status:` 必须是五态枚举之一。
  3. `link`：Markdown 相对链接目标必须存在；跳过 `http(s)`、`mailto`、纯锚点、`<占位符>`。
  4. `ref`：正文里出现的 `docs/...md`、`specs/...md` 路径必须存在；跳过代码块内与含 `<`、`*`、`...` 的占位写法。
  5. `cap-size`：`specs/capabilities/*.md` 单份超过 80 行给 `note`（不影响退出码），
     超过 150 行报 issue（exit 1）。阈值常量在包内，改阈值需同步 `docs/agents/domain.md`。
- 代码块（``` 围栏）内的链接与路径不参与 `link`/`ref` 校验，避免把示例当引用。
- CLI 在 `tools/doccheck/main.go`（与 `tools/genwire` 同构），只做参数解析与退出码。
- 不改协议、不改表结构、不引入新依赖。

## Testing Decisions

- 测哪一层：单元（纯文件系统夹具，`t.TempDir()` 造最小仓库）+ 仓库本体一条集成断言。
- 接缝：`Run(root)` 是唯一被测入口，五条规则各一条"应报错"用例 + 一条"干净仓库零 issue"用例；
  `cap-size` 额外覆盖两个边界：刚到 80 行不提示、81~150 行只出 `note`、超过 150 行才判失败。
- 期望值来源：夹具里手写的坏样例（缺索引、非法状态、死链、死引用），不是"用同样代码再算一遍"。
- 回滚后哪些测试会变红：删掉 `doccheck` 包后 `internal/doccheck` 的测试与 `tools/doccheck` 一起消失，`go build ./...` 因 Makefile/hook 引用而失败——即回滚会显式报错，不会静默。
- 仓库本体断言：`TestRepositoryDocs` 允许 `t.Skip` 仅在找不到 `go.mod` 时触发（例如包被单独拷贝出去），正常检出必须跑真断言。

## Out of Scope

- 不做 Markdown 语法校验、不检查标题层级、不检查中文用词。
- 不校验 `specs/changes/<slug>/spec.md` 是否六段齐全（结构评审仍靠人）。
- 不校验外部链接可用性（网络不稳定，且本机代理策略因人而异）。
- 不迁移任何现有文档内容；本次只加检查并修掉它暴露的问题。

## Further Notes

- 证据：`docs/README.md` 缺 10 份文档、2 处自由文本状态，均为 2026-11-10 实测 `[事实]`。
- 外部参照：`AGENTS.md` 生态的共识是"能被工具强制的规则不要写进指令文件"，本改动即把三条文档规则从"靠人"移到"靠工具"。
- 若未来需要第五类规则（如能力契约必须在 README 枚举），在 `internal/doccheck` 加常量与该规则的夹具即可，不新建包。

package doccheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree 在临时目录里铺一份最小仓库。
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func issuesByRule(issues []Issue, rule string) []Issue {
	var out []Issue
	for _, is := range issues {
		if is.Rule == rule {
			out = append(out, is)
		}
	}
	return out
}

func TestCleanTreeHasNoIssues(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/README.md":          "# 索引\n\n- [机制](mechanism.md)：说明。\n",
		"docs/mechanism.md":       "# 机制\n\n回到 [索引](README.md)。\n",
		"specs/changes/a/spec.md": "Status: in-progress\n\n见 `docs/mechanism.md`。\n",
		"specs/changes/b/spec.md": "Status: done\n",
		"specs/capabilities/c.md": "# 契约\n\n见 `docs/mechanism.md`。\n",
		"CLAUDE.md":               "# 入口\n\n见 `docs/mechanism.md`。\n",
	})
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("期望零违规，实际 %d: %v", len(report.Issues), report.Issues)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("期望零提示，实际 %d: %v", len(report.Notes), report.Notes)
	}
	if report.Stats.Specs != 2 {
		t.Fatalf("期望扫描 2 份 spec，实际 %d", report.Stats.Specs)
	}
	if report.Stats.Refs == 0 {
		t.Fatal("期望统计到文档路径引用，实际 0（规则可能没生效）")
	}
}

func TestUnindexedDocIsReported(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/README.md": "# 索引\n",
		"docs/orphan.md": "# 孤儿文档\n",
	})
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := issuesByRule(report.Issues, RuleDocIndex)
	if len(got) != 1 || got[0].Path != "docs/orphan.md" {
		t.Fatalf("期望命中 docs/orphan.md，实际 %+v", got)
	}
}

func TestInvalidSpecStatusIsReported(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/README.md":          "# 索引\n",
		"specs/changes/a/spec.md": "Status: done (implementation); client validation pending\n",
		"specs/changes/b/spec.md": "Status: implemented; partial validation\n",
		"specs/changes/c/spec.md": "# 没有状态行\n",
	})
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := issuesByRule(report.Issues, RuleSpecStatus)
	if len(got) != 3 {
		t.Fatalf("期望 3 条状态违规，实际 %d: %+v", len(got), got)
	}
	if got[0].Line != 1 {
		t.Fatalf("期望行号 1，实际 %d", got[0].Line)
	}
}

func TestBrokenLinkIsReported(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/README.md": "# 索引\n\n- [在](here.md)\n- [不在](missing.md)\n- [外部](https://example.com/x.md)\n- [锚点](#section)\n",
		"docs/here.md":   "# 在\n",
	})
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := issuesByRule(report.Issues, RuleLink)
	if len(got) != 1 || !strings.Contains(got[0].Message, "missing.md") {
		t.Fatalf("期望只有一条死链命中，实际 %+v", got)
	}
}

func TestBrokenDocRefIsReportedAndPlaceholdersSkipped(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/README.md": "# 索引\n",
		"docs/agents/domain.md": strings.Join([]string{
			"# 约定",
			"",
			"| 单个机制 | `docs/<mechanism>.md` |",
			"",
			"见 `docs/gameplay.md` 与 `docs/gone.md:22`。",
			"",
			"```text",
			"docs/inside-fence.md",
			"```",
			"",
		}, "\n"),
		"docs/gameplay.md": "# 玩法\n",
	})
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := issuesByRule(report.Issues, RuleRef)
	if len(got) != 1 {
		t.Fatalf("期望只有 gone.md 一条死引用，实际 %+v", got)
	}
	if !strings.Contains(got[0].Message, "docs/gone.md") {
		t.Fatalf("命中错误：%+v", got[0])
	}
}

// TestCapabilitySizeSoftAndHardLimits 锁定 80 行提示 / 150 行失败的边界。
func TestCapabilitySizeSoftAndHardLimits(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/README.md":                  "# 索引\n",
		"specs/capabilities/small.md":     strings.Repeat("# 条目\n", capSoftLines),
		"specs/capabilities/soft-over.md": strings.Repeat("# 条目\n", capSoftLines+20),
		"specs/capabilities/hard-over.md": strings.Repeat("# 条目\n", capHardLines+1),
	})
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	hard := issuesByRule(report.Issues, RuleCapSize)
	if len(hard) != 1 || hard[0].Path != "specs/capabilities/hard-over.md" {
		t.Fatalf("期望只命中 hard-over.md，实际 %+v", hard)
	}
	if !strings.Contains(hard[0].Message, "必须拆分") {
		t.Fatalf("硬上限提示措辞异常：%s", hard[0].Message)
	}
	soft := issuesByRule(report.Notes, RuleCapSize)
	if len(soft) != 1 || soft[0].Path != "specs/capabilities/soft-over.md" {
		t.Fatalf("期望只提示 soft-over.md，实际 %+v", soft)
	}
	if report.Stats.Notes != 1 || report.Stats.Issues != 1 {
		t.Fatalf("统计异常：issues=%d notes=%d", report.Stats.Issues, report.Stats.Notes)
	}
}

// TestRepositoryDocs 把规则套在本仓库上：漏登记、非法状态、死链、超长契约必须当场变红。
func TestRepositoryDocs(t *testing.T) {
	root := repoRoot(t)
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Issues) != 0 {
		var sb strings.Builder
		for _, is := range report.Issues {
			sb.WriteString("  " + is.String() + "\n")
		}
		t.Fatalf("文档契约违规 %d 条：\n%s", len(report.Issues), sb.String())
	}
	if report.Stats.Docs == 0 || report.Stats.Specs == 0 {
		t.Fatalf("扫描范围异常：docs=%d specs=%d", report.Stats.Docs, report.Stats.Specs)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("找不到 go.mod，跳过仓库级断言")
		}
		dir = parent
	}
}

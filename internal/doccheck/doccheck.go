// Package doccheck 校验仓库的文档契约：文档索引、spec 状态枚举、相对链接与文档路径引用。
//
// 规则与验收口径见 specs/changes/doc-contract-check/spec.md。
// 本包只读文件系统，不依赖网络、不依赖 git，可直接在 CI 与本地钩子里跑。
package doccheck

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 规则标识。新增规则时同时补 Issue 夹具，否则规则本身没有测试。
const (
	RuleDocIndex   = "doc-index"
	RuleSpecStatus = "spec-status"
	RuleLink       = "link"
	RuleRef        = "ref"
	RuleCapSize    = "cap-size"
)

// 能力契约的体量阈值：超过软上限只提示，超过硬上限判失败。
// 依据见 specs/changes/capability-split/spec.md（102 行跨四域导致"改一处要读全文"）。
const (
	capSoftLines = 80
	capHardLines = 150
)

// statusValues 是 specs/changes 允许的状态值，定义见 docs/agents/issue-tracker.md。
var statusValues = map[string]bool{
	"draft":       true,
	"ready":       true,
	"in-progress": true,
	"done":        true,
	"cancelled":   true,
}

var (
	linkPattern   = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	refPattern    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])((?:docs|specs)/[A-Za-z0-9_./-]+\.md)`)
	skipPrefixes  = []string{"http://", "https://", "mailto:", "#", "/"}
	rootExtraDocs = []string{"CLAUDE.md", "CONTEXT.md", "CONTRIBUTING.md"}
)

// Issue 是一条违规。Path 为相对仓库根的斜杠路径。
type Issue struct {
	Rule    string
	Path    string
	Line    int
	Message string
}

func (i Issue) String() string {
	if i.Line > 0 {
		return fmt.Sprintf("%s:%d: [%s] %s", i.Path, i.Line, i.Rule, i.Message)
	}
	return fmt.Sprintf("%s: [%s] %s", i.Path, i.Rule, i.Message)
}

// Stats 是本次扫描的计数，用于 CLI 输出"检查了多少东西"，避免空跑也算通过。
type Stats struct {
	Docs   int
	Specs  int
	Links  int
	Refs   int
	Issues int
	Notes  int
}

// Report 是一次扫描的结果。Issues 让门禁失败，Notes 只是提示。
type Report struct {
	Issues []Issue
	Notes  []Issue
	Stats  Stats
}

// Run 扫描 root 下的文档契约并返回报告。
func Run(root string) (Report, error) {
	var report Report
	var st Stats
	files, err := collectFiles(root)
	if err != nil {
		return report, err
	}

	var issues []Issue
	indexIssues, err := checkDocIndex(root)
	if err != nil {
		return report, err
	}
	issues = append(issues, indexIssues...)

	specIssues, specs, err := checkSpecStatus(root)
	if err != nil {
		return report, err
	}
	issues = append(issues, specIssues...)
	st.Specs = specs

	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			return report, fmt.Errorf("读取 %s: %w", rel, err)
		}
		if strings.HasPrefix(rel, "docs/") {
			st.Docs++
		}
		linkIssues, links := checkLinks(root, rel, string(data))
		refIssues, refs := checkRefs(root, rel, string(data))
		issues = append(issues, linkIssues...)
		issues = append(issues, refIssues...)
		st.Links += links
		st.Refs += refs
	}

	sizeIssues, sizeNotes, err := checkCapSize(root)
	if err != nil {
		return report, err
	}
	issues = append(issues, sizeIssues...)

	sortIssues(issues)
	sortIssues(sizeNotes)
	st.Issues = len(issues)
	st.Notes = len(sizeNotes)

	report.Issues = issues
	report.Notes = sizeNotes
	report.Stats = st
	return report, nil
}

func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		return issues[i].Line < issues[j].Line
	})
}

// collectFiles 返回参与校验的 Markdown 相对路径（斜杠分隔）。
func collectFiles(root string) ([]string, error) {
	var out []string
	for _, dir := range []string{"docs", "specs"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".md") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, name := range rootExtraDocs {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// checkDocIndex 要求 docs/ 根目录下每份文档都在 docs/README.md 里登记文件名。
func checkDocIndex(root string) ([]Issue, error) {
	readmePath := filepath.Join(root, "docs", "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Issue{{Rule: RuleDocIndex, Path: "docs/README.md", Message: "文档索引缺失"}}, nil
		}
		return nil, err
	}
	text := string(readme)

	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		return nil, err
	}
	var issues []Issue
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		name := e.Name()
		if name == "README.md" {
			continue
		}
		if !strings.Contains(text, name) {
			issues = append(issues, Issue{
				Rule:    RuleDocIndex,
				Path:    "docs/" + name,
				Message: "未在 docs/README.md 索引中登记",
			})
		}
	}
	return issues, nil
}

// checkSpecStatus 要求 specs/changes/*/spec.md 首屏 Status 取值属于五态枚举。
func checkSpecStatus(root string) ([]Issue, int, error) {
	pattern := filepath.Join(root, "specs", "changes", "*", "spec.md")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, 0, err
	}
	var issues []Issue
	count := 0
	for _, path := range paths {
		count++
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, 0, err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, fmt.Errorf("读取 %s: %w", rel, err)
		}
		line, value, found := firstStatusLine(string(data))
		switch {
		case !found:
			issues = append(issues, Issue{Rule: RuleSpecStatus, Path: rel, Message: "首屏缺少 Status: 行"})
		case !statusValues[value]:
			issues = append(issues, Issue{
				Rule:    RuleSpecStatus,
				Path:    rel,
				Line:    line,
				Message: fmt.Sprintf("Status 取值 %q 不在枚举 draft|ready|in-progress|done|cancelled 内", value),
			})
		}
	}
	return issues, count, nil
}

// firstStatusLine 在前 40 行内找第一条 Status: 行，返回行号与去空白后的取值。
func firstStatusLine(content string) (int, string, bool) {
	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		if i >= 40 {
			break
		}
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "Status:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
		return i + 1, value, true
	}
	return 0, "", false
}

// checkCapSize 限制能力契约体量：超过软上限给提示，超过硬上限判失败。
func checkCapSize(root string) ([]Issue, []Issue, error) {
	paths, err := filepath.Glob(filepath.Join(root, "specs", "capabilities", "*.md"))
	if err != nil {
		return nil, nil, err
	}
	var issues, notes []Issue
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, nil, err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("读取 %s: %w", rel, err)
		}
		lines := countLines(string(data))
		switch {
		case lines > capHardLines:
			issues = append(issues, Issue{
				Rule:    RuleCapSize,
				Path:    rel,
				Message: fmt.Sprintf("%d 行，超过 %d 行硬上限，必须拆分", lines, capHardLines),
			})
		case lines > capSoftLines:
			notes = append(notes, Issue{
				Rule:    RuleCapSize,
				Path:    rel,
				Message: fmt.Sprintf("%d 行，超过 %d 行软上限，考虑拆分", lines, capSoftLines),
			})
		}
	}
	return issues, notes, nil
}

// countLines 按行尾换行统计行数，末尾无换行也计一行。
func countLines(content string) int {
	if content == "" {
		return 0
	}
	n := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		n++
	}
	return n
}

// checkLinks 校验 Markdown 相对链接目标是否存在；返回违规数与链接总数。
func checkLinks(root, rel, content string) ([]Issue, int) {
	var issues []Issue
	count := 0
	for i, line := range scanProseLines(content) {
		if line.inFence || line.text == "" {
			continue
		}
		for _, m := range linkPattern.FindAllStringSubmatch(line.text, -1) {
			target := normalizeLinkTarget(m[1])
			if target == "" {
				continue
			}
			count++
			if fileExists(root, rel, target) {
				continue
			}
			issues = append(issues, Issue{
				Rule:    RuleLink,
				Path:    rel,
				Line:    i + 1,
				Message: fmt.Sprintf("链接目标不存在：%s", target),
			})
		}
	}
	return issues, count
}

// checkRefs 校验正文里的 docs/... 与 specs/... 路径引用是否存在；返回违规数与引用总数。
func checkRefs(root, rel, content string) ([]Issue, int) {
	var issues []Issue
	count := 0
	seen := map[string]bool{}
	for i, line := range scanProseLines(content) {
		if line.inFence || line.text == "" {
			continue
		}
		for _, m := range refPattern.FindAllStringSubmatch(line.text, -1) {
			token := strings.TrimSpace(m[1])
			if !plausibleRef(token) {
				continue
			}
			key := fmt.Sprintf("%d|%s", i, token)
			if seen[key] {
				continue
			}
			seen[key] = true
			count++
			if pathExists(root, token) {
				continue
			}
			issues = append(issues, Issue{
				Rule:    RuleRef,
				Path:    rel,
				Line:    i + 1,
				Message: fmt.Sprintf("引用的文档不存在：%s", token),
			})
		}
	}
	return issues, count
}

type proseLine struct {
	text    string
	inFence bool
}

// scanProseLines 按行返回文本，并标记该行是否落在 ``` 围栏内。
func scanProseLines(content string) []proseLine {
	lines := strings.Split(content, "\n")
	out := make([]proseLine, 0, len(lines))
	inFence := false
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			out = append(out, proseLine{text: trimmed, inFence: true})
			inFence = !inFence
			continue
		}
		out = append(out, proseLine{text: raw, inFence: inFence})
	}
	return out
}

// normalizeLinkTarget 去掉标题、锚点与包裹的尖括号，返回需要校验的相对路径；空串表示跳过。
func normalizeLinkTarget(raw string) string {
	target := strings.TrimSpace(raw)
	if target == "" {
		return ""
	}
	if idx := strings.IndexAny(target, " \t"); idx >= 0 {
		// `path "title"` 形式，取第一段
		target = target[:idx]
	}
	target = strings.Trim(target, "<>")
	if target == "" || strings.Contains(target, "<") || strings.Contains(target, "*") {
		return ""
	}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(target, prefix) {
			return ""
		}
	}
	if idx := strings.Index(target, "#"); idx >= 0 {
		target = target[:idx]
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	return target
}

// plausibleRef 排除占位写法（`docs/<mechanism>.md`、`docs/*.md`、`docs/...`）。
func plausibleRef(token string) bool {
	if token == "" {
		return false
	}
	if strings.ContainsAny(token, "<>*") || strings.Contains(token, "...") {
		return false
	}
	if strings.HasSuffix(token, "/") {
		return false
	}
	return true
}

func fileExists(root, rel, target string) bool {
	if strings.HasSuffix(target, "/") {
		return pathExists(root, strings.TrimSuffix(target, "/"))
	}
	joined := filepath.Join(root, filepath.FromSlash(filepath.Dir(rel)), filepath.FromSlash(target))
	if _, err := os.Stat(joined); err == nil {
		return true
	}
	// 少数文档用仓库根相对路径写法，同样接受。
	return pathExists(root, target)
}

func pathExists(root, target string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(target)))
	return err == nil
}

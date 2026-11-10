// Command doccheck 校验仓库文档契约，作为本地钩子与 CI 的文档门禁。
//
// 用法：
//
//	go run ./tools/doccheck           # 校验当前目录所在仓库
//	go run ./tools/doccheck -root .   # 显式指定仓库根
//
// 违规（issues）以非零码退出，并逐条打印 `路径:行号: [规则] 说明`；
// 提示（notes）只打印、不影响退出码。
package main

import (
	"flag"
	"fmt"
	"os"

	"rf4server/internal/doccheck"
)

func main() {
	root := flag.String("root", ".", "仓库根目录")
	flag.Parse()

	report, err := doccheck.Run(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doccheck: %v\n", err)
		os.Exit(2)
	}
	for _, note := range report.Notes {
		fmt.Printf("note: %s\n", note.String())
	}
	for _, issue := range report.Issues {
		fmt.Fprintln(os.Stderr, issue.String())
	}
	st := report.Stats
	fmt.Printf("doccheck: docs=%d specs=%d links=%d refs=%d issues=%d notes=%d\n",
		st.Docs, st.Specs, st.Links, st.Refs, st.Issues, st.Notes)
	if len(report.Issues) > 0 {
		os.Exit(1)
	}
}

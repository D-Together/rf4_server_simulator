// Command doccheck 校验仓库文档契约，作为本地钩子与 CI 的文档门禁。
//
// 用法：
//
//	go run ./tools/doccheck           # 校验当前目录所在仓库
//	go run ./tools/doccheck -root .   # 显式指定仓库根
//
// 任何一条违规都以非零码退出，并逐条打印 `路径:行号: [规则] 说明`。
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

	issues, stats, err := doccheck.Run(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doccheck: %v\n", err)
		os.Exit(2)
	}
	for _, issue := range issues {
		fmt.Fprintln(os.Stderr, issue.String())
	}
	fmt.Printf("doccheck: docs=%d specs=%d links=%d refs=%d issues=%d\n",
		stats.Docs, stats.Specs, stats.Links, stats.Refs, stats.Issues)
	if len(issues) > 0 {
		os.Exit(1)
	}
}

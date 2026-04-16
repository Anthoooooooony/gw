package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/gw-cli/gw/filter"
	"github.com/gw-cli/gw/shell"
	"github.com/spf13/cobra"
)

var rewriteCmd = &cobra.Command{
	Use:   `rewrite "<command>"`,
	Short: "改写命令，将匹配的部分替换为 gw exec 调用",
	Long:  "由 Claude Code hook 调用，判断命令是否需要通过 gw 代理执行。",
	Args:  cobra.ExactArgs(1),
	Run:   runRewrite,
}

func init() {
	rootCmd.AddCommand(rewriteCmd)
}

func runRewrite(cmd *cobra.Command, args []string) {
	command := args[0]

	// 1. 分析命令：检查是否可以改写 + 拆分链式命令（单次扫描）
	canRewrite, segments := shell.AnalyzeCommand(command)
	if !canRewrite {
		os.Exit(1)
	}

	registry := filter.GlobalRegistry()

	// 3. 逐段检查并改写
	anyRewritten := false
	for i, seg := range segments {
		parts := strings.Fields(seg.Cmd)
		if len(parts) == 0 {
			continue
		}
		cmdName := parts[0]
		cmdArgs := parts[1:]

		if registry.Find(cmdName, cmdArgs) != nil {
			segments[i].Cmd = "gw exec " + seg.Cmd
			anyRewritten = true
		}
	}

	// 4. 如果没有任何改写，退出 1
	if !anyRewritten {
		os.Exit(1)
	}

	// 5. 重新组装命令并输出
	var sb strings.Builder
	for i, seg := range segments {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(seg.Cmd)
		if seg.Sep != "" {
			sb.WriteString(" ")
			sb.WriteString(seg.Sep)
		}
	}

	fmt.Println(sb.String())
	os.Exit(0)
}

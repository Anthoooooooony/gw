package cmd

import (
	"fmt"
	"os"

	"github.com/gw-cli/gw/filter"
	"github.com/gw-cli/gw/internal"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:                "exec [command] [args...]",
	Short:              "执行命令并过滤输出",
	Long:               "执行指定命令，通过匹配的过滤器压缩输出以减少 token 消耗。",
	DisableFlagParsing: true,
	Run:                runExec,
}

func init() {
	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) {
	// 1. PARSE: 提取命令名和参数
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "gw exec: 缺少命令参数")
		os.Exit(1)
	}
	cmdName := args[0]
	cmdArgs := args[1:]

	// 2. ROUTE: 从注册表查找匹配的过滤器
	registry := filter.DefaultRegistry()
	matched := registry.Find(cmdName, cmdArgs)

	// 3. EXECUTE: 本地执行命令
	result, err := internal.RunCommand(cmdName, cmdArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw exec: 无法执行命令: %v\n", err)
		os.Exit(127)
	}

	// 4. FILTER: 应用过滤器
	var output string
	originalOutput := result.Stdout + result.Stderr

	if matched != nil {
		input := filter.FilterInput{
			Cmd:      cmdName,
			Args:     cmdArgs,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			ExitCode: result.ExitCode,
		}

		if result.ExitCode == 0 {
			fo := matched.Apply(input)
			output = fo.Content
		} else {
			fo := matched.ApplyOnError(input)
			if fo != nil {
				output = fo.Content
			} else {
				output = originalOutput
			}
		}
	} else {
		// 无匹配过滤器，透传原始输出
		output = originalOutput
	}

	// 5. PRINT: 输出结果
	fmt.Print(output)

	// 6. TRACK: verbose 模式下输出统计信息
	if Verbose {
		originalLen := len(originalOutput)
		filteredLen := len(output)
		saved := originalLen - filteredLen
		fmt.Fprintf(os.Stderr, "[gw] original=%d filtered=%d saved=%d\n", originalLen, filteredLen, saved)
	}

	// 7. 使用原始命令的退出码退出
	os.Exit(result.ExitCode)
}

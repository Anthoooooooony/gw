package cmd

import (
	"github.com/spf13/cobra"
)

// Verbose 控制是否输出详细信息
var Verbose bool

var rootCmd = &cobra.Command{
	Use:   "gw",
	Short: "gw - 命令输出过滤代理",
	Long:  "gw 拦截 shell 命令，本地执行后过滤输出，减少 LLM token 消耗。",
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "输出详细信息")
}

// Execute 运行根命令
func Execute() error {
	return rootCmd.Execute()
}

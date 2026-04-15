package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "卸载 Claude Code hook",
	Long:  "从 ~/.claude/settings.json 中移除 gw rewrite hook。",
	Run:   runUninstall,
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) {
	path := getSettingsPath()
	settings := readSettings(path)

	// 读取现有 hooks
	existing, ok := settings["hooks"]
	if !ok {
		fmt.Println("未找到 hooks 配置，无需卸载。")
		return
	}

	arr, ok := existing.([]interface{})
	if !ok {
		fmt.Println("hooks 格式异常，无需卸载。")
		return
	}

	// 过滤掉 gw hook
	var filtered []interface{}
	found := false
	for _, h := range arr {
		if m, ok := h.(map[string]interface{}); ok {
			if m["hook"] == gwHookCommand {
				found = true
				continue
			}
		}
		filtered = append(filtered, h)
	}

	if !found {
		fmt.Println("gw hook 未安装，无需卸载。")
		return
	}

	if len(filtered) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = filtered
	}

	writeSettings(path, settings)
	fmt.Println("gw hook 已从 " + path + " 中移除。")
}

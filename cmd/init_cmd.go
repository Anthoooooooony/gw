package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "安装 Claude Code hook",
	Long:  "在 ~/.claude/settings.json 中添加 gw rewrite hook，让 Claude Code 的 Bash 命令自动经过 gw 代理。",
	Run:   runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

const gwHookCommand = `gw rewrite "$command"`

func getSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw init: 无法获取用户目录: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func readSettings(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{})
		}
		fmt.Fprintf(os.Stderr, "gw init: 无法读取 %s: %v\n", path, err)
		os.Exit(1)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		fmt.Fprintf(os.Stderr, "gw init: JSON 解析失败: %v\n", err)
		os.Exit(1)
	}
	return settings
}

func writeSettings(path string, settings map[string]interface{}) {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw init: JSON 序列化失败: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "gw init: 无法创建目录: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "gw init: 无法写入 %s: %v\n", path, err)
		os.Exit(1)
	}
}

func runInit(cmd *cobra.Command, args []string) {
	path := getSettingsPath()
	settings := readSettings(path)

	gwHook := map[string]interface{}{
		"matcher": "Bash",
		"hook":    gwHookCommand,
	}

	// 读取现有 hooks
	var hooks []interface{}
	if existing, ok := settings["hooks"]; ok {
		if arr, ok := existing.([]interface{}); ok {
			hooks = arr
		}
	}

	// 检查是否已安装
	for _, h := range hooks {
		if m, ok := h.(map[string]interface{}); ok {
			if m["hook"] == gwHookCommand {
				fmt.Println("gw hook 已安装，无需重复操作。")
				return
			}
		}
	}

	// 添加 hook
	hooks = append(hooks, gwHook)
	settings["hooks"] = hooks

	writeSettings(path, settings)
	fmt.Println("gw hook 已安装到 " + path)
}

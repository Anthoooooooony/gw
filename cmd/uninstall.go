package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "卸载 Claude Code hook",
	Long:  "从 ~/.claude/settings.json 中移除所有带 _gw_managed 标记的 hook 条目。",
	RunE:  runUninstallCmd,
}

// uninstallDryRun 控制 `gw uninstall --dry-run` 行为：仅打印将要写入的结果，不落盘。
var uninstallDryRun bool

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "只打印将要写入的 settings.json，不修改文件")
	rootCmd.AddCommand(uninstallCmd)
}

// runUninstallWith 是 uninstall 的核心实现，接受注入的路径/输出，方便测试。
func runUninstallWith(path string, dryRun bool, stdout io.Writer) error {
	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	// settings.json 不存在时直接提示（readSettings 已把不存在当作空 map 处理，
	// 但 uninstall 场景下无需创建文件；这里通过 stat 二次确认以给出更清晰的信息）。
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		fmt.Fprintln(stdout, "settings.json 不存在，无需卸载。")
		return nil
	}

	updated, status := applyUninstallToSettings(settings)

	if dryRun {
		data, err := marshalSettings(updated)
		if err != nil {
			return err
		}
		if _, err := stdout.Write(data); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "(dry-run) status=%s path=%s\n", status, path)
		return nil
	}

	switch status {
	case uninstallStatusNotFound:
		fmt.Fprintln(stdout, "未找到带 _gw_managed 标记的 hook，无需卸载。")
		return nil
	case uninstallStatusRemoved:
		if err := writeSettingsAtomic(path, updated); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "gw hook 已从 %s 中移除。\n", path)
		return nil
	default:
		return fmt.Errorf("未知状态: %s", status)
	}
}

// runUninstallCmd 是 cobra 绑定的入口。
func runUninstallCmd(cmd *cobra.Command, args []string) error {
	return runUninstallWith(getSettingsPath(), uninstallDryRun, os.Stdout)
}

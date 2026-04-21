package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "安装 Claude Code hook",
	Long:  "在 ~/.claude/settings.json 中添加 gw rewrite hook，让 Claude Code 的 Bash 命令自动经过 gw 代理。",
	RunE:  runInitCmd,
}

// initDryRun 控制 `gw init --dry-run` 行为：仅打印将要写入的 settings，不落盘。
var initDryRun bool

func init() {
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false, "只打印将要写入的 settings.json，不修改文件")
	rootCmd.AddCommand(initCmd)
}

const (
	// gwHookCommand 是 Claude Code PreToolUse hook 调用 gw 的命令模板。
	gwHookCommand = `gw rewrite "$command"`
	// gwManagedKey 用来标识某条 hook 条目由 gw 创建、受 gw 管理。
	// uninstall 时按此字段匹配，避免误删用户手动添加的其他 hook。
	gwManagedKey = "_gw_managed"
)

// init 命令的三态结果。
const (
	initStatusInstalled = "installed" // 成功新增 gw hook
	initStatusAlready   = "already"   // 已存在 gw hook，幂等
)

// uninstall 命令的三态结果。
const (
	uninstallStatusRemoved  = "removed"   // 移除了至少一个 gw hook
	uninstallStatusNotFound = "not-found" // 没有 gw hook 可移除
)

// getSettingsPath 返回 Claude Code settings.json 的默认路径。
// 仅用于命令入口；测试通过 runInitWith/runUninstallWith 注入 path，不经此函数。
func getSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw init: 无法获取用户目录: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// readSettings 读取 settings.json 为 map。文件不存在时返回空 map。
func readSettings(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}
	return settings, nil
}

// marshalSettings 以稳定格式序列化 settings，末尾补一个换行符。
func marshalSettings(settings map[string]interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return append(data, '\n'), nil
}

// writeSettingsAtomic 安全写入 settings.json：
//  1. 若目标文件已存在，先复制一份为 path+".bak" 作为回滚凭证；
//  2. 将新内容写入同目录的临时文件；
//  3. rename 临时文件到目标路径（同文件系统 rename 为原子操作）。
//
// 权限策略：
//   - 原文件已存在：backup 和 tmp 文件保留原文件的 mode（避免把 0600 降级到 0644）
//   - 首次写入（文件不存在）：新文件使用 0600（保守：hook 命令可能包含敏感内容）
//
// 任一步骤失败都不会留下半截的目标文件。
func writeSettingsAtomic(path string, settings map[string]interface{}) error {
	data, err := marshalSettings(settings)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}

	// 探测原文件 mode；不存在则使用保守默认 0600
	var targetMode os.FileMode = 0o600
	if info, err := os.Stat(path); err == nil {
		targetMode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat 原文件 %s 失败: %w", path, err)
	}

	// 1. 若目标已存在，生成备份（权限沿用原文件）
	if existing, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", existing, targetMode); err != nil {
			return fmt.Errorf("写入备份 %s.bak 失败: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取原文件 %s 失败: %w", path, err)
	}

	// 2. 写入临时文件
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	// 失败时清理临时文件
	defer func() {
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // 写入失败时关闭临时文件，错误已不重要
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	// 权限与原文件一致（首次写入时为 0600）
	if err := os.Chmod(tmpPath, targetMode); err != nil {
		return fmt.Errorf("调整临时文件权限失败: %w", err)
	}

	// 3. rename
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("重命名到 %s 失败: %w", path, err)
	}
	return nil
}

// applyInitToSettings 返回插入（或保持）gw hook 后的 settings 及状态。
//
// 判定：hooks[] 里任一条目带 _gw_managed=true → already；否则追加新 gw hook → installed。
func applyInitToSettings(settings map[string]interface{}) (map[string]interface{}, string) {
	out := make(map[string]interface{}, len(settings)+1)
	for k, v := range settings {
		out[k] = v
	}

	var hooks []interface{}
	if arr, ok := out["hooks"].([]interface{}); ok {
		hooks = arr
	}

	for _, h := range hooks {
		if m, ok := h.(map[string]interface{}); ok {
			if v, ok := m[gwManagedKey].(bool); ok && v {
				return out, initStatusAlready
			}
		}
	}

	gwHook := map[string]interface{}{
		"matcher":    "Bash",
		"hook":       gwHookCommand,
		gwManagedKey: true,
	}
	out["hooks"] = append(hooks, gwHook)
	return out, initStatusInstalled
}

// applyUninstallToSettings 返回移除所有带 _gw_managed 标记的条目后的 settings 及状态。
// 不存在任何带标记的条目时返回 uninstallStatusNotFound，且不修改原 settings 的 hooks 内容。
func applyUninstallToSettings(settings map[string]interface{}) (map[string]interface{}, string) {
	out := make(map[string]interface{}, len(settings))
	for k, v := range settings {
		out[k] = v
	}
	raw, ok := out["hooks"]
	if !ok {
		return out, uninstallStatusNotFound
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return out, uninstallStatusNotFound
	}

	filtered := make([]interface{}, 0, len(arr))
	removed := 0
	for _, h := range arr {
		if m, ok := h.(map[string]interface{}); ok {
			if v, ok := m[gwManagedKey].(bool); ok && v {
				removed++
				continue
			}
		}
		filtered = append(filtered, h)
	}
	if removed == 0 {
		return out, uninstallStatusNotFound
	}
	if len(filtered) == 0 {
		delete(out, "hooks")
	} else {
		out["hooks"] = filtered
	}
	return out, uninstallStatusRemoved
}

// runInitWith 是 init 的核心实现，接受注入的 settings 路径和输出目的地，方便测试。
// dryRun=true 时仅把结果 JSON 打印到 stdout，不落盘。
func runInitWith(path string, dryRun bool, stdout io.Writer) error {
	settings, err := readSettings(path)
	if err != nil {
		return err
	}
	updated, status := applyInitToSettings(settings)

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
	case initStatusAlready:
		fmt.Fprintln(stdout, "gw hook 已安装，无需重复。")
		return nil
	case initStatusInstalled:
		if err := writeSettingsAtomic(path, updated); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "gw hook 已安装到 %s\n", path)
		return nil
	default:
		return fmt.Errorf("未知状态: %s", status)
	}
}

// runInitCmd 是 cobra 绑定的入口。
func runInitCmd(cmd *cobra.Command, args []string) error {
	return runInitWith(getSettingsPath(), initDryRun, os.Stdout)
}

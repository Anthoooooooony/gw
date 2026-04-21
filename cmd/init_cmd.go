package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "安装 Claude Code hook",
	Long:  "在 ~/.claude/settings.json 的 hooks.PreToolUse 中安装 gw rewrite hook，让 Claude Code 的 Bash 工具调用自动经过 gw 代理。",
	RunE:  runInitCmd,
}

var initDryRun bool

func init() {
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false, "只打印将要写入的 settings.json，不修改文件")
	rootCmd.AddCommand(initCmd)
}

const (
	// gwManagedKey 标识某条 matcher 由 gw 创建、受 gw 管理。
	// uninstall 时按此字段匹配，避免误删用户的其他 hook。
	gwManagedKey = "_gw_managed"
	// bashMatcher 匹配 Claude Code 的 Bash 工具（精确匹配、大小写敏感）。
	bashMatcher = "Bash"
	// preToolUseEvent 是 Claude Code hook 事件名，对 Bash 工具调用前触发。
	preToolUseEvent = "PreToolUse"
)

const (
	initStatusInstalled = "installed"
	initStatusAlready   = "already"
)

const (
	uninstallStatusRemoved  = "removed"
	uninstallStatusNotFound = "not-found"
)

func getSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw init: 无法获取用户目录: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// shellQuote 把字符串包成单引号，并转义内部单引号。
// Claude Code hook 的 command 字段被 bash -c 执行，路径必须做 shell 安全包装。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// gwHookCommand 根据 gw 可执行文件的绝对路径，构造 hooks.PreToolUse 里写的 command 字段。
// Claude Code hook 执行环境 PATH 受限（macOS Launch Agent 默认只有 /usr/bin:/bin），
// 必须写绝对路径；路径中特殊字符通过 shellQuote 保护。
func gwHookCommand(gwPath string) string {
	return shellQuote(gwPath) + " rewrite"
}

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

func marshalSettings(settings map[string]interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return append(data, '\n'), nil
}

// writeSettingsAtomic 原子写入 settings.json：同目录临时文件 + rename。
// 权限策略：目标已存在则沿用其 mode；首次写入用 0600（hook 命令可能含敏感内容）。
func writeSettingsAtomic(path string, settings map[string]interface{}) error {
	data, err := marshalSettings(settings)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}

	var targetMode os.FileMode = 0o600
	if info, err := os.Stat(path); err == nil {
		targetMode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat 原文件 %s 失败: %w", path, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpPath, targetMode); err != nil {
		return fmt.Errorf("调整临时文件权限失败: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("重命名到 %s 失败: %w", path, err)
	}
	return nil
}

// applyInitToSettings 在 settings.hooks.PreToolUse 中插入或保持 gw matcher。
//
// Claude Code 的 hooks 结构是按事件名分组的 map：
//
//	"hooks": {
//	  "PreToolUse": [
//	    {"matcher": "Bash", "_gw_managed": true, "hooks": [{"type": "command", "command": "..."}]}
//	  ]
//	}
//
// 幂等：PreToolUse 数组内任一 matcher 带 _gw_managed=true → already，不动原 settings。
// 用户自定义的其他事件（PostToolUse 等）与 PreToolUse 内他人 matcher 原样保留。
func applyInitToSettings(settings map[string]interface{}, gwPath string) (map[string]interface{}, string) {
	out := make(map[string]interface{}, len(settings)+1)
	for k, v := range settings {
		out[k] = v
	}

	hooksObj, _ := out["hooks"].(map[string]interface{})
	preArr, _ := hooksObj["PreToolUse"].([]interface{})

	for _, m := range preArr {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if v, ok := mm[gwManagedKey].(bool); ok && v {
			return out, initStatusAlready
		}
	}

	gwMatcher := map[string]interface{}{
		"matcher":    bashMatcher,
		gwManagedKey: true,
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": gwHookCommand(gwPath),
			},
		},
	}

	newHooks := make(map[string]interface{}, len(hooksObj)+1)
	for k, v := range hooksObj {
		newHooks[k] = v
	}
	newHooks["PreToolUse"] = append(preArr, gwMatcher)
	out["hooks"] = newHooks
	return out, initStatusInstalled
}

// applyUninstallToSettings 从 hooks.PreToolUse 移除带 _gw_managed=true 的 matcher。
// 空后清理：PreToolUse 空 → 删该 key；hooks 空 → 删 hooks key。
func applyUninstallToSettings(settings map[string]interface{}) (map[string]interface{}, string) {
	out := make(map[string]interface{}, len(settings))
	for k, v := range settings {
		out[k] = v
	}

	hooksObj, ok := out["hooks"].(map[string]interface{})
	if !ok {
		return out, uninstallStatusNotFound
	}
	preArr, ok := hooksObj["PreToolUse"].([]interface{})
	if !ok {
		return out, uninstallStatusNotFound
	}

	filtered := make([]interface{}, 0, len(preArr))
	removed := 0
	for _, m := range preArr {
		mm, ok := m.(map[string]interface{})
		if ok {
			if v, ok := mm[gwManagedKey].(bool); ok && v {
				removed++
				continue
			}
		}
		filtered = append(filtered, m)
	}
	if removed == 0 {
		return out, uninstallStatusNotFound
	}

	newHooks := make(map[string]interface{}, len(hooksObj))
	for k, v := range hooksObj {
		if k != "PreToolUse" {
			newHooks[k] = v
		}
	}
	if len(filtered) > 0 {
		newHooks["PreToolUse"] = filtered
	}
	if len(newHooks) == 0 {
		delete(out, "hooks")
	} else {
		out["hooks"] = newHooks
	}
	return out, uninstallStatusRemoved
}

// resolveGwPath 返回当前 gw 可执行文件的绝对路径，供 init 写入 hook 使用。
func resolveGwPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法获取 gw 可执行文件路径: %w", err)
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", fmt.Errorf("无法解析 gw 绝对路径: %w", err)
	}
	return abs, nil
}

// runInitWith 是 init 的核心实现，接受注入的 settings 路径、gw 绝对路径和输出目的地。
func runInitWith(path string, gwPath string, dryRun bool, stdout io.Writer) error {
	settings, err := readSettings(path)
	if err != nil {
		return err
	}
	updated, status := applyInitToSettings(settings, gwPath)

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

func runInitCmd(cmd *cobra.Command, args []string) error {
	gwPath, err := resolveGwPath()
	if err != nil {
		return err
	}
	return runInitWith(getSettingsPath(), gwPath, initDryRun, os.Stdout)
}

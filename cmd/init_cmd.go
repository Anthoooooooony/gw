package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/iancoleman/orderedmap"
	"github.com/spf13/cobra"
)

// claudeLookPath 探测 claude CLI 在 PATH 中的可见性，测试里替换成 stub 避免依赖真实环境。
var claudeLookPath = exec.LookPath

// settingsDoc 是 settings.json 的保序内存表示。
// 以 *orderedmap.OrderedMap 为载体保留 top-level / nested object 的 key 顺序，
// 避免 gw init / uninstall 后整文件被字典序重排，污染用户 git diff。
type settingsDoc = *orderedmap.OrderedMap

// defaultSettingsIndent 是没法从原文件检测到缩进时使用的回退值（Claude Code 自身用 2 空格）。
const defaultSettingsIndent = "  "

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
	// gwHookTimeoutSec 写入 hook 对象的 timeout 字段（秒）。Claude Code 超时后放弃等 hook 输出
	// 并走默认行为，防止 gw rewrite 异常挂住整条 Claude 会话。
	gwHookTimeoutSec = 10
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

// detectSettingsIndent 从原始 JSON 字节中侦测首个缩进单元（常见为 2 / 4 空格或 tab）。
// 无法识别时返回 defaultSettingsIndent，避免对空文件 / 单行 JSON 报错。
func detectSettingsIndent(data []byte) string {
	for i, b := range data {
		if b != '\n' {
			continue
		}
		j := i + 1
		for j < len(data) && (data[j] == ' ' || data[j] == '\t') {
			j++
		}
		if j > i+1 {
			return string(data[i+1 : j])
		}
		break
	}
	return defaultSettingsIndent
}

// readSettings 解析 settings.json 为保序文档。缺文件返回空文档 + 默认缩进。
// 第二返回值是检测到的缩进，供后续写回时复用，保证 init/uninstall 不改动用户的缩进风格。
func readSettings(path string) (settingsDoc, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return orderedmap.New(), defaultSettingsIndent, nil
		}
		return nil, "", fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	doc := orderedmap.New()
	if len(data) == 0 {
		return doc, defaultSettingsIndent, nil
	}
	if err := json.Unmarshal(data, doc); err != nil {
		return nil, "", fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return doc, detectSettingsIndent(data), nil
}

func marshalSettings(doc settingsDoc, indent string) ([]byte, error) {
	if indent == "" {
		indent = defaultSettingsIndent
	}
	data, err := json.MarshalIndent(doc, "", indent)
	if err != nil {
		return nil, fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return append(data, '\n'), nil
}

// writeSettingsAtomic 原子写入 settings.json：同目录临时文件 + rename。
// 权限策略：目标已存在则沿用其 mode；首次写入用 0600（hook 命令可能含敏感内容）。
// indent 由 readSettings 检测得到，原样写回避免改动用户缩进风格。
func writeSettingsAtomic(path string, doc settingsDoc, indent string) error {
	data, err := marshalSettings(doc, indent)
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

// asOrderedMap 把 OrderedMap 在 interface{} 里的两种存在形式（值类型 / 指针类型）统一成指针，
// 方便 Set/Delete 直接生效。json.Unmarshal 经 orderedmap 路径会把嵌套对象存为值，
// 而 gw 自己构造的对象习惯传指针。
func asOrderedMap(v interface{}) (*orderedmap.OrderedMap, bool) {
	switch x := v.(type) {
	case *orderedmap.OrderedMap:
		return x, true
	case orderedmap.OrderedMap:
		return &x, true
	default:
		return nil, false
	}
}

// matcherIsGwManaged 在 PreToolUse 数组的某个 matcher 上判定 _gw_managed=true。
func matcherIsGwManaged(m interface{}) bool {
	om, ok := asOrderedMap(m)
	if !ok {
		return false
	}
	v, ok := om.Get(gwManagedKey)
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// newGwMatcher 构造单条 gw 的 PreToolUse matcher，键顺序固定为 matcher / _gw_managed / hooks。
func newGwMatcher(gwPath string) *orderedmap.OrderedMap {
	hookEntry := orderedmap.New()
	hookEntry.Set("type", "command")
	// timeout 以秒为单位，超时后 Claude Code 放弃等待并走默认行为。
	// gw rewrite 典型耗时 <200ms；设 10 秒是保守上限，防止 rewrite 异常
	// 挂死时整条 Claude 会话被卡住。
	hookEntry.Set("timeout", gwHookTimeoutSec)
	hookEntry.Set("command", gwHookCommand(gwPath))

	matcher := orderedmap.New()
	matcher.Set("matcher", bashMatcher)
	matcher.Set(gwManagedKey, true)
	matcher.Set("hooks", []interface{}{*hookEntry})
	return matcher
}

// applyInitToSettings 在 settings.hooks.PreToolUse 中插入或保持 gw matcher。
//
// Claude Code 的 hooks 结构是按事件名分组的 object：
//
//	"hooks": {
//	  "PreToolUse": [
//	    {"matcher": "Bash", "_gw_managed": true, "hooks": [{"type": "command", "command": "..."}]}
//	  ]
//	}
//
// 幂等：PreToolUse 数组内任一 matcher 带 _gw_managed=true → already，不动原 settings。
// 用户自定义的其他事件（PostToolUse 等）与 PreToolUse 内他人 matcher 原样保留，
// 且 top-level / hooks 的 key 顺序保持原样（PreToolUse 若新建追加在末尾）。
func applyInitToSettings(settings settingsDoc, gwPath string) (settingsDoc, string) {
	hooksOM, _ := asOrderedMap(getOr(settings, "hooks"))
	if hooksOM == nil {
		hooksOM = orderedmap.New()
	}

	var preArr []interface{}
	if v, ok := hooksOM.Get(preToolUseEvent); ok {
		if arr, ok := v.([]interface{}); ok {
			preArr = arr
		}
	}

	for _, m := range preArr {
		if matcherIsGwManaged(m) {
			return settings, initStatusAlready
		}
	}

	preArr = append(preArr, *newGwMatcher(gwPath))
	hooksOM.Set(preToolUseEvent, preArr)
	settings.Set("hooks", *hooksOM)
	return settings, initStatusInstalled
}

// getOr 按 key 取 OrderedMap 中的 raw 值，缺则返回 nil。
func getOr(om settingsDoc, key string) interface{} {
	v, _ := om.Get(key)
	return v
}

// applyUninstallToSettings 从 hooks.PreToolUse 移除带 _gw_managed=true 的 matcher。
// 空后清理：PreToolUse 空 → 删该 key；hooks 空 → 删 hooks key。
// 其他 top-level key、hooks 下其他事件、PreToolUse 内他人 matcher 的顺序原样保留。
func applyUninstallToSettings(settings settingsDoc) (settingsDoc, string) {
	hooksOM, _ := asOrderedMap(getOr(settings, "hooks"))
	if hooksOM == nil {
		return settings, uninstallStatusNotFound
	}
	raw, ok := hooksOM.Get(preToolUseEvent)
	if !ok {
		return settings, uninstallStatusNotFound
	}
	preArr, ok := raw.([]interface{})
	if !ok {
		return settings, uninstallStatusNotFound
	}

	filtered := make([]interface{}, 0, len(preArr))
	removed := 0
	for _, m := range preArr {
		if matcherIsGwManaged(m) {
			removed++
			continue
		}
		filtered = append(filtered, m)
	}
	if removed == 0 {
		return settings, uninstallStatusNotFound
	}

	if len(filtered) > 0 {
		hooksOM.Set(preToolUseEvent, filtered)
	} else {
		hooksOM.Delete(preToolUseEvent)
	}

	if len(hooksOM.Keys()) == 0 {
		settings.Delete("hooks")
	} else {
		settings.Set("hooks", *hooksOM)
	}
	return settings, uninstallStatusRemoved
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

// reportClaudeVisibility 在 init 成功后提示 claude CLI 是否可见。
// 非阻塞：缺失只 warn，已装但版本获取失败也只打印路径；任何错误都不影响 init 返回值。
func reportClaudeVisibility(stderr io.Writer) {
	claudePath, err := claudeLookPath("claude")
	if err != nil {
		fmt.Fprintln(stderr, "gw: warning: 未检测到 claude CLI，请先安装 Claude Code，hook 才会生效")
		return
	}
	fmt.Fprintf(stderr, "gw init: 检测到 claude CLI: %s\n", claudePath)
}

// runInitWith 是 init 的核心实现，接受注入的 settings 路径、gw 绝对路径和输出目的地。
// stderr 用于承载 `gw: warning:` / `gw init:` 这类带前缀的诊断信息。
func runInitWith(path string, gwPath string, dryRun bool, stdout, stderr io.Writer) error {
	settings, indent, err := readSettings(path)
	if err != nil {
		return err
	}
	updated, status := applyInitToSettings(settings, gwPath)

	if dryRun {
		data, err := marshalSettings(updated, indent)
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
		reportClaudeVisibility(stderr)
		return nil
	case initStatusInstalled:
		if err := writeSettingsAtomic(path, updated, indent); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "gw hook 已安装到 %s\n", path)
		reportClaudeVisibility(stderr)
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
	return runInitWith(getSettingsPath(), gwPath, initDryRun, os.Stdout, os.Stderr)
}

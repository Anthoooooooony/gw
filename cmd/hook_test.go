package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testGwPath 是测试里虚构的 gw 可执行文件绝对路径，写入 hook command 供断言。
const testGwPath = "/opt/test/gw"

// readJSON 读取 JSON 文件为 map，方便测试断言。
func readJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	return m
}

// preToolUseMatchers 从 settings 中取出 hooks.PreToolUse 数组，找不到返回 nil。
func preToolUseMatchers(t *testing.T, settings map[string]interface{}) []interface{} {
	t.Helper()
	hooksObj, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := hooksObj["PreToolUse"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("hooks.PreToolUse 不是数组: %T", raw)
	}
	return arr
}

// countGwMatchers 统计带 _gw_managed=true 标记的 matcher 数。
func countGwMatchers(matchers []interface{}) int {
	n := 0
	for _, m := range matchers {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if v, ok := mm[gwManagedKey].(bool); ok && v {
			n++
		}
	}
	return n
}

// --- applyInitToSettings ---

// 空 settings → 新建 hooks.PreToolUse 数组，matcher 携带正确字段
func TestApplyInit_EmptySettings(t *testing.T) {
	updated, status := applyInitToSettings(map[string]interface{}{}, testGwPath)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	matchers := preToolUseMatchers(t, updated)
	if len(matchers) != 1 {
		t.Fatalf("期望 1 个 matcher, 得到 %d", len(matchers))
	}
	m := matchers[0].(map[string]interface{})
	if m["matcher"] != bashMatcher {
		t.Errorf("matcher 字段应为 Bash, 得到 %v", m["matcher"])
	}
	if v, _ := m[gwManagedKey].(bool); !v {
		t.Error("缺 _gw_managed: true 标记")
	}
	nested, ok := m["hooks"].([]interface{})
	if !ok || len(nested) != 1 {
		t.Fatalf("matcher.hooks 结构不对: %v", m["hooks"])
	}
	h := nested[0].(map[string]interface{})
	if h["type"] != "command" {
		t.Errorf("嵌套 hook type 应为 command, 得到 %v", h["type"])
	}
	wantCmd := "'" + testGwPath + "' rewrite"
	if h["command"] != wantCmd {
		t.Errorf("嵌套 hook command 不对\n got: %q\nwant: %q", h["command"], wantCmd)
	}
	// timeout 字段防 gw rewrite 挂死（见 #64），整数秒
	if got, ok := h["timeout"].(int); !ok || got != gwHookTimeoutSec {
		t.Errorf("嵌套 hook timeout 应为 int %d, 得到 %T %v", gwHookTimeoutSec, h["timeout"], h["timeout"])
	}
}

// 已有其他事件（PostToolUse）→ 保留其他事件，仅在 PreToolUse 下加 matcher
func TestApplyInit_PreservesOtherEvents(t *testing.T) {
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{"matcher": "Read", "hooks": []interface{}{}},
			},
		},
	}
	updated, status := applyInitToSettings(settings, testGwPath)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	hooksObj := updated["hooks"].(map[string]interface{})
	if _, ok := hooksObj["PostToolUse"]; !ok {
		t.Error("PostToolUse 事件丢失")
	}
	if countGwMatchers(preToolUseMatchers(t, updated)) != 1 {
		t.Error("未在 PreToolUse 下插入 gw matcher")
	}
}

// 已有他人 PreToolUse matcher（无标记）→ 追加 gw matcher，不动原条目
func TestApplyInit_AppendAlongsideForeignMatcher(t *testing.T) {
	foreign := map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{"type": "command", "command": "echo foreign"},
		},
	}
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{foreign},
		},
	}
	updated, status := applyInitToSettings(settings, testGwPath)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	matchers := preToolUseMatchers(t, updated)
	if len(matchers) != 2 {
		t.Fatalf("期望 2 个 matcher, 得到 %d", len(matchers))
	}
	first := matchers[0].(map[string]interface{})
	if _, ok := first[gwManagedKey]; ok {
		t.Error("不应给他人 matcher 添加 _gw_managed")
	}
	if countGwMatchers(matchers) != 1 {
		t.Errorf("gw matcher 数量不对: %d", countGwMatchers(matchers))
	}
}

// 已有 gw matcher（带标记）→ 幂等 already
func TestApplyInit_AlreadyInstalled(t *testing.T) {
	existing := map[string]interface{}{
		"matcher":    bashMatcher,
		gwManagedKey: true,
		"hooks": []interface{}{
			map[string]interface{}{"type": "command", "command": "'/old/path/gw' rewrite"},
		},
	}
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{existing},
		},
	}
	updated, status := applyInitToSettings(settings, testGwPath)
	if status != initStatusAlready {
		t.Fatalf("期望 already, 得到 %q", status)
	}
	// 幂等不应追加，也不应把 command 改写成新的 gwPath
	if len(preToolUseMatchers(t, updated)) != 1 {
		t.Fatal("幂等时不应新增 matcher")
	}
}

// 路径含空格/单引号时 shellQuote 必须正确转义
func TestApplyInit_ShellQuotesGwPath(t *testing.T) {
	tricky := "/Users/foo bar/go bin/gw"
	updated, _ := applyInitToSettings(map[string]interface{}{}, tricky)
	matchers := preToolUseMatchers(t, updated)
	h := matchers[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	got := h["command"].(string)
	want := "'/Users/foo bar/go bin/gw' rewrite"
	if got != want {
		t.Errorf("shell quote 错误\n got: %q\nwant: %q", got, want)
	}
}

// --- applyUninstallToSettings ---

// uninstall：他人 matcher 与 gw matcher 共存 → 只移除 gw matcher
func TestApplyUninstall_KeepsForeignMatcher(t *testing.T) {
	foreign := map[string]interface{}{"matcher": "Bash", "hooks": []interface{}{}}
	gw := map[string]interface{}{
		"matcher":    bashMatcher,
		gwManagedKey: true,
		"hooks":      []interface{}{},
	}
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{foreign, gw},
		},
	}
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	matchers := preToolUseMatchers(t, updated)
	if len(matchers) != 1 {
		t.Fatalf("期望剩 1 个 matcher, 得到 %d", len(matchers))
	}
	if _, ok := matchers[0].(map[string]interface{})[gwManagedKey]; ok {
		t.Error("不应保留 gw matcher")
	}
}

// 只 gw matcher → 清 PreToolUse key；若 hooks 仅含 PreToolUse → 清 hooks key
func TestApplyUninstall_CleansEmptyHooks(t *testing.T) {
	gw := map[string]interface{}{
		"matcher":    bashMatcher,
		gwManagedKey: true,
		"hooks":      []interface{}{},
	}
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{gw},
		},
	}
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	if _, ok := updated["hooks"]; ok {
		t.Error("hooks key 应被清除")
	}
}

// 清 PreToolUse 后仍有 PostToolUse → 只删 PreToolUse key，hooks 保留
func TestApplyUninstall_PreservesOtherEvents(t *testing.T) {
	gw := map[string]interface{}{"matcher": bashMatcher, gwManagedKey: true, "hooks": []interface{}{}}
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse":  []interface{}{gw},
			"PostToolUse": []interface{}{map[string]interface{}{"matcher": "Read"}},
		},
	}
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	hooksObj, ok := updated["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks key 被意外删除")
	}
	if _, ok := hooksObj["PreToolUse"]; ok {
		t.Error("PreToolUse 应被清除")
	}
	if _, ok := hooksObj["PostToolUse"]; !ok {
		t.Error("PostToolUse 被误删")
	}
}

// 无 hooks 字段 → not-found
func TestApplyUninstall_NoHooksField(t *testing.T) {
	_, status := applyUninstallToSettings(map[string]interface{}{})
	if status != uninstallStatusNotFound {
		t.Fatalf("期望 not-found, 得到 %q", status)
	}
}

// hooks.PreToolUse 里全是他人 matcher → not-found，不动任何条目
func TestApplyUninstall_NoGwMatcher(t *testing.T) {
	foreign := map[string]interface{}{"matcher": "Bash", "hooks": []interface{}{}}
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{"PreToolUse": []interface{}{foreign}},
	}
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusNotFound {
		t.Fatalf("期望 not-found, 得到 %q", status)
	}
	if len(preToolUseMatchers(t, updated)) != 1 {
		t.Error("他人 matcher 不应被动")
	}
}

// --- writeSettingsAtomic ---

func TestWriteSettingsAtomic_NoBackupFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := writeSettingsAtomic(path, map[string]interface{}{"v": "old"}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := writeSettingsAtomic(path, map[string]interface{}{"v": "new"}); err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}
	if got := readJSON(t, path); got["v"] != "new" {
		t.Fatalf("新内容未生效: %v", got["v"])
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatal("不应生成 .bak 备份")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("写入 mode 期望 0600，得到 %o", perm)
	}
}

func TestWriteSettingsAtomic_PreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"v":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSettingsAtomic(path, map[string]interface{}{"v": "new"}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("新文件 mode 期望 0600，得到 %o", perm)
	}
}

// --- 端到端：runInitWith / runUninstallWith ---

func TestRunInit_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var buf strings.Builder
	if err := runInitWith(path, testGwPath, true, &buf, io.Discard); err != nil {
		t.Fatalf("dry-run 失败: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry-run 不应创建文件")
	}
	out := buf.String()
	if !strings.Contains(out, testGwPath) {
		t.Fatalf("dry-run 输出未包含 gw 绝对路径: %s", out)
	}
	if !strings.Contains(out, "_gw_managed") {
		t.Fatalf("dry-run 输出未包含 _gw_managed 标记: %s", out)
	}
	if !strings.Contains(out, "PreToolUse") {
		t.Fatalf("dry-run 输出未包含 PreToolUse 事件: %s", out)
	}
}

func TestRunInit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var buf strings.Builder
	if err := runInitWith(path, testGwPath, false, &buf, io.Discard); err != nil {
		t.Fatalf("第一次 init 失败: %v", err)
	}
	buf.Reset()
	if err := runInitWith(path, testGwPath, false, &buf, io.Discard); err != nil {
		t.Fatalf("第二次 init 失败: %v", err)
	}
	settings := readJSON(t, path)
	if got := countGwMatchers(preToolUseMatchers(t, settings)); got != 1 {
		t.Fatalf("幂等失败，gw matcher 数 = %d", got)
	}
}

func TestRunUninstall_KeepsForeignMatcher(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	initial := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{"matcher": "Bash", "hooks": []interface{}{}},
			},
		},
	}
	if err := writeSettingsAtomic(path, initial); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	var buf strings.Builder
	if err := runInitWith(path, testGwPath, false, &buf, io.Discard); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	buf.Reset()
	if err := runUninstallWith(path, false, &buf); err != nil {
		t.Fatalf("uninstall 失败: %v", err)
	}
	matchers := preToolUseMatchers(t, readJSON(t, path))
	if len(matchers) != 1 {
		t.Fatalf("期望剩 1 个 matcher, 得到 %d", len(matchers))
	}
	if _, ok := matchers[0].(map[string]interface{})[gwManagedKey]; ok {
		t.Fatal("gw matcher 未被移除")
	}
}

func TestRunUninstall_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var buf strings.Builder
	if err := runInitWith(path, testGwPath, false, &buf, io.Discard); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	stat1, _ := os.Stat(path)
	buf.Reset()
	if err := runUninstallWith(path, true, &buf); err != nil {
		t.Fatalf("dry-run uninstall 失败: %v", err)
	}
	stat2, _ := os.Stat(path)
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Fatal("dry-run uninstall 不应修改文件")
	}
	if countGwMatchers(preToolUseMatchers(t, readJSON(t, path))) != 1 {
		t.Fatal("dry-run 不应移除 gw matcher")
	}
	out := buf.String()
	if strings.Contains(out, "_gw_managed") {
		t.Fatalf("dry-run 输出应展示移除后的 settings, 不应再含 _gw_managed: %s", out)
	}
}

// withClaudeLookPath 临时替换包级 claudeLookPath 探针，并在清理时恢复。
func withClaudeLookPath(t *testing.T, stub func(string) (string, error)) {
	t.Helper()
	orig := claudeLookPath
	claudeLookPath = stub
	t.Cleanup(func() { claudeLookPath = orig })
}

func TestRunInit_WarnWhenClaudeMissing(t *testing.T) {
	withClaudeLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var stdout, stderr strings.Builder
	if err := runInitWith(path, testGwPath, false, &stdout, &stderr); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	if !strings.Contains(stderr.String(), "gw: warning: 未检测到 claude CLI") {
		t.Fatalf("缺 claude 时应 warn, stderr = %q", stderr.String())
	}
}

func TestRunInit_InfoWhenClaudePresent(t *testing.T) {
	withClaudeLookPath(t, func(string) (string, error) {
		return "/opt/test/claude", nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var stdout, stderr strings.Builder
	if err := runInitWith(path, testGwPath, false, &stdout, &stderr); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	s := stderr.String()
	if !strings.Contains(s, "gw init: 检测到 claude CLI") || !strings.Contains(s, "/opt/test/claude") {
		t.Fatalf("claude 可见时应打印路径, stderr = %q", s)
	}
}

func TestRunInit_DryRunSkipsVisibilityProbe(t *testing.T) {
	called := false
	withClaudeLookPath(t, func(string) (string, error) {
		called = true
		return "/opt/test/claude", nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var stdout, stderr strings.Builder
	if err := runInitWith(path, testGwPath, true, &stdout, &stderr); err != nil {
		t.Fatalf("dry-run 失败: %v", err)
	}
	if called {
		t.Fatal("dry-run 不应触发 claude 可见性探测")
	}
	if stderr.Len() != 0 {
		t.Fatalf("dry-run 不应写 stderr, 得到 %q", stderr.String())
	}
}

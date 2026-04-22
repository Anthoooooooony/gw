package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iancoleman/orderedmap"
)

// testGwPath 是测试里虚构的 gw 可执行文件绝对路径，写入 hook command 供断言。
const testGwPath = "/opt/test/gw"

// readJSON 读取 JSON 文件为 map，方便测试断言（丢弃 key 顺序信息）。
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

// mapToOrderedMap 递归把 map[string]interface{} 转成 *orderedmap.OrderedMap，
// 嵌套对象存为 orderedmap.OrderedMap 值（与 JSON 反序列化路径一致），
// 让既有测试用例的 map literal 输入可以继续用。
func mapToOrderedMap(m map[string]interface{}) *orderedmap.OrderedMap {
	om := orderedmap.New()
	for k, v := range m {
		om.Set(k, convertToOrderedValue(v))
	}
	return om
}

func convertToOrderedValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return *mapToOrderedMap(x)
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, el := range x {
			out[i] = convertToOrderedValue(el)
		}
		return out
	default:
		return v
	}
}

// omToMap 经 JSON round-trip 把 settingsDoc 退回普通 map，方便按值断言（不关心顺序）。
func omToMap(t *testing.T, doc settingsDoc) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化 doc 失败: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("反序列化 doc 失败: %v", err)
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
	updated, status := applyInitToSettings(orderedmap.New(), testGwPath)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	matchers := preToolUseMatchers(t, omToMap(t, updated))
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
	// timeout 字段防 gw rewrite 挂死（见 #64）。
	// omToMap 经 stdlib json round-trip，数字落成 float64；gw 写入的是 int，值一致即可。
	if got, ok := h["timeout"].(float64); !ok || int(got) != gwHookTimeoutSec {
		t.Errorf("嵌套 hook timeout 应为 %d, 得到 %T %v", gwHookTimeoutSec, h["timeout"], h["timeout"])
	}
}

// 已有其他事件（PostToolUse）→ 保留其他事件，仅在 PreToolUse 下加 matcher
func TestApplyInit_PreservesOtherEvents(t *testing.T) {
	settings := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{"matcher": "Read", "hooks": []interface{}{}},
			},
		},
	})
	updated, status := applyInitToSettings(settings, testGwPath)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	result := omToMap(t, updated)
	hooksObj := result["hooks"].(map[string]interface{})
	if _, ok := hooksObj["PostToolUse"]; !ok {
		t.Error("PostToolUse 事件丢失")
	}
	if countGwMatchers(preToolUseMatchers(t, result)) != 1 {
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
	settings := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{foreign},
		},
	})
	updated, status := applyInitToSettings(settings, testGwPath)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	matchers := preToolUseMatchers(t, omToMap(t, updated))
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
	settings := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{existing},
		},
	})
	updated, status := applyInitToSettings(settings, testGwPath)
	if status != initStatusAlready {
		t.Fatalf("期望 already, 得到 %q", status)
	}
	// 幂等不应追加，也不应把 command 改写成新的 gwPath
	if len(preToolUseMatchers(t, omToMap(t, updated))) != 1 {
		t.Fatal("幂等时不应新增 matcher")
	}
}

// 路径含空格/单引号时 shellQuote 必须正确转义
func TestApplyInit_ShellQuotesGwPath(t *testing.T) {
	tricky := "/Users/foo bar/go bin/gw"
	updated, _ := applyInitToSettings(orderedmap.New(), tricky)
	matchers := preToolUseMatchers(t, omToMap(t, updated))
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
	settings := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{foreign, gw},
		},
	})
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	matchers := preToolUseMatchers(t, omToMap(t, updated))
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
	settings := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{gw},
		},
	})
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	if _, ok := updated.Get("hooks"); ok {
		t.Error("hooks key 应被清除")
	}
}

// 清 PreToolUse 后仍有 PostToolUse → 只删 PreToolUse key，hooks 保留
func TestApplyUninstall_PreservesOtherEvents(t *testing.T) {
	gw := map[string]interface{}{"matcher": bashMatcher, gwManagedKey: true, "hooks": []interface{}{}}
	settings := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse":  []interface{}{gw},
			"PostToolUse": []interface{}{map[string]interface{}{"matcher": "Read"}},
		},
	})
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	result := omToMap(t, updated)
	hooksObj, ok := result["hooks"].(map[string]interface{})
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
	_, status := applyUninstallToSettings(orderedmap.New())
	if status != uninstallStatusNotFound {
		t.Fatalf("期望 not-found, 得到 %q", status)
	}
}

// hooks.PreToolUse 里全是他人 matcher → not-found，不动任何条目
func TestApplyUninstall_NoGwMatcher(t *testing.T) {
	foreign := map[string]interface{}{"matcher": "Bash", "hooks": []interface{}{}}
	settings := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{"PreToolUse": []interface{}{foreign}},
	})
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusNotFound {
		t.Fatalf("期望 not-found, 得到 %q", status)
	}
	if len(preToolUseMatchers(t, omToMap(t, updated))) != 1 {
		t.Error("他人 matcher 不应被动")
	}
}

// --- writeSettingsAtomic ---

func TestWriteSettingsAtomic_NoBackupFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := writeSettingsAtomic(path, mapToOrderedMap(map[string]interface{}{"v": "old"}), defaultSettingsIndent); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := writeSettingsAtomic(path, mapToOrderedMap(map[string]interface{}{"v": "new"}), defaultSettingsIndent); err != nil {
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
	if err := writeSettingsAtomic(path, mapToOrderedMap(map[string]interface{}{"v": "new"}), defaultSettingsIndent); err != nil {
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
	initial := mapToOrderedMap(map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{"matcher": "Bash", "hooks": []interface{}{}},
			},
		},
	})
	if err := writeSettingsAtomic(path, initial, defaultSettingsIndent); err != nil {
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

// TestSettings_PreserveKeyOrderAndIndent 验证 issue #55 的两条核心约束：
// 1. init 后 top-level key 顺序 / hooks 内事件顺序 / 4 空格缩进保持原样，diff 只增 gw matcher
// 2. 立即 uninstall 再序列化，与 init 前的原文件字节相等
func TestSettings_PreserveKeyOrderAndIndent(t *testing.T) {
	fixture := filepath.Join("testdata", "settings_with_extras.json")
	original, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	withClaudeLookPath(t, func(string) (string, error) { return "/opt/test/claude", nil })
	if err := runInitWith(path, testGwPath, false, &stdout, &stderr); err != nil {
		t.Fatalf("init 失败: %v", err)
	}

	afterInit, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterStr := string(afterInit)

	// 缩进保留：fixture 用 4 空格，写回必须沿用
	if !strings.Contains(afterStr, "\n    \"theme\"") {
		t.Fatalf("4 空格缩进丢失, 前 160 字节:\n%s", afterStr[:min(160, len(afterStr))])
	}

	// top-level key 顺序保留：theme → mcpServers → env → hooks
	orderIdx := func(s, needle string) int { return strings.Index(s, needle) }
	idxTheme := orderIdx(afterStr, `"theme"`)
	idxMcp := orderIdx(afterStr, `"mcpServers"`)
	idxEnv := orderIdx(afterStr, `"env"`)
	idxHooks := orderIdx(afterStr, `"hooks"`)
	if idxTheme >= idxMcp || idxMcp >= idxEnv || idxEnv >= idxHooks {
		t.Fatalf("top-level key 顺序被重排: theme=%d mcpServers=%d env=%d hooks=%d",
			idxTheme, idxMcp, idxEnv, idxHooks)
	}

	// hooks 下事件顺序保留：PreToolUse 在 PostToolUse 前
	if orderIdx(afterStr, `"PreToolUse"`) >= orderIdx(afterStr, `"PostToolUse"`) {
		t.Fatal("hooks 内事件顺序被重排")
	}

	// 新增了 gw matcher
	if !strings.Contains(afterStr, `"_gw_managed"`) {
		t.Fatal("init 未写入 gw matcher")
	}

	// uninstall 后字节必须与原 fixture 完全相等
	if err := runUninstallWith(path, false, &stdout); err != nil {
		t.Fatalf("uninstall 失败: %v", err)
	}
	afterUninstall, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUninstall, original) {
		t.Fatalf("uninstall 后未恢复到原字节:\n--- want ---\n%s\n--- got ---\n%s",
			string(original), string(afterUninstall))
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

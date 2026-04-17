package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// countGwHooks 统计带有 _gw_managed 标记的 hook 条目数。
func countGwHooks(hooks []interface{}) int {
	n := 0
	for _, h := range hooks {
		m, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if v, ok := m["_gw_managed"].(bool); ok && v {
			n++
		}
	}
	return n
}

// hooksOf 从 settings 中取出 hooks 数组，找不到返回 nil。
func hooksOf(t *testing.T, settings map[string]interface{}) []interface{} {
	t.Helper()
	raw, ok := settings["hooks"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("hooks 不是数组: %T", raw)
	}
	return arr
}

// --- applyInitToSettings：三态逻辑 ---

// 情景 a：settings.json 不存在 → 由调用方传入空 map → 应创建 hooks 数组并插入 gw hook
func TestApplyInit_EmptySettings(t *testing.T) {
	settings := map[string]interface{}{}
	updated, status := applyInitToSettings(settings)
	if status != initStatusInstalled {
		t.Fatalf("期望 status=installed, 得到 %q", status)
	}
	hooks := hooksOf(t, updated)
	if len(hooks) != 1 {
		t.Fatalf("期望 1 条 hook, 得到 %d", len(hooks))
	}
	if got := countGwHooks(hooks); got != 1 {
		t.Fatalf("期望 1 条带标记 gw hook, 得到 %d", got)
	}
	entry := hooks[0].(map[string]interface{})
	if entry["hook"] != gwHookCommand {
		t.Fatalf("hook 命令不正确: %v", entry["hook"])
	}
	if entry["matcher"] != "Bash" {
		t.Fatalf("matcher 不正确: %v", entry["matcher"])
	}
	if v, _ := entry["_gw_managed"].(bool); !v {
		t.Fatal("_gw_managed 字段未置为 true")
	}
}

// 情景 b：存在 settings 但无 hooks 字段 → 与 a 等价
func TestApplyInit_NoHooksField(t *testing.T) {
	settings := map[string]interface{}{
		"theme": "dark",
	}
	updated, status := applyInitToSettings(settings)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	if updated["theme"] != "dark" {
		t.Fatal("原有字段丢失")
	}
	if countGwHooks(hooksOf(t, updated)) != 1 {
		t.Fatal("未插入 gw hook")
	}
}

// 情景 c：已有他人 hook（无 _gw_managed 标记）→ 追加 gw hook，不动原条目
func TestApplyInit_AppendAlongsideForeignHook(t *testing.T) {
	foreign := map[string]interface{}{
		"matcher": "Bash",
		"hook":    "echo foreign",
	}
	settings := map[string]interface{}{
		"hooks": []interface{}{foreign},
	}
	updated, status := applyInitToSettings(settings)
	if status != initStatusInstalled {
		t.Fatalf("期望 installed, 得到 %q", status)
	}
	hooks := hooksOf(t, updated)
	if len(hooks) != 2 {
		t.Fatalf("期望 2 条 hook, 得到 %d", len(hooks))
	}
	// 原条目必须保持原样
	first, _ := hooks[0].(map[string]interface{})
	if first["hook"] != "echo foreign" {
		t.Fatal("他人 hook 被改动")
	}
	if _, ok := first["_gw_managed"]; ok {
		t.Fatal("不应给他人 hook 添加 _gw_managed")
	}
	if countGwHooks(hooks) != 1 {
		t.Fatal("gw hook 数量不对")
	}
}

// 情景 d：已有 gw hook（带标记）→ 幂等，状态为 already
func TestApplyInit_AlreadyInstalled(t *testing.T) {
	existing := map[string]interface{}{
		"matcher":     "Bash",
		"hook":        gwHookCommand,
		"_gw_managed": true,
	}
	settings := map[string]interface{}{
		"hooks": []interface{}{existing},
	}
	updated, status := applyInitToSettings(settings)
	if status != initStatusAlready {
		t.Fatalf("期望 already, 得到 %q", status)
	}
	if len(hooksOf(t, updated)) != 1 {
		t.Fatal("幂等时不应新增 hook")
	}
}

// 情景 d-2：存在旧版 gw hook（hook 字符串含 "gw rewrite" 但无 _gw_managed 标记）
// 应视为已安装并就地迁移：不新增条目，原条目补上 _gw_managed: true 标记。
// 这样从 v0.x 无标记版本升级到新版的用户不会被追加第二条 hook。
func TestApplyInit_LegacyGwHookMigrated(t *testing.T) {
	legacy := map[string]interface{}{
		"matcher": "Bash",
		"hook":    gwHookCommand, // 字面量 `gw rewrite "$command"`
	}
	settings := map[string]interface{}{
		"hooks": []interface{}{legacy},
	}
	updated, status := applyInitToSettings(settings)
	if status != initStatusAlready {
		t.Fatalf("期望 already (视为已安装), 得到 %q", status)
	}
	hooks := hooksOf(t, updated)
	if len(hooks) != 1 {
		t.Fatalf("期望仍为 1 条 hook, 得到 %d", len(hooks))
	}
	migrated, _ := hooks[0].(map[string]interface{})
	if v, _ := migrated["_gw_managed"].(bool); !v {
		t.Fatal("迁移后原条目应补上 _gw_managed: true 标记")
	}
	if migrated["hook"] != gwHookCommand {
		t.Errorf("hook 字面量不应被修改，得到 %v", migrated["hook"])
	}
}

// TestApplyInit_LegacyCustomGwRewrite 旧版可能被用户调整过 hook 参数，
// 只要字符串仍含 "gw rewrite" 关键字就视为 gw 管理，补标记，不重复安装。
func TestApplyInit_LegacyCustomGwRewrite(t *testing.T) {
	legacy := map[string]interface{}{
		"matcher": "Bash",
		"hook":    "gw rewrite --verbose $command",
	}
	settings := map[string]interface{}{
		"hooks": []interface{}{legacy},
	}
	updated, status := applyInitToSettings(settings)
	if status != initStatusAlready {
		t.Fatalf("期望 already, 得到 %q", status)
	}
	hooks := hooksOf(t, updated)
	if len(hooks) != 1 {
		t.Fatalf("期望仍为 1 条 hook, 得到 %d", len(hooks))
	}
	migrated, _ := hooks[0].(map[string]interface{})
	if v, _ := migrated["_gw_managed"].(bool); !v {
		t.Fatal("应迁移补标记")
	}
}

// --- applyUninstallToSettings：只移除带 _gw_managed 的条目 ---

// 情景 f：uninstall 后他人 hook 仍在
func TestApplyUninstall_KeepsForeignHooks(t *testing.T) {
	foreign := map[string]interface{}{
		"matcher": "Bash",
		"hook":    "echo foreign",
	}
	gw := map[string]interface{}{
		"matcher":     "Bash",
		"hook":        gwHookCommand,
		"_gw_managed": true,
	}
	settings := map[string]interface{}{
		"hooks": []interface{}{foreign, gw},
	}
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	hooks := hooksOf(t, updated)
	if len(hooks) != 1 {
		t.Fatalf("期望剩 1 条 hook, 得到 %d", len(hooks))
	}
	if hooks[0].(map[string]interface{})["hook"] != "echo foreign" {
		t.Fatal("他人 hook 丢失")
	}
}

// uninstall：用户手动改了 hook 命令，但 _gw_managed 标记仍在 → 照样移除
func TestApplyUninstall_MatchesByMarkerNotCommand(t *testing.T) {
	mutated := map[string]interface{}{
		"matcher":     "Bash",
		"hook":        "gw rewrite --custom $command",
		"_gw_managed": true,
	}
	settings := map[string]interface{}{
		"hooks": []interface{}{mutated},
	}
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusRemoved {
		t.Fatalf("期望 removed, 得到 %q", status)
	}
	// hooks 应全部移除，字段被清掉
	if _, ok := updated["hooks"]; ok {
		t.Fatal("hooks 字段应已清理")
	}
}

// uninstall：无 hooks 字段 → not-found
func TestApplyUninstall_NoHooksField(t *testing.T) {
	settings := map[string]interface{}{}
	_, status := applyUninstallToSettings(settings)
	if status != uninstallStatusNotFound {
		t.Fatalf("期望 not-found, 得到 %q", status)
	}
}

// uninstall：hooks 里没有任何 _gw_managed 条目 → not-found，且不动其他条目
func TestApplyUninstall_NoGwManagedEntry(t *testing.T) {
	foreign := map[string]interface{}{
		"matcher": "Bash",
		"hook":    "echo foreign",
	}
	settings := map[string]interface{}{
		"hooks": []interface{}{foreign},
	}
	updated, status := applyUninstallToSettings(settings)
	if status != uninstallStatusNotFound {
		t.Fatalf("期望 not-found, 得到 %q", status)
	}
	if len(hooksOf(t, updated)) != 1 {
		t.Fatal("他人 hook 不应被动")
	}
}

// --- 文件层：读/写/备份/atomic rename ---

// writeSettingsAtomic：先写临时文件再 rename，目标文件存在时先备份为 .bak
func TestWriteSettingsAtomic_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// 先写入初始文件
	initial := map[string]interface{}{"v": "old"}
	if err := writeSettingsAtomic(path, initial); err != nil {
		t.Fatalf("写入初始失败: %v", err)
	}
	// 再写入新内容 → 应生成 .bak（内容为 old）
	next := map[string]interface{}{"v": "new"}
	if err := writeSettingsAtomic(path, next); err != nil {
		t.Fatalf("写入新内容失败: %v", err)
	}
	got := readJSON(t, path)
	if got["v"] != "new" {
		t.Fatalf("新内容未生效: %v", got["v"])
	}
	bak := readJSON(t, path+".bak")
	if bak["v"] != "old" {
		t.Fatalf("备份内容不正确: %v", bak["v"])
	}
}

// 首次写入（文件不存在）时不应因没有备份源而失败
func TestWriteSettingsAtomic_FirstWriteNoBak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := writeSettingsAtomic(path, map[string]interface{}{"v": "1"}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatal("首次写入不应生成 .bak")
	}
	// 首次写入的新文件应为 0600（保守：hook 命令可能敏感）
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("首次写入 mode 期望 0600，得到 %o", perm)
	}
}

// TestWriteSettingsAtomic_PreservesMode 备份与新写入的文件应保留原 settings.json 的 mode。
func TestWriteSettingsAtomic_PreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// 人为先写一个 0600 模式的 settings.json（不经 writeSettingsAtomic，避开
	// "首次写入"分支，模拟用户已有的严格权限文件）
	if err := os.WriteFile(path, []byte(`{"v":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	next := map[string]interface{}{"v": "new"}
	if err := writeSettingsAtomic(path, next); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// 新写入的 path 应保留 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("新文件 mode 期望 0600，得到 %o", perm)
	}

	// 备份文件也应保留 0600
	bakInfo, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if perm := bakInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("备份文件 mode 期望 0600，得到 %o", perm)
	}
}

// --- 端到端：runInitWith / runUninstallWith 通过注入 settings path 不碰真实 HOME ---

// 情景 a + e：不存在 settings + dry-run → 不落盘，stdout 打印完整 JSON
func TestRunInit_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var buf strings.Builder
	if err := runInitWith(path, true, &buf); err != nil {
		t.Fatalf("dry-run 失败: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry-run 不应创建文件")
	}
	out := buf.String()
	// 在 JSON 序列化后 hook 命令中的 " 会被转义；改为按子串匹配。
	if !strings.Contains(out, "gw rewrite") {
		t.Fatalf("dry-run 输出未包含 hook 命令: %s", out)
	}
	if !strings.Contains(out, "_gw_managed") {
		t.Fatalf("dry-run 输出未包含 _gw_managed 标记: %s", out)
	}
}

// 情景 a：正常写入后再运行一次 → 幂等
func TestRunInit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var buf strings.Builder
	if err := runInitWith(path, false, &buf); err != nil {
		t.Fatalf("第一次 init 失败: %v", err)
	}
	// 第二次
	buf.Reset()
	if err := runInitWith(path, false, &buf); err != nil {
		t.Fatalf("第二次 init 失败: %v", err)
	}
	settings := readJSON(t, path)
	if got := countGwHooks(hooksOf(t, settings)); got != 1 {
		t.Fatalf("幂等失败，gw hook 数 = %d", got)
	}
}

// 情景 f：uninstall 后他人 hook 仍在，并验证文件级行为
func TestRunUninstall_KeepsForeignHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// 先写入一个已有他人 hook 的 settings
	initial := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{"matcher": "Bash", "hook": "echo foreign"},
		},
	}
	if err := writeSettingsAtomic(path, initial); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	// init（追加 gw hook）
	var buf strings.Builder
	if err := runInitWith(path, false, &buf); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	// uninstall → 应只移除 gw hook
	buf.Reset()
	if err := runUninstallWith(path, false, &buf); err != nil {
		t.Fatalf("uninstall 失败: %v", err)
	}
	settings := readJSON(t, path)
	hooks := hooksOf(t, settings)
	if len(hooks) != 1 {
		t.Fatalf("期望剩 1 条 hook, 得到 %d", len(hooks))
	}
	if hooks[0].(map[string]interface{})["hook"] != "echo foreign" {
		t.Fatal("他人 hook 被误删")
	}
}

// uninstall --dry-run：不落盘
func TestRunUninstall_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var buf strings.Builder
	if err := runInitWith(path, false, &buf); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	// 读入文件的 mtime 作为基线
	stat1, _ := os.Stat(path)
	buf.Reset()
	if err := runUninstallWith(path, true, &buf); err != nil {
		t.Fatalf("dry-run uninstall 失败: %v", err)
	}
	stat2, _ := os.Stat(path)
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Fatal("dry-run uninstall 不应修改文件")
	}
	// 文件内容应仍然包含 gw hook
	if countGwHooks(hooksOf(t, readJSON(t, path))) != 1 {
		t.Fatal("dry-run 不应移除 gw hook")
	}
	// dry-run 输出应展示移除后的 settings（不再包含 _gw_managed）
	out := buf.String()
	if strings.Contains(out, "_gw_managed") {
		t.Fatalf("dry-run 输出应展示移除后的 settings, 不应再含 _gw_managed: %s", out)
	}
}

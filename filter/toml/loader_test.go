package toml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempDirs 临时重定向 user / project 规则路径并在测试结束后恢复。
func withTempDirs(t *testing.T, userDir, projectRoot string) {
	t.Helper()
	origUser := userRulesDirFn
	origProject := projectRulesRootFn
	userRulesDirFn = func() string { return userDir }
	projectRulesRootFn = func() string { return projectRoot }
	t.Cleanup(func() {
		userRulesDirFn = origUser
		projectRulesRootFn = origProject
	})
}

// findLoadedRule 按 ID 检索加载后的规则
func findLoadedRule(rules []LoadedRule, id string) *LoadedRule {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i]
		}
	}
	return nil
}

// TestLoadAllRules_BuiltinOnly 没配用户/项目目录时应只返回内置规则。
func TestLoadAllRules_BuiltinOnly(t *testing.T) {
	withTempDirs(t, "", "")
	rules := LoadAllRules()
	if len(rules) == 0 {
		t.Fatal("预期至少加载到内置规则")
	}
	for _, r := range rules {
		if r.Source != SourceBuiltin {
			t.Errorf("规则 %s 应来自 builtin，实际 %s", r.ID, r.Source)
		}
	}
	// 应包含 docker.ps
	if findLoadedRule(rules, "docker.ps") == nil {
		t.Error("预期内置规则集合包含 docker.ps")
	}
}

// TestLoadAllRules_UserOverridesBuiltin 用户层同 ID 规则覆盖内置。
func TestLoadAllRules_UserOverridesBuiltin(t *testing.T) {
	userDir := t.TempDir()
	override := `[docker.ps]
match = "docker ps"
max_lines = 7
`
	userFile := filepath.Join(userDir, "docker-user.toml")
	if err := os.WriteFile(userFile, []byte(override), 0644); err != nil {
		t.Fatal(err)
	}

	withTempDirs(t, userDir, "")
	rules := LoadAllRules()

	got := findLoadedRule(rules, "docker.ps")
	if got == nil {
		t.Fatal("未找到 docker.ps 规则")
	}
	if got.Rule.MaxLines != 7 {
		t.Errorf("用户层未覆盖 builtin: MaxLines=%d, 期望 7", got.Rule.MaxLines)
	}
	if !strings.HasPrefix(got.Source, "user://") {
		t.Errorf("source 前缀错误: %s", got.Source)
	}
	if !strings.HasSuffix(got.Source, userFile) {
		t.Errorf("source 应指向实际文件，当前 %s", got.Source)
	}
}

// TestLoadAllRules_ProjectOverridesUser 项目层优先级最高。
func TestLoadAllRules_ProjectOverridesUser(t *testing.T) {
	userDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(userDir, "d.toml"),
		[]byte("[docker.ps]\nmatch=\"docker ps\"\nmax_lines=5\n"), 0644); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	rulesDir := filepath.Join(projectRoot, ".gw", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "d.toml"),
		[]byte("[docker.ps]\nmatch=\"docker ps\"\nmax_lines=11\n"), 0644); err != nil {
		t.Fatal(err)
	}

	withTempDirs(t, userDir, projectRoot)
	rules := LoadAllRules()

	got := findLoadedRule(rules, "docker.ps")
	if got == nil {
		t.Fatal("未找到 docker.ps 规则")
	}
	if got.Rule.MaxLines != 11 {
		t.Errorf("项目层未覆盖 user: MaxLines=%d, 期望 11", got.Rule.MaxLines)
	}
	if !strings.HasPrefix(got.Source, "project://") {
		t.Errorf("source 前缀错误: %s", got.Source)
	}
}

// TestLoadAllRules_NewRuleFromUser 用户层可以新增规则（不在 builtin 中）。
func TestLoadAllRules_NewRuleFromUser(t *testing.T) {
	userDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(userDir, "custom.toml"),
		[]byte("[myapp.logs]\nmatch=\"myapp logs\"\ntail_lines=20\n"), 0644); err != nil {
		t.Fatal(err)
	}

	withTempDirs(t, userDir, "")
	rules := LoadAllRules()

	got := findLoadedRule(rules, "myapp.logs")
	if got == nil {
		t.Fatal("未加载用户层新增规则 myapp.logs")
	}
	if got.Rule.TailLines != 20 {
		t.Errorf("TailLines=%d, 期望 20", got.Rule.TailLines)
	}
}

// TestLoadAllRules_BrokenFileSkipped 破损的 TOML 不应让加载流程中断。
func TestLoadAllRules_BrokenFileSkipped(t *testing.T) {
	userDir := t.TempDir()
	// 坏文件
	if err := os.WriteFile(filepath.Join(userDir, "a-broken.toml"),
		[]byte("this is not = = valid toml [[[["), 0644); err != nil {
		t.Fatal(err)
	}
	// 好文件
	if err := os.WriteFile(filepath.Join(userDir, "b-good.toml"),
		[]byte("[good.rule]\nmatch=\"hello\"\nmax_lines=9\n"), 0644); err != nil {
		t.Fatal(err)
	}

	withTempDirs(t, userDir, "")
	// 不应 panic
	rules := LoadAllRules()
	if findLoadedRule(rules, "good.rule") == nil {
		t.Error("好文件中的规则未加载")
	}
}

// TestLoadAllRules_DisabledRemovesBuiltin disabled=true 应剔除同 ID 的内置规则。
func TestLoadAllRules_DisabledRemovesBuiltin(t *testing.T) {
	userDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(userDir, "disable.toml"),
		[]byte("[docker.ps]\nmatch=\"docker ps\"\ndisabled=true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	withTempDirs(t, userDir, "")
	rules := LoadAllRules()

	if findLoadedRule(rules, "docker.ps") != nil {
		t.Error("disabled=true 应让 docker.ps 从最终集合中剔除")
	}
	// 其他内置规则仍应存在
	if findLoadedRule(rules, "kubectl.get") == nil {
		t.Error("其他内置规则不应被影响")
	}
}

// TestFindProjectRulesDir_WalkUp 从子目录向上找 .gw/rules
func TestFindProjectRulesDir_WalkUp(t *testing.T) {
	root := t.TempDir()
	rulesDir := filepath.Join(root, ".gw", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	got := findProjectRulesDir(sub)
	if got != rulesDir {
		t.Errorf("未能向上找到规则目录: got=%s want=%s", got, rulesDir)
	}
}

// TestFindProjectRulesDir_StopAtGit .git 之上的 .gw/rules 不应被访问
func TestFindProjectRulesDir_StopAtGit(t *testing.T) {
	top := t.TempDir()
	// 顶层有 .gw/rules 但中间有 .git，不应越过
	if err := os.MkdirAll(filepath.Join(top, ".gw", "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(top, "project")
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(inner, "src")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	got := findProjectRulesDir(sub)
	if got != "" {
		t.Errorf("遇到 .git 后应停止向上查找，但返回了 %s", got)
	}
}

// TestFindProjectRulesDir_MissingReturnsEmpty 无 .gw/rules 时返回空字符串
func TestFindProjectRulesDir_MissingReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	got := findProjectRulesDir(root)
	if got != "" {
		t.Errorf("预期返回空字符串，得到 %s", got)
	}
}

// TestFindProjectRulesDir_StopAtGitFile git worktree 场景下 .git 是**文件**
// （内含 gitdir: 指向主库），同样应被识别为项目边界而停止向上查找。
func TestFindProjectRulesDir_StopAtGitFile(t *testing.T) {
	top := t.TempDir()
	// 顶层放一个 .gw/rules 诱饵（应被屏蔽）
	if err := os.MkdirAll(filepath.Join(top, ".gw", "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(top, "worktree")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	// .git 是文件（git worktree / submodule 场景）
	if err := os.WriteFile(filepath.Join(inner, ".git"),
		[]byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(inner, "src")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	got := findProjectRulesDir(sub)
	if got != "" {
		t.Errorf("遇到 .git 文件（worktree 场景）后应停止向上查找，但返回了 %s", got)
	}
}

// TestLoadEngine_IntegratesLoaded LoadEngine 构造的实例应与 LoadAllRules 一致。
func TestLoadEngine_IntegratesLoaded(t *testing.T) {
	withTempDirs(t, "", "")
	eng := LoadEngine()
	if len(eng.Loaded) == 0 {
		t.Fatal("LoadEngine 未加载任何规则")
	}
}

// TestLoadAllRules_NonexistentUserDirWarnSafe 用户目录不存在时静默跳过
func TestLoadAllRules_NonexistentUserDirWarnSafe(t *testing.T) {
	withTempDirs(t, filepath.Join(t.TempDir(), "nope"), "")
	// 不应 panic
	_ = LoadAllRules()
}

// TestLoadAllRules_SortedByID 返回值按 ID 排序
func TestLoadAllRules_SortedByID(t *testing.T) {
	withTempDirs(t, "", "")
	rules := LoadAllRules()
	for i := 1; i < len(rules); i++ {
		if rules[i-1].ID > rules[i].ID {
			t.Errorf("未按 ID 排序: %s > %s", rules[i-1].ID, rules[i].ID)
		}
	}
}

package cmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const gwBinary = "/tmp/gw-integration-test"

// resolveSrcRoot 返回 gw 模块根目录。优先 GW_SOURCE_ROOT（用于 git worktree
// 构建当前 worktree 代码的场景），否则从当前 cwd 向上找 go.mod。
// Go 测试的 cwd 始终是包目录，所以此函数在任何环境（本地/CI）都可用。
func resolveSrcRoot() string {
	if env := os.Getenv("GW_SOURCE_ROOT"); env != "" {
		return env
	}
	dir, err := os.Getwd()
	if err != nil {
		panic("resolveSrcRoot: os.Getwd failed: " + err.Error())
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("resolveSrcRoot: 找不到 go.mod，起点 " + dir)
		}
		dir = parent
	}
}

func TestMain(m *testing.M) {
	build := exec.Command("go", "build", "-o", gwBinary, ".")
	build.Dir = resolveSrcRoot()
	if err := build.Run(); err != nil {
		panic("build failed: " + err.Error())
	}
	os.Exit(m.Run())
}

// TestExec_Passthrough 测试无过滤器命令透传
func TestExec_Passthrough(t *testing.T) {
	cmd := exec.Command(gwBinary, "exec", "echo", "hello world")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec echo 失败: %v, output: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "hello world" {
		t.Errorf("期望 'hello world', 得到 %q", got)
	}
}

// TestExec_GitStatus 测试 git status 过滤器去除独立教学提示行
func TestExec_GitStatus(t *testing.T) {
	cmd := exec.Command(gwBinary, "exec", "git", "status")
	cmd.Dir = resolveSrcRoot()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec git status 失败: %v, output: %s", err, out)
	}
	output := string(out)
	// 过滤器应去除以 (use "git 开头的独立教学提示行
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `(use "git restore`) {
			t.Errorf("输出不应包含独立教学提示行 %q", line)
		}
	}
	// 确保主要状态信息保留。GitHub Actions 的 pull_request 事件 checkout 出
	// 一个 merge commit（HEAD detached at pull/N/merge），所以也要接受 detached 状态。
	if !strings.Contains(output, "On branch") && !strings.Contains(output, "HEAD detached") {
		t.Errorf("输出应包含分支信息（On branch 或 HEAD detached）, got:\n%s", output)
	}
}

// runRewriteBinary 通过 stdin 把 Claude Code hook JSON 喂给 gw rewrite 子进程，
// 返回 (stdout, exitCode)。gw rewrite 的协议：exit 0 永远，stdout 要么空（静默放行）
// 要么是 hookSpecificOutput JSON。
func runRewriteBinary(t *testing.T, hookJSON string) (string, int) {
	t.Helper()
	cmd := exec.Command(gwBinary, "rewrite")
	cmd.Stdin = strings.NewReader(hookJSON)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("rewrite 调用异常: %v, output: %s", err, out)
		}
	}
	return string(out), code
}

// 可改写命令 → stdout 出 hookSpecificOutput JSON，updatedInput.command 含 gw exec
func TestRewrite_BinarySimple(t *testing.T) {
	out, code := runRewriteBinary(t, `{"tool_name":"Bash","tool_input":{"command":"git status"}}`)
	if code != 0 {
		t.Fatalf("rewrite 应 exit 0, 得到 %d, output: %s", code, out)
	}
	if !strings.Contains(out, `"hookEventName": "PreToolUse"`) {
		t.Errorf("stdout 应含 PreToolUse: %q", out)
	}
	if !strings.Contains(out, "exec git status") {
		t.Errorf("stdout 应含 exec git status: %q", out)
	}
}

// 链式命令 → 每个可代理段都被改写
func TestRewrite_BinaryChain(t *testing.T) {
	out, code := runRewriteBinary(t, `{"tool_name":"Bash","tool_input":{"command":"mvn clean \u0026\u0026 mvn test"}}`)
	if code != 0 {
		t.Fatalf("rewrite 应 exit 0, 得到 %d, output: %s", code, out)
	}
	if !strings.Contains(out, "exec mvn clean") {
		t.Errorf("未改写 mvn clean: %q", out)
	}
	if !strings.Contains(out, "exec mvn test") {
		t.Errorf("未改写 mvn test: %q", out)
	}
}

// 管道命令（含 |）shell 判定不可代理 → stdout 空、exit 0（静默放行，让 Claude Code 走默认）
func TestRewrite_BinaryPipeSilent(t *testing.T) {
	out, code := runRewriteBinary(t, `{"tool_name":"Bash","tool_input":{"command":"git log | grep fix"}}`)
	if code != 0 {
		t.Fatalf("rewrite 应 exit 0, 得到 %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("管道命令应静默放行（stdout 空），得到: %q", out)
	}
}

// 不在注册表的命令 → stdout 空、exit 0
func TestRewrite_BinaryNoMatchSilent(t *testing.T) {
	out, code := runRewriteBinary(t, `{"tool_name":"Bash","tool_input":{"command":"python script.py"}}`)
	if code != 0 {
		t.Fatalf("rewrite 应 exit 0, 得到 %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("不匹配应静默放行，得到: %q", out)
	}
}

// TestExec_DumpRaw_Batch 验证批量路径 --dump-raw 能把原始输出写入指定文件。
func TestExec_DumpRaw_Batch(t *testing.T) {
	tmpDir := t.TempDir()
	dumpPath := filepath.Join(tmpDir, "raw.txt")
	// echo 无专用过滤器，走批量透传路径
	cmd := exec.Command(gwBinary, "exec", "--dump-raw", dumpPath, "echo", "hello raw world")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir) // 隔离 tracking.db
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec 失败: %v, output: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "hello raw world" {
		t.Errorf("stdout 期望 'hello raw world', 得到 %q", got)
	}

	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("读取 dump 文件失败: %v", err)
	}
	if !strings.Contains(string(data), "hello raw world") {
		t.Errorf("dump 文件应包含 'hello raw world', 得到 %q", string(data))
	}
}

// TestExec_DumpRaw_Equals 验证 --dump-raw=PATH 等号形式同样工作。
func TestExec_DumpRaw_Equals(t *testing.T) {
	tmpDir := t.TempDir()
	dumpPath := filepath.Join(tmpDir, "raw.txt")
	cmd := exec.Command(gwBinary, "exec", "--dump-raw="+dumpPath, "echo", "eq form")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec 失败: %v, output: %s", err, out)
	}

	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("读取 dump 文件失败: %v", err)
	}
	if !strings.Contains(string(data), "eq form") {
		t.Errorf("dump 文件应包含 'eq form', 得到 %q", string(data))
	}
}

// TestExec_DumpRaw_WriteFail 写入不存在目录下的文件应给 warning 但不中断主流程。
func TestExec_DumpRaw_WriteFail(t *testing.T) {
	tmpDir := t.TempDir()
	// /nonexistent/.../raw.txt 无法创建
	badPath := "/nonexistent-gw-dir-abc/raw.txt"
	cmd := exec.Command(gwBinary, "exec", "--dump-raw", badPath, "echo", "still ok")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec 不应因 dump 失败而中断: %v, output: %s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "still ok") {
		t.Errorf("主输出应包含 'still ok', 得到 %q", output)
	}
	if !strings.Contains(output, "warning") {
		t.Errorf("应包含 warning, 得到 %q", output)
	}
}

// TestExec_DumpRaw_Stream 验证流式路径 --dump-raw 也能把原始输出写入文件。
// 使用 java -jar /nonexistent.jar 触发 SpringBootFilter（StreamFilter），
// java 会快速报错退出，stderr 含 "Unable to access jarfile"，会写入 raw buffer。
func TestExec_DumpRaw_Stream(t *testing.T) {
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java 不可用，跳过流式路径集成测试")
	}
	tmpDir := t.TempDir()
	dumpPath := filepath.Join(tmpDir, "raw-stream.txt")
	cmd := exec.Command(gwBinary, "exec", "--dump-raw", dumpPath,
		"java", "-jar", "/nonexistent-gw-test.jar")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	_, _ = cmd.CombinedOutput()
	// 退出码可能非零，这里不校验；关键是 dump 文件应存在且含错误文本。

	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("读取 dump 文件失败（流式路径未写入?）: %v", err)
	}
	if !strings.Contains(string(data), "nonexistent-gw-test.jar") {
		t.Errorf("流式 dump 文件应包含 jar 文件名, 得到 %q", string(data))
	}
}

// TestVersion_Command 验证 `gw version` 和 `gw --version` 输出版本字符串。
func TestVersion_Command(t *testing.T) {
	out, err := exec.Command(gwBinary, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("gw version 失败: %v, output: %s", err, out)
	}
	if !strings.Contains(string(out), "gw version") {
		t.Errorf("输出应包含 'gw version', 得到 %q", string(out))
	}

	out, err = exec.Command(gwBinary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("gw --version 失败: %v, output: %s", err, out)
	}
	if !strings.Contains(string(out), "gw version") {
		t.Errorf("--version 输出应包含 'gw version', 得到 %q", string(out))
	}
}

// TestSummary_NoData 测试空数据库不崩溃
func TestSummary_NoData(t *testing.T) {
	// 设置一个新的 HOME 目录，使 DB 路径指向空目录
	tmpDir := t.TempDir()
	cmd := exec.Command(gwBinary, "summary")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	out, err := cmd.CombinedOutput()
	// summary 命令即使无数据也不应 panic
	output := string(out)
	if strings.Contains(output, "panic") {
		t.Errorf("summary 命令不应 panic, output:\n%s", output)
	}
	// 允许正常退出或因空数据退出非零（但不能 panic/crash）
	_ = err
}

// TestSummary_GainAlias 确保 gain 仍作为 summary 的别名可用（向后兼容）。
func TestSummary_GainAlias(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := exec.Command(gwBinary, "gain")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if strings.Contains(output, "panic") {
		t.Errorf("gain alias 不应 panic, output:\n%s", output)
	}
	if strings.Contains(output, "unknown command") {
		t.Errorf("gain 应作为 summary 别名识别, output:\n%s", output)
	}
	_ = err
}

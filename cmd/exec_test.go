package cmd_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const gwBinary = "/tmp/gw-integration-test"

func TestMain(m *testing.M) {
	// 构建二进制到临时目录
	build := exec.Command("go", "build", "-o", gwBinary, ".")
	build.Dir = "/private/tmp/gw"
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
	cmd.Dir = "/private/tmp/gw"
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
	// 确保主要状态信息保留
	if !strings.Contains(output, "On branch") && !strings.Contains(output, "branch") {
		t.Errorf("输出应包含分支信息, got:\n%s", output)
	}
}

// TestRewrite_Simple 测试单命令改写
func TestRewrite_Simple(t *testing.T) {
	cmd := exec.Command(gwBinary, "rewrite", "git status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rewrite 失败: %v, output: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "gw exec git status" {
		t.Errorf("期望 'gw exec git status', 得到 %q", got)
	}
}

// TestRewrite_Chain 测试链式命令改写
func TestRewrite_Chain(t *testing.T) {
	cmd := exec.Command(gwBinary, "rewrite", "mvn clean && mvn test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rewrite chain 失败: %v, output: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, "gw exec mvn clean") {
		t.Errorf("输出应包含 'gw exec mvn clean', 得到 %q", got)
	}
	if !strings.Contains(got, "gw exec mvn test") {
		t.Errorf("输出应包含 'gw exec mvn test', 得到 %q", got)
	}
}

// TestRewrite_Pipe 测试管道命令不可改写
func TestRewrite_Pipe(t *testing.T) {
	cmd := exec.Command(gwBinary, "rewrite", "git log | grep fix")
	err := cmd.Run()
	if err == nil {
		t.Fatal("管道命令应返回非零退出码")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("期望 ExitError, 得到 %T", err)
	}
	if exitErr.ExitCode() == 0 {
		t.Error("管道命令退出码不应为 0")
	}
}

// TestRewrite_NoMatch 测试无匹配命令不改写
func TestRewrite_NoMatch(t *testing.T) {
	cmd := exec.Command(gwBinary, "rewrite", "python script.py")
	err := cmd.Run()
	if err == nil {
		t.Fatal("无匹配命令应返回非零退出码")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("期望 ExitError, 得到 %T", err)
	}
	if exitErr.ExitCode() == 0 {
		t.Error("无匹配命令退出码不应为 0")
	}
}

// TestGain_NoData 测试空数据库不崩溃
func TestGain_NoData(t *testing.T) {
	// 设置一个不存在的 HOME 目录，使 DB 路径指向空目录
	tmpDir, err := os.MkdirTemp("", "gw-gain-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command(gwBinary, "gain")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	out, err := cmd.CombinedOutput()
	// gain 命令即使无数据也不应 panic
	output := string(out)
	if strings.Contains(output, "panic") {
		t.Errorf("gain 命令不应 panic, output:\n%s", output)
	}
	// 允许正常退出或因空数据退出非零（但不能 panic/crash）
	_ = err
}

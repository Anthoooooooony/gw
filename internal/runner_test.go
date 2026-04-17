package internal

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunCommand_Success(t *testing.T) {
	result, err := RunCommand("echo", []string{"hello"})
	if err != nil {
		t.Fatalf("期望无错误，得到: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("期望退出码 0，得到: %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("期望 stdout 为 %q，得到: %q", "hello\n", result.Stdout)
	}
}

func TestRunCommand_NonZeroExit(t *testing.T) {
	result, err := RunCommand("sh", []string{"-c", "echo fail >&2; exit 42"})
	if err != nil {
		t.Fatalf("非零退出码不应返回 error，得到: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("期望退出码 42，得到: %d", result.ExitCode)
	}
	if result.Stderr != "fail\n" {
		t.Errorf("期望 stderr 为 %q，得到: %q", "fail\n", result.Stderr)
	}
}

func TestRunCommand_CommandNotFound(t *testing.T) {
	result, err := RunCommand("this_command_does_not_exist_xyz", nil)
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
	if result != nil {
		t.Errorf("期望 result 为 nil，得到: %+v", result)
	}
}

// TestRunCommand_Timeout 短超时值触发 SIGTERM，进程被终止，返回 ExitCode 124
func TestRunCommand_Timeout(t *testing.T) {
	os.Setenv("GW_CMD_TIMEOUT", "300ms")
	defer os.Unsetenv("GW_CMD_TIMEOUT")

	start := time.Now()
	result, err := RunCommand("sh", []string{"-c", "sleep 30"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("超时不应返回 Go error，得到: %v", err)
	}
	if result == nil {
		t.Fatal("期望 result 非 nil")
	}
	if result.ExitCode != 124 {
		t.Errorf("期望 ExitCode 124 (timeout)，得到 %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "gw: command timed out") {
		t.Errorf("期望 stderr 包含超时提示，得到: %q", result.Stderr)
	}
	// 宽限期 5s + 超时 300ms，应在 8s 内完成
	if elapsed > 8*time.Second {
		t.Errorf("超时终止耗时过长: %v", elapsed)
	}
}

// TestRunCommand_TimeoutDisabled 超时禁用时长命令不被打断
func TestRunCommand_TimeoutDisabled(t *testing.T) {
	os.Setenv("GW_CMD_TIMEOUT", "0")
	defer os.Unsetenv("GW_CMD_TIMEOUT")

	// 使用一个会在 600ms 内结束的命令；如果超时被误触发会返回 124
	result, err := RunCommand("sh", []string{"-c", "sleep 0.6; echo done"})
	if err != nil {
		t.Fatalf("不应返回 error，得到: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("期望 ExitCode 0，得到 %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "done") {
		t.Errorf("期望输出包含 'done'，得到 %q", result.Stdout)
	}
}

// TestRunCommand_TimeoutSIGKILLGrace trap SIGTERM 后 5s 内被 SIGKILL
func TestRunCommand_TimeoutSIGKILLGrace(t *testing.T) {
	os.Setenv("GW_CMD_TIMEOUT", "300ms")
	defer os.Unsetenv("GW_CMD_TIMEOUT")

	start := time.Now()
	// trap "" TERM 忽略 SIGTERM，必须靠 SIGKILL 收掉
	result, err := RunCommand("bash", []string{"-c", `trap "" TERM; sleep 30`})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("不应返回 Go error，得到: %v", err)
	}
	if result.ExitCode != 124 {
		t.Errorf("期望 ExitCode 124，得到 %d", result.ExitCode)
	}
	// 300ms + 5s 宽限 + 一点余量，应在 7.5s 内完成
	if elapsed > 7500*time.Millisecond {
		t.Errorf("SIGKILL 宽限耗时过长: %v", elapsed)
	}
	// 确认至少经过了宽限期（说明 SIGTERM 确实被忽略了）
	if elapsed < 4500*time.Millisecond {
		t.Errorf("SIGKILL 生效过早（疑似未经过宽限期）: %v", elapsed)
	}
	if !strings.Contains(result.Stderr, "SIGKILL") {
		t.Errorf("期望 stderr 提到 SIGKILL，得到 %q", result.Stderr)
	}
}

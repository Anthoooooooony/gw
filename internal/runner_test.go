package internal

import (
	"testing"
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

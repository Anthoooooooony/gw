package internal

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunCommandStreaming_MultiLine(t *testing.T) {
	var lines []string
	code, err := RunCommandStreaming("printf", []string{"line1\nline2\nline3\n"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	expected := []string{"line1", "line2", "line3"}
	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("line[%d]: expected %q, got %q", i, want, lines[i])
		}
	}
}

func TestRunCommandStreaming_ExitCode(t *testing.T) {
	var lines []string
	code, err := RunCommandStreaming("sh", []string{"-c", "echo hello; exit 42"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 42 {
		t.Fatalf("expected exit code 42, got %d", code)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("expected [\"hello\"], got %v", lines)
	}
}

func TestRunCommandStreamingFull_Stderr(t *testing.T) {
	var lines []string
	var stderrBuf bytes.Buffer
	code, err := RunCommandStreamingFull("sh", []string{"-c", "echo out; echo err >&2"}, func(line string) {
		lines = append(lines, line)
	}, &stderrBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if len(lines) != 1 || lines[0] != "out" {
		t.Fatalf("expected stdout [\"out\"], got %v", lines)
	}
	stderrOutput := strings.TrimSpace(stderrBuf.String())
	if stderrOutput != "err" {
		t.Fatalf("expected stderr \"err\", got %q", stderrOutput)
	}
}

func TestRunCommandStreaming_NotFound(t *testing.T) {
	_, err := RunCommandStreaming("nonexistent_command_xyz_12345", nil, func(line string) {})
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}
}

// TestRunCommandStreaming_Timeout 流式路径短超时触发 SIGTERM，ExitCode 映射为 124
func TestRunCommandStreaming_Timeout(t *testing.T) {
	_ = os.Setenv("GW_CMD_TIMEOUT", "300ms")
	defer func() { _ = os.Unsetenv("GW_CMD_TIMEOUT") }()

	var lines []string
	var stderrBuf bytes.Buffer
	start := time.Now()
	code, err := RunCommandStreamingFull("sh", []string{"-c", "echo started; sleep 30"}, func(line string) {
		lines = append(lines, line)
	}, &stderrBuf)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("超时不应返回 Go error，得到: %v", err)
	}
	if code != 124 {
		t.Errorf("期望 exitCode 124，得到 %d", code)
	}
	if elapsed > 8*time.Second {
		t.Errorf("超时终止耗时过长: %v", elapsed)
	}
	// 超时信息应写入 stderr（便于调用方捕获）
	if !strings.Contains(stderrBuf.String(), "gw: command timed out") {
		t.Errorf("期望 stderr 包含超时提示，得到 %q", stderrBuf.String())
	}
}

// TestRunCommandStreaming_TimeoutDisabled 禁用后长命令完整执行
func TestRunCommandStreaming_TimeoutDisabled(t *testing.T) {
	_ = os.Setenv("GW_CMD_TIMEOUT", "off")
	defer func() { _ = os.Unsetenv("GW_CMD_TIMEOUT") }()

	var lines []string
	var stderrBuf bytes.Buffer
	code, err := RunCommandStreamingFull("sh", []string{"-c", "sleep 0.6; echo done"}, func(line string) {
		lines = append(lines, line)
	}, &stderrBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("期望 exitCode 0，得到 %d", code)
	}
	if len(lines) != 1 || lines[0] != "done" {
		t.Errorf("期望收到 ['done']，得到 %v", lines)
	}
}

// mockStreamProcessor 模拟 cmd/exec.go 使用的流式处理器，记录 Flush 调用
type mockStreamProcessor struct {
	linesSeen    []string
	flushCalled  bool
	flushExit    int
	flushedLines []string
}

func (m *mockStreamProcessor) ProcessLine(line string) {
	m.linesSeen = append(m.linesSeen, line)
}

func (m *mockStreamProcessor) Flush(exitCode int) []string {
	m.flushCalled = true
	m.flushExit = exitCode
	return m.flushedLines
}

// TestRunCommandStreaming_TimeoutInvokesFlush 超时场景下调用方必须能拿到非负 exitCode 以触发 Flush
func TestRunCommandStreaming_TimeoutInvokesFlush(t *testing.T) {
	_ = os.Setenv("GW_CMD_TIMEOUT", "300ms")
	defer func() { _ = os.Unsetenv("GW_CMD_TIMEOUT") }()

	proc := &mockStreamProcessor{}
	var stderrBuf bytes.Buffer
	exitCode, err := RunCommandStreamingFull("sh", []string{"-c", "echo started; sleep 30"}, proc.ProcessLine, &stderrBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 模拟 cmd/exec.go:149 的 Flush 调用
	_ = proc.Flush(exitCode)

	if !proc.flushCalled {
		t.Fatal("Flush 必须被调用")
	}
	if proc.flushExit != 124 {
		t.Errorf("Flush 期望收到 exitCode 124，得到 %d", proc.flushExit)
	}
	// 断言 Flush 收到了有意义的非零 exit，从而能输出错误上下文
	if proc.flushExit == 0 {
		t.Error("Flush 收到 exit 0，错误上下文会被丢弃（违反超时兜底语义）")
	}
}

// TestRunCommandStreaming_SignalExitCode_SIGTERM 验证信号终止保留真实信号值：
// 进程被 SIGTERM 杀掉应返回 128+15=143，而非 -1 或笼统的 130。
func TestRunCommandStreaming_SignalExitCode_SIGTERM(t *testing.T) {
	_ = os.Setenv("GW_CMD_TIMEOUT", "off") // 禁用超时，避免干扰信号路径
	defer func() { _ = os.Unsetenv("GW_CMD_TIMEOUT") }()

	var stderrBuf bytes.Buffer
	code, err := RunCommandStreamingFull("bash", []string{"-c", `echo started; kill -TERM $$`},
		func(line string) {}, &stderrBuf)
	if err != nil {
		t.Fatalf("不应返回 Go error，得到: %v", err)
	}
	if code != 143 {
		t.Errorf("SIGTERM 期望 exitCode 143 (128+15)，得到 %d", code)
	}
}

// TestRunCommandStreaming_SignalExitCode_SIGHUP SIGHUP 应映射为 128+1=129
func TestRunCommandStreaming_SignalExitCode_SIGHUP(t *testing.T) {
	_ = os.Setenv("GW_CMD_TIMEOUT", "off")
	defer func() { _ = os.Unsetenv("GW_CMD_TIMEOUT") }()

	var stderrBuf bytes.Buffer
	code, err := RunCommandStreamingFull("bash", []string{"-c", `echo started; kill -HUP $$`},
		func(line string) {}, &stderrBuf)
	if err != nil {
		t.Fatalf("不应返回 Go error，得到: %v", err)
	}
	if code != 129 {
		t.Errorf("SIGHUP 期望 exitCode 129 (128+1)，得到 %d", code)
	}
}

// TestRunCommandStreaming_TimeoutSIGKILLGrace 流式路径 SIGTERM 被 trap，SIGKILL 生效
func TestRunCommandStreaming_TimeoutSIGKILLGrace(t *testing.T) {
	_ = os.Setenv("GW_CMD_TIMEOUT", "300ms")
	defer func() { _ = os.Unsetenv("GW_CMD_TIMEOUT") }()

	var stderrBuf bytes.Buffer
	start := time.Now()
	code, err := RunCommandStreamingFull("bash", []string{"-c", `echo started; trap "" TERM; sleep 30`}, func(line string) {}, &stderrBuf)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 124 {
		t.Errorf("期望 exitCode 124，得到 %d", code)
	}
	if elapsed < 4500*time.Millisecond {
		t.Errorf("SIGKILL 生效过早: %v", elapsed)
	}
	if elapsed > 7500*time.Millisecond {
		t.Errorf("SIGKILL 耗时过长: %v", elapsed)
	}
	if !strings.Contains(stderrBuf.String(), "SIGKILL") {
		t.Errorf("期望 stderr 包含 SIGKILL，得到 %q", stderrBuf.String())
	}
}

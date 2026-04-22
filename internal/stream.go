package internal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// RunCommandStreaming 流式执行命令，逐行回调 stdout，stderr 输出到 os.Stderr。
func RunCommandStreaming(name string, args []string, onLine func(string)) (int, error) {
	return RunCommandStreamingFull(name, args, onLine, os.Stderr)
}

// RunCommandStreamingFull 流式执行命令，逐行回调 stdout，stderr 写入 stderrWriter。
//
// 超时策略：由 GW_CMD_TIMEOUT 环境变量控制（默认 10m，0/off 禁用）。
// 到期时先发 SIGTERM，5 秒宽限期后仍存活则 SIGKILL（进程组级别）。
// 超时场景返回 exitCode=124（GNU timeout 惯例），保证调用方 Flush 能收到非零退出，
// stderr 追加提示便于上层感知。
func RunCommandStreamingFull(name string, args []string, onLine func(string), stderrWriter io.Writer) (int, error) {
	timeout, enabled := resolveTimeout()

	ctx := context.Background()
	cancel := func() {}
	if enabled {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	// 关闭默认 CommandContext Kill，我们自己做两阶段终止
	cmd.Cancel = func() error { return nil }
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("创建 stdout pipe 失败: %w", err)
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动 %s 失败: %w", name, err)
	}

	stop, sigkillFired, timedOut := startTimeoutKiller(ctx, cmd, timeoutKillGrace)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	// 进程被杀后 stdout pipe 会 EOF，Scanner 自然停止；若是读出错（非 EOF），打 warning
	if scanErr := scanner.Err(); scanErr != nil {
		fmt.Fprintf(os.Stderr, "gw: warning: scanner 读取失败: %v\n", scanErr)
	}

	waitErr := cmd.Wait()
	stop()

	// 超时优先：即便 waitErr 表现为信号终止（-1），也映射为 124
	if timedOut.Load() {
		fmt.Fprint(stderrWriter, formatTimeoutMessage(timeout, sigkillFired.Load()))
		return timeoutExitCode, nil
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			// 若为信号终止，按 POSIX 惯例返回 128+signal；否则返回原始 ExitCode。
			// ExitCode() 在信号终止时返回 -1，此时用 WaitStatus.Signal() 拿真实信号。
			if sig := signalFromExitError(exitErr); sig != 0 {
				return 128 + int(sig), nil
			}
			return exitErr.ExitCode(), nil
		}
		// 其它未知错误，保留旧语义返回 -1 触发调用方 Flush
		return -1, nil
	}
	return 0, nil
}

// signalFromExitError 从 ExitError 中提取被信号终止时的信号值（unix）。
// 非信号终止或非 unix 平台返回 0。
func signalFromExitError(exitErr *exec.ExitError) syscall.Signal {
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return 0
	}
	if !ws.Signaled() {
		return 0
	}
	return ws.Signal()
}

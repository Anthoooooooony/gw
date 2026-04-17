package internal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
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
		return 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start %s: %w", name, err)
	}

	var timedOut atomic.Bool
	var sigkillFired atomic.Bool
	procDone := make(chan struct{})
	killerDone := make(chan struct{})
	if enabled {
		go func() {
			defer close(killerDone)
			select {
			case <-procDone:
				return
			case <-ctx.Done():
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return
				}
			}
			timedOut.Store(true)
			pid := cmd.Process.Pid
			_ = killProcessGroup(pid, syscall.SIGTERM)

			graceTimer := time.NewTimer(timeoutKillGrace)
			defer graceTimer.Stop()
			select {
			case <-procDone:
				return
			case <-graceTimer.C:
				sigkillFired.Store(true)
				_ = killProcessGroup(pid, syscall.SIGKILL)
			}
		}()
	} else {
		close(killerDone)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	// 进程被杀后 stdout pipe 会 EOF，Scanner 自然停止；若是读出错（非 EOF），打 warning
	if scanErr := scanner.Err(); scanErr != nil {
		fmt.Fprintf(os.Stderr, "[gw] scanner error: %v\n", scanErr)
	}

	waitErr := cmd.Wait()
	close(procDone)
	<-killerDone

	// 超时优先：即便 waitErr 表现为信号终止（-1），也映射为 124
	if timedOut.Load() {
		fmt.Fprint(stderrWriter, formatTimeoutMessage(timeout, sigkillFired.Load()))
		return timeoutExitCode, nil
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		// 信号终止（非超时）：返回 -1 触发调用方 Flush(-1)
		return -1, nil
	}
	return 0, nil
}

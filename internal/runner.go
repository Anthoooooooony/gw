package internal

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// CommandResult 保存命令执行的结果
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCommand 执行外部命令，捕获 stdout、stderr 和退出码。
// 仅在命令无法找到或启动时返回 error，非零退出码不视为 error。
//
// 超时策略：由 GW_CMD_TIMEOUT 环境变量控制（默认 10m，0/off 禁用）。
// 到期时先发 SIGTERM，5 秒宽限期后仍存活则 SIGKILL（进程组级别）。
// 超时场景 ExitCode 统一设为 124（GNU timeout 惯例），stderr 追加提示。
func RunCommand(name string, args []string) (*CommandResult, error) {
	timeout, enabled := resolveTimeout()

	ctx := context.Background()
	cancel := func() {}
	if enabled {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	// 关闭 CommandContext 的默认 Kill 行为（我们自己负责两阶段终止）
	cmd.Cancel = func() error { return nil }
	setProcessGroup(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var timedOut atomic.Bool
	var sigkillFired atomic.Bool
	// 进程已结束信号，用于唤醒 killer goroutine 提前退出
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

			// 非 unix 平台（如 Windows）killProcessGroup 忽略 sig 参数，
			// 第一次调用即已 Kill，无 SIGTERM 宽限期概念；标记为已 kill 后直接返回。
			if !procGroupSupportsGraceful {
				sigkillFired.Store(true)
				return
			}

			// 宽限期后若进程仍在，发 SIGKILL
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

	waitErr := cmd.Wait()
	close(procDone)
	<-killerDone

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// 命令无法找到或启动等
			return nil, waitErr
		}
	}

	result := &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}

	if timedOut.Load() {
		result.ExitCode = timeoutExitCode
		result.Stderr += formatTimeoutMessage(timeout, sigkillFired.Load())
	}

	return result, nil
}

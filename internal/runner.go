package internal

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
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

	stop, sigkillFired, timedOut := startTimeoutKiller(ctx, cmd, timeoutKillGrace)

	waitErr := cmd.Wait()
	stop()

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

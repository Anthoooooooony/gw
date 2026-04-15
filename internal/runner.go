package internal

import (
	"bytes"
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
func RunCommand(name string, args []string) (*CommandResult, error) {
	cmd := exec.Command(name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &CommandResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: exitErr.ExitCode(),
			}, nil
		}
		// 命令无法找到或启动
		return nil, err
	}

	return &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}, nil
}

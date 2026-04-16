package internal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// RunCommandStreaming 流式执行命令，逐行回调 stdout，stderr 输出到 os.Stderr。
func RunCommandStreaming(name string, args []string, onLine func(string)) (int, error) {
	return RunCommandStreamingFull(name, args, onLine, os.Stderr)
}

// RunCommandStreamingFull 流式执行命令，逐行回调 stdout，stderr 写入 stderrWriter。
func RunCommandStreamingFull(name string, args []string, onLine func(string), stderrWriter io.Writer) (int, error) {
	cmd := exec.Command(name, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start %s: %w", name, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}

	if scanErr := scanner.Err(); scanErr != nil {
		fmt.Fprintf(os.Stderr, "[gw] scanner error: %v\n", scanErr)
	}

	err = cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		// Signal kill: return -1 instead of error, so Flush is still called
		return -1, nil
	}
	return 0, nil
}

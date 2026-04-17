package internal

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// 超时兜底约定：
//   - 环境变量 GW_CMD_TIMEOUT 控制命令执行上限
//   - 默认 10 分钟；值为 "0" 或 "off" 表示禁用
//   - 解析失败写 warning 到 stderr 并 fallback 到默认值
//   - 到期流程：SIGTERM → 5s 宽限 → SIGKILL（进程组级别）
//   - 超时退出码统一为 124（GNU timeout 惯例），stderr 追加提示

const (
	// defaultCmdTimeout 命令执行默认超时
	defaultCmdTimeout = 10 * time.Minute
	// timeoutKillGrace SIGTERM 与 SIGKILL 之间的宽限期
	timeoutKillGrace = 5 * time.Second
	// timeoutExitCode GNU timeout 惯例退出码
	timeoutExitCode = 124
)

// resolveTimeout 读取 GW_CMD_TIMEOUT，返回 (duration, enabled)。
// enabled 为 false 表示禁用超时。
func resolveTimeout() (time.Duration, bool) {
	return resolveTimeoutWithStderr(os.Stderr)
}

// resolveTimeoutWithStderr 允许注入 stderr writer，便于测试 warning 输出
func resolveTimeoutWithStderr(stderr io.Writer) (time.Duration, bool) {
	raw, ok := os.LookupEnv("GW_CMD_TIMEOUT")
	if !ok || raw == "" {
		return defaultCmdTimeout, true
	}

	// 显式禁用
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "off", "none", "disable", "disabled":
		return 0, false
	}

	dur, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		fmt.Fprintf(stderr, "[gw] warning: invalid GW_CMD_TIMEOUT=%q (%v); falling back to %s\n",
			raw, err, defaultCmdTimeout)
		return defaultCmdTimeout, true
	}
	if dur <= 0 {
		return 0, false
	}
	return dur, true
}

// formatTimeoutMessage 生成写入 stderr 的超时提示。
// 在不支持 SIGTERM 宽限期的平台（Windows / plan9）上，由于无法区分信号，
// 直接输出 "(killed)" 而非误导性的 "SIGTERM[, SIGKILL]"。
func formatTimeoutMessage(dur time.Duration, sigkillFired bool) string {
	if !procGroupSupportsGraceful {
		// 非 unix 平台降级：一次性 Kill，不存在 SIGTERM 阶段
		return fmt.Sprintf("\ngw: command timed out after %s (killed)\n", dur)
	}
	// unix 两阶段语义
	signals := "SIGTERM"
	if sigkillFired {
		signals = "SIGTERM, SIGKILL"
	}
	return fmt.Sprintf("\ngw: command timed out after %s (%s)\n", dur, signals)
}

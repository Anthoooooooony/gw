package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/Anthoooooooony/gw/internal/apiproxy"
	"github.com/Anthoooooooony/gw/internal/apiproxy/dcp"
	"github.com/Anthoooooooony/gw/track"
	"github.com/spf13/cobra"
)

// claudeCmd 透明包装 claude CLI：启动本地 API 代理、注入 ANTHROPIC_BASE_URL、exec claude。
//
// 降级策略：
//   - 代理启动失败 → warn，直接 exec claude 不 hook
//   - claude 进程 stderr/stdout/stdin 完全透传，保持 TTY 体验
//
// 不支持 Bedrock/Vertex：这两条路径下 claude 忽略 ANTHROPIC_BASE_URL，代理无法生效。
// 使用 AWS Bedrock / GCP Vertex 的用户请直接运行 claude，不要经过 gw claude。
var claudeCmd = &cobra.Command{
	Use:                "claude [args...]",
	Short:              "透明包装 claude CLI，启动本地 API 代理以压缩上下文",
	Long:               "启动本地 HTTP 代理并注入 ANTHROPIC_BASE_URL，让 Claude Code 的 API 流量经过 gw 处理。代理失败时自动降级为直接调用 claude。",
	DisableFlagParsing: true,
	Run:                runClaude,
}

func init() {
	rootCmd.AddCommand(claudeCmd)
}

func runClaude(cmd *cobra.Command, args []string) {
	logger := newStderrLogger(os.Stderr, Verbose)

	// 1. 启动本地代理（失败则降级为直接 exec）
	srv, err := apiproxy.Start(logger)
	if err != nil {
		logger.Warnf("apiproxy 启动失败，降级为直接 exec claude: %v", err)
		execClaude(args, nil)
		return
	}

	// 2. 注入环境变量；仅对子进程生效，父 shell 不受影响
	env := append(os.Environ(), "ANTHROPIC_BASE_URL="+srv.URL())

	// 3. 运行 claude，等待其退出
	code := runChild(args, env, logger)

	// 4. 关闭代理（优雅）；超时可通过 GW_APIPROXY_SHUTDOWN_TIMEOUT 覆盖，默认 5s。
	// deadline 触发时是"还有连接没清理完"，实际上 claude 已退，这类连接多半是
	// 上游端还在收尾，属于正常现象，warn 一声即可。
	timeout := apiproxy.ShutdownTimeout()
	if err := srv.Shutdown(timeout); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Warnf("apiproxy shutdown deadline %s 到达，强制终止残余连接", timeout)
		} else {
			logger.Warnf("apiproxy shutdown: %v", err)
		}
	}

	// 5. 打印 dcp 统计摘要。非 verbose 也打印，总结性信息视作必看。
	//   仅在本次 session 至少处理了 1 个请求时打印，避免干扰未真实使用代理的场景。
	//   调用时机：http.Server.Shutdown 返回即代表所有 active handler 已完成，
	//   Stats 计数不会再被写入，故对其做多次 Load 读取是一致性快照。
	writeDCPSummary(os.Stderr, srv.Stats())

	os.Exit(code)
}

// execClaude 替换当前进程为 claude（降级路径，不需要代理生命周期管理）。
func execClaude(args []string, _ []string) {
	path, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw claude: 未找到 claude 可执行文件：%v\n", err)
		os.Exit(127)
	}
	// syscall.Exec 直接替换进程映像，不留 gw 残余
	if err := syscall.Exec(path, append([]string{"claude"}, args...), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "gw claude: exec 失败: %v\n", err)
		os.Exit(127)
	}
}

// runChild 在正常路径下以 subprocess 形式启动 claude，完整透传 stdio 和信号，
// 并返回 claude 退出码。我们需要在 claude 退出后关闭代理，所以不能用 syscall.Exec。
func runChild(args, env []string, logger apiproxy.Logger) int {
	path, err := exec.LookPath("claude")
	if err != nil {
		logger.Warnf("未找到 claude 可执行文件：%v", err)
		return 127
	}

	c := exec.Command(path, args...)
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		logger.Warnf("claude 启动失败: %v", err)
		return 127
	}

	// 信号透传：把收到的 SIGINT/SIGTERM 转给 claude，让它自己决定退出方式。
	// 这样 Ctrl+C 等交互在 TTY 下与直接跑 claude 表现一致。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigCh:
				if c.Process != nil {
					// Wait() 返回到 close(done) 之间存在微秒级窗口，
					// 若此时收到信号会投递到已退出的进程，返回 os.ErrProcessDone。
					// 这是良性的——signal.Stop 随后就会停止投递，错误直接吞掉。
					_ = c.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()

	err = c.Wait()
	// 先 signal.Stop 停止投递到 sigCh，再 close(done) 退出转发 goroutine。
	// 反过来会在两步之间留一个窗口：sigCh 仍接收信号，但 goroutine 已退出。
	signal.Stop(sigCh)
	close(done)

	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	logger.Warnf("claude wait: %v", err)
	return 1
}

// writeDCPSummary 向 w 写入 dcp 统计摘要（请求数、tool_use 扫描数、替换次数、
// 节省字节及估算 token 数）。只在处理过至少 1 个请求时才写入，避免对未触发代理
// 的场景（如 /help 等瞬间退出的子命令）造成干扰。
//
// 抽出 io.Writer 参数便于单测捕获输出；生产调用固定传 os.Stderr。
func writeDCPSummary(w io.Writer, stats *dcp.Stats) {
	reqs := stats.RequestsProcessed.Load()
	if reqs == 0 {
		return
	}
	saved := stats.BytesSaved()
	tokens := track.EstimateTokensByLen(int(saved))
	fmt.Fprintf(w,
		"gw: dcp: %d 请求 / 扫 %d tool_use / 替换 %d tool_result / 节省 %d 字节 (~%d tokens)\n",
		reqs,
		stats.ToolUseScanned.Load(),
		stats.ResultsReplaced.Load(),
		saved,
		tokens,
	)
}

// stderrLogger 是 apiproxy.Logger 的最小实现：infof 仅在 verbose 时打印。
// 输出目标参数化为 io.Writer 便于单测，生产调用固定用 os.Stderr。
type stderrLogger struct {
	w       io.Writer
	verbose bool
}

func newStderrLogger(w io.Writer, verbose bool) *stderrLogger {
	return &stderrLogger{w: w, verbose: verbose}
}

func (l *stderrLogger) Infof(format string, args ...any) {
	if !l.verbose {
		return
	}
	fmt.Fprintf(l.w, "gw: info: "+format+"\n", args...)
}

func (l *stderrLogger) Warnf(format string, args ...any) {
	fmt.Fprintf(l.w, "gw: warning: "+format+"\n", args...)
}

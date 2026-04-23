package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/Anthoooooooony/gw/track"
	"github.com/spf13/cobra"
)

var (
	summaryText      bool
	summaryPort      int
	summaryNoBrowser bool
)

var summaryCmd = &cobra.Command{
	Use:     "summary",
	Aliases: []string{"gain"},
	Short:   "显示 token 节省统计（默认启动 web dashboard）",
	Long: `显示今日、本周、全部的 token 节省统计和排名靠前的命令。

默认启动本地 web dashboard 并用系统默认浏览器打开，SSE 每 5s 刷新。
非交互环境（stdout 非 TTY）或 --text 会退回纯文本摘要。`,
	Run: runSummary,
}

func init() {
	summaryCmd.Flags().BoolVar(&summaryText, "text", false, "强制输出纯文本摘要，不启动 web dashboard")
	summaryCmd.Flags().IntVar(&summaryPort, "port", 0, "web dashboard 监听端口（0 = 随机分配）")
	summaryCmd.Flags().BoolVar(&summaryNoBrowser, "no-browser", false, "启动 server 但不自动打开浏览器，仅打印 URL")
	rootCmd.AddCommand(summaryCmd)
}

func runSummary(cmd *cobra.Command, args []string) {
	dbPath := track.DefaultDBPath()

	// 预处理：Cleanup + TrimBySize。失败不中断（降级即可）。
	if db, err := track.NewDB(dbPath); err == nil {
		_ = db.Cleanup(90)
		if trimmed, terr := db.TrimBySize(); terr != nil && Verbose {
			fmt.Fprintf(os.Stderr, "gw: warning: DB 大小裁剪失败: %v\n", terr)
		} else if trimmed > 0 && Verbose {
			fmt.Fprintf(os.Stderr, "gw: info: DB 大小超限，已裁剪 %d 条旧记录\n", trimmed)
		}
		_ = db.Close()
	}

	// 显式要 web 的信号：用户指定端口 / --no-browser / NO_BROWSER。
	// 缺失这些时才根据 TTY 判断，避免在脚本/CI 里阻塞。
	wantWeb := cmd.Flags().Changed("port") || summaryNoBrowser || os.Getenv("NO_BROWSER") != ""
	if summaryText || (!wantWeb && !stdoutIsTTY()) {
		if err := runSummaryText(os.Stdout, dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "gw summary: %v\n", err)
			os.Exit(1)
		}
		return
	}

	openBrowserFlag := !summaryNoBrowser && os.Getenv("NO_BROWSER") == ""
	// 无 display 环境（Linux SSH 等）仍启 server 打 URL，用户可 port forward
	if openBrowserFlag && !hasDisplay() {
		fmt.Fprintln(os.Stderr, "gw summary: 未检测到 display，仅打印 URL（可用 port forward 访问）")
		openBrowserFlag = false
	}

	if err := startSummaryWeb(dbPath, summaryPort, openBrowserFlag); err != nil {
		fmt.Fprintf(os.Stderr, "gw summary: web dashboard 启动失败，降级纯文本: %v\n", err)
		if err := runSummaryText(os.Stdout, dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "gw summary: %v\n", err)
			os.Exit(1)
		}
	}
}

// stdoutIsTTY 报告 stdout 是否为字符设备（终端）。
// 用于区分 `gw summary | less` / CI 管道 与 交互终端。
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// hasDisplay 报告当前平台是否有可能启动 GUI 浏览器。
// Linux/*BSD 依赖 $DISPLAY 或 $WAYLAND_DISPLAY；macOS/Windows 视为总有。
func hasDisplay() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	case "linux", "freebsd", "openbsd", "netbsd":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	default:
		return false
	}
}

// runSummaryText 把历史摘要以纯文本形式打印到 w。
// 抽出来是为了让 web 降级路径与 --text 共用同一实现。
func runSummaryText(w io.Writer, dbPath string) error {
	db, err := track.NewDB(dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer func() { _ = db.Close() }()

	today, err := db.TodayStats()
	if err != nil {
		return fmt.Errorf("查询统计失败: %w", err)
	}
	week, _ := db.WeekStats()
	all, _ := db.AllStats()

	fmt.Fprintln(w, "=== gw token 节省统计 ===")
	fmt.Fprintln(w)
	printStats(w, "今日", today)
	printStats(w, "本周", week)
	printStats(w, "全部", all)

	top, err := db.TopCommands(5)
	if err == nil && len(top) > 0 {
		fmt.Fprintln(w, "--- Top 5 命令 ---")
		for i, tc := range top {
			fmt.Fprintf(w, "  %d. %-30s saved=%d tokens  (%.1f%%)\n", i+1, tc.Command, tc.TotalSaved, tc.AvgPct)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func printStats(w io.Writer, label string, s track.Stats) {
	savePct := 0.0
	if s.TotalInput > 0 {
		savePct = float64(s.TotalSaved) / float64(s.TotalInput) * 100
	}
	fmt.Fprintf(w, "%s — 命令: %d  输入tokens: %d  节省tokens: %d  (%.1f%%)\n",
		label, s.CommandCount, s.TotalInput, s.TotalSaved, savePct)
}

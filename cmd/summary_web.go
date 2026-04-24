package cmd

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/Anthoooooooony/gw/track"
)

//go:embed web
var webFS embed.FS

const (
	sseTickInterval = 5 * time.Second
	shutdownGrace   = 3 * time.Second
)

// serverStartTime 是 summary web server 首次进入 startSummaryWeb 的时刻。
// 用 Lazy init（非 package-level var = time.Now()）避免把 gw 二进制任何入口的
// 启动时间都算进来——只有实际跑 dashboard 时才记。
var (
	serverStartTime     time.Time
	serverStartTimeOnce sync.Once
)

func markServerStart() {
	serverStartTimeOnce.Do(func() { serverStartTime = time.Now() })
}

// ── Payload schema ─────────────────────────────────────────────────────────
// 前端 app.js 依赖这个 JSON 结构，字段名是契约的一部分。

type statsJSON struct {
	CommandCount int `json:"command_count"`
	InputTokens  int `json:"input_tokens"`
	SavedTokens  int `json:"saved_tokens"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

type topCommandJSON struct {
	Command    string  `json:"command"`
	TotalSaved int     `json:"total_saved"`
	AvgPct     float64 `json:"avg_pct"`
}

type dbInfoJSON struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Rows      int64  `json:"rows"`
}

type summaryJSON struct {
	Today               statsJSON        `json:"today"`
	Week                statsJSON        `json:"week"`
	All                 statsJSON        `json:"all"`
	TopCommands         []topCommandJSON `json:"top_commands"`
	DB                  dbInfoJSON       `json:"db"`
	ServerTime          string           `json:"server_time"`
	ServerUptimeSeconds int64            `json:"server_uptime_seconds"`
}

// ── Server lifecycle ───────────────────────────────────────────────────────

// startSummaryWeb 启动本地 dashboard server，阻塞直到用户 Ctrl+C 或 server 出错。
func startSummaryWeb(dbPath string, port int, openBrowserFlag bool) error {
	markServerStart()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	mux, err := buildSummaryMux(dbPath)
	if err != nil {
		_ = listener.Close()
		return err
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	fmt.Fprintf(os.Stderr, "gw summary: serving dashboard at %s  (Ctrl+C to stop)\n", url)
	if openBrowserFlag {
		if oerr := openBrowser(url); oerr != nil {
			fmt.Fprintf(os.Stderr, "gw summary: 打开浏览器失败 (%v)，请手动访问 %s\n", oerr, url)
		}
	}

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\ngw summary: shutting down")
	case err := <-serveErr:
		if err != nil {
			return err
		}
	}

	shCtx, shCancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer shCancel()
	_ = srv.Shutdown(shCtx)
	return nil
}

// buildSummaryMux 构造路由：静态资源 + /api/data + /api/events（SSE）。
// 抽离以便测试覆盖。
func buildSummaryMux(dbPath string) (*http.ServeMux, error) {
	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, fmt.Errorf("加载 embed 静态资源失败: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(subFS)))
	mux.HandleFunc("/api/data", summaryDataHandler(dbPath))
	mux.HandleFunc("/api/events", summaryEventsHandler(dbPath))
	return mux, nil
}

// ── Handlers ───────────────────────────────────────────────────────────────

func summaryDataHandler(dbPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := buildSummaryPayload(dbPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func summaryEventsHandler(dbPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		send := func() bool {
			payload, err := buildSummaryPayload(dbPath)
			if err != nil {
				return false
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return false
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return false
			}
			flusher.Flush()
			return true
		}
		if !send() {
			return
		}

		ticker := time.NewTicker(sseTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if !send() {
					return
				}
			}
		}
	}
}

// ── Payload builder ────────────────────────────────────────────────────────

// payloadMu 串行化 DB 打开。多个并发 SSE tick 直接复用 track.NewDB 的 WAL，但
// 降低同时开 DB 的锁竞争能让 handler 响应更稳。
var payloadMu sync.Mutex

func buildSummaryPayload(dbPath string) (summaryJSON, error) {
	payloadMu.Lock()
	defer payloadMu.Unlock()

	db, err := track.NewDB(dbPath)
	if err != nil {
		return summaryJSON{}, fmt.Errorf("打开数据库失败: %w", err)
	}
	defer func() { _ = db.Close() }()

	today, err := db.TodayStats()
	if err != nil {
		return summaryJSON{}, fmt.Errorf("查询 today stats 失败: %w", err)
	}
	week, err := db.WeekStats()
	if err != nil {
		return summaryJSON{}, fmt.Errorf("查询 week stats 失败: %w", err)
	}
	all, err := db.AllStats()
	if err != nil {
		return summaryJSON{}, fmt.Errorf("查询 all stats 失败: %w", err)
	}

	top, err := db.TopCommands(10)
	if err != nil {
		return summaryJSON{}, fmt.Errorf("查询 top commands 失败: %w", err)
	}

	size, _ := db.SizeOnDisk() // 降级到 0 即可
	rows, _ := db.RowCount()

	return summaryJSON{
		Today:       toStatsJSON(today, allOutputTokens(today)),
		Week:        toStatsJSON(week, allOutputTokens(week)),
		All:         toStatsJSON(all, allOutputTokens(all)),
		TopCommands: toTopJSON(top),
		DB: dbInfoJSON{
			Path:      dbPath,
			SizeBytes: size,
			Rows:      rows,
		},
		ServerTime:          time.Now().UTC().Format(time.RFC3339),
		ServerUptimeSeconds: serverUptimeSeconds(),
	}, nil
}

// serverUptimeSeconds 返回 server 自 markServerStart 以来运行的秒数。
// 若 server 尚未启动（测试直接调 buildSummaryPayload 的场景）返回 0。
func serverUptimeSeconds() int64 {
	if serverStartTime.IsZero() {
		return 0
	}
	return int64(time.Since(serverStartTime).Seconds())
}

func toStatsJSON(s track.Stats, output int) statsJSON {
	return statsJSON{
		CommandCount: s.CommandCount,
		InputTokens:  s.TotalInput,
		SavedTokens:  s.TotalSaved,
		OutputTokens: output,
	}
}

// allOutputTokens 反推 output = input - saved。saved 可能大于 input（极少数 pytest
// 过滤场景），此时输出 0 避免负值让图表报错。
func allOutputTokens(s track.Stats) int {
	out := s.TotalInput - s.TotalSaved
	if out < 0 {
		return 0
	}
	return out
}

func toTopJSON(list []track.TopCommand) []topCommandJSON {
	out := make([]topCommandJSON, 0, len(list))
	for _, tc := range list {
		out = append(out, topCommandJSON{
			Command:    tc.Command,
			TotalSaved: tc.TotalSaved,
			AvgPct:     tc.AvgPct,
		})
	}
	return out
}

// ── Browser launcher ───────────────────────────────────────────────────────

// openBrowser 调用平台默认浏览器打开 url。失败返回 error，调用方决定是否降级。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "linux", "freebsd", "openbsd", "netbsd":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// Start 而非 Run：xdg-open / open 会立刻返回，但用 Start 避免阻塞
	// 在某些罕见子进程卡住的情况
	return cmd.Start()
}

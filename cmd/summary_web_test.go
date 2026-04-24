package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Anthoooooooony/gw/track"
)

// seedDB 向 dbPath 写几条 tracking 记录，便于 handler 在非空库上测试。
func seedDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	recs := []track.Record{
		{Timestamp: now, Command: "git status", ExitCode: 0, InputTokens: 2000, OutputTokens: 400, SavedTokens: 1600, ElapsedMs: 50, FilterUsed: "git"},
		{Timestamp: now.Add(-2 * time.Hour), Command: "git status", ExitCode: 0, InputTokens: 1000, OutputTokens: 300, SavedTokens: 700, ElapsedMs: 40, FilterUsed: "git"},
		{Timestamp: now.Add(-30 * time.Hour), Command: "mvn test", ExitCode: 0, InputTokens: 8000, OutputTokens: 800, SavedTokens: 7200, ElapsedMs: 300, FilterUsed: "mvn"},
	}
	for _, r := range recs {
		if err := db.InsertRecord(r); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
}

func TestBuildSummaryPayload_Empty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	// NewDB 会建表，后续调用 buildSummaryPayload 拿到零值
	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_ = db.Close()

	p, err := buildSummaryPayload(dbPath)
	if err != nil {
		t.Fatalf("buildSummaryPayload: %v", err)
	}
	if p.All.CommandCount != 0 {
		t.Errorf("expected 0 commands, got %d", p.All.CommandCount)
	}
	if p.DB.Path != dbPath {
		t.Errorf("db.path expected %q, got %q", dbPath, p.DB.Path)
	}
	if len(p.TopCommands) != 0 {
		t.Errorf("expected empty top_commands, got %d", len(p.TopCommands))
	}
}

func TestBuildSummaryPayload_WithData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	seedDB(t, dbPath)

	p, err := buildSummaryPayload(dbPath)
	if err != nil {
		t.Fatalf("buildSummaryPayload: %v", err)
	}
	if p.All.CommandCount != 3 {
		t.Errorf("all.command_count expected 3, got %d", p.All.CommandCount)
	}
	if p.All.SavedTokens != 1600+700+7200 {
		t.Errorf("all.saved_tokens expected %d, got %d", 1600+700+7200, p.All.SavedTokens)
	}
	if p.All.OutputTokens <= 0 {
		t.Errorf("all.output_tokens should be positive, got %d", p.All.OutputTokens)
	}
	if p.Today.CommandCount < 2 {
		t.Errorf("today should include 2+ recent commands, got %d", p.Today.CommandCount)
	}
	// Today / Week 的 OutputTokens 必须用 allOutputTokens 反推（不得硬编码 0）
	if p.Today.OutputTokens != p.Today.InputTokens-p.Today.SavedTokens {
		t.Errorf("today.output_tokens expected %d, got %d",
			p.Today.InputTokens-p.Today.SavedTokens, p.Today.OutputTokens)
	}
	if p.Week.OutputTokens != p.Week.InputTokens-p.Week.SavedTokens {
		t.Errorf("week.output_tokens expected %d, got %d",
			p.Week.InputTokens-p.Week.SavedTokens, p.Week.OutputTokens)
	}
	if len(p.TopCommands) == 0 || p.TopCommands[0].Command != "mvn test" {
		t.Errorf("top[0] should be mvn test (largest saved), got %+v", p.TopCommands)
	}
	if p.DB.Rows != 3 {
		t.Errorf("db.rows expected 3, got %d", p.DB.Rows)
	}
	if p.DB.SizeBytes <= 0 {
		t.Errorf("db.size_bytes should be positive, got %d", p.DB.SizeBytes)
	}
}

func TestSummaryMux_DataEndpoint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	seedDB(t, dbPath)

	mux, err := buildSummaryMux(dbPath)
	if err != nil {
		t.Fatalf("buildSummaryMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/data")
	if err != nil {
		t.Fatalf("GET /api/data: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type expected application/json, got %q", ct)
	}
	var got summaryJSON
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if got.All.CommandCount != 3 {
		t.Errorf("all.command_count expected 3, got %d", got.All.CommandCount)
	}
}

func TestSummaryMux_StaticAssets(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_ = db.Close()

	mux, err := buildSummaryMux(dbPath)
	if err != nil {
		t.Fatalf("buildSummaryMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// index.html 根路径
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/ status expected 200, got %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("gw // telemetry")) {
		t.Errorf("/ body missing brand string")
	}
	if !bytes.Contains(body, []byte("<canvas id=\"ratio-chart\"")) {
		t.Errorf("/ body missing ratio-chart canvas")
	}

	// chart.umd.js 资源
	resp2, err := http.Get(srv.URL + "/chart.umd.js")
	if err != nil {
		t.Fatalf("GET /chart.umd.js: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != 200 {
		t.Fatalf("/chart.umd.js status expected 200, got %d", resp2.StatusCode)
	}
	if resp2.ContentLength < 1024 {
		t.Errorf("/chart.umd.js unexpectedly tiny: %d bytes", resp2.ContentLength)
	}
}

func TestSummaryMux_EventsSSEHeaders(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_ = db.Close()

	mux, err := buildSummaryMux(dbPath)
	if err != nil {
		t.Fatalf("buildSummaryMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// 用 context 控制连接生命周期，只读首个事件就退出
	req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type expected text/event-stream, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control expected no-cache, got %q", cc)
	}

	// 读一小段确认 data: 前缀
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	if n == 0 || !bytes.HasPrefix(buf[:n], []byte("data: ")) {
		t.Errorf("SSE body should start with 'data: ', got %q", buf[:n])
	}
}

func TestRunSummaryText_EmptyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_ = db.Close()

	var buf bytes.Buffer
	if err := runSummaryText(&buf, dbPath); err != nil {
		t.Fatalf("runSummaryText: %v", err)
	}
	out := buf.String()
	for _, needle := range []string{"=== gw token 节省统计 ===", "今日", "本周", "全部"} {
		if !strings.Contains(out, needle) {
			t.Errorf("text output missing %q\n---\n%s", needle, out)
		}
	}
}

func TestRunSummaryText_WithData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	seedDB(t, dbPath)

	var buf bytes.Buffer
	if err := runSummaryText(&buf, dbPath); err != nil {
		t.Fatalf("runSummaryText: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Top 5 命令") {
		t.Errorf("text output missing Top 5 header:\n%s", out)
	}
	if !strings.Contains(out, "mvn test") {
		t.Errorf("text output missing expected command:\n%s", out)
	}
}

func TestHasDisplay_Platforms(t *testing.T) {
	// macOS/Windows 永远 true
	switch runtime.GOOS {
	case "darwin", "windows":
		if !hasDisplay() {
			t.Errorf("hasDisplay on %s should be true", runtime.GOOS)
		}
	case "linux", "freebsd", "openbsd", "netbsd":
		// 通过 env 覆盖测试 Linux 分支
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		if hasDisplay() {
			t.Errorf("hasDisplay should be false on %s without DISPLAY/WAYLAND_DISPLAY", runtime.GOOS)
		}
		t.Setenv("DISPLAY", ":0")
		if !hasDisplay() {
			t.Errorf("hasDisplay should be true on %s with DISPLAY set", runtime.GOOS)
		}
	}
}

func TestToStatsJSON_RatioFields(t *testing.T) {
	s := track.Stats{TotalSaved: 100, TotalInput: 250, CommandCount: 5}
	out := toStatsJSON(s, 150)
	if out.CommandCount != 5 || out.InputTokens != 250 || out.SavedTokens != 100 || out.OutputTokens != 150 {
		t.Errorf("toStatsJSON mismatch: %+v", out)
	}
}

func TestAllOutputTokens_ClampedNonNegative(t *testing.T) {
	// 正常情况
	if got := allOutputTokens(track.Stats{TotalInput: 1000, TotalSaved: 700}); got != 300 {
		t.Errorf("expected 300, got %d", got)
	}
	// saved > input 时 clamp 到 0
	if got := allOutputTokens(track.Stats{TotalInput: 500, TotalSaved: 800}); got != 0 {
		t.Errorf("expected 0 (clamped), got %d", got)
	}
}

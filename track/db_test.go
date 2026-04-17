package track

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecordAndQuery(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	r := Record{
		Timestamp:    time.Now().UTC(),
		Command:      "git status",
		ExitCode:     0,
		InputTokens:  1000,
		OutputTokens: 200,
		SavedTokens:  800,
		ElapsedMs:    150,
		FilterUsed:   "git-status",
	}

	if err := db.InsertRecord(r); err != nil {
		t.Fatalf("插入记录失败: %v", err)
	}

	stats, err := db.TodayStats()
	if err != nil {
		t.Fatalf("查询今日统计失败: %v", err)
	}

	if stats.CommandCount != 1 {
		t.Errorf("期望 CommandCount=1，得到 %d", stats.CommandCount)
	}
	if stats.TotalSaved != 800 {
		t.Errorf("期望 TotalSaved=800，得到 %d", stats.TotalSaved)
	}
	if stats.TotalInput != 1000 {
		t.Errorf("期望 TotalInput=1000，得到 %d", stats.TotalInput)
	}
}

func TestCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 插入一条 100 天前的记录
	oldTime := time.Now().UTC().AddDate(0, 0, -100)
	r := Record{
		Timestamp:    oldTime,
		Command:      "old-command",
		ExitCode:     0,
		InputTokens:  500,
		OutputTokens: 100,
		SavedTokens:  400,
		ElapsedMs:    50,
		FilterUsed:   "test",
	}

	if err := db.InsertRecord(r); err != nil {
		t.Fatalf("插入记录失败: %v", err)
	}

	// 保留 90 天，100 天前的应被删除
	if err := db.Cleanup(90); err != nil {
		t.Fatalf("清理失败: %v", err)
	}

	stats, err := db.AllStats()
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}

	if stats.CommandCount != 0 {
		t.Errorf("期望清理后 CommandCount=0，得到 %d", stats.CommandCount)
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("空字符串应为 0，得到 %d", got)
	}
	// 12 个字符 -> ceil(12/4) = 3
	if got := EstimateTokens("hello world!"); got != 3 {
		t.Errorf("期望 3，得到 %d", got)
	}
	// 1 个字符 -> ceil(1/4) = 1
	if got := EstimateTokens("a"); got != 1 {
		t.Errorf("期望 1，得到 %d", got)
	}
}

func TestDefaultDBPath(t *testing.T) {
	// 清除可能污染的 env
	_ = os.Unsetenv("GW_DB_PATH")
	path := DefaultDBPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".gw", "tracking.db")
	if path != expected {
		t.Errorf("期望 %s，得到 %s", expected, path)
	}
}

// TestDefaultDBPath_EnvOverride GW_DB_PATH 优先级最高
func TestDefaultDBPath_EnvOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom.db")
	_ = os.Setenv("GW_DB_PATH", custom)
	defer func() { _ = os.Unsetenv("GW_DB_PATH") }()

	got := DefaultDBPath()
	if got != custom {
		t.Errorf("GW_DB_PATH 未生效: 期望 %s, 得到 %s", custom, got)
	}
}

// TestDefaultDBPath_HomeUnwritableFallback HOME 不可写时降级到 os.TempDir
// 并通过 stderr 打一次 warning。
func TestDefaultDBPath_HomeUnwritableFallback(t *testing.T) {
	_ = os.Unsetenv("GW_DB_PATH")

	// 通过把 HOME 指向一个只读的不存在路径（但父目录不可写）来触发降级。
	// 在 macOS/Linux 上把 HOME 指向 /nonexistent/readonly-home —— MkdirAll 会失败。
	// 测试走的是 pathResolver，所以我们通过 resolveDBPathWithEnv 注入 homeDir 不可写信号。

	// 重置单次降级 warning 状态，让本测试独立可观察 warn
	dbWarnOnce = sync.Once{}

	var warnBuf strings.Builder
	got := resolveDBPathWithEnv("", "/nonexistent/readonly-home", &warnBuf)

	if !strings.HasPrefix(got, os.TempDir()) {
		t.Errorf("HOME 不可写应降级到 TempDir，得到 %s", got)
	}
	if !strings.Contains(warnBuf.String(), "HOME") {
		t.Errorf("应发出 HOME 相关 warning，得到 %q", warnBuf.String())
	}
}

// TestDefaultDBPath_WarnOnce 同一进程内降级 warning 只打一次
func TestDefaultDBPath_WarnOnce(t *testing.T) {
	_ = os.Unsetenv("GW_DB_PATH")
	dbWarnOnce = sync.Once{}

	var w1, w2 strings.Builder
	_ = resolveDBPathWithEnv("", "/nonexistent/readonly-home", &w1)
	_ = resolveDBPathWithEnv("", "/nonexistent/readonly-home", &w2)

	if w1.Len() == 0 {
		t.Error("首次调用应 warn")
	}
	if w2.Len() != 0 {
		t.Errorf("第二次调用不应重复 warn，得到 %q", w2.String())
	}
}

// TestMigration_AddRawOutputColumn 模拟老版本 DB（没有 raw_output 列），
// 确认 NewDB 能通过 ALTER TABLE 自动加上新列，且旧记录依然可读写。
func TestMigration_AddRawOutputColumn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "legacy.db")

	// 手动用旧 schema 建表（不含 raw_output 列）
	legacy, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	legacySchema := `
CREATE TABLE IF NOT EXISTS tracking (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp TEXT NOT NULL,
	command TEXT NOT NULL,
	exit_code INTEGER NOT NULL,
	input_tokens INTEGER NOT NULL,
	output_tokens INTEGER NOT NULL,
	saved_tokens INTEGER NOT NULL,
	elapsed_ms INTEGER NOT NULL,
	filter_used TEXT NOT NULL DEFAULT ''
);`
	if _, err := legacy.Exec(legacySchema); err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	// 塞一条老记录
	_, err = legacy.Exec(
		`INSERT INTO tracking (timestamp, command, exit_code, input_tokens, output_tokens, saved_tokens, elapsed_ms, filter_used)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339),
		"legacy cmd", 0, 100, 50, 50, 10, "legacy-filter")
	if err != nil {
		t.Fatalf("插旧记录失败: %v", err)
	}
	_ = legacy.Close()

	// NewDB 应触发迁移
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB 迁移失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 旧记录依然能查
	stats, err := db.AllStats()
	if err != nil {
		t.Fatalf("迁移后查询失败: %v", err)
	}
	if stats.CommandCount != 1 {
		t.Fatalf("期望 1 条旧记录，得到 %d", stats.CommandCount)
	}

	// 新 schema 能写入带 RawOutput 的记录
	newRec := Record{
		Timestamp:    time.Now().UTC(),
		Command:      "new cmd",
		ExitCode:     0,
		InputTokens:  200,
		OutputTokens: 80,
		SavedTokens:  120,
		ElapsedMs:    20,
		FilterUsed:   "new-filter",
		RawOutput:    "raw original output",
	}
	if err := db.InsertRecord(newRec); err != nil {
		t.Fatalf("插入新记录失败: %v", err)
	}

	records, err := db.RecentRecords(10)
	if err != nil {
		t.Fatalf("读取最近记录失败: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("期望 2 条记录，得到 %d", len(records))
	}

	// 最近的应是 "new cmd"，RawOutput 要保留
	var foundNew bool
	for _, r := range records {
		if r.Command == "new cmd" {
			foundNew = true
			if r.RawOutput != "raw original output" {
				t.Errorf("期望 RawOutput='raw original output'，得到 %q", r.RawOutput)
			}
		}
		if r.Command == "legacy cmd" && r.RawOutput != "" {
			t.Errorf("迁移后的旧记录 RawOutput 应为空，得到 %q", r.RawOutput)
		}
	}
	if !foundNew {
		t.Error("未找到新插入的记录")
	}

	// 再次 NewDB 应幂等（不崩）
	db2, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("二次 NewDB 失败: %v", err)
	}
	_ = db2.Close()
}

// TestRawOutputRoundTrip 验证 InsertRecord + GetRecord 能正确保留 RawOutput。
func TestRawOutputRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	raw := "line1\nline2\nerror: something\n"
	r := Record{
		Timestamp:    time.Now().UTC(),
		Command:      "mvn test",
		ExitCode:     1,
		InputTokens:  500,
		OutputTokens: 80,
		SavedTokens:  420,
		ElapsedMs:    1000,
		FilterUsed:   "java/maven",
		RawOutput:    raw,
	}
	if err := db.InsertRecord(r); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	recs, err := db.RecentRecords(1)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("期望 1 条，得到 %d", len(recs))
	}
	if recs[0].RawOutput != raw {
		t.Errorf("RawOutput 不一致: 期望 %q，得到 %q", raw, recs[0].RawOutput)
	}
	if recs[0].ID <= 0 {
		t.Errorf("ID 应大于 0，得到 %d", recs[0].ID)
	}

	got, err := db.GetRecord(recs[0].ID)
	if err != nil {
		t.Fatalf("GetRecord 失败: %v", err)
	}
	if got.Command != "mvn test" {
		t.Errorf("Command 不一致: %q", got.Command)
	}
	if got.RawOutput != raw {
		t.Errorf("GetRecord RawOutput 不一致: %q", got.RawOutput)
	}
}

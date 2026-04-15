package track

import (
	"os"
	"path/filepath"
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
	defer db.Close()

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
	defer db.Close()

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
	path := DefaultDBPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".gw", "tracking.db")
	if path != expected {
		t.Errorf("期望 %s，得到 %s", expected, path)
	}
}

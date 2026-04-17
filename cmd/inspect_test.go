package cmd

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gw-cli/gw/track"
)

// TestInspect_Empty 空数据库应提示"无记录"。
func TestInspect_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "t.db")

	var buf bytes.Buffer
	if err := runInspectWithDB(&buf, dbPath, nil, false); err != nil {
		t.Fatalf("runInspect: %v", err)
	}
	if !strings.Contains(buf.String(), "(无记录)") {
		t.Errorf("期望 '(无记录)'，得到 %q", buf.String())
	}
}

// TestInspect_List 无 id 时打印最近记录列表。
func TestInspect_List(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "t.db")

	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.InsertRecord(track.Record{
		Timestamp: time.Now().UTC(), Command: "git status",
		ExitCode: 0, InputTokens: 100, OutputTokens: 30,
		SavedTokens: 70, ElapsedMs: 5, FilterUsed: "git/status",
	})
	_ = db.InsertRecord(track.Record{
		Timestamp: time.Now().UTC(), Command: "mvn test",
		ExitCode: 0, InputTokens: 1000, OutputTokens: 50,
		SavedTokens: 950, ElapsedMs: 3000, FilterUsed: "java/maven",
	})
	db.Close()

	var buf bytes.Buffer
	if err := runInspectWithDB(&buf, dbPath, nil, false); err != nil {
		t.Fatalf("runInspect: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "git status") {
		t.Errorf("列表应包含 'git status'，得到:\n%s", out)
	}
	if !strings.Contains(out, "mvn test") {
		t.Errorf("列表应包含 'mvn test'，得到:\n%s", out)
	}
	if !strings.Contains(out, "id") {
		t.Errorf("列表应包含表头 'id'，得到:\n%s", out)
	}
}

// TestInspect_Detail_NoRaw 指定 id、未传 --raw 时应提示有 N bytes 但不 dump。
func TestInspect_Detail_NoRaw(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "t.db")

	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.InsertRecord(track.Record{
		Timestamp: time.Now().UTC(), Command: "mvn test",
		ExitCode: 1, InputTokens: 500, OutputTokens: 40,
		SavedTokens: 460, ElapsedMs: 1200, FilterUsed: "java/maven",
		RawOutput: "full-maven-log-very-long",
	})
	recs, _ := db.RecentRecords(1)
	db.Close()

	var buf bytes.Buffer
	idStr := ""
	if len(recs) > 0 {
		idStr = itoa64(recs[0].ID)
	}
	if err := runInspectWithDB(&buf, dbPath, []string{idStr}, false); err != nil {
		t.Fatalf("runInspect: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "mvn test") {
		t.Errorf("期望详情包含命令，得到:\n%s", out)
	}
	if !strings.Contains(out, "bytes available") {
		t.Errorf("未带 --raw 时应提示 bytes available，得到:\n%s", out)
	}
	if strings.Contains(out, "full-maven-log-very-long") {
		t.Errorf("未带 --raw 时不应打印 raw 内容，得到:\n%s", out)
	}
}

// TestInspect_Detail_WithRaw 指定 id + --raw 应打印原文。
func TestInspect_Detail_WithRaw(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "t.db")

	db, err := track.NewDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	raw := "line1\nline2\nerror: boom\n"
	_ = db.InsertRecord(track.Record{
		Timestamp: time.Now().UTC(), Command: "mvn test",
		ExitCode: 1, InputTokens: 500, OutputTokens: 40,
		SavedTokens: 460, ElapsedMs: 1200, FilterUsed: "java/maven",
		RawOutput: raw,
	})
	recs, _ := db.RecentRecords(1)
	db.Close()

	var buf bytes.Buffer
	idStr := ""
	if len(recs) > 0 {
		idStr = itoa64(recs[0].ID)
	}
	if err := runInspectWithDB(&buf, dbPath, []string{idStr}, true); err != nil {
		t.Fatalf("runInspect: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "line1") || !strings.Contains(out, "error: boom") {
		t.Errorf("带 --raw 应打印 raw 内容，得到:\n%s", out)
	}
}

// TestInspect_BadID 无效 id 应报错。
func TestInspect_BadID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "t.db")

	var buf bytes.Buffer
	err := runInspectWithDB(&buf, dbPath, []string{"abc"}, false)
	if err == nil {
		t.Fatal("期望无效 id 报错")
	}
}

// TestInspect_NotFound 不存在的 id 应报错。
func TestInspect_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "t.db")

	var buf bytes.Buffer
	err := runInspectWithDB(&buf, dbPath, []string{"999"}, false)
	if err == nil {
		t.Fatal("期望未找到记录报错")
	}
}

func itoa64(i int64) string {
	return strconv.FormatInt(i, 10)
}

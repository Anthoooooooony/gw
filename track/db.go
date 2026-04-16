package track

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Record 记录一次命令执行的追踪数据
type Record struct {
	Timestamp    time.Time
	Command      string
	ExitCode     int
	InputTokens  int
	OutputTokens int
	SavedTokens  int
	ElapsedMs    int64
	FilterUsed   string
}

// Stats 汇总统计
type Stats struct {
	TotalSaved   int
	TotalInput   int
	CommandCount int
}

// TopCommand 按节省量排名的命令
type TopCommand struct {
	Command    string
	TotalSaved int
	AvgPct     float64
}

// DB 封装 SQLite 连接
type DB struct {
	db *sql.DB
}

const createTableSQL = `
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
);
`

// DefaultDBPath 返回默认数据库路径 ~/.gw/tracking.db
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".gw", "tracking.db")
}

// NewDB 打开或创建数据库
func NewDB(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	sqlDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=3000")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.Exec(createTableSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("创建表失败: %w", err)
	}

	return &DB{db: sqlDB}, nil
}

// Close 关闭数据库连接
func (d *DB) Close() error {
	return d.db.Close()
}

// InsertRecord 插入一条追踪记录
func (d *DB) InsertRecord(r Record) error {
	_, err := d.db.Exec(
		`INSERT INTO tracking (timestamp, command, exit_code, input_tokens, output_tokens, saved_tokens, elapsed_ms, filter_used)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Timestamp.UTC().Format(time.RFC3339),
		r.Command,
		r.ExitCode,
		r.InputTokens,
		r.OutputTokens,
		r.SavedTokens,
		r.ElapsedMs,
		r.FilterUsed,
	)
	return err
}

// queryStats 按时间范围查询统计
func (d *DB) queryStats(since string) (Stats, error) {
	var s Stats
	var query string
	var args []interface{}

	if since == "" {
		query = `SELECT COALESCE(SUM(saved_tokens),0), COALESCE(SUM(input_tokens),0), COUNT(*) FROM tracking`
	} else {
		query = `SELECT COALESCE(SUM(saved_tokens),0), COALESCE(SUM(input_tokens),0), COUNT(*) FROM tracking WHERE timestamp >= ?`
		args = append(args, since)
	}

	err := d.db.QueryRow(query, args...).Scan(&s.TotalSaved, &s.TotalInput, &s.CommandCount)
	return s, err
}

// TodayStats 返回今日统计
func (d *DB) TodayStats() (Stats, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	return d.queryStats(today)
}

// WeekStats 返回本周统计
func (d *DB) WeekStats() (Stats, error) {
	weekAgo := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	return d.queryStats(weekAgo)
}

// AllStats 返回全部统计
func (d *DB) AllStats() (Stats, error) {
	return d.queryStats("")
}

// TopCommands 返回按节省量排名的前 N 条命令
func (d *DB) TopCommands(limit int) ([]TopCommand, error) {
	rows, err := d.db.Query(
		`SELECT command,
		        SUM(saved_tokens) as total_saved,
		        CASE WHEN SUM(input_tokens) > 0
		             THEN CAST(SUM(saved_tokens) AS REAL) / SUM(input_tokens) * 100
		             ELSE 0 END as avg_pct
		 FROM tracking
		 GROUP BY command
		 ORDER BY total_saved DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TopCommand
	for rows.Next() {
		var tc TopCommand
		if err := rows.Scan(&tc.Command, &tc.TotalSaved, &tc.AvgPct); err != nil {
			return nil, err
		}
		result = append(result, tc)
	}
	return result, rows.Err()
}

// Cleanup 删除超过 retentionDays 天的记录
func (d *DB) Cleanup(retentionDays int) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339)
	_, err := d.db.Exec(`DELETE FROM tracking WHERE timestamp < ?`, cutoff)
	return err
}

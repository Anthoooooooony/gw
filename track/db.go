package track

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// dbWarnOnce 保证 HOME 只读降级的 warning 在同一进程内只打一次，
// 避免并发调用方（如 verbose 模式下多次 NewDB）刷屏。
var dbWarnOnce sync.Once

// Record 记录一次命令执行的追踪数据
type Record struct {
	ID           int64
	Timestamp    time.Time
	Command      string
	ExitCode     int
	InputTokens  int
	OutputTokens int
	SavedTokens  int
	ElapsedMs    int64
	FilterUsed   string
	// RawOutput 保存命令的原始未过滤输出。默认不落盘（避免 DB 爆炸），
	// 仅在 GW_STORE_RAW=1 时由调用方填充。
	RawOutput string
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
	filter_used TEXT NOT NULL DEFAULT '',
	raw_output TEXT NOT NULL DEFAULT ''
);
`

// DefaultDBPath 返回 tracking DB 的路径。
// 优先级：
//  1. 环境变量 GW_DB_PATH（显式路径，覆盖一切）
//  2. ~/.gw/tracking.db（默认）
//  3. $TMPDIR/gw-tracking.db（HOME 不可写时降级，并 stderr warn 一次）
func DefaultDBPath() string {
	home, _ := os.UserHomeDir()
	return resolveDBPathWithEnv(os.Getenv("GW_DB_PATH"), home, os.Stderr)
}

// resolveDBPathWithEnv 是 DefaultDBPath 的可测试内核。
// envPath 为 GW_DB_PATH 原始值（空串表示未设置）；homeDir 为 os.UserHomeDir 结果。
func resolveDBPathWithEnv(envPath, homeDir string, stderr io.Writer) string {
	if envPath != "" {
		return envPath
	}

	// 默认路径
	if homeDir != "" {
		primary := filepath.Join(homeDir, ".gw", "tracking.db")
		// 尝试创建目标目录，失败则视为 HOME 不可写降级
		dir := filepath.Dir(primary)
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return primary
		}
	}

	// 降级：只 warn 一次
	fallback := filepath.Join(os.TempDir(), "gw-tracking.db")
	dbWarnOnce.Do(func() {
		fmt.Fprintf(stderr, "gw: warning: HOME not writable, tracking DB fallback to %s\n", fallback)
	})
	return fallback
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
		_ = sqlDB.Close() // 已在错误路径，关闭错误不重要
		return nil, fmt.Errorf("创建表失败: %w", err)
	}

	// 迁移：旧版本 DB 可能缺少 raw_output 列。探测并按需 ALTER TABLE。
	// 绝对不能 DROP TABLE：用户 ~/.gw/tracking.db 里是生产数据。
	if err := ensureRawOutputColumn(sqlDB); err != nil {
		_ = sqlDB.Close() // 已在错误路径，关闭错误不重要
		return nil, fmt.Errorf("迁移 raw_output 列失败: %w", err)
	}

	return &DB{db: sqlDB}, nil
}

// ensureRawOutputColumn 通过 PRAGMA table_info 检查 tracking 表列集合，
// 若不含 raw_output 则 ALTER TABLE 添加。已存在时为 no-op，幂等。
func ensureRawOutputColumn(sqlDB *sql.DB) error {
	rows, err := sqlDB.Query(`PRAGMA table_info(tracking)`)
	if err != nil {
		return fmt.Errorf("读取表结构失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	hasRaw := false
	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			dfltValue  sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &primaryKey); err != nil {
			return fmt.Errorf("扫描列信息失败: %w", err)
		}
		if name == "raw_output" {
			hasRaw = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if !hasRaw {
		if _, err := sqlDB.Exec(`ALTER TABLE tracking ADD COLUMN raw_output TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ALTER TABLE 失败: %w", err)
		}
	}
	return nil
}

// Close 关闭数据库连接
func (d *DB) Close() error {
	return d.db.Close()
}

// InsertRecord 插入一条追踪记录
func (d *DB) InsertRecord(r Record) error {
	_, err := d.db.Exec(
		`INSERT INTO tracking (timestamp, command, exit_code, input_tokens, output_tokens, saved_tokens, elapsed_ms, filter_used, raw_output)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Timestamp.UTC().Format(time.RFC3339),
		r.Command,
		r.ExitCode,
		r.InputTokens,
		r.OutputTokens,
		r.SavedTokens,
		r.ElapsedMs,
		r.FilterUsed,
		r.RawOutput,
	)
	return err
}

// RecentRecords 返回最近 limit 条记录，按 id 降序。
func (d *DB) RecentRecords(limit int) ([]Record, error) {
	rows, err := d.db.Query(
		`SELECT id, timestamp, command, exit_code, input_tokens, output_tokens,
		        saved_tokens, elapsed_ms, filter_used, raw_output
		 FROM tracking
		 ORDER BY id DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []Record
	for rows.Next() {
		var (
			r       Record
			tsStr   string
			rawOut  sql.NullString
			filterU sql.NullString
		)
		if err := rows.Scan(&r.ID, &tsStr, &r.Command, &r.ExitCode,
			&r.InputTokens, &r.OutputTokens, &r.SavedTokens,
			&r.ElapsedMs, &filterU, &rawOut); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
			r.Timestamp = t
		}
		if filterU.Valid {
			r.FilterUsed = filterU.String
		}
		if rawOut.Valid {
			r.RawOutput = rawOut.String
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetRecord 按 id 查询单条记录，未找到返回 sql.ErrNoRows。
func (d *DB) GetRecord(id int64) (Record, error) {
	var (
		r       Record
		tsStr   string
		rawOut  sql.NullString
		filterU sql.NullString
	)
	err := d.db.QueryRow(
		`SELECT id, timestamp, command, exit_code, input_tokens, output_tokens,
		        saved_tokens, elapsed_ms, filter_used, raw_output
		 FROM tracking WHERE id = ?`, id).Scan(
		&r.ID, &tsStr, &r.Command, &r.ExitCode,
		&r.InputTokens, &r.OutputTokens, &r.SavedTokens,
		&r.ElapsedMs, &filterU, &rawOut)
	if err != nil {
		return r, err
	}
	if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
		r.Timestamp = t
	}
	if filterU.Valid {
		r.FilterUsed = filterU.String
	}
	if rawOut.Valid {
		r.RawOutput = rawOut.String
	}
	return r, nil
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
	defer func() { _ = rows.Close() }()

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

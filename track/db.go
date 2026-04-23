package track

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// 默认大小阈值 & 一次裁剪后的软目标（软目标 = 硬阈值 * softTargetRatio）。
// 100 MiB 够存数千到数万条带 raw_output 的记录，足以覆盖日常回溯需求，又不会让
// SQLite 文件在笔记本上爆炸。裁剪后压到 80 MiB，给后续写入留 20 MiB 预算，避免
// 反复触发 trim。
const (
	defaultDBMaxBytes    int64   = 100 << 20 // 100 MiB
	defaultSoftTargetPct float64 = 0.80
)

// DBMaxBytes 返回 DB 文件硬阈值字节数。
// 由 GW_DB_MAX_BYTES 覆盖（支持纯数字字节数）；解析失败回落默认值。
// 设为 0 或负数时视为"关闭大小裁剪"。
func DBMaxBytes() int64 {
	v := os.Getenv("GW_DB_MAX_BYTES")
	if v == "" {
		return defaultDBMaxBytes
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return defaultDBMaxBytes
	}
	return n
}

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
	// RawOutput 保存命令的原始未过滤输出。始终落盘以便 inspect --raw 回溯，
	// DB 体积由 TrimBySize 按阈值裁剪（默认 100 MiB，GW_DB_MAX_BYTES 可覆盖）。
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
		return fmt.Errorf("遍历列信息失败: %w", err)
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

// sizeOnDisk 估算 SQLite 数据库的磁盘占用（含 WAL 文件）。
// 使用文件大小而非 PRAGMA page_count*page_size：WAL 模式下未合并的页会长期留在
// .wal 里，只看主文件会低估真实占用，从而让阈值形同虚设。
func (d *DB) sizeOnDisk() (int64, error) {
	var total int64
	paths, err := dbFilePaths(d.db)
	if err != nil {
		return 0, err
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue // WAL / SHM 在静默期可能不存在
			}
			return 0, err
		}
		total += info.Size()
	}
	return total, nil
}

// dbFilePaths 返回 main DB 文件及其 WAL / SHM 辅助文件路径。
func dbFilePaths(sqlDB *sql.DB) ([]string, error) {
	var (
		seq      int
		name     string
		filePath sql.NullString
	)
	row := sqlDB.QueryRow(`PRAGMA database_list`)
	if err := row.Scan(&seq, &name, &filePath); err != nil {
		return nil, fmt.Errorf("读取 database_list 失败: %w", err)
	}
	if !filePath.Valid || filePath.String == "" {
		return nil, nil
	}
	main := filePath.String
	return []string{main, main + "-wal", main + "-shm"}, nil
}

// TrimBySize 检查 DB 大小，若超过阈值则按 timestamp 删最旧记录直到低于软目标。
// 返回实际删除的记录数。阈值由 DBMaxBytes() 决定（可经 GW_DB_MAX_BYTES 调）。
// 阈值 ≤ 0 时直接跳过（视为关闭）。
//
// 实现：先按大小估算"每条平均字节数"，反推需要删除的条数并一次性 DELETE，
// 再 VACUUM 回收页。相比逐条 DELETE 性能好，且避免在统计命令路径上 block 过久。
func (d *DB) TrimBySize() (int, error) {
	return d.trimBySize(DBMaxBytes(), defaultSoftTargetPct)
}

// trimBySize 是 TrimBySize 的可测试内核。
func (d *DB) trimBySize(maxBytes int64, softRatio float64) (int, error) {
	if maxBytes <= 0 {
		return 0, nil
	}
	size, err := d.sizeOnDisk()
	if err != nil {
		return 0, err
	}
	if size <= maxBytes {
		return 0, nil
	}

	// 算需要腾出多少字节才能降到 soft target。
	target := int64(float64(maxBytes) * softRatio)
	if target <= 0 {
		target = maxBytes / 2
	}
	toFree := size - target

	var rowCount int64
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM tracking`).Scan(&rowCount); err != nil {
		return 0, fmt.Errorf("统计行数失败: %w", err)
	}
	if rowCount == 0 {
		return 0, nil
	}

	avg := size / rowCount
	if avg <= 0 {
		avg = 1
	}
	// 至少删 1 条；最多删全表（VACUUM 会负责把空页还给 OS）。
	toDelete := toFree/avg + 1
	if toDelete > rowCount {
		toDelete = rowCount
	}

	res, err := d.db.Exec(
		`DELETE FROM tracking WHERE id IN (
			SELECT id FROM tracking ORDER BY timestamp ASC, id ASC LIMIT ?
		)`, toDelete)
	if err != nil {
		return 0, fmt.Errorf("裁剪旧记录失败: %w", err)
	}
	affected, _ := res.RowsAffected()

	// VACUUM 回收页到 OS。在 WAL 模式下 VACUUM 自动处理 checkpoint，
	// 失败不致命（只是 DB 文件仍然偏大，下次写入会复用空页）。
	if _, err := d.db.Exec(`VACUUM`); err != nil {
		return int(affected), fmt.Errorf("VACUUM 失败（裁剪已完成但未回收空间）: %w", err)
	}
	return int(affected), nil
}

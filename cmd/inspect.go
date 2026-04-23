package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/Anthoooooooony/gw/track"
	"github.com/spf13/cobra"
)

var inspectShowRaw bool

var inspectCmd = &cobra.Command{
	Use:   "inspect [id]",
	Short: "查看最近命令记录或单条详情",
	Long: `不带参数显示最近 10 条记录列表。
带 id 显示该记录详情。加 --raw 可打印原始未过滤输出（原始输出默认始终落盘）。`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInspect(cmd.OutOrStdout(), args, inspectShowRaw)
	},
}

func init() {
	inspectCmd.Flags().BoolVar(&inspectShowRaw, "raw", false, "打印 raw_output 原始输出（默认省略）")
	rootCmd.AddCommand(inspectCmd)
}

func runInspect(w io.Writer, args []string, showRaw bool) error {
	return runInspectWithDB(w, track.DefaultDBPath(), args, showRaw)
}

// runInspectWithDB 方便测试：允许显式指定 DB 路径。
func runInspectWithDB(w io.Writer, dbPath string, args []string, showRaw bool) error {
	db, err := track.NewDB(dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer func() { _ = db.Close() }()

	if len(args) == 0 {
		return listRecentRecords(w, db)
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("无效的 id %q: %w", args[0], err)
	}
	return showRecord(w, db, id, showRaw)
}

func listRecentRecords(w io.Writer, db *track.DB) error {
	recs, err := db.RecentRecords(10)
	if err != nil {
		return fmt.Errorf("读取记录失败: %w", err)
	}
	if len(recs) == 0 {
		fmt.Fprintln(w, "(无记录)")
		return nil
	}

	fmt.Fprintf(w, "%-6s  %-20s  %-30s  %10s  %10s  %8s\n",
		"id", "timestamp", "command", "input", "saved", "ratio")
	fmt.Fprintln(w, "------  --------------------  ------------------------------  ----------  ----------  --------")
	for _, r := range recs {
		ratio := 0.0
		if r.InputTokens > 0 {
			ratio = float64(r.SavedTokens) / float64(r.InputTokens) * 100
		}
		cmdStr := r.Command
		if len(cmdStr) > 30 {
			cmdStr = cmdStr[:27] + "..."
		}
		ts := r.Timestamp.Format("2006-01-02 15:04:05")
		fmt.Fprintf(w, "%-6d  %-20s  %-30s  %10d  %10d  %7.1f%%\n",
			r.ID, ts, cmdStr, r.InputTokens, r.SavedTokens, ratio)
	}
	return nil
}

func showRecord(w io.Writer, db *track.DB, id int64, showRaw bool) error {
	r, err := db.GetRecord(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("未找到 id=%d 的记录", id)
		}
		return err
	}

	fmt.Fprintf(w, "ID:            %d\n", r.ID)
	fmt.Fprintf(w, "Timestamp:     %s\n", r.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Command:       %s\n", r.Command)
	fmt.Fprintf(w, "Exit code:     %d\n", r.ExitCode)
	fmt.Fprintf(w, "Filter:        %s\n", r.FilterUsed)
	fmt.Fprintf(w, "Input tokens:  %d\n", r.InputTokens)
	fmt.Fprintf(w, "Output tokens: %d\n", r.OutputTokens)
	fmt.Fprintf(w, "Saved tokens:  %d\n", r.SavedTokens)
	if r.InputTokens > 0 {
		ratio := float64(r.SavedTokens) / float64(r.InputTokens) * 100
		fmt.Fprintf(w, "Ratio:         %.1f%%\n", ratio)
	}
	fmt.Fprintf(w, "Elapsed:       %dms\n", r.ElapsedMs)

	if r.RawOutput == "" {
		fmt.Fprintln(w, "Raw output:    (无，该记录未记录原文)")
		return nil
	}

	if !showRaw {
		fmt.Fprintf(w, "Raw output:    %d bytes available (加 --raw 打印)\n", len(r.RawOutput))
		return nil
	}

	fmt.Fprintln(w, "--- raw output ---")
	if _, err := io.WriteString(w, r.RawOutput); err != nil {
		return err
	}
	if len(r.RawOutput) > 0 && r.RawOutput[len(r.RawOutput)-1] != '\n' {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "--- end raw output ---")
	return nil
}

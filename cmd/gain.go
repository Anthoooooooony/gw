package cmd

import (
	"fmt"
	"os"

	"github.com/gw-cli/gw/track"
	"github.com/spf13/cobra"
)

var gainCmd = &cobra.Command{
	Use:   "gain",
	Short: "显示 token 节省统计",
	Long:  "显示今日、本周、全部的 token 节省统计和排名靠前的命令。",
	Run:   runGain,
}

func init() {
	rootCmd.AddCommand(gainCmd)
}

func runGain(cmd *cobra.Command, args []string) {
	db, err := track.NewDB(track.DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw gain: 打开数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	// 清理超过 90 天的旧记录
	_ = db.Cleanup(90)

	today, err := db.TodayStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw gain: 查询统计失败: %v\n", err)
		os.Exit(1)
	}

	week, _ := db.WeekStats()
	all, _ := db.AllStats()

	fmt.Println("=== gw token 节省统计 ===")
	fmt.Println()
	printStats("今日", today)
	printStats("本周", week)
	printStats("全部", all)

	top, err := db.TopCommands(5)
	if err == nil && len(top) > 0 {
		fmt.Println("--- Top 5 命令 ---")
		for i, tc := range top {
			fmt.Printf("  %d. %-30s saved=%d tokens  (%.1f%%)\n", i+1, tc.Command, tc.TotalSaved, tc.AvgPct)
		}
		fmt.Println()
	}
}

func printStats(label string, s track.Stats) {
	savePct := 0.0
	if s.TotalInput > 0 {
		savePct = float64(s.TotalSaved) / float64(s.TotalInput) * 100
	}
	fmt.Printf("[%s]  命令: %d  输入tokens: %d  节省tokens: %d  (%.1f%%)\n",
		label, s.CommandCount, s.TotalInput, s.TotalSaved, savePct)
}

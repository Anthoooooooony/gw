package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Anthoooooooony/gw/filter"
	"github.com/spf13/cobra"
)

var filtersCmd = &cobra.Command{
	Use:   "filters",
	Short: "管理和查看已注册的过滤器",
	Long:  "查看 gw 中已注册的 Go 硬编码过滤器和 TOML 声明式规则。",
}

var filtersListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出全部过滤器和 TOML 规则",
	Long:  "以表格形式列出所有已注册的过滤器，显示来源（builtin / user / project）。",
	Run:   runFiltersList,
}

func init() {
	filtersCmd.AddCommand(filtersListCmd)
	rootCmd.AddCommand(filtersCmd)
}

// collectFilterRows 遍历全局注册表，优先让实现 Describable 的 filter 自描述多行
// （如 TOML filter 展开每条 rule），其余按 Go 硬编码过滤器生成单行。
func collectFilterRows() []filter.FilterRow {
	rows := make([]filter.FilterRow, 0)
	for _, f := range filter.GlobalRegistry().List() {
		if d, ok := f.(filter.Describable); ok {
			rows = append(rows, d.Describe()...)
			continue
		}
		rows = append(rows, filter.FilterRow{
			Name:   f.Name(),
			Type:   "go",
			Source: "builtin",
			Match:  goFilterMatchHint(f),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Type != rows[j].Type {
			return rows[i].Type < rows[j].Type
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

// goFilterMatchHint 为 Go 硬编码过滤器生成一个可读的 MATCH 提示。
// Match() 是黑盒函数无法直接反向推导，这里用命名约定 "foo/bar" → "foo bar"。
func goFilterMatchHint(f filter.Filter) string {
	return goFilterMatchHintFromName(f.Name())
}

// goFilterMatchHintFromName 对名字执行映射，便于单测。
func goFilterMatchHintFromName(name string) string {
	if idx := strings.Index(name, "/"); idx > 0 {
		return strings.ReplaceAll(name[:idx]+" "+name[idx+1:], "_", " ")
	}
	return name
}

// renderFilters 将 rows 写入 tabwriter 表格。
func renderFilters(w io.Writer, rows []filter.FilterRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tSOURCE\tMATCH")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Type, r.Source, r.Match)
	}
	_ = tw.Flush()
}

func runFiltersList(cmd *cobra.Command, args []string) {
	rows := collectFilterRows()
	renderFilters(cmd.OutOrStdout(), rows)
}

package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Anthoooooooony/gw/filter"
	tomlfilter "github.com/Anthoooooooony/gw/filter/toml"
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

// filterRow 是表格的一行
type filterRow struct {
	Name   string
	Type   string // go | toml
	Source string // builtin | user://... | project://...
	Match  string
}

// collectFilterRows 遍历全局注册表，为每个过滤器生成一行，
// 若是 TomlFilter 则按其 Loaded 列表展开为多行。
func collectFilterRows() []filterRow {
	rows := make([]filterRow, 0)
	for _, f := range filter.GlobalRegistry().List() {
		// TOML 过滤器单独展开
		if tf, ok := f.(*tomlfilter.TomlFilter); ok {
			for _, lr := range tf.Loaded {
				rows = append(rows, filterRow{
					Name:   lr.ID,
					Type:   "toml",
					Source: lr.Source,
					Match:  lr.Rule.Match,
				})
			}
			continue
		}
		// Go 硬编码过滤器
		rows = append(rows, filterRow{
			Name:   f.Name(),
			Type:   "go",
			Source: tomlfilter.SourceBuiltin,
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
func renderFilters(w io.Writer, rows []filterRow) {
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

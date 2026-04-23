// Package fs 实现文件系统相关命令（ls / find / grep）的输出压缩。
package fs

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&LsFilter{})
}

// LsFilter 压缩 ls 输出：丢弃 `total NNNN` 头行和 `.`/`..` 条目。
// 保守策略，不改格式——gw 是透明代理，重排输出会让用户困惑。
type LsFilter struct{}

func (f *LsFilter) Name() string { return "fs/ls" }

func (f *LsFilter) Match(cmd string, args []string) bool {
	return cmd == "ls"
}

func (f *LsFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	// ls 默认不加色，但 `ls -G` (BSD) / `ls --color=always` (GNU) / 别名
	// 会给目录名加色码，HasSuffix("/") 之类的判断会因尾部色码失效。
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	var out []string
	for _, line := range lines {
		// `ls -l` / `ls -la` 会打印 `total 1234` 作为块数——对 LLM 毫无价值
		if strings.HasPrefix(line, "total ") {
			continue
		}
		// `ls -a` / `ls -la` 会打印 `.` 和 `..`——也无价值
		if isLsDotEntry(line) {
			continue
		}
		out = append(out, line)
	}

	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

func (f *LsFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

// isLsDotEntry 识别 `.` / `..` 行。支持 `ls -a`（纯名字）和 `ls -la`（含元信息）两种格式。
func isLsDotEntry(line string) bool {
	trimmed := strings.TrimRight(line, " \t")
	// 纯名字行
	if trimmed == "." || trimmed == ".." {
		return true
	}
	// `-la` 行以名字结尾——`\s+\.$` 或 `\s+\.\.$`
	if strings.HasSuffix(trimmed, " .") || strings.HasSuffix(trimmed, " ..") {
		return true
	}
	return false
}

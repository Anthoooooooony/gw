package fs

import (
	"fmt"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&GrepFilter{})
}

// GrepFilter 压缩 grep / rg / ripgrep 输出：
//   - 超长行（如 minified JS）截断到 grepLineMaxChars
//   - 命中过多时 head + tail
type GrepFilter struct{}

func (f *GrepFilter) Name() string { return "fs/grep" }

// grepCommands 本 filter 接管的工具名。
var grepCommands = map[string]bool{
	"grep":    true,
	"egrep":   true,
	"fgrep":   true,
	"rg":      true,
	"ripgrep": true,
}

func (f *GrepFilter) Match(cmd string, args []string) bool {
	return grepCommands[cmd]
}

const (
	grepLineMaxChars   = 300
	grepPassThroughMax = 150
	grepHeadKeep       = 80
	grepTailKeep       = 30
)

func (f *GrepFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	// 逐行截断超长行
	var truncated []string
	for _, line := range lines {
		if len(line) > grepLineMaxChars {
			truncated = append(truncated, line[:grepLineMaxChars]+fmt.Sprintf("... [截断 %d 字符]", len(line)-grepLineMaxChars))
		} else {
			truncated = append(truncated, line)
		}
	}

	// 短输出直接返回
	if len(truncated) <= grepPassThroughMax {
		return filter.FilterOutput{
			Content:  strings.Join(truncated, "\n"),
			Original: original,
		}
	}

	// 长输出 head + tail
	omitted := len(truncated) - grepHeadKeep - grepTailKeep
	var out []string
	out = append(out, truncated[:grepHeadKeep]...)
	out = append(out, fmt.Sprintf("... (%d 行省略) ...", omitted))
	out = append(out, truncated[len(truncated)-grepTailKeep:]...)

	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

func (f *GrepFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// grep 的 exit 1 = no match（正常情况），不是失败；直接透传。
	return nil
}

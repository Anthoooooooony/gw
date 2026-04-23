package fs

import (
	"fmt"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&FindFilter{})
}

// FindFilter 压缩 find 输出：过长结果集取 head + tail，丢 `Permission denied` 噪声。
type FindFilter struct{}

func (f *FindFilter) Name() string { return "fs/find" }

func (f *FindFilter) Match(cmd string, args []string) bool {
	return cmd == "find"
}

// 阈值：≤ findPassThroughMax 行原样透传；否则 head + tail 截断。
const (
	findPassThroughMax = 120
	findHeadKeep       = 60
	findTailKeep       = 20
)

func (f *FindFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)

	// find 把 `Permission denied` 等错误写到 stderr，gw 拼接 stdout+stderr 时会混入。
	// 这些行对 LLM 是纯噪声。
	lines := strings.Split(content, "\n")
	var kept []string
	for _, line := range lines {
		if isFindNoise(line) {
			continue
		}
		kept = append(kept, line)
	}

	// 保守透传（含尾部空行引入的条目）
	if len(kept) <= findPassThroughMax {
		return filter.FilterOutput{
			Content:  strings.Join(kept, "\n"),
			Original: original,
		}
	}

	// 超长：head + elision + tail
	omitted := len(kept) - findHeadKeep - findTailKeep
	var out []string
	out = append(out, kept[:findHeadKeep]...)
	out = append(out, fmt.Sprintf("... (%d 行省略) ...", omitted))
	out = append(out, kept[len(kept)-findTailKeep:]...)

	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

func (f *FindFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

// isFindNoise 识别 find 的已知噪声行（stderr 合流过来的权限错误等）。
func isFindNoise(line string) bool {
	// GNU find: `find: '/proc/1': Permission denied`
	// BSD find: `find: /path: Permission denied`
	if strings.HasPrefix(line, "find:") && strings.Contains(line, "Permission denied") {
		return true
	}
	return false
}

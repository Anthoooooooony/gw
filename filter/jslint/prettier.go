package jslint

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&PrettierFilter{})
}

// PrettierFilter 压缩 prettier --check 输出：
// 丢 `Checking formatting...` 头行，其余透传（每文件一行已经很 compact）。
type PrettierFilter struct{}

func (f *PrettierFilter) Name() string { return "jslint/prettier" }

func (f *PrettierFilter) Match(cmd string, args []string) bool {
	return cmd == "prettier"
}

func (f *PrettierFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	var out []string
	for _, line := range lines {
		// `Checking formatting...` 是开始标记，没有 LLM 价值
		if strings.HasPrefix(line, "Checking formatting") {
			continue
		}
		out = append(out, line)
	}

	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

func (f *PrettierFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// prettier exit=1 = 发现未格式化文件，正常情况。
	out := f.Apply(input)
	return &out
}

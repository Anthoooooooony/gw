package jsbuild

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&NextFilter{})
}

// NextFilter 压缩 next build：
// 丢 ▲ 标识行、Creating... 进度、图例 `○ (Static)` 这类标记说明；
// 保留 ✓ status checklist 和 Route/Size 表。
type NextFilter struct{}

func (f *NextFilter) Name() string { return "jsbuild/next" }

func (f *NextFilter) Match(cmd string, args []string) bool {
	if cmd != "next" {
		return false
	}
	for _, a := range args {
		if a == "build" {
			return true
		}
	}
	return false
}

// nextLegendPrefixes 识别 `○  (Static)` 这类标记图例行（图例对 LLM 冗余）。
var nextLegendPrefixes = []string{
	"○  (Static)",
	"●  (SSG)",
	"λ  (Dynamic)",
	"ƒ  (Dynamic)",
}

// nextNoiseSubstrings 整行匹配的噪声。
var nextNoiseSubstrings = []string{
	"▲ Next.js",
	"Creating an optimized production build",
}

func (f *NextFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	var out []string
	for _, line := range lines {
		if isNextNoise(line) {
			continue
		}
		out = append(out, line)
	}

	joined := strings.Join(out, "\n")
	return filter.FilterOutput{
		Content:  collapseBlankLines(joined),
		Original: original,
	}
}

func (f *NextFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

func isNextNoise(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	for _, p := range nextLegendPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	for _, s := range nextNoiseSubstrings {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

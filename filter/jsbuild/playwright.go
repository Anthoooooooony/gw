// Package jsbuild 实现 JS/TS 构建+E2E 测试命令（playwright / prisma / next）的输出压缩。
package jsbuild

import (
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&PlaywrightFilter{})
}

// PlaywrightFilter 压缩 playwright test：
// 成功场景丢 ✓ 通过行只留 summary；失败场景保留失败详情但丢通过行。
type PlaywrightFilter struct{}

func (f *PlaywrightFilter) Name() string { return "jsbuild/playwright" }

func (f *PlaywrightFilter) Match(cmd string, args []string) bool {
	return cmd == "playwright"
}

// `  ✓  1 [chromium] › tests/login.spec.ts:3:1 › title (1.2s)`
var playwrightPassLineRe = regexp.MustCompile(`^\s+✓\s+\d+\s+\[`)

// `  Slow test file: ...`
var playwrightSlowLineRe = regexp.MustCompile(`^\s+Slow test file:`)

func (f *PlaywrightFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	return compressPlaywright(input.Stdout, input.ExitCode == 0)
}

func (f *PlaywrightFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	out := compressPlaywright(input.Stdout, false)
	return &out
}

func compressPlaywright(original string, success bool) filter.FilterOutput {
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	var out []string
	for _, line := range lines {
		// ✓ 通过行：对 LLM 冗余，无论成功/失败都丢
		if playwrightPassLineRe.MatchString(line) {
			continue
		}
		// "Slow test file" 提示也丢
		if playwrightSlowLineRe.MatchString(line) {
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

// collapseBlankLines 2+ 连续空行压成 1 个。
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank {
			continue
		}
		out = append(out, line)
		prevBlank = isBlank
	}
	return strings.Join(out, "\n")
}

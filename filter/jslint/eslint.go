// Package jslint 实现 JS/TS lint 家族命令（eslint / tsc / prettier）的输出压缩。
package jslint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&ESLintFilter{})
}

// ESLintFilter 压缩 eslint stylish 格式输出：丢 "fixable with --fix" 等提示尾，
// 超长时按文件块 head+tail 截断。
type ESLintFilter struct{}

func (f *ESLintFilter) Name() string { return "jslint/eslint" }

func (f *ESLintFilter) Match(cmd string, args []string) bool {
	return cmd == "eslint" || cmd == "biome"
}

// 问题行：`  1:1  error  msg  rule`
var eslintProblemRe = regexp.MustCompile(`^\s+\d+:\d+\s+(error|warning)\s+`)

// summary 行：`✖ 9 problems (7 errors, 2 warnings)`
var eslintSummaryRe = regexp.MustCompile(`^.\s*\d+ problems? \(`)

// fixable hint：`  6 errors and 0 warnings potentially fixable with the ...`
var eslintFixableHintRe = regexp.MustCompile(`potentially fixable with`)

const (
	eslintPassThroughMaxLines = 120
	eslintHeadKeep            = 80
	eslintTailKeep            = 20
)

func (f *ESLintFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	// 丢 fixable 提示行（噪声）
	var kept []string
	for _, line := range lines {
		if eslintFixableHintRe.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}

	// 短输出原样
	if len(kept) <= eslintPassThroughMaxLines {
		return filter.FilterOutput{
			Content:  strings.Join(kept, "\n"),
			Original: original,
		}
	}

	// 超长：保留 head + summary 行 + tail
	var out []string
	out = append(out, kept[:eslintHeadKeep]...)
	omitted := len(kept) - eslintHeadKeep - eslintTailKeep
	out = append(out, fmt.Sprintf("... (%d 行省略) ...", omitted))
	out = append(out, kept[len(kept)-eslintTailKeep:]...)

	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

func (f *ESLintFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// eslint exit=1 只是"发现问题"，不是程序崩溃；按 Apply 压。
	out := f.Apply(input)
	return &out
}

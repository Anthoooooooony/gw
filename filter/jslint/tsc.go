package jslint

import (
	"fmt"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&TscFilter{})
}

// TscFilter 压缩 tsc --noEmit 输出：短输出透传；超长做 head + tail。
// tsc 默认已按 `file(line,col): error TSxxxx: ...` 格式输出一行一错，
// 本身就很 compact，不做激进压缩。
type TscFilter struct{}

func (f *TscFilter) Name() string { return "jslint/tsc" }

func (f *TscFilter) Match(cmd string, args []string) bool {
	return cmd == "tsc"
}

const (
	tscPassThroughMaxLines = 150
	tscHeadKeep            = 100
	tscTailKeep            = 30
)

func (f *TscFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	if len(lines) <= tscPassThroughMaxLines {
		return filter.FilterOutput{Content: content, Original: original}
	}

	omitted := len(lines) - tscHeadKeep - tscTailKeep
	var out []string
	out = append(out, lines[:tscHeadKeep]...)
	out = append(out, fmt.Sprintf("... (%d 行省略) ...", omitted))
	out = append(out, lines[len(lines)-tscTailKeep:]...)

	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

func (f *TscFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	out := f.Apply(input)
	return &out
}

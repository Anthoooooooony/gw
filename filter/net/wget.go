// Package net 实现网络工具（wget 等）的输出压缩。
package net

import (
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&WgetFilter{})
}

// WgetFilter 压缩 wget 输出：丢握手/进度条，保留 URL + Length + saved summary。
type WgetFilter struct{}

func (f *WgetFilter) Name() string { return "net/wget" }

func (f *WgetFilter) Match(cmd string, args []string) bool {
	return cmd == "wget"
}

// 进度条行：`large.bin   50%[=====>     ]     5M   1.8MB/s`
// 特征：前导空格后是文件名，接着 `数字%[` 和等号条。
var wgetProgressRe = regexp.MustCompile(`\d+%\[[=> ]*\]`)

// 握手/DNS 行前缀——这些对 LLM 冗余。
var wgetNoisePrefixes = []string{
	"Resolving ",
	"Connecting to ",
	"HTTP request sent",
	"Proxy request sent",
	"Reusing existing connection",
}

func (f *WgetFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	var out []string
	for _, line := range lines {
		if isWgetNoise(line) {
			continue
		}
		out = append(out, line)
	}

	joined := strings.Join(out, "\n")
	// 压连续空行为单个空行（丢握手后会留大量空行）
	joined = collapseBlankLines(joined)

	return filter.FilterOutput{
		Content:  joined,
		Original: original,
	}
}

func (f *WgetFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// wget 失败信息（DNS 失败 / 403 / 404）通常短且关键，透传。
	return nil
}

func isWgetNoise(line string) bool {
	// 进度条行
	if wgetProgressRe.MatchString(line) {
		return true
	}
	// 握手/DNS
	trimmed := strings.TrimLeft(line, " \t")
	for _, p := range wgetNoisePrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

// collapseBlankLines 把 2 个或以上连续空行压成 1 个。
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

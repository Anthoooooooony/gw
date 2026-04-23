// Package gh 实现 GitHub CLI（gh）命令的输出压缩。
package gh

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&Filter{})
}

// Filter 压缩 gh pr/issue/run 的 list/view 输出。
type Filter struct{}

func (f *Filter) Name() string { return "gh" }

// ghResources 本 filter 接管的 gh 资源类型。
var ghResources = map[string]bool{
	"pr":       true,
	"issue":    true,
	"run":      true,
	"workflow": true,
}

// ghActions 本 filter 处理的操作。
var ghActions = map[string]bool{
	"list": true,
	"view": true,
}

func (f *Filter) Match(cmd string, args []string) bool {
	if cmd != "gh" || len(args) < 2 {
		return false
	}
	// --json 输出是结构化数据，盲压缩风险大，让它原样透传。
	for _, a := range args {
		if a == "--json" || strings.HasPrefix(a, "--json=") {
			return false
		}
	}
	return ghResources[args[0]] && ghActions[args[1]]
}

// Subname 让 FilterUsed 展示为 "gh/pr/list" 等，诊断友好。
func (f *Filter) Subname(cmd string, args []string) string {
	if cmd != "gh" || len(args) < 2 {
		return ""
	}
	return args[0] + "/" + args[1]
}

const (
	ghListPassThroughMax = 100
	ghListHeadKeep       = 60
	ghListTailKeep       = 20
)

func (f *Filter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)

	action := ""
	if len(input.Args) >= 2 {
		action = input.Args[1]
	}

	switch action {
	case "view":
		return compressView(original, content)
	case "list":
		return compressList(original, content)
	}
	return filter.FilterOutput{Content: original, Original: original}
}

func (f *Filter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// gh 失败常因 auth/404/rate limit，透传保留诊断信息。
	return nil
}

// compressView 压缩 `gh pr view` / `issue view` / `run view` 的元信息头：
// 丢弃空字段行（label:\t 后无内容），保留 body（在 `--\n` 之后）。
func compressView(original, content string) filter.FilterOutput {
	lines := strings.Split(content, "\n")
	var out []string
	inBody := false
	for _, line := range lines {
		if !inBody {
			if line == "--" {
				inBody = true
				out = append(out, line)
				continue
			}
			if isEmptyHeaderField(line) {
				continue
			}
		}
		out = append(out, line)
	}
	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

// emptyHeaderFieldRe 匹配 `label:` 后只有空白的行（gh pr view 的空字段）。
var emptyHeaderFieldRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z\- ]*:\s*$`)

func isEmptyHeaderField(line string) bool {
	return emptyHeaderFieldRe.MatchString(line)
}

// compressList 压缩 `gh pr list` / `issue list` / `run list` 的长表格：
// ≤ ghListPassThroughMax 行透传；超长时 head + elision + tail。
func compressList(original, content string) filter.FilterOutput {
	lines := strings.Split(content, "\n")
	// 末尾空行不计入行数
	effective := lines
	if len(effective) > 0 && effective[len(effective)-1] == "" {
		effective = effective[:len(effective)-1]
	}

	if len(effective) <= ghListPassThroughMax {
		return filter.FilterOutput{Content: original, Original: original}
	}

	omitted := len(effective) - ghListHeadKeep - ghListTailKeep
	var out []string
	out = append(out, effective[:ghListHeadKeep]...)
	out = append(out, fmt.Sprintf("... (%d 行省略) ...", omitted))
	out = append(out, effective[len(effective)-ghListTailKeep:]...)
	if strings.HasSuffix(original, "\n") {
		out = append(out, "")
	}
	return filter.FilterOutput{
		Content:  strings.Join(out, "\n"),
		Original: original,
	}
}

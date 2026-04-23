package git

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&DiffFilter{})
}

// DiffFilter 压缩 git diff 输出：保留文件身份/hunk 头/变更行，丢弃 context 行。
type DiffFilter struct{}

func (f *DiffFilter) Name() string { return "git/diff" }

func (f *DiffFilter) Match(cmd string, args []string) bool {
	return cmd == "git" && len(args) > 0 && args[0] == "diff"
}

func (f *DiffFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	// `color.diff=always` 会给 `+`/`-`/`@@` 前加色码，HasPrefix 判断失败，
	// StripANSI 保证 diff 在强制彩色下仍能正确识别锚点。
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	// 锚点缺失兜底：没有任何 `diff --git` 或 `@@` 行说明这不是 diff 文本
	// （可能是空 diff、`--stat`/`--numstat` 纯统计、或用户把 diff 改成自定义格式）。
	// 此时透传原文，避免压成空字符串（filter invariant）。
	hasDiffHeader := false
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "@@") {
			hasDiffHeader = true
			break
		}
	}
	if !hasDiffHeader {
		return filter.FilterOutput{Content: original, Original: original}
	}

	var out []string
	for _, line := range lines {
		switch {
		// 文件身份行全留：后续定位需要
		case strings.HasPrefix(line, "diff --git"),
			strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "new file mode"),
			strings.HasPrefix(line, "deleted file mode"),
			strings.HasPrefix(line, "similarity index"),
			strings.HasPrefix(line, "rename from"),
			strings.HasPrefix(line, "rename to"),
			strings.HasPrefix(line, "--- "),
			strings.HasPrefix(line, "+++ "):
			out = append(out, line)
		// hunk 头：保留行号信息，LLM 需要用它来定位修改位置
		case strings.HasPrefix(line, "@@"):
			out = append(out, line)
		// 实际变更行（排除 --- +++ 这类，已在上面处理）
		case strings.HasPrefix(line, "+"), strings.HasPrefix(line, "-"):
			out = append(out, line)
			// context 行（以空格开头或空行）：丢弃
		}
	}

	joined := strings.Join(out, "\n")
	// 保留原文尾部换行（如果有），避免写回时少一行
	if strings.HasSuffix(original, "\n") && !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	return filter.FilterOutput{
		Content:  joined,
		Original: original,
	}
}

func (f *DiffFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

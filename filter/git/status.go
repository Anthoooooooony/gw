package git

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&StatusFilter{})
}

// StatusFilter 过滤 git status 输出，去除教学提示信息
type StatusFilter struct{}

func (f *StatusFilter) Name() string { return "git/status" }

func (f *StatusFilter) Match(cmd string, args []string) bool {
	return cmd == "git" && len(args) > 0 && args[0] == "status"
}

func (f *StatusFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	// `color.status=always` / `color.ui=always` 会给 modified/untracked 行加色码，
	// 影响 `(use "git ...` 提示行的前缀判定。先去色再过滤。
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	var filtered []string
	prevBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 跳过教学提示行
		if strings.HasPrefix(trimmed, "(use \"git") {
			continue
		}
		// 合并连续空行
		if trimmed == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		filtered = append(filtered, line)
	}

	joined := strings.Join(filtered, "\n")
	return filter.FilterOutput{
		Content:  joined,
		Original: original,
	}
}

func (f *StatusFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

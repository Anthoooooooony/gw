package shell

import (
	"strings"
)

// Segment 表示链式命令中的一个片段
type Segment struct {
	Cmd string // 命令内容（已 trim）
	Sep string // 该片段后面的分隔符，最后一个片段为 ""
}

// ShouldRewrite 判断命令是否可以被改写
// 包含管道 |、重定向 > >> <、子 shell $( 或反引号的命令不可改写
// 注意 || 是链式操作符，允许改写
func ShouldRewrite(command string) bool {
	// 将 || 替换为占位符，避免误判
	replaced := strings.ReplaceAll(command, "||", "\x00\x00")

	// 检查剩余的 | （管道）
	if strings.Contains(replaced, "|") {
		return false
	}

	// 检查重定向
	if strings.Contains(command, ">>") || strings.Contains(command, ">") || strings.Contains(command, "<") {
		return false
	}

	// 检查子 shell
	if strings.Contains(command, "$(") {
		return false
	}

	// 检查反引号
	if strings.Contains(command, "`") {
		return false
	}

	return true
}

// SplitChainedCommands 将链式命令按 &&、||、; 分割
func SplitChainedCommands(command string) []Segment {
	var segments []Segment
	remaining := command

	for len(remaining) > 0 {
		// 查找最近的分隔符
		minIdx := -1
		minSep := ""

		for _, sep := range []string{"&&", "||", ";"} {
			idx := strings.Index(remaining, sep)
			if idx >= 0 && (minIdx < 0 || idx < minIdx) {
				minIdx = idx
				minSep = sep
			}
		}

		if minIdx < 0 {
			// 没有更多分隔符
			segments = append(segments, Segment{
				Cmd: strings.TrimSpace(remaining),
				Sep: "",
			})
			break
		}

		segments = append(segments, Segment{
			Cmd: strings.TrimSpace(remaining[:minIdx]),
			Sep: minSep,
		})
		remaining = remaining[minIdx+len(minSep):]
	}

	return segments
}

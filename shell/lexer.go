package shell

import (
	"strings"
)

// Segment 表示链式命令中的一个片段
type Segment struct {
	Cmd string // 命令内容（已 trim）
	Sep string // 该片段后面的分隔符，最后一个片段为 ""
}

// AnalyzeCommand 对 shell 命令进行引号感知的单遍扫描，返回：
// - canRewrite: 命令是否可以安全改写（不含管道、重定向、子 shell 等）
// - segments: 按链式操作符（&&、||、;）分割的命令片段
// 引号内的特殊字符视为普通文本，不触发拒绝或分割。
func AnalyzeCommand(command string) (bool, []Segment) {
	runes := []rune(command)
	n := len(runes)

	var inQuote rune
	escaped := false

	segStart := 0
	var segments []Segment

	i := 0
	for i < n {
		c := runes[i]

		// 处理转义：上一个字符是 \，跳过当前字符
		if escaped {
			escaped = false
			i++
			continue
		}

		// 引号内：只关注关闭引号（双引号内还支持转义）
		if inQuote != 0 {
			if inQuote == '"' && c == '\\' {
				escaped = true
			} else if c == inQuote {
				inQuote = 0
			}
			i++
			continue
		}

		// 正常模式：检测特殊字符
		switch c {
		case '\\':
			escaped = true
			i++
			continue
		case '\'', '"':
			inQuote = c
			i++
			continue
		case '|':
			if i+1 < n && runes[i+1] == '|' {
				// || 链式操作符，安全
				segments = append(segments, Segment{
					Cmd: strings.TrimSpace(string(runes[segStart:i])),
					Sep: "||",
				})
				i += 2
				segStart = i
				continue
			}
			// 单独的 | 是管道，不安全
			return false, nil
		case '&':
			if i+1 < n && runes[i+1] == '&' {
				// && 链式操作符，安全
				segments = append(segments, Segment{
					Cmd: strings.TrimSpace(string(runes[segStart:i])),
					Sep: "&&",
				})
				i += 2
				segStart = i
				continue
			}
			// 单独的 & 是后台执行，不安全
			return false, nil
		case ';':
			segments = append(segments, Segment{
				Cmd: strings.TrimSpace(string(runes[segStart:i])),
				Sep: ";",
			})
			i++
			segStart = i
			continue
		case '>', '<':
			// 重定向，不安全
			return false, nil
		case '$':
			if i+1 < n && runes[i+1] == '(' {
				// 子 shell $()，不安全
				return false, nil
			}
			i++
			continue
		case '`':
			// 反引号子 shell，不安全
			return false, nil
		default:
			i++
			continue
		}
	}

	// 添加最后一个片段
	last := strings.TrimSpace(string(runes[segStart:]))
	if last != "" {
		segments = append(segments, Segment{Cmd: last, Sep: ""})
	}

	if len(segments) == 0 {
		return false, nil
	}

	return true, segments
}

// TokenizeSegment 把单段 shell 命令（由 AnalyzeCommand 保证无链式/管道/重定向）
// 按空白切成 token。引号内的空白保留在同一 token，双引号内支持反斜杠转义，
// 外层引号剥离。Token 里的中间引号（如 --grep='a b'）归入同一 token 且剥离引号。
//
// 典型结果：
//
//	"git commit -m \"fix: foo bar\""  → ["git", "commit", "-m", "fix: foo bar"]
//	"git log --grep='fix bar'"        → ["git", "log", "--grep=fix bar"]
//
// 与 strings.Fields 的差异在于不会把引号内的空白当分隔符。
func TokenizeSegment(s string) []string {
	var tokens []string
	var cur strings.Builder
	inToken := false
	var inQuote rune
	escaped := false

	flush := func() {
		if inToken {
			tokens = append(tokens, cur.String())
			cur.Reset()
			inToken = false
		}
	}

	for _, c := range s {
		if escaped {
			cur.WriteRune(c)
			escaped = false
			inToken = true
			continue
		}
		if inQuote != 0 {
			if inQuote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == inQuote {
				inQuote = 0
				continue
			}
			cur.WriteRune(c)
			continue
		}
		switch c {
		case '\\':
			escaped = true
			continue
		case '\'', '"':
			inQuote = c
			inToken = true
			continue
		case ' ', '\t', '\n':
			flush()
		default:
			cur.WriteRune(c)
			inToken = true
		}
	}
	flush()
	return tokens
}

// ShouldRewrite 判断命令是否可以被改写（向后兼容包装）
func ShouldRewrite(command string) bool {
	canRewrite, _ := AnalyzeCommand(command)
	return canRewrite
}

// SplitChainedCommands 将链式命令按 &&、||、; 分割（向后兼容包装）
func SplitChainedCommands(command string) []Segment {
	_, segments := AnalyzeCommand(command)
	return segments
}

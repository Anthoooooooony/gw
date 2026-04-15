package java

import (
	"strings"

	"github.com/gw-cli/gw/filter"
)

// MavenFilter 过滤 Maven 构建输出，压缩下载日志和插件执行信息
type MavenFilter struct{}

func (f *MavenFilter) Match(cmd string, args []string) bool {
	return cmd == "mvn"
}

func (f *MavenFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout + input.Stderr
	lines := strings.Split(original, "\n")

	var filtered []string
	for _, line := range lines {
		if isNoiseLine(line) {
			continue
		}
		filtered = append(filtered, line)
	}

	content := collapseBlankLines(filtered)
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *MavenFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout + input.Stderr
	lines := strings.Split(original, "\n")

	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 仍然去除下载日志和插件执行行
		if isDownloadLine(trimmed) || isPluginLine(trimmed) {
			continue
		}

		// 将 [ERROR] 前缀去除以提高可读性
		if strings.HasPrefix(trimmed, "[ERROR] ") {
			filtered = append(filtered, strings.TrimPrefix(trimmed, "[ERROR] "))
			continue
		}

		// 保留栈追踪行
		if isStackTraceLine(trimmed) {
			filtered = append(filtered, line)
			continue
		}

		// 保留 BUILD FAILURE、测试摘要、Total time 等关键行
		if isMavenKeyLine(trimmed) {
			filtered = append(filtered, line)
			continue
		}

		// 去除其他噪音行
		if isNoiseLine(line) {
			continue
		}

		filtered = append(filtered, line)
	}

	content := collapseBlankLines(filtered)
	return &filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

// isDownloadLine 判断是否为下载日志行
func isDownloadLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "[INFO] Downloading from") ||
		strings.HasPrefix(trimmed, "[INFO] Downloaded from")
}

// isPluginLine 判断是否为插件执行行
func isPluginLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "[INFO] --- maven-")
}

// isSeparatorLine 判断是否为分隔线（全是 - 和空格）
func isSeparatorLine(trimmed string) bool {
	if len(trimmed) < 10 {
		return false
	}
	// [INFO] ------...------
	inner := trimmed
	if strings.HasPrefix(inner, "[INFO] ") {
		inner = strings.TrimPrefix(inner, "[INFO] ")
	}
	for _, c := range inner {
		if c != '-' && c != ' ' {
			return false
		}
	}
	return true
}

// isNoiseLine 判断是否为噪音行（成功场景下需要去除）
func isNoiseLine(line string) bool {
	trimmed := strings.TrimSpace(line)

	if isDownloadLine(trimmed) {
		return true
	}
	if isPluginLine(trimmed) {
		return true
	}
	if isSeparatorLine(trimmed) {
		return true
	}
	// 空 [INFO] 行
	if trimmed == "[INFO]" {
		return true
	}

	noisePatterns := []string{
		"[INFO] Scanning for projects",
		"[INFO] Building ",
		"[INFO] Copying ",
		"[INFO] Compiling ",
		"[INFO] Nothing to compile",
		"[INFO] Using auto detected",
		"[INFO] Finished at:",
		"[INFO] Results:",
	}
	for _, p := range noisePatterns {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

// isStackTraceLine 判断是否为栈追踪行
func isStackTraceLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "at ") ||
		strings.HasPrefix(trimmed, "org.") ||
		strings.HasPrefix(trimmed, "java.")
}

// isMavenKeyLine 判断是否为关键信息行
func isMavenKeyLine(trimmed string) bool {
	return strings.Contains(trimmed, "BUILD FAILURE") ||
		strings.Contains(trimmed, "BUILD SUCCESS") ||
		strings.Contains(trimmed, "Tests run:") ||
		strings.Contains(trimmed, "Total time:")
}

// collapseBlankLines 合并连续空行
func collapseBlankLines(lines []string) string {
	var result []string
	prevBlank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

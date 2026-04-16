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
	seenErrors := make(map[string]bool) // 用于去重相同类型的错误

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 去除所有噪音行（WARNING、下载、插件、帮助建议等）
		if isNoiseLine(line) {
			continue
		}

		// [ERROR] 行：去前缀 + 去重
		if strings.HasPrefix(trimmed, "[ERROR] ") {
			errContent := strings.TrimPrefix(trimmed, "[ERROR] ")
			// 空 [ERROR] 行跳过
			if strings.TrimSpace(errContent) == "" {
				continue
			}
			// 对 Unresolved reference 类错误做去重（保留第一次 + 计数）
			if strings.Contains(errContent, "Unresolved reference") {
				// 提取引用名
				parts := strings.SplitN(errContent, "Unresolved reference", 2)
				if len(parts) == 2 {
					refKey := "unresolved:" + strings.TrimSpace(parts[1])
					if seenErrors[refKey] {
						continue
					}
					seenErrors[refKey] = true
				}
			}
			filtered = append(filtered, errContent)
			continue
		}

		// 保留栈追踪行
		if isStackTraceLine(trimmed) {
			filtered = append(filtered, line)
			continue
		}

		// 保留关键行
		if isMavenKeyLine(trimmed) {
			// 去掉 [INFO] 前缀
			clean := trimmed
			if strings.HasPrefix(clean, "[INFO] ") {
				clean = strings.TrimPrefix(clean, "[INFO] ")
			}
			filtered = append(filtered, clean)
			continue
		}

		// Reactor Summary 中的 SUCCESS/FAILURE 行保留
		if strings.HasPrefix(trimmed, "[INFO]") && (strings.Contains(trimmed, "SUCCESS") || strings.Contains(trimmed, "FAILURE")) && strings.Contains(trimmed, "...") {
			clean := strings.TrimPrefix(trimmed, "[INFO] ")
			filtered = append(filtered, clean)
			continue
		}
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

// isWarningNoise 判断 [WARNING] 行是否为噪音
func isWarningNoise(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "[WARNING]") {
		return false
	}
	// 依赖版本 WARNING（LATEST/RELEASE deprecated）
	if strings.Contains(trimmed, "LATEST or RELEASE") {
		return true
	}
	// systemPath WARNING
	if strings.Contains(trimmed, "systemPath") && strings.Contains(trimmed, "should not point at") {
		return true
	}
	// effective model WARNING 标题行
	if strings.Contains(trimmed, "Some problems were encountered while building the effective model") {
		return true
	}
	// 空 [WARNING] 行
	if trimmed == "[WARNING]" {
		return true
	}
	// Kotlin 参数名 WARNING
	if strings.Contains(trimmed, "file:///") && strings.Contains(trimmed, "The corresponding parameter in the supertype") {
		return true
	}
	// Kotlin 'open' has no effect WARNING
	if strings.Contains(trimmed, "file:///") && strings.Contains(trimmed, "has no effect on a final class") {
		return true
	}
	return false
}

// isReactorSkipped 判断 Reactor Summary 中的 SKIPPED 行
func isReactorSkipped(trimmed string) bool {
	return strings.HasPrefix(trimmed, "[INFO]") && strings.Contains(trimmed, "SKIPPED")
}

// isMavenHelpSuggestion 判断是否为 Maven 帮助建议行
func isMavenHelpSuggestion(trimmed string) bool {
	suggestions := []string{
		"To see the full stack trace",
		"Re-run Maven using the -X switch",
		"Re-run Maven using the -e switch",
		"For more information about the errors",
		"[Help 1]",
		"After correcting the problems, you can resume",
		"mvn <args> -rf :",
	}
	for _, s := range suggestions {
		if strings.Contains(trimmed, s) {
			return true
		}
	}
	return false
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
	if isWarningNoise(trimmed) {
		return true
	}
	if isReactorSkipped(trimmed) {
		return true
	}
	if isMavenHelpSuggestion(trimmed) {
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
		"[INFO] Using 'UTF-8'",
		"[INFO] Changes detected",
		"[INFO] skip non existing",
	}
	for _, p := range noisePatterns {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}

	// [INFO] --- *-plugin: 通用插件行
	if strings.HasPrefix(trimmed, "[INFO] --- ") && strings.Contains(trimmed, "-plugin:") {
		return true
	}
	// [INFO] --- *:*:* (kotlin-maven-plugin 等)
	if strings.HasPrefix(trimmed, "[INFO] --- ") {
		return true
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

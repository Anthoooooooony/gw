package java

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gw-cli/gw/filter"
)

func init() {
	filter.Register(&GradleFilter{})
}

// GradleFilter 过滤 Gradle 构建输出，压缩任务进度和守护进程启动信息
type GradleFilter struct{}

func (f *GradleFilter) Name() string { return "java/gradle" }

func (f *GradleFilter) Match(cmd string, args []string) bool {
	base := filepath.Base(cmd)
	return base == "gradle" || base == "gradlew"
}

// ansiRegexp 匹配 ANSI 转义序列
var ansiRegexp = regexp.MustCompile(`\x1B\[[0-9;]*[a-zA-Z]`)

// stripANSI 去除 ANSI 转义序列
func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

// javaCompileErrorRegexp 匹配 Java 编译错误行
var javaCompileErrorRegexp = regexp.MustCompile(`\.java:\d+: error:`)

// javaCompileWarningRegexp 匹配 Java 编译警告行
var javaCompileWarningRegexp = regexp.MustCompile(`\.java:\d+: warning:`)

func (f *GradleFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout + input.Stderr
	lines := strings.Split(original, "\n")

	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 只保留: BUILD SUCCESSFUL、actionable tasks 摘要、测试结果行
		if strings.HasPrefix(trimmed, "BUILD SUCCESSFUL") {
			filtered = append(filtered, line)
			continue
		}
		if strings.Contains(trimmed, "actionable task") {
			filtered = append(filtered, line)
			continue
		}
		if strings.Contains(trimmed, "tests completed") ||
			strings.Contains(trimmed, "PASSED") ||
			strings.Contains(trimmed, "FAILED") {
			filtered = append(filtered, line)
			continue
		}

		// 其他所有行丢弃
	}

	content := collapseBlankLines(filtered)
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *GradleFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout + input.Stderr
	lines := strings.Split(original, "\n")

	var filtered []string
	inWhatWrong := false
	inTrySection := false
	inException := false

	for _, line := range lines {
		cleaned := stripANSI(line)
		trimmed := strings.TrimSpace(cleaned)

		// --- 丢弃规则 ---

		// 普通 Task 进度行（不含 FAILED）
		if strings.HasPrefix(trimmed, "> Task :") && !strings.HasSuffix(trimmed, "FAILED") {
			continue
		}

		// Configure project 行
		if strings.HasPrefix(trimmed, "> Configure project :") {
			continue
		}

		// Starting Daemon 行
		if strings.HasPrefix(trimmed, "Starting a Gradle Daemon") {
			continue
		}

		// Kotlin 警告
		if strings.HasPrefix(trimmed, "w: file:///") {
			continue
		}

		// Java 编译警告
		if javaCompileWarningRegexp.MatchString(trimmed) {
			continue
		}

		// Configuration cache
		if strings.Contains(trimmed, "Calculating task graph as") {
			continue
		}

		// Build scan
		if strings.Contains(trimmed, "Publishing build scan") {
			continue
		}

		// Deprecation 警告
		if strings.Contains(trimmed, "has been deprecated") || strings.Contains(trimmed, "Deprecated Gradle features") {
			continue
		}

		// Progress 行
		if strings.Contains(trimmed, "% EXECUTING") {
			continue
		}

		// 报告链接（在 What went wrong 内容中也要过滤）
		if strings.Contains(trimmed, "See the report at:") {
			continue
		}

		// --- * Try: section 检测和过滤 ---
		if strings.HasPrefix(trimmed, "* Try:") {
			inTrySection = true
			inWhatWrong = false
			inException = false
			continue
		}
		if inTrySection {
			// Try section 持续到下一个 * section 或空行之后的非 > 行
			if strings.HasPrefix(trimmed, "> Run with --") || strings.HasPrefix(trimmed, "> Get more help") {
				continue
			}
			if trimmed == "" {
				inTrySection = false
				continue
			}
			continue
		}

		// --- * What went wrong: section 保留 ---
		if strings.HasPrefix(trimmed, "* What went wrong:") {
			inWhatWrong = true
			inException = false
			filtered = append(filtered, cleaned)
			continue
		}
		if inWhatWrong {
			if strings.HasPrefix(trimmed, "*") {
				inWhatWrong = false
				// 继续处理这行（可能是 * Try: 或 * Exception is:）
			} else if trimmed == "" {
				// What went wrong section 结束
				inWhatWrong = false
			} else {
				// 保留内容，但过滤报告链接（已在上面处理）
				filtered = append(filtered, cleaned)
				continue
			}
		}

		// --- * Exception is: section 保留 ---
		if strings.HasPrefix(trimmed, "* Exception is:") {
			inException = true
			filtered = append(filtered, cleaned)
			continue
		}
		if inException {
			if strings.HasPrefix(trimmed, "*") {
				inException = false
			} else if trimmed == "" {
				// 空行可能是 stack trace 中的分隔，暂不结束
				filtered = append(filtered, cleaned)
				continue
			} else {
				filtered = append(filtered, cleaned)
				continue
			}
		}

		// --- 保留规则 ---

		// FAILED 任务行
		if strings.HasPrefix(trimmed, "> Task :") && strings.HasSuffix(trimmed, "FAILED") {
			filtered = append(filtered, cleaned)
			continue
		}

		// BUILD FAILED
		if strings.HasPrefix(trimmed, "BUILD FAILED") {
			filtered = append(filtered, cleaned)
			continue
		}

		// FAILURE: 行
		if strings.HasPrefix(trimmed, "FAILURE:") {
			filtered = append(filtered, cleaned)
			continue
		}

		// actionable tasks 摘要
		if strings.Contains(trimmed, "actionable task") {
			filtered = append(filtered, cleaned)
			continue
		}

		// 测试失败详情: FAILED 行
		if strings.Contains(trimmed, "FAILED") {
			filtered = append(filtered, cleaned)
			continue
		}

		// 测试结果摘要: tests completed
		if strings.Contains(trimmed, "tests completed") {
			filtered = append(filtered, cleaned)
			continue
		}

		// 测试通过行
		if strings.Contains(trimmed, "PASSED") {
			filtered = append(filtered, cleaned)
			continue
		}

		// assertion 错误详情（缩进行，包含断言信息）
		if strings.HasPrefix(line, "    ") && (strings.Contains(trimmed, "Error") ||
			strings.Contains(trimmed, "Exception") ||
			strings.Contains(trimmed, "expected:") ||
			strings.Contains(trimmed, "assert")) {
			filtered = append(filtered, cleaned)
			continue
		}

		// Stack trace 行
		if strings.HasPrefix(trimmed, "at ") {
			filtered = append(filtered, cleaned)
			continue
		}

		// Kotlin 编译错误
		if strings.HasPrefix(trimmed, "e: file:///") {
			filtered = append(filtered, cleaned)
			continue
		}

		// Java 编译错误
		if javaCompileErrorRegexp.MatchString(trimmed) {
			filtered = append(filtered, cleaned)
			continue
		}

		// 其他行丢弃
	}

	content := collapseBlankLines(filtered)
	return &filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

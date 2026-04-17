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

// longRunningGradleTasks 是可能导致长驻进程的 Gradle task
var longRunningGradleTasks = []string{
	"bootRun",
	"run",
	"appRun",
	"jettyRun",
	"tomcatRun",
	"quarkusDev",
}

func (f *GradleFilter) Match(cmd string, args []string) bool {
	base := filepath.Base(cmd)
	if base != "gradle" && base != "gradlew" {
		return false
	}
	// 排除长驻进程的 task
	for _, arg := range args {
		for _, task := range longRunningGradleTasks {
			if arg == task {
				return false
			}
		}
	}
	return true
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

// --- 流式过滤器 ---
//
// GradleStreamProcessor 实现 filter.StreamProcessor，逐行解析 Gradle 构建输出。
// 状态机维护当前是否处于 What went wrong / Exception is / Try 块，以及已发射
// 行集合用于去重（同一 deprecation/警告反复出现时只保留首次）。
//
// 与批量模式 ApplyOnError 共享辅助正则与判断函数，但状态机结构完全独立，
// 不影响批量模式语义。
type gradleSection int

const (
	gradleSectionNormal    gradleSection = iota // 普通输出
	gradleSectionWhatWrong                      // 在 * What went wrong: 块
	gradleSectionException                      // 在 * Exception is: 块（含栈帧）
	gradleSectionTry                            // 在 * Try: 块（整段丢弃）
)

// gradleNoiseRegexps 是在任何状态下都直接丢弃的噪音行模式。
// 灵感来自 rtk gradle.toml 的 strip_lines_matching 配置。
var gradleNoiseRegexps = []*regexp.Regexp{
	regexp.MustCompile(`^> Configure project`),
	regexp.MustCompile(`^> Configuring project`),
	regexp.MustCompile(`^> Resolving dependencies`),
	regexp.MustCompile(`^> Transform `),
	regexp.MustCompile(`^Download(ing)?\s+http`),
	regexp.MustCompile(`^\s*<-+>\s`), // 进度条 <===>
	regexp.MustCompile(`^Starting a Gradle Daemon`),
	regexp.MustCompile(`^Daemon will be stopped`),
	regexp.MustCompile(`^To honour the JVM settings`),
	regexp.MustCompile(`^\[Incubating\]`),
	regexp.MustCompile(`^Publishing build scan`),
	regexp.MustCompile(`% EXECUTING`),
	regexp.MustCompile(`Calculating task graph as`),
	regexp.MustCompile(`See the report at:`),
}

// gradleStackFrameRegexp 匹配栈帧行（限制深度时使用）。
var gradleStackFrameRegexp = regexp.MustCompile(`^\s+at\s`)

// gradleMaxStackFrames 是单个错误块最多保留的栈帧数，避免日志爆炸。
const gradleMaxStackFrames = 20

// NewStreamInstance 创建新的流式处理器实例（每次命令执行一份状态）。
func (f *GradleFilter) NewStreamInstance() filter.StreamProcessor {
	return &GradleStreamProcessor{
		section:     gradleSectionNormal,
		seenLines:   make(map[string]bool),
		emittedAny:  false,
		stackFrames: 0,
	}
}

// GradleStreamProcessor 是 GradleFilter 的流式状态机。
type GradleStreamProcessor struct {
	section     gradleSection   // 当前所处的输出段
	seenLines   map[string]bool // 已发射的行内容集合，用于去重 deprecation 等重复警告
	emittedAny  bool            // 本次执行是否已经发出过任何输出
	stackFrames int             // 当前错误块内已保留的栈帧数
	taskCount   int             // 已观察到的成功 Task 数（Flush 时用于摘要）
}

// ProcessLine 处理单行输出，返回是否发射以及发射内容。
func (p *GradleStreamProcessor) ProcessLine(line string) (filter.StreamAction, string) {
	cleaned := stripANSI(line)
	trimmed := strings.TrimSpace(cleaned)

	// 1. 段落边界优先识别（必须在噪音判断前，避免段开头被噪音规则误杀）
	switch {
	case strings.HasPrefix(trimmed, "* What went wrong:"):
		p.section = gradleSectionWhatWrong
		p.stackFrames = 0
		return p.emit(cleaned)
	case strings.HasPrefix(trimmed, "* Try:"):
		// 进入 Try 段后整段丢弃，直到下一个 * 段或空行
		p.section = gradleSectionTry
		return filter.StreamDrop, ""
	case strings.HasPrefix(trimmed, "* Exception is:"):
		p.section = gradleSectionException
		p.stackFrames = 0
		return p.emit(cleaned)
	}

	// 2. 段内逻辑
	switch p.section {
	case gradleSectionTry:
		// Try 段：整段丢弃；遇到下一个 * 段（已在上面处理）或空行结束
		if trimmed == "" {
			p.section = gradleSectionNormal
		}
		return filter.StreamDrop, ""

	case gradleSectionWhatWrong:
		// What went wrong 段：保留所有非空行直到遇到下一个 * 段或空行
		if trimmed == "" {
			p.section = gradleSectionNormal
			return filter.StreamDrop, ""
		}
		// 报告链接在任何段内都丢弃
		if strings.Contains(trimmed, "See the report at:") {
			return filter.StreamDrop, ""
		}
		return p.emit(cleaned)

	case gradleSectionException:
		// Exception 段：保留正文，栈帧限制深度
		if trimmed == "" {
			// 空行可能是栈帧之间的分隔，保留但不结束段
			return p.emit(cleaned)
		}
		if gradleStackFrameRegexp.MatchString(cleaned) {
			p.stackFrames++
			if p.stackFrames > gradleMaxStackFrames {
				return filter.StreamDrop, ""
			}
			return p.emit(cleaned)
		}
		return p.emit(cleaned)
	}

	// 3. normal 段：先丢噪音
	for _, re := range gradleNoiseRegexps {
		if re.MatchString(trimmed) {
			return filter.StreamDrop, ""
		}
	}

	// Kotlin 警告（w: file:///...）
	if strings.HasPrefix(trimmed, "w: file:///") {
		return filter.StreamDrop, ""
	}
	// Java 编译警告
	if javaCompileWarningRegexp.MatchString(trimmed) {
		return filter.StreamDrop, ""
	}
	// 单行 deprecation 提示（不在 What went wrong 内）
	if strings.Contains(trimmed, "has been deprecated") ||
		strings.Contains(trimmed, "Deprecated Gradle features") {
		return filter.StreamDrop, ""
	}

	// 空行：丢弃，避免输出端连续空行
	if trimmed == "" {
		return filter.StreamDrop, ""
	}

	// 4. normal 段保留规则
	// > Task :xxx FAILED
	if strings.HasPrefix(trimmed, "> Task :") && strings.HasSuffix(trimmed, "FAILED") {
		return p.emit(cleaned)
	}
	// > Task :xxx UP-TO-DATE / NO-SOURCE / FROM-CACHE / SKIPPED
	if strings.HasPrefix(trimmed, "> Task :") {
		// 带状态后缀的 task 进度行丢弃
		for _, suffix := range []string{"UP-TO-DATE", "NO-SOURCE", "FROM-CACHE", "SKIPPED"} {
			if strings.HasSuffix(trimmed, suffix) {
				return filter.StreamDrop, ""
			}
		}
		// 无后缀的成功 task 也丢弃，仅累计计数用于 Flush
		p.taskCount++
		return filter.StreamDrop, ""
	}

	// BUILD SUCCESSFUL / BUILD FAILED
	if strings.HasPrefix(trimmed, "BUILD SUCCESSFUL") || strings.HasPrefix(trimmed, "BUILD FAILED") {
		return p.emit(cleaned)
	}
	// FAILURE: 行
	if strings.HasPrefix(trimmed, "FAILURE:") {
		return p.emit(cleaned)
	}
	// actionable tasks 摘要
	if strings.Contains(trimmed, "actionable task") {
		return p.emit(cleaned)
	}
	// 测试摘要 / 单测结果
	if strings.Contains(trimmed, "tests completed") {
		return p.emit(cleaned)
	}
	if strings.Contains(trimmed, "PASSED") {
		return p.emit(cleaned)
	}
	if strings.Contains(trimmed, "FAILED") {
		return p.emit(cleaned)
	}
	// 测试断言详情（缩进且包含错误关键字）
	if strings.HasPrefix(line, "    ") && (strings.Contains(trimmed, "Error") ||
		strings.Contains(trimmed, "Exception") ||
		strings.Contains(trimmed, "expected:") ||
		strings.Contains(trimmed, "assert")) {
		return p.emit(cleaned)
	}
	// 栈帧（normal 段也保留，常见于测试断言失败的栈追踪）
	if strings.HasPrefix(trimmed, "at ") {
		return p.emit(cleaned)
	}
	// Kotlin 编译错误
	if strings.HasPrefix(trimmed, "e: file:///") {
		return p.emit(cleaned)
	}
	// Java 编译错误
	if javaCompileErrorRegexp.MatchString(trimmed) {
		return p.emit(cleaned)
	}

	return filter.StreamDrop, ""
}

// emit 是统一的发射出口：负责去重已经输出过的整行内容，并打 emittedAny 标记。
// 缩进栈帧、空行不参与去重（它们的重复是正常结构）。
func (p *GradleStreamProcessor) emit(line string) (filter.StreamAction, string) {
	trimmed := strings.TrimSpace(line)
	// 空行与栈帧不去重
	if trimmed != "" && !gradleStackFrameRegexp.MatchString(line) {
		if p.seenLines[trimmed] {
			return filter.StreamDrop, ""
		}
		p.seenLines[trimmed] = true
	}
	p.emittedAny = true
	return filter.StreamEmit, line
}

// Flush 在命令结束时调用：成功且无任何输出时补一行摘要，避免 LLM 收到空内容。
func (p *GradleStreamProcessor) Flush(exitCode int) []string {
	if exitCode == 0 && !p.emittedAny {
		if p.taskCount > 0 {
			return []string{"gw: gradle build ok (no notable output)"}
		}
		return []string{"gw: gradle build ok"}
	}
	return nil
}

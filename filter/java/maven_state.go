package java

import "strings"

// MavenState 表示 Maven 构建生命周期中的状态
type MavenState int

const (
	StateInit        MavenState = iota // 初始状态
	StateDiscovery                     // 项目扫描阶段
	StateWarning                       // POM 警告阶段
	StateModuleBuild                   // 模块构建阶段
	StateMojo                          // 插件目标执行阶段
	StatePluginOutput                  // 插件输出阶段
	StateTestOutput                    // 测试输出阶段
	StateReactor                       // Reactor Summary 阶段
	StateResult                        // 构建结果阶段
	StateStats                         // 统计信息阶段
	StateErrorReport                   // 错误报告阶段
)

// MavenLineClass 表示 Maven 输出行的分类
type MavenLineClass int

const (
	LineDiscovery       MavenLineClass = iota // [INFO] Scanning for projects
	LineModuleHeader                          // [INFO] Building xxx
	LineMojoHeader                            // [INFO] --- plugin:ver:goal ---
	LineTransfer                              // [INFO] Downloading/Downloaded from
	LinePomWarning                            // [WARNING] 非编译器警告
	LineCompilerWarning                       // [WARNING] file:/// 编译器警告
	LineReactorHeader                         // [INFO] Reactor Summary
	LineReactorEntry                          // [INFO] xxx ... SUCCESS/FAILURE/SKIPPED [time]
	LineBuildResult                           // [INFO] BUILD SUCCESS/FAILURE
	LineStats                                 // [INFO] Total time:
	LineFinishedAt                            // [INFO] Finished at:
	LineSeparator                             // [INFO] --------...--------
	LineError                                 // [ERROR] 普通错误
	LineTestHeader                            // [INFO] T E S T S
	LineTestSummary                           // Tests run: 结果摘要
	LineTestRunning                           // [INFO] Running com.xxx
	LineStackTrace                            // at / org. / java. 栈追踪
	LineHelpSuggestion                        // Maven 帮助建议行
	LineEmpty                                 // 空行
	LineProcessNoise                          // 编译、拷贝等过程噪音
	LinePluginOutput                          // 其他插件输出
)

// classifyLine 对单行 Maven 输出进行分类
func classifyLine(line string) MavenLineClass {
	trimmed := strings.TrimSpace(line)

	// 空行判断
	if trimmed == "" || trimmed == "[INFO]" || trimmed == "[ERROR]" || trimmed == "[WARNING]" {
		return LineEmpty
	}

	// [ERROR] 行
	if strings.HasPrefix(trimmed, "[ERROR]") {
		content := strings.TrimSpace(strings.TrimPrefix(trimmed, "[ERROR]"))

		// 帮助建议
		if isHelpSuggestionContent(content) {
			return LineHelpSuggestion
		}
		// 测试摘要
		if strings.Contains(content, "Tests run:") {
			return LineTestSummary
		}
		return LineError
	}

	// [WARNING] 行
	if strings.HasPrefix(trimmed, "[WARNING]") {
		if strings.Contains(trimmed, "file:///") {
			return LineCompilerWarning
		}
		return LinePomWarning
	}

	// [INFO] 行
	if strings.HasPrefix(trimmed, "[INFO]") {
		content := strings.TrimSpace(strings.TrimPrefix(trimmed, "[INFO]"))

		// 分隔线
		if isSeparatorContent(content) {
			return LineSeparator
		}

		if strings.HasPrefix(content, "Scanning for projects") {
			return LineDiscovery
		}
		if strings.HasPrefix(content, "Building ") {
			return LineModuleHeader
		}
		if strings.HasPrefix(content, "--- ") {
			return LineMojoHeader
		}
		if strings.HasPrefix(content, "Downloading from") || strings.HasPrefix(content, "Downloaded from") {
			return LineTransfer
		}
		if strings.HasPrefix(content, "Reactor Summary") {
			return LineReactorHeader
		}
		// Reactor 条目: 包含 SUCCESS/FAILURE/SKIPPED 且有 ...
		if strings.Contains(content, "...") && (strings.Contains(content, "SUCCESS") || strings.Contains(content, "FAILURE") || strings.Contains(content, "SKIPPED")) {
			return LineReactorEntry
		}
		if strings.HasPrefix(content, "BUILD SUCCESS") || strings.HasPrefix(content, "BUILD FAILURE") {
			return LineBuildResult
		}
		if strings.HasPrefix(content, "Total time:") {
			return LineStats
		}
		if strings.HasPrefix(content, "Finished at:") {
			return LineFinishedAt
		}
		if content == "T E S T S" {
			return LineTestHeader
		}
		if strings.HasPrefix(content, "Tests run:") {
			return LineTestSummary
		}
		if strings.HasPrefix(content, "Running ") {
			return LineTestRunning
		}

		// 过程噪音
		if isProcessNoiseContent(content) {
			return LineProcessNoise
		}

		return LinePluginOutput
	}

	// 无前缀行: 下载/进度（部分 Maven 版本不带 [INFO] 前缀）
	if strings.HasPrefix(trimmed, "Downloading from") || strings.HasPrefix(trimmed, "Downloaded from") || strings.HasPrefix(trimmed, "Progress (") {
		return LineTransfer
	}

	// 无前缀行: 栈追踪
	if strings.HasPrefix(trimmed, "at ") || strings.HasPrefix(trimmed, "org.") || strings.HasPrefix(trimmed, "java.") {
		return LineStackTrace
	}

	return LinePluginOutput
}

// isHelpSuggestionContent 判断 [ERROR] 后的内容是否为帮助建议
func isHelpSuggestionContent(content string) bool {
	// 保留恢复构建命令（mvn <args> -rf :module），对 AI 有操作价值
	if strings.Contains(content, "-rf :") {
		return false
	}
	suggestions := []string{
		"To see the full stack trace",
		"Re-run Maven",
		"[Help 1]",
		"After correcting",
		"For more information about the errors",
	}
	for _, s := range suggestions {
		if strings.Contains(content, s) {
			return true
		}
	}
	return false
}

// isSeparatorContent 判断 [INFO] 后的内容是否为分隔线
// 匹配纯 dash 线和包含项目坐标/包类型的分隔线
func isSeparatorContent(content string) bool {
	if len(content) < 10 {
		return false
	}
	// 统计 dash 字符占比
	dashCount := 0
	for _, c := range content {
		if c == '-' {
			dashCount++
		}
	}
	// 如果 dash 占 60% 以上且总长度足够，认为是分隔线
	if float64(dashCount)/float64(len(content)) > 0.6 {
		return true
	}
	return false
}

// isProcessNoiseContent 判断 [INFO] 后的内容是否为过程噪音
func isProcessNoiseContent(content string) bool {
	noises := []string{
		"Compiling ",
		"Nothing to compile",
		"No sources to compile",
		"Copying ",
		"Using '",
		"Changes detected",
		"skip non existing",
		"Using auto detected",
		"Applied plugin:",
		"Results:",
	}
	for _, n := range noises {
		if strings.HasPrefix(content, n) {
			return true
		}
	}
	// javac 编译器告警（deprecation / unchecked）
	if (strings.Contains(content, "uses or overrides a deprecated API") ||
		strings.Contains(content, "use or override a deprecated API") ||
		strings.Contains(content, "Some input files use or override a deprecated API") ||
		strings.Contains(content, "Recompile with -Xlint:") ||
		strings.Contains(content, "uses unchecked or unsafe operations") ||
		strings.Contains(content, "use unchecked or unsafe operations") ||
		strings.Contains(content, "Some input files use unchecked or unsafe operations")) {
		return true
	}
	return false
}

// nextState 根据当前状态和行分类计算下一个状态
func nextState(current MavenState, lc MavenLineClass) MavenState {
	// 全局转移（任意状态）
	switch lc {
	case LineModuleHeader:
		return StateModuleBuild
	case LineMojoHeader:
		return StateMojo
	case LineReactorHeader:
		return StateReactor
	case LineBuildResult:
		return StateResult
	case LineStats:
		return StateStats
	}

	// 按状态分别处理
	switch current {
	case StateInit:
		if lc == LineDiscovery {
			return StateDiscovery
		}

	case StateDiscovery:
		if lc == LinePomWarning {
			return StateWarning
		}

	case StateWarning:
		if lc == LinePomWarning || lc == LineEmpty {
			return StateWarning
		}

	case StateModuleBuild:
		// MojoHeader 已在全局转移中处理

	case StateMojo:
		// 非 MojoHeader 的任何行进入 PluginOutput
		return StatePluginOutput

	case StatePluginOutput:
		// MojoHeader 已在全局转移中处理
		if lc == LineTestHeader {
			return StateTestOutput
		}

	case StateTestOutput:
		// MojoHeader 已在全局转移中处理

	case StateResult:
		if lc == LineError {
			return StateErrorReport
		}
	}

	return current
}

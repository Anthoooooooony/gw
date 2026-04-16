# Maven State Machine Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用状态机重写 MavenFilter，基于 Maven 源码（`ExecutionEventLogger.java`）的事件模型精确追踪构建阶段，替代当前的逐行正则匹配，在真实 大型 Java 产线 项目（905行输出）上实现 95%+ 压缩率。

**Architecture:** 定义 `MavenState` 枚举表示构建流程的各阶段，逐行扫描时根据固定标记行驱动状态转移，每个状态有独立的保留/丢弃策略。成功和失败共用同一个状态机，仅在保留策略上有差异（失败时保留错误详情）。

**Tech Stack:** Go, 无新依赖

---

## File Structure

```
filter/java/
├── maven.go           # 重写：状态机实现 + Match/Apply/ApplyOnError
├── maven_state.go     # 新增：MavenState 枚举 + 状态转移逻辑 + 行分类
├── maven_state_test.go# 新增：状态机单元测试（状态转移、行分类）
├── maven_test.go      # 修改：增加真实 大型 Java 产线 fixture 测试
├── gradle.go          # 不动
├── springboot.go      # 不动
testdata/
├── mvn_test_success.txt          # 已有
├── mvn_test_failure.txt          # 已有
├── mvn_compile_real_failure.txt  # 新增：905行 大型 Java 产线 真实输出
```

职责分离：
- `maven_state.go` — 纯状态机逻辑（状态定义、转移函数、行分类），可独立测试
- `maven.go` — Filter 接口实现，调用状态机处理，保持 Match/Apply/ApplyOnError 签名不变

---

### Task 1: 定义状态机和行分类器

**Files:**
- Create: `filter/java/maven_state.go`
- Create: `filter/java/maven_state_test.go`

- [ ] **Step 1: 编写状态转移测试**

```go
// filter/java/maven_state_test.go
package java

import "testing"

func TestClassifyLine(t *testing.T) {
	tests := []struct {
		line string
		want MavenLineClass
	}{
		{"[INFO] Scanning for projects...", LineDiscovery},
		{"[INFO] Building myapp 1.0.0", LineModuleHeader},
		{"[INFO] Building myapp 1.0.0 [3/20]", LineModuleHeader},
		{"[INFO] --- maven-compiler-plugin:3.10.1:compile (default-compile) @ myapp ---", LineMojoHeader},
		{"[INFO] --- kotlin-maven-plugin:2.2.0:compile (compile) @ core-key-business ---", LineMojoHeader},
		{"[INFO] Downloading from central: https://repo.maven.apache.org/...", LineTransfer},
		{"[INFO] Downloaded from central: https://repo.maven.apache.org/...", LineTransfer},
		{"[WARNING] 'dependencies.dependency.version' for ISFJ:ISFJ:jar is either LATEST or RELEASE", LinePomWarning},
		{"[WARNING] Some problems were encountered while building the effective model", LinePomWarning},
		{"[WARNING]", LinePomWarning},
		{"[WARNING] file:///app/src/Foo.kt:42:5 The corresponding parameter in the supertype", LineCompilerWarning},
		{"[WARNING] file:///app/src/Foo.kt:10:14 'open' has no effect on a final class.", LineCompilerWarning},
		{"[INFO] Reactor Summary for myapp 1.0.0-SNAPSHOT:", LineReactorHeader},
		{"[INFO] Reactor Summary:", LineReactorHeader},
		{"[INFO] myapp ...................................... SUCCESS [  1.234 s]", LineReactorEntry},
		{"[INFO] myapp ...................................... FAILURE [  1.234 s]", LineReactorEntry},
		{"[INFO] myapp ...................................... SKIPPED", LineReactorEntry},
		{"[INFO] BUILD SUCCESS", LineBuildResult},
		{"[INFO] BUILD FAILURE", LineBuildResult},
		{"[INFO] Total time:  02:27 min", LineStats},
		{"[INFO] Finished at: 2026-04-16T02:09:51Z", LineFinishedAt},
		{"[ERROR] Failed to execute goal org.jetbrains.kotlin:kotlin-maven-plugin", LineError},
		{"[ERROR] file:///app/src/Foo.kt:8:52 Unresolved reference 'BusinessLog'.", LineError},
		{"[ERROR]", LineEmpty},
		{"[ERROR] To see the full stack trace of the errors, re-run Maven with the -e switch.", LineHelpSuggestion},
		{"[ERROR] Re-run Maven using the -X switch to enable full debug logging.", LineHelpSuggestion},
		{"[ERROR] [Help 1] http://cwiki.apache.org/confluence/display/MAVEN/MojoFailureException", LineHelpSuggestion},
		{"[ERROR] After correcting the problems, you can resume the build with the command", LineHelpSuggestion},
		{"[ERROR]   mvn <args> -rf :core-key-business", LineHelpSuggestion},
		{"[INFO] ------------------------------------------------------------------------", LineSeparator},
		{"[INFO]", LineEmpty},
		{"[INFO] Compiling 12 source files to /home/user/myapp/target/classes", LineProcessNoise},
		{"[INFO] Nothing to compile - all classes are up to date", LineProcessNoise},
		{"[INFO] Copying 3 resources", LineProcessNoise},
		{"[INFO] Using 'UTF-8' encoding to copy filtered resources.", LineProcessNoise},
		{"[INFO] Changes detected - recompiling the module!", LineProcessNoise},
		{"[INFO] skip non existing resourceDirectory", LineProcessNoise},
		{"[INFO]  T E S T S", LineTestHeader},
		{"[INFO] Tests run: 46, Failures: 0, Errors: 0, Skipped: 1", LineTestSummary},
		{"[ERROR] Tests run: 12, Failures: 2, Errors: 0, Skipped: 0", LineTestSummary},
		{"[INFO] Running com.example.UserServiceTest", LineTestRunning},
		{"at org.junit.jupiter.api.AssertionUtils.fail(AssertionUtils.java:55)", LineStackTrace},
		{"org.opentest4j.AssertionFailedError: expected: <200> but was: <401>", LineStackTrace},
		{"Some random plugin output line", LinePluginOutput},
	}
	for _, tt := range tests {
		got := classifyLine(tt.line)
		if got != tt.want {
			t.Errorf("classifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestStateTransition(t *testing.T) {
	tests := []struct {
		current MavenState
		lineClass MavenLineClass
		want    MavenState
	}{
		{StateInit, LineDiscovery, StateDiscovery},
		{StateDiscovery, LinePomWarning, StateWarning},
		{StateWarning, LinePomWarning, StateWarning},
		{StateWarning, LineModuleHeader, StateModuleBuild},
		{StateInit, LineModuleHeader, StateModuleBuild},
		{StateModuleBuild, LineMojoHeader, StateMojo},
		{StateMojo, LinePluginOutput, StatePluginOutput},
		{StateMojo, LineCompilerWarning, StatePluginOutput},
		{StatePluginOutput, LineMojoHeader, StateMojo},
		{StatePluginOutput, LineModuleHeader, StateModuleBuild},
		{StatePluginOutput, LineTestHeader, StateTestOutput},
		{StateTestOutput, LineTestRunning, StateTestOutput},
		{StateTestOutput, LineModuleHeader, StateModuleBuild},
		{StateModuleBuild, LineReactorHeader, StateReactor},
		{StatePluginOutput, LineReactorHeader, StateReactor},
		{StateReactor, LineBuildResult, StateResult},
		{StateResult, LineStats, StateStats},
		{StateResult, LineError, StateErrorReport},
		{StateErrorReport, LineStats, StateStats},
	}
	for _, tt := range tests {
		got := nextState(tt.current, tt.lineClass)
		if got != tt.want {
			t.Errorf("nextState(%v, %v) = %v, want %v", tt.current, tt.lineClass, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run "TestClassifyLine|TestStateTransition"
```

Expected: FAIL — types 未定义

- [ ] **Step 3: 实现状态定义和行分类器**

```go
// filter/java/maven_state.go
package java

import "strings"

// MavenState 表示 Maven 构建输出的当前阶段
type MavenState int

const (
	StateInit         MavenState = iota // 初始状态
	StateDiscovery                      // "Scanning for projects" 后
	StateWarning                        // POM [WARNING] 块
	StateModuleBuild                    // "Building xxx" 后
	StateMojo                           // "--- plugin:ver:goal ---" 后
	StatePluginOutput                   // Mojo 之后的插件自由文本
	StateTestOutput                     // "T E S T S" 后的测试输出
	StateReactor                        // "Reactor Summary" 后
	StateResult                         // "BUILD SUCCESS/FAILURE" 后
	StateStats                          // "Total time:" 后
	StateErrorReport                    // 构建失败后的 [ERROR] 报告
)

// MavenLineClass 表示一行的语义分类
type MavenLineClass int

const (
	LineDiscovery       MavenLineClass = iota // [INFO] Scanning for projects
	LineModuleHeader                          // [INFO] Building xxx
	LineMojoHeader                            // [INFO] --- plugin:ver:goal ---
	LineTransfer                              // Downloading/Downloaded from
	LinePomWarning                            // POM 模型校验 [WARNING]
	LineCompilerWarning                       // 编译器 [WARNING]（file:/// 开头）
	LineReactorHeader                         // [INFO] Reactor Summary
	LineReactorEntry                          // [INFO] module ... SUCCESS/FAILURE/SKIPPED
	LineBuildResult                           // [INFO] BUILD SUCCESS / BUILD FAILURE
	LineStats                                 // [INFO] Total time:
	LineFinishedAt                            // [INFO] Finished at:
	LineSeparator                             // [INFO] --------...--------
	LineError                                 // [ERROR] 编译/运行错误
	LineTestHeader                            // [INFO]  T E S T S
	LineTestSummary                           // Tests run: X, Failures: Y
	LineTestRunning                           // [INFO] Running com.example.XxxTest
	LineStackTrace                            // at org.xxx / org.opentest4j.xxx
	LineHelpSuggestion                        // Maven 帮助建议
	LineEmpty                                 // 空行或空 [INFO]/[ERROR]
	LineProcessNoise                          // Compiling/Copying/Nothing to compile 等过程行
	LinePluginOutput                          // 无法分类的插件输出
)

// classifyLine 对单行进行语义分类
func classifyLine(line string) MavenLineClass {
	trimmed := strings.TrimSpace(line)

	// 空行
	if trimmed == "" || trimmed == "[INFO]" || trimmed == "[ERROR]" || trimmed == "[WARNING]" {
		return LineEmpty
	}

	// 分隔线：[INFO] 后全是 - 和空格，长度 > 10
	if strings.HasPrefix(trimmed, "[INFO]") {
		inner := strings.TrimSpace(strings.TrimPrefix(trimmed, "[INFO]"))
		if len(inner) > 10 && isAllDashes(inner) {
			return LineSeparator
		}
	}

	// [ERROR] 行细分
	if strings.HasPrefix(trimmed, "[ERROR]") {
		return classifyErrorLine(trimmed)
	}

	// [WARNING] 行细分
	if strings.HasPrefix(trimmed, "[WARNING]") {
		return classifyWarningLine(trimmed)
	}

	// 栈追踪行（无前缀）
	if strings.HasPrefix(trimmed, "at ") ||
		strings.HasPrefix(trimmed, "org.") ||
		strings.HasPrefix(trimmed, "java.") {
		return LineStackTrace
	}

	// [INFO] 行细分
	if strings.HasPrefix(trimmed, "[INFO]") {
		return classifyInfoLine(trimmed)
	}

	// 其他（插件自由文本、测试输出行等）
	return LinePluginOutput
}

func classifyInfoLine(trimmed string) MavenLineClass {
	content := strings.TrimSpace(strings.TrimPrefix(trimmed, "[INFO]"))

	// 固定标记行（来自 ExecutionEventLogger.java）
	if strings.HasPrefix(content, "Scanning for projects") {
		return LineDiscovery
	}
	if strings.HasPrefix(content, "Building ") {
		return LineModuleHeader
	}
	if strings.HasPrefix(content, "--- ") {
		return LineMojoHeader
	}
	if strings.HasPrefix(content, "Reactor Summary") {
		return LineReactorHeader
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

	// 传输行
	if strings.HasPrefix(content, "Downloading from") || strings.HasPrefix(content, "Downloaded from") {
		return LineTransfer
	}

	// 测试相关
	if strings.Contains(content, "T E S T S") {
		return LineTestHeader
	}
	if strings.HasPrefix(content, "Tests run:") || strings.HasPrefix(content, "Results:") {
		return LineTestSummary
	}
	if strings.HasPrefix(content, "Running ") {
		return LineTestRunning
	}

	// Reactor 条目：包含 SUCCESS/FAILURE/SKIPPED 且有 ... 对齐
	if strings.Contains(content, "...") &&
		(strings.Contains(content, "SUCCESS") || strings.Contains(content, "FAILURE") || strings.Contains(content, "SKIPPED")) {
		return LineReactorEntry
	}

	// 过程噪音
	processNoisePatterns := []string{
		"Compiling ", "Nothing to compile", "Copying ", "Using '",
		"Changes detected", "skip non existing", "Using auto detected",
	}
	for _, p := range processNoisePatterns {
		if strings.HasPrefix(content, p) {
			return LineProcessNoise
		}
	}

	return LinePluginOutput
}

func classifyErrorLine(trimmed string) MavenLineClass {
	content := strings.TrimSpace(strings.TrimPrefix(trimmed, "[ERROR]"))

	// 帮助建议
	helpPatterns := []string{
		"To see the full stack trace",
		"Re-run Maven using",
		"For more information about the errors",
		"[Help 1]",
		"After correcting the problems",
		"mvn <args> -rf :",
	}
	for _, p := range helpPatterns {
		if strings.Contains(content, p) {
			return LineHelpSuggestion
		}
	}

	// 测试摘要（可能以 [ERROR] 前缀出现）
	if strings.HasPrefix(content, "Tests run:") {
		return LineTestSummary
	}

	return LineError
}

func classifyWarningLine(trimmed string) MavenLineClass {
	// 编译器 WARNING：以 file:/// 开头
	if strings.Contains(trimmed, "file:///") {
		return LineCompilerWarning
	}

	// 其他都是 POM 模型校验 WARNING
	return LinePomWarning
}

// nextState 根据当前状态和行分类计算下一个状态
func nextState(current MavenState, lc MavenLineClass) MavenState {
	// 全局转移（任何状态都可以跳到这些）
	switch lc {
	case LineModuleHeader:
		return StateModuleBuild
	case LineReactorHeader:
		return StateReactor
	case LineBuildResult:
		return StateResult
	case LineStats:
		return StateStats
	}

	// 状态内转移
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
		if lc == LineMojoHeader {
			return StateMojo
		}
	case StateMojo:
		if lc != LineMojoHeader && lc != LineModuleHeader && lc != LineReactorHeader {
			return StatePluginOutput
		}
	case StatePluginOutput:
		if lc == LineMojoHeader {
			return StateMojo
		}
		if lc == LineTestHeader {
			return StateTestOutput
		}
	case StateTestOutput:
		if lc == LineMojoHeader {
			return StateMojo
		}
	case StateReactor:
		// 保持在 Reactor 状态直到 BUILD 行
	case StateResult:
		if lc == LineError {
			return StateErrorReport
		}
	case StateErrorReport:
		// 保持在 ErrorReport 直到 Stats
	}

	return current
}

func isAllDashes(s string) bool {
	for _, c := range s {
		if c != '-' && c != ' ' {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run "TestClassifyLine|TestStateTransition"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /private/tmp/gw
git add filter/java/maven_state.go filter/java/maven_state_test.go
git commit -m "添加 Maven 状态机：行分类器和状态转移逻辑"
```

---

### Task 2: 用状态机重写 MavenFilter

**Files:**
- Modify: `filter/java/maven.go` — 完全重写 Apply 和 ApplyOnError
- Modify: `filter/java/maven_test.go` — 增加真实 fixture 测试

- [ ] **Step 1: 添加真实 大型 Java 产线 fixture 测试**

在 `filter/java/maven_test.go` 末尾添加：

```go
func TestMavenFilter_RealProject_Failure(t *testing.T) {
	f := &MavenFilter{}
	fixture := loadFixture(t, "mvn_compile_real_failure.txt")

	result := f.ApplyOnError(filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"compile"},
		Stdout:   fixture,
		ExitCode: 1,
	})

	if result == nil {
		t.Fatal("ApplyOnError 不应返回 nil")
	}

	content := result.Content

	// 应保留 BUILD FAILURE
	if !strings.Contains(content, "BUILD FAILURE") {
		t.Error("应保留 BUILD FAILURE")
	}

	// 应保留 Total time
	if !strings.Contains(content, "Total time") {
		t.Error("应保留 Total time")
	}

	// 应保留至少一个 Reactor 条目（SUCCESS 或 FAILURE 的模块）
	if !strings.Contains(content, "SUCCESS") && !strings.Contains(content, "FAILURE") {
		t.Error("应保留 Reactor 条目")
	}

	// 应保留核心编译错误（去重后）
	if !strings.Contains(content, "Unresolved reference") {
		t.Error("应保留编译错误")
	}

	// 不应包含 POM WARNING
	if strings.Contains(content, "LATEST or RELEASE") {
		t.Error("不应包含 POM 校验 WARNING")
	}

	// 不应包含 Kotlin 编译器 WARNING
	if strings.Contains(content, "The corresponding parameter in the supertype") {
		t.Error("不应包含 Kotlin 编译器 WARNING")
	}

	// 不应包含帮助建议
	if strings.Contains(content, "Re-run Maven using the -X switch") {
		t.Error("不应包含帮助建议")
	}

	// 不应包含下载日志
	if strings.Contains(content, "Downloading from") {
		t.Error("不应包含下载日志")
	}

	// 压缩率应 > 90%（905 行 → <90 行）
	originalLines := strings.Count(fixture, "\n")
	filteredLines := strings.Count(content, "\n")
	compressionPct := 1.0 - float64(filteredLines)/float64(originalLines)
	if compressionPct < 0.90 {
		t.Errorf("压缩率 %.1f%% 低于 90%%（%d → %d 行）", compressionPct*100, originalLines, filteredLines)
	}

	t.Logf("压缩结果: %d → %d 行 (%.1f%%)", originalLines, filteredLines, compressionPct*100)
	t.Logf("过滤后内容:\n%s", content)
}

func TestMavenFilter_RealProject_Apply(t *testing.T) {
	f := &MavenFilter{}
	// 同一个文件但假装成功（测试 Apply 路径也能处理真实输出结构）
	fixture := loadFixture(t, "mvn_compile_real_failure.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"compile"},
		Stdout:   fixture,
		ExitCode: 0,
	})

	// 成功时应更激进压缩
	originalLines := strings.Count(fixture, "\n")
	filteredLines := strings.Count(output.Content, "\n")
	compressionPct := 1.0 - float64(filteredLines)/float64(originalLines)
	if compressionPct < 0.90 {
		t.Errorf("成功场景压缩率 %.1f%% 低于 90%%（%d → %d 行）", compressionPct*100, originalLines, filteredLines)
	}
}
```

- [ ] **Step 2: 运行新测试验证失败（当前实现可能无法达到 90% 压缩率）**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run "TestMavenFilter_RealProject"
```

Expected: 可能 PASS 也可能 FAIL（当前正则实现在 Apply 路径可能不够好）

- [ ] **Step 3: 用状态机重写 maven.go**

完全替换 `filter/java/maven.go` 的内容：

```go
// filter/java/maven.go
package java

import (
	"fmt"
	"strings"

	"github.com/gw-cli/gw/filter"
)

// MavenFilter 基于状态机过滤 Maven 构建输出
type MavenFilter struct{}

func (f *MavenFilter) Match(cmd string, args []string) bool {
	return cmd == "mvn"
}

func (f *MavenFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout + input.Stderr
	content := processMavenOutput(original, true)
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *MavenFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout + input.Stderr
	content := processMavenOutput(original, false)
	return &filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

// processMavenOutput 用状态机处理 Maven 输出
// successMode=true 时激进压缩（只保留摘要），false 时保留错误详情
func processMavenOutput(output string, successMode bool) string {
	lines := strings.Split(output, "\n")
	state := StateInit
	var result []string
	seenErrors := make(map[string]bool) // 错误去重

	for _, line := range lines {
		lc := classifyLine(line)
		state = nextState(state, lc)

		action := decideAction(state, lc, successMode)

		switch action {
		case ActionKeep:
			result = append(result, stripPrefix(line))
		case ActionKeepError:
			errLine := stripPrefix(line)
			// 错误去重
			dedupeKey := extractErrorKey(errLine)
			if dedupeKey != "" {
				if seenErrors[dedupeKey] {
					continue
				}
				seenErrors[dedupeKey] = true
			}
			result = append(result, errLine)
		case ActionKeepRaw:
			result = append(result, line)
		case ActionDrop:
			continue
		}
	}

	return collapseBlankLines(result)
}

// ActionType 决定一行的处理方式
type ActionType int

const (
	ActionDrop      ActionType = iota // 丢弃
	ActionKeep                        // 保留（去 [INFO]/[ERROR] 前缀）
	ActionKeepError                   // 保留并去重
	ActionKeepRaw                     // 保留原始行（含前缀）
)

// decideAction 根据状态和行分类决定处理方式
func decideAction(state MavenState, lc MavenLineClass, successMode bool) ActionType {
	// 全局丢弃：无论在哪个状态，这些行都是噪音
	switch lc {
	case LineDiscovery, LineSeparator, LineEmpty, LineFinishedAt,
		LineTransfer, LinePomWarning, LineCompilerWarning,
		LineProcessNoise, LineHelpSuggestion:
		return ActionDrop
	}

	// 按状态决定
	switch state {
	case StateInit, StateDiscovery, StateWarning:
		return ActionDrop

	case StateModuleBuild:
		// "Building xxx" 标题行 — Reactor Summary 已有此信息
		return ActionDrop

	case StateMojo:
		// "--- plugin:ver:goal ---" — 噪音
		return ActionDrop

	case StatePluginOutput:
		if successMode {
			return ActionDrop // 成功时插件输出全部丢弃
		}
		// 失败时保留错误和栈追踪
		if lc == LineError {
			return ActionKeepError
		}
		if lc == LineStackTrace {
			return ActionKeep
		}
		return ActionDrop

	case StateTestOutput:
		switch lc {
		case LineTestHeader:
			return ActionDrop // "T E S T S" 标题无信息量
		case LineTestSummary:
			return ActionKeep
		case LineTestRunning:
			if successMode {
				return ActionDrop // 成功时不需要每个测试类的 Running 行
			}
			return ActionKeep
		case LineError:
			return ActionKeepError
		case LineStackTrace:
			return ActionKeep
		}
		if successMode {
			return ActionDrop
		}
		return ActionKeep // 失败时保留测试详情

	case StateReactor:
		if lc == LineReactorHeader {
			return ActionDrop // "Reactor Summary:" 标题行不需要
		}
		if lc == LineReactorEntry {
			return ActionKeep
		}
		return ActionDrop

	case StateResult:
		if lc == LineBuildResult {
			return ActionKeep
		}
		return ActionDrop

	case StateStats:
		if lc == LineStats {
			return ActionKeep
		}
		return ActionDrop

	case StateErrorReport:
		if lc == LineError {
			return ActionKeepError
		}
		if lc == LineStackTrace {
			return ActionKeep
		}
		return ActionDrop
	}

	return ActionDrop
}

// stripPrefix 去除 [INFO]/[ERROR]/[WARNING] 前缀
func stripPrefix(line string) string {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"[INFO] ", "[ERROR] ", "[WARNING] "} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimPrefix(trimmed, prefix)
		}
	}
	// 只有 [INFO]/[ERROR]/[WARNING] 没有空格的情况
	for _, prefix := range []string{"[INFO]", "[ERROR]", "[WARNING]"} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return trimmed
}

// extractErrorKey 提取错误的去重 key
// 对同一类错误（相同引用名/相同异常类型）返回相同的 key
func extractErrorKey(errLine string) string {
	// Unresolved reference 'Xxx'
	if idx := strings.Index(errLine, "Unresolved reference"); idx >= 0 {
		ref := strings.TrimSpace(errLine[idx:])
		return "unresolved:" + ref
	}
	// Type mismatch: 同类型错误
	if idx := strings.Index(errLine, "Type mismatch"); idx >= 0 {
		return "type_mismatch:" + strings.TrimSpace(errLine[idx:])
	}
	// Cannot access class 'Xxx'
	if idx := strings.Index(errLine, "Cannot access class"); idx >= 0 {
		ref := strings.TrimSpace(errLine[idx:])
		return "access:" + ref
	}
	return "" // 不去重
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
	// 去掉首尾空行
	text := strings.Join(result, "\n")
	return strings.TrimSpace(text) + "\n"
}
```

- [ ] **Step 4: 运行全部 Maven 测试**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run TestMaven
```

Expected: 全部 PASS，包括 RealProject 测试达到 90%+ 压缩率

- [ ] **Step 5: 运行全项目测试确保无回归**

```bash
cd /private/tmp/gw
go test ./... -v
```

Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
cd /private/tmp/gw
git add filter/java/maven.go filter/java/maven_test.go testdata/mvn_compile_real_failure.txt
git commit -m "用状态机重写 MavenFilter：基于 ExecutionEventLogger 事件模型精确追踪构建阶段"
```

---

### Task 3: 真实输出验证 + 边缘 case 补充

**Files:**
- Modify: `filter/java/maven_state_test.go` — 增加边缘 case 测试

- [ ] **Step 1: 用真实 大型 Java 产线 输出验证并打印结果**

```bash
cd /private/tmp/gw
cat > /tmp/verify_maven_sm.go << 'GOEOF'
package main

import (
	"fmt"
	"os"
	"github.com/gw-cli/gw/filter"
	"github.com/gw-cli/gw/filter/java"
)

func main() {
	data, _ := os.ReadFile("testdata/mvn_compile_real_failure.txt")
	input := filter.FilterInput{Cmd: "mvn", Args: []string{"compile"}, Stdout: string(data), ExitCode: 1}
	f := &java.MavenFilter{}
	output := f.ApplyOnError(input)
	fmt.Print(output.Content)
	origLines := len(strings.Split(string(data), "\n"))
	filtLines := len(strings.Split(output.Content, "\n"))
	fmt.Fprintf(os.Stderr, "\n[结果] %d → %d 行，压缩 %.1f%%\n", origLines, filtLines, (1-float64(filtLines)/float64(origLines))*100)
}
GOEOF
go run /tmp/verify_maven_sm.go
```

Expected: 905 行 → <50 行，压缩 > 95%

- [ ] **Step 2: 添加边缘 case 测试 — 单模块项目（无 Reactor Summary）**

在 `filter/java/maven_state_test.go` 末尾添加：

```go
func TestStateMachine_SingleModule(t *testing.T) {
	// 单模块项目没有 Reactor Summary，直接从 BUILD 到 Stats
	input := `[INFO] Scanning for projects...
[INFO] 
[INFO] -----------------------< com.example:myapp >------------------------
[INFO] Building myapp 1.0.0
[INFO] --------------------------------[ jar ]---------------------------------
[INFO] 
[INFO] --- maven-compiler-plugin:3.10.1:compile (default-compile) @ myapp ---
[INFO] Nothing to compile - all classes are up to date
[INFO] 
[INFO] --- maven-surefire-plugin:3.0.0:test (default-test) @ myapp ---
[INFO] Tests run: 5, Failures: 0, Errors: 0, Skipped: 0
[INFO] 
[INFO] BUILD SUCCESS
[INFO] Total time:  1.234 s
[INFO] Finished at: 2026-04-16T10:00:00Z
`
	lines := strings.Split(input, "\n")
	state := StateInit
	var kept []MavenLineClass
	for _, line := range lines {
		lc := classifyLine(line)
		state = nextState(state, lc)
		action := decideAction(state, lc, true)
		if action != ActionDrop {
			kept = append(kept, lc)
		}
	}
	// 应该只保留：TestSummary + BuildResult + Stats
	if len(kept) > 4 {
		t.Errorf("单模块成功场景应保留 ≤4 行，实际保留 %d 行", len(kept))
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run TestStateMachine
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd /private/tmp/gw
git add filter/java/maven_state_test.go
git commit -m "补充状态机边缘 case 测试：单模块项目"
```

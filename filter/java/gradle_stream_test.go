package java

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

// TestGradleStreamFilter_Interface 编译期校验 GradleFilter 实现 StreamFilter 接口
func TestGradleStreamFilter_Interface(t *testing.T) {
	var _ filter.StreamFilter = (*GradleFilter)(nil)
}

// TestGradleStreamFilter_NoiseDropped 验证已知噪音行全部被丢弃
func TestGradleStreamFilter_NoiseDropped(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	noises := []string{
		"> Configure project :app",
		"> Configuring project :lib",
		"> Resolving dependencies of :app:runtimeClasspath",
		"> Transform artifact-1.0.jar (org.example:artifact:1.0) with Foo",
		"Downloading https://repo.maven.apache.org/maven2/org/example/foo/1.0/foo.jar",
		"  <==========---> 80% EXECUTING [3s]",
		"Starting a Gradle Daemon, 1 incompatible Daemon could not be reused",
		"Daemon will be stopped at the end of the build",
		"To honour the JVM settings for this build a single-use Daemon process will be forked.",
		"[Incubating] Problems report is available at: file:///app/build/reports/problems/problems-report.html",
		"Publishing build scan...",
		"> Task :app:compileJava UP-TO-DATE",
		"> Task :app:processResources NO-SOURCE",
		"> Task :lib:jar FROM-CACHE",
		"> Task :app:test SKIPPED",
		"w: file:///path/to/Foo.kt: (10, 5): Parameter 'x' is never used",
		"Deprecated Gradle features were used in this build, making it incompatible with Gradle 9.0.",
		"  Some plugin has been deprecated. This is scheduled to be removed in Gradle 9.",
		"",
	}

	for _, line := range noises {
		action, output := proc.ProcessLine(line)
		if action != filter.StreamDrop {
			t.Errorf("噪音行应丢弃但被发射: %q -> %q", line, output)
		}
	}
}

// TestGradleStreamFilter_KeyLinesEmitted 验证关键行被保留
func TestGradleStreamFilter_KeyLinesEmitted(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	type expect struct {
		line     string
		wantEmit bool
		contains string
	}

	cases := []expect{
		{"> Task :lib:test FAILED", true, "FAILED"},
		{"BUILD SUCCESSFUL in 7s", true, "BUILD SUCCESSFUL"},
		{"BUILD FAILED in 12s", true, "BUILD FAILED"},
		{"FAILURE: Build failed with an exception.", true, "FAILURE"},
		{"7 actionable tasks: 7 executed", true, "actionable task"},
		{"2 tests completed, 1 failed", true, "tests completed"},
		{"AppTest > integration() PASSED", true, "PASSED"},
		{"AuthServiceTest > testAuthWrongPassword() FAILED", true, "FAILED"},
	}

	for _, c := range cases {
		action, output := proc.ProcessLine(c.line)
		if c.wantEmit && action != filter.StreamEmit {
			t.Errorf("关键行应发射但被丢弃: %q", c.line)
		}
		if c.wantEmit && !strings.Contains(output, c.contains) {
			t.Errorf("发射内容 %q 应包含 %q", output, c.contains)
		}
	}
}

// TestGradleStreamFilter_WhatWentWrongBlock 验证 What went wrong 块跨多行保留
func TestGradleStreamFilter_WhatWentWrongBlock(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	lines := []string{
		"FAILURE: Build failed with an exception.",
		"",
		"* What went wrong:",
		"Execution failed for task ':lib:test'.",
		"> There were failing tests. See the report at: file:///app/lib/build/reports/tests/test/index.html",
		"",
		"* Try:",
		"> Run with --stacktrace option to get the stack trace.",
		"> Run with --info or --debug option to get more log output.",
		"",
		"BUILD FAILED in 7s",
	}

	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}
	joined := strings.Join(emitted, "\n")

	if !strings.Contains(joined, "* What went wrong:") {
		t.Error("应保留 * What went wrong: 标题行")
	}
	if !strings.Contains(joined, "Execution failed for task ':lib:test'.") {
		t.Error("应保留 What went wrong 正文")
	}
	// 报告链接应被剔除
	if strings.Contains(joined, "See the report at:") {
		t.Errorf("报告链接应丢弃，实际输出: %s", joined)
	}
	// Try 块应整段丢弃
	if strings.Contains(joined, "* Try:") {
		t.Error("* Try: 段应整段丢弃")
	}
	if strings.Contains(joined, "Run with --stacktrace") {
		t.Error("Try 段子项应整段丢弃")
	}
	// BUILD FAILED 必须保留
	if !strings.Contains(joined, "BUILD FAILED") {
		t.Error("应保留 BUILD FAILED")
	}
}

// TestGradleStreamFilter_ExceptionBlock 验证 Exception is 块（含栈帧）保留
func TestGradleStreamFilter_ExceptionBlock(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	lines := []string{
		"* Exception is:",
		"org.gradle.api.tasks.TaskExecutionException: Execution failed for task ':lib:test'.",
		"\tat org.gradle.api.internal.tasks.execution.ExecuteActionsTaskExecuter.lambda$executeIfValid$1(ExecuteActionsTaskExecuter.java:145)",
		"\tat org.gradle.internal.Try$Failure.ifSuccessfulOrElse(Try.java:282)",
	}

	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}
	joined := strings.Join(emitted, "\n")

	if !strings.Contains(joined, "* Exception is:") {
		t.Error("应保留 Exception is 标题")
	}
	if !strings.Contains(joined, "TaskExecutionException") {
		t.Error("应保留异常类名")
	}
	if !strings.Contains(joined, "ExecuteActionsTaskExecuter") {
		t.Error("应保留栈帧")
	}
}

// TestGradleStreamFilter_DeduplicateDeprecation 验证重复 deprecation 警告被去重
//
// 注：当前实现把单行 deprecation 直接归入噪音丢弃。该测试改为校验 What went wrong
// 块内重复的 "Execution failed for task ':lib:test'." 只发射一次。
func TestGradleStreamFilter_DeduplicateRepeatedErrors(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	lines := []string{
		"* What went wrong:",
		"Execution failed for task ':lib:test'.",
		"",
		"* What went wrong:",
		"Execution failed for task ':lib:test'.",
		"",
		"* What went wrong:",
		"Execution failed for task ':app:compileJava'.",
		"",
	}

	emitCount := map[string]int{}
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			trimmed := strings.TrimSpace(output)
			emitCount[trimmed]++
		}
	}

	if emitCount["* What went wrong:"] != 1 {
		t.Errorf("* What went wrong: 应只发射一次，实际 %d 次", emitCount["* What went wrong:"])
	}
	if emitCount["Execution failed for task ':lib:test'."] != 1 {
		t.Errorf("重复的 Execution failed 应去重，实际 %d 次", emitCount["Execution failed for task ':lib:test'."])
	}
	// 不同的错误内容应正常发射
	if emitCount["Execution failed for task ':app:compileJava'."] != 1 {
		t.Errorf("不同的错误应发射，实际 %d 次", emitCount["Execution failed for task ':app:compileJava'."])
	}
}

// TestGradleStreamFilter_FlushOnSuccessNoOutput 验证成功但无输出时 Flush 给摘要
func TestGradleStreamFilter_FlushOnSuccessNoOutput(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	// 全是噪音
	noises := []string{
		"> Task :app:compileJava UP-TO-DATE",
		"> Task :app:test UP-TO-DATE",
	}
	for _, line := range noises {
		proc.ProcessLine(line)
	}

	flushed := proc.Flush(0)
	if len(flushed) == 0 {
		t.Fatal("成功且无输出时 Flush 应至少返回一行摘要")
	}
	if !strings.Contains(strings.Join(flushed, "\n"), "gradle build ok") {
		t.Errorf("Flush 摘要应包含 'gradle build ok'，实际: %v", flushed)
	}
}

// TestGradleStreamFilter_FlushOnSuccessHasOutput 验证有输出时 Flush 返回 nil
func TestGradleStreamFilter_FlushOnSuccessHasOutput(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	proc.ProcessLine("BUILD SUCCESSFUL in 7s")
	proc.ProcessLine("8 actionable tasks: 8 executed")

	flushed := proc.Flush(0)
	if len(flushed) != 0 {
		t.Errorf("有输出时 Flush 应返回 nil，实际: %v", flushed)
	}
}

// TestGradleStreamFilter_FlushOnFailure 验证失败时 Flush 不附加摘要
func TestGradleStreamFilter_FlushOnFailure(t *testing.T) {
	f := &GradleFilter{}
	proc := f.NewStreamInstance()

	// 即便没产生输出，失败时也不应该补摘要（避免误导）
	flushed := proc.Flush(1)
	if len(flushed) != 0 {
		t.Errorf("失败时 Flush 应返回 nil，实际: %v", flushed)
	}
}

// TestGradleStreamFilter_RealFailureFixture 用真实 fixture 验证流式过滤
func TestGradleStreamFilter_RealFailureFixture(t *testing.T) {
	f := &GradleFilter{}
	fixture := loadFixture(t, "gradle_test_failure.txt")
	lines := strings.Split(fixture, "\n")

	proc := f.NewStreamInstance()
	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}
	flushed := proc.Flush(1)
	emitted = append(emitted, flushed...)

	content := strings.Join(emitted, "\n")
	totalLines := len(lines)
	emittedLines := len(emitted)
	compression := 1.0 - float64(emittedLines)/float64(totalLines)
	t.Logf("原始行数: %d, 输出行数: %d, 压缩率: %.1f%%", totalLines, emittedLines, compression*100)

	mustContain := []string{
		"BUILD FAILED",
		"> Task :lib:test FAILED",
		"FAILURE:",
		"* What went wrong:",
		"Execution failed for task ':lib:test'.",
		"actionable task",
		"tests completed",
		"401",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("流式输出应包含 %q，实际:\n%s", s, content)
		}
	}

	mustNotContain := []string{
		"Starting a Gradle Daemon",
		"Daemon will be stopped",
		"Deprecated Gradle features",
		"* Try:",
		"Run with --scan",
		"See the report at:",
		"[Incubating]",
	}
	for _, s := range mustNotContain {
		if strings.Contains(content, s) {
			t.Errorf("流式输出不应包含 %q，实际:\n%s", s, content)
		}
	}

	if compression < 0.30 {
		t.Errorf("压缩率 %.1f%% 偏低", compression*100)
	}
}

// TestGradleStreamFilter_RealSuccessFixture 用真实 success fixture 验证
func TestGradleStreamFilter_RealSuccessFixture(t *testing.T) {
	f := &GradleFilter{}
	fixture := loadFixture(t, "gradle_build_success.txt")
	lines := strings.Split(fixture, "\n")

	proc := f.NewStreamInstance()
	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}
	flushed := proc.Flush(0)
	emitted = append(emitted, flushed...)
	content := strings.Join(emitted, "\n")

	if !strings.Contains(content, "BUILD SUCCESSFUL") {
		t.Errorf("成功 fixture 应包含 BUILD SUCCESSFUL，实际:\n%s", content)
	}
	if !strings.Contains(content, "actionable task") {
		t.Errorf("成功 fixture 应包含 actionable task，实际:\n%s", content)
	}
	if strings.Contains(content, "Starting a Gradle Daemon") {
		t.Error("成功 fixture 不应包含 Daemon 启动行")
	}
	if strings.Contains(content, "Deprecated Gradle features") {
		t.Error("成功 fixture 不应包含 Deprecated 行")
	}
}

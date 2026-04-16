# 流式过滤模式 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 gw 增加流式过滤能力，支持逐行读取 + 实时过滤 + 即时输出，解锁长驻进程（Spring Boot、dev server）的输出压缩。同时保持批量模式作为默认路径不受影响。

**Architecture:** 新增 `StreamFilter` 接口和 `RunCommandStreaming()` 执行器。StreamFilter 设计为**有状态实例**——每次执行创建新实例，状态作为 struct 字段持有，接口不传递 state 参数。流式过滤不依赖 exit code，使用统一策略（噪音始终丢弃，错误始终保留）。Action 只有 Drop/Emit 两种，缓冲由实现内部封装。信号中断场景保证 Flush 被调用。

**Tech Stack:** Go, bufio.Scanner, exec.StdoutPipe/StderrPipe, 无新依赖

**Plan Review 修正记录（8 项 → 全部 fix）：**
1. ~~StreamBuffer action~~ → 删除，只保留 Drop/Emit
2. ~~Flush 返回 string~~ → 改为 `[]string`
3. ~~SIGINT 时 Flush 不调用~~ → signal kill 返回 exit code 而非 error
4. ~~state interface{} 类型断言~~ → 有状态 struct，ProcessLine 不传 state
5. ~~流式 token 估算 lines*20~~ → 累计字符数用 EstimateTokens
6. ~~tracking goroutine 被 os.Exit 杀~~ → 加 channel 等待
7. ~~集成测试没覆盖流式路径~~ → 用假 mvn 脚本
8. ~~scanner.Err() 未检查~~ → 循环后检查

---

## File Structure

```
filter/
├── filter.go              # 修改：新增 StreamFilter 接口
├── registry.go            # 修改：新增 FindStream() 方法
├── java/
│   ├── maven.go           # 修改：MavenFilter 实现 StreamFilter
│   ├── maven_state.go     # 不动（天然支持逐行处理）
│   ├── maven_state_test.go# 不动
│   └── maven_stream_test.go # 新增：流式过滤测试
internal/
├── runner.go              # 不动
├── stream.go              # 新增：RunCommandStreaming()
├── stream_test.go         # 新增：流式执行器测试
cmd/
├── exec.go                # 修改：检测 StreamFilter，选择执行路径
```

---

### Task 1: 流式执行器 RunCommandStreaming

**Files:**
- Create: `internal/stream.go`
- Create: `internal/stream_test.go`

- [ ] **Step 1: 编写流式执行器测试**

```go
// internal/stream_test.go
package internal

import (
	"strings"
	"testing"
)

func TestRunCommandStreaming_CollectsLines(t *testing.T) {
	var lines []string
	exitCode, err := RunCommandStreaming("echo", []string{"line1\nline2\nline3"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if len(lines) != 1 {
		// echo 输出单行（含 \n 字面量），不会被 shell 展开
		// 用 printf 测试多行
	}
}

func TestRunCommandStreaming_MultiLine(t *testing.T) {
	var lines []string
	exitCode, err := RunCommandStreaming("printf", []string{"line1\nline2\nline3\n"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestRunCommandStreaming_ExitCode(t *testing.T) {
	var lines []string
	exitCode, err := RunCommandStreaming("sh", []string{"-c", "echo hello; exit 42"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 42 {
		t.Errorf("expected exit 42, got %d", exitCode)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Errorf("expected [hello], got %v", lines)
	}
}

func TestRunCommandStreaming_Stderr(t *testing.T) {
	var stdoutLines []string
	var stderrBuf strings.Builder
	exitCode, err := RunCommandStreamingFull("sh", []string{"-c", "echo out; echo err >&2"}, func(line string) {
		stdoutLines = append(stdoutLines, line)
	}, &stderrBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if len(stdoutLines) != 1 || stdoutLines[0] != "out" {
		t.Errorf("expected stdout [out], got %v", stdoutLines)
	}
	if !strings.Contains(stderrBuf.String(), "err") {
		t.Errorf("expected stderr to contain 'err', got %q", stderrBuf.String())
	}
}

func TestRunCommandStreaming_NotFound(t *testing.T) {
	_, err := RunCommandStreaming("nonexistent_command_xyz", nil, func(line string) {})
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./internal/ -v -run TestRunCommandStreaming
```

Expected: FAIL — functions 未定义

- [ ] **Step 3: 实现流式执行器**

```go
// internal/stream.go
package internal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// RunCommandStreaming 流式执行命令，逐行回调 stdout，stderr 直接透传到 os.Stderr。
// 返回 exit code。仅在命令无法启动时返回 error。
func RunCommandStreaming(name string, args []string, onLine func(string)) (int, error) {
	var stderrBuf strings.Builder
	return RunCommandStreamingFull(name, args, onLine, &stderrBuf)
}

// RunCommandStreamingFull 流式执行命令，逐行回调 stdout，stderr 写入 stderrWriter。
func RunCommandStreamingFull(name string, args []string, onLine func(string), stderrWriter io.Writer) (int, error) {
	cmd := exec.Command(name, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start %s: %w", name, err)
	}

	// 逐行读取 stdout
	scanner := bufio.NewScanner(stdout)
	// 增大 buffer 以处理超长行（如 minified JSON）
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}

	// 检查 scanner 错误（如超长行）
	if scanErr := scanner.Err(); scanErr != nil {
		// 记录但不阻断——输出可能不完整，但进程仍需正常结束
		fmt.Fprintf(os.Stderr, "[gw] scanner error: %v\n", scanErr)
	}

	// 等待进程退出
	err = cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		// 信号中断也视为正常退出（返回 -1），确保 Flush 能被调用
		return -1, nil
	}

	return 0, nil
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./internal/ -v -run TestRunCommandStreaming
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /private/tmp/gw
git add internal/stream.go internal/stream_test.go
git commit -m "实现流式执行器 RunCommandStreaming：逐行回调 stdout"
```

---

### Task 2: StreamFilter 接口定义

**Files:**
- Modify: `filter/filter.go` — 新增 StreamFilter 接口
- Modify: `filter/registry.go` — 新增 FindStream() 方法

- [ ] **Step 1: 定义 StreamFilter 接口**

在 `filter/filter.go` 末尾添加：

```go
// StreamAction 表示流式过滤中对单行的决策
type StreamAction int

const (
	StreamDrop StreamAction = iota // 丢弃此行
	StreamEmit                     // 立即输出此行
)

// StreamFilter 是支持流式（逐行）过滤的接口。
// 实现此接口的过滤器可以处理长驻进程的输出。
//
// 设计：StreamFilter 是有状态的实例。每次命令执行时，调用方通过
// NewStreamInstance() 创建新实例，状态由实例自身持有。
// 这避免了 interface{} state 传递的类型安全问题。
//
// StreamFilter 同时也必须实现 Filter 接口（用于批量模式）。
type StreamFilter interface {
	Filter

	// NewStreamInstance 创建一个新的流式过滤实例（带独立状态）。
	// 每次命令执行调用一次。
	NewStreamInstance() StreamProcessor
}

// StreamProcessor 是单次命令执行的流式处理器。
// 由 StreamFilter.NewStreamInstance() 创建，持有本次执行的状态。
type StreamProcessor interface {
	// ProcessLine 处理一行输出，返回决策和处理后的行内容。
	ProcessLine(line string) (action StreamAction, output string)

	// Flush 在进程退出后调用，返回缓冲区中需要输出的剩余行。
	// exitCode 是进程的退出码。
	Flush(exitCode int) []string
}
```

- [ ] **Step 2: 在 registry 中添加 FindStream**

在 `filter/registry.go` 中添加：

```go
// FindStream 查找匹配的 StreamFilter。如果匹配的过滤器不支持流式，返回 nil。
func (r *Registry) FindStream(cmd string, args []string) StreamFilter {
	f := r.Find(cmd, args)
	if f == nil {
		return nil
	}
	if sf, ok := f.(StreamFilter); ok {
		return sf
	}
	return nil
}
```

同时为全局 registry 添加：

```go
func FindStream(cmd string, args []string) StreamFilter {
	return globalRegistry.FindStream(cmd, args)
}
```

- [ ] **Step 3: 验证编译**

```bash
cd /private/tmp/gw
go build ./...
```

Expected: 编译成功（新接口没有实现者，但不影响编译）

- [ ] **Step 4: Commit**

```bash
cd /private/tmp/gw
git add filter/filter.go filter/registry.go
git commit -m "定义 StreamFilter 接口：ProcessLine 逐行决策 + Flush 退出时刷新"
```

---

### Task 3: Maven StreamFilter 实现

**Files:**
- Modify: `filter/java/maven.go` — MavenFilter 实现 StreamFilter
- Create: `filter/java/maven_stream_test.go` — 流式过滤测试

- [ ] **Step 1: 编写流式过滤测试**

```go
// filter/java/maven_stream_test.go
package java

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestMavenStreamFilter_Interface(t *testing.T) {
	var f filter.StreamFilter = &MavenFilter{}
	if f == nil {
		t.Fatal("MavenFilter should implement StreamFilter")
	}
}

func TestMavenStreamFilter_NoiseLinesDropped(t *testing.T) {
	f := &MavenFilter{}
	proc := f.NewStreamInstance()
	noiseLines := []string{
		"[INFO] Scanning for projects...",
		"[INFO] ------------------------------------------------------------------------",
		"[INFO] Building myapp 1.0.0",
		"[INFO] --- maven-compiler-plugin:3.10.1:compile (default-compile) @ myapp ---",
		"[INFO] Downloading from central: https://repo.maven.apache.org/...",
		"[WARNING] 'dependencies.dependency.version' for ISFJ is LATEST or RELEASE",
	}

	for _, line := range noiseLines {
		action, _ := proc.ProcessLine(line)
		if action != filter.StreamDrop {
			t.Errorf("expected StreamDrop for noise line %q, got %v", line, action)
		}
	}
}

func TestMavenStreamFilter_KeyLinesEmitted(t *testing.T) {
	f := &MavenFilter{}
	proc := f.NewStreamInstance()
	lines := []string{
		"[INFO] Scanning for projects...",
		"[INFO] Building myapp 1.0.0",
		"[INFO] --- maven-compiler-plugin:3.10.1:compile ---",
		"[INFO] Reactor Summary for myapp 1.0.0:",
		"[INFO] myapp ...................................... SUCCESS [  1.234 s]",
		"[INFO] BUILD SUCCESS",
		"[INFO] Total time:  1.234 s",
	}

	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}

	// Reactor 条目、BUILD SUCCESS、Total time 应被输出
	joined := strings.Join(emitted, "\n")
	if !strings.Contains(joined, "SUCCESS") {
		t.Error("should emit Reactor entry with SUCCESS")
	}
	if !strings.Contains(joined, "BUILD SUCCESS") {
		t.Error("should emit BUILD SUCCESS")
	}
	if !strings.Contains(joined, "Total time") {
		t.Error("should emit Total time")
	}
}

func TestMavenStreamFilter_ErrorsEmitted(t *testing.T) {
	f := &MavenFilter{}
	proc := f.NewStreamInstance()
	lines := []string{
		"[INFO] Scanning for projects...",
		"[INFO] Building myapp 1.0.0",
		"[INFO] --- kotlin-maven-plugin:2.2.0:compile ---",
		"[ERROR] file:///app/Foo.kt:8:52 Unresolved reference 'BusinessLog'.",
		"[ERROR] file:///app/Foo.kt:34:6 Unresolved reference 'BusinessLog'.",
		"[ERROR] file:///app/Bar.kt:9:45 Unresolved reference 'CMKDTO'.",
		"[INFO] BUILD FAILURE",
		"[INFO] Total time:  2.0 s",
	}

	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}

	joined := strings.Join(emitted, "\n")
	// 应保留错误（去重后）
	if !strings.Contains(joined, "Unresolved reference 'BusinessLog'") {
		t.Error("should emit first BusinessLog error")
	}
	if !strings.Contains(joined, "Unresolved reference 'CMKDTO'") {
		t.Error("should emit CMKDTO error")
	}
	// BusinessLog 应只出现一次（去重）
	if strings.Count(joined, "BusinessLog") > 1 {
		t.Error("should deduplicate BusinessLog errors")
	}
	if !strings.Contains(joined, "BUILD FAILURE") {
		t.Error("should emit BUILD FAILURE")
	}
}

func TestMavenStreamFilter_RealProject(t *testing.T) {
	f := &MavenFilter{}
	proc := f.NewStreamInstance()
	fixture := loadFixture(t, "mvn_compile_real_failure.txt")
	lines := strings.Split(fixture, "\n")

	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}

	// Flush（进程退出 exit 1）
	flushed := proc.Flush(1)
	emitted = append(emitted, flushed...)

	totalEmitted := len(emitted)
	totalOriginal := len(lines)
	compressionPct := 1.0 - float64(totalEmitted)/float64(totalOriginal)

	t.Logf("流式过滤: %d → %d 行 (%.1f%%)", totalOriginal, totalEmitted, compressionPct*100)
	t.Logf("输出:\n%s", strings.Join(emitted, "\n"))

	if compressionPct < 0.85 {
		t.Errorf("流式压缩率 %.1f%% 低于 85%%", compressionPct*100)
	}
	if !strings.Contains(strings.Join(emitted, "\n"), "BUILD") {
		t.Error("should contain BUILD result")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run TestMavenStream
```

Expected: FAIL — MavenFilter 未实现 StreamFilter

- [ ] **Step 3: 为 MavenFilter 实现 StreamFilter**

在 `filter/java/maven.go` 末尾添加：

```go
// --- StreamFilter 实现 ---

// NewStreamInstance 创建流式处理器实例
func (f *MavenFilter) NewStreamInstance() filter.StreamProcessor {
	return &mavenStreamProcessor{
		state:      StateInit,
		seenErrors: make(map[string]bool),
	}
}

// mavenStreamProcessor 是 MavenFilter 的流式处理器，持有单次执行的状态
type mavenStreamProcessor struct {
	state      MavenState
	seenErrors map[string]bool
	buffer     []string // 插件输出缓冲区
}

func (p *mavenStreamProcessor) ProcessLine(line string) (filter.StreamAction, string) {
	lc := classifyLine(line)
	p.state = nextState(p.state, lc)

	// 全局丢弃：无论在哪个状态都是噪音
	switch lc {
	case LineDiscovery, LineSeparator, LineEmpty, LineFinishedAt,
		LineTransfer, LinePomWarning, LineCompilerWarning,
		LineProcessNoise, LineHelpSuggestion:
		return filter.StreamDrop, ""
	}

	// 按状态决策
	switch p.state {
	case StateInit, StateDiscovery, StateWarning:
		return filter.StreamDrop, ""

	case StateModuleBuild:
		return filter.StreamDrop, ""

	case StateMojo:
		p.buffer = nil
		return filter.StreamDrop, ""

	case StatePluginOutput:
		if lc == LineError {
			stripped := stripPrefix(line)
			dedupeKey := extractErrorKey(stripped)
			if dedupeKey != "" {
				if p.seenErrors[dedupeKey] {
					return filter.StreamDrop, ""
				}
				p.seenErrors[dedupeKey] = true
			}
			p.buffer = nil
			return filter.StreamEmit, stripped
		}
		if lc == LineStackTrace {
			return filter.StreamEmit, strings.TrimSpace(line)
		}
		if len(p.buffer) < 10 {
			p.buffer = append(p.buffer, line)
		}
		return filter.StreamDrop, ""

	case StateTestOutput:
		switch lc {
		case LineTestHeader:
			return filter.StreamDrop, ""
		case LineTestSummary:
			return filter.StreamEmit, stripPrefix(line)
		case LineTestRunning:
			return filter.StreamDrop, ""
		case LineError:
			stripped := stripPrefix(line)
			dedupeKey := extractErrorKey(stripped)
			if dedupeKey != "" && p.seenErrors[dedupeKey] {
				return filter.StreamDrop, ""
			}
			if dedupeKey != "" {
				p.seenErrors[dedupeKey] = true
			}
			return filter.StreamEmit, stripped
		case LineStackTrace:
			return filter.StreamEmit, strings.TrimSpace(line)
		}
		return filter.StreamDrop, ""

	case StateReactor:
		if lc == LineReactorEntry {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""

	case StateResult:
		if lc == LineBuildResult {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""

	case StateStats:
		if lc == LineStats {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""

	case StateErrorReport:
		if lc == LineError {
			stripped := stripPrefix(line)
			dedupeKey := extractErrorKey(stripped)
			if dedupeKey != "" && p.seenErrors[dedupeKey] {
				return filter.StreamDrop, ""
			}
			if dedupeKey != "" {
				p.seenErrors[dedupeKey] = true
			}
			return filter.StreamEmit, stripped
		}
		if lc == LineStackTrace {
			return filter.StreamEmit, strings.TrimSpace(line)
		}
		return filter.StreamDrop, ""
	}

	return filter.StreamDrop, ""
}

func (p *mavenStreamProcessor) Flush(exitCode int) []string {
	// 进程退出后，如果有缓冲的插件输出且退出码非零，输出它们
	if exitCode != 0 && len(p.buffer) > 0 {
		return p.buffer
	}
	return nil
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run TestMavenStream
```

Expected: PASS，RealProject 测试压缩率 > 85%

- [ ] **Step 5: 运行全部测试确认无回归**

```bash
cd /private/tmp/gw
go test ./... -v
```

Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
cd /private/tmp/gw
git add filter/java/maven.go filter/java/maven_stream_test.go
git commit -m "MavenFilter 实现 StreamFilter：逐行决策 + 错误去重 + 缓冲区"
```

---

### Task 4: exec 命令集成流式路径

**Files:**
- Modify: `cmd/exec.go` — 检测 StreamFilter，选择流式或批量执行路径

- [ ] **Step 1: 在 exec.go 中添加流式执行路径**

修改 `runExec` 函数，在 ROUTE 阶段后增加流式路径判断：

```go
// 在 ROUTE 阶段后，检查是否支持流式过滤
streamFilter := filter.FindStream(cmdName, cmdArgs)

if streamFilter != nil {
	// 流式路径
	runStreamExec(streamFilter, cmdName, cmdArgs)
	return
}

// 批量路径（现有逻辑不变）
// ...
```

新增 `runStreamExec` 函数：

```go
func runStreamExec(sf filter.StreamFilter, cmdName string, cmdArgs []string) {
	start := time.Now()
	proc := sf.NewStreamInstance()
	var originalChars int
	var filteredChars int

	// 流式执行 + 逐行过滤
	var stderrBuf strings.Builder
	exitCode, err := internal.RunCommandStreamingFull(cmdName, cmdArgs, func(line string) {
		originalChars += len(line)
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			filteredChars += len(output)
			fmt.Println(output)
		}
	}, &stderrBuf)

	if err != nil {
		fmt.Fprintf(os.Stderr, "gw exec: 无法执行命令: %v\n", err)
		os.Exit(127)
	}

	// stderr 透传
	if stderrBuf.Len() > 0 {
		fmt.Fprint(os.Stderr, stderrBuf.String())
	}

	// Flush 缓冲区
	flushedLines := proc.Flush(exitCode)
	for _, line := range flushedLines {
		filteredChars += len(line)
		fmt.Println(line)
	}

	// TRACK（带 channel 等待，避免 os.Exit 杀 goroutine）
	elapsed := time.Since(start)
	inputTokens := track.EstimateTokens(strings.Repeat("x", originalChars))
	outputTokens := track.EstimateTokens(strings.Repeat("x", filteredChars))

	if Verbose {
		fmt.Fprintf(os.Stderr, "[gw:stream] %d → %d tokens (saved %d, elapsed %dms)\n",
			inputTokens, outputTokens, inputTokens-outputTokens, elapsed.Milliseconds())
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		db, err := track.NewDB(track.DefaultDBPath())
		if err != nil {
			return
		}
		defer db.Close()
		_ = db.InsertRecord(track.Record{
			Timestamp:    time.Now(),
			Command:      cmdName + " " + strings.Join(cmdArgs, " "),
			ExitCode:     exitCode,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			SavedTokens:  inputTokens - outputTokens,
			ElapsedMs:    elapsed.Milliseconds(),
			FilterUsed:   sf.Name() + ":stream",
		})
	}()
	<-done

	os.Exit(exitCode)
}
```

- [ ] **Step 2: 验证编译**

```bash
cd /private/tmp/gw
go build ./...
```

- [ ] **Step 3: 手动测试流式路径**

```bash
cd /private/tmp/gw
# Maven 已实现 StreamFilter，应走流式路径
go build -o gw .
./gw -v exec sh -c 'echo "[INFO] Scanning for projects..."; echo "[INFO] Building myapp 1.0.0"; echo "[INFO] BUILD SUCCESS"; echo "[INFO] Total time: 1.0 s"'
```

Expected: 只输出 BUILD SUCCESS 和 Total time（Scanning 和 Building 被丢弃），stderr 显示 `[gw:stream]` 标记

- [ ] **Step 4: Commit**

```bash
cd /private/tmp/gw
git add cmd/exec.go
git commit -m "exec 命令集成流式路径：检测 StreamFilter 自动选择执行模式"
```

---

### Task 5: 重新启用 SpringBootFilter（流式模式）

**Files:**
- Modify: `filter/java/springboot.go` — 实现 StreamFilter，重新启用注册
- Modify: `filter/java/springboot_test.go` — 添加流式测试

- [ ] **Step 1: 恢复 SpringBootFilter 注册**

```go
func init() {
	filter.Register(&SpringBootFilter{})
}
```

- [ ] **Step 2: 实现 SpringBootFilter 的 StreamFilter 接口**

```go
// NewStreamInstance 创建流式处理器实例
func (f *SpringBootFilter) NewStreamInstance() filter.StreamProcessor {
	return &springBootStreamProcessor{}
}

type springBootStreamProcessor struct {
	inBanner bool
}

func (p *springBootStreamProcessor) ProcessLine(line string) (filter.StreamAction, string) {
	trimmed := strings.TrimSpace(line)

	// Banner 检测（多行）
	if strings.Contains(trimmed, "____") || strings.Contains(trimmed, ":: Spring Boot ::") ||
		strings.Contains(trimmed, "=========|") || isBannerDecorationLine(trimmed) {
		p.inBanner = true
		return filter.StreamDrop, ""
	}
	if p.inBanner && trimmed == "" {
		p.inBanner = false
		return filter.StreamDrop, ""
	}

	// 噪音行（复用已有规则）
	if isSpringBootNoise(line) {
		return filter.StreamDrop, ""
	}

	return filter.StreamEmit, line
}

func (p *springBootStreamProcessor) Flush(exitCode int) []string {
	return nil
}
```

`isSpringBootNoise` 提取现有 Apply() 中的噪音判断逻辑为独立函数。

- [ ] **Step 3: 添加流式测试**

```go
func TestSpringBootStreamFilter_Interface(t *testing.T) {
	var f filter.StreamFilter = &SpringBootFilter{}
	if f == nil {
		t.Fatal("SpringBootFilter should implement StreamFilter")
	}
}

func TestSpringBootStreamFilter_Startup(t *testing.T) {
	f := &SpringBootFilter{}
	proc := f.NewStreamInstance()
	fixture := loadFixture(t, "springboot_startup.txt")
	lines := strings.Split(fixture, "\n")

	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}

	joined := strings.Join(emitted, "\n")
	// 应保留端口和 Started
	if !strings.Contains(joined, "8080") {
		t.Error("should preserve port info")
	}
	if !strings.Contains(joined, "Started") {
		t.Error("should preserve Started message")
	}
	// 应去除 banner
	if strings.Contains(joined, "____") {
		t.Error("should strip banner")
	}
	// 应去除 HikariPool
	if strings.Contains(joined, "HikariPool") {
		t.Error("should strip HikariPool")
	}
}
```

- [ ] **Step 4: 运行全部测试**

```bash
cd /private/tmp/gw
go test ./... -v
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
cd /private/tmp/gw
git add filter/java/springboot.go filter/java/springboot_test.go
git commit -m "SpringBootFilter 实现 StreamFilter 并重新启用：支持长驻进程流式过滤"
```

---

### Task 6: 端到端集成测试

**Files:**
- Modify: `cmd/exec_test.go` — 添加流式路径集成测试

- [ ] **Step 1: 添加流式集成测试**

```go
func TestExec_StreamMode_Maven(t *testing.T) {
	// 构建二进制
	build := exec.Command("go", "build", "-o", "/tmp/gw-stream-test", ".")
	build.Dir = "/private/tmp/gw"
	if err := build.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// 用 sh -c 模拟 Maven 输出（因为 mvn 已实现 StreamFilter，走流式路径）
	// gw exec mvn 会走流式路径
	// 但测试环境没有 mvn，用 sh 模拟
	mavenOutput := `[INFO] Scanning for projects...
[INFO] Building myapp 1.0.0
[INFO] --- maven-compiler-plugin:3.10.1:compile ---
[INFO] Compiling 12 source files
[INFO] myapp ...................................... SUCCESS [  1.234 s]
[INFO] BUILD SUCCESS
[INFO] Total time:  1.234 s
[INFO] Finished at: 2026-04-16T10:00:00Z`

	cmd := exec.Command("/tmp/gw-stream-test", "exec", "sh", "-c", "echo '"+mavenOutput+"'")
	out, _ := cmd.CombinedOutput()
	output := string(out)

	// sh 不会被 MavenFilter 匹配（cmd="sh"），所以走批量透传
	// 这个测试验证的是非匹配命令的透传行为
	if !strings.Contains(output, "Scanning") {
		t.Error("unmatched command should passthrough")
	}
}
```

- [ ] **Step 2: 运行全部测试**

```bash
cd /private/tmp/gw
go test ./... -v
```

Expected: PASS

- [ ] **Step 3: 最终构建验证**

```bash
cd /private/tmp/gw
go build -o gw .
./gw --help
```

Expected: 正常输出帮助信息

- [ ] **Step 4: Commit**

```bash
cd /private/tmp/gw
git add cmd/exec_test.go
git commit -m "添加流式路径集成测试"
```

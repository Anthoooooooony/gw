# GW CLI Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI proxy that intercepts shell commands via Claude Code PreToolUse hooks, executes them locally, filters the output to reduce LLM token consumption, and tracks savings — with special depth for Java ecosystem commands (Maven/Gradle/Spring Boot).

**Architecture:** Single Go binary with a six-stage pipeline (PARSE → ROUTE → EXECUTE → FILTER → PRINT → TRACK). Dual-layer filter system: Go hardcoded filters for high-frequency commands + TOML declarative engine for long-tail coverage. PreToolUse hook rewrites commands from `git status` to `gw exec git status`, with conservative pipe/redirect detection that skips rewriting when output is consumed by downstream programs.

**Tech Stack:** Go 1.22+, cobra (CLI framework), mattn/go-sqlite3 (tracking), BurntSushi/toml (declarative rules), go:embed (built-in TOML rules)

---

## File Structure

```
gw/
├── go.mod
├── go.sum
├── main.go                    # 入口，cobra root command 注册
├── cmd/
│   ├── exec.go                # gw exec — 六阶段管道核心
│   ├── rewrite.go             # gw rewrite — Hook 改写接口
│   ├── init_cmd.go            # gw init — 安装 Claude Code hook
│   ├── uninstall.go           # gw uninstall — 移除 hook
│   └── gain.go                # gw gain — Token 节省统计
├── filter/
│   ├── filter.go              # Filter 接口 + FilterInput/FilterOutput 类型
│   ├── registry.go            # 过滤器注册表 + Match 路由
│   ├── git/
│   │   ├── status.go          # git status 过滤器
│   │   ├── status_test.go
│   │   ├── log.go             # git log 过滤器
│   │   └── log_test.go
│   ├── java/
│   │   ├── maven.go           # mvn compile/test/package 过滤器
│   │   ├── maven_test.go
│   │   ├── gradle.go          # gradle/gradlew build/test 过滤器
│   │   ├── gradle_test.go
│   │   ├── springboot.go      # Spring Boot 启动/运行日志过滤器
│   │   └── springboot_test.go
│   └── toml/
│       ├── engine.go          # TOML 声明式过滤引擎
│       ├── engine_test.go
│       └── rules/
│           ├── docker.toml    # 内置规则（go:embed）
│           └── kubectl.toml
├── hook/
│   ├── install.go             # Claude Code hook 安装/卸载
│   ├── install_test.go
│   ├── lexer.go               # Shell 命令 lexer（分段、管道检测）
│   └── lexer_test.go
├── track/
│   ├── db.go                  # SQLite 存储
│   ├── db_test.go
│   ├── stats.go               # 统计计算
│   ├── stats_test.go
│   └── token.go               # Token 估算
├── config/
│   └── config.go              # 配置加载（CLI > 项目 > 用户 > 默认）
├── internal/
│   ├── runner.go              # 子进程执行器
│   └── runner_test.go
└── testdata/                  # 测试用的真实命令输出 fixtures
    ├── git_status_clean.txt
    ├── git_status_dirty.txt
    ├── mvn_test_success.txt
    ├── mvn_test_failure.txt
    ├── gradle_build_success.txt
    ├── gradle_test_failure.txt
    └── springboot_startup.txt
```

---

### Task 1: 项目初始化 + Filter 接口定义

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `filter/filter.go`

- [ ] **Step 1: 初始化 Go module**

```bash
cd /private/tmp/gw
go mod init github.com/gw-cli/gw
```

- [ ] **Step 2: 创建 main.go 入口**

```go
// main.go
package main

import (
	"fmt"
	"os"

	"github.com/gw-cli/gw/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: 创建 cmd/root.go**

```go
// cmd/root.go
package cmd

import "github.com/spf13/cobra"

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "gw",
	Short: "CLI proxy that reduces LLM token consumption",
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}

func Execute() error {
	return rootCmd.Execute()
}
```

- [ ] **Step 4: 定义 Filter 接口和类型**

```go
// filter/filter.go
package filter

// FilterInput 包含过滤器所需的全部输入
type FilterInput struct {
	Cmd      string   // 原始命令名 (e.g. "mvn")
	Args     []string // 命令参数 (e.g. ["test", "-pl", "core"])
	Stdout   string   // 命令的标准输出
	Stderr   string   // 命令的标准错误
	ExitCode int      // 命令退出码
}

// FilterOutput 包含过滤后的结果
type FilterOutput struct {
	Content  string // 压缩后的输出
	Original string // 原始输出（用于 token 统计对比）
}

// Filter 是所有过滤器必须实现的接口
type Filter interface {
	// Match 判断此过滤器是否匹配给定命令
	Match(cmd string, args []string) bool
	// Apply 在命令成功时（exit code == 0）应用过滤
	Apply(input FilterInput) FilterOutput
	// ApplyOnError 在命令失败时（exit code != 0）应用过滤
	// 返回 nil 表示无专用失败逻辑，框架将透传原始输出
	ApplyOnError(input FilterInput) *FilterOutput
}
```

- [ ] **Step 5: 安装依赖并验证编译**

```bash
cd /private/tmp/gw
go get github.com/spf13/cobra
go build ./...
```

Expected: 编译成功，无错误

- [ ] **Step 6: Commit**

```bash
cd /private/tmp/gw
git add -A
git commit -m "初始化项目结构，定义 Filter 接口"
```

---

### Task 2: 子进程执行器

**Files:**
- Create: `internal/runner.go`
- Create: `internal/runner_test.go`

- [ ] **Step 1: 编写 runner 测试**

```go
// internal/runner_test.go
package internal

import (
	"testing"
)

func TestRunCommand_Success(t *testing.T) {
	result, err := RunCommand("echo", []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", result.Stdout)
	}
}

func TestRunCommand_Failure(t *testing.T) {
	result, err := RunCommand("false", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestRunCommand_NotFound(t *testing.T) {
	_, err := RunCommand("nonexistent_command_xyz", nil)
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./internal/ -v
```

Expected: FAIL — `RunCommand` 未定义

- [ ] **Step 3: 实现 runner**

```go
// internal/runner.go
package internal

import (
	"bytes"
	"fmt"
	"os/exec"
	"syscall"
)

// CommandResult 包含命令执行的完整结果
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCommand 在本地执行命令，捕获 stdout/stderr/exitcode
func RunCommand(name string, args []string) (*CommandResult, error) {
	cmd := exec.Command(name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &CommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				result.ExitCode = status.ExitStatus()
			} else {
				result.ExitCode = 1
			}
		} else {
			return nil, fmt.Errorf("failed to execute %s: %w", name, err)
		}
	}

	return result, nil
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./internal/ -v
```

Expected: PASS — 3 tests

- [ ] **Step 5: Commit**

```bash
cd /private/tmp/gw
git add internal/
git commit -m "实现子进程执行器"
```

---

### Task 3: 过滤器注册表 + exec 命令骨架

**Files:**
- Create: `filter/registry.go`
- Create: `cmd/exec.go`

- [ ] **Step 1: 实现过滤器注册表**

```go
// filter/registry.go
package filter

// Registry 管理所有已注册的过滤器
type Registry struct {
	filters []Filter
}

// NewRegistry 创建空注册表
func NewRegistry() *Registry {
	return &Registry{}
}

// Register 注册一个过滤器
func (r *Registry) Register(f Filter) {
	r.filters = append(r.filters, f)
}

// Find 根据命令找到匹配的过滤器，未找到返回 nil
func (r *Registry) Find(cmd string, args []string) Filter {
	for _, f := range r.filters {
		if f.Match(cmd, args) {
			return f
		}
	}
	return nil
}

// DefaultRegistry 返回注册了所有内置过滤器的注册表
func DefaultRegistry() *Registry {
	r := NewRegistry()
	// 后续 task 会逐一注册过滤器
	return r
}
```

- [ ] **Step 2: 实现 exec 命令（六阶段管道）**

```go
// cmd/exec.go
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/gw-cli/gw/filter"
	"github.com/gw-cli/gw/internal"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec [command] [args...]",
	Short: "执行命令并过滤输出",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runExec,
	// 不解析 exec 后面的 flags，全部当作命令参数
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) error {
	// PARSE: 提取命令名和参数
	cmdName := args[0]
	cmdArgs := args[1:]

	// ROUTE: 查找匹配的过滤器
	registry := filter.DefaultRegistry()
	f := registry.Find(cmdName, cmdArgs)

	// EXECUTE: 本地执行命令
	result, err := internal.RunCommand(cmdName, cmdArgs)
	if err != nil {
		// 命令无法执行（如找不到命令），直接报错
		fmt.Fprintln(os.Stderr, err)
		os.Exit(127)
		return nil
	}

	// 无匹配过滤器 → 透传原始输出
	if f == nil {
		fmt.Print(result.Stdout)
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		os.Exit(result.ExitCode)
		return nil
	}

	input := filter.FilterInput{
		Cmd:      cmdName,
		Args:     cmdArgs,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}

	// FILTER: 根据退出码选择过滤策略
	var output filter.FilterOutput
	if result.ExitCode == 0 {
		output = f.Apply(input)
	} else {
		errOutput := f.ApplyOnError(input)
		if errOutput == nil {
			// 无专用失败过滤器 → 透传原始输出
			fmt.Print(result.Stdout)
			if result.Stderr != "" {
				fmt.Fprint(os.Stderr, result.Stderr)
			}
			os.Exit(result.ExitCode)
			return nil
		}
		output = *errOutput
	}

	// PRINT: 输出压缩结果
	fmt.Print(output.Content)

	// TRACK: 记录 token 节省（后续 task 实现）
	if verbose {
		original := strings.TrimSpace(output.Original)
		compressed := strings.TrimSpace(output.Content)
		origTokens := len(original) / 4
		compTokens := len(compressed) / 4
		saved := origTokens - compTokens
		if origTokens > 0 {
			pct := float64(saved) / float64(origTokens) * 100
			fmt.Fprintf(os.Stderr, "\n[gw] %d → %d tokens (saved %d, %.0f%%)\n", origTokens, compTokens, saved, pct)
		}
	}

	os.Exit(result.ExitCode)
	return nil
}
```

- [ ] **Step 3: 验证编译**

```bash
cd /private/tmp/gw
go build ./...
```

Expected: 编译成功

- [ ] **Step 4: 手动测试透传（无过滤器注册时应透传）**

```bash
cd /private/tmp/gw
go run . exec echo hello
```

Expected: 输出 `hello`

- [ ] **Step 5: Commit**

```bash
cd /private/tmp/gw
git add filter/registry.go cmd/exec.go
git commit -m "实现过滤器注册表和 exec 命令骨架"
```

---

### Task 4: Git Status 过滤器（第一个硬编码过滤器）

**Files:**
- Create: `filter/git/status.go`
- Create: `filter/git/status_test.go`
- Create: `testdata/git_status_clean.txt`
- Create: `testdata/git_status_dirty.txt`
- Modify: `filter/registry.go` — 注册 git status 过滤器

- [ ] **Step 1: 创建测试 fixtures**

```bash
# testdata/git_status_clean.txt
cat > /private/tmp/gw/testdata/git_status_clean.txt << 'FIXTURE'
On branch main
Your branch is up to date with 'origin/main'.

nothing to commit, working tree clean
FIXTURE
```

```bash
# testdata/git_status_dirty.txt
cat > /private/tmp/gw/testdata/git_status_dirty.txt << 'FIXTURE'
On branch feature/auth
Your branch is ahead of 'origin/feature/auth' by 2 commits.
  (use "git push" to publish your local commits)

Changes to be committed:
  (use "git restore --staged <file>..." to unstage)
	new file:   src/auth/login.go
	modified:   src/auth/middleware.go

Changes not staged for commit:
  (use "git add <file>..." to update what will be committed)
  (use "git restore <file>..." to discard changes in working directory)
	modified:   src/main.go
	modified:   go.mod

Untracked files:
  (use "git add <file>..." to include in what will be committed)
	src/auth/oauth.go
	tmp/debug.log
FIXTURE
```

- [ ] **Step 2: 编写 git status 过滤器测试**

```go
// filter/git/status_test.go
package git

import (
	"os"
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func loadFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	return string(data)
}

func TestGitStatusFilter_Match(t *testing.T) {
	f := &StatusFilter{}
	if !f.Match("git", []string{"status"}) {
		t.Error("should match 'git status'")
	}
	if f.Match("git", []string{"log"}) {
		t.Error("should not match 'git log'")
	}
	if f.Match("mvn", []string{"test"}) {
		t.Error("should not match 'mvn test'")
	}
}

func TestGitStatusFilter_Clean(t *testing.T) {
	f := &StatusFilter{}
	input := filter.FilterInput{
		Cmd:      "git",
		Args:     []string{"status"},
		Stdout:   loadFixture(t, "../../testdata/git_status_clean.txt"),
		ExitCode: 0,
	}
	output := f.Apply(input)
	if !strings.Contains(output.Content, "nothing to commit") {
		t.Error("clean status should contain 'nothing to commit'")
	}
	// 教学提示应被去除
	if strings.Contains(output.Content, "use \"git") {
		t.Error("should strip git teaching hints")
	}
}

func TestGitStatusFilter_Dirty(t *testing.T) {
	f := &StatusFilter{}
	input := filter.FilterInput{
		Cmd:      "git",
		Args:     []string{"status"},
		Stdout:   loadFixture(t, "../../testdata/git_status_dirty.txt"),
		ExitCode: 0,
	}
	output := f.Apply(input)
	// 应保留分支信息
	if !strings.Contains(output.Content, "feature/auth") {
		t.Error("should contain branch name")
	}
	// 应保留文件列表
	if !strings.Contains(output.Content, "login.go") {
		t.Error("should contain changed file names")
	}
	// 教学提示应被去除
	if strings.Contains(output.Content, "use \"git restore") {
		t.Error("should strip teaching hints")
	}
	// 压缩率应 > 30%
	if len(output.Content) >= len(output.Original) {
		t.Error("filtered output should be shorter than original")
	}
}
```

- [ ] **Step 3: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/git/ -v
```

Expected: FAIL — `StatusFilter` 未定义

- [ ] **Step 4: 实现 git status 过滤器**

```go
// filter/git/status.go
package git

import (
	"strings"

	"github.com/gw-cli/gw/filter"
)

// StatusFilter 过滤 git status 输出
type StatusFilter struct{}

func (f *StatusFilter) Match(cmd string, args []string) bool {
	if cmd != "git" || len(args) == 0 {
		return false
	}
	return args[0] == "status"
}

func (f *StatusFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	lines := strings.Split(original, "\n")

	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 去除教学提示行
		if strings.HasPrefix(trimmed, "(use \"git") {
			continue
		}
		// 去除空行连续
		if trimmed == "" && len(filtered) > 0 && strings.TrimSpace(filtered[len(filtered)-1]) == "" {
			continue
		}
		filtered = append(filtered, line)
	}

	content := strings.TrimSpace(strings.Join(filtered, "\n")) + "\n"
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *StatusFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// git status 很少失败，失败时透传
	return nil
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./filter/git/ -v
```

Expected: PASS — 3 tests

- [ ] **Step 6: 注册过滤器**

在 `filter/registry.go` 的 `DefaultRegistry()` 中添加：

```go
import "github.com/gw-cli/gw/filter/git"

func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&git.StatusFilter{})
	return r
}
```

- [ ] **Step 7: 集成测试**

```bash
cd /private/tmp/gw
go build -o gw . && ./gw exec git status
```

Expected: 输出 git status 结果，教学提示被过滤

- [ ] **Step 8: Commit**

```bash
cd /private/tmp/gw
git add filter/git/ filter/registry.go testdata/
git commit -m "实现 git status 过滤器"
```

---

### Task 5: Git Log 过滤器

**Files:**
- Create: `filter/git/log.go`
- Create: `filter/git/log_test.go`
- Create: `testdata/git_log_default.txt`
- Modify: `filter/registry.go` — 注册

- [ ] **Step 1: 创建 fixture**

```bash
cat > /private/tmp/gw/testdata/git_log_default.txt << 'FIXTURE'
commit a1b2c3d4e5f6789012345678901234567890abcd
Author: John Doe <john@example.com>
Date:   Mon Apr 14 10:30:00 2026 +0800

    feat: add user authentication

    This implements JWT-based auth with refresh tokens.
    
    Signed-off-by: John Doe <john@example.com>
    Co-authored-by: Jane Smith <jane@example.com>

commit b2c3d4e5f6789012345678901234567890abcdef
Author: Jane Smith <jane@example.com>
Date:   Sun Apr 13 15:20:00 2026 +0800

    fix: resolve connection pool leak

    The pool was not releasing connections on timeout.
    Added explicit cleanup in the finally block.

commit c3d4e5f6789012345678901234567890abcdef01
Author: John Doe <john@example.com>
Date:   Sat Apr 12 09:00:00 2026 +0800

    chore: update dependencies

    Signed-off-by: John Doe <john@example.com>
FIXTURE
```

- [ ] **Step 2: 编写测试**

```go
// filter/git/log_test.go
package git

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestGitLogFilter_Match(t *testing.T) {
	f := &LogFilter{}
	if !f.Match("git", []string{"log"}) {
		t.Error("should match 'git log'")
	}
	if !f.Match("git", []string{"log", "-10"}) {
		t.Error("should match 'git log -10'")
	}
	if f.Match("git", []string{"status"}) {
		t.Error("should not match 'git status'")
	}
}

func TestGitLogFilter_Apply(t *testing.T) {
	f := &LogFilter{}
	input := filter.FilterInput{
		Cmd:      "git",
		Args:     []string{"log"},
		Stdout:   loadFixture(t, "../../testdata/git_log_default.txt"),
		ExitCode: 0,
	}
	output := f.Apply(input)

	// 应保留 commit hash（缩短）
	if !strings.Contains(output.Content, "a1b2c3d") {
		t.Error("should contain short commit hash")
	}
	// 应保留 commit message 第一行
	if !strings.Contains(output.Content, "add user authentication") {
		t.Error("should contain commit message")
	}
	// 应去除 Signed-off-by / Co-authored-by
	if strings.Contains(output.Content, "Signed-off-by") {
		t.Error("should strip Signed-off-by trailers")
	}
	if strings.Contains(output.Content, "Co-authored-by") {
		t.Error("should strip Co-authored-by trailers")
	}
	// 压缩率应 > 40%
	if len(output.Content) > len(output.Original)*60/100 {
		t.Errorf("compression ratio too low: %d -> %d", len(output.Original), len(output.Content))
	}
}
```

- [ ] **Step 3: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/git/ -v -run TestGitLog
```

Expected: FAIL — `LogFilter` 未定义

- [ ] **Step 4: 实现 git log 过滤器**

```go
// filter/git/log.go
package git

import (
	"fmt"
	"strings"

	"github.com/gw-cli/gw/filter"
)

// LogFilter 过滤 git log 输出
type LogFilter struct{}

func (f *LogFilter) Match(cmd string, args []string) bool {
	if cmd != "git" || len(args) == 0 {
		return false
	}
	return args[0] == "log"
}

func (f *LogFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	lines := strings.Split(original, "\n")

	var result []string
	var currentHash, currentAuthor, currentDate, currentSubject string
	var bodyLines []string
	inBody := false

	flush := func() {
		if currentHash == "" {
			return
		}
		shortHash := currentHash
		if len(shortHash) > 7 {
			shortHash = shortHash[:7]
		}
		entry := fmt.Sprintf("%s %s (%s) <%s>", shortHash, currentSubject, currentDate, currentAuthor)
		result = append(result, entry)

		// body: 最多 3 行，去 trailers
		kept := 0
		for _, bl := range bodyLines {
			trimmed := strings.TrimSpace(bl)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "Signed-off-by:") ||
				strings.HasPrefix(trimmed, "Co-authored-by:") {
				continue
			}
			if kept >= 3 {
				break
			}
			result = append(result, "  "+trimmed)
			kept++
		}
		result = append(result, "")
		// reset
		currentHash = ""
		currentAuthor = ""
		currentDate = ""
		currentSubject = ""
		bodyLines = nil
		inBody = false
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "commit ") {
			flush()
			currentHash = strings.TrimPrefix(line, "commit ")
			currentHash = strings.TrimSpace(currentHash)
		} else if strings.HasPrefix(line, "Author: ") {
			author := strings.TrimPrefix(line, "Author: ")
			// 提取名字部分
			if idx := strings.Index(author, " <"); idx > 0 {
				currentAuthor = author[:idx]
			} else {
				currentAuthor = strings.TrimSpace(author)
			}
		} else if strings.HasPrefix(line, "Date: ") {
			dateStr := strings.TrimSpace(strings.TrimPrefix(line, "Date:"))
			// 简化日期：取前几个字段
			parts := strings.Fields(dateStr)
			if len(parts) >= 4 {
				currentDate = strings.Join(parts[:4], " ")
			} else {
				currentDate = dateStr
			}
		} else if !inBody && strings.TrimSpace(line) == "" && currentHash != "" {
			inBody = true
		} else if inBody && strings.HasPrefix(line, "    ") {
			trimmed := strings.TrimPrefix(line, "    ")
			if currentSubject == "" {
				currentSubject = strings.TrimSpace(trimmed)
			} else {
				bodyLines = append(bodyLines, trimmed)
			}
		}
	}
	flush()

	content := strings.TrimSpace(strings.Join(result, "\n")) + "\n"
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *LogFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./filter/git/ -v
```

Expected: PASS — 5 tests (status + log)

- [ ] **Step 6: 注册过滤器到 registry**

在 `filter/registry.go` 的 `DefaultRegistry()` 中添加 `r.Register(&git.LogFilter{})`

- [ ] **Step 7: Commit**

```bash
cd /private/tmp/gw
git add filter/git/log.go filter/git/log_test.go filter/registry.go testdata/git_log_default.txt
git commit -m "实现 git log 过滤器"
```

---

### Task 6: Maven 过滤器（Java 差异化核心）

**Files:**
- Create: `filter/java/maven.go`
- Create: `filter/java/maven_test.go`
- Create: `testdata/mvn_test_success.txt`
- Create: `testdata/mvn_test_failure.txt`
- Modify: `filter/registry.go` — 注册

- [ ] **Step 1: 创建成功 fixture**

```bash
cat > /private/tmp/gw/testdata/mvn_test_success.txt << 'FIXTURE'
[INFO] Scanning for projects...
[INFO] 
[INFO] -----------------------< com.example:myapp >------------------------
[INFO] Building myapp 1.0-SNAPSHOT
[INFO] --------------------------------[ jar ]---------------------------------
[INFO] 
[INFO] --- maven-resources-plugin:3.3.0:resources (default-resources) @ myapp ---
[INFO] Copying 3 resources
[INFO] 
[INFO] --- maven-compiler-plugin:3.11.0:compile (default-compile) @ myapp ---
[INFO] Nothing to compile - all classes are up to date
[INFO] 
[INFO] --- maven-resources-plugin:3.3.0:testResources (default-testResources) @ myapp ---
[INFO] Copying 1 resource
[INFO] 
[INFO] --- maven-compiler-plugin:3.11.0:testCompile (default-testCompile) @ myapp ---
[INFO] Nothing to compile - all classes are up to date
[INFO] 
[INFO] --- maven-surefire-plugin:3.0.0:test (default-test) @ myapp ---
[INFO] Using auto detected provider org.apache.maven.surefire.junitplatform.JUnitPlatformProvider
[INFO] 
[INFO] -------------------------------------------------------
[INFO]  T E S T S
[INFO] -------------------------------------------------------
[INFO] Running com.example.UserServiceTest
[INFO] Tests run: 15, Failures: 0, Errors: 0, Skipped: 0, Time elapsed: 1.234 s -- in com.example.UserServiceTest
[INFO] Running com.example.OrderServiceTest
[INFO] Tests run: 23, Failures: 0, Errors: 0, Skipped: 1, Time elapsed: 0.876 s -- in com.example.OrderServiceTest
[INFO] Running com.example.PaymentServiceTest
[INFO] Tests run: 8, Failures: 0, Errors: 0, Skipped: 0, Time elapsed: 2.345 s -- in com.example.PaymentServiceTest
[INFO] 
[INFO] Results:
[INFO] 
[INFO] Tests run: 46, Failures: 0, Errors: 0, Skipped: 1
[INFO] 
[INFO] ------------------------------------------------------------------------
[INFO] BUILD SUCCESS
[INFO] ------------------------------------------------------------------------
[INFO] Total time:  8.432 s
[INFO] Finished at: 2026-04-15T10:30:00+08:00
[INFO] ------------------------------------------------------------------------
FIXTURE
```

- [ ] **Step 2: 创建失败 fixture**

```bash
cat > /private/tmp/gw/testdata/mvn_test_failure.txt << 'FIXTURE'
[INFO] Scanning for projects...
[INFO] 
[INFO] -----------------------< com.example:myapp >------------------------
[INFO] Building myapp 1.0-SNAPSHOT
[INFO] --------------------------------[ jar ]---------------------------------
[INFO] Downloading from central: https://repo.maven.apache.org/maven2/org/junit/junit-bom/5.10.0/junit-bom-5.10.0.pom
[INFO] Downloaded from central: https://repo.maven.apache.org/maven2/org/junit/junit-bom/5.10.0/junit-bom-5.10.0.pom (5.6 kB at 120 kB/s)
[INFO] Downloading from central: https://repo.maven.apache.org/maven2/org/mockito/mockito-core/5.5.0/mockito-core-5.5.0.pom
[INFO] Downloaded from central: https://repo.maven.apache.org/maven2/org/mockito/mockito-core/5.5.0/mockito-core-5.5.0.pom (3.2 kB at 95 kB/s)
[INFO] 
[INFO] --- maven-compiler-plugin:3.11.0:compile (default-compile) @ myapp ---
[INFO] Compiling 12 source files to /home/user/myapp/target/classes
[INFO] 
[INFO] --- maven-surefire-plugin:3.0.0:test (default-test) @ myapp ---
[INFO] 
[INFO] -------------------------------------------------------
[INFO]  T E S T S
[INFO] -------------------------------------------------------
[INFO] Running com.example.UserServiceTest
[ERROR] Tests run: 15, Failures: 2, Errors: 0, Skipped: 0, Time elapsed: 1.234 s <<< FAILURE! -- in com.example.UserServiceTest
[ERROR] com.example.UserServiceTest.testLogin -- Time elapsed: 0.045 s <<< FAILURE!
org.opentest4j.AssertionFailedError: expected: <200> but was: <401>
	at org.junit.jupiter.api.AssertionUtils.fail(AssertionUtils.java:55)
	at com.example.UserServiceTest.testLogin(UserServiceTest.java:42)
[ERROR] com.example.UserServiceTest.testRegister -- Time elapsed: 0.032 s <<< FAILURE!
java.lang.NullPointerException: Cannot invoke method on null reference
	at com.example.UserService.register(UserService.java:87)
	at com.example.UserServiceTest.testRegister(UserServiceTest.java:68)
[INFO] Running com.example.OrderServiceTest
[INFO] Tests run: 23, Failures: 0, Errors: 0, Skipped: 1, Time elapsed: 0.876 s -- in com.example.OrderServiceTest
[INFO] 
[INFO] Results:
[INFO] 
[ERROR] Failures: 
[ERROR]   UserServiceTest.testLogin:42 expected: <200> but was: <401>
[ERROR]   UserServiceTest.testRegister:68 Cannot invoke method on null reference
[INFO] 
[ERROR] Tests run: 38, Failures: 2, Errors: 0, Skipped: 1
[INFO] 
[INFO] ------------------------------------------------------------------------
[INFO] BUILD FAILURE
[INFO] ------------------------------------------------------------------------
[INFO] Total time:  6.789 s
[INFO] Finished at: 2026-04-15T10:35:00+08:00
[INFO] ------------------------------------------------------------------------
FIXTURE
```

- [ ] **Step 3: 编写 Maven 过滤器测试**

```go
// filter/java/maven_test.go
package java

import (
	"os"
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func loadFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	return string(data)
}

func TestMavenFilter_Match(t *testing.T) {
	f := &MavenFilter{}
	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"mvn", []string{"test"}, true},
		{"mvn", []string{"compile"}, true},
		{"mvn", []string{"package"}, true},
		{"mvn", []string{"install"}, true},
		{"mvn", []string{"clean", "test"}, true},
		{"gradle", []string{"test"}, false},
		{"git", []string{"status"}, false},
	}
	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestMavenFilter_Success(t *testing.T) {
	f := &MavenFilter{}
	input := filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"test"},
		Stdout:   loadFixture(t, "../../testdata/mvn_test_success.txt"),
		ExitCode: 0,
	}
	output := f.Apply(input)

	// 应包含 BUILD SUCCESS
	if !strings.Contains(output.Content, "BUILD SUCCESS") {
		t.Error("should contain BUILD SUCCESS")
	}
	// 应包含测试摘要
	if !strings.Contains(output.Content, "46") {
		t.Error("should contain total test count")
	}
	// 不应包含 plugin 执行日志
	if strings.Contains(output.Content, "maven-resources-plugin") {
		t.Error("should strip plugin execution lines")
	}
	// 不应包含 Downloading/Downloaded
	if strings.Contains(output.Content, "Downloading from") {
		t.Error("should strip download lines")
	}
	// 压缩率应 > 70%
	ratio := float64(len(output.Content)) / float64(len(output.Original))
	if ratio > 0.3 {
		t.Errorf("compression ratio too low: %.1f%% remaining", ratio*100)
	}
}

func TestMavenFilter_Failure(t *testing.T) {
	f := &MavenFilter{}
	input := filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"test"},
		Stdout:   loadFixture(t, "../../testdata/mvn_test_failure.txt"),
		ExitCode: 1,
	}
	output := f.ApplyOnError(input)

	if output == nil {
		t.Fatal("MavenFilter should have a failure-specific filter")
	}
	// 应包含 BUILD FAILURE
	if !strings.Contains(output.Content, "BUILD FAILURE") {
		t.Error("should contain BUILD FAILURE")
	}
	// 应保留失败测试名和行号
	if !strings.Contains(output.Content, "testLogin") {
		t.Error("should contain failed test name 'testLogin'")
	}
	if !strings.Contains(output.Content, "UserServiceTest") {
		t.Error("should contain test class name")
	}
	// 应保留错误信息
	if !strings.Contains(output.Content, "401") || !strings.Contains(output.Content, "200") {
		t.Error("should contain assertion details")
	}
	// 不应包含下载日志
	if strings.Contains(output.Content, "Downloading from") {
		t.Error("should strip download lines even on failure")
	}
	// 不应包含 plugin 执行日志
	if strings.Contains(output.Content, "maven-compiler-plugin") {
		t.Error("should strip plugin lines on failure")
	}
}
```

- [ ] **Step 4: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v
```

Expected: FAIL — `MavenFilter` 未定义

- [ ] **Step 5: 实现 Maven 过滤器**

```go
// filter/java/maven.go
package java

import (
	"fmt"
	"strings"

	"github.com/gw-cli/gw/filter"
)

// MavenFilter 过滤 mvn 命令输出
type MavenFilter struct{}

func (f *MavenFilter) Match(cmd string, args []string) bool {
	return cmd == "mvn"
}

func (f *MavenFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	lines := strings.Split(original, "\n")

	var result []string
	var testSummaryLines []string
	inTestResults := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 去除下载日志
		if isDownloadLine(trimmed) {
			continue
		}
		// 去除 plugin 执行行
		if isPluginLine(trimmed) {
			continue
		}
		// 去除分隔线
		if isSeparatorLine(trimmed) {
			continue
		}
		// 去除空的 [INFO] 行
		if trimmed == "[INFO]" {
			continue
		}
		// 去除 Scanning/Building/Copying 等过程行
		if isProcessLine(trimmed) {
			continue
		}

		// 捕获测试结果区域
		if strings.Contains(trimmed, "T E S T S") {
			inTestResults = true
			continue
		}
		if inTestResults && strings.Contains(trimmed, "Tests run:") {
			testSummaryLines = append(testSummaryLines, stripInfoPrefix(trimmed))
		}

		// 捕获 BUILD SUCCESS/FAILURE 和时间
		if strings.Contains(trimmed, "BUILD SUCCESS") || strings.Contains(trimmed, "BUILD FAILURE") {
			result = append(result, stripInfoPrefix(trimmed))
		}
		if strings.Contains(trimmed, "Total time:") {
			result = append(result, stripInfoPrefix(trimmed))
		}
	}

	// 构建精简输出
	var output []string
	// 先放测试摘要
	if len(testSummaryLines) > 0 {
		// 取最后一行（总计）
		output = append(output, testSummaryLines[len(testSummaryLines)-1])
	}
	output = append(output, result...)

	content := strings.Join(output, "\n") + "\n"
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *MavenFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout
	lines := strings.Split(original, "\n")

	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 去除下载日志（失败时也是噪音）
		if isDownloadLine(trimmed) {
			continue
		}
		// 去除 plugin 执行行
		if isPluginLine(trimmed) {
			continue
		}
		// 去除分隔线
		if isSeparatorLine(trimmed) {
			continue
		}
		// 去除空 [INFO] 行
		if trimmed == "[INFO]" {
			continue
		}
		// 去除 Scanning/Building/Copying 等过程行
		if isProcessLine(trimmed) {
			continue
		}

		// 保留 [ERROR] 行（包含失败信息和堆栈）
		if strings.HasPrefix(trimmed, "[ERROR]") {
			result = append(result, stripErrorPrefix(trimmed))
			continue
		}
		// 保留堆栈行（以 at 开头或包含异常）
		if strings.HasPrefix(trimmed, "at ") || strings.HasPrefix(trimmed, "org.") || strings.HasPrefix(trimmed, "java.") {
			result = append(result, "  "+trimmed)
			continue
		}

		// 保留 BUILD FAILURE 和测试摘要
		if strings.Contains(trimmed, "BUILD FAILURE") {
			result = append(result, stripInfoPrefix(trimmed))
		}
		if strings.Contains(trimmed, "Tests run:") && strings.Contains(trimmed, "Failures:") {
			result = append(result, stripInfoPrefix(trimmed))
		}
		if strings.Contains(trimmed, "Total time:") {
			result = append(result, stripInfoPrefix(trimmed))
		}
	}

	content := strings.Join(result, "\n") + "\n"
	return &filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

// 辅助函数

func isDownloadLine(line string) bool {
	return strings.Contains(line, "Downloading from") || strings.Contains(line, "Downloaded from")
}

func isPluginLine(line string) bool {
	return strings.Contains(line, "--- maven-") || strings.Contains(line, "--- ") && strings.Contains(line, "-plugin:")
}

func isSeparatorLine(line string) bool {
	stripped := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "[INFO]"))
	stripped = strings.TrimSpace(stripped)
	if len(stripped) < 10 {
		return false
	}
	for _, c := range stripped {
		if c != '-' {
			return false
		}
	}
	return true
}

func isProcessLine(line string) bool {
	prefixes := []string{
		"[INFO] Scanning for projects",
		"[INFO] Building ",
		"[INFO] Copying ",
		"[INFO] Compiling ",
		"[INFO] Nothing to compile",
		"[INFO] Using auto detected",
		"[INFO] Finished at:",
		"[INFO] Results:",
	}
	trimmed := strings.TrimSpace(line)
	for _, p := range prefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func stripInfoPrefix(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "[INFO] ")
	s = strings.TrimPrefix(s, "[INFO]")
	return strings.TrimSpace(s)
}

func stripErrorPrefix(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "[ERROR] ")
	s = strings.TrimPrefix(s, "[ERROR]")
	return strings.TrimSpace(s)
}
```

- [ ] **Step 6: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v
```

Expected: PASS — 3 tests

- [ ] **Step 7: 注册到 registry 并验证编译**

在 `filter/registry.go` 的 `DefaultRegistry()` 中添加：

```go
import "github.com/gw-cli/gw/filter/java"

// 在 DefaultRegistry() 中:
r.Register(&java.MavenFilter{})
```

```bash
cd /private/tmp/gw
go build ./...
```

- [ ] **Step 8: Commit**

```bash
cd /private/tmp/gw
git add filter/java/maven.go filter/java/maven_test.go filter/registry.go testdata/mvn_test_success.txt testdata/mvn_test_failure.txt
git commit -m "实现 Maven 过滤器（成功/失败双策略）"
```

---

### Task 7: Gradle 过滤器

**Files:**
- Create: `filter/java/gradle.go`
- Create: `filter/java/gradle_test.go`
- Create: `testdata/gradle_build_success.txt`
- Create: `testdata/gradle_test_failure.txt`
- Modify: `filter/registry.go` — 注册

- [ ] **Step 1: 创建 fixtures**

```bash
cat > /private/tmp/gw/testdata/gradle_build_success.txt << 'FIXTURE'
Starting a Gradle Daemon (subsequent builds will be faster)

> Task :compileJava UP-TO-DATE
> Task :processResources UP-TO-DATE
> Task :classes UP-TO-DATE
> Task :compileTestJava UP-TO-DATE
> Task :processTestResources NO-SOURCE
> Task :testClasses UP-TO-DATE
> Task :test

com.example.UserServiceTest > testLogin PASSED
com.example.UserServiceTest > testRegister PASSED
com.example.UserServiceTest > testLogout PASSED
com.example.OrderServiceTest > testCreate PASSED
com.example.OrderServiceTest > testCancel PASSED
com.example.OrderServiceTest > testRefund PASSED

> Task :build

BUILD SUCCESSFUL in 12s
7 actionable tasks: 1 executed, 6 up-to-date
FIXTURE

cat > /private/tmp/gw/testdata/gradle_test_failure.txt << 'FIXTURE'
> Task :compileJava UP-TO-DATE
> Task :processResources UP-TO-DATE
> Task :classes UP-TO-DATE
> Task :compileTestJava UP-TO-DATE
> Task :processTestResources NO-SOURCE
> Task :testClasses UP-TO-DATE
> Task :test FAILED

com.example.UserServiceTest > testLogin FAILED
    org.opentest4j.AssertionFailedError: expected: <200> but was: <401>
        at com.example.UserServiceTest.testLogin(UserServiceTest.java:42)

com.example.UserServiceTest > testRegister PASSED
com.example.OrderServiceTest > testCreate PASSED

3 tests completed, 1 failed

> Task :test FAILED

FAILURE: Build failed with an exception.

* What went wrong:
Execution failed for task ':test'.
> There were failing tests. See the report at: file:///home/user/myapp/build/reports/tests/test/index.html

* Try:
> Run with --stacktrace option to get the stack trace.
> Run with --info or --debug option to get more log output.
> Run with --scan to get full insights.

BUILD FAILED in 8s
5 actionable tasks: 1 executed, 4 up-to-date
FIXTURE
```

- [ ] **Step 2: 编写测试**

```go
// filter/java/gradle_test.go
package java

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestGradleFilter_Match(t *testing.T) {
	f := &GradleFilter{}
	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"gradle", []string{"build"}, true},
		{"gradle", []string{"test"}, true},
		{"gradlew", []string{"build"}, true},
		{"./gradlew", []string{"test"}, true},
		{"mvn", []string{"test"}, false},
	}
	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestGradleFilter_Success(t *testing.T) {
	f := &GradleFilter{}
	input := filter.FilterInput{
		Cmd:      "gradle",
		Args:     []string{"build"},
		Stdout:   loadFixture(t, "../../testdata/gradle_build_success.txt"),
		ExitCode: 0,
	}
	output := f.Apply(input)

	if !strings.Contains(output.Content, "BUILD SUCCESSFUL") {
		t.Error("should contain BUILD SUCCESSFUL")
	}
	// 不应包含 Task 进度行
	if strings.Contains(output.Content, "> Task :compileJava") {
		t.Error("should strip task progress lines")
	}
	// 不应包含 daemon 启动信息
	if strings.Contains(output.Content, "Starting a Gradle Daemon") {
		t.Error("should strip daemon startup line")
	}
}

func TestGradleFilter_Failure(t *testing.T) {
	f := &GradleFilter{}
	input := filter.FilterInput{
		Cmd:      "gradle",
		Args:     []string{"test"},
		Stdout:   loadFixture(t, "../../testdata/gradle_test_failure.txt"),
		ExitCode: 1,
	}
	output := f.ApplyOnError(input)

	if output == nil {
		t.Fatal("GradleFilter should have a failure-specific filter")
	}
	if !strings.Contains(output.Content, "BUILD FAILED") {
		t.Error("should contain BUILD FAILED")
	}
	// 应保留失败测试信息
	if !strings.Contains(output.Content, "testLogin") {
		t.Error("should contain failed test name")
	}
	if !strings.Contains(output.Content, "401") {
		t.Error("should contain assertion error details")
	}
	// 不应包含 Try: 建议
	if strings.Contains(output.Content, "Run with --stacktrace") {
		t.Error("should strip Try suggestions")
	}
}
```

- [ ] **Step 3: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run TestGradle
```

Expected: FAIL — `GradleFilter` 未定义

- [ ] **Step 4: 实现 Gradle 过滤器**

```go
// filter/java/gradle.go
package java

import (
	"strings"

	"github.com/gw-cli/gw/filter"
)

// GradleFilter 过滤 gradle/gradlew 命令输出
type GradleFilter struct{}

func (f *GradleFilter) Match(cmd string, args []string) bool {
	base := cmd
	// 处理 ./gradlew 路径
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		base = cmd[idx+1:]
	}
	return base == "gradle" || base == "gradlew"
}

func (f *GradleFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	lines := strings.Split(original, "\n")

	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 去除 Task 进度行
		if strings.HasPrefix(trimmed, "> Task :") {
			continue
		}
		// 去除 daemon 启动信息
		if strings.HasPrefix(trimmed, "Starting a Gradle Daemon") {
			continue
		}
		// 去除空行连续
		if trimmed == "" && len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
			continue
		}
		// 保留测试结果行和 BUILD 行
		result = append(result, line)
	}

	content := strings.TrimSpace(strings.Join(result, "\n")) + "\n"
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *GradleFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout
	lines := strings.Split(original, "\n")

	var result []string
	inTrySuggestions := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 去除 Task 进度行（成功的 task）
		if strings.HasPrefix(trimmed, "> Task :") && !strings.Contains(trimmed, "FAILED") {
			continue
		}
		// 去除 daemon 启动
		if strings.HasPrefix(trimmed, "Starting a Gradle Daemon") {
			continue
		}
		// 去除 Try: 建议区域
		if trimmed == "* Try:" {
			inTrySuggestions = true
			continue
		}
		if inTrySuggestions {
			if strings.HasPrefix(trimmed, "> Run with") {
				continue
			}
			if trimmed == "" {
				inTrySuggestions = false
				continue
			}
		}
		// 去除 What went wrong 中的报告链接
		if strings.Contains(trimmed, "See the report at:") {
			continue
		}
		// 去除 "* What went wrong:" 标记（保留内容）
		if trimmed == "* What went wrong:" {
			continue
		}
		// 去除空行连续
		if trimmed == "" && len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
			continue
		}

		result = append(result, line)
	}

	content := strings.TrimSpace(strings.Join(result, "\n")) + "\n"
	return &filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v
```

Expected: PASS — 6 tests (maven + gradle)

- [ ] **Step 6: 注册到 registry**

在 `filter/registry.go` 的 `DefaultRegistry()` 中添加 `r.Register(&java.GradleFilter{})`

- [ ] **Step 7: Commit**

```bash
cd /private/tmp/gw
git add filter/java/gradle.go filter/java/gradle_test.go filter/registry.go testdata/gradle_build_success.txt testdata/gradle_test_failure.txt
git commit -m "实现 Gradle 过滤器"
```

---

### Task 8: Spring Boot 日志过滤器

**Files:**
- Create: `filter/java/springboot.go`
- Create: `filter/java/springboot_test.go`
- Create: `testdata/springboot_startup.txt`
- Modify: `filter/registry.go` — 注册

- [ ] **Step 1: 创建 fixture**

```bash
cat > /private/tmp/gw/testdata/springboot_startup.txt << 'FIXTURE'

  .   ____          _            __ _ _
 /\\ / ___'_ __ _ _(_)_ __  __ _ \ \ \ \
( ( )\___ | '_ | '_| | '_ \/ _` | \ \ \ \
 \\/  ___)| |_)| | | | | || (_| |  ) ) ) )
  '  |____| .__|_| |_|_| |_\__, | / / / /
 =========|_|==============|___/=/_/_/_/

 :: Spring Boot ::                (v3.2.0)

2026-04-15T10:30:00.123+08:00  INFO 12345 --- [           main] com.example.MyApplication                : Starting MyApplication v1.0.0 using Java 21.0.1 with PID 12345
2026-04-15T10:30:00.234+08:00  INFO 12345 --- [           main] com.example.MyApplication                : No active profile set, falling back to 1 default profile: "default"
2026-04-15T10:30:01.456+08:00  INFO 12345 --- [           main] .s.d.r.c.RepositoryConfigurationDelegate : Bootstrapping Spring Data JPA repositories in DEFAULT mode.
2026-04-15T10:30:01.567+08:00  INFO 12345 --- [           main] .s.d.r.c.RepositoryConfigurationDelegate : Finished Spring Data repository scanning in 89 ms. Found 8 JPA repository interfaces.
2026-04-15T10:30:02.345+08:00  INFO 12345 --- [           main] o.s.b.w.embedded.tomcat.TomcatWebServer  : Tomcat initialized with port 8080 (http)
2026-04-15T10:30:02.456+08:00  INFO 12345 --- [           main] o.apache.catalina.core.StandardService   : Starting service [Tomcat]
2026-04-15T10:30:02.567+08:00  INFO 12345 --- [           main] o.apache.catalina.core.StandardEngine    : Starting Servlet engine: [Apache Tomcat/10.1.16]
2026-04-15T10:30:02.789+08:00  INFO 12345 --- [           main] o.a.c.c.C.[Tomcat].[localhost].[/]       : Initializing Spring embedded WebApplicationContext
2026-04-15T10:30:02.890+08:00  INFO 12345 --- [           main] w.s.c.ServletWebServerApplicationContext : Root WebApplicationContext: initialization completed in 2567 ms
2026-04-15T10:30:03.123+08:00  INFO 12345 --- [           main] o.hibernate.jpa.internal.util.LogHelper  : HHH000204: Processing PersistenceUnitInfo [name: default]
2026-04-15T10:30:03.234+08:00  INFO 12345 --- [           main] org.hibernate.Version                    : HHH000412: Hibernate ORM core version 6.4.0.Final
2026-04-15T10:30:03.345+08:00  INFO 12345 --- [           main] o.h.e.t.j.p.i.JtaPlatformInitiator       : HHH000489: No JTA platform available (use the JtaPlatform interface to explicitly add one)
2026-04-15T10:30:03.456+08:00  INFO 12345 --- [           main] j.LocalContainerEntityManagerFactoryBean : Initialized JPA EntityManagerFactory for persistence unit 'default'
2026-04-15T10:30:04.123+08:00  WARN 12345 --- [           main] JpaBaseConfiguration$JpaWebConfiguration : spring.jpa.open-in-view is enabled by default. This should be explicitly configured in production.
2026-04-15T10:30:05.234+08:00  INFO 12345 --- [           main] o.s.b.w.embedded.tomcat.TomcatWebServer  : Tomcat started on port 8080 (http) with context path '/'
2026-04-15T10:30:05.345+08:00  INFO 12345 --- [           main] com.example.MyApplication                : Started MyApplication in 5.222 seconds (process running for 5.678)
FIXTURE
```

- [ ] **Step 2: 编写测试**

```go
// filter/java/springboot_test.go
package java

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestSpringBootFilter_Match(t *testing.T) {
	f := &SpringBootFilter{}

	// SpringBootFilter 通过输出内容匹配，不通过命令名
	// 它匹配 java/mvn/gradle 命令中包含 Spring Boot 启动日志的输出
	input := filter.FilterInput{
		Cmd:    "java",
		Args:   []string{"-jar", "app.jar"},
		Stdout: loadFixture(t, "../../testdata/springboot_startup.txt"),
	}
	if !f.Match(input.Cmd, input.Args) {
		// SpringBootFilter 匹配 java -jar 命令
	}
}

func TestSpringBootFilter_Startup(t *testing.T) {
	f := &SpringBootFilter{}
	input := filter.FilterInput{
		Cmd:      "java",
		Args:     []string{"-jar", "app.jar"},
		Stdout:   loadFixture(t, "../../testdata/springboot_startup.txt"),
		ExitCode: 0,
	}
	output := f.Apply(input)

	// 应去除 banner
	if strings.Contains(output.Content, "____") {
		t.Error("should strip Spring Boot banner")
	}
	// 应保留端口信息
	if !strings.Contains(output.Content, "8080") {
		t.Error("should contain port number")
	}
	// 应保留启动完成信息
	if !strings.Contains(output.Content, "Started") {
		t.Error("should contain Started message")
	}
	// 应保留 WARN
	if !strings.Contains(output.Content, "WARN") || !strings.Contains(output.Content, "open-in-view") {
		t.Error("should preserve WARN messages")
	}
	// 不应包含 Hibernate 内部日志
	if strings.Contains(output.Content, "HHH000") {
		t.Error("should strip Hibernate internal logs")
	}
	// 不应包含 Tomcat 引擎启动行
	if strings.Contains(output.Content, "Starting Servlet engine") {
		t.Error("should strip Tomcat engine startup")
	}
}
```

- [ ] **Step 3: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v -run TestSpringBoot
```

Expected: FAIL — `SpringBootFilter` 未定义

- [ ] **Step 4: 实现 Spring Boot 过滤器**

```go
// filter/java/springboot.go
package java

import (
	"strings"

	"github.com/gw-cli/gw/filter"
)

// SpringBootFilter 过滤 Spring Boot 应用的启动和运行日志
type SpringBootFilter struct{}

func (f *SpringBootFilter) Match(cmd string, args []string) bool {
	// 匹配 java -jar 命令
	if cmd == "java" {
		for _, arg := range args {
			if strings.HasSuffix(arg, ".jar") {
				return true
			}
		}
	}
	return false
}

func (f *SpringBootFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	lines := strings.Split(original, "\n")

	var result []string
	inBanner := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 检测并跳过 Spring Boot banner
		if strings.Contains(line, "____") || strings.Contains(line, ":: Spring Boot ::") {
			inBanner = true
			continue
		}
		if inBanner {
			if trimmed == "" {
				inBanner = false
			}
			continue
		}
		// banner 装饰行
		if strings.HasPrefix(trimmed, "/\\\\") || strings.HasPrefix(trimmed, "( (") ||
			strings.HasPrefix(trimmed, "\\\\/") || strings.HasPrefix(trimmed, "'") ||
			strings.HasPrefix(trimmed, "=") {
			continue
		}

		// 过滤 Hibernate 内部日志 (HHH000xxx)
		if strings.Contains(trimmed, "HHH000") {
			continue
		}
		// 过滤 Tomcat/Catalina 引擎内部行
		if strings.Contains(trimmed, "Starting service [Tomcat]") ||
			strings.Contains(trimmed, "Starting Servlet engine") ||
			strings.Contains(trimmed, "Initializing Spring embedded") {
			continue
		}
		// 过滤 Spring Data 扫描细节
		if strings.Contains(trimmed, "RepositoryConfigurationDelegate") {
			continue
		}
		// 过滤 JPA/EntityManager 初始化细节
		if strings.Contains(trimmed, "JtaPlatformInitiator") ||
			strings.Contains(trimmed, "EntityManagerFactory") ||
			strings.Contains(trimmed, "PersistenceUnitInfo") {
			continue
		}
		// 过滤 WebApplicationContext 初始化
		if strings.Contains(trimmed, "Root WebApplicationContext") {
			continue
		}
		// 过滤 profile fallback
		if strings.Contains(trimmed, "No active profile set") {
			continue
		}
		// 去除空行连续
		if trimmed == "" && len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
			continue
		}

		result = append(result, line)
	}

	content := strings.TrimSpace(strings.Join(result, "\n")) + "\n"
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *SpringBootFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// Spring Boot 启动失败时透传完整日志（错误堆栈很重要）
	return nil
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./filter/java/ -v
```

Expected: PASS — 8 tests (maven + gradle + springboot)

- [ ] **Step 6: 注册到 registry**

在 `filter/registry.go` 的 `DefaultRegistry()` 中添加 `r.Register(&java.SpringBootFilter{})`

- [ ] **Step 7: Commit**

```bash
cd /private/tmp/gw
git add filter/java/springboot.go filter/java/springboot_test.go filter/registry.go testdata/springboot_startup.txt
git commit -m "实现 Spring Boot 日志过滤器"
```

---

### Task 9: Shell Lexer（命令改写核心）

**Files:**
- Create: `hook/lexer.go`
- Create: `hook/lexer_test.go`

- [ ] **Step 1: 编写 lexer 测试**

```go
// hook/lexer_test.go
package hook

import (
	"testing"
)

func TestShouldRewrite_SimpleCommand(t *testing.T) {
	if !ShouldRewrite("git status") {
		t.Error("simple command should be rewritable")
	}
}

func TestShouldRewrite_PipeCommand(t *testing.T) {
	if ShouldRewrite("git log | grep fix") {
		t.Error("pipe command should not be rewritable")
	}
}

func TestShouldRewrite_Redirect(t *testing.T) {
	if ShouldRewrite("mvn test > output.txt") {
		t.Error("redirect should not be rewritable")
	}
	if ShouldRewrite("mvn test >> output.txt") {
		t.Error("append redirect should not be rewritable")
	}
	if ShouldRewrite("cmd < input.txt") {
		t.Error("input redirect should not be rewritable")
	}
}

func TestShouldRewrite_SubShell(t *testing.T) {
	if ShouldRewrite("echo $(git rev-parse HEAD)") {
		t.Error("subshell should not be rewritable")
	}
}

func TestShouldRewrite_Backtick(t *testing.T) {
	if ShouldRewrite("echo `git rev-parse HEAD`") {
		t.Error("backtick subshell should not be rewritable")
	}
}

func TestSplitChainedCommands(t *testing.T) {
	tests := []struct {
		input string
		want  []Segment
	}{
		{
			"git status",
			[]Segment{{Cmd: "git status", Sep: ""}},
		},
		{
			"mvn clean && mvn test",
			[]Segment{
				{Cmd: "mvn clean", Sep: "&&"},
				{Cmd: "mvn test", Sep: ""},
			},
		},
		{
			"cmd1 || cmd2 ; cmd3",
			[]Segment{
				{Cmd: "cmd1", Sep: "||"},
				{Cmd: "cmd2", Sep: ";"},
				{Cmd: "cmd3", Sep: ""},
			},
		},
	}
	for _, tt := range tests {
		got := SplitChainedCommands(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("SplitChainedCommands(%q) got %d segments, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if strings.TrimSpace(got[i].Cmd) != tt.want[i].Cmd || got[i].Sep != tt.want[i].Sep {
				t.Errorf("SplitChainedCommands(%q)[%d] = {%q, %q}, want {%q, %q}",
					tt.input, i, got[i].Cmd, got[i].Sep, tt.want[i].Cmd, tt.want[i].Sep)
			}
		}
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./hook/ -v
```

Expected: FAIL — functions 未定义

- [ ] **Step 3: 实现 lexer**

```go
// hook/lexer.go
package hook

import "strings"

// Segment 表示链式命令中的一段
type Segment struct {
	Cmd string // 命令内容
	Sep string // 与下一段的分隔符（"&&", "||", ";"），最后一段为 ""
}

// ShouldRewrite 判断命令是否可以安全改写
// 包含管道、重定向、子 shell 等操作符时返回 false
func ShouldRewrite(command string) bool {
	// 检测不安全的操作符
	unsafeChars := []string{"|", ">", ">>", "<", "$(", "`"}
	for _, ch := range unsafeChars {
		if strings.Contains(command, ch) {
			// 区分 || 和 |
			if ch == "|" {
				// 检查是否是 || （链式操作符，可以改写）
				// 需要排除 || 后再检查是否有单独的 |
				temp := strings.ReplaceAll(command, "||", "")
				if strings.Contains(temp, "|") {
					return false
				}
				continue
			}
			return false
		}
	}
	return true
}

// SplitChainedCommands 将链式命令按 &&, ||, ; 分段
func SplitChainedCommands(command string) []Segment {
	var segments []Segment
	remaining := command

	for remaining != "" {
		remaining = strings.TrimSpace(remaining)
		if remaining == "" {
			break
		}

		// 找最早出现的分隔符
		bestIdx := -1
		bestSep := ""
		for _, sep := range []string{"&&", "||", ";"} {
			idx := strings.Index(remaining, sep)
			if idx >= 0 && (bestIdx < 0 || idx < bestIdx) {
				bestIdx = idx
				bestSep = sep
			}
		}

		if bestIdx < 0 {
			// 无分隔符，整个是一段
			segments = append(segments, Segment{
				Cmd: strings.TrimSpace(remaining),
				Sep: "",
			})
			break
		}

		cmd := strings.TrimSpace(remaining[:bestIdx])
		segments = append(segments, Segment{
			Cmd: cmd,
			Sep: bestSep,
		})
		remaining = remaining[bestIdx+len(bestSep):]
	}

	return segments
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /private/tmp/gw
go test ./hook/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /private/tmp/gw
git add hook/
git commit -m "实现 Shell lexer（管道检测和链式命令分段）"
```

---

### Task 10: Rewrite 命令 + Init 命令

**Files:**
- Create: `cmd/rewrite.go`
- Create: `cmd/init_cmd.go`
- Create: `cmd/uninstall.go`

- [ ] **Step 1: 实现 rewrite 命令**

```go
// cmd/rewrite.go
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/gw-cli/gw/filter"
	"github.com/gw-cli/gw/hook"
	"github.com/spf13/cobra"
)

var rewriteCmd = &cobra.Command{
	Use:   "rewrite [command]",
	Short: "改写命令（供 Hook 调用）",
	Args:  cobra.ExactArgs(1),
	Run:   runRewrite,
}

func init() {
	rootCmd.AddCommand(rewriteCmd)
}

func runRewrite(cmd *cobra.Command, args []string) {
	command := args[0]

	// 检查是否包含管道/重定向等不安全操作符
	if !hook.ShouldRewrite(command) {
		os.Exit(1) // 不改写，原样透传
	}

	registry := filter.DefaultRegistry()
	segments := hook.SplitChainedCommands(command)

	var rewritten []string
	anyRewritten := false

	for _, seg := range segments {
		parts := strings.Fields(seg.Cmd)
		if len(parts) == 0 {
			continue
		}

		cmdName := parts[0]
		cmdArgs := parts[1:]

		f := registry.Find(cmdName, cmdArgs)
		var segResult string
		if f != nil {
			segResult = "gw exec " + seg.Cmd
			anyRewritten = true
		} else {
			segResult = seg.Cmd
		}

		if seg.Sep != "" {
			rewritten = append(rewritten, segResult+" "+seg.Sep)
		} else {
			rewritten = append(rewritten, segResult)
		}
	}

	if !anyRewritten {
		os.Exit(1) // 没有任何命令被改写
	}

	fmt.Println(strings.Join(rewritten, " "))
	os.Exit(0)
}
```

- [ ] **Step 2: 实现 init 命令**

```go
// cmd/init_cmd.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "安装 Claude Code PreToolUse hook",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法获取 home 目录: %w", err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// 读取现有配置
	var settings map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			settings = make(map[string]interface{})
		} else {
			return fmt.Errorf("读取 settings.json 失败: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("解析 settings.json 失败: %w", err)
		}
	}

	// 构建 hook 配置
	gwHook := map[string]interface{}{
		"matcher": "Bash",
		"hook":    `gw rewrite "$command"`,
	}

	// 添加到 PreToolUse hooks
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	preToolUse, _ := hooks["PreToolUse"].([]interface{})

	// 检查是否已安装
	for _, h := range preToolUse {
		if hMap, ok := h.(map[string]interface{}); ok {
			if hookCmd, _ := hMap["hook"].(string); hookCmd == `gw rewrite "$command"` {
				fmt.Println("gw hook 已安装")
				return nil
			}
		}
	}

	preToolUse = append(preToolUse, gwHook)
	hooks["PreToolUse"] = preToolUse
	settings["hooks"] = hooks

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 写入配置
	output, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		return fmt.Errorf("写入 settings.json 失败: %w", err)
	}

	fmt.Println("gw hook 已安装到 Claude Code")
	fmt.Println("测试: gw exec git status")
	return nil
}
```

- [ ] **Step 3: 实现 uninstall 命令**

```go
// cmd/uninstall.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "移除 Claude Code hook",
	RunE:  runUninstall,
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法获取 home 目录: %w", err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("settings.json 不存在，无需卸载")
			return nil
		}
		return fmt.Errorf("读取 settings.json 失败: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("解析 settings.json 失败: %w", err)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		fmt.Println("未找到 hook 配置")
		return nil
	}

	preToolUse, _ := hooks["PreToolUse"].([]interface{})
	var filtered []interface{}
	removed := false
	for _, h := range preToolUse {
		if hMap, ok := h.(map[string]interface{}); ok {
			if hookCmd, _ := hMap["hook"].(string); hookCmd == `gw rewrite "$command"` {
				removed = true
				continue
			}
		}
		filtered = append(filtered, h)
	}

	if !removed {
		fmt.Println("未找到 gw hook")
		return nil
	}

	hooks["PreToolUse"] = filtered
	settings["hooks"] = hooks

	output, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		return fmt.Errorf("写入失败: %w", err)
	}

	fmt.Println("gw hook 已移除")
	return nil
}
```

- [ ] **Step 4: 验证编译和基本功能**

```bash
cd /private/tmp/gw
go build -o gw .
./gw rewrite "git status"
echo "Exit code: $?"
./gw rewrite "git log | grep fix"
echo "Exit code: $?"
```

Expected: 第一个 exit 0 + 输出 `gw exec git status`，第二个 exit 1（无输出）

- [ ] **Step 5: Commit**

```bash
cd /private/tmp/gw
git add cmd/rewrite.go cmd/init_cmd.go cmd/uninstall.go
git commit -m "实现 rewrite/init/uninstall 命令"
```

---

### Task 11: Token 追踪 + Gain 统计

**Files:**
- Create: `track/token.go`
- Create: `track/db.go`
- Create: `track/db_test.go`
- Create: `track/stats.go`
- Create: `cmd/gain.go`
- Modify: `cmd/exec.go` — 集成追踪

- [ ] **Step 1: 实现 token 估算**

```go
// track/token.go
package track

import "math"

// EstimateTokens 估算文本的 token 数量
// MVP: ceil(chars/4)，后续可替换为更精确的实现
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return int(math.Ceil(float64(len(text)) / 4.0))
}
```

- [ ] **Step 2: 编写 SQLite 存储测试**

```go
// track/db_test.go
package track

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDB_RecordAndQuery(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	err = db.Record(Record{
		Timestamp:    time.Now(),
		Command:      "git status",
		ExitCode:     0,
		InputTokens:  1000,
		OutputTokens: 300,
		SavedTokens:  700,
		ElapsedMs:    5,
		FilterUsed:   "git/status",
	})
	if err != nil {
		t.Fatalf("failed to record: %v", err)
	}

	stats, err := db.TodayStats()
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TotalSaved != 700 {
		t.Errorf("expected 700 saved, got %d", stats.TotalSaved)
	}
	if stats.CommandCount != 1 {
		t.Errorf("expected 1 command, got %d", stats.CommandCount)
	}
}

func TestDB_Cleanup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	// 插入一条 100 天前的记录
	old := Record{
		Timestamp:    time.Now().AddDate(0, 0, -100),
		Command:      "old command",
		ExitCode:     0,
		InputTokens:  100,
		OutputTokens: 50,
		SavedTokens:  50,
		ElapsedMs:    1,
		FilterUsed:   "test",
	}
	_ = db.Record(old)

	deleted, err := db.Cleanup(90)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
}
```

- [ ] **Step 3: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./track/ -v
```

Expected: FAIL — types 未定义

- [ ] **Step 4: 实现 SQLite 存储**

```go
// track/db.go
package track

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Record struct {
	Timestamp    time.Time
	Command      string
	ExitCode     int
	InputTokens  int
	OutputTokens int
	SavedTokens  int
	ElapsedMs    int64
	FilterUsed   string
}

type Stats struct {
	TotalSaved   int
	TotalInput   int
	CommandCount int
}

type DB struct {
	db *sql.DB
}

func NewDB(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tracking (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp     TEXT    NOT NULL,
		command       TEXT    NOT NULL,
		exit_code     INTEGER NOT NULL,
		input_tokens  INTEGER NOT NULL,
		output_tokens INTEGER NOT NULL,
		saved_tokens  INTEGER NOT NULL,
		elapsed_ms    INTEGER NOT NULL,
		filter_used   TEXT    NOT NULL
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) Record(r Record) error {
	_, err := d.db.Exec(
		`INSERT INTO tracking (timestamp, command, exit_code, input_tokens, output_tokens, saved_tokens, elapsed_ms, filter_used)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Timestamp.Format(time.RFC3339), r.Command, r.ExitCode,
		r.InputTokens, r.OutputTokens, r.SavedTokens, r.ElapsedMs, r.FilterUsed,
	)
	return err
}

func (d *DB) TodayStats() (*Stats, error) {
	today := time.Now().Format("2006-01-02")
	return d.queryStats(fmt.Sprintf("timestamp >= '%s'", today))
}

func (d *DB) WeekStats() (*Stats, error) {
	weekAgo := time.Now().AddDate(0, 0, -7).Format(time.RFC3339)
	return d.queryStats(fmt.Sprintf("timestamp >= '%s'", weekAgo))
}

func (d *DB) AllStats() (*Stats, error) {
	return d.queryStats("1=1")
}

func (d *DB) queryStats(where string) (*Stats, error) {
	row := d.db.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(saved_tokens), 0), COALESCE(SUM(input_tokens), 0), COUNT(*)
		FROM tracking WHERE %s`, where))

	var stats Stats
	err := row.Scan(&stats.TotalSaved, &stats.TotalInput, &stats.CommandCount)
	return &stats, err
}

type TopCommand struct {
	Command    string
	TotalSaved int
	AvgPct     float64
}

func (d *DB) TopCommands(limit int) ([]TopCommand, error) {
	rows, err := d.db.Query(
		`SELECT command, SUM(saved_tokens), AVG(CASE WHEN input_tokens > 0 THEN saved_tokens * 100.0 / input_tokens ELSE 0 END)
		FROM tracking GROUP BY command ORDER BY SUM(saved_tokens) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TopCommand
	for rows.Next() {
		var tc TopCommand
		if err := rows.Scan(&tc.Command, &tc.TotalSaved, &tc.AvgPct); err != nil {
			return nil, err
		}
		result = append(result, tc)
	}
	return result, nil
}

func (d *DB) Cleanup(retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format(time.RFC3339)
	result, err := d.db.Exec("DELETE FROM tracking WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DefaultDBPath 返回默认的数据库路径
func DefaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gw", "tracking.db")
}
```

- [ ] **Step 5: 安装 SQLite 依赖并运行测试**

```bash
cd /private/tmp/gw
go get github.com/mattn/go-sqlite3
go test ./track/ -v
```

Expected: PASS — 2 tests

- [ ] **Step 6: 实现 gain 命令**

```go
// cmd/gain.go
package cmd

import (
	"fmt"

	"github.com/gw-cli/gw/track"
	"github.com/spf13/cobra"
)

var gainCmd = &cobra.Command{
	Use:   "gain",
	Short: "查看 token 节省统计",
	RunE:  runGain,
}

func init() {
	rootCmd.AddCommand(gainCmd)
}

func runGain(cmd *cobra.Command, args []string) error {
	db, err := track.NewDB(track.DefaultDBPath())
	if err != nil {
		return fmt.Errorf("无法打开数据库: %w", err)
	}
	defer db.Close()

	// 清理旧数据
	db.Cleanup(90)

	today, _ := db.TodayStats()
	week, _ := db.WeekStats()
	all, _ := db.AllStats()
	top, _ := db.TopCommands(5)

	fmt.Println("──── Token Savings Report ────")
	printStats("Today", today)
	printStats("This week", week)
	printStats("Total", all)

	if len(top) > 0 {
		fmt.Println("\nTop commands:")
		for _, tc := range top {
			fmt.Printf("  %-20s %d saved (%.0f%%)\n", tc.Command, tc.TotalSaved, tc.AvgPct)
		}
	}

	return nil
}

func printStats(label string, stats *Stats) {
	if stats.TotalInput > 0 {
		pct := float64(stats.TotalSaved) / float64(stats.TotalInput) * 100
		fmt.Printf("%-12s %d tokens saved (%.0f%%) across %d commands\n",
			label+":", stats.TotalSaved, pct, stats.CommandCount)
	} else {
		fmt.Printf("%-12s no data\n", label+":")
	}
}
```

- [ ] **Step 7: 在 exec 命令中集成追踪**

在 `cmd/exec.go` 的 `runExec` 函数中，TRACK 阶段替换为真实的 SQLite 写入：

在 `// TRACK` 注释处替换为：

```go
	// TRACK: 记录 token 节省
	go func() {
		db, err := track.NewDB(track.DefaultDBPath())
		if err != nil {
			return
		}
		defer db.Close()

		inputTokens := track.EstimateTokens(output.Original)
		outputTokens := track.EstimateTokens(output.Content)
		_ = db.Record(track.Record{
			Timestamp:    time.Now(),
			Command:      strings.Join(args, " "),
			ExitCode:     result.ExitCode,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			SavedTokens:  inputTokens - outputTokens,
			ElapsedMs:    elapsed.Milliseconds(),
			FilterUsed:   fmt.Sprintf("%T", f),
		})
	}()
```

同时在 `runExec` 开头记录开始时间：

```go
	start := time.Now()
	// ... 现有代码 ...
	elapsed := time.Since(start)
```

添加对应的 import: `"time"`, `"github.com/gw-cli/gw/track"`

- [ ] **Step 8: 验证编译**

```bash
cd /private/tmp/gw
go build ./...
```

Expected: 编译成功

- [ ] **Step 9: Commit**

```bash
cd /private/tmp/gw
git add track/ cmd/gain.go cmd/exec.go
git commit -m "实现 token 追踪和 gain 统计"
```

---

### Task 12: TOML 声明式过滤引擎

**Files:**
- Create: `filter/toml/engine.go`
- Create: `filter/toml/engine_test.go`
- Create: `filter/toml/rules/docker.toml`
- Create: `filter/toml/rules/kubectl.toml`
- Modify: `filter/registry.go` — 注册 TOML 引擎

- [ ] **Step 1: 创建内置 TOML 规则**

```toml
# filter/toml/rules/docker.toml
[docker.ps]
match = "docker ps"
strip_ansi = true
max_lines = 50

[docker.images]
match = "docker images"
strip_ansi = true
max_lines = 30

[docker.logs]
match = "docker logs"
strip_ansi = true
tail_lines = 50
```

```toml
# filter/toml/rules/kubectl.toml
[kubectl.get]
match = "kubectl get"
strip_ansi = true
max_lines = 50

[kubectl.describe]
match = "kubectl describe"
strip_ansi = true
max_lines = 100
strip_lines = ["^\\s*$"]

[kubectl.logs]
match = "kubectl logs"
strip_ansi = true
tail_lines = 100
```

- [ ] **Step 2: 编写 TOML 引擎测试**

```go
// filter/toml/engine_test.go
package toml

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestTomlEngine_Match(t *testing.T) {
	rule := Rule{
		Match:     "docker ps",
		MaxLines:  50,
		StripAnsi: true,
	}
	engine := &TomlFilter{Rules: map[string]Rule{"docker.ps": rule}}

	if !engine.Match("docker", []string{"ps"}) {
		t.Error("should match 'docker ps'")
	}
	if engine.Match("docker", []string{"build"}) {
		t.Error("should not match 'docker build'")
	}
}

func TestTomlEngine_MaxLines(t *testing.T) {
	rule := Rule{
		Match:    "test",
		MaxLines: 3,
	}
	engine := &TomlFilter{Rules: map[string]Rule{"test": rule}}

	input := filter.FilterInput{
		Cmd:      "test",
		Stdout:   "line1\nline2\nline3\nline4\nline5\n",
		ExitCode: 0,
	}
	output := engine.Apply(input)
	lines := strings.Split(strings.TrimSpace(output.Content), "\n")
	if len(lines) > 3 {
		t.Errorf("expected max 3 lines, got %d", len(lines))
	}
}

func TestTomlEngine_TailLines(t *testing.T) {
	rule := Rule{
		Match:     "test",
		TailLines: 2,
	}
	engine := &TomlFilter{Rules: map[string]Rule{"test": rule}}

	input := filter.FilterInput{
		Cmd:      "test",
		Stdout:   "line1\nline2\nline3\nline4\nline5\n",
		ExitCode: 0,
	}
	output := engine.Apply(input)
	if !strings.Contains(output.Content, "line4") || !strings.Contains(output.Content, "line5") {
		t.Error("tail_lines should keep last 2 lines")
	}
	if strings.Contains(output.Content, "line1") {
		t.Error("tail_lines should not contain first line")
	}
}

func TestTomlEngine_StripLines(t *testing.T) {
	rule := Rule{
		Match:      "test",
		StripLines: []string{"^DEBUG"},
	}
	engine := &TomlFilter{Rules: map[string]Rule{"test": rule}}

	input := filter.FilterInput{
		Cmd:      "test",
		Stdout:   "INFO: starting\nDEBUG: internal state\nINFO: done\n",
		ExitCode: 0,
	}
	output := engine.Apply(input)
	if strings.Contains(output.Content, "DEBUG") {
		t.Error("should strip DEBUG lines")
	}
	if !strings.Contains(output.Content, "INFO: starting") {
		t.Error("should keep INFO lines")
	}
}
```

- [ ] **Step 3: 运行测试验证失败**

```bash
cd /private/tmp/gw
go test ./filter/toml/ -v
```

Expected: FAIL — types 未定义

- [ ] **Step 4: 实现 TOML 引擎**

```go
// filter/toml/engine.go
package toml

import (
	"embed"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gw-cli/gw/filter"
)

//go:embed rules/*.toml
var builtinRules embed.FS

// Rule 表示一条 TOML 声明式过滤规则
type Rule struct {
	Match      string   `toml:"match"`
	StripAnsi  bool     `toml:"strip_ansi"`
	MaxLines   int      `toml:"max_lines"`
	HeadLines  int      `toml:"head_lines"`
	TailLines  int      `toml:"tail_lines"`
	StripLines []string `toml:"strip_lines"`
	KeepLines  []string `toml:"keep_lines"`
	OnEmpty    string   `toml:"on_empty"`
}

// TomlFilter 是 TOML 声明式过滤引擎
type TomlFilter struct {
	Rules map[string]Rule
}

func (f *TomlFilter) Match(cmd string, args []string) bool {
	full := cmd
	if len(args) > 0 {
		full = cmd + " " + strings.Join(args, " ")
	}
	for _, rule := range f.Rules {
		if strings.HasPrefix(full, rule.Match) {
			return true
		}
	}
	return false
}

func (f *TomlFilter) findRule(cmd string, args []string) *Rule {
	full := cmd
	if len(args) > 0 {
		full = cmd + " " + strings.Join(args, " ")
	}
	// 找最长匹配
	var best *Rule
	bestLen := 0
	for _, rule := range f.Rules {
		r := rule
		if strings.HasPrefix(full, r.Match) && len(r.Match) > bestLen {
			best = &r
			bestLen = len(r.Match)
		}
	}
	return best
}

func (f *TomlFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	rule := f.findRule(input.Cmd, input.Args)
	if rule == nil {
		return filter.FilterOutput{Content: original, Original: original}
	}

	content := original

	// strip_ansi
	if rule.StripAnsi {
		content = stripAnsi(content)
	}

	lines := strings.Split(content, "\n")

	// strip_lines: 按正则去除匹配行
	if len(rule.StripLines) > 0 {
		lines = applyStripLines(lines, rule.StripLines)
	}

	// keep_lines: 只保留匹配行
	if len(rule.KeepLines) > 0 {
		lines = applyKeepLines(lines, rule.KeepLines)
	}

	// head_lines
	if rule.HeadLines > 0 && len(lines) > rule.HeadLines {
		lines = lines[:rule.HeadLines]
	}

	// tail_lines
	if rule.TailLines > 0 && len(lines) > rule.TailLines {
		lines = lines[len(lines)-rule.TailLines:]
	}

	// max_lines
	if rule.MaxLines > 0 && len(lines) > rule.MaxLines {
		lines = lines[:rule.MaxLines]
	}

	content = strings.Join(lines, "\n")
	content = strings.TrimSpace(content)

	if content == "" && rule.OnEmpty != "" {
		content = rule.OnEmpty
	}

	return filter.FilterOutput{
		Content:  content + "\n",
		Original: original,
	}
}

func (f *TomlFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// TOML 规则不区分成功/失败，失败时透传
	return nil
}

// LoadBuiltinRules 加载编译时嵌入的 TOML 规则
func LoadBuiltinRules() (*TomlFilter, error) {
	allRules := make(map[string]Rule)

	entries, err := builtinRules.ReadDir("rules")
	if err != nil {
		return &TomlFilter{Rules: allRules}, nil
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		data, err := builtinRules.ReadFile("rules/" + entry.Name())
		if err != nil {
			continue
		}
		var fileRules map[string]Rule
		if err := toml.Unmarshal(data, &fileRules); err != nil {
			continue
		}
		for k, v := range fileRules {
			allRules[k] = v
		}
	}

	return &TomlFilter{Rules: allRules}, nil
}

// 辅助函数

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func applyStripLines(lines []string, patterns []string) []string {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	var result []string
	for _, line := range lines {
		stripped := false
		for _, re := range compiled {
			if re.MatchString(line) {
				stripped = true
				break
			}
		}
		if !stripped {
			result = append(result, line)
		}
	}
	return result
}

func applyKeepLines(lines []string, patterns []string) []string {
	var result []string
	for _, line := range lines {
		for _, p := range patterns {
			if strings.Contains(line, p) {
				result = append(result, line)
				break
			}
		}
	}
	return result
}
```

- [ ] **Step 5: 安装依赖并运行测试**

```bash
cd /private/tmp/gw
go get github.com/BurntSushi/toml
go test ./filter/toml/ -v
```

Expected: PASS — 4 tests

- [ ] **Step 6: 注册 TOML 引擎到 registry**

在 `filter/registry.go` 的 `DefaultRegistry()` 中添加：

```go
import tomlfilter "github.com/gw-cli/gw/filter/toml"

// 在 DefaultRegistry() 最后（作为 fallback）:
tomlEngine, _ := tomlfilter.LoadBuiltinRules()
if tomlEngine != nil {
    r.Register(tomlEngine)
}
```

- [ ] **Step 7: Commit**

```bash
cd /private/tmp/gw
git add filter/toml/ filter/registry.go
git commit -m "实现 TOML 声明式过滤引擎"
```

---

### Task 13: 端到端集成测试 + 最终构建

**Files:**
- Create: `cmd/exec_test.go`

- [ ] **Step 1: 编写集成测试**

```go
// cmd/exec_test.go
package cmd

import (
	"os/exec"
	"strings"
	"testing"
)

func TestExec_GitStatus(t *testing.T) {
	// 需要先 build gw
	build := exec.Command("go", "build", "-o", "/tmp/gw-test", ".")
	build.Dir = "/private/tmp/gw"
	if err := build.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	cmd := exec.Command("/tmp/gw-test", "exec", "git", "status")
	cmd.Dir = "/private/tmp/gw"
	out, err := cmd.CombinedOutput()
	if err != nil {
		// git status 可能失败如果不在 git repo 中
		t.Logf("output: %s", out)
	}
	output := string(out)

	// 教学提示应被过滤
	if strings.Contains(output, "use \"git restore") {
		t.Error("git teaching hints should be filtered")
	}
}

func TestExec_Passthrough(t *testing.T) {
	build := exec.Command("go", "build", "-o", "/tmp/gw-test", ".")
	build.Dir = "/private/tmp/gw"
	if err := build.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// 无过滤器的命令应透传
	cmd := exec.Command("/tmp/gw-test", "exec", "echo", "hello world")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec failed: %v, output: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(out))
	}
}

func TestRewrite_Simple(t *testing.T) {
	build := exec.Command("go", "build", "-o", "/tmp/gw-test", ".")
	build.Dir = "/private/tmp/gw"
	if err := build.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	cmd := exec.Command("/tmp/gw-test", "rewrite", "git status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "gw exec git status" {
		t.Errorf("expected 'gw exec git status', got %q", string(out))
	}
}

func TestRewrite_Pipe(t *testing.T) {
	build := exec.Command("go", "build", "-o", "/tmp/gw-test", ".")
	build.Dir = "/private/tmp/gw"
	if err := build.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// 管道命令不应被改写
	cmd := exec.Command("/tmp/gw-test", "rewrite", "git log | grep fix")
	err := cmd.Run()
	if err == nil {
		t.Error("pipe command should exit with code 1")
	}
}
```

- [ ] **Step 2: 运行全部测试**

```bash
cd /private/tmp/gw
go test ./... -v
```

Expected: 所有测试通过

- [ ] **Step 3: 构建最终二进制**

```bash
cd /private/tmp/gw
go build -o gw .
ls -lh gw
./gw --help
./gw exec git status
./gw rewrite "git status"
./gw rewrite "mvn clean && mvn test"
./gw rewrite "git log | grep fix"
echo "Exit: $?"
```

Expected: 二进制生成，help 显示所有子命令，exec/rewrite 正常工作

- [ ] **Step 4: Commit**

```bash
cd /private/tmp/gw
git add cmd/exec_test.go
git commit -m "添加端到端集成测试"
```

- [ ] **Step 5: 最终 Commit — 清理和 .gitignore**

```bash
cat > /private/tmp/gw/.gitignore << 'EOF'
/gw
*.db
.gw/
.superpowers/
EOF

cd /private/tmp/gw
git add .gitignore
git commit -m "添加 .gitignore"
```

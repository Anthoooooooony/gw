# gw

CLI proxy for AI coding tools. 拦截 shell 命令，本地执行，过滤输出，减少 LLM token 消耗。

## Installation

### 下载预编译二进制（推荐）

从 [GitHub Releases](https://github.com/Anthoooooooony/gw/releases/latest) 下载对应平台 tar.gz，解压后把 `gw` 放入 `PATH`：

```bash
# macOS Apple Silicon（M 系列）
curl -L -o gw.tar.gz https://github.com/Anthoooooooony/gw/releases/latest/download/gw_v0.1.0_darwin_arm64.tar.gz
tar xzf gw.tar.gz
sudo mv gw /usr/local/bin/
gw version
```

```bash
# Linux amd64
curl -L -o gw.tar.gz https://github.com/Anthoooooooony/gw/releases/latest/download/gw_v0.1.0_linux_amd64.tar.gz
tar xzf gw.tar.gz
sudo mv gw /usr/local/bin/
gw version
```

当前平台覆盖：`linux_amd64` / `darwin_arm64`。Intel Mac、Linux arm64、Windows 暂不提供二进制，用下面 `go install` 自建。

### 从源码构建

```bash
go install github.com/gw-cli/gw@latest
```

注意：需要 CGO（go-sqlite3 依赖），本地需有 C 编译器（gcc / clang）。

## 工作原理

```
Claude Code Agent 想执行 mvn test
        │
   PreToolUse Hook
        │
   gw rewrite "mvn test"
        │  输出: gw exec mvn test
        │
   gw exec mvn test
        │
   ┌────┴────────────────────────────────────────┐
   │  PARSE → ROUTE → EXECUTE → FILTER → PRINT  │
   │                                     → TRACK │
   └─────────────────────────────────────────────┘
        │
   Claude Code 收到过滤后的输出
   905 行 → 46 行 (95% 压缩，真实多模块 Maven 项目)
```

gw 通过 Claude Code 的 PreToolUse Hook 自动将 `mvn test` 改写为 `gw exec mvn test`。gw 在本地执行原始命令，对输出应用过滤器压缩噪音，然后将精简结果返回给 LLM。整个过程对 AI agent 透明。

## 快速开始

**前置依赖**：

- Go 1.22+
- Claude Code 已安装
- **CGO 工具链**：`mattn/go-sqlite3` 需要 `CGO_ENABLED=1` + C 编译器
  - macOS：Xcode Command Line Tools（`xcode-select --install`）
  - Linux：`gcc`、`libc` 头文件（如 Debian/Ubuntu 的 `build-essential`）
  - 精简 CI 镜像（如 `golang:alpine`）需额外安装 `gcc musl-dev sqlite-dev`，或改用 `golang:1.22` 基础镜像

```bash
# 构建
go build -o gw .

# 安装 Claude Code Hook（一键）
./gw init

# 验证
./gw exec git status
./gw exec mvn test

# 查看 token 节省统计
./gw gain
```

安装后 Claude Code 会自动将匹配的命令路由到 gw，无需手动操作。

卸载：

```bash
./gw uninstall
```

## 环境变量

| 变量 | 默认 | 用途 |
|------|------|------|
| `GW_CMD_TIMEOUT` | `10m` | 命令执行超时；`0` / `off` / `none` / `disable` / `disabled` 禁用；负值（`-1s` 等）等同禁用 |
| `GW_STORE_RAW` | `0` | 设 `1` 时把原始输出存入 DB，供 `gw inspect --raw` 回溯 |
| `GW_DB_PATH` | `~/.gw/tracking.db` | 覆盖 tracking DB 路径；HOME 只读时自动降级到 `$TMPDIR/gw-tracking.db` |

## 设计

### 六阶段管道

每次命令执行经过六个阶段：

| 阶段 | 职责 |
|------|------|
| **PARSE** | 提取命令名和参数 |
| **ROUTE** | 在过滤器注册表中查找匹配的过滤器 |
| **EXECUTE** | 本地执行命令，捕获 stdout + stderr + exit code |
| **FILTER** | 应用匹配的过滤器压缩输出 |
| **PRINT** | 输出压缩结果，exit code 透传 |
| **TRACK** | 记录 token 节省量到 SQLite |

### 批量模式 vs 流式模式

gw 有两条执行路径，根据过滤器类型自动选择：

**判定逻辑** —— gw 不根据命令本身，而是根据**过滤器的接口实现**决定路径：

```go
if sf := filter.FindStream(cmd, args); sf != nil {
    runStreamExec(sf, ...)  // 过滤器实现了 StreamFilter → 流式
} else {
    runExec(...)            // 只实现 Filter → 批量
}
```

每个过滤器开发时根据命令特征选择实现哪个接口：

| 过滤器 | 实现的接口 | 实际走的路径 | 理由 |
|--------|------------|-------------|------|
| `git status/log` | Filter | 批量 | 输出小且快，批量足够 |
| `java/maven` | Filter + StreamFilter | 流式（优先） | 多模块构建输出大但会退出，流式可实时反馈；批量接口保留用于测试 |
| `java/gradle` | Filter + StreamFilter | 流式（优先） | 构建输出量大，流式可实时反馈任务进度；批量接口保留覆盖短时命令 |
| `java/springboot` | Filter + StreamFilter | 流式（必须） | `java -jar` 是长驻进程；两个接口都实现但 FindStream 优先，批量路径永远不会被触发 |

**批量模式** — `cmd.Run()` 等待退出后一次性过滤：

```
cmd.Run() → 拿到全部输出 → 知道 exit code → 选择 Apply 或 ApplyOnError → 输出
```

**流式模式** — `cmd.StdoutPipe() + bufio.Scanner` 逐行读取实时过滤：

```
StdoutPipe → Scanner 逐行读 → ProcessLine 立即决策 → 实时输出 → 退出后 Flush 缓冲
```

### 双层过滤器

**第一层：Go 硬编码过滤器** — 深度优化的高频命令

| 命令 | 过滤器 | 技术 | 压缩率 |
|------|--------|------|--------|
| `git status` | 去除教学提示 | 正则 | ~30% |
| `git log` | 紧凑格式 + 去 trailer | 正则 | ~45% |
| `mvn compile/test/...` | 状态机（11 态，基于 Maven 源码事件模型） | 状态机 | **95%** |
| `gradle build/test` | 白名单模式，成功只留摘要 | 正则 | ~80% |
| `java -jar *.jar` | 去 banner / Hibernate / 内部引擎日志 | 正则 + 流式 | ~62% |

**第二层：TOML 声明式过滤器** — 零代码覆盖长尾命令

```toml
# filter/toml/rules/docker.toml
[docker.ps]
match = "docker ps"
strip_ansi = true
max_lines = 50
```

TOML 引擎支持 7 阶段处理管道：`strip_ansi → strip_lines → keep_lines → head_lines → tail_lines → max_lines → on_empty`

内置 TOML 规则覆盖：`docker`（ps/images/logs）、`kubectl`、`node`（npm/yarn/pnpm install/test/build/ci）、`python`（pip/pytest/venv）、`rust`（cargo build/test/check/clippy）。

### 错误处理

批量模式和流式模式的错误处理机制**不同**，因为流式模式在过滤时不知道 exit code。

**批量模式 — 三级策略**（依赖 exit code 做决策）：

| 场景 | exit code | 策略 |
|------|-----------|------|
| 命令成功 | 0 | `Apply()` 激进压缩（去下载日志、进度条、WARNING 等噪音） |
| 命令失败 + 有专用过滤器 | != 0 | `ApplyOnError()` 去噪音但保留错误信息、堆栈、测试失败详情 |
| 命令失败 + 无专用过滤器 | != 0 | `ApplyOnError()` 返回 nil → 透传原始输出（宁可不压缩也不丢信息） |

**流式模式 — 两阶段策略**（ProcessLine 不知道 exit code，延迟到 Flush 决策）：

| 阶段 | 逻辑 |
|------|------|
| `ProcessLine(line)` 实时决策 | 不知道 exit code。噪音始终丢弃，错误和栈追踪**始终立即输出**（保守策略）。普通插件输出**缓冲**（最多 10 行） |
| `Flush(exitCode)` 进程退出后 | 失败则追加缓冲内容作为错误上下文；成功则丢弃缓冲 |

流式模式不需要"透传"降级路径 —— 只实现 `StreamFilter` 的过滤器已经在设计时考虑了长驻进程，错误场景通过"实时输出 error 行 + Flush 补 buffer"覆盖。

### Maven 状态机

Maven 过滤器是整个项目中最复杂的组件。基于对 Maven 源码 `ExecutionEventLogger.java` 的分析，用 11 个状态精确追踪构建流程：

```
Init → Discovery → Warning → ModuleBuild → Mojo → PluginOutput → TestOutput
                                                                       ↓
                                                   Reactor → Result → Stats → ErrorReport
```

状态转移由 Maven 源码中的固定标记行驱动，不依赖输出内容的细节。在真实的多模块 Maven 项目（905 行构建日志 fixture）上实现 95% 压缩率（`filter/java/testdata/mvn_compile_real_failure.txt`）。

### 引号感知 Shell Lexer

Hook 改写命令时，需要判断 `|` `>` 等特殊字符是真正的操作符还是在引号内。gw 的 lexer 是单遍引号感知的 tokenizer：

```
git log --format="%H|%s"   → 引号内的 | → 允许改写 ✓
git log | grep fix          → 真正的管道 → 拒绝改写，原样透传
mvn clean && mvn test       → 链式操作符 → 逐段改写
```

管道和重定向命令整条不改写。管道左侧的输出本应传给右侧程序消费（如 `git log | grep fix` 中 `git log` 的输出给 `grep`），若改写压缩会破坏数据完整性，因此整条保守透传。

### 自注册模式

过滤器通过 `func init()` 自注册，新增过滤器不需要修改任何其他文件：

```go
// filter/java/maven.go
func init() {
    filter.Register(&MavenFilter{})
}
```

`filter/all/all.go` 通过 blank import 聚合所有过滤器包：

```go
import (
    _ "github.com/gw-cli/gw/filter/git"
    _ "github.com/gw-cli/gw/filter/java"
    _ "github.com/gw-cli/gw/filter/toml"
)
```

### 长驻进程排除

Maven/Gradle 的 Match 函数排除可能导致永远阻塞的长驻进程 goal：

- Maven: `spring-boot:run`, `jetty:run`, `tomcat7:run`, `liberty:run`, `quarkus:dev`, `exec:java`
- Gradle: `bootRun`, `run`, `appRun`, `jettyRun`, `tomcatRun`, `quarkusDev`

Spring Boot (`java -jar`) 虽然同时实现了批量和流式接口，但 ROUTE 阶段 `FindStream` 优先，实际只走流式路径（避免批量模式下 `java -jar` 永不退出导致的死锁）。

## 项目结构

```
gw/
├── main.go                         # 入口
│
├── cmd/                            # CLI 命令
│   ├── root.go                     # cobra 根命令 + --verbose flag
│   ├── exec.go                     # gw exec — 六阶段管道（批量 + 流式双路径）
│   ├── rewrite.go                  # gw rewrite — Hook 调用的命令改写
│   ├── init_cmd.go                 # gw init — 安装 Claude Code Hook
│   ├── uninstall.go                # gw uninstall — 移除 Hook
│   ├── gain.go                     # gw gain — Token 节省统计
│   ├── version.go                  # gw --version / gw version（ldflags + runtime/debug fallback）
│   ├── inspect.go                  # gw inspect [id] — 查询历史记录，--raw 打印原文
│   ├── filters.go                  # gw filters list — 列出已注册过滤器及来源
│   └── registry.go                 # blank import 触发过滤器自注册
│
├── filter/                         # 过滤器核心
│   ├── filter.go                   # Filter / StreamFilter / StreamProcessor 接口
│   ├── registry.go                 # 全局注册表 + Register / Find / FindStream
│   ├── all/all.go                  # 聚合所有过滤器包的 blank import
│   ├── git/
│   │   ├── status.go              # git status 过滤器
│   │   └── log.go                 # git log 过滤器（紧凑格式）
│   ├── java/
│   │   ├── maven.go               # Maven 过滤器（批量 + 流式，错误去重）
│   │   ├── maven_state.go         # Maven 状态机（11 态 + 21 种行分类 + 转移逻辑）
│   │   ├── gradle.go              # Gradle 过滤器（批量白名单 + 流式状态机）
│   │   └── springboot.go          # Spring Boot 过滤器（banner + logger name 匹配）
│   └── toml/
│       ├── engine.go              # TOML 声明式过滤引擎（7 阶段管道）
│       ├── loader.go              # TOML 三级加载（builtin / user / project）
│       └── rules/                 # 内置 TOML 规则（go:embed）
│           ├── docker.toml
│           ├── kubectl.toml
│           ├── node.toml          # npm / yarn / pnpm
│           ├── python.toml        # pip / pytest / venv
│           └── rust.toml          # cargo build/test/check/clippy
│
├── shell/
│   └── lexer.go                   # 引号感知 shell tokenizer（AnalyzeCommand）
│
├── internal/
│   ├── runner.go                  # 批量执行器（cmd.Run）
│   ├── stream.go                  # 流式执行器（StdoutPipe + Scanner）
│   ├── timeout.go                 # GW_CMD_TIMEOUT 解析 + 超时提示
│   ├── killer.go                  # 超时 killer goroutine（runner/stream 共用）
│   ├── procgroup_unix.go          # 进程组 SIGTERM/SIGKILL（unix 平台）
│   └── procgroup_other.go         # 非 unix 平台降级实现（仅杀主进程）
│
└── track/
    ├── db.go                      # SQLite 存储（WAL + busy_timeout）
    └── token.go                   # Token 估算（ceil(runes/4)）
```

## 扩展过滤器

### 添加 Go 硬编码过滤器

```go
// filter/node/npm.go
package node

import "github.com/gw-cli/gw/filter"

func init() {
    filter.Register(&NpmFilter{})
}

type NpmFilter struct{}

func (f *NpmFilter) Name() string                  { return "node/npm" }
func (f *NpmFilter) Match(cmd string, args []string) bool { return cmd == "npm" }
func (f *NpmFilter) Apply(input filter.FilterInput) filter.FilterOutput {
    // 过滤逻辑
}
func (f *NpmFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
    return nil // 失败时透传
}
```

然后在 `filter/all/all.go` 加一行：

```go
_ "github.com/gw-cli/gw/filter/node"
```

### 添加 TOML 声明式规则

在 `filter/toml/rules/` 下新建 `.toml` 文件即可，无需写 Go 代码：

```toml
# filter/toml/rules/terraform.toml
[terraform.plan]
match = "terraform plan"
strip_ansi = true
max_lines = 100
strip_lines = ["^Refreshing state"]
```

### 实现流式过滤

让过滤器实现 `StreamFilter` 接口：

```go
func (f *NpmFilter) NewStreamInstance() filter.StreamProcessor {
    return &npmStreamProcessor{}
}

type npmStreamProcessor struct {
    // 持有本次执行的状态
}

func (p *npmStreamProcessor) ProcessLine(line string) (filter.StreamAction, string) {
    // StreamDrop 丢弃 / StreamEmit 立即输出
}

func (p *npmStreamProcessor) Flush(exitCode int) []string {
    // 进程退出后刷出缓冲区
}
```

## 测试

```bash
go test ./...           # 运行全部 164 个测试
go test ./... -v        # 详细输出
go test ./filter/java/  # 只跑 Java 过滤器测试
```

测试 fixture 使用 Docker 从真实开源项目抓取的构建输出（Maven 3 模块项目、Gradle 2 模块项目、Spring Petclinic），不使用手写模拟数据。

## 命令参考

| 命令 | 用途 |
|------|------|
| `gw exec <cmd> [args...]` | 执行命令并过滤输出 |
| `gw exec --dump-raw <path> <cmd> [args...]` | 执行命令并把原始输出写入指定文件（流式和批量路径都支持） |
| `gw rewrite "<command>"` | Hook 改写接口（exit 0=改写成功, 1=不改写） |
| `gw init` | 安装 Claude Code PreToolUse Hook |
| `gw init --dry-run` | 打印将要写入的变更但不落盘 |
| `gw uninstall` | 移除 Hook |
| `gw uninstall --dry-run` | 打印将要移除的变更但不落盘 |
| `gw gain` | 查看 token 节省统计 |
| `gw -v exec <cmd>` | 执行并显示 token 节省详情 |
| `gw --version` / `gw version` | 打印版本（ldflags 注入，runtime/debug 回退） |
| `gw inspect [id]` | 查询历史执行记录；不带 id 列出最近记录，带 id 查看详情 |
| `gw inspect [id] --raw` | 打印原始输出（需执行时 `GW_STORE_RAW=1` 写过原文） |
| `gw filters list` | 列出已注册过滤器及来源（builtin / user / project） |

## 已知限制

- **只支持 Claude Code**：`gw init` 目前只适配 Claude Code 的 PreToolUse Hook 机制。Cursor、Copilot 等其他 AI 编程工具的 Hook 机制不同，需要单独适配。
- **有损压缩**：过滤会丢弃部分原始信息。虽然设计上保留错误和诊断信息、仅丢弃噪音，但不可能 100% 无损。关键命令建议用 `gw -v exec` 观察压缩详情，或直接跳过 gw。
- **SQLite 追踪的并发性**：多个 gw 进程并发时，SQLite 写入使用 3 秒的 busy_timeout，极端情况下最长阻塞 ~3 秒。不影响主输出，只影响追踪数据记录。
- **Token 估算是近似值**：用 `ceil(runes/4)` 估算，不是真实 tokenizer。在中日韩字符密集的输出上估算偏差较大。
- **Windows 超时降级**：`internal/procgroup_other.go` 只能杀主进程，不覆盖进程组；`SIGTERM` 宽限期在 Windows 上无效，直接 kill。`_gw_managed` 标记会写入但 hook 数组位置管理不做验证。
- **用户配置目录平台差异**：TOML 用户规则目录由 `os.UserConfigDir()` 决定，macOS 是 `~/Library/Application Support/gw/rules/` 而**非** `~/.config`。如果在 macOS 上按 Linux 习惯把规则放到 `~/.config/gw/rules/` 将不会被加载。

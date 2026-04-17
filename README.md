# gw

CLI proxy for AI coding tools. 拦截 shell 命令，本地执行，过滤输出，减少 LLM token 消耗。

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
   427 行 → 12 行 (97% 压缩)
```

gw 通过 Claude Code 的 PreToolUse Hook 自动将 `mvn test` 改写为 `gw exec mvn test`。gw 在本地执行原始命令，对输出应用过滤器压缩噪音，然后将精简结果返回给 LLM。整个过程对 AI agent 透明。

## 快速开始

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

| 过滤器 | 批量 | 流式 | 理由 |
|--------|------|------|------|
| `git status/log` | ✅ | | 输出小且快，批量足够 |
| `java/maven` | ✅ | ✅ | 多模块构建输出大但会退出，流式可实时反馈 |
| `java/gradle` | ✅ | | 当前只实现批量（未来可能加流式） |
| `java/springboot` | | ✅ | `java -jar` 是长驻进程，批量会永远阻塞 |

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

状态转移由 Maven 源码中的固定标记行驱动，不依赖输出内容的细节。在真实的 大型 Java 产线 多模块项目（905 行输出）上实现 95% 压缩率。

### 引号感知 Shell Lexer

Hook 改写命令时，需要判断 `|` `>` 等特殊字符是真正的操作符还是在引号内。gw 的 lexer 是单遍引号感知的 tokenizer：

```
git log --format="%H|%s"   → 引号内的 | → 允许改写 ✓
git log | grep fix          → 真正的管道 → 拒绝改写，原样透传
mvn clean && mvn test       → 链式操作符 → 逐段改写
```

管道和重定向命令整条不改写（比 RTK 更保守）。RTK 改写管道左侧但有已知的数据损坏 bug。

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

- Maven: `spring-boot:run`, `jetty:run`, `quarkus:dev`, `exec:java` 等
- Gradle: `bootRun`, `run`, `appRun` 等

Spring Boot (`java -jar`) 只走流式路径，不走批量路径。

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
│   │   ├── gradle.go              # Gradle 过滤器（白名单模式）
│   │   └── springboot.go          # Spring Boot 过滤器（banner + logger name 匹配）
│   └── toml/
│       ├── engine.go              # TOML 声明式过滤引擎（7 阶段管道）
│       └── rules/                 # 内置 TOML 规则（go:embed）
│           ├── docker.toml
│           └── kubectl.toml
│
├── shell/
│   └── lexer.go                   # 引号感知 shell tokenizer（AnalyzeCommand）
│
├── internal/
│   ├── runner.go                  # 批量执行器（cmd.Run）
│   └── stream.go                  # 流式执行器（StdoutPipe + Scanner）
│
├── track/
│   ├── db.go                      # SQLite 存储（WAL + busy_timeout）
│   └── token.go                   # Token 估算（ceil(runes/4)）
│
└── config/
    └── config.go                  # 配置加载（预留）
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
| `gw rewrite "<command>"` | Hook 改写接口（exit 0=改写成功, 1=不改写） |
| `gw init` | 安装 Claude Code PreToolUse Hook |
| `gw uninstall` | 移除 Hook |
| `gw gain` | 查看 token 节省统计 |
| `gw -v exec <cmd>` | 执行并显示 token 节省详情 |

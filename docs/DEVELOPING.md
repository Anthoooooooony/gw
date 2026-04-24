# 开发指南

面向贡献者与深度定制用户。日常使用看根目录 [`README.md`](../README.md)；贡献流程看 [`CONTRIBUTING.md`](../CONTRIBUTING.md)；架构决策记录看 [`DECISIONS.md`](./DECISIONS.md)。

## 架构总览

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
| `pytest` / `python -m pytest` | parse summary + FAILURES 锚点 | 锚点切片 | **99% / 82%**（成功 / 失败） |

**第二层：TOML 声明式过滤器** — 零代码覆盖长尾命令（**仅无损变换**）

```toml
# filter/toml/rules/docker.toml
[docker.ps]
match = "docker ps"
strip_ansi = true
max_lines = 50
```

v2 DSL 只提供语义无关的安全变换：`strip_ansi` / `head_lines` / `tail_lines` / `max_lines` / `on_empty`。

**故意不提供** `strip_lines` / `keep_lines` / `on_error` 这类基于正则的行级裁剪——词法匹配无法区分"真噪音"和"用户恰好需要的那一行"，长期会制造误删信任危机。想要 "pytest 只留 failures"、"vitest 生成 PASS/FAIL 摘要" 这种语义压缩，请写专属 Go filter（第一层），按命令语义 parse 后生成摘要、parse 失败 fallback 到原文。用户规则里若出现已弃用字段，loader 会打一次 warning 指引迁移，规则的无损部分仍然生效。

内置 TOML 规则覆盖：`docker`（ps/images/logs）、`kubectl`、`node`（npm/yarn/pnpm install/test/build）、`python`（pip/pytest/venv）、`rust`（cargo build/test/check/clippy）。

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
    _ "github.com/Anthoooooooony/gw/filter/git"
    _ "github.com/Anthoooooooony/gw/filter/java"
    _ "github.com/Anthoooooooony/gw/filter/pytest"
    _ "github.com/Anthoooooooony/gw/filter/toml"
)
```

import 顺序只控制 `init()` 运行次序，**不承载优先级语义**——优先级由 filter 自身实现 `filter.Fallback` 接口声明（TOML 兜底，Go 专属过滤器非兜底）。

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
│   ├── claude.go                   # gw claude — 透明包装 claude CLI 并启动本地 API 代理
│   ├── summary.go                  # gw summary — dispatcher + --text / --port / --no-browser flag（gain 作为 alias 保留）
│   ├── summary_web.go              # gw summary web dashboard — embed.FS + net/http + SSE + 跨平台浏览器打开
│   ├── web/                        # embed 静态资源：index.html（单文件 dashboard）+ chart.umd.js
│   ├── version.go                  # gw --version / gw version（ldflags + runtime/debug fallback）
│   ├── inspect.go                  # gw inspect [id] — 查询历史记录，--raw 打印原文
│   ├── filters.go                  # gw filters list — 列出已注册过滤器及来源
│   └── registry.go                 # blank import 触发过滤器自注册
│
├── filter/                         # 过滤器核心
│   ├── filter.go                   # Filter / StreamFilter / StreamProcessor 主接口 + Fallback / Describable / SubnameResolver 可选接口
│   ├── registry.go                 # 全局注册表（sync.RWMutex 并发安全）+ Register / Find / FindStream / List
│   ├── all/all.go                  # 聚合所有过滤器包的 blank import
│   ├── git/
│   │   ├── status.go              # git status 过滤器
│   │   └── log.go                 # git log 过滤器（紧凑格式）
│   ├── java/
│   │   ├── maven.go               # Maven 过滤器（批量 + 流式，错误去重）
│   │   ├── maven_state.go         # Maven 状态机（11 态 + 21 种行分类 + 转移逻辑）
│   │   ├── gradle.go              # Gradle 过滤器（批量白名单 + 流式状态机）
│   │   ├── springboot.go          # Spring Boot 过滤器（banner + logger name 匹配）
│   │   └── dedupset.go            # 有界去重集合（cap 10000，防长驻构建 OOM）
│   ├── pytest/
│   │   └── pytest.go              # pytest / python -m pytest 语义过滤器（summary + FAILURES 锚点）
│   └── toml/
│       ├── engine.go              # TOML 声明式过滤引擎（v2 DSL 无损字段）
│       ├── loader.go              # TOML 三级加载（builtin / user / project）
│       └── rules/                 # 内置 TOML 规则（go:embed）
│           ├── docker.toml
│           ├── kubectl.toml
│           ├── node.toml          # npm / yarn / pnpm
│           ├── python.toml        # pip / venv（pytest 交由 filter/pytest 接管）
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
│   ├── procgroup_other.go         # 非 unix 平台降级实现（仅杀主进程）
│   └── apiproxy/                  # gw claude 子命令的本地 HTTP 代理（Claude Code → Anthropic API）
│       ├── server.go              # 127.0.0.1 随机端口 + httputil.ReverseProxy
│       ├── anthropic.go           # /v1/messages handler + BodyTransformer 注入点
│       ├── env.go                 # GW_APIPROXY_* 环境变量解析（body 上限 / header 超时 / 关闭 grace）
│       └── dcp/                   # DCP 风格 tool_result 去重（同签名 tool_use 历史替换为占位符）
│           ├── dedup.go           # Transformer.Transform：解析 → 改写 → 序列化
│           ├── signature.go       # tool_use 签名算法（name + sorted input JSON）
│           ├── types.go           # messagesRequest / message / tool_use_block 等 JSON 模型
│           └── stats.go           # 原子计数器（请求/扫描/替换/字节）供摘要打印
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

import "github.com/Anthoooooooony/gw/filter"

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
_ "github.com/Anthoooooooony/gw/filter/node"
```

### 添加 TOML 声明式规则

在 `filter/toml/rules/` 下新建 `.toml` 文件即可，无需写 Go 代码：

```toml
# filter/toml/rules/terraform.toml
[terraform.plan]
match = "terraform plan"
strip_ansi = true
max_lines = 100
```

DSL 只接受无损字段：`strip_ansi` / `head_lines` / `tail_lines` / `max_lines` / `on_empty`。如果这些长度兜底对你的命令不够，需要真正的语义压缩（区分噪音和信号），应该写专属 Go filter 而不是硬塞 TOML。

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
go test ./...           # 运行全部测试
go test ./... -v        # 详细输出
go test ./filter/java/  # 只跑 Java 过滤器测试
```

测试 fixture 使用 Docker 从真实开源项目抓取的构建输出（Maven 3 模块项目、Gradle 2 模块项目、Spring Petclinic），不使用手写模拟数据。

便捷命令：

```bash
make test        # 跑 CI 对齐的 race + cover 全量测试
make test-fast   # 本地反复迭代用，不带 race 与 cover
make ci          # 本地跑 CI 核心 gate（tidy / vet / test / bump-test）等价集
```

场景化压缩率 baseline（`filter/scenario_test.go`）的维护流程见 [`CONTRIBUTING.md`](../CONTRIBUTING.md#场景化压缩率-baseline)。

## 调试 `gw claude` 代理

当 `gw claude` 行为异常（请求没进代理、DCP 替换没生效等）时：

- **verbose 模式打逐请求日志**：`gw -v claude` 会在 stderr 打每条 `/v1/messages` 的 `dcp: 替换 N 条 tool_result` / `dcp: 解析失败，透传` 信息
- **每次退出都会打出统计摘要**（非 verbose 也会）：`gw: dcp: N 请求 / 扫 M tool_use / 替换 K tool_result / 节省 B 字节`；0 请求时静默
- **健康检查端点**：代理监听的 127.0.0.1 随机端口暴露 `/_gw/health`，返回 `200 ok`；查到具体端口可用 `lsof -iTCP -sTCP:LISTEN -P | grep gw` 或读 verbose 日志里的 `apiproxy 已监听 http://127.0.0.1:PORT`
- **上游逃生舱**：`GW_APIPROXY_UPSTREAM=https://xxx` 把上游替换为任意 URL，测试场景下可指向本地 mock
- **不支持 Bedrock/Vertex**：`CLAUDE_CODE_USE_BEDROCK=1` / `CLAUDE_CODE_USE_VERTEX=1` 模式下 claude 忽略 `ANTHROPIC_BASE_URL`，代理失效；这些用户应直接运行 `claude`，不走 `gw claude`

## 环境变量参考

所有 gw 可识别的环境变量。按用途分组；速查表见 [`CLAUDE.md` "环境变量"](../CLAUDE.md#环境变量) 节。

### 执行（`gw exec`）

#### `GW_CMD_TIMEOUT` — 命令执行超时兜底

控制 `gw exec` 执行外部命令时的最长耗时，避免恶意命令或网络挂死导致 Claude Code 的 PreToolUse hook 无限阻塞。

| 值 | 语义 |
|----|------|
| 未设置 / 空 | 使用默认值 `10m` |
| `10m` / `30s` / `500ms` / `2h` 等 | 使用 `time.ParseDuration` 可解析的任意 duration |
| `0` / `off` / `none` / `disable` / `disabled` | 禁用超时（长驻命令场景） |
| `-1s` / `-500ms` 等负值 | 视为禁用，等同 `off` |
| 无法解析的值 | 写 warning 到 stderr，fallback 到默认 `10m` |

**两阶段终止**：
1. 到期后对整个进程组（`Setpgid` + `kill(-pgid, sig)`）发送 `SIGTERM`
2. 5 秒宽限期后若进程仍存活，发送 `SIGKILL`

**退出码约定**：超时场景统一返回 `124`（GNU `timeout(1)` 惯例），stderr 末尾追加 `gw: command timed out after <dur> (SIGTERM[, SIGKILL])`。

**批量 vs 流式**：
- 批量路径（`internal.RunCommand`）：超时后 `CommandResult.ExitCode = 124`，stderr 追加提示，不返回 Go error，走正常 `ApplyOnError` 路径
- 流式路径（`internal.RunCommandStreamingFull`）：超时后返回 `exitCode = 124`（非 `-1`），调用方 `proc.Flush(124)` 能拿到非零 exit 从而输出错误上下文

**平台兼容**：
- 进程组相关代码在 `internal/procgroup_unix.go`，`//go:build unix` 覆盖 macOS / Linux / *BSD
- 非 unix 平台（如 Windows）在 `internal/procgroup_other.go` 提供仅杀主进程的降级实现

### 存储（tracking DB）

#### `GW_DB_PATH` — 覆盖 tracking DB 路径

默认 `~/.gw/tracking.db`。HOME 只读时降级到 `$TMPDIR/gw-tracking.db` 并 stderr warn 一次。
设置该变量可把 DB 放在任意可写路径（CI 临时目录、共享挂载等），路径不存在时按常规 `MkdirAll` + open 流程处理。

#### `GW_DB_MAX_BYTES` — tracking DB 硬阈值

默认 `104857600`（100 MiB）。`gw exec` 每次把原始输出连同统计一并写入 `records.raw_output`；DB 超过阈值时，`gw summary` 会按 timestamp 删最旧记录并 `VACUUM`，压到软目标（默认 80%）。设 `0` 或负值关闭裁剪。

### Dashboard（`gw summary`）

#### `NO_BROWSER` — 抑制自动开浏览器

默认未设。`gw summary` 默认启 `127.0.0.1` 本地 server 并调 `open`（macOS）/ `xdg-open`（Linux）/ `rundll32`（Windows）打开 dashboard。`NO_BROWSER` 非空时仍启 server，但跳过浏览器 spawn，只在 stderr 打印 URL。典型用途：SSH + port forward、headless 容器、CI 里想 probe 一下 dashboard URL 然后手工访问。

### API 代理（`gw claude`）

| 变量 | 默认 | 用途 |
|------|------|------|
| `GW_APIPROXY_MAX_BODY` | `33554432`（32 MiB） | 代理接受的最大 POST body 字节数，超限回 413 |
| `GW_APIPROXY_HEADER_TIMEOUT` | `60s` | 等上游响应头的最长时间（不影响 SSE 正文） |
| `GW_APIPROXY_SHUTDOWN_TIMEOUT` | `5s` | 代理 shutdown 的 grace period |
| `GW_APIPROXY_UPSTREAM` | `https://api.anthropic.com` | 代理的上游 URL（测试用逃生舱） |

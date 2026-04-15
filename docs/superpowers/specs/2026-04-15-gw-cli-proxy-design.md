# GW — CLI Proxy for AI Coding Tools

> Go 语言 CLI 代理，通过 PreToolUse Hook 拦截命令输出并压缩，减少 LLM token 消耗。商业产品 MVP。

## 1. 产品定位

| 维度 | 决策 |
|------|------|
| 形态 | 商业产品 MVP，面向企业级私有部署 |
| 技术栈 | Go，单二进制分发 |
| 范围 | CLI proxy 层（PreToolUse Hook），暂不做 API proxy |
| 目标 AI 工具 | 只支持 Claude Code |
| 核心差异化 | Java 生态深度支持（Maven/Gradle/Spring Boot）+ 商业授权 |
| 企业特性 | MVP 不做（License 验证、审计日志、集中配置等后续迭代） |

## 2. 整体架构

### 2.1 数据流：六阶段管道

```
PARSE → ROUTE → EXECUTE → FILTER → PRINT → TRACK
```

一次命令的完整生命周期：

1. **PARSE** — 解析命令和参数（cobra 或类似框架）
2. **ROUTE** — 查询过滤器注册表，匹配命令处理器
3. **EXECUTE** — 在本地执行原始命令，捕获 stdout + stderr + exit code
4. **FILTER** — 应用匹配的过滤器压缩输出
5. **PRINT** — 输出压缩结果到 stdout，stderr 保持原样
6. **TRACK** — 记录 token 节省量到本地 SQLite

失败安全原则：任何环节出错 → 透传原始输出 + 原始 exit code。

### 2.2 入口命令

| 命令 | 用途 |
|------|------|
| `gw exec <cmd...>` | 核心：执行命令 + 过滤输出 |
| `gw rewrite "<cmd>"` | Hook 调用：判断是否有匹配过滤器，输出改写后的命令 |
| `gw init` | 一键安装 Claude Code PreToolUse hook |
| `gw uninstall` | 移除 hook |
| `gw gain` | 查看 token 节省统计 |

## 3. 过滤器设计

### 3.1 双层架构

**第一层：Go 硬编码过滤器** — 针对高频命令的深度优化，每个命令根据输出特征选择最优策略。

**第二层：TOML 声明式过滤器** — 覆盖长尾命令，用户可扩展，无需写 Go 代码。

路由逻辑：命令解析 → 查硬编码注册表 → 未匹配则查 TOML 规则 → 都不匹配则纯透传。

### 3.2 Filter 接口

```go
type Filter interface {
    // 是否匹配此命令
    Match(cmd string, args []string) bool
    // 成功时的过滤（exit code == 0）
    Apply(input FilterInput) FilterOutput
    // 失败时的过滤（exit code != 0）
    // 返回 nil 表示无专用失败逻辑，由框架透传原始输出
    ApplyOnError(input FilterInput) *FilterOutput
}

type FilterInput struct {
    Cmd      string
    Args     []string
    Stdout   string
    Stderr   string
    ExitCode int
}

type FilterOutput struct {
    Content  string // 压缩后的输出
    Original string // 原始输出（用于 token 统计对比）
}
```

### 3.3 错误处理策略（三级）

| 场景 | exit code | 策略 |
|------|-----------|------|
| 命令成功 | 0 | 激进压缩（去下载日志、进度条、冗余信息） |
| 命令失败 + 有专用失败过滤器 | != 0 | 使用 `ApplyOnError()`：仍去噪音，但保留完整的错误信息和堆栈 |
| 命令失败 + 无专用失败过滤器 | != 0 | `ApplyOnError()` 返回 nil → 透传原始 stdout + stderr |

核心原则：成功时压缩噪音，失败时保留诊断信息。没有专用失败过滤器的命令，宁可不压缩也不丢失调试信息。

此策略优于 RTK 的 opt-in 模型（RTK 大部分命令在失败时仍激进压缩，已导致 Playwright 测试失败信息被压缩 90-99% 等实际问题）。

### 3.4 MVP 命令覆盖范围

**第一层：Go 硬编码过滤器**

| 生态 | 命令 | 过滤策略 |
|------|------|---------|
| Git | status, log, diff, show, push, pull | 去教学提示、进度条，保留文件状态和核心信息 |
| Maven | `mvn compile/test/package/install` | 去下载日志、INFO 前缀；成功只留摘要，失败保留错误+堆栈 |
| Gradle | `gradle build/test` (`gradlew`) | 去 task 进度条、daemon 日志；成功只留摘要，失败保留完整报告 |
| Spring Boot | 启动日志、运行时日志 | 折叠 banner、去自动配置噪音、保留关键 bean 错误和端口信息 |
| System | ls, find, grep | 基础截断和去重 |

**第二层：TOML 声明式规则** — docker, kubectl, terraform, npm, pnpm 等长尾命令通过 TOML 规则覆盖。

### 3.5 Maven 过滤示例

**成功时** (`mvn test`, exit 0):

```
原始 (~300 行):
  [INFO] Downloading from central: https://... (50 行下载日志)
  [INFO] --- maven-surefire-plugin:3.0.0:test ---
  [INFO] Tests run: 142, Failures: 0, Errors: 0, Skipped: 3
  ...

压缩后 (~3 行):
  BUILD SUCCESS
  Tests: 142 run, 0 failed, 3 skipped (2.3s)
```

**失败时** (`mvn test`, exit 1):

```
原始 (~300 行):
  [INFO] Downloading... (50 行下载日志)
  [ERROR] COMPILATION ERROR:
  [ERROR] /src/main/java/Foo.java:[42,5] cannot find symbol: class Bar
  [INFO] BUILD FAILURE

压缩后 (~5 行，去噪音但保留诊断):
  BUILD FAILURE — Compilation Error
  /src/main/java/Foo.java:42 — cannot find symbol: class Bar
```

### 3.6 TOML 声明式引擎

处理管道：

```
strip_ansi → regex_replace → match_lines → keep/strip_lines → truncate → head/tail → max_lines → on_empty
```

规则示例：

```toml
[kubectl.get]
match = "kubectl get"
strip_ansi = true
max_lines = 50
keep_lines = ["NAME", "STATUS", "READY", "AGE"]
```

规则查找优先级：项目级 `.gw/filters/` → 用户级 `~/.gw/filters/` → 内置规则（编译时 embed）。

## 4. Hook 系统

### 4.1 安装

```bash
gw init
```

执行操作：
1. 检测 Claude Code 配置目录 (`~/.claude/`)
2. 写入 PreToolUse hook 配置到 `~/.claude/settings.json`
3. 验证 `gw` 在 PATH 中
4. 输出安装成功信息 + 测试命令

写入的 hook 配置：

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hook": "gw rewrite \"$command\""
      }
    ]
  }
}
```

### 4.2 命令改写逻辑

`gw rewrite` 内部的 shell lexer 按 `&&`、`||`、`;` 分段，逐段查询过滤器注册表。

**管道和重定向策略（比 RTK 更保守）**：检测到 `|`、`>`、`>>`、`<`、`$(` 等操作符时，整条命令不改写，直接 exit 1 透传。

理由：管道场景下命令输出是给下游程序消费的，压缩会破坏数据完整性。RTK 在这方面有已知 bug（#838, #1087, #1282）。

改写规则：

| 命令类型 | 示例 | 行为 |
|---------|------|------|
| 简单命令 | `git status` | → `gw exec git status` |
| 链式命令 | `mvn clean && mvn test` | → `gw exec mvn clean && gw exec mvn test` |
| 管道命令 | `git log \| grep fix` | 不改写，原样透传 |
| 重定向 | `mvn test > out.txt` | 不改写，原样透传 |
| 子 shell | `$(git rev-parse HEAD)` | 不改写，原样透传 |
| 无匹配规则 | `python script.py` | 不改写，exit 1 透传 |

### 4.3 退出码协议

| `gw rewrite` 退出码 | 含义 | Claude Code 行为 |
|---------------------|------|-----------------|
| 0 | 改写成功 | 自动执行改写后的命令 |
| 1 | 无匹配/不改写 | 原样执行原始命令 |
| 2 | 命令被阻止 | 不执行 |

## 5. Token 追踪与统计

### 5.1 存储

SQLite，存储在 `~/.gw/tracking.db`：

```sql
CREATE TABLE tracking (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp     TEXT    NOT NULL,
    command       TEXT    NOT NULL,
    exit_code     INTEGER NOT NULL,
    input_tokens  INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    saved_tokens  INTEGER NOT NULL,
    elapsed_ms    INTEGER NOT NULL,
    filter_used   TEXT    NOT NULL
);
```

### 5.2 Token 估算

MVP 使用 `ceil(chars/4)` 近似值。逻辑隔离在 `track/token.go` 中，后续可替换为更精确的实现（按字符类型加权等）。

### 5.3 统计查看

```bash
gw gain

# 输出:
# ──── Token Savings Report ────
# Today:     12,340 tokens saved (73%)
# This week: 89,210 tokens saved (71%)
# Total:     342,800 tokens saved across 1,247 commands
#
# Top commands:
#   mvn test        48,200 saved (82%)
#   git log         18,400 saved (75%)
#   gradle build    15,600 saved (79%)
#
# Avg overhead: 6ms per command
```

### 5.4 设计约束

- 写入失败不阻断命令执行
- 90 天前的记录自动删除
- 追踪模块可独立移除，不影响核心过滤功能

## 6. 配置系统

### 6.1 配置层级（优先级从高到低）

1. 命令行参数 — `gw exec --verbose mvn test`
2. 项目级配置 — `.gw/config.toml`
3. 用户全局配置 — `~/.gw/config.toml`
4. 内置默认值

### 6.2 配置格式

```toml
# ~/.gw/config.toml

[general]
verbose = false

[tracking]
enabled = true
retention_days = 90

[filters]
disabled = ["system/cat"]
```

## 7. 项目结构

```
gw/
├── main.go
├── cmd/
│   ├── exec.go
│   ├── rewrite.go
│   ├── init.go
│   ├── uninstall.go
│   └── gain.go
│
├── filter/
│   ├── filter.go          # Filter 接口定义
│   ├── registry.go        # 过滤器注册表 + 路由
│   ├── git/
│   │   ├── status.go
│   │   ├── log.go
│   │   └── diff.go
│   ├── java/
│   │   ├── maven.go
│   │   ├── gradle.go
│   │   └── springboot.go
│   ├── system/
│   │   └── ls.go
│   └── toml/
│       ├── engine.go      # TOML 声明式引擎
│       └── rules/         # 内置 TOML 规则（go:embed）
│
├── hook/
│   ├── install.go         # Claude Code hook 安装/卸载
│   └── lexer.go           # Shell 命令 lexer
│
├── track/
│   ├── db.go              # SQLite 存储
│   ├── stats.go           # 统计计算
│   └── token.go           # Token 估算
│
├── config/
│   └── config.go          # 配置加载
│
└── internal/
    └── exec.go            # 子进程执行器
```

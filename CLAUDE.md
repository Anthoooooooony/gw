# CLAUDE.md

## 项目概述

gw 是一个 Go CLI proxy，通过 Claude Code PreToolUse Hook 拦截 shell 命令，本地执行后过滤输出，减少 LLM token 消耗。商业产品 MVP，面向企业级私有部署。

- **仓库:** `Anthoooooooony/gw` (private)
- **技术栈:** Go 1.22+, cobra, go-sqlite3, BurntSushi/toml, go:embed
- **核心差异化:** Java 生态深度支持（Maven 状态机 / Gradle / Spring Boot），RTK 对这些只用 TOML 正则

## 架构

### 六阶段管道

```
PARSE → ROUTE → EXECUTE → FILTER → PRINT → TRACK
```

### 双执行路径

- **批量路径:** `cmd.Run()` 等待命令退出 → 拿到全部输出 → 过滤（git status/log 等短命令）
- **流式路径:** `cmd.StdoutPipe()` + `bufio.Scanner` 逐行读取 → 实时过滤（Maven/Spring Boot 等可能长时间运行的命令）
- 选择逻辑: 过滤器实现了 `StreamFilter` 接口 → 走流式；否则走批量

### 双层过滤器

- **第一层: Go 硬编码过滤器** — git(status/log), java(maven/gradle/springboot), 深度优化
- **第二层: TOML 声明式引擎** — docker, kubectl 等长尾命令，go:embed 内嵌规则
- 路由: 硬编码注册表 → TOML fallback → 无匹配则透传

### 自注册模式

过滤器通过 `func init() { filter.Register(&XxxFilter{}) }` 自注册。`filter/all/all.go` 用 blank import 聚合所有过滤器包。新增过滤器只需：写包 + init() + 在 all.go 加一行 import。

## 过滤策略设计原则

### 三级错误处理

| 场景 | 策略 |
|------|------|
| 命令成功 (exit 0) | 激进压缩 |
| 命令失败 + 有 ApplyOnError | 去噪音但保留错误详情 |
| 命令失败 + ApplyOnError 返回 nil | 透传原始输出 |

### 长驻进程排除

Maven/Gradle 的 Match 函数排除长驻进程 goal（spring-boot:run, bootRun 等）。SpringBootFilter 只实现 StreamFilter（流式模式），不走批量路径（java -jar 是长驻进程，批量模式会永远阻塞）。

### 管道/重定向处理

Shell lexer（`shell/lexer.go`）是引号感知的单遍 tokenizer。检测到 `|` `>` `<` `$(` `` ` `` `&` 等操作符时整条命令不改写（比 RTK 更保守，RTK 有管道场景的已知 bug）。`||` `&&` `;` 是链式操作符，逐段改写。

## 匹配与过滤规则指南

### 优先使用结构化特征匹配，避免内容关键词

**正确做法:**
- 用 **logger name** 匹配日志来源: `strings.Contains(line, "org.hibernate")` — 基于日志框架的结构化字段
- 用 **固定前缀/标记** 匹配框架输出: `[INFO] Scanning for projects` — 来自 Maven `ExecutionEventLogger.java` 的固定代码
- 用 **状态机** 追踪上下文: Maven 输出有严格阶段序列，同一行在不同阶段含义不同

**错误做法:**
- 用 **短关键词** 匹配内容: ~~`strings.Contains(line, "HHH")`~~ — 可能误伤包含 HHH 的业务日志
- 用 **纯字符串包含** 判断: ~~`strings.Contains(line, ">")`~~ — 无法区分引号内外

### 何时用状态机 vs 正则

| 输出特征 | 方案 | 例子 |
|---------|------|------|
| 严格阶段序列 + 同一行不同阶段含义不同 | **状态机** | Maven（ExecutionEventLogger 事件模型） |
| 扁平结构 + 每行自带上下文标识 | **正则** | Gradle（`> Task :` 前缀）、Spring Boot（logger name） |
| 长尾命令，输出格式简单 | **TOML 声明式** | docker, kubectl |

### Maven 状态机（最复杂的过滤器）

基于 Maven 源码 `ExecutionEventLogger.java` 的事件模型，11 个状态：

```
StateInit → StateDiscovery → StateWarning → StateModuleBuild → StateMojo → StatePluginOutput
                                                                              ↓
                                                               StateTestOutput → StateReactor → StateResult → StateStats → StateErrorReport
```

状态转移由固定标记行驱动（`Scanning for projects` / `Building xxx` / `--- plugin ---` / `Reactor Summary` / `BUILD SUCCESS|FAILURE` / `Total time:`）。这些标记来自 Maven 核心代码，极少变动。

### Spring Boot 过滤规则

优先按 **logger name** 过滤（结构化字段），其次按内容关键词（足够唯一的才用）：

| 噪音类型 | 匹配方式 | 规则 |
|---------|---------|------|
| Hibernate | logger name | `org.hibernate` / `o.hibernate` / `o.h.` |
| Tomcat 引擎 | logger name | `o.apache.catalina.core.Standard` |
| HikariCP | 内容关键词（足够唯一） | `HikariPool` / `HikariDataSource` |
| Spring Data | 内容关键词 | `RepositoryConfigurationDelegate` |
| Banner | 内容特征 | `____` / `:: Spring Boot ::` / ASCII art 装饰行 |

### 错误去重

`extractErrorKey()` 提取错误签名用于去重：
- `Unresolved reference 'Xxx'` → key = `unresolved:...`
- `Type mismatch: ...` → key = `type_mismatch:...`
- `Cannot access class 'Xxx'` → key = `access:...`
- 其他错误不去重

## 关键源码参考（上游项目）

过滤规则的设计基于对上游项目源码的分析：

- **Maven:** `ExecutionEventLogger.java` — 所有生命周期日志的唯一来源
- **Maven:** `AbstractMavenTransferListener` — 下载日志的来源，固定前缀 `Downloading from` / `Downloaded from`
- **Maven:** `BuildExceptionReporter` — 错误报告区域的固定标记 `* What went wrong:` / `* Try:`
- **Gradle:** `PrettyPrefixedLogHeaderFormatter` — `> ` 前缀的来源
- **Gradle:** `BuildResultLogger` — `BUILD SUCCESSFUL/FAILED` 的来源
- **Spring Boot:** `SpringBootBanner.java` — banner 直接 stdout（不走日志框架）
- **Spring Boot:** `defaults.xml` — 日志格式模板 `%d %5p %pid --- [%t] %-40.40logger{39} : %m%n`

## 构建和测试

```bash
go build -o gw .              # 构建
go test ./... -v               # 运行全部测试（164+）
./gw exec git status           # 测试过滤
./gw rewrite "mvn clean test"  # 测试改写
./gw gain                      # 查看 token 节省统计
```

## 代码规范

- 注释和 commit message 使用中文
- 使用 linux 换行符（\n）
- 新增过滤器后在 `filter/all/all.go` 添加 blank import
- 测试 fixture 使用真实 Docker 构建输出，不手写模拟数据

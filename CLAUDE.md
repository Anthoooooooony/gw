# CLAUDE.md

该文件为 Claude Code（claude.ai/code）在本仓库工作时提供 Runbook：顶部 "快速导航" 给出最常回查的文件索引，中段 "日常任务模板" 固化高频操作流程，底部 "开发流程与雷区" 标明必须遵守的 DO/DON'T 与既有不变式。

## 项目概览

gw 是一个 CLI 代理，作为 Claude Code PreToolUse Hook 运行，拦截 shell 命令并压缩输出，减少 LLM token 消耗。

## 延伸阅读（按需主动取用）

本文件只承载高频 runbook。遇到以下场景**必须**先读对应文档再动手，别猜：

| 文档 | 触发场景 |
|------|---------|
| [`README.md`](./README.md) | 产品定位、效果对比、安装 / 卸载、环境变量速查 |
| [`docs/USAGE.md`](./docs/USAGE.md) | 使用者视角：支持的命令矩阵、临时绕过、TOML 自定义规则、`gw inspect` 回溯、数据隐私 |
| [`docs/DEVELOPING.md`](./docs/DEVELOPING.md) | 架构总览、六阶段管道、双层过滤器、错误处理机制、Maven 状态机、项目结构、环境变量完整语义 |
| [`docs/DECISIONS.md`](./docs/DECISIONS.md) | ADR-lite 决策记录；改架构 / 公开接口 / 产品边界前先检索有无同主题前置决策，新决策同 PR 追加到顶部 |
| [`CONTRIBUTING.md`](./CONTRIBUTING.md) | 7-gate CI 细则、Conventional Commits 前缀到 release notes 的映射、场景化压缩率 baseline 维护流程、Issue 规范 |
| [`.claude/skills/feature-dev/SKILL.md`](./.claude/skills/feature-dev/SKILL.md) | 跨模块 feature 开发的四阶段流程（需求澄清 → code-explorer → architect → reviewer） |

**一般读取时机**：动工前读相关文档 → 改动时本文件的 DO / DON'T 做兜底约束 → 触发架构变更时写 `docs/DECISIONS.md`。

## 快速导航

开发时最常回查的文件一览（其余文件按需 `Grep`）：

| 任务 | 先读 |
|------|------|
| `gw exec` 执行管道（PARSE → ROUTE → EXECUTE → FILTER → PRINT → TRACK） | `cmd/exec.go` |
| `gw claude` 代理入口 + 去重摘要 | `cmd/claude.go` |
| 过滤器接口定义（Filter / StreamFilter / Fallback / Describable） | `filter/filter.go` |
| 过滤器注册与优先级（"第一匹配胜出"） | `filter/registry.go` + `filter/all/all.go` |
| 压缩率 baseline 断言与更新流程 | `filter/scenario_test.go` + `filter/testdata/scenario_baseline.json` |
| TOML DSL v2 无损变换约束 | `filter/toml/engine.go` |
| TOML 三级加载（builtin → user → project） | `filter/toml/loader.go` |
| Maven 状态机（语义过滤器参考实现） | `filter/java/maven.go` |
| tool_result 内容去重 | `internal/apiproxy/dedup/dedup.go` |
| 引号感知 shell tokenizer | `shell/lexer.go` |
| Release 版本号计算 + CC 前缀分类 | `scripts/release-helpers.sh` |
| DB schema 迁移约定 | `track/db.go` |

## 日常任务模板

### 新增过滤器

先判断类型：

| 需求 | 用 | 原因 |
|------|----|------|
| 按 exit_code / 结构化语义压缩（成功摘要、失败留错误） | **Go 硬编码** | TOML 无语义能力 |
| 纯前后裁剪、去 ANSI、按行数限制 | **TOML** | 零代码、用户可覆盖 |

**Go 硬编码 3 步最短路径**：

1. 新建 `filter/<pkg>/<name>.go`，`init()` 里调 `filter.Register(&XxxFilter{})`
2. `filter/all/all.go` 加一条空白 import：`_ "github.com/Anthoooooooony/gw/filter/<pkg>"`
3. 在 `filter/scenario_test.go` 的 `scenarios` 切片追加条目 → 跑 `go test ./filter/ -run TestScenarioCompression -args -update` 更新 baseline → **人工 review diff 再提交**

**TOML 1 步**：`filter/toml/rules/*.toml` 新增（全局 builtin，需要 PR）或让用户放到 `.gw/rules/*.toml`（项目级覆盖，无需 PR）。

流式能力：同时实现 `filter.StreamFilter` 接口即可让长驻命令（`tail -f` 类）走流式路径；`Registry.FindStream()` 的 type assertion 失败会自动降级到批量。

### 跑测试

| 命令 | 何时用 |
|------|------|
| `make test` | 推分支前、与 CI 对齐（race + cover + 全平台） |
| `make test-fast` | 本地快速迭代（无 race / 无 cover） |
| `go test ./filter/ -run TestScenarioCompression -args -update` | 改过滤器后更新 baseline |
| `go test -run Timeout ./internal/ -timeout 60s` | 超时相关专项 |
| `make ci` | 本地复现 CI 主路径（tidy + vet + test） |

**`CGO_ENABLED=1` 是硬性前提**（`mattn/go-sqlite3` 依赖 CGO）。Makefile 已强制开启；手跑 `go test` 前若 shell 环境显式关了 CGO，会直接 cc 编译失败。

场景 baseline 断言偏差 ≤ 2pp。有意改动压缩率后必须跑 `-args -update` 重写 baseline，否则 CI 的 `TestScenarioCompression` 会红。

### Release（全自动，不可手工干预）

每次 master push，`.github/workflows/release.yml` 的 decide job 扫自上个 tag 以来的 commit subject，按 CC 前缀决定 bump：

| CC 前缀 | 动作 |
|---------|------|
| `feat:` | minor bump + 发版 |
| `fix:` / `perf:` / `refactor:` / `revert:` / `deps:` | patch bump + 发版 |
| `docs:` / `chore:` / `ci:` / `test:` | **不触发**发版 |

验证预期 bump 读 `scripts/release-helpers.sh::classify_commit`。**禁止**手工 tag / 手工 release notes / 维护 `CHANGELOG.md`（workflow 全包）。发布记录看 [Releases 页面](https://github.com/Anthoooooooony/gw/releases)。

### 调 `gw claude` 代理

```bash
gw -v claude   # stderr 打 dedup: 替换 N 条 / 扫 M tool_use / 退出摘要
```

apiproxy 相关环境变量详见 [`docs/DEVELOPING.md`](./docs/DEVELOPING.md)：`GW_APIPROXY_MAX_BODY` / `GW_APIPROXY_HEADER_TIMEOUT` / `GW_APIPROXY_SHUTDOWN_TIMEOUT` / `GW_APIPROXY_UPSTREAM`。

### 复杂 feature 开发（跨模块 / 新 ADR / 影响面广）

走 [`/feature-dev`](./.claude/skills/feature-dev/SKILL.md) skill，固化四阶段流程：

1. 需求澄清与规模判断
2. `feature-dev:code-explorer` 摸底
3. `feature-dev:code-architect`（按需）+ 实现 + TDD
4. `feature-dev:code-reviewer` 自检 + PR

简单 bug fix / docs 不走本流程，按常规分支工作流即可。

## 开发流程与雷区

### GitHub Flow（默认）

所有改动走分支 → PR → 等 7-gate CI 全绿 → squash merge。分支前缀与 CC type 对齐：

| 前缀 | 语义 |
|------|------|
| `feature/` | 新功能 |
| `fix/` | Bug 修复 |
| `docs/` | 纯文档改动（不触发发版） |
| `chore/` | 构建、CI、依赖升级等非功能改动 |
| `refactor/` | 重构（可能触发 patch bump） |
| `hotfix/` | 紧急修复已发布版本 |

PR title 必须是 Conventional Commits。**仅当用户明确说"直推"时**才 push master。短期分支合入后立即删除。

### DO

- 推分支前本地跑 `make test` 对齐 CI
- 改过滤器时同步更新 `filter/testdata/scenario_baseline.json` diff，提交前人工 review
- `CGO_ENABLED=1` 始终开启
- 触发架构 / 公开接口 / 产品边界变更时同批提交 `docs/DECISIONS.md` 新条目（见下方 "决策留痕" 节）
- 脚本先在 macOS（BSD utils）跑通再推，确保 CI 不爆

### DON'T

- **别直推 master**（例外：用户明确说"直推"）
- **别给 TOML DSL 加 `strip_lines` / `keep_lines` 这类基于正则的行级裁剪字段**（永久否决；要语义压缩写 Go filter）。`on_error` 子表已作为例外放开，但其字段集**必须**限于无损变换（`strip_ansi` / `head_lines` / `tail_lines` / `max_lines` / `on_empty`），见下节
- **别在 worktree 布局下** `git checkout <主分支>`（会报 `'<branch>' is already used by worktree`，用 `git -C <worktree> ...` 或新建 worktree）
- **别用 GNU-only 工具选项**：`cat -A`、`grep --color=when`、BSD awk 不接受多行 `-v var=...`；本地若缺 `shellcheck` / `actionlint`，不要耗时间装，直接推分支让 CI 跑
- **别手工 bump 版本号**或维护 `CHANGELOG.md`（release.yml 全包）
- **别 `os.MkdirTemp + defer os.RemoveAll`**，用 `t.TempDir()`
- **别 `DROP TABLE` / `DROP COLUMN`**（用户 `~/.gw/tracking.db` 是生产数据；schema 只进不退）

## TOML 规则 DSL（v2：仅无损变换）

TOML 规则**只做语义无关的安全变换**，字段集：

- 主规则：`strip_ansi` / `head_lines` / `tail_lines` / `max_lines` / `on_empty`
- `[section.name.on_error]` 子表（失败场景独立参数，见 `docs/DECISIONS.md` 2026-04-22 "TOML DSL v2 扩展 on_error 子表"）：与主规则**完全同构**的字段集，禁止嵌套，无 `match`

**永久否决** `strip_lines` / `keep_lines`——基于正则的行级裁剪无法区分"真噪音"和"用户恰好需要的那一行"，长期会产生误删信任危机。想要 "pytest 只留 failures"、"vitest 生成 PASS/FAIL 摘要" 这类**语义压缩**写专属 Go filter（第一层），按命令语义 parse 后生成摘要、parse 失败 fallback 原文。`on_error` 子表只解决"成功/失败 tail 数量级不同"的场景，不是重新打开正则裁剪的入口。

用户规则若含已弃用字段（`strip_lines` / `keep_lines` / 旧版顶层 `on_error` 字符串），loader 打一次 warning 并丢弃值；规则的无损部分仍然生效。

## TOML 规则三级加载

TOML 声明式规则走**三级加载**，由 `filter/toml/loader.go::LoadAllRules` 统一合并。
按加载顺序从低到高，**高层同 ID 覆盖低层**：

1. **builtin**：`go:embed` 烘进二进制的 `filter/toml/rules/*.toml`
2. **user**：`os.UserConfigDir()/gw/rules/*.toml`
   - Linux：`$XDG_CONFIG_HOME/gw/rules/`（默认 `~/.config/gw/rules/`）
   - macOS：`~/Library/Application Support/gw/rules/`
   - Windows：`%AppData%\gw\rules\`
3. **project**：从当前工作目录向上查找 `.gw/rules/*.toml`，遇到 `.git` 目录或文件系统根时停止

规则唯一 ID 用 `section.name`（例如 `docker.ps`）。`disabled = true` 可让高层剔除同 ID 的低层规则。
解析错误只打 warning 到 stderr，不中断加载（企业环境鲁棒性要求）。

`gw filters list` 查看全部已注册的过滤器及其来源：

```
NAME              TYPE  SOURCE                                                 MATCH
git/status        go    builtin                                                git status
docker.ps         toml  user:///home/u/.config/gw/rules/docker-prod.toml       docker ps
myapp.logs        toml  project:///workspace/.gw/rules/custom.toml             myapp logs
```

## 环境变量

速查表。完整语义（超时两阶段终止、进程组杀死、平台差异、代理细节）见 [`docs/DEVELOPING.md` "环境变量参考"](./docs/DEVELOPING.md#环境变量参考)。

| 变量 | 类别 | 默认 | 用途 |
|------|------|------|------|
| `GW_CMD_TIMEOUT` | 执行 | `10m` | 命令执行超时兜底；`0` / `off` / 负值禁用；超时退出码 `124`（两阶段 SIGTERM → SIGKILL） |
| `GW_DB_PATH` | 存储 | `~/.gw/tracking.db` | tracking DB 路径；HOME 只读时降级到 `$TMPDIR/` |
| `GW_DB_MAX_BYTES` | 存储 | `104857600`（100 MiB） | DB 硬阈值，超限 `gw summary` 自动 VACUUM 裁剪；`0` / 负值关闭 |
| `NO_BROWSER` | Dashboard | 未设 | 非空时 `gw summary` 启 server 但不开浏览器 |
| `GW_APIPROXY_MAX_BODY` | 代理 | `33554432`（32 MiB） | `gw claude` 代理 POST body 上限，超限 413 |
| `GW_APIPROXY_HEADER_TIMEOUT` | 代理 | `60s` | 代理等上游响应头超时（不影响 SSE 正文） |
| `GW_APIPROXY_SHUTDOWN_TIMEOUT` | 代理 | `5s` | 代理 shutdown grace period |
| `GW_APIPROXY_UPSTREAM` | 代理 | `https://api.anthropic.com` | 代理上游 URL（测试逃生舱） |

## `gw summary` dashboard 降级矩阵

`cmd/summary.go` 的 dispatch 顺序：

1. `--text` 显式要求 → 纯文本，立即退出（无 server）
2. `stdout` 非 TTY 且用户未显式要 server（`--port` / `--no-browser` / `NO_BROWSER` 都未设）→ 纯文本，立即退出
3. 其余情况启 server。`openBrowserFlag = !--no-browser && NO_BROWSER == ""`；Linux 下 `$DISPLAY` + `$WAYLAND_DISPLAY` 都空时强制 `openBrowserFlag = false`（SSH 会话）
4. `openBrowser` 失败不中断（打印 URL + 保留 server）；`Ctrl+C` → `signal.NotifyContext` → `srv.Shutdown(3s)`

`cmd/summary_web.go` 持有 `//go:embed web` FS，静态资源零外部依赖（Chart.js 也 embed）。SSE `/api/events` 每 5s `buildSummaryPayload` → 每次新开 `track.NewDB`（用 `payloadMu` 串行化，避免 WAL 锁竞争）。payload schema 是前端契约，字段改名需同步 `cmd/web/index.html`。

## 执行路径关键不变式

- `RunCommand` 与 `RunCommandStreamingFull` 的函数签名**稳定**，超时/落盘等只通过环境变量或 flag 控制
- 流式路径超时后必须保证 `cmd/exec.go` 能调用 `proc.Flush(exitCode)`，即 `RunCommandStreamingFull` 不泄漏 goroutine、不死锁
- 信号终止（非超时）保持 `exitCode = -1` 语义，与超时的 `124` 区分开
- DB schema 演进只走 `ALTER TABLE ADD COLUMN`，**禁止** `DROP TABLE`（用户 `~/.gw/tracking.db` 是生产数据）

## Hook 安装约定

- `gw init` 往 `~/.claude/settings.json` 的 `hooks.PreToolUse[]` 注入一条 matcher：
  ```json
  {"matcher":"Bash","_gw_managed":true,"hooks":[{"type":"command","timeout":10,"command":"'/abs/path/to/gw' rewrite"}]}
  ```
  字段含义遵循 Claude Code hooks 文档（https://code.claude.com/docs/en/hooks.md）。
- `command` 字段写 **绝对路径**（`os.Executable()`），因 Claude Code hook 执行环境 PATH 受限；路径经 `shellQuote` 单引号包裹防空格/特殊字符
- `_gw_managed: true` 是 gw 的私有标记，`gw uninstall` 按此精确清理，不碰他人 matcher / 其他事件（PostToolUse 等）
- `gw rewrite` 是 hook 脚本：从 stdin 读 `{tool_input:{command:"..."}}`，匹配到可代理命令则 stdout 输出 `{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"allow",updatedInput:{command:"'/abs/gw' exec <原命令>"}}}` 让 Claude Code 走改写后的命令；未匹配则静默放行
- `gw init --dry-run` / `gw uninstall --dry-run` 只打印变更，不落盘
- 写入走同目录临时文件 + rename 原子替换，失败不留半截文件

## 日志与错误输出约定

gw 的 stderr 输出严格区分致命错误与非致命降级，便于 Claude Code hook 日志和 CI 抓取：

| 前缀 | 场景 | 示例 |
|------|------|------|
| `gw <subcmd>: <msg>` | 子命令致命错误，紧邻非零 exit | `gw exec: failed to open db: ...` |
| `gw: warning: <msg>` | 非致命降级 / 回退提示，程序继续执行 | `gw: warning: GW_CMD_TIMEOUT=abc unparseable, fallback to 10m` |
| `gw: info: <msg>` | 详细统计 / 调试信息，仅在 `--verbose` flag 下输出 | `gw: info: input_tokens=120 output_tokens=40 saved=80 elapsed=200ms` |

**禁止**使用 `[gw] warning: ...` 这种方括号风格 —— 与表格其他消息不一致，且在终端日志中难以 grep。

同一降级场景只 warn 一次（如 `GW_DB_PATH` 降级），避免多进程并发污染日志。

## 代码规范

### 通用

- Go 代码注释、日志、错误消息使用中文（项目主语言）
- 文件末尾统一 `\n` 行结尾
- 不引新依赖（TOML 解析器固定 `github.com/BurntSushi/toml`）
- 加载失败只 warning 不 panic（企业部署稳定性硬要求）

### 决策留痕（ADR-lite）

凡是影响**架构 / 公开接口 / 产品边界 / 环境依赖**的决策，必须在 `docs/DECISIONS.md` 追加一条。
触发条件举例：

- 新增或移除 CLI 子命令、环境变量、TOML DSL 字段
- 调整过滤器优先级机制、注册表 API、DB schema
- 决定**不**做某事（否决方案同等重要，防止半年后被同一想法轮回再提）
- 依赖升级/更换（BurntSushi/toml、go-sqlite3 等）
- 产品层面边界（如"只支持 Anthropic 协议，不做多厂商适配"）

**不**触发：内部重构、bug fix、测试补齐、文档润色、CI 调参。

格式：每条 `## YYYY-MM-DD — 标题`，含 **上下文 / 决策 / 替代方案 / 影响** 四段。新条目追加到文件顶部（最新在上）。PR 如触发上述场景，`docs/DECISIONS.md` 的新增条目与代码改动同批提交，reviewer 可一并审阅决策本身。

### 模块与接口

- 跨包访问能力用**显式接口**，不用具体类型断言。示例：
  - 过滤器在全局注册表中的优先级由 `filter.Fallback` 接口表达（`IsFallback() bool`），而不是靠 `filter/all/all.go` 的 import 顺序
  - `gw filters list` 通过 `filter.Describable` 拿过滤器来源，不 import `filter/toml` 具体类型
- `cmd/` 只依赖 `filter/` / `internal/` 的公开接口；新增"向 cmd 层暴露的能力"先在目标包加接口，再改 cmd，避免反向依赖
- 同名接口跨包复用时用类型别名（`type Logger = dedup.Logger`），不要重复声明

### 并发

- 全局可变状态必须显式加锁。`filter.Registry` 用 `sync.RWMutex`，`Find` / `List` 走 `RLock`，`Register` 走 `Lock`
- 信号 goroutine 关停顺序：**先 `signal.Stop(sigCh)`，再 `close(done)`**。反了会把信号送进已关闭的 channel 触发 panic（详见 `cmd/claude.go` 的 `waitForExit`）
- 流式路径禁止在回调里无上限累积。dedup/seen 集合走 `filter/java/dedupset.go::boundedDedupSet` 模式：到达 cap 后新元素不再入集，等同放弃去重但不 OOM

### 内存与字符串

- 纯集合语义用 `map[string]struct{}`，不要 `map[string]bool`（value 永远是 `true`，既浪费一个字节又让读者误以为语义是"map 到布尔"）
- 零值可用的类型优先零值：`var buf bytes.Buffer` 好于 `bytes.NewBuffer(nil)`；`var b strings.Builder` 同理
- 字符计数用 `utf8.RuneCountInString(s)`，**不要** `len([]rune(s))`——后者会分配完整 rune 切片。统一入口在 `track/token.go`
- 错误包装一律 `fmt.Errorf("... %w", err)` 保留因果链；直接丢 err 只有在加了独立语境且原 err 无信息量时才可行

### 测试

- 临时目录一律 `t.TempDir()`；**禁止** `os.MkdirTemp + defer os.RemoveAll`（测试 panic 下 defer 不跑会留脏目录，`t.TempDir` 注册到 testing runtime）
- 需要触发"慢响应"的 handler 用 channel 阻塞，不要 `time.Sleep`。`httptest.Server` 的 defer 顺序利用 LIFO：
  ```go
  block := make(chan struct{})
  slow := httptest.NewServer(...)   // handler 内 <-block
  defer slow.Close()                // 后声明，LIFO 里后跑
  defer close(block)                // 先声明，LIFO 里先跑：解除 handler 阻塞让 Close 能收敛
  ```
  顺序反了会导致 Close 等 handler、handler 等 close(block)，测试 hang 到超时
- 流式过滤器（`filter.StreamFilter`）的 `Flush` 必须覆盖三条路径：成功无 buffer / 失败有 buffer / 失败空 buffer
- 写 stderr/stdout 的函数参数化 `io.Writer`，测试注入 `&bytes.Buffer{}` 断言内容。示例：`cmd/claude.go::writeDedupSummary(w io.Writer, ...)`
- 场景压缩率改动同时更新 `filter/testdata/scenario_baseline.json`（见顶部 "跑测试" 节）

### API 边界

- `gw claude` 代理只接入 Anthropic 原生协议（`ANTHROPIC_BASE_URL` 指向本地 apiproxy）。**不**支持 `ANTHROPIC_BEDROCK` / `ANTHROPIC_VERTEX` 切换——上下文去重依赖 Anthropic messages schema，第三方供应商协议差异会让去重逻辑失配
- DB schema 演进只走 `ALTER TABLE ADD COLUMN`，**禁止** `DROP COLUMN` / `DROP TABLE`（见 `track/db.go` 迁移约定）
- `RunCommand` / `RunCommandStreamingFull` 的函数签名稳定；新配置开关走环境变量或 flag，不改签名

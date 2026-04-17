# CLAUDE.md

该文件为 Claude Code（claude.ai/code）在本仓库工作时提供指引，记录开发约定、环境变量与关键不变式。

## 项目概览

gw 是一个 CLI 代理，拦截 shell 命令并过滤输出，减少 LLM token 消耗。详见 `README.md`。

## 分支约定

两干分支模型：

| 分支 | 角色 | PR base |
|------|------|---------|
| `master` | 已发布代码，每次 tag 对应一个 release | —— |
| `dev` | 集成分支，GitHub default branch | feature PR 默认落这里 |
| `feature/*` | 功能/修复 | base = `dev` |
| `hotfix/*` | 紧急修复已发布版本 | base = `master`，merge 后 cherry-pick 到 dev |

版本机制：SemVer + `scripts/bump.sh [patch|minor|major]`，详见 `docs/superpowers/specs/2026-04-17-versioning-git-workflow-design.md`。

**hotfix 顺序铁律**：先 PR 到 master，merge → bump patch → 再 cherry-pick / merge master 到 dev。顺序反了会丢修复。

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

### `GW_CMD_TIMEOUT` — 命令执行超时兜底

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

### `GW_STORE_RAW` — 是否持久化原始输出到 SQLite

默认 **不** 把每次执行的原始输出写入 `~/.gw.db`（避免 DB 爆炸）。设为 `1` 后 `gw exec` 会把原始输出存入 `records.raw_output` 字段，供 `gw inspect [id] --raw` 回溯。

### `GW_DB_PATH` — 覆盖 tracking DB 路径

默认 `~/.gw/tracking.db`。HOME 只读时降级到 `$TMPDIR/gw-tracking.db` 并 stderr warn 一次。
设置该变量可把 DB 放在任意可写路径（CI 临时目录、共享挂载等），路径不存在时按常规 `MkdirAll` + open 流程处理。

## 日志与错误输出约定

gw 的 stderr 输出严格区分致命错误与非致命降级，便于 Claude Code hook 日志和 CI 抓取：

| 前缀 | 场景 | 示例 |
|------|------|------|
| `gw <subcmd>: <msg>` | 子命令致命错误，紧邻非零 exit | `gw exec: failed to open db: ...` |
| `gw: warning: <msg>` | 非致命降级 / 回退提示，程序继续执行 | `gw: warning: GW_CMD_TIMEOUT=abc unparseable, fallback to 10m` |

**禁止**使用 `[gw] warning: ...` 这种方括号风格 —— 与表格其他消息不一致，且在终端日志中难以 grep。

同一降级场景只 warn 一次（如 `GW_DB_PATH` 降级），避免多进程并发污染日志。

## 执行路径关键不变式

- `RunCommand` 与 `RunCommandStreamingFull` 的函数签名**稳定**，超时/落盘等只通过环境变量或 flag 控制
- 流式路径超时后必须保证 `cmd/exec.go` 能调用 `proc.Flush(exitCode)`，即 `RunCommandStreamingFull` 不泄漏 goroutine、不死锁
- 信号终止（非超时）保持 `exitCode = -1` 语义，与超时的 `124` 区分开
- DB schema 演进只走 `ALTER TABLE ADD COLUMN`，**禁止** `DROP TABLE`（用户 `~/.gw.db` 是生产数据）

## Hook 安装约定

- 写入 `~/.claude/settings.json` 的 hook 条目必须带 `_gw_managed: true` 标记，`gw uninstall` 按此标记清理
- `gw init --dry-run` / `gw uninstall --dry-run` 只打印变更，不落盘
- 写入前自动 backup 为 `settings.json.bak`，写入走临时文件 + rename 原子替换

## 关键文件

| 路径 | 职责 |
|------|------|
| `filter/toml/loader.go` | TOML 三级加载器、来源追踪、disabled 支持 |
| `filter/toml/engine.go` | TOML 过滤引擎（7 阶段管道），`LoadEngine` 调用 loader |
| `filter/registry.go` | 全局注册表 + `List()`（给 `gw filters list` 用） |
| `cmd/filters.go` | `gw filters list` 命令 |
| `cmd/version.go` | `gw --version` / `gw version`（ldflags + runtime/debug fallback） |
| `cmd/inspect.go` | `gw inspect [id]` 查询 DB 历史记录 |
| `internal/timeout.go` | `GW_CMD_TIMEOUT` 解析 + 超时提示 |
| `internal/procgroup_*.go` | 进程组 SIGTERM/SIGKILL（跨平台拆分） |
| `track/db.go` | SQLite 存储 + raw_output 列 migration |
| `filter/all/all.go` | blank import 聚合过滤器包 |

## 代码规范

- Go 代码注释、日志、错误消息使用中文（项目主语言）
- 文件末尾统一 `\n` 行结尾
- 不引新依赖（TOML 解析器固定 `github.com/BurntSushi/toml`）
- 测试：`go test ./...`；超时相关 `go test -run Timeout ./internal/ -timeout 60s`
- 加载失败只 warning 不 panic（企业部署稳定性硬要求）

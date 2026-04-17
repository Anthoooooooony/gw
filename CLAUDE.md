# CLAUDE.md

本文档记录 gw 项目的开发约定和环境变量配置，供 Claude Code 与协作者参考。

## 环境变量

### `GW_CMD_TIMEOUT` — 命令执行超时兜底

控制 `gw exec` 执行外部命令时的最长耗时，避免恶意命令或网络挂死导致 Claude Code 的 PreToolUse hook 无限阻塞。

| 值 | 语义 |
|----|------|
| 未设置 / 空 | 使用默认值 `10m` |
| `10m` / `30s` / `500ms` / `2h` 等 | 使用 `time.ParseDuration` 可解析的任意 duration |
| `0` / `off` / `none` / `disable` / `disabled` | 禁用超时（长驻命令场景） |
| 无法解析的值 | 写 warning 到 stderr，fallback 到默认 `10m` |

**两阶段终止**：
1. 到期后对整个进程组（`Setpgid` + `kill(-pgid, sig)`）发送 `SIGTERM`
2. 5 秒宽限期后若进程仍存活，发送 `SIGKILL`

**退出码约定**：超时场景统一返回 `124`（GNU `timeout(1)` 惯例），stderr 末尾追加 `gw: command timed out after <dur> (SIGTERM[, SIGKILL])`。

**批量 vs 流式**：
- 批量路径（`internal.RunCommand`）：超时后 `CommandResult.ExitCode = 124`，stderr 追加提示，不返回 Go error，让 `cmd/exec.go` 走正常的 `ApplyOnError` 路径。
- 流式路径（`internal.RunCommandStreamingFull`）：超时后返回 `exitCode = 124`（非 `-1`），调用方 `proc.Flush(124)` 能拿到非零 exit 从而输出错误上下文。stderr writer 收到超时提示。

**平台兼容**：
- 进程组相关代码在 `internal/procgroup_unix.go`，`//go:build unix` 覆盖 macOS / Linux / *BSD
- 非 unix 平台（如 Windows）在 `internal/procgroup_other.go` 提供仅杀主进程的降级实现

**使用示例**：

```bash
# 默认 10 分钟
gw exec mvn test

# CI 场景缩短到 5 分钟
GW_CMD_TIMEOUT=5m gw exec mvn test

# 调试时禁用超时
GW_CMD_TIMEOUT=off gw exec npm run dev

# 单元测试使用短超时（保持测试运行时间）
GW_CMD_TIMEOUT=300ms go test ./internal/...
```

## 执行路径关键不变式

- `RunCommand` 和 `RunCommandStreamingFull` 的函数签名**稳定**，超时只通过环境变量控制，调用方不需要改动
- 流式路径超时后必须保证 `cmd/exec.go` 能调用 `proc.Flush(exitCode)`，即 `RunCommandStreamingFull` 不泄漏 goroutine、不死锁
- 信号终止（非超时）继续保持 `exitCode = -1` 语义，与超时的 `124` 区分开

## 测试

```bash
# 全量测试
go test -count=1 ./...

# 超时相关（较慢，含宽限期验证）
go test -v -count=1 -run Timeout ./internal/ -timeout 60s
```

# CLAUDE.md

该文件为 Claude Code（claude.ai/code）在本仓库工作时提供指引。

## 项目概览

gw 是一个 CLI 代理，拦截 shell 命令并过滤输出，减少 LLM token 消耗。详见 `README.md`。

## TOML 规则三级加载（重要）

从 v0.3 起，TOML 声明式规则走**三级加载**，由 `filter/toml/loader.go::LoadAllRules` 统一合并。
按加载顺序从低到高，**高层同 ID 覆盖低层**：

1. **builtin**：`go:embed` 烘进二进制的 `filter/toml/rules/*.toml`。
2. **user**：`os.UserConfigDir()/gw/rules/*.toml`
   - Linux：`$XDG_CONFIG_HOME/gw/rules/`（默认 `~/.config/gw/rules/`）
   - macOS：`~/Library/Application Support/gw/rules/`
   - Windows：`%AppData%\gw\rules\`
3. **project**：从当前工作目录向上查找 `.gw/rules/*.toml`，遇到 `.git` 目录或文件系统根时停止。

规则唯一 ID 用 `section.name`（例如 `docker.ps`）。`disabled = true` 可让高层剔除同 ID 的低层规则。
解析错误只打 warning 到 stderr，不中断加载（企业环境鲁棒性要求）。

## `gw filters list`

查看全部已注册的过滤器及其来源：

```
NAME              TYPE  SOURCE                                                 MATCH
git/status        go    builtin                                                git status
docker.ps         toml  user:///home/u/.config/gw/rules/docker-prod.toml       docker ps
myapp.logs        toml  project:///workspace/.gw/rules/custom.toml             myapp logs
```

`TYPE` 为 `go`（硬编码）或 `toml`（声明式）；`SOURCE` 为 `builtin | user://<path> | project://<path>`。

## 关键文件

| 路径 | 职责 |
|------|------|
| `filter/toml/loader.go` | 三级加载器、来源追踪、disabled 支持 |
| `filter/toml/engine.go` | TOML 过滤引擎（7 阶段管道），`LoadEngine` 调用 loader |
| `filter/registry.go` | 全局注册表 + `List()`（for filters list） |
| `cmd/filters.go` | `gw filters list` 命令 |
| `filter/all/all.go` | blank import 聚合过滤器包 |

## 代码规范

- Go 代码注释、日志、错误消息使用中文（项目主语言）
- 文件末尾统一 `\n` 行结尾
- 不引新依赖（TOML 解析器复用 `github.com/BurntSushi/toml`）
- 测试：`go test ./...`（当前 180+ 个测试）
- 加载失败要 warning 不 panic（企业部署稳定性硬要求）

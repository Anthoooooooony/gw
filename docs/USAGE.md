# 使用指南

本文档面向 gw 的日常使用者，覆盖自动介入机制、命令绕过、自定义规则、历史查询、数据隐私等主题。架构与扩展开发见 [`DEVELOPING.md`](./DEVELOPING.md)。

## 自动介入机制

执行 `gw init` 后，Claude Code 在每次调用 Bash 工具前先咨询 gw，由 gw 判断当前命令是否存在匹配的过滤器。命中则改写为 `gw exec <原命令>`，否则静默透传。

以下情形一律不改写，命令原样执行：

- 命令含管道 `|`、重定向 `>` / `>>` / `<`、链式操作符 `&&` / `||` / `;`。这些场景下输出需要传给其他程序消费，压缩会破坏下游输入。
- 找不到匹配的过滤器。

## 支持的命令矩阵

| 生态 | 命令示例 | 典型压缩率 |
|------|---------|------------|
| Git | `git status` / `git log` | 30–45% |
| Java | `mvn compile/test/package` / `gradle build/test` / `java -jar *.jar` | 62–95% |
| Python | `pytest` / `python -m pytest` / `pip install` / `python -m venv` | 80–99% |
| Node | `npm install` / `yarn install` / `pnpm install` + test/build | ~70% |
| Rust | `cargo build/test/check/clippy` | ~70% |
| Docker | `docker ps` / `docker images` / `docker logs` | 50–80% |
| Kubernetes | `kubectl get/logs/describe` | 60–80% |

实际压缩率取决于命令输出噪音量。运行 `gw filters list` 可查看所有已注册过滤器及来源。

## 命令参考

| 命令 | 用途 |
|------|------|
| `gw exec <cmd> [args...]` | 执行命令并过滤输出 |
| `gw exec --dump-raw <path> <cmd>` | 执行命令并把原始输出写入指定文件（批量 / 流式皆支持） |
| `gw claude [args...]` | 透明包装 claude CLI：启动本地 API 代理、注入 `ANTHROPIC_BASE_URL`，对同签名 tool_use 的历史 tool_result 做去重 |
| `gw rewrite` | PreToolUse hook 入口，从 stdin 读 Claude Code hook JSON（内部使用） |
| `gw init` | 安装 Claude Code PreToolUse Hook |
| `gw init --dry-run` | 打印将要写入的变更但不落盘 |
| `gw uninstall` | 移除 Hook |
| `gw uninstall --dry-run` | 打印将要移除的变更但不落盘 |
| `gw summary` (alias: `gw gain`) | 默认启动本地 web dashboard；运行时按 `GW_DB_MAX_BYTES` 裁剪 DB |
| `gw summary --text` | 纯文本摘要（脚本 / CI / SSH 首选） |
| `gw summary --port N --no-browser` | 自定义端口 + 仅打印 URL（port forward 场景） |
| `gw -v exec <cmd>` | 执行并在 stderr 打印 token 节省详情 |
| `gw inspect [id]` | 查询历史执行记录；不带 id 列出最近 20 条，带 id 查看详情 |
| `gw inspect [id] --raw` | 打印原始未压缩输出（原文始终落盘） |
| `gw filters list` | 列出已注册过滤器及来源（builtin / user / project） |
| `gw --version` / `gw version` | 打印版本（ldflags 注入，runtime/debug 回退） |

## 查看效果

```bash
gw summary              # 启动本地 web dashboard 并打开浏览器（SSE 每 5s 刷新）
gw summary --text       # 纯文本摘要
gw summary --no-browser # 启 server 但不开浏览器，只打印 URL
gw summary --port 8080  # 指定端口（默认 0，系统随机）
gw -v exec mvn test     # 单次执行：显示 input_tokens / output_tokens / saved / elapsed
gw inspect              # 最近 20 条执行记录
gw inspect 42 --raw     # 查看 ID 42 的原始输出
```

### dashboard 降级规则

| 条件 | 行为 |
|------|------|
| `--text` 显式指定 | 纯文本摘要，不启 server |
| stdout 非 TTY（脚本 / 管道 / CI） | 纯文本摘要 |
| `NO_BROWSER=1` | 启 server 但不打开浏览器，只打印 URL |
| Linux 下 `$DISPLAY` 与 `$WAYLAND_DISPLAY` 均未设（SSH 会话） | 启 server 只打印 URL |
| 浏览器进程启动失败 | 打印 URL 后继续保留 server，不中断 |

## 临时绕过 gw

针对特定命令临时关闭压缩，有以下方式：

- **加管道**：`mvn test | cat` —— gw 检测到管道后放弃改写。
- **走全路径**：`$(which mvn) test` —— lexer 按命令名匹配，全路径会绕过注册表。
- **保留原始日志**：`gw exec --dump-raw /tmp/raw.log mvn test` —— 压缩仍执行，但原文同时落盘到指定路径。
- **完全停用**：`gw uninstall` 移除 hook，之后可随时 `gw init` 重启。

## 自定义 TOML 规则

若 gw 默认不覆盖某条命令，可在用户规则目录放置 TOML 文件，无需重新编译：

| 位置 | 路径 |
|------|------|
| macOS 用户目录 | `~/Library/Application Support/gw/rules/*.toml` |
| Linux 用户目录 | `~/.config/gw/rules/*.toml` |
| 项目私有（优先级最高） | 仓库内 `.gw/rules/*.toml` |

示例，让 `terraform plan` 去 ANSI 颜色并截取前 100 行：

```toml
# ~/.config/gw/rules/terraform.toml
[terraform.plan]
match = "terraform plan"
strip_ansi = true
max_lines = 100
```

成功和失败需要不同截断长度时，用 `[section.name.on_error]` 子表为失败场景配独立参数。例如 `cargo build` 成功只保留后 30 行摘要，失败保留后 200 行以便看错误上下文：

```toml
[cargo.build]
match = "cargo build"
strip_ansi = true
tail_lines = 30

  [cargo.build.on_error]
  strip_ansi = true
  tail_lines = 200
```

保存后立即生效。TOML DSL 仅支持无损变换，字段集：`strip_ansi` / `head_lines` / `tail_lines` / `max_lines` / `on_empty`；`on_error` 子表字段集与主规则一致，禁止嵌套。需要按语义压缩（例如"pytest 只留 failures 详情"），需编写 Go filter，详见 [`DEVELOPING.md` 的扩展过滤器章节](./DEVELOPING.md#扩展过滤器)。

## 在 Claude Code 之外使用

gw 本质是独立 CLI 工具，不绑定 Claude Code。任何 shell 场景皆可直接调用：

```bash
gw exec pytest -x                   # CI 脚本中压缩测试输出
gw exec mvn -pl service-a compile   # 本地 shell 手动调用
```

## 压缩异常排查

```bash
gw exec --dump-raw /tmp/out.txt <cmd>   # 压缩的同时备份原文
gw -v exec <cmd>                        # 详细 stderr：命中的过滤器、节省量
gw inspect <id> --raw                   # 从 DB 回溯历史记录原文
```

遇到误压缩（被裁掉的信息恰为关键信息）请提 Issue，附 `gw inspect <id> --raw` 的对比输出。

## 数据与隐私

gw 将**统计摘要**（命令串、token 数、退出码、耗时、命中的过滤器名）与**原始输出**一并存入 `~/.gw/tracking.db`（SQLite）。数据库体积由 `gw summary` 按阈值自动裁剪（默认 100 MiB，可用 `GW_DB_MAX_BYTES` 覆盖），超限时按时间删最旧记录并 `VACUUM`。通过 `GW_DB_PATH` 可覆盖 DB 路径。`gw summary` 与 `gw inspect` 读取的均为此本地文件，不上传任何外部系统。

## 卸载

```bash
gw uninstall        # 移除 Claude Code hook
rm -rf ~/.gw        # 连同 tracking DB 一起删除（可选）
```

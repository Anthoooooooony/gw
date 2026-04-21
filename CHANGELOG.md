# Changelog

本文件记录 gw 所有 notable 变更，遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式 + [Semantic Versioning](https://semver.org/)。

## [Unreleased]

### Added
- feat(pytest): 专属 Go filter，按 summary + FAILURES 锚点做语义压缩，压缩率 99%（成功）/ 82%（失败），**语义无损**（FAILURES 区块原样保留）。
  parse 锚点缺失（输出被 `--tb=no` / `head` 截断等）时回退原文透传。

### Changed
- **Breaking（TOML DSL v2）**：TOML 规则只保留语义无关的无损字段（`strip_ansi` / `head_lines` / `tail_lines` / `max_lines` / `on_empty`）。基于正则的 `strip_lines` / `keep_lines` / `on_error` 因误删风险移除。
- 内置 node/python/rust TOML 规则精简为 strip_ansi + 长度兜底；需要语义压缩的命令（pytest 已接管，vitest / cargo test / npm test 待补）走专属 Go filter。
- `filter/all` 注册顺序：专属 filter → toml（Registry 第一匹配胜出），保证 pytest 等优先命中 Go 实现。

### Fixed

### Removed
- `filter/toml` 的 `strip_lines` / `keep_lines` / `on_error` 字段与对应 loader warning 兼容代码（不考虑 v1 配置前向兼容）。
- `filter/toml/rules/python.toml` 的 `[pytest.*]` 规则（由专属 filter 接管）。

[Unreleased]: https://github.com/Anthoooooooony/gw/compare/v0.1.1...HEAD
[v0.1.0]: https://github.com/Anthoooooooony/gw/releases/tag/v0.1.0

## [v0.1.1] - 2026-04-17

### Fixed
- fix: 统一错误前缀到 CLAUDE.md 约定（删 [gw] 方括号风格） (#12)
- fix(bump): 修复 pre-release bug + CHANGELOG 链接自动维护 (#9)
## [v0.1.0] - 2026-04-17

### Added
- feat: release 管道基础设施（CHANGELOG/bump.sh/GoReleaser/release.yml） (#4)
- feat(filter): 新增 Node/Python/Rust 生态 TOML 规则 (#2)
- feat(filter): Gradle StreamFilter 流式过滤器 (#1)
- feat: 重新启用 SpringBootFilter 并实现 StreamFilter 接口，支持流式过滤长驻进程输出
- feat: exec 命令集成流式过滤路径，优先匹配 StreamFilter 走逐行流式处理
- feat: 用状态机重写 MavenFilter，真实 NetEDS 输出压缩率 95%
- feat: 新增 Maven 状态机内核 — 状态定义、行分类器、状态转移逻辑
- feat: 添加 TOML 声明式过滤引擎，支持 docker/kubectl 内置规则

### Changed
- refactor: 抽取 startTimeoutKiller 统一 runner 与 stream 的两阶段终止逻辑

### Fixed
- fix(bump): awk -v 不支持多行值 —— BSD awk 兼容修复 (#5)
- fix(test): exec_test 接受 HEAD detached 状态（PR 事件 CI 场景） (#3)
- fix(ci): exec_test 自动探测 go.mod 定位模块根，取代 /private/tmp/gw fallback
- fix(P2): exec_test.go 改用 GW_SOURCE_ROOT 替代硬编码 /private/tmp/gw
- fix(P2): 兼容 v0.x 无 _gw_managed 标记的 gw hook，就地迁移补标
- fix(P1): writeSettingsAtomic 保留原 settings.json mode，首次写入用 0600
- fix(P1): DB 路径支持 GW_DB_PATH + HOME 只读降级到 TempDir（warn-once）
- fix(P1): --dump-raw 支持任意位置（命令名之前），避免吞子命令同名 flag
- fix(P1): 流式路径信号终止返回 128+signal 保留真实信号值
- fix(P1): findProjectRulesDir 识别 .git 为文件的 worktree 场景
- fix(P0): Windows 降级 killProcessGroup 忽略 sig，避免误导性 SIGTERM 日志
[v0.1.1]: https://github.com/Anthoooooooony/gw/releases/tag/v0.1.1

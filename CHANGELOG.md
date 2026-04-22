# Changelog

本文件记录 gw 所有 notable 变更，遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式 + [Semantic Versioning](https://semver.org/)。

## [Unreleased]

### Added

### Changed

### Fixed

### Removed

[Unreleased]: https://github.com/Anthoooooooony/gw/compare/v0.3.0...HEAD
[v0.1.0]: https://github.com/Anthoooooooony/gw/releases/tag/v0.1.0

## [v0.3.0] - 2026-04-22

### Added

### Changed
- refactor(apiproxy/stream/gain): 错误 / warn / info / HTTP body 文本中文化；`cmd/gain.go` 的 `[今日]` 方括号风格改为 `今日 —`，与 CLAUDE.md "禁止方括号前缀" 规范对齐 (#86)
- refactor(filter): 新增 `filter.StripANSI` 公共工具替代 `filter/java/gradle.go` 与 `filter/toml/engine.go` 的两份 ANSI 正则（字面量大小写原本不一致）；`GradleStreamProcessor` 去导出为 `gradleStreamProcessor`（无跨包使用）；pytest `Subname("pytest")` 返回空串避免展示 `pytest/pytest` 冗余；dcp.Logger 注释纠正"避免包循环"误导 (#89)

### Fixed
- fix(install): go.mod module path 从失效的 `github.com/gw-cli/gw` 改为 `github.com/Anthoooooooony/gw`（`go install` 原本 404）；README 安装脚本改为运行时查 `releases/latest` API 取 tag，代替硬编码 v0.1.0 tarball；CI / release workflow 改用 `go-version-file: go.mod` 消除 README `1.22+` 与 CI 硬编码 `1.25` 的漂移（govulncheck job 例外，保留 1.25 因工具自身要求） (#84)
- fix(bump): `[Unreleased]` 有手工内容时整体迁移到新版本节，`trap` 用 `${var:-}` 默认空展开避免 unset variable，bump.sh 现覆盖 migration / auto-gen 两条路径（43 个 bash 单测全覆盖） (#83)

### Removed

## [v0.2.0] - 2026-04-21

### Added
- feat(claude): DCP 观测——退出摘要 + verbose 逐请求日志 (#77) (#81)
- feat(claude): 代理 hardening——body 上限 + header 超时 + shutdown grace (#77) (#80)
- feat(claude): DCP 风格 tool_result 去重 (#77) (#79)
- feat(claude): gw claude 子命令 + 本地 API 代理最小版（v0 纯透传） (#77) (#78)
- feat(pytest): 专属 Go filter，语义压缩 99%/82% (#43) (#73)
- feat(toml): on_error 子规则扩展 + 失败场景压缩 (#43) (#70)

### Changed
- refactor(toml): DSL v2 仅保留无损字段 (#43) (#72)

### Fixed
- fix(shell): TokenizeSegment 替代 strings.Fields 引号感知分词 (#66) (#67)
- fix(init): hook 加 timeout:10 防 rewrite 挂死卡 Claude Code (#64) (#65)
- fix(bump): trap 清理 tmp + Unreleased 节预检 (#60) (#61)
- fix(internal): killer SIGKILL 前 peek procDone 缓解 PID 复用 race (#58) (#59)
- fix(filter): 消除 TomlFilter matchedRule 共享状态 (#56) (#57)
- fix(init): 重写 hook 整合对齐 Claude Code 真实 schema (#53) (#54)
- fix(bump): 剥 NUL 流前导 \n 避免第 2+ 条 commit subject 被吞 (#46) (#47)
- fix(bump): 支持 BREAKING CHANGE footer 完整识别 (#20) (#34)
- fix(bump): 加 tag 幂等性检查避免重复 bump 留脏 tag (#15) (#25)
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
- feat: 用状态机重写 MavenFilter，真实大型 Java 产线输出压缩率 95%
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
[v0.2.0]: https://github.com/Anthoooooooony/gw/releases/tag/v0.2.0

[v0.3.0]: https://github.com/Anthoooooooony/gw/releases/tag/v0.3.0

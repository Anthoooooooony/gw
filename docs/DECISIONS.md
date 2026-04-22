# 决策记录（Decisions）

本文件记录 gw 项目中**影响架构 / 公开接口 / 产品边界 / 环境依赖**的决策。

触发条件与格式规范见 `CLAUDE.md` 的「决策留痕（ADR-lite）」节。
新决策**追加到文件顶部**（最新在上）；已落地决策不回改，仅通过新条目「Supersedes YYYY-MM-DD xxx」形式覆盖。

条目模板：

```
## YYYY-MM-DD — 标题

**上下文**：为什么需要做这个决策（业务/技术背景、约束）
**决策**：选择了什么方案
**替代方案**：考虑过的其他选项 + 为何否决
**影响**：代码 / 接口 / 文档 / 运行时行为的连带改动
```

---

## 2026-04-22 — 不支持 ANTHROPIC_BEDROCK_* / ANTHROPIC_VERTEX_* 环境变量

**上下文**：`gw claude` 子命令透明代理 Claude Code 请求以实现 DCP 上下文压缩。Claude Code 原生支持通过环境变量切换到 AWS Bedrock / GCP Vertex 后端。用户需要明确 gw 在多厂商场景下的边界。

**决策**：gw 只接入 Anthropic 原生协议，代理启动后仅通过 `ANTHROPIC_BASE_URL` 指向本地 apiproxy。检测到 Bedrock/Vertex 环境变量时**不**做兼容分支，用户需要走这两家时请直接使用原版 Claude Code。

**替代方案**：
- 检测 Bedrock/Vertex 变量后回退到"透明透传、不压缩"——否决：给用户造成"gw 启动了但没生效"的认知裂缝，不如一开始明确不支持
- 实现多厂商 body transformer——否决：DCP 去重依赖 Anthropic messages schema（tool_use/tool_result 结构），Bedrock/Vertex 的 wrapper 差异会让压缩逻辑持续打补丁，偏离项目主线

**影响**：
- 移除 `internal/apiproxy/env.go::BedrockOrVertexEnabled()` 及对应测试
- `cmd/claude.go` 删除 Bedrock/Vertex 检测分支，步骤注释重排
- README「调试 gw claude 代理」节显式说明不支持
- 对应 PR #91

## 2026-04-22 — 过滤器优先级用显式 Fallback 接口，不用 import 顺序

**上下文**：`filter/all/all.go` 通过 blank import 顺序（专属 Go filter 先 import、TOML 兜底后 import）决定 `Registry.Find` 的匹配优先级。这种隐式耦合容易在新人或自动化工具调整 import 排序时默默失效。

**决策**：新增 `filter.Fallback` 接口（`IsFallback() bool`）。`Registry.Find` 两遍扫描：先找非 fallback 的命中，再找 fallback。`TomlFilter` 实现 `IsFallback() = true`，Go 专属过滤器默认不实现（即非 fallback）。

**替代方案**：
- 保留 import 顺序约定 + 加注释——否决：gofmt/goimports 不会尊重这种隐式约束
- 在注册时传入 `priority int`——否决：离散优先级会诱导未来用户造出 5 级优先级的复杂度；当前场景只需要"普通/兜底"两档

**影响**：
- `filter/filter.go` 新增 `Fallback` 接口
- `filter/registry.go` 新增 `findIn()` 两遍扫描
- `filter/all/all.go` 注释说明 import 顺序不再承担语义
- 对应 PR #92

## 2026-04-22 — 流式累积结构必须有容量上限（boundedDedupSet）

**上下文**：`filter/java/{gradle,maven}.go` 的流式过滤器用 map 去重已见行。长驻构建（例如 Gradle daemon 几小时不退出）会让 map 持续增长，最终 OOM。

**决策**：引入 `filter/java/dedupset.go::boundedDedupSet`，cap 默认 10000。达到 cap 后新元素不再入集，效果是"不再去重"但不吃内存。优雅降级优先于严格去重。

**替代方案**：
- LRU 淘汰——否决：构建日志去重的 recency 意义不大（同一条错误每次出现都应该被吞），LRU 的复杂度换来的价值有限
- 不设上限、信任构建时长合理——否决：生产上确实有跑到几小时的 CI job

**影响**：
- 新增 `filter/java/dedupset.go`
- `filter/java/gradle.go` / `maven.go` 的 seen map 替换为 `*boundedDedupSet`
- 对应 PR #93

## 2026-04-22 — TOML DSL v2：仅无损变换

**上下文**：早期 TOML DSL 支持 `strip_lines` / `keep_lines` / `on_error` 等基于正则的行级裁剪。实际使用中发现误删用户恰好需要的行会长期累积信任危机——用户不敢依赖过滤器输出。

**决策**：TOML DSL v2 只保留语义无关的安全变换：`strip_ansi` / `head_lines` / `tail_lines` / `max_lines` / `on_empty`。需要语义压缩（按 exit_code 分场景、pytest 只留 failures 等）必须写 Go filter，走 parse + fallback 原文模式。

**替代方案**：
- 保留正则裁剪 + 加 warning——否决：warning 不会被读，误删仍然发生
- 让 TOML 支持结构化表达式——否决：会演化成半吊子 DSL，不如让语义场景直接写 Go

**影响**：
- 弃用字段在 loader 打一次 warning 并丢弃值，规则无损部分仍生效
- `CLAUDE.md` 「TOML 规则 DSL」节固化该约束

## 历史约定（日期未溯源，供后续覆盖式更新用）

- **DB schema 演进只走 `ALTER TABLE ADD COLUMN`**：`~/.gw/tracking.db` 是用户生产数据，`DROP` 类操作会丢历史记录。见 `track/db.go`。
- **执行路径接口稳定**：`RunCommand` / `RunCommandStreamingFull` 的函数签名不改，新配置通过环境变量或 flag 承载。
- **stderr 输出格式**：`gw <subcmd>: <msg>` / `gw: warning: <msg>` / `gw: info: <msg>` 三段式，禁止方括号风格。

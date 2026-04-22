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

## 2026-04-22 — 放弃 release-please，改自写单 workflow 一体化发版 (Supersedes 同日 "切换到 release-please")

**上下文**：切换 release-please 后试跑发现硬约束：`GITHUB_TOKEN` 触发的 tag push **不会**触发其他 workflow（GitHub 防递归设计），导致 release-please 打 tag 后 `release.yml` 不触发，binary assets 必须手工删 tag 重推才能上传（v0.3.2 就是这样补救的）。长期解法只有两条：引入 PAT/GitHub App 打破跨 workflow 限制；或彻底抛弃跨 workflow 架构。

**决策**：抛弃 release-please，在 `.github/workflows/release.yml` 里单一 workflow 串联 `decide → build → release` 三 job：
1. `decide` 用 `scripts/release-helpers.sh` 的 shell 函数（`classify_commit` / `kind_from_classifications` / `bump_version` / `build_release_notes`）扫 `prev_tag..HEAD` commit subject，决定是否发版 + 版本号 + notes
2. `build` matrix 跑 CGO 编译产 `*.tar.gz`
3. `release` 打 tag + `gh release create` 上传 assets

整条链在一个 workflow run 内完成，`GITHUB_TOKEN` 的 `contents:write` 足够。

**替代方案**：
- PAT/GitHub App + release-please：社区 best practice，但引入 token 维护面和权限风险；单人项目性价比一般
- 保留 release-please + 每次手工重推 tag 补救：维护者体验差，容易忘
- 手写脚本但仍跑在本地（类似原 bump.sh）：发版流程不透明、不 CI 化，用户主动移除过

**影响**：
- 删：`.github/workflows/release-please.yml`、`release-please-config.json`、`.release-please-manifest.json`
- 改写：`.github/workflows/release.yml`（从"tag 触发构建"变"master push 决定是否发版 + 构建 + 上传"）
- 新：`scripts/release-helpers.sh`（~120 行，源自删去的 `bump.sh` 核心函数，去掉 CHANGELOG migration 与 edit/push 步骤）
- 文档：`CONTRIBUTING.md` / `CLAUDE.md` Release 节改写为"单 workflow"
- 运行时：发版从 "合 PR → 手工合 release PR → 手工补 tag" 变 "合 PR → workflow 全自动"；发版前丧失 release PR preview 窗口（PR title 本身是 review 过的 CC，影响可接受）
- 同日"切换到 release-please"决策被本次反转

## 2026-04-22 — 切换 release 工具链到 release-please 并移除 CHANGELOG 文件

**上下文**：原 release 流程由 `scripts/bump.sh`（359 行 bash + 413 行测试）承担版本 bump / CHANGELOG migration / tag 创建 / push 六步。Keep-a-Changelog 双路径（migration + auto-gen fallback）在实测中几乎不用 migration，发布频率有望上升到每周，bash 维护成本高于收益。与 merge-commit 策略的讨论同步触发了一次合并策略复核：release-please 的 best practice 是 squash-merge + PR title 遵循 Conventional Commits。

**决策**：
1. 引入 [release-please](https://github.com/googleapis/release-please) 作为 release 驱动（GitHub Action + JSON 配置）；PR title 成为唯一的 CC 约束点，squash 后进 master 的 commit subject 即 PR title
2. **彻底移除 `CHANGELOG.md`**，配置 `skip-changelog: true`，变更说明只在 GitHub Releases 页面
3. 删除 `scripts/bump.sh` / `scripts/bump_test.sh` / `RELEASING.md`；CI 的 shellcheck job 一并移除（scripts/ 目录已空）
4. 合并策略保持 squash-merge（与之前瞬时的"切 merge commit"想法相反——release-please 机制依赖 squash）
5. `.github/workflows/release.yml` 改造：不再从 CHANGELOG 提取 release notes，不再打包 CHANGELOG 进 tar.gz；tag push 触发构建 → `gh release upload` 把 assets 追加到 release-please 已创建的 Release，不碰 body

**替代方案**：
- 保留 bump.sh：维护成本实际可接受，但 release-please 的"合 release PR 即发版"比"本地跑脚本"更安全（push 到 CI 可复核）、更少本地环境依赖
- 换 git-cliff + make release：只能替代 CHANGELOG 生成，幂等 tag / 发布触发仍需另写脚本——收益不足
- 换 semantic-release（Node 生态）：对 Go 项目引入 Node 依赖不划算
- 保留 CHANGELOG 但用 release-please 维护：增加一份"与 Release notes 重复"的文件，删更彻底

**影响**：
- 删：`scripts/bump.sh`、`scripts/bump_test.sh`、`CHANGELOG.md`、`RELEASING.md`
- 新：`.github/workflows/release-please.yml`、`release-please-config.json`、`.release-please-manifest.json`
- 改：`.github/workflows/release.yml`（去 CHANGELOG 依赖，assets 追加到已有 Release）、`.github/workflows/ci.yml`（删 bash 单测步骤 + shellcheck job）、`Makefile`（删 bump-test target）
- 文档同步：`CONTRIBUTING.md` / `CLAUDE.md` / `README.md` / `.github/pull_request_template.md`（CI gate 从 8 层改为 7 层）
- 运行时：release 频率可拉高；维护者不再需要本地跑 `bump.sh`
- 对应 PR：Phase 1 #98（引入），Phase 3（本次清理）

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

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

## 2026-04-22 — Filter fallback invariant：锚点缺失绝不 silent data loss

**上下文**：v0.11.1 前 `gw exec git log --oneline` 被压缩到 0 字节——`git/log` 的 `parseCommits` 依赖 `commit <hash>` + `Author:` 前缀；`--oneline` / `--pretty=format:` / `--graph` / 用户 `format.pretty` 配置均无此前缀，解析返回 0 条 commit，Apply 直接 join 成空串。类似风险面广：`CARGO_TERM_COLOR=always` 加色码破坏正则锚点、pytest 插件改 `=+ FAILURES =+` 结构、vitest 升级改 ` Test Files` 行前缀等。

**决策**：所有 filter 的 Apply / ApplyOnError **必须保证锚点缺失时的行为是透传或 nil，不得返回空串或残缺内容**：

1. Apply 锚点缺失 → `return filter.FilterOutput{Content: input.Stdout, Original: input.Stdout}`（透传原文）
2. ApplyOnError 锚点缺失 → `return nil`（上层按原文走，语义同透传）
3. 任何 `detectX / findX` 类辅助函数返回空值/索引 -1 时，外层必须走 fallback 分支

**替代方案**：
- 返回空串 + 日志 warn：用户看不到 warn，等于静默丢数据；否决
- 设计"部分压缩"兜底：不稳定，不同命令需要手工调参；过度设计

**影响**：
- `filter/git/log.go` 增加 0-commit 兜底（原本代码路径在 `--oneline` 时会 join 空切片产出 ""）
- 全部 Go filter 入口统一 `filter.StripANSI(input.Stdout)` 做防御性去色（见独立条目）
- CLAUDE.md 的 "filter 契约" 节显式写入该 invariant
- 对应 PR：#117 (git log 兜底)、#118 (ANSI 防御)

## 2026-04-22 — Filter 设计约束：gw 按 "stdout + stderr" 拼接，切片起点须在 stdout

**上下文**：vitest 失败输出把测试进度和 `Test Files N failed (1)` / `Tests N failed | M passed` 汇总写 stdout，`⎯ Failed Tests N ⎯` 详情分隔符及后续 AssertionError/code frame 写 stderr。gw 捕获子进程时按**先 stdout 后 stderr** 拼接成 `input.Stdout + input.Stderr`，与 TTY 终端按时间线交错的显示顺序不同。v0.10.0 的 vitest 嗅探器用 stderr 里的 `Failed Tests N` 作切片起点，结果丢掉了 stdout 尾部的 `Test Files` / `Tests` 汇总行——实机测试才暴露。

**决策**：任何多源嗅探器必须优先使用 **stdout 里最早出现**的稳定锚点作为切片起点，让切片自然覆盖 stdout 尾部和整个 stderr。具体落地：

- vitest：首选 ` ❯ \S+ \(\d+ tests? \| \d+ failed\)`（stdout 文件级失败摘要），回退才用 `Failed Tests N`
- 测试 fixture 必须通过 `cmd >stdout.txt 2>stderr.txt && cat stdout.txt stderr.txt > fixture.txt` 生成，**不**能用 `cmd >file 2>&1`（后者是 terminal interleave 顺序，与 gw 实际捕获不符）

**替代方案**：
- 让 gw 按时间戳交错合并 stdout/stderr：需要 PTY 或子进程级时间戳，复杂度高；否决
- 在 filter 里只看 stdout：失败详情常在 stderr 会全丢；否决
- 改 gw 拼接顺序为 stderr + stdout：对既有过滤器破坏性强；否决

**影响**：
- `filter/npmtest/npmtest.go` vitest 嗅探器改锚点
- `filter/toml/testdata/vitest_failure.txt` 重新按 stdout 后 stderr 顺序生成
- 对应 PR：#116
- CLAUDE.md "filter 契约" 节记录此约束

## 2026-04-22 — TOML DSL v2 扩展 on_error 子表 (部分 Supersedes 同日 "TOML DSL v2：仅无损变换")

**上下文**：原 v2 决策明确拒绝 `on_error`，要求按 exit_code 分场景走 Go filter。但实测发现 `cargo build` / `npm test` 的失败场景与成功场景 **tail 数量级不同**（失败要留更多上下文），单一 `tail_lines` 盖不住两种场景。又因专属 Go filter 覆盖面需要多个 PR 迭代，短期内大量命令卡在"失败 0% 压缩"。

**决策**：TOML DSL v2 引入 `[section.name.on_error]` 子表，字段集与主规则**完全一致**（strip_ansi / head_lines / tail_lines / max_lines / on_empty）。**严守"仅无损变换"核心原则不变**——子表本身不引入 strip_lines / keep_lines 这类基于正则的裁剪；只是让成功/失败可以配不同的截断长度。

**替代方案**：
- 坚守原 v2 拒绝 on_error，等 Go filter 全覆盖：过渡期用户体验差；否决
- 引入 `on_error` 支持 keep_lines 正则过滤：违反无损原则；否决
- 把 on_error 收编到 Go filter 做"TOML 兜底 + Go 分支"：架构复杂；否决

**影响**：
- 新增 `OnErrorRule` struct + `rawOnErrorRule`；`ApplyOnError` 从硬编码 `return nil` 改为"有子表则按子表应用"
- 内置规则新增 `[cargo.build|check|clippy.on_error]` / `[npm|yarn|pnpm.test.on_error]` / `[docker.pull|compose-up.on_error]`（部分在 Go filter 合入后被 supersede）
- 对应 PR：#109；后续 Go filter 落地 PR (#110/#111/#113/#114) 逐步删除对应 TOML 节

## 2026-04-22 — 专属 Go filter 的包结构与命名约定

**上下文**：#74 落地 cargo/pip/npmtest 多个专属 filter 后，需要固化统一的包结构，避免后续每个 contributor 各写一套。pytest 作为首个落地（#73）已提供模板，但只有一个 filter 的情况没暴露多 filter 同包的命名矛盾。

**决策**：

1. **包命名**：按生态/工具名单复数小写，例如 `filter/cargo`、`filter/pip`、`filter/npmtest`。同生态下多子命令 filter 共用同包（cargo test/build/check/clippy 共居 `filter/cargo`）。
2. **文件命名**：`filter/{pkg}/{subcmd}.go` + `{subcmd}_test.go`。避免文件名以 `_test` 结尾被 Go 误认为纯测试文件（早期写 `cargo_test.go` 被识别为测试文件是教训）。
3. **Filter Name**：使用二级分层 `"pkg/subcmd"`（例如 `"cargo/test"`、`"pip/install"`、`"git/log"`），与 Maven/Gradle 的 `"java/maven"` / `"java/gradle"` 对齐。
4. **注册**：每个文件 `init()` 单独调 `filter.Register(&FooFilter{})`；`filter/all/all.go` 按字母序 blank import 各包，不承担优先级语义（fallback 由 `IsFallback()` 接口管）。
5. **Match 严格形态**：只认明确调用（`cargo test`），拒绝 wrapper（`cargo nextest run` / `uv pip install` / `npm run test:*`）；wrapper 的输出结构不保证与底层工具一致。
6. **回退路径**：Apply 锚点缺失 → 返回原文；ApplyOnError 锚点缺失 → 返回 nil（见独立 invariant 条目）。

**替代方案**：
- 单包 `filter/toolkit` 收拢所有：生态间相互 import 容易混；否决
- 按命令名展平 `filter/cargotest` / `filter/cargobuild`：同生态多个小包冗余；否决
- Filter Name 扁平 `"cargo-test"`：失去层级可观察性；否决

**影响**：
- 新增 `filter/cargo` / `filter/pip` / `filter/npmtest` 三个包
- `filter/pytest` / `filter/java` / `filter/git` 既有命名回顾确认符合约定
- 对应 PR：#110 (cargo/test)、#111 (cargo/build)、#113 (pip/install)、#114 (npm test)、#115 (vitest)

## 2026-04-22 — settings.json 写回保留 key 顺序（引入 iancoleman/orderedmap）

**上下文**：`gw init` / `gw uninstall` 用 `json.MarshalIndent(map[string]interface{})` 序列化 settings，Go 的 encoding/json 按 key 字母序输出。用户原 settings.json 通常按 Claude Code 的插入顺序组织（env 在前、permissions 居中等），经 gw 一次往返后整文件变字典序——纯语义无变化但：(a) 手工 diff 看不清 gw 到底改了什么；(b) git 纳管 dotfiles 的 commit history 被"首次跑 gw init"污染。

**决策**：引入 `github.com/iancoleman/orderedmap` v0.3.0（MIT、零 transitive dep、200 行纯 Go）替代 `map[string]interface{}`；`readSettings` 同时侦测首行缩进（2/4 空格或 tab），`marshalSettings` / `writeSettingsAtomic` 原样沿用。

**替代方案**：
- 手写 json.Decoder.Token() 保序解析：180-250 行，corner case 多（nested array、number 精度）；否决
- 接受字典序改写，在 README 告知用户：破坏 git history；否决
- 改走 TOML/YAML 作为 settings 格式：不归我们决定（Claude Code 约定）；否决

**影响**：
- `readSettings` 返回新增 `indent string`；`applyInitToSettings` / `applyUninstallToSettings` / `marshalSettings` / `writeSettingsAtomic` 全部改签名
- `cmd/testdata/settings_with_extras.json` fixture + `TestSettings_PreserveKeyOrderAndIndent` 做字节级 round-trip 断言
- 限制：字节相等的前提是原文件已是 `json.MarshalIndent` 规范格式（每数组元素单行）；用户手写 inline 数组 `["a","b"]` 仍会被展开——`encoding/json` 固有行为，不在本 PR 解决
- 对应 PR：#107

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

**上下文**：`gw claude` 子命令透明代理 Claude Code 请求以实现上下文去重。Claude Code 原生支持通过环境变量切换到 AWS Bedrock / GCP Vertex 后端。用户需要明确 gw 在多厂商场景下的边界。

**决策**：gw 只接入 Anthropic 原生协议，代理启动后仅通过 `ANTHROPIC_BASE_URL` 指向本地 apiproxy。检测到 Bedrock/Vertex 环境变量时**不**做兼容分支，用户需要走这两家时请直接使用原版 Claude Code。

**替代方案**：
- 检测 Bedrock/Vertex 变量后回退到"透明透传、不压缩"——否决：给用户造成"gw 启动了但没生效"的认知裂缝，不如一开始明确不支持
- 实现多厂商 body transformer——否决：去重依赖 Anthropic messages schema（tool_use/tool_result 结构），Bedrock/Vertex 的 wrapper 差异会让压缩逻辑持续打补丁，偏离项目主线

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

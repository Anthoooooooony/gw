---
name: feature-dev
description: 启动 gw 项目的新 feature 开发流程。当用户表达"新增 / 实现 / 加一个 / 支持 / 开发一个"等意图，且改动跨多文件或模块、可能需要 ADR 时使用。覆盖需求澄清、调度 code-explorer 摸底、按需调用 code-architect 出设计、TDD 实现、调用 code-reviewer 自检、PR 提交。不用于纯 bug 修复、文档改动、依赖升级或单文件小改。
---

# feature-dev — gw 项目新 feature 开发流程

固化 "复杂 feature" 的启动仪式：通过有序调度子代理保证研究-设计-实现-审核链条不跳步。本 skill **只编排流程**，不写实现细节——代码细节看 CLAUDE.md / DEVELOPING.md。

## 流程（四阶段，不跳步）

### 阶段 1：需求澄清与规模判断

动手前与用户确认 4 件事：

1. **目标**：这个 feature 要解决什么用户问题？（避免解决错误的问题）
2. **范围**：只涉及一个模块还是跨多模块？（影响要不要调度 architect）
3. **是否需要 ADR**：触发条件见 `CLAUDE.md` "决策留痕" 节。若需要，ADR 条目必须和代码同 PR 提交，不能后补
4. **是否走完整流程**：若用户要"快速加一下"且规模小（单文件 < 100 行），允许跳过阶段 2，直接进阶段 3 的实现；否则全流程走完

澄清结果向用户复述一次，确认无误再进入阶段 2。

### 阶段 2：调度 code-explorer 摸底

用 Agent 工具，`subagent_type: feature-dev:code-explorer`。prompt 要包含：

- 本次 feature 的目标（从阶段 1 复述）
- 要摸清的模块清单（具体到文件，如 "`filter/registry.go` 的注册/优先级逻辑"、"`cmd/exec.go` 的 pipeline 六阶段"）
- 期望产出：既有模式、调用链、测试覆盖情况、可能的扩展点、潜在的坑

等 code-explorer 返回后，向用户**汇报**：调研到了什么、哪些地方需要改、估算改动面。若用户要求调整范围，回阶段 1。

### 阶段 3：设计与实现

按规模分流：

- **跨模块 / 新架构**：调度 `feature-dev:code-architect`，产出文件清单 / 数据流 / 构建顺序。prompt 要包含阶段 2 的调研结果和阶段 1 的目标
- **单模块**：主 agent 自行出实现计划

然后：

1. **起分支**：`feature/<kebab-desc>`
2. **TDD**：先补测试——参考 `CLAUDE.md` "测试" 节的约定（`t.TempDir()` / `httptest.Server` defer LIFO / 流式过滤器三路径 Flush）
3. **实现**：对照 CLAUDE.md 的 "模块与接口"、"并发"、"内存与字符串" 等规范
4. **涉及过滤器**：跑 `go test ./filter/ -run TestScenarioCompression -args -update` 更新 baseline，**人工 review diff 再 commit**
5. **涉及 ADR**：同批写 `docs/DECISIONS.md` 新条目（格式见 CLAUDE.md "决策留痕" 节）
6. **本地验证**：`make test` 对齐 CI

### 阶段 4：code-reviewer 自检 + PR

提 PR 前调度 `feature-dev:code-reviewer`。prompt 要包含：

- 本次 feature 的目标
- 改动的文件清单
- 特别想 review 的点（业务逻辑核心 / 引入新依赖 / 改了 API 边界 / TOML DSL 扩展）

根据 reviewer 结果修复 **high confidence** 的问题；low confidence 的可忽略（避免过度修改）。

最后走 PR 流程：

```bash
gh issue create --title "<意图>"            # 若还没有 issue
gh pr create --title "feat: <scope>: <描述>" \
             --body "... Closes #<issue>"   # title 用 CC 前缀
gh pr checks <N> --watch                    # 盯 CI
gh pr merge <N> --squash --delete-branch    # 全绿后合并
git checkout master && git pull --ff-only
```

## 关键约束

- **不跳阶段 2**，除非阶段 1 已显式裁剪
- **ADR 与代码同 PR** 提交，不能后补
- **`feat:` 前缀触发 minor bump**，确认这不会打断用户节奏；若用户想先合多个 feature 再发版，用 `refactor:` 或在多个 PR 合完后再显式 push master
- **子代理不直接改文件**，只产报告，主 agent 根据报告操作

## 不在本 skill 范围

| 主题 | 去哪看 |
|------|--------|
| 新增过滤器 3 步最短路径 | `CLAUDE.md` "日常任务模板 → 新增过滤器" |
| 测试命令矩阵（make test / -args -update） | `CLAUDE.md` "日常任务模板 → 跑测试" |
| Release 触发规则（CC 前缀 → bump） | `CLAUDE.md` "日常任务模板 → Release" |
| 架构总览 / 六阶段 pipeline / Maven 状态机 | `docs/DEVELOPING.md` |
| 决策历史 | `docs/DECISIONS.md` |
| bug fix 工作流 | 常规 `fix/` 分支即可，不走本 skill |
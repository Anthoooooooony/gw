# 分支模型回退 → GitHub Flow

**Status**: Approved
**Date**: 2026-04-17
**Supersedes**: `2026-04-17-versioning-git-workflow-design.md` §2（分支拓扑）

## 动机

4 路治理审计独立发现：
- **Audit B**：PR #1-#7 全部 base=master，dev 分支 ahead master 3 commit 但内容完全同步，feature→dev 从未执行
- **Audit B**：hotfix cherry-pick 铁律一次未执行，均改为 merge master→dev
- **Audit C**：spec §4.4 声明 darwin_amd64 覆盖，实际 release.yml 不产出；两干模型同样是"纸面规范"
- **Audit A**：单维护者项目的集成缓冲（dev 分支）价值小于维护成本

## 新规则

见 spec §2（已改写）、CLAUDE.md 分支约定节（已改写）。

## 不变的内容

- SemVer 语义、`scripts/bump.sh` 流程、release.yml 构建逻辑、CI 门禁
- 所有 feature/hotfix 分支语义本来就是 master-based，没变

## 迁移动作

由 orchestrator（会话 driver）执行：
1. 本地 + 远端删 `dev` 分支
2. GitHub default branch 从 `dev` 改回 `master`
3. 清理本地 tracking ref

# 发布流程（RELEASING）

gw 用 `scripts/bump.sh` 承担 SemVer 版本号推进 + CHANGELOG 归档 + tag 创建。
本文档记录一次标准发布从准备到验证的全部步骤；如果手上是 hotfix 而非常规发布，查末尾的 Hotfix 条目。

## 前置条件

- 工作目录 clean（`git status` 无未提交改动）
- 当前在 `master`，且与 `origin/master` 同步
- 本次要发布的所有 PR 都已经 squash merge 进 master
- `CHANGELOG.md [Unreleased]` 段内容反映真实改动；空段不触发 release（bump.sh 的 migration 或 auto-gen 路径会从 commit 历史补，但 handwritten 内容优先级更高）

## 标准发布

### 1. 决定版本类型

- **patch**：bug fix、纯文档、CI、测试；对外行为不变 → `./scripts/bump.sh patch`
- **minor**：新功能、接口新增（向后兼容）→ `./scripts/bump.sh minor`
- **major**：breaking change（v0.x 阶段暂不使用）→ `./scripts/bump.sh major`

不确定时看 `CHANGELOG.md [Unreleased]` 里 `### Added` / `### Changed` / `### Removed` 的分布：
- 有 `Removed` + breaking → major；有 `Added` → minor；只有 `Fixed` → patch。

### 2. 跑 bump

```bash
# 常规
EDITOR=true bash scripts/bump.sh minor

# dry-run 预览不落盘
bash scripts/bump.sh minor --dry-run

# pre-release（如 v0.4.0-rc.1）
bash scripts/bump.sh minor --pre rc.1
```

bump.sh 做的事（按顺序）：

1. 幂等性检查：tag 已存在则拒绝（避免重复 bump 留脏 tag）
2. 迁移 `[Unreleased]` 内容到新版本节；若 `[Unreleased]` 为空，回落到 Conventional Commits 自动分类
3. 更新 CHANGELOG 底部的 `[Unreleased]` / `[vX.Y.Z]` 链接引用
4. 创建 `chore(release): vX.Y.Z` commit（`--no-verify` 跳过，但建议不跳）
5. 打带签名 tag（若 git 配置了签名）`vX.Y.Z` 指向该 commit
6. push master + tag 到 origin

### 3. 验证 release workflow

push tag 触发 `.github/workflows/release.yml`：

```bash
gh run list --workflow=release.yml --limit 1
gh run watch <run-id>   # 或直接 gh run view <run-id>
```

workflow 产物应包含：

- `gw_vX.Y.Z_darwin_arm64.tar.gz`
- `gw_vX.Y.Z_linux_amd64.tar.gz`
- `checksums.txt`
- Release notes（从 CHANGELOG 截取对应版本段）

### 4. 人工复核

- 打开 `https://github.com/Anthoooooooony/gw/releases/tag/vX.Y.Z`
- 确认 assets 齐全
- 确认 Release notes 正文非空且与 CHANGELOG 对齐

## Hotfix（已发布版本紧急修复）

从 tag 切 `hotfix/<short-desc>` 分支开始：

```bash
git checkout -b hotfix/xxx vX.Y.Z
# 修复 + 提 PR → master
```

PR 合入 master 后按上述标准 patch 流程发布。v0.x 阶段**不维护历史版本分支**，所有修复都从 master 打新 patch tag。

## 失败排查

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| bump.sh 拒绝因为 tag 已存在 | 上一次 bump 半途失败留下 local tag | `git tag -d vX.Y.Z` 后重跑 |
| release workflow 卡在 goreleaser-like 步骤 | workflow 由自定义 `release.yml` 构建，无 `.goreleaser.yml` | 查 `.github/workflows/release.yml`（见 P0 重构说明）|
| tag 推上去但无 release | release workflow 失败 | `gh run view <id> --log-failed`；修复后删除 tag 重 bump |
| `[Unreleased]` 空内容 | bump.sh 会回落 auto-gen；若这不是你想要的，手工编辑 CHANGELOG 再 bump | —— |

## 参考

- `scripts/bump.sh` 纯函数 + 集成测试：`scripts/bump_test.sh`（43 个 assertion）
- CI 8 层 gate：`CONTRIBUTING.md` 测试表 / `.github/workflows/ci.yml`
- 版本规范：[Semantic Versioning](https://semver.org/) + [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)

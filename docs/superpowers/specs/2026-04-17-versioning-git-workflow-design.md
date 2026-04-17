# gw 版本管理体系 + Git Workflow 设计

**Status**: Approved
**Date**: 2026-04-17
**Scope**: 面向 B-minimal 分发目标（GitHub Releases 二进制）的版本管理与分支工作流

---

## 变更历史

- 2026-04-17 初稿：两干模型（master + dev）
- 2026-04-17 修订：退回 GitHub Flow（master + feature/* + hotfix/*）。原因：实际执行中 feature 流向从未走 dev，hotfix cherry-pick 铁律一次未执行，dev 分支内容与 master 完全同步——规范与现实脱节。单维护者项目 GitHub Flow 更匹配。

---

## 1. 背景与目标

gw 目前（master @ `fad7e5b`）已完成 P0 MVP + P1 Track A/B，具备完整 CLI 能力与双层过滤引擎，但**零版本化**：

- 无 git tag、无 CHANGELOG、无 release 配置
- `cmd/version.go` 已备好 ldflags 注入接口，但 `Version` 默认为 `"dev"`
- PR workflow 临时启用，master 作为唯一长期分支

本 spec 确定分发目标为 **B-minimal**：GitHub Releases 提供跨平台二进制，严格 SemVer，不涉及 Homebrew / Docker / 签名 / LTS。

## 2. 分支拓扑

采用 **GitHub Flow 单干模型**：`master` 是唯一长期分支，永远可发布。

```
feature/* ──PR──▶ master ──tag vX.Y.Z──▶ GitHub Release
                    ▲
hotfix/* ──PR──────▶ (紧急修复已发布版本，同样 base master)
```

### 2.1 分支角色

| 分支 | 语义 | 生存期 | Base |
|------|------|--------|------|
| `master` | 唯一长期分支，永远可发布，是 GitHub default branch | 永久 | —— |
| `feature/*` | 单个功能/修复 | 短期，合入 master 后删除 | `master` |
| `hotfix/*` | 紧急修复已发布版本 | 短期，合入 master 后删除 | `master` |

### 2.2 关键规则

- **所有 PR base 均为 `master`**（feature 和 hotfix 一致）
- **短期分支合入 master 后立即删除**
- **release** 在 master 上直接运行 `scripts/bump.sh`，生成 CHANGELOG + tag + push，CI 触发 release workflow
- **hotfix** 语义上与 feature 区分只是为了追踪便利，流程完全相同（base=master，merge 后删分支）

### 2.3 CI 触发矩阵

| 事件 | 触发 CI | 触发 Release |
|------|---------|--------------|
| push to `master` | ✅ test + build | ❌ |
| PR open/sync（任何 base）| ✅ test + build | ❌ |
| push tag `v*.*.*` | ❌（tag 所在 commit 已在 master push 时测过）| ✅ |

## 3. 版本号机制

严格 SemVer：`vX.Y.Z`。

- **MVP 阶段**：`v0.y.z`（y 递增表示功能集，z 表示补丁）
- **首个 tag**：`v0.1.0`，打在 spec 合并 + P1 Track A/B 落地 + bump 脚本就绪之后
- **v1.0.0 门槛**：承诺向后兼容 gw CLI 接口 + TOML 规则 schema 稳定，暂不定时间
- **Pre-release**：支持 `vX.Y.Z-rc.N` / `vX.Y.Z-alpha.N` / `vX.Y.Z-beta.N`，`bump.sh --pre rc.1` 参数生成

### 3.1 bump 脚本

位置：`scripts/bump.sh`

用法：
```bash
./scripts/bump.sh patch                # v0.1.0 → v0.1.1
./scripts/bump.sh minor                # v0.1.0 → v0.2.0
./scripts/bump.sh major                # v0.1.0 → v1.0.0
./scripts/bump.sh minor --pre rc.1     # v0.1.0 → v0.2.0-rc.1
./scripts/bump.sh patch --dry-run      # 只打印预期结果，不 commit/tag
```

脚本职责（按顺序）：

1. **校验**：确保在 master 分支、工作区干净、已 fetch 远程
2. **计算新版本**：`git describe --tags --abbrev=0` 取上一个 tag，按 patch/minor/major/pre 规则递增
3. **生成 CHANGELOG 条目**：
   - `git log vPREV..HEAD --oneline` 抽 commit 列表
   - 按 conventional commit 前缀分类到 `### Added` / `### Changed` / `### Fixed` / `### Removed` 四节
   - 插入到 `CHANGELOG.md` 顶部，格式遵循 [Keep a Changelog](https://keepachangelog.com/)
4. **人工编辑门**：脚本暂停（或开 `$EDITOR`）让维护者调整 CHANGELOG 内容
5. **提交**：`git add CHANGELOG.md && git commit -m "chore(release): vX.Y.Z"`
6. **打 tag**：`git tag -a vX.Y.Z -m "release: vX.Y.Z"`（annotated tag，GoReleaser 依赖）
7. **push**：`git push origin master && git push origin vX.Y.Z`

### 3.2 CHANGELOG 格式

文件：`CHANGELOG.md`（仓库根）

结构（Keep a Changelog 风格）：

```markdown
# Changelog

All notable changes to gw will be documented in this file.

## [Unreleased]

## [v0.1.0] - 2026-04-17

### Added
- feat(filter): Gradle StreamFilter 流式过滤器 (#1)
- feat(filter): Node/Python/Rust 生态 TOML 规则 (#2)

### Fixed
- fix(test): exec_test 接受 HEAD detached 状态 (#3)

[Unreleased]: https://github.com/Anthoooooooony/gw/compare/v0.1.0...HEAD
[v0.1.0]: https://github.com/Anthoooooooony/gw/releases/tag/v0.1.0
```

Commit 前缀到节的映射（bump.sh 按此分类）：

| Prefix | CHANGELOG 节 |
|--------|---------------|
| `feat:` | Added |
| `fix:` | Fixed |
| `refactor:` / `perf:` | Changed |
| `remove:` / BREAKING | Removed |
| `docs:` / `chore:` / `test:` / `ci:` | 不入 CHANGELOG（除非 BREAKING） |

## 4. Release 管道

工具：**GoReleaser**（Go 生态事实标准）。

### 4.1 文件清单

- `.github/workflows/release.yml` —— tag push 触发的 release 工作流
- `.goreleaser.yml` —— GoReleaser 配置

### 4.2 `.github/workflows/release.yml`

触发：`push: tags: ['v*.*.*']`

步骤：
1. `actions/checkout@v4` with `fetch-depth: 0`（GoReleaser 需要完整 git 历史）
2. `actions/setup-go@v5` 安装 Go
3. `goreleaser/goreleaser-action@v6` run release（需 `GITHUB_TOKEN` 环境变量）

**CGO 处理**：因 go-sqlite3 依赖 CGO，不能在单个 runner 上跨平台编译。采用 **GoReleaser split/merge 多作业模板**：

- job `build-linux`（ubuntu-latest）→ 产 `linux-amd64` 部分的 partial release
- job `build-darwin-amd64`（macos-13）→ 产 `darwin-amd64`
- job `build-darwin-arm64`（macos-latest）→ 产 `darwin-arm64`
- job `release-merge`（ubuntu-latest，依赖前三者）→ 合并 partial releases，生成最终 GitHub Release

GoReleaser 的 `goreleaser release --split` / `goreleaser continue --merge` 命令天然支持此模式。相比在单个 runner 上用 matrix 循环，split/merge 让每个平台在对应 runner 上**原生编译**，无需 cross-compile toolchain。

### 4.3 `.goreleaser.yml` 关键配置

```yaml
project_name: gw
before:
  hooks:
    - go mod download
builds:
  - id: gw
    main: .
    binary: gw
    env:
      - CGO_ENABLED=1
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    ignore:
      - goos: linux
        goarch: arm64    # 首版跳过（需 zig cc 或 arm runner）
    ldflags:
      - -s -w
      - -X github.com/gw-cli/gw/cmd.Version={{.Version}}
      - -X github.com/gw-cli/gw/cmd.Commit={{.Commit}}
      - -X github.com/gw-cli/gw/cmd.BuildDate={{.Date}}
archives:
  - id: gw
    name_template: 'gw_{{.Version}}_{{.Os}}_{{.Arch}}'
    format: tar.gz
    files: [README.md, CHANGELOG.md, LICENSE*]
checksum:
  name_template: 'checksums.txt'
release:
  github:
    owner: Anthoooooooony
    name: gw
  draft: false
  prerelease: auto   # v*-rc.* / v*-alpha.* / v*-beta.* 自动标为 prerelease
  extract_changelog: true  # 从 CHANGELOG.md 对应节抽 release notes
```

### 4.4 首版交付平台矩阵

| Platform | Arch | 支持 | 理由 |
|----------|------|------|------|
| Linux | amd64 | ✅ | ubuntu-latest 原生 CGO |
| Darwin | amd64 | ✅ | macos-13 runner 原生 |
| Darwin | arm64 | ✅ | macos-latest 原生 |
| Linux | arm64 | ❌（后续版本补齐）| 需 zig cc 或 arm runner |
| Windows | any | ❌（长期降级）| CGO + Windows 痛点多，且 procgroup 仅降级支持 |

## 5. 一次完整 Release 流程（走查）

以 `v0.1.0` 首版为例：

```bash
# 1. 确保所有预期 feature/* 已合入 master
git checkout master && git pull origin master

# 2. 跑 bump 脚本
./scripts/bump.sh minor
# 脚本开 $EDITOR 让你审 CHANGELOG，保存退出
# 脚本 commit + tag + push

# 3. CI 的 release workflow 自动触发（push tag v0.1.0）
gh run watch <run-id>
# release.yml 跑完后，GitHub Releases 页面出现 v0.1.0 + 3 个二进制 + checksums.txt
```

**Hotfix 场景**（假设 v0.1.0 已发布，发现严重 bug）：

```bash
git checkout master && git pull
git checkout -b hotfix/xxx
# 修复 + 测试
gh pr create --base master --title "fix: ..."
# merge
git checkout master && git pull
./scripts/bump.sh patch   # → v0.1.1 打 tag
```

## 6. 迁移计划（当前 → 此 spec）

1. **首个 CHANGELOG**：创建 `CHANGELOG.md`，写入 `## [Unreleased]` + 历史节（可选回填）
2. **写 `scripts/bump.sh`**：遵循 §3.1 职责
3. **写 `.goreleaser.yml` 和 `.github/workflows/release.yml`**：遵循 §4
4. **首次 dry-run**：`./scripts/bump.sh minor --dry-run` 验证输出
5. **打 `v0.1.0`**：实际 release
6. **更新 README**：加一段"Installation"说明 GitHub Release 下载链接
7. **更新 CLAUDE.md**：记录分支约定（GitHub Flow，所有 PR base=master）

## 7. 未覆盖（显式 YAGNI）

以下内容**本 spec 不纳入**，需要时再升级：

- 二进制签名（Sigstore / cosign）
- Homebrew formula（`brew tap` 仓库）
- Docker 镜像（ghcr.io）
- Linux arm64（等有需求再加 zig cc）
- Windows 支持
- SBOM / provenance attestation
- 自动化 changelog 审批（release-please 等 bot）
- 双向同步 dev ↔ master 的自动化 workflow

## 8. 成功标准

- 首个 release `v0.1.0` 通过 GitHub Releases 可下载 3 个平台二进制
- `gw version` 输出包含真实 tag 版本号（非 "dev"）和 commit
- CHANGELOG 对应节内容非空且语义清晰
- hotfix 流程至少走通一次（即便是人为制造的演练）
- 维护者能在 5 分钟内从"想发版"到"tag pushed"

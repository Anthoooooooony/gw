# Contributing

面向维护者的操作约定。日常开发规范见 [`CLAUDE.md`](./CLAUDE.md)（Claude Code 也读这份）。

## Issue 规范

- 所有 issue 必须走 `.github/ISSUE_TEMPLATE/` 下的模板，blank issue 已禁用。
- 两档模板：
  - `bug_report.md`：汇报异常行为，须给现象 / 复现步骤 / 预期 vs 实际 / 环境信息。
  - `feature_request.md`：新功能、改进行为、或长程 **tracking / meta** 类议题。tracking 类在 title 加 `tracking: ` 前缀（如 `tracking: Windows 适配`）。
- **每个 issue 必须同时贴 priority + 类型两个维度的 label**，缺一不可：
  - **priority 维度**（按紧迫度三选一）：
    - `priority:near` 近期（1-2 周内会动手）
    - `priority:mid` 中期（1-2 月内可能启动）
    - `priority:long` 长期 / YAGNI 观察，先挂着不排期
  - **类型维度**（二选一）：
    - `bug` 异常行为修复
    - `feature` 新功能 / 行为改进 / 长程 tracking
- area / breaking / security 等其它维度信息通过 title 前缀和 body 内容承载，**不再用 label 表达**——减少标签噪音。
- 关闭策略：被 PR 修复由 commit message 里的 `Closes #N` 自动关闭；失效或决定不做的挂 `priority:long` 或直接 close 并在评论里说明。

## 分支与 PR

- **GitHub Flow 单干**：`master` 唯一长期分支，所有改动走 `feature/*` / `fix/*` / `chore/*` / `docs/*` / `hotfix/*` → PR → squash merge。
- **release-please bot 例外**：它会直接向 `master` 开 release PR 并在合入后 push tag，这是由 `.github/workflows/release-please.yml` 自动处理的合法路径，不走人工 PR 流程。
- 短期分支合入后立即删除（`gh pr merge --delete-branch`）。
- PR 必须过 7 层 CI gate：`test (ubuntu-latest)` / `test (macos-latest)` / `build (ubuntu-latest, CGO)` / `build (macos-latest, CGO)` / `actionlint` / `golangci-lint` / `govulncheck`。`test` job 内部串联 `go mod tidy check` / `go vet` / `gofmt` / `go test -race -cover`。

## PR title（Conventional Commits）

**强制**：PR title 符合 [Conventional Commits](https://www.conventionalcommits.org/)。合并策略是 squash，GitHub repo 设置 "Default commit message = Pull request title"，因此 master 上每个 commit subject 就是对应 PR 的 title——release-please 就从这些 subject 推导版本 bump 与 GitHub Release notes。

| 前缀 | 版本 bump（v0.x 阶段） | 归入 Release notes 节 |
|------|------------------------|------------------------|
| `feat:` / `feat(scope):` | minor | Features |
| `fix:` / `fix(scope):` | patch | Bug Fixes |
| `perf:` | patch | Performance Improvements |
| `refactor:` | patch | Code Refactoring |
| `revert:` | patch | Reverts |
| `deps:` | patch | Dependencies |
| `docs:` / `chore:` / `ci:` / `test:` / `style:` / `build:` | 不触发 release | hidden |

映射在 `release-please-config.json::changelog-sections` 里显式声明（覆盖 release-please 默认值——默认会把 refactor / build 归入 hidden）。

**破坏性改动**（v0.x 阶段仍只触发 minor，进 1.0 后触发 major）：

1. 主标题带 `!`：`feat(api)!: drop /v1/legacy`
2. body footer：
   ```
   feat: redesign filter API

   BREAKING CHANGE: FilterFunc signature changed
   ```

## Release 流程（全自动，单 workflow）

Release 由 `.github/workflows/release.yml` 一体化驱动：

1. PR 合入 `master`（PR title 是 CC 格式）
2. workflow 触发，`decide` job 跑 `scripts/release-helpers.sh` 的分类函数扫自上一个 release tag 以来的 commit subject
3. 全部是 `docs:` / `chore:` / `ci:` / `test:` / `style:` / `build:` → skip，workflow 静默结束
4. 有任一 `feat:` / `fix:` / `perf:` / `refactor:` / `revert:` / `deps:` 或 BREAKING → 进入发版链路：
   - 计算新 tag（v0.x 阶段 feat/BREAKING→minor，其他 visible→patch）
   - 生成 markdown release notes（分节：Features / Bug Fixes / Performance Improvements / Code Refactoring / Reverts / Dependencies / BREAKING），作为 artifact 传递
   - `build` matrix 在 linux_amd64 / darwin_arm64 上 CGO 编译，产物 `*.tar.gz`
   - `release` job 打 annotated tag 并 push、生成 `checksums.txt`、`gh release create` 创建 GitHub Release 并上传所有 assets

不开 release PR、不需要人工 approve、不依赖外部 token——整条链在一个 workflow run 内完成，`GITHUB_TOKEN` 的 `contents:write` 权限即可。

不维护仓库内 `CHANGELOG.md` 文件，变更说明只在 GitHub Release 页面。历史发布记录从 [Releases](https://github.com/Anthoooooooony/gw/releases) 翻阅。

### 预览将要发什么版

本地也可以跑相同分类逻辑预览：

```bash
source scripts/release-helpers.sh
prev_tag=$(git describe --tags --abbrev=0)
git log "${prev_tag}..HEAD" --format='%s%n%b%x1F%h%x00' |
  build_release_notes "v-next" "$prev_tag" "Anthoooooooony/gw"
```

## 测试

| 层 | 命令 | 触发条件 |
|----|------|---------|
| Go 单测 | `go test -race ./...` | 所有 Go 改动 |
| 格式 / 静态 | `gofmt -l .` / `go vet ./...` | 所有 Go 改动（CI test job 内部同跑） |
| mod 整洁 | `go mod tidy && git diff --exit-code go.mod go.sum` | 依赖变动 |
| lint | `golangci-lint run` | 本地选测 |
| vulncheck | `govulncheck ./...` | 依赖升级后 |

本地跑 CI 等价集：`make ci`（串起 tidy / vet / race test）。

本地覆盖率：`go test -coverprofile=coverage.out -covermode=atomic ./... && go tool cover -html=coverage.out`。

## 场景化压缩率 baseline

`filter/scenario_test.go` 按命令场景（mvn compile/test/package × 成功/失败 × batch/stream、gradle、git）断言当前压缩率与入库的 `filter/testdata/scenario_baseline.json` 相比**不偏离超过 2 个百分点**。

改了过滤规则后流程：

1. 本地跑 `go test ./filter/ -run TestScenarioCompression -v`
2. 失败时查看 `filter/scenario-compression-report.md` 的 `Δ (pp)` 列确认变化是否符合预期
3. **有意改进**：`go test ./filter/ -run TestScenarioCompression -args -update` 重新生成 baseline，把 `scenario_baseline.json` 的 diff 一并提交，PR 里 reviewer 会看到 "77% → 85%" 的压缩率跃迁
4. **无意退化**：回头查规则，不要随手 `-update`

新增场景：fixture 放 `filter/*/testdata/`，在 `scenario_test.go::scenarios` 加一行，走 3 跑 `-update` 生成 baseline。

CI 会把每次运行的压缩率表贴到 Actions 页面的 Summary，也作为 `scenario-compression-report` artifact 上传。

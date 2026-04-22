# Contributing

面向维护者的操作约定。日常开发规范见 [`CLAUDE.md`](./CLAUDE.md)（Claude Code 也读这份）。

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

## Release 流程（全自动）

Release 由 [release-please](https://github.com/googleapis/release-please) 驱动，维护者无需手工打 tag 或编辑版本号：

1. PR 合入 `master`（PR title 是 CC 格式）
2. `release-please.yml` workflow 触发，扫自上一个 release tag 以来的 master commit
3. 若有 `feat:` / `fix:` / `refactor:` / `perf:` / `remove:` 或 BREAKING commit，release-please 开（或更新）一个 **release PR**，PR body 预览下个版本号 + 变更摘要
4. 审阅 release PR，合入即触发：
   - 打 annotated tag `vX.Y.Z`
   - 创建 GitHub Release（Release notes 自动生成，不依赖仓库内 CHANGELOG 文件）
   - Tag push 触发 `release.yml` 构建跨平台 binary，上传 `*.tar.gz` + `checksums.txt` 到刚创建的 Release

配置入口：
- `release-please-config.json` — release-type / skip-changelog / bump 策略
- `.release-please-manifest.json` — 当前版本源（release-please 合 release PR 时自动更新）

不再维护 `CHANGELOG.md` 文件——变更说明只出现在 GitHub Release 页面。历史发布记录可从 [Releases](https://github.com/Anthoooooooony/gw/releases) 页面翻阅。

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

# Contributing

面向维护者的操作约定。日常开发规范见 [`CLAUDE.md`](./CLAUDE.md)（Claude Code 也读这份）。

## 分支与 PR

- **GitHub Flow 单干**：`master` 唯一长期分支，所有改动走 `feature/*` / `fix/*` / `chore/*` / `docs/*` / `hotfix/*` → PR → squash merge。
- 仅 **`scripts/bump.sh` 触发的 release commit** 允许直推 `master`。
- 短期分支合入后立即删除（`gh pr merge --delete-branch`）。
- PR 必须过 8 层 CI gate：`test (ubuntu-latest)` / `test (macos-latest)` / `build (ubuntu-latest, CGO)` / `build (macos-latest, CGO)` / `shellcheck` / `actionlint` / `golangci-lint` / `govulncheck`。`test` job 内部串联 `go mod tidy check` / `go vet` / `gofmt` / `go test -race -cover` / `bash scripts/bump_test.sh`（最后一项只在 ubuntu runner 上跑）。

## Commit message

强制 [Conventional Commits](https://www.conventionalcommits.org/)：

| 前缀 | 对应 CHANGELOG 节 |
|------|-------------------|
| `feat:` / `feat(scope):` | Added |
| `fix:` / `fix(scope):` | Fixed |
| `refactor:` / `perf:` | Changed |
| `remove:` | Removed |
| `docs:` / `chore:` / `ci:` / `test:` | 不入 CHANGELOG |

**破坏性改动**两种写法都认（`scripts/bump.sh::classify_commit`）：

1. 主标题带 `!`：`feat(api)!: drop /v1/legacy`
2. body footer：
   ```
   feat: redesign filter API

   BREAKING CHANGE: FilterFunc signature changed
   ```

两者都归入 `Removed` 节。

## CHANGELOG 手工编辑时机

`scripts/bump.sh` 有两条生成路径：

1. **Migration 模式**（优先）：`[Unreleased]` 下有手工内容 → 整体搬到新版本节，`[Unreleased]` 自动复位为空子节骨架（`### Added / Changed / Fixed / Removed`）
2. **Auto-gen fallback**：`[Unreleased]` 为空（仅保留子节骨架）→ 用 commit subject 按前缀（feat / fix / refactor / remove / BREAKING）自动归类生成

下面几种场景**应当手工编辑 `[Unreleased]` 节**后再 bump：

- Commit subject 不足以说明改动（内部重构影响到用户行为）
- 多个 commit 合并为单一 user-facing feature，需要合并条目
- 安全/破坏性改动需要迁移指南链接
- 手工调整条目顺序以凸显重点

手工编辑只改 `[Unreleased]` 节，bump.sh 会把手工内容**迁移**到新版本节（不是追加/保留）。`--dry-run` 会显示使用的路径（`migration` 或 `auto-gen fallback`）。

## Release 流程

Pre-bump 清单（在 `master` 干净、已 `git pull` 的前提下）：

1. `bash scripts/bump_test.sh` 本地全绿
2. `./scripts/bump.sh <kind> --dry-run` 预览 CHANGELOG 节
3. 如需补充，编辑 `CHANGELOG.md` 的 `[Unreleased]` 节并提交（走 PR 流程）
4. 正式 bump：`./scripts/bump.sh <patch|minor|major>`
   - 运行 `EDITOR` 打开 CHANGELOG 做最终 review
   - 编辑器关闭后 commit + tag + push 触发 `release.yml`

`bump.sh` 已内置的安全护栏（勿重复造）：
- 必须在 `master` 分支 + 干净工作区 + 与 `origin/master` 同步
- 新 tag 不得已存在于本地或远端（幂等性）
- `--dry-run` 可预览不落盘

## 测试

| 层 | 命令 | 触发条件 |
|----|------|---------|
| Go 单测 | `go test -race ./...` | 所有 Go 改动 |
| bash 单测 | `bash scripts/bump_test.sh` | `scripts/` 改动 |
| lint | `golangci-lint run` / `shellcheck scripts/*.sh` | 本地选测 |
| vulncheck | `govulncheck ./...` | 依赖升级后 |

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

# gw Versioning + Git Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status**: Partially Superseded（Task 8 已作废）

**Goal:** 搭建 gw 首版 release 管道——CHANGELOG、bump 脚本、GoReleaser 配置、release GitHub Action，并发布 `v0.1.0`。

**Architecture:** GitHub Flow 单干模型（master），SemVer 版本号，GoReleaser split/merge 多平台原生 CGO 构建。bump.sh 读 git 历史生成 CHANGELOG + tag + push；tag push 触发 release.yml 在三台 runner（ubuntu/macos-13/macos-latest）并行构建，合并产出 GitHub Release + 3 个 tar.gz + checksums.txt。

**Tech Stack:** bash（bump 脚本）、GoReleaser v2、GitHub Actions、Go 1.22+、CGO(go-sqlite3)。

**Spec:** `docs/superpowers/specs/2026-04-17-versioning-git-workflow-design.md`

---

## Task 1: Bootstrap CHANGELOG.md

**Files:**
- Create: `CHANGELOG.md`

- [ ] **Step 1: 创建 CHANGELOG.md（仅种子 [Unreleased] 节）**

> **Rationale**：v0.1.0 的具体内容由 Task 9 的 `bump.sh` 从 `git log HEAD` 自动抽取分类，避免在此手写再被 bump 覆盖产生重复节。

```markdown
# Changelog

本文件记录 gw 所有 notable 变更，遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式 + [Semantic Versioning](https://semver.org/)。

## [Unreleased]

### Added

### Changed

### Fixed

### Removed

[Unreleased]: https://github.com/Anthoooooooony/gw/compare/HEAD
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): 初始化 CHANGELOG（仅 [Unreleased] 种子节）"
```

---

## Task 2: bump.sh — 版本解析与校验（TDD）

**Files:**
- Create: `scripts/bump.sh`
- Create: `scripts/bump_test.sh`

本任务用**函数式拆分**：核心纯函数（版本号递增、conventional commit 分类）放 `scripts/bump.sh` 顶部并导出，由 `bump_test.sh` 通过 `source` 引入独立测试。副作用（git 操作）留到后续任务。

- [ ] **Step 1: 写 bump_test.sh 初版（失败测试）**

```bash
#!/usr/bin/env bash
# scripts/bump_test.sh — bump.sh 纯函数单元测试
set -u
PASS=0
FAIL=0

assert_eq() {
  local expected="$1" actual="$2" msg="${3:-}"
  if [[ "$expected" == "$actual" ]]; then
    PASS=$((PASS+1))
  else
    FAIL=$((FAIL+1))
    echo "FAIL: $msg"
    echo "  expected: $expected"
    echo "  actual:   $actual"
  fi
}

# 让 bump.sh 进入 "被 source 模式"——不执行 main
BUMP_LIB_ONLY=1
# shellcheck source=./bump.sh
source "$(dirname "$0")/bump.sh"

# ========== parse_version ==========
assert_eq "0 1 2" "$(parse_version v0.1.2)" "parse_version v0.1.2"
assert_eq "1 0 0" "$(parse_version v1.0.0)" "parse_version v1.0.0"
assert_eq "0 0 0" "$(parse_version v0.0.0)" "parse_version v0.0.0"

# ========== bump_version ==========
assert_eq "v0.1.3" "$(bump_version v0.1.2 patch)" "patch bump"
assert_eq "v0.2.0" "$(bump_version v0.1.2 minor)" "minor bump"
assert_eq "v1.0.0" "$(bump_version v0.1.2 major)" "major bump"
assert_eq "v0.1.3-rc.1" "$(bump_version v0.1.2 patch rc.1)" "patch + pre"

echo "---"
echo "PASS: $PASS, FAIL: $FAIL"
[[ $FAIL -eq 0 ]]
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
chmod +x scripts/bump_test.sh && bash scripts/bump_test.sh
```

Expected: `scripts/bump.sh: No such file or directory`

- [ ] **Step 3: 写 bump.sh 最小实现（只含 parse_version / bump_version）**

```bash
#!/usr/bin/env bash
# scripts/bump.sh — gw 版本 bump 工具
# 用法：./scripts/bump.sh [patch|minor|major] [--pre LABEL] [--dry-run]
set -euo pipefail

# ========== 纯函数（供测试 source）==========

# parse_version v0.1.2 → "0 1 2"
parse_version() {
  local v="${1#v}"
  IFS=. read -r major minor patch <<< "$v"
  echo "$major $minor $patch"
}

# bump_version v0.1.2 patch [pre-label] → vX.Y.Z[-pre]
bump_version() {
  local curr="$1" kind="$2" pre="${3:-}"
  read -r major minor patch <<< "$(parse_version "$curr")"
  case "$kind" in
    patch) patch=$((patch+1)) ;;
    minor) minor=$((minor+1)); patch=0 ;;
    major) major=$((major+1)); minor=0; patch=0 ;;
    *) echo "bump_version: unknown kind: $kind" >&2; return 1 ;;
  esac
  local out="v${major}.${minor}.${patch}"
  [[ -n "$pre" ]] && out="${out}-${pre}"
  echo "$out"
}

# ========== main（副作用部分，后续任务填充）==========

main() {
  echo "bump.sh: main 尚未实现（在 Task 4/5 中完善）" >&2
  return 1
}

# 仅当被直接执行、非 source 时调用 main
if [[ -z "${BUMP_LIB_ONLY:-}" ]]; then
  main "$@"
fi
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
chmod +x scripts/bump.sh && bash scripts/bump_test.sh
```

Expected:
```
---
PASS: 7, FAIL: 0
```

- [ ] **Step 5: Commit**

```bash
git add scripts/bump.sh scripts/bump_test.sh
git commit -m "feat(bump): 版本号解析与递增纯函数 + 单元测试"
```

---

## Task 3: bump.sh — Conventional commit 分类器（TDD）

**Files:**
- Modify: `scripts/bump.sh`
- Modify: `scripts/bump_test.sh`

- [ ] **Step 1: 扩展 bump_test.sh 加 classify_commit 测试**

在 `bump_test.sh` 的 `========== bump_version ==========` 段之后、`echo "---"` 之前加入：

```bash
# ========== classify_commit ==========
assert_eq "Added" "$(classify_commit 'feat(filter): 新规则')" "feat → Added"
assert_eq "Added" "$(classify_commit 'feat: xxx')" "feat (no scope) → Added"
assert_eq "Fixed" "$(classify_commit 'fix(test): detached HEAD')" "fix → Fixed"
assert_eq "Changed" "$(classify_commit 'refactor(cmd): 抽取')" "refactor → Changed"
assert_eq "Changed" "$(classify_commit 'perf(engine): 缓存正则')" "perf → Changed"
assert_eq "Removed" "$(classify_commit 'remove: 淘汰旧 API')" "remove → Removed"
assert_eq ""        "$(classify_commit 'docs(readme): 同步')" "docs → 忽略"
assert_eq ""        "$(classify_commit 'chore: 版本 bump')" "chore → 忽略"
assert_eq ""        "$(classify_commit 'ci: runner 升级')" "ci → 忽略"
assert_eq ""        "$(classify_commit 'test: 加 fixture')" "test → 忽略"
# BREAKING CHANGE 覆盖——不管是什么前缀都归 Removed
assert_eq "Removed" "$(classify_commit 'feat!: BREAKING')" "feat! → Removed"
assert_eq "Removed" "$(classify_commit 'fix(api)!: BREAKING')" "fix(scope)! → Removed"
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
bash scripts/bump_test.sh
```

Expected: `FAIL: feat → Added` 等多条失败，因为 `classify_commit` 未定义。

- [ ] **Step 3: 实现 classify_commit 并加到 bump.sh**

在 `bump.sh` 的 `bump_version` 函数之后插入：

```bash
# classify_commit "feat(x): msg" → "Added" | "Fixed" | "Changed" | "Removed" | ""
# 空字符串表示该 commit 不入 CHANGELOG（docs/chore/ci/test）
classify_commit() {
  local msg="$1"
  # BREAKING CHANGE 优先——任何前缀带 ! 都是 Removed（向后不兼容）
  if [[ "$msg" =~ ^[a-z]+(\([^\)]+\))?!: ]]; then
    echo "Removed"
    return
  fi
  # 按前缀分类
  case "$msg" in
    feat\(*\):*|feat:*)                 echo "Added" ;;
    fix\(*\):*|fix:*)                   echo "Fixed" ;;
    refactor\(*\):*|refactor:*|perf\(*\):*|perf:*) echo "Changed" ;;
    remove\(*\):*|remove:*)             echo "Removed" ;;
    *) echo "" ;;
  esac
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
bash scripts/bump_test.sh
```

Expected: `PASS: 19, FAIL: 0`

- [ ] **Step 5: Commit**

```bash
git add scripts/bump.sh scripts/bump_test.sh
git commit -m "feat(bump): conventional commit 分类器 + BREAKING CHANGE 处理"
```

---

## Task 4: bump.sh — CHANGELOG 生成器（TDD）

**Files:**
- Modify: `scripts/bump.sh`
- Modify: `scripts/bump_test.sh`

- [ ] **Step 1: 扩展 bump_test.sh 加 build_changelog_section 测试**

在已有测试段之后插入：

```bash
# ========== build_changelog_section ==========
input='feat(filter): 新 toml 规则
fix(test): detached HEAD
docs(readme): 同步
refactor(cmd): 抽取'

expected='## [v0.2.0] - 2026-04-17

### Added
- feat(filter): 新 toml 规则

### Changed
- refactor(cmd): 抽取

### Fixed
- fix(test): detached HEAD
'

actual=$(build_changelog_section "v0.2.0" "2026-04-17" <<<"$input")
assert_eq "$expected" "$actual" "build_changelog_section 基础分类"

# 空输入不输出空节
actual=$(build_changelog_section "v0.3.0" "2026-05-01" <<<"docs: 只有文档改动")
expected='## [v0.3.0] - 2026-05-01

_无 notable 变更（仅文档/构建/测试）_
'
assert_eq "$expected" "$actual" "build_changelog_section 仅忽略类"
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
bash scripts/bump_test.sh
```

Expected: `build_changelog_section: command not found`

- [ ] **Step 3: 实现 build_changelog_section**

在 `bump.sh` 的 `classify_commit` 函数之后插入：

```bash
# build_changelog_section VERSION DATE < stdin（每行一条 commit message）→ markdown 节
# 按 Added / Changed / Fixed / Removed 四节输出；无任何归类内容时输出"无 notable 变更"
build_changelog_section() {
  local version="$1" date="$2"
  declare -A buckets=([Added]="" [Changed]="" [Fixed]="" [Removed]="")
  local line cat
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    cat=$(classify_commit "$line")
    [[ -z "$cat" ]] && continue
    buckets[$cat]+="- $line"$'\n'
  done

  local out="## [$version] - $date"$'\n\n'
  local has_any=0
  for section in Added Changed Fixed Removed; do
    if [[ -n "${buckets[$section]}" ]]; then
      out+="### $section"$'\n'"${buckets[$section]}"$'\n'
      has_any=1
    fi
  done
  if [[ $has_any -eq 0 ]]; then
    out+="_无 notable 变更（仅文档/构建/测试）_"$'\n'
  fi
  printf '%s' "$out"
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
bash scripts/bump_test.sh
```

Expected: `PASS: 21, FAIL: 0`

- [ ] **Step 5: Commit**

```bash
git add scripts/bump.sh scripts/bump_test.sh
git commit -m "feat(bump): CHANGELOG 段生成器（四节分类，空变更降级提示）"
```

---

## Task 5: bump.sh — main 流程（副作用整合）

**Files:**
- Modify: `scripts/bump.sh`

这一步实现 `main` 的完整流程。因为涉及真实 git 操作，采用 `--dry-run` 作为验证手段，不做单测。

- [ ] **Step 1: 实现 main 函数**

替换 `bump.sh` 里现有的 `main()` 占位函数为：

```bash
# ========== main ==========

usage() {
  cat <<EOF
Usage: $0 <patch|minor|major> [--pre LABEL] [--dry-run]

  patch|minor|major    bump 类别（必填）
  --pre LABEL          pre-release 后缀，如 --pre rc.1 → vX.Y.Z-rc.1
  --dry-run            打印预期结果但不 commit / tag / push

Examples:
  $0 minor                      # v0.1.0 → v0.2.0
  $0 patch --pre rc.1           # v0.1.0 → v0.1.1-rc.1
  $0 major --dry-run            # 只打印，不落盘
EOF
}

main() {
  local kind="" pre="" dry_run=0

  while [[ $# -gt 0 ]]; do
    case "$1" in
      patch|minor|major) kind="$1"; shift ;;
      --pre)             pre="$2"; shift 2 ;;
      --dry-run)         dry_run=1; shift ;;
      -h|--help)         usage; exit 0 ;;
      *)                 echo "gw bump: unknown arg: $1" >&2; usage; exit 2 ;;
    esac
  done

  [[ -z "$kind" ]] && { echo "gw bump: 缺少 bump 类别（patch|minor|major）" >&2; usage; exit 2; }

  # 1. 校验 master 分支 + 干净工作区
  local branch
  branch=$(git rev-parse --abbrev-ref HEAD)
  if [[ "$branch" != "master" ]]; then
    echo "gw bump: 必须在 master 分支运行（当前 ${branch}）" >&2
    exit 1
  fi
  if [[ -n "$(git status --porcelain)" ]]; then
    echo "gw bump: 工作区有未提交改动，先 commit 或 stash" >&2
    exit 1
  fi

  # 2. fetch + 校验与远端同步
  git fetch origin master --tags --quiet
  local local_head remote_head
  local_head=$(git rev-parse HEAD)
  remote_head=$(git rev-parse origin/master)
  if [[ "$local_head" != "$remote_head" ]]; then
    echo "gw bump: 本地 master 与 origin/master 不一致，先 pull / rebase" >&2
    exit 1
  fi

  # 3. 计算上一个 / 下一个 tag
  local prev_tag new_tag date
  prev_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
  new_tag=$(bump_version "$prev_tag" "$kind" "$pre")
  date=$(date -u +%Y-%m-%d)
  echo "gw bump: $prev_tag → $new_tag ($date)"

  # 4. 生成 CHANGELOG 节
  local commits changelog_section
  if [[ "$prev_tag" == "v0.0.0" ]]; then
    commits=$(git log --format="%s" HEAD)
  else
    commits=$(git log --format="%s" "${prev_tag}..HEAD")
  fi
  changelog_section=$(build_changelog_section "$new_tag" "$date" <<<"$commits")

  if [[ $dry_run -eq 1 ]]; then
    echo "--- 预期 CHANGELOG 新增 ---"
    printf '%s\n' "$changelog_section"
    echo "--- 预期 tag：$new_tag ---"
    echo "(dry-run：不 commit / 不 tag / 不 push)"
    exit 0
  fi

  # 5. 插入到 CHANGELOG.md 的 [Unreleased] 节之后
  if [[ ! -f CHANGELOG.md ]]; then
    echo "gw bump: CHANGELOG.md 不存在" >&2
    exit 1
  fi
  # 用 awk 在 "## [Unreleased]" 节结束（下一个 "## [" 之前）插入新节
  local tmp
  tmp=$(mktemp)
  awk -v block="$changelog_section" '
    /^## \[Unreleased\]/ { print; in_unreleased=1; next }
    in_unreleased && /^## \[/ { print block; in_unreleased=0 }
    { print }
    END { if (in_unreleased) print block }
  ' CHANGELOG.md > "$tmp"
  mv "$tmp" CHANGELOG.md

  # 6. 让维护者审 CHANGELOG
  ${EDITOR:-vi} CHANGELOG.md

  # 7. commit + tag + push
  git add CHANGELOG.md
  git commit -m "chore(release): $new_tag"
  git tag -a "$new_tag" -m "release: $new_tag"
  git push origin master "$new_tag"

  echo "gw bump: 已推送 $new_tag，release.yml 将由 tag push 触发"
}
```

- [ ] **Step 2: dry-run 验证（必须在 master 且 CHANGELOG.md 存在）**

```bash
bash scripts/bump.sh minor --dry-run
```

Expected: 打印 `gw bump: v0.0.0 → v0.1.0` 加预期 CHANGELOG 节（含 Task 1 中历史节被识别为 Added 的 feat commit），最后 `(dry-run：不 commit / 不 tag / 不 push)`。

- [ ] **Step 3: Commit**

```bash
git add scripts/bump.sh
git commit -m "feat(bump): main 流程——校验分支、生成 CHANGELOG、commit + tag + push"
```

---

## Task 6: 编写 .goreleaser.yml

**Files:**
- Create: `.goreleaser.yml`

- [ ] **Step 1: 创建 .goreleaser.yml**

```yaml
# .goreleaser.yml — gw release 构建配置
# GoReleaser v2 格式。每平台在对应 runner 上原生编译以处理 CGO。
version: 2

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
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    ignore:
      # 首版跳过 linux-arm64（需 zig cc 或 arm runner）
      - goos: linux
        goarch: arm64
    ldflags:
      - -s -w
      - -X github.com/gw-cli/gw/cmd.Version={{ .Version }}
      - -X github.com/gw-cli/gw/cmd.Commit={{ .Commit }}
      - -X github.com/gw-cli/gw/cmd.BuildDate={{ .Date }}

archives:
  - id: gw
    name_template: 'gw_{{ .Version }}_{{ .Os }}_{{ .Arch }}'
    formats: [tar.gz]
    files:
      - README.md
      - CHANGELOG.md
      - LICENSE*

checksum:
  name_template: 'checksums.txt'
  algorithm: sha256

changelog:
  # 用 CHANGELOG.md 对应节作为 release notes；GoReleaser v2 支持 --release-notes flag 外部注入
  disable: true

release:
  github:
    owner: Anthoooooooony
    name: gw
  draft: false
  prerelease: auto   # -rc.* / -alpha.* / -beta.* 自动标 prerelease
  mode: keep-existing
```

- [ ] **Step 2: 本地 snapshot 构建验证（不 push 不 tag）**

```bash
# 如果本地无 goreleaser，先安装：
# brew install goreleaser  或  go install github.com/goreleaser/goreleaser/v2@latest
goreleaser build --snapshot --clean --single-target
```

Expected: `dist/gw_<snapshot>_<os>_<arch>/gw` 产出；`./dist/gw_*/gw version` 输出 version 含 snapshot commit 信息。

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yml
git commit -m "build(release): GoReleaser v2 配置，CGO 多平台原生构建"
```

---

## Task 7: 编写 .github/workflows/release.yml

**Files:**
- Create: `.github/workflows/release.yml`

GoReleaser 的 split/merge 模式：每个平台在自己 runner 上跑 `goreleaser release --split`，最后在合并作业用 `goreleaser continue --merge` 汇总。split 模式通过 `GGOOS` 环境变量选择构建目标。

- [ ] **Step 1: 创建 .github/workflows/release.yml**

```yaml
name: release

on:
  push:
    tags:
      - 'v*.*.*'

permissions:
  contents: write   # 创建 GitHub Release 需要

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - runner: ubuntu-latest
            goos: linux
          - runner: macos-13           # Intel
            goos: darwin
          - runner: macos-latest       # Apple Silicon
            goos: darwin
    runs-on: ${{ matrix.runner }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0   # GoReleaser 需要完整 git 历史

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: Extract release notes from CHANGELOG
        id: notes
        run: |
          # 从 CHANGELOG.md 抽当前 tag 对应节
          version="${GITHUB_REF_NAME}"
          # 提取 "## [vX.Y.Z] - ..." 节内容到下一个 "## [" 或文件尾为止
          awk -v v="$version" '
            $1=="##" && $2=="["v"]" { in_block=1; next }
            in_block && /^## \[/ { exit }
            in_block { print }
          ' CHANGELOG.md > /tmp/release-notes.md
          echo "notes_file=/tmp/release-notes.md" >> "$GITHUB_OUTPUT"

      - name: Run GoReleaser (split)
        uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: release --split --clean --release-notes=${{ steps.notes.outputs.notes_file }}
        env:
          GGOOS: ${{ matrix.goos }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Upload partial dist
        uses: actions/upload-artifact@v4
        with:
          name: dist-${{ matrix.runner }}
          path: dist/
          retention-days: 1

  merge:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Download all partial dists
        uses: actions/download-artifact@v4
        with:
          path: dist/
          pattern: dist-*
          merge-multiple: true

      - name: Extract release notes
        run: |
          version="${GITHUB_REF_NAME}"
          # 提取 "## [vX.Y.Z] - ..." 节内容到下一个 "## [" 或文件尾为止
          awk -v v="$version" '
            $1=="##" && $2=="["v"]" { in_block=1; next }
            in_block && /^## \[/ { exit }
            in_block { print }
          ' CHANGELOG.md > /tmp/release-notes.md

      - name: Run GoReleaser (merge)
        uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: continue --merge --release-notes=/tmp/release-notes.md
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 2: lint workflow 文件（可选但推荐）**

```bash
# 如果本地有 actionlint：
actionlint .github/workflows/release.yml
```

Expected: 无 error（warning 可接受）。若无 actionlint，跳过此步，由后续实跑验证。

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci(release): tag push 触发 GoReleaser split/merge 多平台原生构建"
```

---

## Task 8: ~~建 dev 分支 + 设 default~~（**Superseded**）

此 task 在 2026-04-17 实施时完成，但审计后（同日）撤销——项目退回 GitHub Flow 单干模型。
参见 spec §2 的变更历史。

---

## Task 9: 首次真实 release — v0.1.0

**Files:**
- Modify: `CHANGELOG.md`（由 bump.sh 自动 commit）

- [ ] **Step 1: dry-run 预览**

```bash
bash scripts/bump.sh minor --dry-run
```

Expected: 打印 `v0.0.0 → v0.1.0` 加 CHANGELOG 节预览，**不**落盘。

- [ ] **Step 2: 真实 bump**

```bash
bash scripts/bump.sh minor
```

脚本会打开 `$EDITOR` 让你审 CHANGELOG.md。检查以下：
- `[Unreleased]` 节之后是否新增了 `[v0.1.0] - YYYY-MM-DD` 节
- 各 `###` 节分类是否正确（Added/Fixed 应非空）
- 底部 compare/release 链接是否正常

保存退出 → 脚本执行 commit + tag + push。

Expected stdout 末尾：`gw bump: 已推送 v0.1.0，release.yml 将由 tag push 触发`

- [ ] **Step 3: watch release CI**

```bash
sleep 5  # 给 GitHub 同步 tag 的时间
RUN_ID=$(gh run list --workflow=release.yml --event=push --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status
```

Expected: 4 个 job 全绿（build-ubuntu / build-macos-13 / build-macos-latest / merge）。

- [ ] **Step 4: 验证 GitHub Release**

```bash
gh release view v0.1.0 --json assets -q '.assets[].name'
```

Expected 输出包含：
```
gw_v0.1.0_linux_amd64.tar.gz
gw_v0.1.0_darwin_amd64.tar.gz
gw_v0.1.0_darwin_arm64.tar.gz
checksums.txt
```

- [ ] **Step 5: 下载 + 手验 version 注入**

```bash
mkdir -p /tmp/gw-v010-verify && cd /tmp/gw-v010-verify
gh release download v0.1.0 --pattern "*darwin_arm64*" --pattern "checksums.txt"
shasum -a 256 -c checksums.txt --ignore-missing
tar xzf gw_v0.1.0_darwin_arm64.tar.gz
./gw version
```

Expected：`./gw version` 输出含 `gw version v0.1.0 (commit <sha7>, built ..., go1.22.x)`。

---

## Task 10: README + CLAUDE.md docs 同步

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: README 加 Installation 章节**

在 README.md 现有 "特性" 节之前（或 README 顶部合理位置）加入：

```markdown
## Installation

### 下载预编译二进制（推荐）

从 [GitHub Releases](https://github.com/Anthoooooooony/gw/releases/latest) 下载对应平台 tar.gz，解压后把 `gw` 放入 `PATH`：

```bash
# macOS Apple Silicon
curl -L -o gw.tar.gz https://github.com/Anthoooooooony/gw/releases/latest/download/gw_v0.1.0_darwin_arm64.tar.gz
tar xzf gw.tar.gz
sudo mv gw /usr/local/bin/
gw version
```

平台覆盖：`linux_amd64` / `darwin_amd64` / `darwin_arm64`。Linux arm64 与 Windows 暂不提供二进制，可用 `go install` 自行构建。

### 从源码构建

```bash
go install github.com/gw-cli/gw@latest
```

注意：需要 CGO（go-sqlite3 依赖），本地需有 C 编译器（gcc / clang）。
```

- [ ] **Step 2: CLAUDE.md 加分支约定**

在 CLAUDE.md 的 "项目概览" 之后、"TOML 规则三级加载" 之前插入：

```markdown
## 分支约定

两干分支模型：

| 分支 | 角色 | PR base |
|------|------|---------|
| `master` | 已发布代码，每次 tag 对应一个 release | —— |
| `dev` | 集成分支，GitHub default branch | feature PR 默认落这里 |
| `feature/*` | 功能/修复 | base = `dev` |
| `hotfix/*` | 紧急修复已发布版本 | base = `master`，merge 后 cherry-pick 到 dev |

版本机制：SemVer + `scripts/bump.sh [patch|minor|major]`，详见 `docs/superpowers/specs/2026-04-17-versioning-git-workflow-design.md`。

**hotfix 顺序铁律**：先 PR 到 master，merge → bump patch → 再 cherry-pick 到 dev。顺序反了会丢修复。
```

- [ ] **Step 3: Commit + push（走 feature 分支）**

```bash
git checkout -b feature/docs-post-v010 master
# 做 README + CLAUDE.md 的改动
git add README.md CLAUDE.md
git commit -m "docs: Installation 章节 + CLAUDE.md 分支约定"
git push -u origin feature/docs-post-v010
```

- [ ] **Step 4（可选）: 开 PR 把 feature 分支合入 master**

docs 类改动可以留到下次 release 时随批次合入，或者立刻单独走：

```bash
gh pr create --base master --head feature/docs-post-v010 \
  --title "docs: v0.1.0 release 后文档同步" \
  --body "$(cat <<'EOF'
## Summary
- README 加 Installation 章节（GitHub Release 下载步骤）
- CLAUDE.md 记录 GitHub Flow 分支约定

## Test plan
- [x] CI 通过
EOF
)"
```

Expected: PR base=master，head=feature/docs-post-v010，CI 绿后合入并删除 feature 分支。

---

## Self-Review 检查清单（写完后我做一次）

- [ ] **Spec 覆盖**：对照 spec §6 Migration 的 9 步，每步都有对应 task
- [ ] **无占位符**：搜 "TBD"、"TODO"、"fill in"、"similar to" —— 全部替换为具体内容
- [ ] **类型/签名一致**：`classify_commit` / `bump_version` / `build_changelog_section` 在测试和实现里签名一致
- [ ] **命令可执行**：每个 `Expected` 下的命令都能不修改直接跑
- [ ] **TDD 节奏**：凡有代码的 task 都走"写测试→失败→实现→通过→commit"五步

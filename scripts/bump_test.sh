#!/usr/bin/env bash
# scripts/bump_test.sh — bump.sh 纯函数单元测试
set -euo pipefail
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
# shellcheck disable=SC2034  # BUMP_LIB_ONLY 在 bump.sh 里通过 ${BUMP_LIB_ONLY:-} 读
BUMP_LIB_ONLY=1
# shellcheck source=./bump.sh disable=SC1091
source "$(dirname "$0")/bump.sh"

# ========== parse_version ==========
assert_eq "0 1 2" "$(parse_version v0.1.2)" "parse_version v0.1.2"
assert_eq "1 0 0" "$(parse_version v1.0.0)" "parse_version v1.0.0"
assert_eq "0 0 0" "$(parse_version v0.0.0)" "parse_version v0.0.0"

# ========== parse_version pre-release ==========
assert_eq "0 1 2" "$(parse_version v0.1.2-rc.1)" "parse_version 剥离 -rc.N"
assert_eq "1 0 0" "$(parse_version v1.0.0-alpha.3)" "parse_version 剥离 -alpha.N"
assert_eq "0 2 0" "$(parse_version v0.2.0-beta.1)" "parse_version 剥离 -beta.N"

# ========== bump_version ==========
assert_eq "v0.1.3" "$(bump_version v0.1.2 patch)" "patch bump"
assert_eq "v0.2.0" "$(bump_version v0.1.2 minor)" "minor bump"
assert_eq "v1.0.0" "$(bump_version v0.1.2 major)" "major bump"
assert_eq "v0.1.3-rc.1" "$(bump_version v0.1.2 patch rc.1)" "patch + pre"

# ========== bump_version pre-release prev ==========
assert_eq "v0.1.3" "$(bump_version v0.1.2-rc.1 patch)" "rc 版本 prev → 正式 patch"
assert_eq "v0.2.0" "$(bump_version v0.1.2-rc.1 minor)" "rc 版本 prev → 正式 minor"
assert_eq "v1.0.0-rc.2" "$(bump_version v0.9.9-rc.1 major rc.2)" "rc 升级 major + 新 rc 标签"

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
# BREAKING CHANGE 覆盖 (subject `!`)——不管是什么前缀都归 Removed
assert_eq "Removed" "$(classify_commit 'feat!: BREAKING')" "feat! → Removed"
assert_eq "Removed" "$(classify_commit 'fix(api)!: BREAKING')" "fix(scope)! → Removed"

# BREAKING CHANGE footer 支持（#20）——前缀无 `!` 但 body 含 BREAKING CHANGE:
breaking_body='feat: add new endpoint

BREAKING CHANGE: drops /v1/legacy support'
assert_eq "Removed" "$(classify_commit "$breaking_body")" "BREAKING CHANGE footer → Removed"

breaking_hyphen='fix(api): some fix

BREAKING-CHANGE: renamed response field'
assert_eq "Removed" "$(classify_commit "$breaking_hyphen")" "BREAKING-CHANGE footer (连字符) → Removed"

# footer 必须在行首，中途提到不算
not_footer='feat: x

看 log 会发现 BREAKING CHANGE: foo 出现在段中某处'
assert_eq "Added" "$(classify_commit "$not_footer")" "body 行中段提到 BREAKING CHANGE 不算 footer"

# 大小写宽容（F11a）
assert_eq "Added"  "$(classify_commit 'Feat: xxx')"       "Feat → Added (case insensitive)"
assert_eq "Added"  "$(classify_commit 'FEAT: xxx')"       "FEAT → Added"
assert_eq "Fixed"  "$(classify_commit 'Fix(api): xxx')"   "Fix(scope) → Fixed"
assert_eq "Fixed"  "$(classify_commit 'FIX: 紧急修复')"    "FIX → Fixed"
assert_eq "Changed" "$(classify_commit 'Refactor: 重构')"  "Refactor → Changed"
assert_eq "Removed" "$(classify_commit 'FEAT!: BREAKING')" "FEAT! → Removed (case insensitive breaking)"

# ========== build_changelog_section ==========
# 入参现在是 NUL 分隔的 commit 记录，每条可含 body（支持 BREAKING CHANGE footer）
expected='## [v0.2.0] - 2026-04-17

### Added
- feat(filter): 新 toml 规则

### Changed
- refactor(cmd): 抽取

### Fixed
- fix(test): detached HEAD'

actual=$(printf '%s\0' \
  'feat(filter): 新 toml 规则' \
  'fix(test): detached HEAD' \
  'docs(readme): 同步' \
  'refactor(cmd): 抽取' \
  | build_changelog_section "v0.2.0" "2026-04-17")
assert_eq "$expected" "$actual" "build_changelog_section 基础分类"

# 空输入不输出空节
actual=$(printf '%s\0' 'docs: 只有文档改动' | build_changelog_section "v0.3.0" "2026-05-01")
expected='## [v0.3.0] - 2026-05-01

_无 notable 变更（仅文档/构建/测试）_'
assert_eq "$expected" "$actual" "build_changelog_section 仅忽略类"

# BREAKING CHANGE footer 集成到 changelog 节（#20）
expected='## [v1.0.0] - 2026-05-01

### Fixed
- fix: minor patch

### Removed
- feat: add endpoint'

actual=$(printf '%s\0' \
  'feat: add endpoint

BREAKING CHANGE: drops v1 support' \
  'fix: minor patch' \
  | build_changelog_section "v1.0.0" "2026-05-01")
assert_eq "$expected" "$actual" "build_changelog_section BREAKING CHANGE footer 归入 Removed"

# ========== integration: 幂等 tag 检查 (#15) ==========
# 预先打一个 bump 算出的 tag，dry-run 也必须拒绝（exit != 0）
integration_idempotent_tag() {
  local bump_script tmp_repo ec
  bump_script="$(cd "$(dirname "$0")" && pwd)/bump.sh"

  tmp_repo=$(mktemp -d)
  trap 'rm -rf "$tmp_repo"' RETURN

  (
    cd "$tmp_repo"
    git init -q -b master
    git config user.email "t@gw.local"
    git config user.name  "t"
    echo "# t" > README.md
    echo "# CHANGELOG" > CHANGELOG.md
    git add README.md CHANGELOG.md
    git commit -q -m "init"
    git tag v0.1.0
    git tag v0.1.1  # bump patch 会算出 v0.1.1，预先占位
    # 空远端：用 local bare 模拟 origin
    git init -q --bare "$tmp_repo/origin.git"
    git remote add origin "$tmp_repo/origin.git"
    git push -q origin master --tags
  ) >/dev/null 2>&1

  set +e
  (
    cd "$tmp_repo"
    EDITOR=true bash "$bump_script" patch --dry-run
  ) >/dev/null 2>&1
  ec=$?
  set -e

  if [[ $ec -eq 0 ]]; then
    FAIL=$((FAIL+1))
    echo "FAIL: 幂等 tag 检查——预期 exit != 0，实际 0"
  else
    PASS=$((PASS+1))
  fi
}
integration_idempotent_tag

echo "---"
echo "PASS: $PASS, FAIL: $FAIL"
[[ $FAIL -eq 0 ]]

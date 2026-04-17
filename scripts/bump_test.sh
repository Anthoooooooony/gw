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
# shellcheck source=./bump.sh
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
# BREAKING CHANGE 覆盖——不管是什么前缀都归 Removed
assert_eq "Removed" "$(classify_commit 'feat!: BREAKING')" "feat! → Removed"
assert_eq "Removed" "$(classify_commit 'fix(api)!: BREAKING')" "fix(scope)! → Removed"

# 大小写宽容（F11a）
assert_eq "Added"  "$(classify_commit 'Feat: xxx')"       "Feat → Added (case insensitive)"
assert_eq "Added"  "$(classify_commit 'FEAT: xxx')"       "FEAT → Added"
assert_eq "Fixed"  "$(classify_commit 'Fix(api): xxx')"   "Fix(scope) → Fixed"
assert_eq "Fixed"  "$(classify_commit 'FIX: 紧急修复')"    "FIX → Fixed"
assert_eq "Changed" "$(classify_commit 'Refactor: 重构')"  "Refactor → Changed"
assert_eq "Removed" "$(classify_commit 'FEAT!: BREAKING')" "FEAT! → Removed (case insensitive breaking)"

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
- fix(test): detached HEAD'

actual=$(build_changelog_section "v0.2.0" "2026-04-17" <<<"$input")
assert_eq "$expected" "$actual" "build_changelog_section 基础分类"

# 空输入不输出空节
actual=$(build_changelog_section "v0.3.0" "2026-05-01" <<<"docs: 只有文档改动")
expected='## [v0.3.0] - 2026-05-01

_无 notable 变更（仅文档/构建/测试）_'
assert_eq "$expected" "$actual" "build_changelog_section 仅忽略类"

echo "---"
echo "PASS: $PASS, FAIL: $FAIL"
[[ $FAIL -eq 0 ]]

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

echo "---"
echo "PASS: $PASS, FAIL: $FAIL"
[[ $FAIL -eq 0 ]]

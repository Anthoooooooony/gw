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

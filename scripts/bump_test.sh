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

# (#46) 模拟 git log 真实输出：NUL 分隔符之间有换行，不能吞 subject
# git log --format='%s%n%b%x00' 的输出是 "<c1>\0\n<c2>\0\n...<cN>\0"，
# 第 2 条起的 record 在 read -d '' 后会以 \n 开头。修复前第 2..N 条会被吞。
expected='## [v0.4.0] - 2026-05-02

### Added
- feat: first
- feat: second

### Fixed
- fix: third'

actual=$(printf 'feat: first\n\0\nfeat: second\n\0\nfix: third\n\0' \
  | build_changelog_section "v0.4.0" "2026-05-02")
assert_eq "$expected" "$actual" "NUL 流带前导 \\n 的 commit 全部被分类 (#46 回归)"

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
    origin_dir=$(mktemp -d); git init -q --bare "$origin_dir/origin.git"
    git remote add origin "$origin_dir/origin.git"
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

# ========== integration: CHANGELOG 缺 [Unreleased] 节应拒绝 (#60) ==========
# CHANGELOG.md 存在但无 `## [Unreleased]` 节时，bump 必须 exit != 0，
# 避免 awk 找不到插入锚点而静默丢失 changelog 节。
integration_missing_unreleased_rejected() {
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
    # 故意只留标题，不含 ## [Unreleased] 节
    printf '# CHANGELOG\n\n## [v0.1.0] - 2026-01-01\n\n### Added\n- initial\n' > CHANGELOG.md
    git add README.md CHANGELOG.md
    git commit -q -m "init"
    git tag v0.1.0
    origin_dir=$(mktemp -d); git init -q --bare "$origin_dir/origin.git"
    git remote add origin "$origin_dir/origin.git"
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
    echo "FAIL: Unreleased 缺失检查——预期 exit != 0，实际 0"
  else
    PASS=$((PASS+1))
  fi
}
integration_missing_unreleased_rejected

# ========== integration: [Unreleased] 手工内容迁移到新版本节 ==========
# 有真实内容时走 migration 路径：body 移到新版本节，Unreleased 复位为空子节骨架。
integration_unreleased_migrates_to_new_version() {
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
    cat > CHANGELOG.md <<'EOF'
# Changelog

## [Unreleased]

### Added
- feat: 手工合并的条目（migration 应迁移这行到新版本节）

### Changed

### Fixed

### Removed

[Unreleased]: https://example.com/compare/v0.1.0...HEAD

## [v0.1.0] - 2026-01-01

### Added
- initial
EOF
    git add README.md CHANGELOG.md
    git commit -q -m "init"
    git tag v0.1.0
    # 再加一个 commit，确保 build_changelog_section 会生成 auto-gen 内容（用于对比）
    echo "x" > dummy.txt
    git add dummy.txt
    git commit -q -m "feat: dummy from commit subject"
    origin_dir=$(mktemp -d); git init -q --bare "$origin_dir/origin.git"
    git remote add origin "$origin_dir/origin.git"
    git push -q origin master --tags
  ) >/dev/null 2>&1

  set +e
  (
    cd "$tmp_repo"
    EDITOR=true bash "$bump_script" patch
  ) >/dev/null 2>&1
  ec=$?
  set -e

  local changelog
  changelog=$(cd "$tmp_repo" && cat CHANGELOG.md)

  if [[ $ec -ne 0 ]]; then
    FAIL=$((FAIL+1))
    echo "FAIL: migration——bump exit=$ec"
  elif ! grep -q "手工合并的条目" <<<"$changelog"; then
    FAIL=$((FAIL+1))
    echo "FAIL: migration——原手工条目丢失"
  elif ! awk '/## \[v0\.1\.1\]/{f=1} /## \[v0\.1\.0\]/{exit} f && /手工合并的条目/{found=1} END{exit !found}' <<<"$changelog"; then
    FAIL=$((FAIL+1))
    echo "FAIL: migration——手工条目不在新版本节 v0.1.1 下"
  elif awk '/## \[Unreleased\]/{f=1; next} /## \[v/{exit} f && /手工合并的条目/{found=1} END{exit !found}' <<<"$changelog"; then
    FAIL=$((FAIL+1))
    echo "FAIL: migration——手工条目仍残留在 [Unreleased] 下，没搬走"
  elif grep -q "feat: dummy from commit subject" <<<"$changelog"; then
    FAIL=$((FAIL+1))
    echo "FAIL: migration——auto-gen 内容不应出现（migration 模式应替换 auto-gen）"
  elif ! awk '/## \[Unreleased\]/{f=1; next} /## \[v/{exit} f && /^### Added$/{found=1} END{exit !found}' <<<"$changelog"; then
    FAIL=$((FAIL+1))
    echo "FAIL: migration——Unreleased 未复位为骨架（缺 ### Added stub）"
  else
    PASS=$((PASS+1))
  fi
}
integration_unreleased_migrates_to_new_version

# ========== integration: [Unreleased] 空（仅裸子节头）走 auto-gen fallback ==========
integration_unreleased_empty_uses_autogen() {
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
    cat > CHANGELOG.md <<'EOF'
# Changelog

## [Unreleased]

### Added

### Changed

### Fixed

### Removed

[Unreleased]: https://example.com/compare/v0.1.0...HEAD

## [v0.1.0] - 2026-01-01

### Added
- initial
EOF
    git add README.md CHANGELOG.md
    git commit -q -m "init"
    git tag v0.1.0
    echo "x" > dummy.txt
    git add dummy.txt
    git commit -q -m "feat: fallback-triggering-commit"
    origin_dir=$(mktemp -d); git init -q --bare "$origin_dir/origin.git"
    git remote add origin "$origin_dir/origin.git"
    git push -q origin master --tags
  ) >/dev/null 2>&1

  set +e
  (
    cd "$tmp_repo"
    EDITOR=true bash "$bump_script" patch
  ) >/dev/null 2>&1
  ec=$?
  set -e

  local changelog
  changelog=$(cd "$tmp_repo" && cat CHANGELOG.md)

  if [[ $ec -ne 0 ]]; then
    FAIL=$((FAIL+1))
    echo "FAIL: auto-gen fallback——bump exit=$ec"
  elif ! grep -q "fallback-triggering-commit" <<<"$changelog"; then
    FAIL=$((FAIL+1))
    echo "FAIL: auto-gen fallback——commit subject 应被写入新版本节"
  else
    PASS=$((PASS+1))
  fi
}
integration_unreleased_empty_uses_autogen

# ========== integration: trap 在 main 外展开不炸 unbound（tmp: unbound variable 回归） ==========
# 直接 eval trap 字符串，期望无 stderr 输出和 exit 0（默认空展开生效）。
integration_trap_safe_with_unset_vars() {
  local out
  set +e
  # 模拟 main 已退出、所有 local 已脱离作用域的场景，
  # 直接在纯净 bash 子进程里执行 trap 实际展开的 rm 命令。
  out=$(bash -c '
    set -euo pipefail
    rm -f "${tmp:-}" "${block_file:-}" "${tmp2:-}" "${skeleton_file:-}" "${section_file:-}"
    echo ok
  ' 2>&1)
  local ec=$?
  set -e

  if [[ $ec -ne 0 ]]; then
    FAIL=$((FAIL+1))
    echo "FAIL: trap 清理——exit=$ec, output=$out"
  elif [[ "$out" == *"unbound variable"* ]]; then
    FAIL=$((FAIL+1))
    echo "FAIL: trap 清理——仍触发 unbound variable: $out"
  else
    PASS=$((PASS+1))
  fi
}
integration_trap_safe_with_unset_vars

echo "---"
echo "PASS: $PASS, FAIL: $FAIL"
[[ $FAIL -eq 0 ]]

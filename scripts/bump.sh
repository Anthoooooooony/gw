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
      [[ $has_any -eq 1 ]] && out+=$'\n'
      out+="### $section"$'\n'"${buckets[$section]}"
      has_any=1
    fi
  done
  if [[ $has_any -eq 0 ]]; then
    out+="_无 notable 变更（仅文档/构建/测试）_"$'\n'
  fi
  printf '%s' "$out"
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

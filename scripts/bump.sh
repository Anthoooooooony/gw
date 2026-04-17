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
  # 用 awk 在 "## [Unreleased]" 节结束（下一个 "## [" 之前）插入新节。
  # 通过文件读取 block 避免 BSD awk 对含换行的 -v 值报错（GNU awk 无此限制）。
  local tmp block_file
  tmp=$(mktemp)
  block_file=$(mktemp)
  printf '%s\n' "$changelog_section" > "$block_file"
  awk -v block_file="$block_file" '
    BEGIN {
      while ((getline line < block_file) > 0) block = block line "\n"
      close(block_file)
    }
    /^## \[Unreleased\]/ { print; in_unreleased=1; next }
    in_unreleased && /^## \[/ { printf "%s", block; in_unreleased=0 }
    { print }
    END { if (in_unreleased) printf "\n%s", block }
  ' CHANGELOG.md > "$tmp"
  mv "$tmp" CHANGELOG.md
  rm -f "$block_file"

  # 6. 让维护者审 CHANGELOG
  ${EDITOR:-vi} CHANGELOG.md

  # 7. commit + tag + push
  git add CHANGELOG.md
  git commit -m "chore(release): $new_tag"
  git tag -a "$new_tag" -m "release: $new_tag"
  git push origin master "$new_tag"

  echo "gw bump: 已推送 ${new_tag}，release.yml 将由 tag push 触发"
}

# 仅当被直接执行、非 source 时调用 main
if [[ -z "${BUMP_LIB_ONLY:-}" ]]; then
  main "$@"
fi

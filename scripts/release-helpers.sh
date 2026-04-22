#!/usr/bin/env bash
# scripts/release-helpers.sh — CI-driven release 辅助函数
#
# 供 .github/workflows/release.yml source 使用，避免把复杂 shell 逻辑挤进 YAML。
# 纯函数：不读写文件、不碰 git、可独立 source。参数通过位置传入，结果写 stdout。
#
# 分类规则与 CONTRIBUTING.md 的 PR title 约束对齐：
#   feat → Features（触发 minor，v0.x 阶段）
#   fix → Bug Fixes（patch）
#   perf → Performance Improvements（patch）
#   refactor → Code Refactoring（patch）
#   revert → Reverts（patch）
#   deps → Dependencies（patch）
#   feat!:/fix!:/... 或 body 带 BREAKING CHANGE → BREAKING（v0.x 仍 minor；进 1.0 后 major）
#   docs/chore/ci/test/style/build → "" 表示不触发 release 且不入 notes

set -euo pipefail

# classify_commit <full-commit-msg> → "Features | Bug Fixes | Performance Improvements |
#   Code Refactoring | Reverts | Dependencies | BREAKING | ''"
# 入参可以是单行 subject 或 subject + body 多行。识别不区分大小写。
classify_commit() {
  local msg="$1"
  local subject="${msg%%$'\n'*}"
  local restore_nocasematch
  restore_nocasematch=$(shopt -p nocasematch)
  shopt -s nocasematch

  local nl result=""
  nl=$'\n'
  if [[ "$subject" =~ ^[a-z]+(\([^\)]+\))?!: ]]; then
    result="BREAKING"
  elif [[ "${nl}${msg}" =~ ${nl}BREAKING[[:space:]-]CHANGE: ]]; then
    result="BREAKING"
  else
    case "$subject" in
      feat\(*\):*|feat:*)         result="Features" ;;
      fix\(*\):*|fix:*)           result="Bug Fixes" ;;
      perf\(*\):*|perf:*)         result="Performance Improvements" ;;
      refactor\(*\):*|refactor:*) result="Code Refactoring" ;;
      revert\(*\):*|revert:*)     result="Reverts" ;;
      deps\(*\):*|deps:*)         result="Dependencies" ;;
      *) result="" ;;
    esac
  fi

  eval "$restore_nocasematch"
  printf '%s' "$result"
}

# kind_from_classifications <cls1> <cls2> ... → "minor | patch | none"
# v0.x 阶段：BREAKING / Features → minor；Bug Fixes / Perf / Refactor / Reverts / Deps → patch；
# 其他（空字符串）→ none。任一条命中 minor 则整体为 minor；否则有命中即 patch。
kind_from_classifications() {
  local cls has_visible=0 has_minor=0
  for cls in "$@"; do
    case "$cls" in
      BREAKING|Features) has_minor=1; has_visible=1 ;;
      "Bug Fixes"|"Performance Improvements"|"Code Refactoring"|Reverts|Dependencies) has_visible=1 ;;
    esac
  done
  if [[ $has_minor -eq 1 ]]; then
    printf 'minor'
  elif [[ $has_visible -eq 1 ]]; then
    printf 'patch'
  else
    printf 'none'
  fi
}

# parse_version v0.1.2 → "0 1 2"（剥离 -pre 后缀）
parse_version() {
  local v="${1#v}"
  v="${v%%-*}"
  IFS=. read -r major minor patch <<< "$v"
  printf '%s %s %s' "$major" "$minor" "$patch"
}

# bump_version <current-tag> <patch|minor|major> → vX.Y.Z
bump_version() {
  local curr="$1" kind="$2" major minor patch
  read -r major minor patch <<< "$(parse_version "$curr")"
  case "$kind" in
    patch) patch=$((patch+1)) ;;
    minor) minor=$((minor+1)); patch=0 ;;
    major) major=$((major+1)); minor=0; patch=0 ;;
    *) echo "bump_version: unknown kind: $kind" >&2; return 1 ;;
  esac
  printf 'v%d.%d.%d' "$major" "$minor" "$patch"
}

# build_release_notes <new-tag> <prev-tag> <repo-slug> < NUL 分隔的 (subject + body + short_sha) 三元组
# 每条记录格式：subject\n<optional body>\x1F<short_sha>\x00
# （\x1F 作为 body / sha 的分隔；git log --format='%s%n%b%x1F%h%x00' 提供）
# 输出 markdown 到 stdout：按 BREAKING / Features / Bug Fixes / ... 分节，每条带 short_sha 链接。
build_release_notes() {
  local new_tag="$1" prev_tag="$2" repo="$3"
  declare -A bucket=(
    [BREAKING]=""
    [Features]=""
    ["Bug Fixes"]=""
    ["Performance Improvements"]=""
    ["Code Refactoring"]=""
    [Reverts]=""
    [Dependencies]=""
  )
  local record msg sha subject cls
  while IFS= read -r -d '' record; do
    # 每条 record 以 \n 开头（从第二条起），剥除前导换行
    while [[ "$record" == $'\n'* ]]; do record="${record#$'\n'}"; done
    [[ -z "$record" ]] && continue
    msg="${record%$'\x1F'*}"
    sha="${record##*$'\x1F'}"
    subject="${msg%%$'\n'*}"
    [[ -z "$subject" ]] && continue
    cls=$(classify_commit "$msg")
    [[ -z "$cls" ]] && continue
    bucket[$cls]+="- ${subject} ([${sha}](https://github.com/${repo}/commit/${sha}))"$'\n'
  done

  local section
  printf '## [%s](https://github.com/%s/compare/%s...%s)\n\n' \
    "${new_tag#v}" "$repo" "$prev_tag" "$new_tag"
  for section in BREAKING Features "Bug Fixes" "Performance Improvements" "Code Refactoring" Reverts Dependencies; do
    if [[ -n "${bucket[$section]}" ]]; then
      printf '### %s\n\n%s\n' "$section" "${bucket[$section]}"
    fi
  done
}

# 仅当被直接执行、非 source 时报错引导（脚本本体只暴露函数，不可当命令跑）
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "release-helpers.sh 设计为被 source，不可直接执行。" >&2
  exit 2
fi

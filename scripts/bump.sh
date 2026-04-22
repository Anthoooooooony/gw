#!/usr/bin/env bash
# scripts/bump.sh — gw 版本 bump 工具
# 用法：./scripts/bump.sh [patch|minor|major] [--pre LABEL] [--dry-run]
set -euo pipefail

# ========== 纯函数（供测试 source）==========

# parse_version v0.1.2 → "0 1 2"（剥离 -rc.N / -alpha.N / -beta.N 等 pre-release 后缀）
parse_version() {
  local v="${1#v}"
  v="${v%%-*}"   # 剥离 -rc.N / -alpha.N / -beta.N 等 pre-release 后缀
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

# classify_commit <full-commit-msg> → "Added" | "Fixed" | "Changed" | "Removed" | ""
# 空字符串表示该 commit 不入 CHANGELOG（docs/chore/ci/test）
# 入参可以是单行 subject，也可以是 subject + body 多行文本。
# 识别规则优先级：
#   1) 首行前缀带 ! （`feat!:` / `fix(api)!:`）→ Removed
#   2) body 任一行匹配 `^BREAKING CHANGE:` 或 `^BREAKING-CHANGE:` footer → Removed
#   3) 首行前缀：feat→Added / fix→Fixed / refactor|perf→Changed / remove→Removed
# 大小写宽容（Feat: / FEAT: / Fix(api): 均有效）
classify_commit() {
  local msg="$1"
  local subject="${msg%%$'\n'*}"

  local restore_nocasematch
  restore_nocasematch=$(shopt -p nocasematch)
  shopt -s nocasematch

  # nl 持有换行符，避免 shellcheck 把正则中的 $'\n' 里的 ' 误判为字符串终结 (SC1011)
  local nl result=""
  nl=$'\n'
  if [[ "$subject" =~ ^[a-z]+(\([^\)]+\))?!: ]]; then
    result="Removed"
  elif [[ "${nl}${msg}" =~ ${nl}BREAKING[[:space:]-]CHANGE: ]]; then
    result="Removed"
  else
    case "$subject" in
      feat\(*\):*|feat:*)                 result="Added" ;;
      fix\(*\):*|fix:*)                   result="Fixed" ;;
      refactor\(*\):*|refactor:*|perf\(*\):*|perf:*) result="Changed" ;;
      remove\(*\):*|remove:*)             result="Removed" ;;
      *) result="" ;;
    esac
  fi

  eval "$restore_nocasematch"
  echo "$result"
}

# extract_unreleased_body FILE → stdout
# 打印 CHANGELOG.md 里 `## [Unreleased]` 与下一个 `## [` 之间的正文，
# 跳过 Markdown 引用式链接定义行（`^[xxx]: http...`），它们属于全局链接区，
# 不应该迁移进版本节。保留空行与子节头原样。
extract_unreleased_body() {
  local file="$1"
  awk '
    /^## \[Unreleased\]/ { in_unreleased=1; next }
    in_unreleased && /^## \[/ { exit }
    in_unreleased && /^\[[^]]+\]:/ { next }
    in_unreleased { print }
  ' "$file"
}

# unreleased_body_is_empty FILE → exit 0 如果 body 只含空行 + 空的子节头
# （"空的子节头" 指单独一行 `### Added` 之类，下面没有条目的情况）
# exit 1 如果 body 里有任何实际条目。
unreleased_body_is_empty() {
  local body
  body=$(extract_unreleased_body "$1")
  # 剥掉空行和形如 `### Added` 的裸子节头，剩下什么就说明有真内容
  local leftover
  leftover=$(printf '%s' "$body" | awk '
    NF == 0 { next }
    /^### (Added|Changed|Fixed|Removed)$/ { next }
    { print }
  ')
  [[ -z "$leftover" ]]
}

# build_changelog_section VERSION DATE < stdin（NUL 分隔的 commit 记录，每条含 subject + 可选 body）
# → markdown 节：按 Added / Changed / Fixed / Removed 四节输出；无归类内容时输出"无 notable 变更"
# 使用 NUL 分隔是因为 commit body 可能跨多行，无法用换行分隔。
build_changelog_section() {
  local version="$1" date="$2"
  declare -A buckets=([Added]="" [Changed]="" [Fixed]="" [Removed]="")
  local msg subject cat
  while IFS= read -r -d '' msg; do
    # git log --format='%s%n%b%x00' 在两条 NUL 记录之间会插一个换行符，
    # 第 2..N 条 read 到的 msg 以 $'\n' 开头。不剥掉的话 ${msg%%$'\n'*}
    # 会截出空字符串，subject 为空被 continue 跳过，整条 commit 静默丢失。
    # 前导空行也一并清掉以免 body 意外多空行（对分类语义无影响）。
    while [[ "$msg" == $'\n'* ]]; do msg="${msg#$'\n'}"; done
    [[ -z "$msg" ]] && continue
    subject="${msg%%$'\n'*}"
    [[ -z "$subject" ]] && continue
    cat=$(classify_commit "$msg")
    [[ -z "$cat" ]] && continue
    buckets[$cat]+="- $subject"$'\n'
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
  # 统一 trap 清理：main 里所有 mktemp 产物，任一中途 set -e 退出都不残留 /tmp。
  # 允许空值（rm -f "" 是 no-op）。
  local tmp="" block_file="" tmp2="" skeleton_file="" section_file=""
  # `${var:-}` 默认空值展开：main 返回后本地变量已脱离作用域，
  # set -u 下直接 `"$var"` 会报 unbound；默认空使 trap 兼容 "未初始化" 和 "已清理" 两种状态。
  trap 'rm -f "${tmp:-}" "${block_file:-}" "${tmp2:-}" "${skeleton_file:-}" "${section_file:-}"' EXIT

  while [[ $# -gt 0 ]]; do
    case "$1" in
      patch|minor|major) kind="$1"; shift ;;
      --pre)
        [[ $# -lt 2 ]] && { echo "gw bump: --pre 需要一个 LABEL 参数（如 rc.1）" >&2; usage; exit 2; }
        pre="$2"; shift 2
        ;;
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

  # 3b. 幂等性：新 tag 必须尚未存在（本地 + 远端），否则 push 阶段会炸并留脏 tag
  if git rev-parse -q --verify "refs/tags/${new_tag}" >/dev/null; then
    echo "gw bump: tag ${new_tag} 已存在于本地，先 git tag -d ${new_tag} 或选别的版本" >&2
    exit 1
  fi
  if git ls-remote --exit-code --tags origin "refs/tags/${new_tag}" >/dev/null 2>&1; then
    echo "gw bump: tag ${new_tag} 已存在于远端 origin" >&2
    exit 1
  fi

  # 4. 生成 CHANGELOG 节
  # 用 %s%n%b%x00：subject + body + NUL 分隔。body 里可能有 BREAKING CHANGE footer
  local log_range changelog_section
  if [[ "$prev_tag" == "v0.0.0" ]]; then
    log_range="HEAD"
  else
    log_range="${prev_tag}..HEAD"
  fi
  changelog_section=$(git log --format='%s%n%b%x00' "$log_range" |
    build_changelog_section "$new_tag" "$date")

  # 5. CHANGELOG 预检：dry-run / 真实写入都必须通过，防止进入到写入阶段才静默丢内容。
  if [[ ! -f CHANGELOG.md ]]; then
    echo "gw bump: CHANGELOG.md 不存在" >&2
    exit 1
  fi
  # `## [Unreleased]` 节是 awk 插入锚点；缺失时插入会失败但不报错，changelog 被吞。
  if ! grep -q '^## \[Unreleased\]' CHANGELOG.md; then
    echo "gw bump: CHANGELOG.md 缺少 '## [Unreleased]' 节，无法定位插入点" >&2
    exit 1
  fi

  # 4b. 判断新版本节使用哪个来源：
  #   - migration：[Unreleased] 手工编辑过（有真内容） → 把 body 搬进新版本节，Unreleased 复位为空子节
  #   - auto-gen：Unreleased body 空（或仅裸子节头） → 用 commit-subject 自动生成节作为 fallback
  # 这对应 CONTRIBUTING.md 里"多个 commit 合并为单一 user-facing feature 时手工编辑 [Unreleased]"的约定。
  local migrate=0 unreleased_body=""
  if ! unreleased_body_is_empty CHANGELOG.md; then
    migrate=1
    # `$(...)` 会吞尾部所有换行，body 末尾的空行（用于和后续章节分隔）会因此丢失。
    # 经典解决：追加 sentinel 字符 X 再用 ${var%X} 剥掉，换行原样保留。
    unreleased_body=$(extract_unreleased_body CHANGELOG.md; echo X)
    unreleased_body="${unreleased_body%X}"
  fi

  # 为 migration 模式预先构造新版本节内容（header + 空行 + body + 尾部空行）。
  # 结构：`## [vX] - DATE\n` + body + `\n` 确保与下一个 `## [` 有一个空行分隔。
  local migrated_section=""
  if [[ $migrate -eq 1 ]]; then
    migrated_section="## [${new_tag}] - ${date}"$'\n'"${unreleased_body}"
    # 归一化为"恰好以 \n\n 结尾"：剥掉所有尾部 \n 后补两个
    while [[ "$migrated_section" == *$'\n' ]]; do
      migrated_section="${migrated_section%$'\n'}"
    done
    migrated_section+=$'\n\n'
  fi

  if [[ $dry_run -eq 1 ]]; then
    if [[ $migrate -eq 1 ]]; then
      echo "--- 预期 CHANGELOG 新增 (migration：使用 [Unreleased] 手工内容) ---"
      printf '%s' "$migrated_section"
    else
      echo "--- 预期 CHANGELOG 新增 (auto-gen fallback) ---"
      printf '%s\n' "$changelog_section"
    fi
    echo "--- 预期 tag：$new_tag ---"
    echo "(dry-run：不 commit / 不 tag / 不 push)"
    exit 0
  fi

  # 5. 用 awk 重写 CHANGELOG.md：
  #   migration：[Unreleased] 头保留，body 换成空子节骨架，新版本节插在 body 之后（即原 body 位置之下）
  #   auto-gen：完全沿用旧行为，把 commit-subject 生成节插在 [Unreleased] 与上一个 "## [" 之间
  # 通过文件读取多行值避免 BSD awk 的 -v 不支持换行符限制。
  tmp=$(mktemp)
  skeleton_file=$(mktemp)
  # 空骨架：下一个 dev cycle 可以直接往对应子节填。
  printf '\n### Added\n\n### Changed\n\n### Fixed\n\n### Removed\n\n' > "$skeleton_file"

  section_file=$(mktemp)
  if [[ $migrate -eq 1 ]]; then
    printf '%s' "$migrated_section" > "$section_file"
    awk -v skel_file="$skeleton_file" -v sec_file="$section_file" '
      BEGIN {
        while ((getline line < skel_file) > 0) skeleton = skeleton line "\n"
        close(skel_file)
        while ((getline line < sec_file) > 0) section = section line "\n"
        close(sec_file)
      }
      /^## \[Unreleased\]/ {
        print
        printf "%s", skeleton
        in_unreleased=1
        next
      }
      in_unreleased && /^\[[^]]+\]:/ { print; next }     # 链接定义行原样透传
      in_unreleased && /^## \[/ {
        # section 已预拼"\n\n"结尾，这里再补一个前导 \n 保证和前文（skeleton 或 link 定义）隔一个空行
        printf "\n%s", section
        in_unreleased=0
        # fall through to print this "## [" line
      }
      in_unreleased { next }                              # 旧 body 行全部吞掉（已换成 skeleton）
      { print }
      END { if (in_unreleased) printf "\n%s", section }   # 文件末尾也没有旧版本节的兜底
    ' CHANGELOG.md > "$tmp"
  else
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
  fi
  mv "$tmp" CHANGELOG.md

  # 5b. 维护 CHANGELOG.md 底部链接定义区
  # 更新 [Unreleased]: compare/<prev>...HEAD 为 compare/<new>...HEAD
  tmp2=$(mktemp)
  awk -v new="$new_tag" '
    /^\[Unreleased\]:/ {
      sub(/compare\/.*\.\.\.HEAD|compare\/HEAD$/, "compare/" new "...HEAD")
      print
      next
    }
    { print }
  ' CHANGELOG.md > "$tmp2"
  mv "$tmp2" CHANGELOG.md

  # 在文件末尾追加 [new_tag]: releases/tag/new_tag
  # 先确保最后一行以 \n 结尾，再补一个空行作为与前文的分隔，最后写链接。
  [[ "$(tail -c1 CHANGELOG.md)" != $'\n' ]] && printf '\n' >> CHANGELOG.md
  printf '[%s]: https://github.com/Anthoooooooony/gw/releases/tag/%s\n' "$new_tag" "$new_tag" >> CHANGELOG.md

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

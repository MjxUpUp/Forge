#!/bin/bash
# route-match.sh — 跨 agent 共享的 skill 路由匹配引擎
#
# 输入：prompt 文本（stdin 或 $1）
# 输出（stdout）：命中 → "SKILL_NAME|reason"；未命中 → 空 + exit 1
#
# 路由表（单一真相源，优先级）：
#   $ROUTES_FILE → $FORGE_SKILLS_CANONICAL/skill-routing/routes.json → ~/.forge/skills-cache/embedded/skill-routing/routes.json → ~/.forge/skill-routes.json
#
# 各 agent 适配层调用本脚本做匹配：
#   - Claude Code UserPromptSubmit hook
#   - Cursor / Codex（通过命令调用）
#
# 设计：匹配逻辑只维护一份，路由表只存一份，各 agent 只做薄适配。

set -uo pipefail

resolve_routes_file() {
  if [ -n "${ROUTES_FILE:-}" ] && [ -f "${ROUTES_FILE:-}" ]; then
    echo "${ROUTES_FILE}"; return 0
  fi
  local home="${USERPROFILE:-${HOME:-}}"
  for cand in \
    "${FORGE_SKILLS_CANONICAL:-}/skill-routing/routes.json" \
    "$home/.forge/skills-cache/embedded/skill-routing/routes.json" \
    "$home/.forge/skill-routes.json"; do
    [ -f "$cand" ] && { echo "$cand"; return 0; }
  done
  return 1
}

ROUTES=$(resolve_routes_file) || exit 1

PROMPT="${1:-}"
[ -z "$PROMPT" ] && PROMPT=$(cat)
[ -z "$PROMPT" ] && exit 1

TRIMMED="${PROMPT#"${PROMPT%%[![:space:]]*}"}"
case "$TRIMMED" in /*) exit 1 ;; esac
[ ${#TRIMMED} -lt 2 ] && exit 1

LOWER=$(printf '%s' "$PROMPT" | tr '[:upper:]' '[:lower:]')

# 提取 skill<TAB>reason<TAB>keyword 三元组到数组，避免管道子 shell 退出码丢失
mapfile -t ENTRIES < <(
  jq -r '
    .[] | select(.match != null and (.match | type == "array") and .skill)
    | .skill as $s | (.reason // "") as $r
    | .match[] | select(. != null) | [$s, $r, .] | @tsv
  ' "$ROUTES" 2>/dev/null
)

for entry in "${ENTRIES[@]}"; do
  # 剥离可能存在的 \r（Windows/git-bash 下 jq @tsv 输出带 CRLF）
  entry="${entry%$'\r'}"
  IFS=$'\t' read -r skill reason kw <<< "$entry"
  [ -z "$kw" ] && continue
  kw_lower=$(printf '%s' "$kw" | tr '[:upper:]' '[:lower:]')
  if [[ "$LOWER" == *"$kw_lower"* ]]; then
    printf '%s|%s' "$skill" "$reason"
    exit 0
  fi
done
exit 1

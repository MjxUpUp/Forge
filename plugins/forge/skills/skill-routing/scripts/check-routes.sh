#!/bin/bash
# check-routes.sh — canonical 路由表发布守卫
#
# 校验 routes.json 引用的每个 skill 都存在于 skills/ 目录（含 SKILL.md），
# 防止把指向未分发 skill 的路由（死路由）发布进 canonical。
# 个人/环境专属路由（如 lark-*）应放用户级覆盖层 ~/.forge/skill-routes.json，
# 本脚本只校验 canonical，不校验覆盖层。
#
# 用法：
#   bash scripts/check-routes.sh                 # 校验默认 <skills-root>/skill-routing/routes.json
#   ROUTES_FILE=/path/to/routes.json bash scripts/check-routes.sh
#
# 退出码：0 = 全部引用存在；1 = 有死路由或环境缺 jq/路由表

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILLS_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROUTES="${ROUTES_FILE:-$SKILLS_ROOT/skill-routing/routes.json}"

[ -f "$ROUTES" ] || { echo "check-routes: 路由表不存在: $ROUTES" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "check-routes: 需要 jq" >&2; exit 1; }

mapfile -t SKILLS_REF < <(
  jq -r '.[] | select(.match != null and (.match | type == "array") and .skill) | .skill' "$ROUTES" 2>/dev/null | sort -u
)

[ ${#SKILLS_REF[@]} -gt 0 ] || { echo "check-routes: 路由表无有效条目: $ROUTES" >&2; exit 1; }

missing=0
for skill in "${SKILLS_REF[@]}"; do
  skill="${skill%$'\r'}"
  [ -z "$skill" ] && continue
  if [ ! -f "$SKILLS_ROOT/$skill/SKILL.md" ]; then
    echo "check-routes: 死路由 — routes.json 引用 '$skill'，但 $SKILLS_ROOT/$skill/SKILL.md 不存在" >&2
    missing=1
  fi
done

if [ "$missing" -ne 0 ]; then
  echo "check-routes: FAILED — 未分发的 skill 路由请移到覆盖层（examples/personal-overlay.example.json）" >&2
  exit 1
fi

echo "check-routes: OK — ${#SKILLS_REF[@]} 个被引用 skill 全部存在于 $SKILLS_ROOT"

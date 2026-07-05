#!/bin/bash
# skill-router-claude.sh — Claude Code UserPromptSubmit hook
#
# Claude Code 不能改写用户 prompt（只能 block 或 additionalContext 注入）。
# 所以本 hook 用 additionalContext 注入"路由提示"：命中关键词时，
# 告诉 Claude 这个输入应该走哪个 skill，要求它先 read SKILL.md 再处理。
#
# 这是 Claude 能做到的"最强软强制"（比纯 AGENTS.md 文字强，因为是针对性命中注入）。
#
# 配置位置：~/.claude/settings.json → hooks → UserPromptSubmit
#
# 输入（stdin）：Claude Code UserPromptSubmit 事件 JSON，含 .prompt 字段
# 输出（stdout）：JSON {"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"..."}}
#                未命中 → exit 0 无输出（放行）
#
# 路由匹配：调用共享引擎 route-match.sh（候选链见下方 MATCH 解析）

set -uo pipefail

# 路由表真相源（优先级）：$FORGE_SKILLS_CANONICAL（开发者显式覆盖）> embedded 缓存(跨机器) > pi 路径
MATCH=""
ROUTES_FILE=""
for cand in \
  "${FORGE_SKILLS_CANONICAL:-}/skill-routing/routes.json" \
  "${USERPROFILE:-$HOME}/.forge/skills-cache/embedded/skill-routing/routes.json" \
  "${USERPROFILE:-$HOME}/.forge/skill-routes.json"; do
  if [ -f "$cand" ]; then ROUTES_FILE="$cand"; break; fi
done
# 匹配引擎同优先级链
for cand in \
  "${FORGE_SKILLS_CANONICAL:-}/skill-routing/scripts/route-match.sh" \
  "${USERPROFILE:-$HOME}/.forge/skills-cache/embedded/skill-routing/scripts/route-match.sh"; do
  if [ -f "$cand" ]; then MATCH="$cand"; break; fi
done

# 无引擎则放行（不阻断用户）
[ -z "$MATCH" ] && exit 0

# 读 Claude 事件 JSON
INPUT=$(cat)
PROMPT=$(printf '%s' "$INPUT" | jq -r '.prompt // empty' 2>/dev/null)
[ -z "$PROMPT" ] && exit 0

# 匹配
RESULT=$(bash "$MATCH" "$PROMPT" 2>/dev/null)
[ -z "$RESULT" ] && exit 0  # 未命中，正常放行

# 解析 skill|reason
SKILL="${RESULT%%|*}"
REASON="${RESULT#*|}"
[ "$REASON" = "$RESULT" ] && REASON=""

# 注入路由提示（additionalContext）。措辞强制，要求先 read skill 全文。
# 路径解析：优先 Canonical（开发者显式覆盖），其次 embedded 缓存（跨机器通用）
SKILL_PATH=""
for base in "${FORGE_SKILLS_CANONICAL:-}" "${USERPROFILE:-$HOME}/.forge/skills-cache/embedded"; do
  [ -n "$base" ] && [ -f "$base/${SKILL}/SKILL.md" ] && { SKILL_PATH="$base/${SKILL}/SKILL.md"; break; }
done
# 找不到实体文件也注入（fallback 用 skill 名占位 + 备选路径提示）——路由命中不被路径解析阻断
[ -z "$SKILL_PATH" ] && SKILL_PATH="${SKILL}/SKILL.md（候选未命中实体；请试 ~/.forge/skills-cache/embedded/${SKILL}/SKILL.md 或 \$FORGE_SKILLS_CANONICAL 同名 skill）"

CONTEXT="【强制 Skill 路由】本次输入命中 skill 路由规则（${REASON:-关键词匹配}）。
你必须先 read 这个 skill 的完整内容，然后严格按其流程处理，不要自己瞎搞：
- skill: ${SKILL}
- 路径: ${SKILL_PATH}

用户原始意图：${PROMPT}
先读取上述 SKILL.md，再继续。"

# 输出 Claude Code 期望的 JSON 格式
jq -nc --arg ctx "$CONTEXT" '{
  hookSpecificOutput: {
    hookEventName: "UserPromptSubmit",
    additionalContext: $ctx
  }
}'
exit 0

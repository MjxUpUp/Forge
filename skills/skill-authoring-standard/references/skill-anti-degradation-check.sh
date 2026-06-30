#!/usr/bin/env bash
# skill-anti-degradation-check.sh — 扫描 skill 是否有"声称校验但无具体方法"的注水
#
# 用法：
#   bash skill-anti-degradation-check.sh                  # 扫全仓所有 skill
#   bash skill-anti-degradation-check.sh <skill-name>     # 扫单个 skill
#   bash skill-anti-degradation-check.sh <绝对路径>       # 扫指定路径
#
# 退出码：0 = 干净；1 = 发现注水点（需修）
#
# 三类检测：
#   1. 弱校验措辞（"人工校验/肉眼/大致/应该通过"等给 agent rationalize 空间的词）
#   2. 门控段无具体方法（含"门控/必跑/强制"但附近无命令/工具/判定）
#   3. checklist 无验证手段（"是否通过/是否满足"类 yes/no 项无配套命令）

set -uo pipefail

CANONICAL="${SKILLS_CANONICAL:-E:/Forge/skills}"
[ -d "$CANONICAL" ] || CANONICAL="$HOME/.pi/agent/skills"

if [ $# -ge 1 ]; then
  case "$1" in
    /*|*:) TARGETS="$1" ;;
    *) TARGETS="$CANONICAL/$1" ;;
  esac
else
  TARGETS="$CANONICAL"
fi

[ -d "$TARGETS" ] || { echo "❌ 目录不存在: $TARGETS"; exit 2; }

RED='\033[0;31m'; YELLOW='\033[0;33m'; GREEN='\033[0;32m'; NC='\033[0m'

FOUND_ISSUES=0

# === 收集单 skill 的所有 markdown 文件 ===
collect_files() {
  local skill_dir="$1"
  local fs=""
  [ -f "$skill_dir/SKILL.md" ] && fs="$skill_dir/SKILL.md"
  if [ -d "$skill_dir/references" ]; then
    for f in "$skill_dir/references"/*.md; do
      [ -f "$f" ] && fs="$fs $f"
    done
  fi
  echo "$fs"
}

# === 判断一行是否为"正向引用"（反注水语境，不作为注水点） ===
is_positive_ref() {
  local line="$1"
  # Rationalization 表格中的反例引用（| "xxx 应该 xxx" |）
  echo "$line" | grep -qE '^[|] ".*(应该|人工|肉眼)' && return 0
  # "不能/不靠/不要/不是"等否定或反制语境
  echo "$line" | grep -qE '(不能|不靠|不要|不是|不得|禁止|强制|必跑|红线|High.Signal|核心原则|反模式|反例|注水|代替|反驳|≠).*(应该通过|应该没问题|肉眼|人工)'
}

# === 扫描单个 skill ===
scan_skill() {
  local skill_dir="$1"
  local skill_name; skill_name=$(basename "$skill_dir")
  local files; files=$(collect_files "$skill_dir")
  [ -z "$files" ] && return

  local issues=0
  local out=""

  # == 检测 1：弱校验措辞 ==
  # 喂入所有疑似行，过滤掉正向引用，剩余即为真注水
  local hits1
  hits1=$(grep -nE '(人工|肉眼)(校验|审查|核对)|目测|凭肉眼|(大致|简单)(核对|看看|看下)|(应该|应该就|应该会)(通过|没问题)' $files 2>/dev/null || true)
  if [ -n "$hits1" ]; then
    while IFS= read -r l; do
      is_positive_ref "$l" && continue
      out+="${RED}[弱措辞]${NC} $skill_name: $l\n"
      issues=$((issues+1))
    done <<< "$hits1"
  fi

  # == 检测 2：门控段无具体方法 ==
  local hits2
  hits2=$(grep -nE '门控|必跑|强制.*验证|必验证' $files 2>/dev/null || true)
  if [ -n "$hits2" ]; then
    while IFS= read -r l; do
      local file; file=$(echo "$l" | cut -d: -f1)
      local ln; ln=$(echo "$l" | cut -d: -f2 | tr -dc '0-9')
      [ -z "$ln" ] && continue
      local ctx; ctx=$(awk -v ln="$ln" 'NR>=ln-3 && NR<=ln+3' "$file" 2>/dev/null)
      # Inversion / 纯流程门控（不需工具）
      echo "$ctx" | grep -qE '用户确认|过了才进|确认前不要|Inversion 门控|决策门' && continue
      # 有具体方法信号
      echo "$ctx" | grep -qE 'cargo |npm |npx |tsc|eslint|biome|grep |jq |find |gh |curl |docker |git diff|axe-core|jscpd|gitleaks|semgrep|style-dictionary|get_node_dsl|exit code|运行.*命令|跑.*测试' && continue
      out+="${YELLOW}[门控无方法]${NC} $skill_name:$(echo "$l" | head -c 80)\n"
      issues=$((issues+1))
    done <<< "$hits2"
  fi

  # == 检测 3：checklist yes/no 无命令 ==
  local hits3
  hits3=$(grep -nE '^\s*- \[ \] .*(是否通过|是否满足|是否正确|是否完整|是否一致|是否有)' $files 2>/dev/null || true)
  if [ -n "$hits3" ]; then
    while IFS= read -r l; do
      local file; file=$(echo "$l" | cut -d: -f1)
      local ln; ln=$(echo "$l" | cut -d: -f2 | tr -dc '0-9')
      [ -z "$ln" ] && continue
      local ctx; ctx=$(awk -v ln="$ln" 'NR>=ln && NR<=ln+3' "$file" 2>/dev/null)
      echo "$ctx" | grep -qE 'cargo |npm |tsc|eslint|grep |运行|跑|命令|exit code' && continue
      out+="${YELLOW}[checklist无命令]${NC} $skill_name:$(echo "$l" | head -c 80)\n"
      issues=$((issues+1))
    done <<< "$hits3"
  fi

  if [ $issues -gt 0 ]; then
    echo "===== $skill_name: $issues 处可疑 ====="
    echo -e "$out"
    FOUND_ISSUES=$((FOUND_ISSUES+issues))
  fi
}

echo "扫描目录: $TARGETS"
echo "============================================"

if [ $# -ge 1 ] && [ ! -d "$TARGETS/$1" ] && [ "$TARGETS" != "$CANONICAL" ]; then
  # 传了绝对路径，直接扫该 skill
  scan_skill "$TARGETS"
else
  for d in "$TARGETS"/*/; do
    [ -d "$d" ] || continue
    scan_skill "$d"
  done
fi

echo "============================================"
if [ $FOUND_ISSUES -gt 0 ]; then
  echo -e "${RED}发现 $FOUND_ISSUES 处注水点${NC}"
  echo ""
  echo "修复原则："
  echo "  1. 弱措辞（人工/肉眼/大致）→ 替换为可执行命令 + 量化阈值"
  echo "  2. 门控无方法 → 补具体工具/命令/判定标准"
  echo "  3. checklist 无命令 → 每项配验证命令 + 通过标准"
  echo ""
  echo "详见 skill-authoring-standard 的'防注水验证'段。"
  exit 1
else
  echo -e "${GREEN}✅ 干净，无注水点${NC}"
  exit 0
fi

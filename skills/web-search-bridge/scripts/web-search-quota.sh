#!/usr/bin/env bash
# web-search-quota — 检查/记录各搜索 provider 的额度状态
#
# 用法:
#   web-search-quota.sh check [provider]   预检额度（provider 省略=全部可用）
#   web-search-quota.sh status             查看本地累计调用统计
#
# 参数:
#   check [tavily|serper|exa]              查指定 provider；省略=查所有可用
#
# 能力差异（实测）:
#   Tavily  GET /usage 返回月度计划额度（plan_usage/plan_limit）—— 能预检月度余额
#   Serper  响应头 x-ratelimit-* 只反映速率窗口（25req/s）—— 只能防瞬时超限
#   Exa     无额度 endpoint、无响应头额度字段 —— 无法预检，靠调用失败被动应对
#
# 环境变量: TAVILY_API_KEY / SERPER_API_KEY / EXA_API_KEY
# 退出码: 0 正常；1 额度低/不可用；2 环境问题

set -euo pipefail

# 本地调用统计文件（累计记录，避免重复调用消耗额度）
STATS_DIR="${WEB_SEARCH_STATS_DIR:-$HOME/.forge/web-search-stats}"
mkdir -p "$STATS_DIR"
STATS_FILE="$STATS_DIR/usage.json"
[ -f "$STATS_FILE" ] || echo '{"tavily":0,"serper":0,"exa":0}' > "$STATS_FILE"

# Tavily 阈值：剩余 < 此值警告（百分比）
TAVILY_WARN_PCT=20
# Serper 速率剩余 < 此值警告（req/s 窗口）
SERPER_RATE_WARN=5

check_tavily() {
  if [ -z "${TAVILY_API_KEY:-}" ]; then echo "TAVILY: ❌ 未配置 key"; return 2; fi
  local resp
  resp=$(curl -s --max-time 10 "https://api.tavily.com/usage" \
    -H "Authorization: Bearer $TAVILY_API_KEY" 2>/dev/null || echo '{"_err":"curl failed"}')
  if echo "$resp" | jq -e '._err' >/dev/null 2>&1 || ! echo "$resp" | jq -e '.account' >/dev/null 2>&1; then
    echo "TAVILY: ❌ 额度查询失败（key 失效或网络问题）"
    return 2
  fi
  local plan usage limit remaining pct
  usage=$(echo "$resp" | jq -r '.account.plan_usage')
  limit=$(echo "$resp" | jq -r '.account.plan_limit')
  if [ "$limit" = "null" ] || [ -z "$limit" ]; then
    echo "TAVILY: ✅ 无限额度（paygo 模式，按量付费）— plan_usage=$usage"
    return 0
  fi
  remaining=$((limit - usage))
  pct=$(( remaining * 100 / limit ))
  local status="✅"
  [ "$pct" -lt "$TAVILY_WARN_PCT" ] && status="⚠️"
  echo "TAVILY: $status 月度剩余 ${remaining}/${limit} (${pct}%) — plan=Researcher usage=$usage"
  [ "$pct" -lt "$TAVILY_WARN_PCT" ] && return 1
  return 0
}

check_serper() {
  if [ -z "${SERPER_API_KEY:-}" ]; then echo "SERPER: ❌ 未配置 key"; return 2; fi
  # Serper 无额度 endpoint，只能通过一次真实调用的响应头看速率窗口
  # 用最便宜查询探测，-D 存头
  local hdr
  hdr=$(curl -s --max-time 10 -D - \
    "https://google.serper.dev/search" \
    -H "X-API-KEY: $SERPER_API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"q":"test","num":1}' -o /dev/null 2>/dev/null || echo "")
  local limit remaining
  limit=$(echo "$hdr" | grep -i '^x-ratelimit-limit:' | tr -d '\r' | awk '{print $2}')
  remaining=$(echo "$hdr" | grep -i '^x-ratelimit-remaining:' | tr -d '\r' | awk '{print $2}')
  if [ -z "$limit" ]; then
    echo "SERPER: ⚠️ 无法读取速率头（key 可能失效）— 注意：Serper 无月度额度预检，只能看速率窗口"
    return 2
  fi
  local status="✅"
  [ "${remaining:-0}" -lt "$SERPER_RATE_WARN" ] && status="⚠️"
  echo "SERPER: $status 速率窗口剩 ${remaining:-?}/${limit} req — ⚠️ 无法预检月度额度（Serper 不提供）"
  [ "${remaining:-0}" -lt "$SERPER_RATE_WARN" ] && return 1
  return 0
}

check_exa() {
  if [ -z "${EXA_API_KEY:-}" ]; then echo "EXA: ❌ 未配置 key"; return 2; fi
  # Exa 无任何额度 endpoint，无响应头额度字段
  echo "EXA: ⚠️ 无额度预检能力（Exa 不提供 usage API，响应头也无额度）— 靠调用失败被动应对（429=超额）"
  return 0
}

cmd_check() {
  local target="${1:-all}"
  echo "=== 搜索 provider 额度预检（$(date '+%Y-%m-%d %H:%M')）==="
  echo ""
  local worst=0
  case "$target" in
    tavily)  check_tavily || worst=1 ;;
    serper)  check_serper || worst=1 ;;
    exa)     check_exa    || worst=1 ;;
    all)
      check_tavily || worst=1
      echo ""
      check_serper || worst=1
      echo ""
      check_exa    || worst=1
      ;;
    *) echo "未知 provider: $target（可选: tavily|serper|exa|all）"; exit 2 ;;
  esac
  echo ""
  echo "=== 建议（按能力排序选择）==="
  echo "  预检月度额度优先 Tavily（有完整 usage API）"
  echo "  Tavily 额度低时切 Serper（无月度预检但有速率保护）"
  echo "  Exa 作语义搜索备用（无任何额度信息，按需用）"
  return $worst
}

cmd_status() {
  echo "=== 本地累计调用统计（避免重复消耗额度而记录）==="
  if [ ! -s "$STATS_FILE" ]; then echo "无记录"; return; fi
  jq '.' "$STATS_FILE" 2>/dev/null
  echo ""
  echo "统计文件: $STATS_FILE"
}

# 被其他脚本 source 时调用：记录一次调用
record_call() {
  local provider="$1"
  [ -z "$provider" ] && return
  # jq 要返回整个更新后的对象，不是只返回增量值
  # 错误写法（原 bug）：'(.[$p] // 0) + 1' 只返回数字，会覆盖整个 JSON
  local tmp
  tmp=$(jq --arg p "$provider" '.[$p] = ((.[$p] // 0) + 1)' "$STATS_FILE" 2>/dev/null)
  if [ -n "$tmp" ]; then
    echo "$tmp" > "$STATS_FILE"
  fi
}

case "${1:-}" in
  check) shift; cmd_check "$@" ;;
  status) cmd_status ;;
  record) record_call "${2:-}" ;;
  -h|--help)
    sed -n '2,25p' "$0"; exit 0 ;;
  *) echo "用法: web-search-quota.sh check [tavily|serper|exa|all] | status"; exit 2 ;;
esac

#!/usr/bin/env bash
# session-search — 在 pi 会话历史中检索内容
#
# 用法:
#   session-search.sh <query> [options]
#
# 选项:
#   -c, --cwd <path>      只搜指定项目的会话（支持模糊匹配，如 DevWorkbench）
#   -r, --role <role>     只看指定角色: user | assistant | toolResult（默认全角色）
#   -l, --limit <n>       最多返回 n 条命中（默认 20）
#   --full                 输出完整消息内容（默认只输出摘要）
#   --list-cwd            列出所有会话涉及的 cwd（用于发现有哪些项目可检索）
#   --sessions-only       只返回命中的会话文件清单，不输出消息内容
#
# 环境变量:
#   PI_SESSIONS_DIR       pi 会话目录（默认 ~/.pi/agent/sessions）
#
# 退出码:
#   0  有命中
#   1  无命中
#   2  参数错误/环境问题
#
# 原理: pi session 是 jsonl，每行一条记录。type=session 带 cwd；
#       type=message 带 message.role 和 message.content[]。
#       本脚本用 jq 做结构化提取 + rg 做高速文本匹配。

set -euo pipefail

PI_SESSIONS_DIR="${PI_SESSIONS_DIR:-$HOME/.pi/agent/sessions}"

if [ ! -d "$PI_SESSIONS_DIR" ]; then
  echo "❌ 会话目录不存在: $PI_SESSIONS_DIR" >&2
  exit 2
fi

# 参数解析
QUERY=""
CWD_FILTER=""
ROLE=""
LIMIT=20
FULL=false
LIST_CWD=false
SESSIONS_ONLY=false

while [ $# -gt 0 ]; do
  case "$1" in
    -c|--cwd)       CWD_FILTER="$2"; shift 2 ;;
    -r|--role)      ROLE="$2"; shift 2 ;;
    -l|--limit)     LIMIT="$2"; shift 2 ;;
    --full)         FULL=true; shift ;;
    --list-cwd)     LIST_CWD=true; shift ;;
    --sessions-only) SESSIONS_ONLY=true; shift ;;
    -h|--help)
      sed -n '2,30p' "$0"; exit 0 ;;
    *)              QUERY="$QUERY $1"; shift ;;
  esac
done
QUERY="${QUERY# }"

# --list-cwd 模式：列出所有项目的会话分布
if [ "$LIST_CWD" = true ]; then
  find "$PI_SESSIONS_DIR" -name "*.jsonl" -exec jq -r 'select(.type=="session") | .cwd // empty' {} \; 2>/dev/null \
    | sort | uniq -c | sort -rn
  exit 0
fi

if [ -z "$QUERY" ]; then
  echo "用法: session-search.sh <query> [-c <cwd>] [-r <role>] [-l <n>] [--full|--sessions-only|--list-cwd]" >&2
  exit 2
fi

# 确定搜索范围（按 cwd 过滤）
if [ -n "$CWD_FILTER" ]; then
  # cwd 双重存储：session 行的 cwd 字段 + 文件名编码目录。
  # 优先用 session 行的 cwd 精确/模糊匹配，回退到文件名目录匹配。
  CWD_FILTER_LOWER=$(echo "$CWD_FILTER" | tr '[:upper:]' '[:lower:]')
  search_files=$(while IFS= read -r f; do
    file_cwd=$(jq -r 'select(.type=="session") | .cwd // empty' "$f" 2>/dev/null | head -1)
    file_cwd_lower=$(echo "$file_cwd" | tr '[:upper:]' '[:lower:]')
    dir_name=$(basename "$(dirname "$f")")
    if [[ "$file_cwd_lower" == *"$CWD_FILTER_LOWER"* ]] || [[ "$dir_name" == *"$CWD_FILTER_LOWER"* ]]; then
      echo "$f"
    fi
  done < <(find "$PI_SESSIONS_DIR" -name "*.jsonl")
  )
  if [ -z "$search_files" ]; then
    echo "❌ 无 cwd 匹配 \"$CWD_FILTER\" 的会话。可用 --list-cwd 查看所有项目。" >&2
    exit 1
  fi
else
  search_files=$(find "$PI_SESSIONS_DIR" -name "*.jsonl")
fi

# rg 预筛：先找出含关键词的文件，再上 jq 精解（避免对全量文件跑 jq）
# rg 不做转义，让用户用正则；jq 内部用 contains 做二次精确匹配
matched_files=""
while IFS= read -r f; do
  if rg -q -- "$QUERY" "$f" 2>/dev/null; then
    matched_files="$matched_files
$f"
  fi
done <<< "$search_files"
matched_files="${matched_files#
}"

if [ -z "$matched_files" ]; then
  echo "❌ 无命中: \"$QUERY\"" >&2
  exit 1
fi

# --sessions-only 模式：只列会话清单
if [ "$SESSIONS_ONLY" = true ]; then
  for f in $matched_files; do
    cwd=$(jq -r 'select(.type=="session") | .cwd // "?"' "$f" 2>/dev/null | head -1)
    ts=$(jq -r 'select(.type=="session") | .timestamp // "?"' "$f" 2>/dev/null | head -1)
    echo "$(basename "$f")  [$ts]  $cwd"
  done
  exit 0
  fi

# 提取并格式化命中消息
# jq 逻辑:
#   - 只取 type=message 的行
#   - role 过滤（如指定）
#   - content 数组展开，只取 text 类型，检查是否含 query（大小写不敏感）
#   - 输出: 文件名 | 时间 | 角色 | 命中片段
QUERY_ESC=$(printf '%s' "$QUERY" | jq -R .)

OUTPUT_TYPE="brief"
if [ "$FULL" = true ]; then OUTPUT_TYPE="full"; fi

count=0
for f in $matched_files; do
  # 先取该会话的 cwd 和 session 时间，用于输出上下文
  meta=$(jq -c 'select(.type=="session") | {cwd: .cwd, ts: .timestamp}' "$f" 2>/dev/null | head -1)
  cwd=$(echo "$meta" | jq -r '.cwd // "?"' 2>/dev/null)
  session_ts=$(echo "$meta" | jq -r '.ts // "?"' 2>/dev/null)

  # 命中消息提取
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    msg_ts=$(echo "$line" | jq -r '.timestamp // "?"')
    role=$(echo "$line" | jq -r '.message.role // "?"')

    if [ "$OUTPUT_TYPE" = "full" ]; then
      text=$(echo "$line" | jq -r '.message.content[]? | select(.type=="text") | .text')
    else
      # 摘要：取命中文本块，截断到 300 字符
      text=$(echo "$line" | jq -r --arg q "$QUERY" \
        '.message.content[]? | select(.type=="text") | select(.text | ascii_downcase | contains($q | ascii_downcase)) | .text' \
        | head -c 300)
      [ ${#text} -ge 300 ] && text="${text}..."
    fi

    if [ -n "$text" ]; then
      count=$((count + 1))
      printf '\n─── #%d ───────────────────────────────\n' "$count"
      printf '📁 %s\n' "$(basename "$f")"
      printf '🕒 %s  (会话: %s)\n' "$msg_ts" "$session_ts"
      printf '🏷️  cwd: %s  role: %s\n' "$cwd" "$role"
      printf '%s\n' "$text"
      if [ "$count" -ge "$LIMIT" ]; then
        echo ""
        echo "（已达 --limit $LIMIT，更多命中省略）"
        break 2
      fi
    fi
  done < <(jq -c --arg q "$QUERY" --arg r "$ROLE" '
      select(.type=="message")
      | select($r == "" or .message.role == $r)
      | select(any(.message.content[]?; (.type == "text") and (.text | ascii_downcase | contains($q | ascii_downcase))))
    ' "$f" 2>/dev/null
  )
done

if [ "$count" -eq 0 ]; then
  # 区分两种无命中：文本中无该词 vs 文件被 rg 预筛命中但 jq 精解后无 user/assistant 文本
  if [ -n "$ROLE" ]; then
    echo "❌ \"$QUERY\" 在 [$ROLE] 角色的文本消息中无命中。" >&2
    echo "   提示：试不加 -r 过滤，或换 -r assistant / -r toolResult（工具返回内容也常含线索）" >&2
  else
    echo "❌ \"$QUERY\" 在 user/assistant 的文本消息中无命中。" >&2
    echo "   提示：内容可能只在 thinking（推理）/toolCall（工具调用）/toolResult（工具返回）中，或换个关键词" >&2
  fi
  exit 1
fi

exit 0

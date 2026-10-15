#!/usr/bin/env bash
# research-lint.sh — research-workflow 交付前确定性检查（bash 3.2 兼容，只依赖 awk/grep/sed）。
#
# 用法：bash research-lint.sh [--check-urls] <run_dir>
#   退出码 0 = 无 ERROR（可能有 WARN）；1 = 有 ERROR，不得交付；2 = 用法错误。
#   报告写完（report.md 出现）即可跑；通过后再把 status.json 的 stage 改为 delivered。
#
# 检查项（规范见 references/credibility-protocol.md）：
#   E1 run_dir 存在          E2 brief.md 存在且含「原问题」
#   E3 status.json 存在、含 mode/stage/degradations、mode 合法
#   E4 report.md 已写出或 stage=delivered 时，按 mode 检查必备文件
#   E5 report.md 引用：用到的 [^N^] 都有定义；URL 定义带日期，文件定义带 File: 定位
#   E6 report.md 不泄漏内部标签
#   E7 claims.md 每行有核验结论（支持/部分支持/不支持/不可达）
#   W1 search_log.md 行数少于 status.json 自报次数
#   W2 claims.md 仍有「不支持」行（须删除或改写后再交付）
#   W3 （--check-urls）report 定义里的 URL 实测不可达
#   W4 claims.md 断言里的数字（百分数/小数/≥3 位整数，年份除外）不在原文 quote 中

set -u

check_urls=0
run_dir=""
for arg in "$@"; do
  case "${arg}" in
    --check-urls) check_urls=1 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    -*) echo "research-lint: unknown flag ${arg}" >&2; exit 2 ;;
    *) run_dir="${arg}" ;;
  esac
done
if [ -z "${run_dir}" ]; then
  echo "usage: research-lint.sh [--check-urls] <run_dir>" >&2
  exit 2
fi

errors=0
warns=0
err()  { echo "ERROR [${1}] ${2}"; errors=$((errors + 1)); }
warn() { echo "WARN  [${1}] ${2}"; warns=$((warns + 1)); }

# tally 打印 awk 产出的 ERROR/WARN 行并计数（不经临时文件——run_dir 只读时也不漏计）。
tally() {
  local out="${1}" line
  [ -n "${out}" ] || return 0
  while IFS= read -r line; do
    [ -n "${line}" ] || continue
    echo "${line}"
    case "${line}" in
      ERROR*) errors=$((errors + 1)) ;;
      WARN*) warns=$((warns + 1)) ;;
    esac
  done <<EOF
${out}
EOF
}

# awk 程序先存变量再引用：bash 3.2 在 $(...) 里解析含不成对 ) 的单引号串会出错。
E5_AWK='
    /^\[\^[0-9]+\^?\]:/ {
      id = $0; sub(/^\[\^/, "", id); sub(/\^?\]:.*/, "", id)
      def[id] = 1
      line = $0
      if (line ~ /File:/) next
      if (line !~ /https?:\/\//) { printf "ERROR [E5] 引用 [^%s^] 的定义没有 URL 或 File: 定位\n", id; next }
      bare = line; gsub(/https?:\/\/[^ )>]+/, "", bare)
      if (bare !~ /(19|20)[0-9][0-9]/) printf "ERROR [E5] 引用 [^%s^] 的定义没有日期（发布或访问年份，URL 内的数字不算）\n", id
      next
    }
    {
      s = $0
      while (match(s, /\[\^[0-9]+\^?\]/)) {
        id = substr(s, RSTART + 2, RLENGTH - 3); sub(/\^$/, "", id)
        used[id] = 1
        s = substr(s, RSTART + RLENGTH)
      }
    }
    END {
      n = 0
      for (id in used) { n++; if (!(id in def)) printf "ERROR [E5] 正文用了 [^%s^] 但没有定义\n", id }
      if (n == 0) print "ERROR [E5] report.md 正文没有任何 [^N^] 引用"
    }
'

E7_AWK='
    function trim(s) { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
    /^\|[ \t]*C[0-9]+[ \t]*\|/ {
      rows++
      line = $0
      gsub(/\\\|/, "\001", line)
      n = split(line, col, "|")
      id = trim(col[2]); claim = trim(col[3]); quote = trim(col[6]); verdict = trim(col[8])
      gsub(/\001/, "|", claim); gsub(/\001/, "|", quote)
      if (n < 9 || verdict !~ /^(部分支持|不支持|支持|不可达)/) {
        printf "ERROR [E7] %s 缺核验结论（第 7 列须以 支持/部分支持/不支持/不可达 开头）\n", id
        next
      }
      if (verdict ~ /^不支持/) printf "WARN  [W2] %s 核验为不支持——须删除或改写，交付消息里说明\n", id
      s = claim
      while (match(s, /[0-9]+([.,][0-9]+)?%?/)) {
        num = substr(s, RSTART, RLENGTH)
        s = substr(s, RSTART + RLENGTH)
        if (num !~ /%/ && num !~ /[.,]/ && length(num) < 3) continue
        if (num ~ /^(19|20)[0-9][0-9]$/) continue
        if (index(quote, num) == 0) printf "WARN  [W4] %s 断言里的数字 %s 不在原文 quote 中——核对是否转述失真或口径换算\n", id, num
      }
    }
    END { if (rows == 0) print "ERROR [E7] claims.md 没有任何 C<n> 行" }
'

if [ ! -d "${run_dir}" ]; then
  err E1 "run_dir 不存在: ${run_dir}"
  echo "research-lint: ${errors} error(s), ${warns} warning(s)"
  exit 1
fi

# ---- E2 brief ----
if [ ! -f "${run_dir}/brief.md" ]; then
  err E2 "缺 brief.md（研究契约：原问题/决策/子问题/范围/时间窗/出口条件）"
elif ! grep -q "原问题" "${run_dir}/brief.md"; then
  err E2 "brief.md 缺「原问题」字段（须逐字记录用户原文）"
fi

# ---- E3 status ----
mode=""
stage=""
status="${run_dir}/status.json"
if [ ! -f "${status}" ]; then
  err E3 "缺 status.json（mode/stage/degradations/searches/fetches）"
else
  for key in mode stage degradations; do
    grep -q "\"${key}\"" "${status}" || err E3 "status.json 缺字段 \"${key}\""
  done
  mode=$(sed -n 's/.*"mode"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${status}" | head -n 1)
  stage=$(sed -n 's/.*"stage"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${status}" | head -n 1)
  case "${mode}" in
    scatter|locked|file-only|file-augmented) ;;
    *) err E3 "status.json mode 非法：\"${mode}\"（scatter/locked/file-only/file-augmented）" ;;
  esac
fi

report="${run_dir}/report.md"

# ---- E4 required files once the report exists ----
if [ -f "${report}" ] || [ "${stage}" = "delivered" ]; then
  need="report.md verify.md insight.md claims.md search_log.md"
  case "${mode}" in
    file-only) need="${need} file_analysis.md" ;;
    file-augmented) need="${need} file_analysis.md map.md scan_anti.md" ;;
    scatter|locked) need="${need} map.md scan_anti.md" ;;
  esac
  for f in ${need}; do
    [ -f "${run_dir}/${f}" ] || err E4 "报告已写出但缺 ${f}（mode=${mode}）；跳过的步骤写进 status.json degradations 并补齐"
  done
  ls "${run_dir}"/dive_*.md >/dev/null 2>&1 || err E4 "报告已写出但没有任何 dive_*.md"
fi

# ---- E5 citations ----
if [ -f "${report}" ]; then
  tally "$(awk "${E5_AWK}" "${report}")"
fi

# ---- E6 internal label leak ----
if [ -f "${report}" ]; then
  leak=$(grep -n -E '撒网模式|锁定模式|文件 ?only ?模式|文件增强模式|工序 ?[0-9F]|dive_[0-9]|scan_[0-9]|scan_anti|run_dir' "${report}" | head -n 5)
  if [ -n "${leak}" ]; then
    err E6 "report.md 泄漏内部标签（对外改白话）："
    echo "${leak}" | sed 's/^/        /'
  fi
fi

# ---- E7 / W2 / W4 claims ----
claims="${run_dir}/claims.md"
if [ -f "${claims}" ]; then
  tally "$(awk "${E7_AWK}" "${claims}")"
fi

# ---- W1 search log reconciliation ----
log="${run_dir}/search_log.md"
if [ -f "${log}" ] && [ -f "${status}" ]; then
  rows=$(grep -c '^|' "${log}")
  rows=$((rows > 2 ? rows - 2 : 0))
  s=$(sed -n 's/.*"searches"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "${status}" | head -n 1)
  f=$(sed -n 's/.*"fetches"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "${status}" | head -n 1)
  claimed=$(( ${s:-0} + ${f:-0} ))
  if [ "${claimed}" -gt "${rows}" ]; then
    warn W1 "status.json 自报 searches+fetches=${claimed}，search_log.md 只有 ${rows} 行——以日志为准改正自报数"
  fi
fi

# ---- W3 URL liveness (opt-in) ----
if [ "${check_urls}" = "1" ] && [ -f "${report}" ]; then
  set -f  # URL 里的 ? 与 * 不做通配展开
  for url in $(grep -E '^\[\^[0-9]+\^?\]:' "${report}" | grep -oE 'https?://[^ )>]+' | sort -u); do
    code=$(curl -s -o /dev/null -L -m 10 -A 'Mozilla/5.0' -w '%{http_code}' "${url}" 2>/dev/null)
    case "${code}" in
      2*|3*) ;;
      *) warn W3 "URL 不可达（HTTP ${code:-000}）：${url}" ;;
    esac
  done
  set +f
fi

echo "research-lint: ${errors} error(s), ${warns} warning(s)"
[ "${errors}" -eq 0 ]

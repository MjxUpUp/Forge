#!/usr/bin/env bash
# research-lint.sh — research-workflow 交付前确定性检查（bash 3.2 兼容，只依赖 awk/grep/sed）。
#
# 用法：bash research-lint.sh [--check-urls] <run_dir>
#   退出码 0 = 无 ERROR（可能有 WARN）；1 = 有 ERROR，不得交付；2 = 用法错误。
#
# 检查项（规范见 references/credibility-protocol.md）：
#   E1 run_dir 存在          E2 brief.md 存在且含「原问题」
#   E3 status.json 存在且含 mode/stage/degradations
#   E4 stage=delivered 时按 mode 检查必备文件
#   E5 report.md 引用：用到的 [^N^] 都有定义；定义带 URL（或 File:）与日期
#   E6 report.md 不泄漏内部标签
#   E7 claims.md：每行有核验结论；断言里的数字须出现在原文 quote 中
#   W1 search_log.md 行数与 status.json 自报次数对账
#   W2 claims.md 仍有「不支持」行（须删除或改写后再交付）
#   W3 （--check-urls）report 定义里的 URL 实测不可达

set -u

check_urls=0
run_dir=""
for arg in "$@"; do
  case "${arg}" in
    --check-urls) check_urls=1 ;;
    -h|--help) sed -n '2,18p' "$0"; exit 0 ;;
    *) run_dir="${arg}" ;;
  esac
done
if [ -z "${run_dir}" ]; then
  echo "usage: research-lint.sh [--check-urls] <run_dir>" >&2
  exit 2
fi

errors=0
warns=0
err()  { echo "ERROR [$1] $2"; errors=$((errors + 1)); }
warn() { echo "WARN  [$1] $2"; warns=$((warns + 1)); }

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
fi

# ---- E4 required files at delivery ----
if [ "${stage}" = "delivered" ]; then
  need="verify.md insight.md claims.md report.md search_log.md"
  case "${mode}" in
    file-only) need="${need} file_analysis.md" ;;
    file-augmented) need="${need} file_analysis.md map.md scan_anti.md" ;;
    scatter|locked) need="${need} map.md scan_anti.md" ;;
    *) err E4 "status.json mode 非法：\"${mode}\"（scatter/locked/file-only/file-augmented）" ;;
  esac
  for f in ${need}; do
    [ -f "${run_dir}/${f}" ] || err E4 "stage=delivered 但缺 ${f}（mode=${mode}）；跳过的步骤须写进 degradations 并补齐或改 stage"
  done
  ls "${run_dir}"/dive_*.md >/dev/null 2>&1 || err E4 "stage=delivered 但没有任何 dive_*.md"
fi

report="${run_dir}/report.md"

# ---- E5 citations ----
if [ -f "${report}" ]; then
  awk '
    /^\[\^[0-9]+\^?\]:/ {
      id = $0; sub(/^\[\^/, "", id); sub(/\^?\]:.*/, "", id)
      def[id] = 1
      line = $0
      if (line !~ /https?:\/\// && line !~ /File:/) printf "ERROR [E5] 引用 [^%s^] 的定义没有 URL 或 File: 定位\n", id
      if (line !~ /(19|20)[0-9][0-9]/) printf "ERROR [E5] 引用 [^%s^] 的定义没有日期（发布或访问年份）\n", id
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
  ' "${report}" > "${run_dir}/.lint_e5" 2>/dev/null
  while IFS= read -r line; do
    [ -n "${line}" ] || continue
    echo "${line}"; errors=$((errors + 1))
  done < "${run_dir}/.lint_e5"
  rm -f "${run_dir}/.lint_e5"
fi

# ---- E6 internal label leak ----
if [ -f "${report}" ]; then
  leak=$(grep -n -E '撒网模式|锁定模式|文件 ?only ?模式|文件增强模式|工序 ?[0-9F]|dive_[0-9]|scan_[0-9]|scan_anti|run_dir|(^|[^A-Za-z])workers?([^A-Za-z]|$)' "${report}" | head -n 5)
  if [ -n "${leak}" ]; then
    err E6 "report.md 泄漏内部标签（对外改白话）："
    echo "${leak}" | sed 's/^/        /'
  fi
fi

# ---- E7 / W2 claims ----
claims="${run_dir}/claims.md"
if [ -f "${claims}" ]; then
  awk -F'|' '
    function trim(s) { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
    /^\|[ \t]*C[0-9]+[ \t]*\|/ {
      rows++
      id = trim($2); claim = trim($3); quote = trim($6); verdict = trim($8)
      if (verdict !~ /(部分支持|不支持|支持|不可达)/) printf "ERROR [E7] %s 缺核验结论（支持/部分支持/不支持/不可达）\n", id
      if (verdict ~ /不支持/) printf "WARN  [W2] %s 核验为不支持——须删除或改写，交付消息里说明\n", id
      s = claim
      while (match(s, /[0-9]+([.,][0-9]+)?%?/)) {
        num = substr(s, RSTART, RLENGTH)
        s = substr(s, RSTART + RLENGTH)
        # 只核对小数、百分数与 ≥3 位整数：两位数多为日期/序号片段，格式不同属正常
        if (num !~ /%/ && num !~ /[.,]/ && length(num) < 3) continue
        if (index(quote, num) == 0) printf "ERROR [E7] %s 断言里的数字 %s 不在原文 quote 中（转述失真或口径不一致）\n", id, num
      }
    }
    END { if (rows == 0) print "ERROR [E7] claims.md 没有任何 C<n> 行" }
  ' "${claims}" > "${run_dir}/.lint_e7" 2>/dev/null
  while IFS= read -r line; do
    [ -n "${line}" ] || continue
    echo "${line}"
    case "${line}" in
      ERROR*) errors=$((errors + 1)) ;;
      WARN*) warns=$((warns + 1)) ;;
    esac
  done < "${run_dir}/.lint_e7"
  rm -f "${run_dir}/.lint_e7"
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
  grep -E '^\[\^[0-9]+\^?\]:' "${report}" | grep -oE 'https?://[^ )>]+' | sort -u > "${run_dir}/.lint_urls"
  while IFS= read -r url; do
    [ -n "${url}" ] || continue
    code=$(curl -s -o /dev/null -L -m 10 -A 'Mozilla/5.0' -w '%{http_code}' "${url}" 2>/dev/null)
    case "${code}" in
      2*|3*) ;;
      *) warn W3 "URL 不可达（HTTP ${code:-000}）：${url}" ;;
    esac
  done < "${run_dir}/.lint_urls"
  rm -f "${run_dir}/.lint_urls"
fi

echo "research-lint: ${errors} error(s), ${warns} warning(s)"
[ "${errors}" -eq 0 ]

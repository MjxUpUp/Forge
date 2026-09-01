package hooks

// embed_quality.go —— embed.go 同包分文件产物（2026-09 普查 P7：2322 行单文件按职能
// 拆分；内容逐字节不变——embeddedHooks 名册与 guard test 钉住等价性）。

const AutoCompileHook = `#!/bin/bash
# auto-compile.sh — PostToolUse hook for Write|Edit (advisory, non-blocking).
# v0.25: 降级为纯提醒。原版硬编码 go build/cargo check/tsc——对 Java(Maven)/
# Python 等技术栈失效，与 forge "技术栈无关" 定位冲突。loop engineering 下 agent
# 自己知道用什么编译命令，forge 不再越俎代庖跑编译器，只在触及源码时提醒 agent
# 自检。永远 PASS，不阻塞；编译结果由 agent 用自己技术栈的命令验证。
set -eo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

# is_source — BSD-safe 源码扩展判定（case-glob，不用 grep -E alternation，
# 避免 BSD/macOS "Unmatched ( or \(" abort，参 file-sentinel/task-verify 同款处理）。
is_source() {
  case "$1" in
    *.go|*.rs|*.ts|*.tsx|*.js|*.jsx|*.mjs|*.cjs|*.py|*.java|*.rb|*.zig|*.nim|*.c|*.cc|*.cpp|*.h|*.hpp|*.cs|*.kt|*.swift|*.scala) return 0 ;;
  esac
  return 1
}

FILE_PATH="${FORGE_FILE_PATH:-}"
SESSION_ID="${FORGE_SESSION_ID:-default}"
: "${TMPDIR:=/tmp}"
# Session-marker root — must match task-guard's writer (see TaskGuardHook): the
# source-touched marker is written by task-guard under FORGE_DATA_DIR (fallback
# TMPDIR), in the markers/ subdir; reading it from any other root splits the
# pair and every session looks research-mode-forever (2026-08-23 migration).
#
# 会话 marker 根目录——必须与 task-guard 的写端一致（见 TaskGuardHook）：
# source-touched 标记由 task-guard 写在 FORGE_DATA_DIR（兜底 TMPDIR）的
# markers/ 子目录下；从其他根读取会读写分家，每个会话都永远像
# research-mode（2026-08-23 迁移）。
_MARKER_DIR="${FORGE_DATA_DIR:-${TMPDIR:-/tmp}}/markers"

# 是否触及源码：PostToolUse 模式看 FORGE_FILE_PATH；gate 模式（无 FILE_PATH）
# 看 git 工作区有无源码变更。两者都不依赖具体构建系统——技术栈无关。
TOUCHED_SOURCE=0
if [ -n "$FILE_PATH" ]; then
  is_source "$FILE_PATH" && TOUCHED_SOURCE=1
elif git rev-parse --git-dir >/dev/null 2>&1; then
  _diff=$(git diff --name-only HEAD 2>/dev/null || true)
  if [ -n "$_diff" ]; then
    while IFS= read -r _f; do
      [ -n "$_f" ] && is_source "$_f" && { TOUCHED_SOURCE=1; break; }
    done <<< "$_diff"
  fi
fi

# v0.25 advisory：提醒放 stdout PASS detail——forge hook 把 stdout 作为
# AdditionalContext 显示给 agent；stderr 不透传（只进 checklog），agent 看不到。
# 故提醒必须在 stdout。stdout 永远 PASS（不阻塞），编译自检委托给 agent。
#
# dogfood 5.1：会话级 source-touched marker。本会话从未 Edit/Write 源码（AgentFare
# 调研/审查场景，task-guard 不会进 'no task' 分支因此不会设置 marker），抑制 advisory
# 输出——研究场景"自己用编译命令自检"完全无关，PASS detail 只占 AdditionalContext 字符
# 配额。一旦 task-guard 看到源 Edit/Write 即设 marker，本会话后续 advisory 正常输出。
_TOUCHED="${_MARKER_DIR}/forge-source-touched-${SESSION_ID}"
if [ ! -f "$_TOUCHED" ]; then
  echo "PASS [auto-compile] research-mode session, advisory suppressed (set by Edit|Write of source)"
else
  if [ "$TOUCHED_SOURCE" = "1" ]; then
    echo "PASS [auto-compile] Advisory: 已修改源码——请用你技术栈的编译命令确认编译通过（go build ./... / cargo check / mvn -o compile / tsc --noEmit 等）。编译报错时加载 compile-fix-loop skill：编译错误修复闭环方法论，按语言分类定位根因。forge 不再强制编译，适配 loop engineering，由 agent 自检。"
  else
    echo "PASS [auto-compile] no source touched (compile self-check delegated to agent)"
  fi
fi
`


const AssertionCheckHook = `#!/bin/bash
# assertion-check.sh — PreToolUse hook for Write|Edit (advisory, non-blocking).
# v0.25: 降级为纯提醒——检测到疑似断言弱化在 stdout PASS detail 提醒（forge hook 把 stdout 作 AdditionalContext 显示给 agent；stderr 不透传），不再 FAIL 阻塞
# Write|Edit（适配 loop engineering，断言强度由 agent + test-discipline /
# code-review-gate 自检）。保留全部检测逻辑以产出有内容的提醒。
# Two modes: per-edit (FORGE_FILE_PATH set) or batch (checks all git diffs —
# the task-implement gate path via executor's runEmbeddedHook).
#
# 2026-08-24 per-edit 分析（修三连发/四连发重复 advisory）：per-edit 模式只分析
# 本次调用引入的变化——Write 用 新 content vs 盘上旧内容、Edit 用 old_string→
# new_string——不再扫整个 staged+unstaged diff。旧实现里一处陈旧/无关改动（如
# init_test.go 9 删 1 加）会让之后**每次** Edit 重复同一 advisory（真实日志
# 三连发、四连发，agent 被逼读 hook 源码自调试、一次 git checkout -- 丢弃改动
# 止噪）。PreToolUse 时本次编辑尚未落盘，git diff 里根本没有它——旧实现只能
# 分析陈旧 diff 正是结构性根因。Batch 模式（无 FILE_PATH）仍扫全量 diff——
# 那是 task-implement 门禁评判任务整体变更的职责，与 per-edit 互不干扰。
set -eo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

FILE_PATH="${FORGE_FILE_PATH:-}"
CONTENT="${FORGE_CONTENT:-}"
TOOL_NAME="${FORGE_TOOL_NAME:-}"
SESSION_ID="${FORGE_SESSION_ID:-default}"
# session id 消毒成 filename-safe 再拼进 marker 文件名（2026-08-25 review minor：
# 含 / 等文件系统特殊字符的 session id 裸拼会让 marker 写入失败）——与 Go 侧
# readsFileKey 同规则：[A-Za-z0-9._-] 之外一律折叠为 _。
_SID_SAFE=$(printf '%s' "$SESSION_ID" | tr -c 'A-Za-z0-9._-' '_' 2>/dev/null || true)
[ -z "$_SID_SAFE" ] && _SID_SAFE="default"
VIOLATIONS=""

# t.Skip rationale 启发式（2026-08-24 放宽）：带理由的 skip 是合法跳过（fixture
# 生成器、bootstrap、env 守卫都会在消息里写明为什么跳）。旧版只认
# regenerate|bootstrap|intentional|first run|update flag 关键词——把理由写在格式串里
# 的 t.Skipf（如 "…(non-forge repo layout — nothing to regenerate against): %v"）
# 全部误报。新规：引号消息内含空格（多词说明）或含既有关键词 = 有 rationale；
# 裸 t.Skip() / 单词消息（"flaky"）才是弱化。
SKIP_RATIONALE_PAT='t\.Skip(f)?\([[:space:]]*"[^"]*([ ]|(regenerate|bootstrap|intentional|first run|update flag))[^"]*"'

# --- Per-edit mode (hook-triggered by the host: FILE_PATH set) ---
if [ -n "$FILE_PATH" ]; then
# Only check source code files
printf '%s' "$FILE_PATH" | grep -qE '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || exit 0

# Only check test files
printf '%s' "$FILE_PATH" | grep -qE '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/|__tests__/)' || exit 0

# 本次调用引入的旧/新文本。Write：新=content，旧=盘上现内容（PreToolUse 时写入
# 尚未落盘，盘上即 before）；Edit：新=new_string，旧=old_string。其他 file 工具
# （MultiEdit 等）tool_input 无可分析的新旧文本，跳过（fail-open，不占 diff 扫描
# ——扫了只会带回陈旧 diff 的噪音）。
NEW_TEXT=""
OLD_TEXT=""
case "$TOOL_NAME" in
  Write)
    NEW_TEXT="$CONTENT"
    if [ -f "$FILE_PATH" ]; then
      OLD_TEXT=$(cat "$FILE_PATH" 2>/dev/null || true)
    fi
    ;;
  Edit)
    NEW_TEXT="${FORGE_NEW_STRING:-}"
    OLD_TEXT="${FORGE_OLD_STRING:-}"
    ;;
  *)
    exit 0
    ;;
esac

# Go: t.Skip added — flag only if the skip carries no rationale (see the
# heuristic note at SKIP_RATIONALE_PAT).
if printf '%s' "$NEW_TEXT" | grep -qE 't\.Skip(f)?\(' 2>/dev/null; then
  skip_total=$(printf '%s' "$NEW_TEXT" | grep -cE 't\.Skip(f)?\(' 2>/dev/null || true)
  skip_ok=$(printf '%s' "$NEW_TEXT" | grep -cE "$SKIP_RATIONALE_PAT" 2>/dev/null || true)
  if [ "${skip_total:-0}" -gt "${skip_ok:-0}" ]; then
    VIOLATIONS="${VIOLATIONS}[Go] t.Skip added without rationale. "
  fi
fi

# Rust: #[ignore] added
printf '%s' "$NEW_TEXT" | grep -qE '#\[ignore\]' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Rust] #[ignore] found. "

# TypeScript/JavaScript: test.skip / it.skip / describe.skip
printf '%s' "$NEW_TEXT" | grep -qE '(test|it|describe)\.skip\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[TS/JS] test/it/describe.skip found. "

printf '%s' "$NEW_TEXT" | grep -qE '\bx(it|describe)\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[TS/JS] xit/xdescribe found. "

# Python: unittest.skip / pytest.mark.skip
printf '%s' "$NEW_TEXT" | grep -qE '@(unittest\.skip|pytest\.mark\.skip)' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Python] skip decorator found. "

# 净删除检查（OLD_TEXT vs NEW_TEXT）：t.Fatal / assert! 只报**净减少**——
# 改一行断言（如 expected 4 -> 5）删添等量，net zero 不是弱化（旧 false-positive
# 根源）。Edit 的比较域是 old_string/new_string（精确到本次改动）；Write 是
# 整文件 旧内容→新内容。
fatal_old=$(printf '%s' "$OLD_TEXT" | grep -cE 't\.Fatal(f)?\(' 2>/dev/null || true)
fatal_new=$(printf '%s' "$NEW_TEXT" | grep -cE 't\.Fatal(f)?\(' 2>/dev/null || true)
if [ "${fatal_old:-0}" -gt "${fatal_new:-0}" ]; then
  VIOLATIONS="${VIOLATIONS}[Go] t.Fatal net removed by this edit (${fatal_old} -> ${fatal_new}). "
fi
asrt_old=$(printf '%s' "$OLD_TEXT" | grep -cE 'assert(_eq|_ne)?!\(' 2>/dev/null || true)
asrt_new=$(printf '%s' "$NEW_TEXT" | grep -cE 'assert(_eq|_ne)?!\(' 2>/dev/null || true)
if [ "${asrt_old:-0}" -gt "${asrt_new:-0}" ]; then
  VIOLATIONS="${VIOLATIONS}[Rust] assert! net removed by this edit. "
fi
fi

# --- Batch mode (task-implement gate: no FILE_PATH) — full-diff scan ---
if [ -z "$FILE_PATH" ] && git rev-parse --git-dir >/dev/null 2>&1; then
  check_diff() {
    local diff="$1"
    local label="$2"
    [ -z "$diff" ] && return
    # t.Fatal / assert!: flag only a NET reduction. A line edit (e.g. bumping
    # a count constant "expected 4" -> "expected 5") deletes and re-adds the
    # assertion in equal measure — net zero, not a weakening. Only del > add
    # counts. This was the false-positive source when legitimate assertions
    # were edited.
    local fatal_del fatal_add
    fatal_del=$(printf '%s' "$diff" | grep -cE '^\-.*\bt\.Fatal(f)?\(' 2>/dev/null || true)
    fatal_add=$(printf '%s' "$diff" | grep -cE '^\+.*\bt\.Fatal(f)?\(' 2>/dev/null || true)
    if [ "${fatal_del:-0}" -gt "${fatal_add:-0}" ]; then
      VIOLATIONS="${VIOLATIONS}[Go] t.Fatal net removed in ${label} (${fatal_del} del, ${fatal_add} add). "
    fi
    local asrt_del asrt_add
    asrt_del=$(printf '%s' "$diff" | grep -cE '^\-.*\bassert(_eq|_ne)?!\(' 2>/dev/null || true)
    asrt_add=$(printf '%s' "$diff" | grep -cE '^\+.*\bassert(_eq|_ne)?!\(' 2>/dev/null || true)
    if [ "${asrt_del:-0}" -gt "${asrt_add:-0}" ]; then
      VIOLATIONS="${VIOLATIONS}[Rust] assert! net removed in ${label}. "
    fi
    # t.Skip added: flag only if the new skip has no rationale (see the
    # heuristic note at SKIP_RATIONALE_PAT).
    local skip_total skip_rationale
    skip_total=$(printf '%s' "$diff" | grep -cE '^\+.*\bt\.Skip(f)?\(' 2>/dev/null || true)
    skip_rationale=$(printf '%s' "$diff" | grep -cE "^\\+.*\\b${SKIP_RATIONALE_PAT}" 2>/dev/null || true)
    if [ "${skip_total:-0}" -gt "${skip_rationale:-0}" ]; then
      VIOLATIONS="${VIOLATIONS}[Go] t.Skip added without rationale in ${label}. "
    fi
    printf '%s' "$diff" | grep -qE '^\+.*#\[ignore\]' 2>/dev/null && \
      VIOLATIONS="${VIOLATIONS}[Rust] #[ignore] added in ${label}. "
    printf '%s' "$diff" | grep -qE '^\+.*\b(test|it|describe)\.skip\(' 2>/dev/null && \
      VIOLATIONS="${VIOLATIONS}[TS/JS] .skip() added in ${label}. "
    : # always return 0 — grep misses are not errors
  }

  CODE_FILES=$( (git diff --cached --name-only 2>/dev/null; git diff --name-only 2>/dev/null) | sort -u | grep -E '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/)' | grep -E '\.(go|rs|ts|tsx|js|jsx)$' || true)
  if [ -n "$CODE_FILES" ]; then
    STAGED_DIFF=$(git diff --cached -- $CODE_FILES 2>/dev/null || true)
    check_diff "$STAGED_DIFF" "staged diff" || true
    UNSTAGED_DIFF=$(git diff -- $CODE_FILES 2>/dev/null || true)
    check_diff "$UNSTAGED_DIFF" "unstaged diff" || true
  fi
fi

# v0.25 advisory：VIOLATIONS 非空时把提醒放 stdout PASS detail（forge hook 把
# stdout 作为 AdditionalContext 显示给 agent；stderr 不透传只进 checklog，agent
# 看不到）。stdout 永远 PASS（不阻塞），检测逻辑保留以产出有内容的提醒。
if [ -n "$VIOLATIONS" ]; then
  MSG_AC="PASS [assertion-check] Advisory: 疑似断言弱化——${VIOLATIONS}${FILE_PATH:+（文件: $FILE_PATH）}请核查（修代码不修测试）。forge 不再阻塞，由 agent 自检。"
  # 同一 finding 本会话只报一次（2026-08-24，marker 机制——与 skilltrigger 的
  # session marker / task-guard 的 NOWARN 同类）：同一断言弱化 finding 重复触发时
  # 抑制。指纹=FILE_PATH+VIOLATIONS 全文的 cksum（per-edit 文案按类归纳，必须
  # 显式纳入文件名——否则同类的不同文件 finding 互相误抑制）；finding 变了照常
  # 提示。marker 与 task-guard 同根（FORGE_DATA_DIR 兜底 TMPDIR 的 markers/ 子目录，
  # 由 task-guard/bash-guard 的 7 天清扫顺带回收）；写失败降级为重复提示，可接受。
  # 去重仅限 per-edit 模式：batch（task-implement 门禁，SESSION_ID 恒 default、无
  # FORGE_DATA_DIR）重试要如实反映当次扫描，且 marker 落共享 TMPDIR 会跨项目串扰。
  _SUPPRESSED=0
  if [ -n "$FILE_PATH" ]; then
    : "${TMPDIR:=/tmp}"
    _MARKER_DIR="${FORGE_DATA_DIR:-${TMPDIR:-/tmp}}/markers"
    mkdir -p "$_MARKER_DIR" 2>/dev/null || true
    _FP=$(printf '%s' "${FILE_PATH}|${VIOLATIONS}" | cksum | awk '{print $1}')
    _SEEN="${_MARKER_DIR}/forge-assertion-seen-${_SID_SAFE}"
    if [ -n "$_FP" ] && [ -f "$_SEEN" ] && grep -qxF "$_FP" "$_SEEN" 2>/dev/null; then
      _SUPPRESSED=1
    else
      [ -n "$_FP" ] && printf '%s\n' "$_FP" >> "$_SEEN" 2>/dev/null
    fi
  fi
  if [ "$_SUPPRESSED" = "1" ]; then
    # 裸 PASS（无 detail）：kimi 的 advisory 队列按 detail 非空入队
    # （emitAdvisoryRouted）——被抑制的重复若带文案会占攒发名额。
    echo "PASS"
  else
    echo "$MSG_AC"
  fi
else
  echo "PASS [assertion-check] no weakening detected (advisory)"
fi
`


const TaskVerifyHook = `#!/bin/bash
# task-verify.sh — Stop hook (advisory).
# Surfaces quality issues to stderr + checklog (queryable via 'forge trace') but
# NEVER blocks session end. Earlier this hook FAIL'd the Stop to force users to
# address unpassed gates / pending reviews / master-without-task; the cost was
# trapping sessions in stop-retry loops (only papered over by a 3-failure
# force-pass counter). Blocking adds friction without reliably changing the
# outcome — the advisory form keeps the signal, drops the trap.
# Protocol: Stop hooks output PASS or FAIL on stdout; we always PASS.
set -eo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

# runtime state 在用户级 DataDir（refactor-data-home commit D：git 项目
# ~/.forge/projects/<key>/）。hook 无法复现 Key 算法（FNV-64a），调 forge data-dir
# 拿路径；失败回退 .forge（非 git 语义）。hook 已多次 fork forge，多一次无感。
_DATA_DIR="$(forge data-dir 2>/dev/null || echo ".forge")"

# Throttle: collapse PostToolUse trigger storms. Stop fires once per session
# (intervals >> 60s), so a 60s window only suppresses repeated PostToolUse
# invocations — e.g. legacy settings that mis-bind this hook to a wide
# Bash|Read|Glob matcher. Advisory skip is safe: the signal resurfaces on the
# next non-throttled run. Without this, a stale binding + 4 subshells/call can
# fire 100+ times per session (observed in real heavy-use projects).
_STAMP="$_DATA_DIR/.task-verify-throttle.last"
_NOW=$(date +%s 2>/dev/null || echo 0)
if [ "$_NOW" != "0" ] && [ -f "$_STAMP" ]; then
  _LAST=$(cat "$_STAMP" 2>/dev/null || echo 0)
  if [ -n "$_LAST" ] && [ $((_NOW - _LAST)) -lt 60 ]; then
    echo "PASS"
    exit 0
  fi
fi
printf '%s' "$_NOW" > "$_STAMP" 2>/dev/null || true

# is_code_file — BSD-safe source-file filter. grep -E '\.(go|rs|...)$' aborts
# on BSD/macOS with "Unmatched ( or \(" (ERE alternation in a group); case-glob
# is portable and mirrors the extension set task-guard / file-sentinel use.
is_code_file() {
  case "$1" in
    *.go|*.rs|*.ts|*.tsx|*.js|*.jsx|*.py|*.java|*.rb) return 0 ;;
  esac
  return 1
}

MESSAGES=""

# Task gate check — capture gate output so executor stderr advisories
# (test-coverage / scope-drift) reach MESSAGES instead of being discarded.
# dogfood 4.2 + 2.1: test-coverage advisory carries test-discipline guidance
# and must surface here for parity with the act-nudge channel below.
GATE_OUT=$(forge task gate task-verify --silent 2>&1) || {
  MESSAGES="${MESSAGES}[task-gate] Task verify gate not yet passed — run 'forge task gate task-verify' for details. "
}
# 方案1 契约：gate 的 advisory stderr 现统一带 "ADVISORY: " 前缀（GateAdvisory）。
# 旧 grep '[task-verify] Advisory' 在 test-coverage 迁移到 ADVISORY: 前缀后失配，吞了
# test-discipline advisory（TestTaskVerifyHook_SurfacesTestDisciplineAdvisory 回归）。
# 改匹配契约前缀——GATE_OUT 已 scoped 到 task-verify gate，所有 ADVISORY: 行均本 gate 的
# advisory（test-coverage / scope-drift / cheat-scan / test-capability / skill-eval / acceptance）。
ADV=$(printf '%s' "$GATE_OUT" | grep -F 'ADVISORY:' || true)
if [ -n "$ADV" ]; then
  MESSAGES="${MESSAGES}${ADV} "
fi

# Code changes on main/master without active task
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
if [ "$BRANCH" = "master" ] || [ "$BRANCH" = "main" ]; then
  TASK_STATUS=$(forge task status 2>&1 || true)
  if printf '%s' "$TASK_STATUS" | grep -qF "No active task"; then
    CODE_CHANGES=$(git diff --name-only 2>/dev/null | while IFS= read -r _f; do is_code_file "$_f" && printf '%s\n' "$_f"; done || true)
    STAGED_CHANGES=$(git diff --cached --name-only 2>/dev/null | while IFS= read -r _f; do is_code_file "$_f" && printf '%s\n' "$_f"; done || true)
    if [ -n "$CODE_CHANGES" ] || [ -n "$STAGED_CHANGES" ]; then
      MESSAGES="${MESSAGES}Code changes on ${BRANCH} without active task. Start one: forge task start --ref <type>/<desc> --branch "
    fi
  fi
fi

# Act 反馈臂：最新任务结论若标 RetrospectiveNudge（证据弱 Unverified/Weak 或低分<70），
# surface 到会话结束。与 task-gate/pending-review 同级——质量信号在会话结束集中呈现，
# 确保“高分但没真验证”的盲区到达回顾检查点（Directive 在 task complete 打印一次易被
# 后续工作淹没）。forge act nudge 干净完成时静默，只在有盲区时输出一行。
NUDGE=$(forge act nudge 2>/dev/null) || true
if [ -n "${NUDGE}" ]; then
  MESSAGES="${MESSAGES}${NUDGE} "
fi

# Advisory: always PASS, never block. Surface issues to stderr (user-visible)
# and checklog (trace-queryable).
# level 显式写 "advisory"（checklog Level 字段）：Detail 无 ADVISORY: 前缀，
# 读取侧 derive 只会给 pass——本条目实为 advisory 语义，必须显式标注。
# checklog 行携带真实上下文（2026-08-24 死记录修复：此前 detail 是固定串
# "advisory: non-blocking issues surfaced to stderr"、checked=false、无
# task_ref/session_id——占 task-verify 记录约 45% 且零诊断价值）。detail 取
# MESSAGES 摘要：去引号/反斜杠（JSON 安全）、换行压空格、去其余控制字符
#（tab 等——手写 JSONL 不转义，控制字符会产出非法 JSON）、截 200 字符。
# task_ref/session_id 来自 Go 层注入的 env（runHook 已 sanitize），但本行是
# 手写 JSON——嵌入前仍套同一净化（去引号/反斜杠/控制字符），不信任上游契约。
if [ -n "$MESSAGES" ]; then
  _DETAIL=$(printf '%s' "$MESSAGES" | tr -d '"\\' | tr '\n' ' ' | tr -d '[:cntrl:]' | cut -c1-200)
  _TASK_REF_J=$(printf '%s' "${FORGE_TASK_REF:-}" | tr -d '"\\' | tr -d '[:cntrl:]')
  _SESSION_ID_J=$(printf '%s' "${FORGE_SESSION_ID:-}" | tr -d '"\\' | tr -d '[:cntrl:]')
  _NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)
  if [ -n "$_NOW" ]; then
    printf '{"check":"task-verify","passed":true,"checked":true,"detail":"%s","level":"advisory","task_ref":"%s","session_id":"%s","recorded_at":"%s"}\n' \
      "$_DETAIL" "$_TASK_REF_J" "$_SESSION_ID_J" "$_NOW" >> "$_DATA_DIR/checklog.jsonl" 2>/dev/null || true
  fi
  # kimi 的 Stop stdout/stderr 都到不了模型（见 agentbridge/kimi-hook-routing.md），
  # 但 Go 层会把本 hook 的 advisory stdout 入队、下次 UserPromptSubmit 攒发
  # （hook_kimi_advisory.go）——故 kimi 下把消息打到 stdout（WARN 前缀供
  # extractDetail 剥离）。其余宿主维持 stderr（user-visible）不动。
  if [ "${FORGE_AGENT:-}" = "kimi" ]; then
    echo "WARN [task-verify] ${MESSAGES}"
    exit 0
  fi
  echo "[task-verify] ${MESSAGES}" >&2
fi

echo "PASS"
`


const ReviewStopHook = `#!/bin/bash
# review-stop.sh — Stop hook. 让 code-review-gate 自动挡：未审源码变更时 block 会话结束。
#
# 双路径（见 forge review gate）：
#   - task 模式：gate 直接 PASS 放行——审查由 task-complete 门禁的 ReviewPassed 硬前置
#     强制（executor.go），此处不重复拦（否则 task 流程每次改代码都被拦，与门禁重复且扰人）。
#   - 非 task 模式：gate 按 diff stamp 决策，未审则 block。
#
# 与 task-verify 的关键区别：task-verify 永远 PASS（advisory）——因 Stop block 曾致
# retry-loop 死循环。本 hook 敢 block 是因为 review 包有 max-rounds 兜底（Evaluate 在
# MaxReviewRounds=3 后 advisory 放行），block 最多 3 次必然收敛，不会死循环。
#
# 误触发防护（2026-06-27）：gate 内部只统计源码变更（扩展名白名单 + 排除 .forge/文档/
# 生成物），纯文档/配置变更、无变更、commit 后干净工作区都不 block。
#
# Protocol: stdout = AdditionalContext（agent 可见，承载审查指引）；exit 0 = 允许 Stop，
# exit 2 = block Stop（agent 继续工作）。forge review gate: exit 0=PASS/ADVISORY, 1=FAIL。
ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

# gate 是判定引擎（task 检测 + diff hash + max-rounds 兜底全在里面）。
# 不用 set -e：gate 在 NeedReview 时 exit 1，set -e 会让脚本此刻意外退出。
OUTPUT=$(forge review gate 2>/dev/null)
CODE=$?

if [ "$CODE" -eq 0 ]; then
  # PASS / ADVISORY：允许 Stop。必须静默——Stop hook 的 stdout 一律被 harness 当
  # AdditionalContext feedback 注入，即便 exit 0 也会激活 agent 再响应一轮，造成
  # Stop→feedback→响应→Stop 死循环（"无未提交变更，无需审查"反复刷屏即此症）。
  # PASS 无事可做；ADVISORY 已是放行兜底，提醒留待 forge review status 手查，不占 Stop。
  exit 0
fi

# dogfood 1.1 fail-open 诊断：gate 异常（exit≠0 且空 stdout，非正常 NeedReview 的
# exit=1 带指引）时补可读理由，避免 block Stop 但 additionalContext 为空让 agent
# 不知为何被拦。CODE=1 正常路径 OUTPUT 非空，不触发兜底。
if [ -z "$OUTPUT" ]; then
  OUTPUT="forge review gate 异常（exit $CODE 无输出）——运行 'forge review status' 诊断。"
fi

# FAIL：block Stop。stdout（gate 已打印审查指引）成为 AdditionalContext，
# 指引 agent 加载 code-review-gate、派只读子 agent 审查、forge review pass。
echo "$OUTPUT"
exit 2
`


const WorkflowTestGuardHook = `#!/bin/bash
# workflow-test-guard.sh — PostToolUse hook for Write|Edit.
# 改 .github/workflows/*.yml 后自动跑 internal/ci 守护测试——把"沙盒异常"在修改
# 当下反馈给 agent，不依赖 CI、不依赖自觉。这是 release.yml test→goreleaser→npm
# needs 链的实时守护层（见 internal/ci/release_workflow_test.go）：CI 层只在 push/PR
# 兜底，agent 本地改 workflow 时只有这个 hook 能即时反馈，闭合"沙盒能检测→异常反馈
# 到真实修改"的最后一环。
#
# 设计：
#   - 只对 .github/workflows/*.yml 触发（case-glob，BSD 安全，不用 grep -E 交替）。
#   - 跑 go test ./internal/ci/；FAIL 则 exit 1 把测试输出反馈给 agent。
#   - internal/ci 不存在（老项目/未启用 CI 配置守护）→ 静默 PASS。
#   - 不用 set -e：要捕获 go test exit code（set -e 会在 go test 失败时杀脚本）。
#
# Protocol: stdout = 反馈（PASS detail 或 FAIL reason）；exit 0 = 放行，exit 1 = block。
ROOT="${1:-.}"
FILE_PATH="${FORGE_FILE_PATH:-}"

# 无文件路径（batch/非文件工具）→ 放行
[ -z "$FILE_PATH" ] && { echo "PASS"; exit 0; }

# 归一化路径分隔符（Windows 反斜杠 → 正斜杠），便于 case-glob。
# Claude Code 的 tool_input file_path 在 Windows 可能是反斜杠。
NORM_PATH="${FILE_PATH//\\//}"

# 是否 .github/workflows/*.yml——BSD 安全 case-glob（不用 grep -E 交替）。
# 匹配仓库根相对（.github/workflows/x.yml）和带前导路径（a/b/.github/workflows/x.yml）。
case "$NORM_PATH" in
  .github/workflows/*.yml|*/.github/workflows/*.yml) ;;
  *) echo "PASS"; exit 0 ;;
esac

cd "$ROOT" 2>/dev/null || { echo "PASS"; exit 0; }

# internal/ci 不存在 → 无守护测试可跑，静默放行（老项目/未启用 CI 配置守护）。
[ -d "internal/ci" ] || { echo "PASS"; exit 0; }

# go 不在 PATH：go test 会 exit 127，被下面 CODE!=0 分支误当测试失败 FAIL 阻塞
# 编辑——工具故障不是测试失败。拦截承诺只覆盖"测试真跑了且挂了"，fail-open skip。
command -v go >/dev/null 2>&1 || { echo "PASS [workflow-test-guard] go not on PATH, skipping internal/ci guard tests"; exit 0; }

# 守护范围是"任何有 internal/ci 的项目自己的守护测试"（opt-in via internal/ci
# 存在性，见 e2e setupGuardProject），不限 forge 仓。无 go / package 不可解析
# 两类环境故障已在上下分支 fail-open，不会误阻塞。

# 跑守护测试，捕获 exit code（不用 set -e，否则 go test 失败会杀脚本拿不到 CODE）。
OUTPUT=$(go test ./internal/ci/ -count=1 2>&1)
CODE=$?

if [ "$CODE" -eq 0 ]; then
  echo "PASS [workflow-test-guard] workflow 配置变更后 internal/ci 守护测试全绿"
  exit 0
fi

# "package 不存在/不可解析"类错误（目录里无 Go package、路径不解析）不是守护
# 测试失败——是环境/布局问题，fail-open skip 而非阻塞编辑（grep -qF 固定串，
# BSD 安全，无 ERE 交替）。
if printf '%s' "$OUTPUT" | grep -qF 'no required module provides package' || \
   printf '%s' "$OUTPUT" | grep -qF 'matched no packages' || \
   printf '%s' "$OUTPUT" | grep -qF 'no Go files'; then
  echo "PASS [workflow-test-guard] internal/ci package 不可解析（非测试失败），skip: $(printf '%s' "$OUTPUT" | head -1)"
  exit 0
fi

# FAIL：workflow 变更破坏了 internal/ci 守护测试（needs 链/触发条件/test job 源头）。
# stdout 反馈给 agent（PostToolUse exit 1 → additionalContext），agent 据此还原或同步断言。
echo "FAIL [workflow-test-guard] workflow 配置变更破坏了 internal/ci 守护测试："
echo "$OUTPUT" | tail -25
echo ""
echo "这是 CI 防绕过链的实时守护。要么还原对 .github/workflows/ 的破坏性修改，"
echo "要么（若有意改 needs 链/触发条件）同步更新 internal/ci/release_workflow_test.go 的断言。"
exit 1
`

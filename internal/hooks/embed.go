package hooks

// Embedded hook scripts for forge init.
// These are written to .forge/hooks/ during project initialization.
//
// Protocol: bash scripts output plain text to stdout.
// - Line starting with "PASS" = check passed, rest of line is optional detail.
// - Line starting with "FAIL" = check failed, rest of line is the reason.
// - If multiple lines, the LAST PASS/FAIL line determines the result.
// - Any output to stderr is captured for debugging.
// Go wraps the result into structured JSON for Claude Code.
//
// Go extracts tool_input fields into env vars (FORGE_FILE_PATH, FORGE_CONTENT,
// FORGE_TOOL_NAME) so bash scripts don't need to parse JSON.

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
if [ "$TOUCHED_SOURCE" = "1" ]; then
  echo "PASS [auto-compile] Advisory: 已修改源码——请用你技术栈的编译命令确认编译通过（go build ./... / cargo check / mvn -o compile / tsc --noEmit 等）。forge 不再强制编译，适配 loop engineering，由 agent 自检。"
else
  echo "PASS [auto-compile] no source touched (compile self-check delegated to agent)"
fi
`

const AssertionCheckHook = `#!/bin/bash
# assertion-check.sh — PreToolUse hook for Write|Edit (advisory, non-blocking).
# v0.25: 降级为纯提醒——检测到疑似断言弱化在 stdout PASS detail 提醒（forge hook 把 stdout 作 AdditionalContext 显示给 agent；stderr 不透传），不再 FAIL 阻塞
# Write|Edit（适配 loop engineering，断言强度由 agent + test-discipline /
# code-review-gate 自检）。保留全部检测逻辑以产出有内容的提醒。
# Two modes: per-file (FORGE_FILE_PATH set) or batch (checks all git diffs).
set -eo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

FILE_PATH="${FORGE_FILE_PATH:-}"
CONTENT="${FORGE_CONTENT:-}"
VIOLATIONS=""

# --- Per-file mode (hook-triggered by Claude Code) ---
if [ -n "$FILE_PATH" ]; then
# Only check source code files
printf '%s' "$FILE_PATH" | grep -qE '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || exit 0

# Only check test files
printf '%s' "$FILE_PATH" | grep -qE '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/|__tests__/)' || exit 0

# Go: t.Skip added — flag only if the skip carries no rationale keyword.
# Legitimate skips (fixture generators, bootstraps, env guards) state why
# they skip in the message; bare t.Skip()/t.Skip("flaky") is the weakening.
if printf '%s' "$CONTENT" | grep -qE 't\.Skip(f)?\(' 2>/dev/null; then
  printf '%s' "$CONTENT" | grep -qE 't\.Skip(f)?\([^)]*(regenerate|bootstrap|intentional|first run|update flag)' 2>/dev/null || \
    VIOLATIONS="${VIOLATIONS}[Go] t.Skip without rationale keyword. "
fi

# Rust: #[ignore] added
printf '%s' "$CONTENT" | grep -qE '#\[ignore\]' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Rust] #[ignore] found. "

# TypeScript/JavaScript: test.skip / it.skip / describe.skip
printf '%s' "$CONTENT" | grep -qE '(test|it|describe)\.skip\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[TS/JS] test/it/describe.skip found. "

printf '%s' "$CONTENT" | grep -qE '\bx(it|describe)\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[TS/JS] xit/xdescribe found. "

# Python: unittest.skip / pytest.mark.skip
printf '%s' "$CONTENT" | grep -qE '@(unittest\.skip|pytest\.mark\.skip)' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Python] skip decorator found. "
fi

# --- Diff mode (batch gate check + per-file fallback) ---
if git rev-parse --git-dir >/dev/null 2>&1; then
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
    # t.Skip added: flag only if the new skip has no rationale keyword. Legit
    # skips (generators/bootstraps/env guards) annotate why in the message.
    local skip_total skip_rationale
    skip_total=$(printf '%s' "$diff" | grep -cE '^\+.*\bt\.Skip(f)?\(' 2>/dev/null || true)
    skip_rationale=$(printf '%s' "$diff" | grep -cE '^\+.*\bt\.Skip(f)?\([^)]*(regenerate|bootstrap|intentional|first run|update flag)' 2>/dev/null || true)
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
  echo "PASS [assertion-check] Advisory: 疑似断言弱化——${VIOLATIONS}请核查（修代码不修测试）。forge 不再阻塞，由 agent 自检。"
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

# Throttle: collapse PostToolUse trigger storms. Stop fires once per session
# (intervals >> 60s), so a 60s window only suppresses repeated PostToolUse
# invocations — e.g. legacy settings that mis-bind this hook to a wide
# Bash|Read|Glob matcher. Advisory skip is safe: the signal resurfaces on the
# next non-throttled run. Without this, a stale binding + 4 subshells/call can
# fire 100+ times per session (observed in real heavy-use projects).
_STAMP=".forge/.task-verify-throttle.last"
_NOW=$(date +%s 2>/dev/null || echo 0)
if [ "$_NOW" != "0" ] && [ -f "$_STAMP" ]; then
  _LAST=$(cat "$_STAMP" 2>/dev/null || echo 0)
  if [ -n "$_LAST" ] && [ $((_NOW - _LAST)) -lt 60 ]; then
    echo "PASS"
    exit 0
  fi
fi
printf '%s' "$_NOW" > "$_STAMP" 2>/dev/null || true

# FORGE_SKIP_VERIFY escape hatch: skip the advisory checks entirely. Even
# though task-verify no longer blocks, an explicit skip is audited to checklog
# (A4) so the bypass stays traceable via 'forge trace'.
if [ "${FORGE_SKIP_VERIFY}" = "1" ]; then
  echo "PASS"
  _SKIP_NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)
  if [ -n "$_SKIP_NOW" ]; then
    printf '{"check":"escape-hatch","passed":true,"checked":true,"detail":"escape-hatch: FORGE_SKIP_VERIFY=1 (task-verify gate bypassed)","recorded_at":"%s"}\n' \
      "$_SKIP_NOW" >> .forge/checklog.jsonl 2>/dev/null || true
  fi
  exit 0
fi

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

# Task gate check
forge task gate task-verify --silent >/dev/null 2>&1 || {
  MESSAGES="${MESSAGES}[task-gate] Task verify gate not yet passed. "
}

# Pending mandatory reviews
if REVIEW_OUTPUT=$(forge experience list 2>/dev/null); then
  printf '%s' "$REVIEW_OUTPUT" | grep -qE 'mandatory[[:space:]]+pending' && {
    MESSAGES="${MESSAGES}Pending mandatory review detected. Run 'forge experience list'. "
  }
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
# and checklog (trace-queryable). Detail is a fixed string — MESSAGES may carry
# quotes/paths that would break the JSON line, so it goes to stderr only.
if [ -n "$MESSAGES" ]; then
  echo "[task-verify] Advisory (non-blocking): ${MESSAGES}" >&2
  _NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)
  if [ -n "$_NOW" ]; then
    printf '{"check":"task-verify","passed":true,"checked":false,"detail":"advisory: non-blocking issues surfaced to stderr","recorded_at":"%s"}\n' \
      "$_NOW" >> .forge/checklog.jsonl 2>/dev/null || true
  fi
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

# FAIL：block Stop。stdout（gate 已打印审查指引）成为 AdditionalContext，
# 指引 agent 加载 code-review-gate、派只读子 agent 审查、forge review pass。
echo "$OUTPUT"
exit 2
`

const TaskGuardHook = `#!/bin/bash
# task-guard.sh — PreToolUse hook for Write|Edit.
# Self-protection: blocks direct writes to Forge-managed files.
# Auto-creates tasks on feature branches when no active task exists.
# v0.17: 3-gate pipeline (implement / verify / complete).
set -eo pipefail

FILE_PATH="${FORGE_FILE_PATH:-}"
TASK_REF="${FORGE_TASK_REF:-}"
TASK_GATE="${FORGE_TASK_GATE:-}"

# No file path (batch mode or non-file tool) — allow
[ -z "$FILE_PATH" ] && exit 0
# Self-protection: block direct writes to Forge-managed runtime files.
# Exception: .forge/protocol.yml and .forge/pipeline.yml are user-editable
# project config (autoSync never overwrites them) — direct Edit is allowed so
# agents can adjust project quality rules. All other .forge/* are runtime state
# (state.json/tasks/gates/hooks/checklog/etc.) managed only by forge commands.
case "$FILE_PATH" in
  .forge/protocol.yml|.forge/pipeline.yml)
    # User-editable config — fall through to source-file checks below
    ;;
  .forge/*|.claude/settings*)
    echo "FAIL [task-guard] Direct modification of Forge-managed files is not allowed. Use forge commands."
    exit 1
    ;;
esac

# Not a source code file — allow
printf '%s' "$FILE_PATH" | grep -qE '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || exit 0

# Test files — always allow (TDD workflow)
printf '%s' "$FILE_PATH" | grep -qE '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/|__tests__/)' && exit 0

# No active task — try auto-create on feature branch
if [ -z "$TASK_REF" ]; then
  BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
  if [ "$BRANCH" != "master" ] && [ "$BRANCH" != "main" ] && [ -n "$BRANCH" ]; then
    # On feature branch: auto-create task from branch name
    if forge task start --ref "$BRANCH" 2>/dev/null; then
      echo "WARN [task-guard] Auto-created task '${BRANCH}' from branch. Source changes tracked."
      exit 0
    fi
  fi
  # On master/main or auto-creation failed: warn but allow
  echo "WARN [task-guard] No active task. Source changes are allowed but not tracked by a Forge task."
  exit 0
fi

echo "PASS"
`

const BashGuardHook = `#!/bin/bash
# bash-guard.sh — PreToolUse hook for Bash.
# Layer 1: Detect write patterns in Bash commands, block if no active task.
# Layer 2: Snapshot file state for PostToolUse file-sentinel comparison.
# Forge command detection for file-sentinel exemption.
set -eo pipefail

COMMAND="${FORGE_COMMAND:-}"
TASK_REF="${FORGE_TASK_REF:-}"
SESSION_ID="${FORGE_SESSION_ID:-default}"
SNAPSHOT_FILE="${TMPDIR:-/tmp}/forge-snapshot-${SESSION_ID}"
# Defensive: never let SNAPSHOT_FILE be empty — an empty value would make a
# redirect write to a literal/misdirected filename.
[ -z "${SNAPSHOT_FILE:-}" ] && SNAPSHOT_FILE="${TMPDIR:-/tmp}/forge-snapshot-unknown"

# WRITE_FLAG_FILE tells file-sentinel whether THIS Bash command was a write
# (non-empty "1" = write; empty = read-only). Defined here so both the snapshot
# policy and the no-task WARN below see it.
WRITE_FLAG_FILE="${TMPDIR:-/tmp}/forge-write-${SESSION_ID}"
[ -z "${WRITE_FLAG_FILE:-}" ] && WRITE_FLAG_FILE="${TMPDIR:-/tmp}/forge-write-unknown"

# --- Detect write patterns in command ---
# POSIX case-glob + a single BRE — NO grep -E alternation. BSD grep aborts on
# ERE alternation with "Unmatched ( or \(" (178× in field logs, all under
# BSD/macOS; GNU grep tolerates it). The old ERE also missed printf>, dd of=,
# rsync, tee, and absolute-path/append redirects. This rewrite is portable and
# strictly broader in coverage.
has_write_pattern() {
  local cmd="$1"
  # JavaScript file writes — distinctive tokens, glob match (no regex).
  case "$cmd" in
    *writeFile*|*writeFileSync*|*"fs.write"*) return 0 ;;
  esac
  # Shell commands that always write to disk — token scan gives real word
  # boundaries without ERE alternation.
  local tok
  for tok in $cmd; do
    case "$tok" in
      cp|mv|dd|install|rsync|scp|cpio|wget|tee) return 0 ;;
    esac
  done
  # Flag-gated writers: in-place editors (-i), download-to-file (-o/-O/--output),
  # git apply, patch -p<n>.
  case " $cmd " in
    *" sed "*-i*|*" perl "*-i*|*" curl "*-o*|*" curl "*-O*|*" curl "*--output*|*" git apply"*|*" patch -p"*) return 0 ;;
  esac
  # Shell redirect to a real file. Neutralize stderr (2>) and JS arrows (=>)
  # so neither masquerades as an output redirect, then require a path-like
  # target (contains "." or "/"), excluding /dev/null. A bare comparison like
  # "x > 0" is rejected because "0" carries no path character. Single BRE,
  # no ERE — portable across GNU/BSD grep.
  local s="${cmd//2>/··}"
  s="${s//=>/··}"
  case "$s" in
    *"> /dev/null"*) : ;;
    *) printf '%s' "$s" | grep -q '>[[:space:]]*[^[:space:]&][^[:space:]]*[./][^[:space:]]*' && return 0 ;;
  esac
  return 1
}

# --- Forge command detection (for file-sentinel exemption) ---
# case-glob, not grep -E — keeps this BSD-safe too.
IS_FORGE_CMD=0
case " $COMMAND " in
  *" forge "*) IS_FORGE_CMD=1 ;;
esac

# --- Snapshot file state (always — file-sentinel is defense-in-depth) ---
# Snapshot EVERY Bash command, not just detected writes. file-sentinel's
# unauthorized-config-tamper detection (an external process rewriting
# .forge/gates/status.json during an otherwise read-only ls/cat) needs a
# pre-command baseline for every command; gating the snapshot on
# write-detection blinds it (regression caught by
# TestHook_FileSentinel_QuarantinesTamperedGateStatus). The 2 git calls per
# command are the cost of that defense — accepted over the false economy of
# skipping them.
{
  git diff --name-only 2>/dev/null || true
  git ls-files --others --exclude-standard 2>/dev/null || true
} | sort -u > "$SNAPSHOT_FILE" 2>/dev/null || true

# Record whether THIS command is a write, for file-sentinel's secondary gate.
# MUST be written BEFORE the no-task WARN-exit below — otherwise, when there is
# no active task, the flag is never recorded and file-sentinel would mistake a
# real Bash source write for a read-only command and fail-open (P0 DevWorkbench
# incident root cause: working tree restored to HEAD). A read-only command
# cannot produce source changes, so file-sentinel must never quarantine under
# one.
IS_WRITE_CMD=0
if has_write_pattern "$COMMAND"; then
  IS_WRITE_CMD=1
  printf '1' > "$WRITE_FLAG_FILE" 2>/dev/null || true
else
  : > "$WRITE_FLAG_FILE" 2>/dev/null || true
fi

# --- WARN on write without active task (allowed, just not tracked) ---
if [ $IS_WRITE_CMD -eq 1 ] && [ -z "$TASK_REF" ]; then
  echo "WARN [bash-guard] Bash write without active task. Changes are allowed but not tracked."
  exit 0
fi

# Mark as forge command for file-sentinel
if [ $IS_FORGE_CMD -eq 1 ]; then
  touch "${TMPDIR:-/tmp}/forge-cmd-${SESSION_ID}" 2>/dev/null || true
fi

echo "PASS"`

const HazardGuardHook = `#!/bin/bash
# hazard-guard.sh — PreToolUse hook for Bash. on-demand-guards 自动挡：高危命令
# human-in-the-loop 拦截。
#
# 检测高危命令（rm -rf / git push --force / DROP TABLE / TRUNCATE / kubectl delete /
# DELETE 无 WHERE 等）→ block + 指引 agent 用所在 AI 工具的提问确认工具获用户确认 →
# agent 获确认后 forge hazard confirm 登记限时（5min）标记 → 重试原命令 → 本 hook 见
# 标记放行。
#
# 为什么是 HITL 而非硬 block 或静默放行：硬 block 误伤合法高危操作（删 build 目录），
# 静默放行失守；HITL 要求用户明确知情确认。Forge hook 模型只有 approve/block、调不起
# 各工具私有的确认弹窗，所以靠 block + additionalContext 指引 + forge hazard confirm
# 限时标记闭环（见 internal/hazard + internal/cli/hazard.go）。
#
# 不用 set -e：forge hazard confirmed 在未确认时 exit 1，set -e 会误杀脚本。
# BSD 安全：检测用 case-glob + 独立 grep -qi，不用 grep -E 交替（bash-guard 同款，
# BSD/macOS grep 在 ERE 交替 abort "Unmatched ("）。
#
# Protocol: stdout = AdditionalContext（agent 可见，承载 HITL 指引）；exit 0 = 放行，
# exit 1 = block（runHook 转 decision:block，stdout 进 additionalContext）。
COMMAND="${FORGE_COMMAND:-}"

# 空命令 / 非命令工具：放行
[ -z "$COMMAND" ] && { echo "PASS"; exit 0; }

# 逃生：FORGE_ALLOW_HAZARD=1（测试/CI）直接放行
[ "${FORGE_ALLOW_HAZARD:-0}" = "1" ] && { echo "PASS [hazard-guard] FORGE_ALLOW_HAZARD=1 跳过"; exit 0; }

# 豁免 forge hazard 命令本身：agent 运行 forge hazard confirm "rm -rf x" 登记确认时，
# 这个 Bash 命令的 FORGE_COMMAND 含 "rm -rf" 会被自己拦——必须豁免 forge hazard 前缀。
# forge hazard 只登记/查询标记，不执行传入的命令串，豁免安全。
case "$COMMAND" in
  "forge hazard "*|"forge hazard") echo "PASS"; exit 0 ;;
esac

# --- 高危命令检测 ---
is_hazardous() {
  local cmd="$1"
  local lower
  # 大小写归一 + 换行/CR 先映射成 ;（与 &&/||/|/; 同为段分隔符），再压缩连续空白为单空格
  # （tab→空格保留参数分隔语义，不让 tab 变段分隔符破坏 rm<TAB>-rf 的 flag 匹配）。
  # 换行必须先于 squeeze 映射：tr -s '[:space:]' 会把换行压成空格，跨行复合命令
  # rm -rf /tmp/x<NL>rm -rf /important 被合并进同一段、段隔离失效（task3 reviewer MINOR-1）。
  lower=$(printf '%s' "$cmd" | tr '[:upper:]' '[:lower:]' | tr '\n\r' ';;' | tr -s '[:space:]' ' ')
  # shred / mkfs：不可逆破坏，整串子串匹配即可（这些 token 不会出现在普通路径里）。
  case "$lower" in
    *shred\ *|*mkfs\ *|*mkfs.*) return 0 ;;
  esac
  # rm 临时目录白名单（/tmp、/var/folders、/private/tmp）下沉到 rm_hit 循环内按段隔离：
  # rm -rf /tmp/x 段放行，但 rm -rf /tmp/x && rm -rf /important 的第二段仍 block。
  # 原全串 case 的 return 1 会吞整条命令（连非白名单 rm -rf 段也放行）——task3 补审发现。
  # rm 递归强删：rm 命令的 flag 簇里同时出现 r 与 f 才算（递归 + 强制）。
  # 不要求 rm 紧跟 flag——'rm -i -rf' / 'rm --one-file-system -rf' 也覆盖（2026-06 审查 S1：
  # 紧跟单簇锚定会漏 rm -i -rf）。合并簇用 '-[a-z]*r[a-z]*f' 匹配单 - 开头短选项
  # （-rf/-fr/-irf/-rfv），不匹配路径 .lark-report（其 -report 的 r 后无 f）；分离簇用
  # ' -r' / ' -f' 前空格锚定，避免旧版裸 '-r' 误中路径 -report（2026-06 .lark-report 事故根因）。
  # 仅 -r/-R 无 -f 不拦：rm 交互式确认仍生效，破坏力低于 rm -rf（设计决策，非漏检）。
  # 长选项 --recursive + --force。BSD 安全：每条独立 grep -qi，无 -E 交替。
  # rm 检测按命令段隔离：flag 只在其所在命令段（&&/||/|/;/换行 分隔的子命令）内查，不扫全命令串。
  # rm 词边界：行首 rm 或非小写字母前导，消除 confirm/perform 等词内 rm 子串（task2）。
  # 段隔离消除 flag 误中：rm x && git checkout fix/hazard-confirm-y 的 -confirm 属 git 段
  # （含 f...r），原实现 flag 扫全串误中（task2 残留：-[a-z]*r[a-z]*f / -[a-z]*f[a-z]*r 命中
  # -confirm/-formatter/-prefix 等跨命令 hyphen-token）。tr 把 &|;换行 统一换行切段，while read 逐段判。
  # BSD 安全：tr char 集合替换；rm-token 与 flag 全独立 grep -qi（BRE），|| / && 是 shell 短路，非 grep -E 交替。
  # printf '%s\n' 补尾部换行：while read 遇 EOF（输入无换行尾）返回非零会丢最后一段，单行 rm -rf x 会被漏检。
  local rm_hit
  rm_hit=$(printf '%s\n' "$lower" | tr '&|;\n' '\n\n\n\n' | while IFS= read -r seg; do
    [ -z "$seg" ] && continue
    if printf '%s' "$seg" | grep -qi '^rm ' || printf '%s' "$seg" | grep -qi '[^a-z]rm '; then
      if printf '%s' "$seg" | grep -qi -- '-[a-z]*r[a-z]*f' || \
         printf '%s' "$seg" | grep -qi -- '-[a-z]*f[a-z]*r' || \
         { printf '%s' "$seg" | grep -qi -- ' -r' && printf '%s' "$seg" | grep -qi -- ' -f'; } || \
         { printf '%s' "$seg" | grep -qi -- '--recursive' && printf '%s' "$seg" | grep -qi -- '--force'; }; then
        # 白名单 arg-aware：rm -rf /tmp/x /important 会删 /important，不能因含 /tmp 子串整体放行。
        # 逐 word 查 rm 的目标参数，全部指向一次性临时区（/tmp、/var/folders、/private/tmp）且无 ..
        # 路径穿越才 continue；任一参数非白名单→不 continue，落 echo H block。只看目标路径不依赖
        # flag 写法，覆盖 rm --recursive --force /tmp/x 长选项形式（reviewer MINOR-2/NIT-2）。
        # -- 终止符（POSIX）：rm -rf -- -sensitive 里 -- 后的 -sensitive 是字面文件名（rm 真删），
        # 不能当 flag 跳过（reviewer MINOR-B）。遇 -- 置 past_dd=1，后续 word 一律按目标查白名单。
        # 已知限制：for word in $seg 是 IFS split 不解析引号，rm -rf "/tmp/my dir" 含空格路径会因
        # word 断开而 all_tmp=0 误 block（reviewer MINOR-A，方向安全可 confirm 豁免，罕见不修）。
        # rm -rf 无目标参数时 all_tmp 保持 1 放行——rm 自身 missing operand 报错不删，无危害。
        all_tmp=1
        past_dd=0
        # bash 3.2（macOS CI，2007 年版）case parser 对分支 action 里的复杂命令有 bug：
        # 嵌套 case 报 syntax error near ')' 字符、单行 [[ ]] && cmd ;; 报 syntax error near ';;'
        # ——根因是 pattern 里的 glob '*' 与 action list 里的 glob（*..*）互相干扰，parser
        # 状态错乱。Git Bash 5.x 容忍，本地测试不报，macOS CI 才炸（已踩两次：a6199a4/bab0f6e）。
        # 全改 if [[ ]] + glob（bash 2.0+ 标准；glob 在 [[ ]] 内不进 case parser，绕开 bug）。
        # 语义与原 case 等价：白名单前缀且无 .. → 保持；含 .. 或非白名单 → all_tmp=0。
        # -- 终止符置 past_dd=1（其后 word 按字面目标查，不跳过 - 开头文件名）；rm/sudo/flag 跳过。
        for word in $seg; do
          if [[ $past_dd = 1 ]]; then
            if [[ $word == /tmp/* || $word == /var/folders/* || $word == /private/tmp/* ]]; then
              [[ $word == *..* ]] && all_tmp=0
            else
              all_tmp=0
            fi
            continue
          fi
          if [[ $word == -- ]]; then
            past_dd=1
            continue
          fi
          if [[ $word == rm || $word == sudo || $word == -* ]]; then
            continue
          fi
          if [[ $word == /tmp/* || $word == /var/folders/* || $word == /private/tmp/* ]]; then
            [[ $word == *..* ]] && all_tmp=0
          else
            all_tmp=0
          fi
        done
        [[ $all_tmp = 1 ]] && continue
        echo H
      fi
    fi
  done)
  [ -n "$rm_hit" ] && return 0
  # git 危险推送 / 强制重置。--force-with-lease 是安全版（remote 有新提交自动拒绝），git
  # 官方推荐用以替代 --force——前置分支放行，不与裸 --force 同等硬拦（2026-06 误伤）。
  # case 按序匹配：lease 命令命中首分支即跳出 case，不会落到 --force 分支。
  case "$lower" in
    *git\ push*--force-with-lease*) ;;
    *git\ push*--force*|*git\ push*\ -f*) return 0 ;;
    *git\ push*--delete*) return 0 ;;
    *git\ reset*--hard*) return 0 ;;
  esac
  # SQL 破坏性 DDL / 权限滥用（大小写已归一为 lower）
  case "$lower" in
    *"drop database"*|*"drop table"*|*"drop schema"*) return 0 ;;
    *"truncate"*) return 0 ;;
    *"grant all"*) return 0 ;;
    *"grant"*" to public"*) return 0 ;;
  esac
  # k8s / docker 破坏性操作
  case "$lower" in
    *"kubectl delete"*) return 0 ;;
    *"docker system prune"*|*"docker volume rm"*|*"docker rm "*"-f"*) return 0 ;;
  esac
  # DELETE/UPDATE 无 WHERE 近似检测：两次独立 grep -qi（非 ERE 交替）+ WHERE 取反。
  # 语义复杂边界（表名/字符串里恰好含 where/delete）留给 code-review-gate 审查——
  # hook 只做高危信号预警，宁误拦勿漏。
  if printf '%s' "$lower" | grep -qi 'delete .*from'; then
    printf '%s' "$lower" | grep -qi 'where' || return 0
  fi
  if printf '%s' "$lower" | grep -qi 'update .*set'; then
    printf '%s' "$lower" | grep -qi 'where' || return 0
  fi
  return 1
}

# strip_quotes 剥离命令中引号内内容（含引号本身），用于 context classification：
# 判断危险串是数据（引号内）还是执行。awk 状态机逐字符跟踪单/双引号开合，引号内字符
# 丢弃。BSD/GNU awk 均支持；用 \x27 表示单引号（awk 体内避免直接写 '）。不完美但够用：
# bash -c "rm" 内层引号也会被剥离，由下方 is_exec_wrapped 单独兜底。
strip_quotes() {
  printf '%s' "$1" | awk '
    {
      sq=0; dq=0; out=""
      for(i=1;i<=length($0);i++){
        c=substr($0,i,1)
        if(c=="\x27"){sq=!sq; continue}
        if(c=="\""){dq=!dq; continue}
        if(!sq && !dq) out=out c
      }
      print out
    }'
}

# is_exec_wrapped 判定命令是否把字符串当代码执行——这类即使危险串在引号内也是真高危，
# context classification 不能放行。strip_quotes 会剥离引号内代码串，若不兜底会漏放：
# bash -c "rm -rf" / mysql -e 'DROP TABLE' / python -c "os.remove()" 等。case-glob，BSD
# 安全（无 grep -E 交替）。"| sh" 后用结束/空格锚定，不误伤 "| sha256sum"。
# 注意：is_exec_wrapped 只在 is_hazardous(原命令) 命中后才调，故 -c/-e 的宽匹配只影响
# "已含危险串"的命令，不会误拦正常 python script.py -v 等（那些 is_hazardous 不命中）。
is_exec_wrapped() {
  case "$1" in
    # shell 把字符串当代码执行
    *bash\ -c*|*sh\ -c*|*\ eval\ *|*"eval "*|*xargs\ sh*|*xargs\ bash*|*xargs\ -I*sh*|*xargs\ -I*bash*) return 0 ;;
    # SQL 执行型：-e/-c flag 把后续字符串当 SQL 执行
    *mysql\ *-e*|*mariadb\ *-e*|*psql\ *-c*|*psql\ *-e*) return 0 ;;
    # 代码执行型：-c/-e/-r flag 把后续字符串当代码执行
    *python*\ *-c*|*node\ *-e*|*ruby\ *-e*|*perl\ *-e*|*perl\ *-E*|*php\ *-r*|*lua\ *-e*) return 0 ;;
    # sqlite3 后接引号包裹的 SQL（单/双引号）——直接执行 SQL 的常见形态
    *sqlite3\ *\'*|*sqlite3\ *\"*) return 0 ;;
    # pipe 到 shell 执行
    *"| bash"|*"| bash "*|*"| sh"|*"| sh "*) return 0 ;;
  esac
  return 1
}

if ! is_hazardous "$COMMAND"; then
  echo "PASS"
  exit 0
fi

# --- context classification：危险串是数据（引号内）还是执行 ---
# is_hazardous 命中后，剥离引号再判一次：剥离后不再命中 → 危险串都在引号里（数据），
# 且命令非执行包裹（bash -c/eval/pipe-shell）→ 放行。根治 grep "rm -rf" /
# git commit -m "fix rm -rf bug" 类误判（2026-06 类别级；.lark-report 是其单点表现）。
STRIPPED=$(strip_quotes "$COMMAND")
if [ "$STRIPPED" != "$COMMAND" ] && ! is_hazardous "$STRIPPED" && ! is_exec_wrapped "$COMMAND"; then
  forge hazard log data "$COMMAND" >/dev/null 2>&1 || true
  echo "PASS [hazard-guard] 危险串仅在引号内（数据上下文），放行: $COMMAND"
  exit 0
fi

# --- 命中高危：查是否已 human-in-the-loop 确认（forge hazard confirm 登记） ---
FP=$(forge hazard fingerprint "$COMMAND" 2>/dev/null)
if [ -n "$FP" ] && forge hazard confirmed "$FP" >/dev/null 2>&1; then
  forge hazard log release "$COMMAND" >/dev/null 2>&1 || true
  echo "PASS [hazard-guard] 已确认放行（5min 窗口内）: $COMMAND"
  exit 0
fi

# --- 未确认：block + HITL 指引（落盘 block 事件供审计追溯） ---
forge hazard log block "$COMMAND" >/dev/null 2>&1 || true
echo "FAIL [hazard-guard] 高危操作已拦截（需 human-in-the-loop 确认）"
echo "命令: $COMMAND"
echo "指纹: ${FP:-<unknown>}"
echo ""
echo "如确需执行："
echo "  1. 用你的确认工具（Claude Code→AskUserQuestion；codex/cursor/windsurf→各自提问机制）"
echo "     向用户说明该操作的风险，并获得用户的明确确认"
echo "  2. 获确认后运行: forge hazard confirm --fingerprint \"$FP\""
echo "     （回传上方 hex 指纹；勿复制命令串——shell 会吃掉其中的引号致指纹失真）"
echo "  3. 逐字重试原命令（5min 内同指纹自动放行）——重试时勿加 && echo/&& ls 等验证"
echo "     后缀：命令串变了→指纹变→重新被拦（这是 confirm 后仍被反复拦截的最常见原因）"
echo ""
echo "测试/CI 可设 FORGE_ALLOW_HAZARD=1 临时跳过拦截。"
exit 1
`

const FileSentinelHook = `#!/bin/bash
# file-sentinel.sh — PostToolUse hook for Bash.
# Detects unauthorized file changes after Bash execution.
# Compares against PreToolUse bash-guard snapshot and quarantines violations.
# NEVER deletes user files — always moves to .forge/quarantine/ for recovery.
# Only checks source code files and Forge config — ignores all other changes.
set -eo pipefail

TASK_REF="${FORGE_TASK_REF:-}"
SESSION_ID="${FORGE_SESSION_ID:-default}"
SNAPSHOT_FILE="${TMPDIR:-/tmp}/forge-snapshot-${SESSION_ID}"
# Defensive: never let SNAPSHOT_FILE be empty — an empty value would make a
# redirect write to a literal/misdirected filename.
[ -z "${SNAPSHOT_FILE:-}" ] && SNAPSHOT_FILE="${TMPDIR:-/tmp}/forge-snapshot-unknown"
FORGE_CMD_FILE="${TMPDIR:-/tmp}/forge-cmd-${SESSION_ID}"
WRITE_FLAG_FILE="${TMPDIR:-/tmp}/forge-write-${SESSION_ID}"
[ -z "${WRITE_FLAG_FILE:-}" ] && WRITE_FLAG_FILE="${TMPDIR:-/tmp}/forge-write-unknown"

# No snapshot from PreToolUse → nothing to compare (fail-open). Also clear the
# write-flag so a stale flag from this session never leaks into a later
# invocation that has no matching bash-guard snapshot.
if [ ! -f "$SNAPSHOT_FILE" ]; then
  rm -f "$WRITE_FLAG_FILE" 2>/dev/null || true
  echo "PASS"
  exit 0
fi

# Not a git repo → cannot diff, pass silently
git rev-parse --git-dir >/dev/null 2>&1 || {
  rm -f "$SNAPSHOT_FILE" "$FORGE_CMD_FILE" 2>/dev/null
  echo "PASS"
  exit 0
}

# Source code extension pattern
SRC_EXT='\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$'
# Forge config pattern
CFG_EXT='(\.forge/(hooks|tasks|specs|reviews|gates)/|\.claude/settings)'

# Get current changed source files only (not all files)
CURRENT_ALL=$(
  {
    git diff --name-only 2>/dev/null || true
    git ls-files --others --exclude-standard 2>/dev/null || true
  } | grep -E "${SRC_EXT}|${CFG_EXT}" | sort -u || true
)

# Read pre-Bash snapshot
BEFORE_ALL=$(cat "$SNAPSHOT_FILE" 2>/dev/null | grep -E "${SRC_EXT}|${CFG_EXT}" | sort -u || true)

# EMPTY snapshot = bash-guard's git commands failed silently (errors swallowed
# by 2>/dev/null: cwd drift, index.lock, Windows newline, session id drift).
# With no reliable baseline we CANNOT compute a diff — the else-branch below
# would treat the ENTIRE working tree as "new violations" and quarantine +
# run "git checkout --" to discard the user's existing uncommitted source. That is
# fail-destructive on an unprovable violation. Fail-open instead: WARN only.
# (P0 DevWorkbench incident 2026-06: 71 files moved to quarantine, working
# tree .tsx/.rs silently restored to HEAD because BEFORE_ALL was empty.)
if [ -z "$BEFORE_ALL" ] && [ -n "$CURRENT_ALL" ]; then
  rm -f "$SNAPSHOT_FILE" "$FORGE_CMD_FILE" "$WRITE_FLAG_FILE" 2>/dev/null || true
  echo "WARN [file-sentinel] PreToolUse snapshot empty (git failed silently) while working tree has uncommitted source/config changes — cannot compute reliable diff, skipping quarantine to protect existing work."
  echo "PASS"
  exit 0
fi

# Check if this was a forge command
IS_FORGE_CMD=0
[ -f "$FORGE_CMD_FILE" ] && IS_FORGE_CMD=1

# Read whether THIS Bash command was a write command (recorded by bash-guard).
# Secondary gate for the source-change branch below.
IS_WRITE_CMD=0
[ -s "$WRITE_FLAG_FILE" ] && IS_WRITE_CMD=1

# Clean up
rm -f "$SNAPSHOT_FILE" "$FORGE_CMD_FILE" "$WRITE_FLAG_FILE" 2>/dev/null || true

# No current changes at all → pass
[ -z "$CURRENT_ALL" ] && { echo "PASS"; exit 0; }

# Find NEW changes: lines in CURRENT but not in BEFORE
# Use grep -Fxv for reliable line-by-line exact match
if [ -n "$BEFORE_ALL" ]; then
  NEW_CHANGES=$(printf '%s\n' "$CURRENT_ALL" | grep -Fxvf <(printf '%s\n' "$BEFORE_ALL") 2>/dev/null || true)
else
  NEW_CHANGES="$CURRENT_ALL"
fi

# No new changes → pass
[ -z "$NEW_CHANGES" ] && { echo "PASS"; exit 0; }

# Categorize new changes
SOURCE_CHANGES=$(printf '%s' "$NEW_CHANGES" | grep -E "$SRC_EXT" || true)
CONFIG_CHANGES=$(printf '%s' "$NEW_CHANGES" | grep -E "$CFG_EXT" || true)

# No protected changes → pass
[ -z "$SOURCE_CHANGES" ] && [ -z "$CONFIG_CHANGES" ] && { echo "PASS"; exit 0; }

# Helper: quarantine files — NEVER delete, always preserve for recovery.
# Tracked files: moved to quarantine, then HEAD restored from git.
# Untracked files: moved to quarantine (not in git, so can't restore from HEAD).
# All files are recoverable: cp -r .forge/quarantine/<session-id>/* .
quarantine_files() {
  local files="$1"
  local quarantine_base="${FORGE_QUARANTINE_DIR:-.forge/quarantine}"
  local qdir="${quarantine_base}/${SESSION_ID}"
  mkdir -p "$qdir" 2>/dev/null || true

  local quarantined=""
  local failed=""
  while IFS= read -r f; do
    [ -z "$f" ] && continue

    # Preserve directory structure in quarantine
    local rel_dir
    rel_dir=$(dirname "$f")
    if [ "$rel_dir" != "." ]; then
      mkdir -p "${qdir}/${rel_dir}" 2>/dev/null || true
    fi

    # Move to quarantine FIRST — always preserves content for recovery
    if ! mv "$f" "${qdir}/${f}" 2>/dev/null; then
      failed="${failed} ${f}"
      continue
    fi

    # For tracked files, restore HEAD version from git
    if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
      git checkout -- "$f" 2>/dev/null || true
    fi

    quarantined="${quarantined} ${f}"
  done <<< "$files"

  QUARANTINED="$quarantined"
  QUARANTINE_FAILED="$failed"
  QUARANTINE_DIR="$qdir"
}

# Self-protection: quarantine config changes (unless forge command was detected)
if [ -n "$CONFIG_CHANGES" ] && [ $IS_FORGE_CMD -eq 0 ]; then
  quarantine_files "$CONFIG_CHANGES"
  MSG="FAIL [file-sentinel] Quarantined unauthorized changes to Forge config:${QUARANTINED}."
  [ -n "$QUARANTINE_FAILED" ] && MSG="${MSG} FAILED to quarantine:${QUARANTINE_FAILED}."
  MSG="${MSG} Files in ${QUARANTINE_DIR}/. Recover: cp -r ${QUARANTINE_DIR}/* ."
  echo "${MSG} Use forge commands instead."
  exit 1
fi

# Source code changes without active task → quarantine. SECONDARY GATE: only
# when THIS Bash command was actually a write command. A read-only command
# (ls/cat/git diff/find) cannot produce source changes — if source changes
# appear under one, the snapshot diff is unreliable (partial snapshot, external
# editor, or another process), not a Bash-written violation. Fail-open to avoid
# destroying existing uncommitted work on an unprovable violation.
if [ -z "$TASK_REF" ] && [ -n "$SOURCE_CHANGES" ]; then
  if [ $IS_WRITE_CMD -eq 0 ]; then
    echo "WARN [file-sentinel] Source changes present but the Bash command was read-only — diff unreliable (partial snapshot / external interference), skipping quarantine to protect existing work:${SOURCE_CHANGES}"
    echo "PASS"
    exit 0
  fi
  quarantine_files "$SOURCE_CHANGES"
  MSG="FAIL [file-sentinel] Quarantined unauthorized code changes (no active task):${QUARANTINED}."
  [ -n "$QUARANTINE_FAILED" ] && MSG="${MSG} FAILED to quarantine:${QUARANTINE_FAILED}."
  MSG="${MSG} Files in ${QUARANTINE_DIR}/. Recover: cp -r ${QUARANTINE_DIR}/* ."
  echo "${MSG} Start a task: forge task start --ref <type>/<desc> --branch"
  exit 1
fi

echo "PASS"`

const ToolTrackHook = `#!/bin/bash
# tool-track.sh — PostToolUse Read hook (silent toollog recorder).
# Records Read calls into toollog (via the forge hook dispatch in hook.go) so
# the read-before-edit gate at task-verify can confirm the agent read code
# before editing it. Deliberately minimal and silent: Read-only matcher, no
# checklog entry, no stderr output — the toollog append is the only effect.
#
# This restores the Read-recording the tool-track hook provided before 644b142
# removed it (alongside the untrusted tool-selection dimension). That removal
# left the gate with no Read data, making it always-fail on any task with
# edits. This minimal version records Read only — not Bash/Grep/Glob — to
# avoid re-introducing the toollog volume that motivated the original deletion.
echo "PASS"
`

const SkillScanHook = `#!/bin/bash
# skill-scan.sh — SessionStart hook (advisory, non-blocking).
# 会话开始扫描 skill 目录安全性（forge audit 19 规则：prompt 注入/数据外发/危险
# 代码/系统提示泄露）。补 install 门控的缺口：skill 经 install 之外的路径进入 agent
# 环境（手动 cp/clone、git pull 更新、external junction 如 lark-*）时 install 门控
# 扫不到，SessionStart 是天然检查点，覆盖所有来源。advisory：stdout PASS detail
# 列出有 finding 的风险 skill（含 MEDIUM），不阻塞会话（advisory 方向），
# 由 agent/用户自检是否使用。全局 hook：不依赖 forge project。
SCAN_DIR="$HOME/.claude/skills"
if [ ! -d "$SCAN_DIR" ]; then
  echo "PASS [skill-scan] no ~/.claude/skills (advisory)"
  exit 0
fi

# 捕获 stderr 到临时文件而非直接丢弃：exit code 只说"scan 崩了"，stderr（panic/错误）
# 说"怎么崩的"，是崩溃分支的诊断线索（review suggest#2）。trap EXIT 自动清理临时文件。
# mktemp 失败（极罕见）降级到 /dev/null 而非空串——2>"" 会触发 ambiguous redirect 让
# forge 调用本身失败（其 exit 被误报为崩溃）。trap 只清理 mktemp 创建的真实临时文件，
# 绝不 rm /dev/null（设备文件，rm 可能报权限噪声或被误判）。
STDERR_FILE=$(mktemp 2>/dev/null) || STDERR_FILE="/dev/null"
trap '[ -n "$STDERR_FILE" ] && [ "$STDERR_FILE" != "/dev/null" ] && rm -f "$STDERR_FILE"' EXIT

# --gate 编码 HIGH/CRITICAL 为 exit 4；正常 exit 0。两者都表示 scan 成功执行。
# audit scan 输出每行一个 skill：✓/✗ name score=X SEV (rec, N finding)。
OUTPUT=$(forge skills audit scan --canonical "$SCAN_DIR" --gate 2>"$STDERR_FILE")
CODE=$?

# 诚实信号：scan 成功 = exit 0 或 4；其他 exit code = 崩溃；空输出 = 未产生结果。
# 两者都报"未完成"，避免 scan 失败却报 "all SAFE" 的假阴性（advisory fail-open 的
# 副作用——宁放过不阻塞是对的，但报"全部安全"是撒谎；正确是报"没扫成"）。
if { [ "$CODE" != 0 ] && [ "$CODE" != 4 ]; } || [ -z "$OUTPUT" ]; then
  # stderr 尾部（≤400 字节）作为诊断线索——exit code 定性"崩了"，stderr 定位"为何"。
  STDERR_TAIL=$(tail -c 400 "$STDERR_FILE" 2>/dev/null | tr -d '\r')
  if [ -n "$STDERR_TAIL" ]; then
    echo "PASS [skill-scan] Advisory: skill 安全扫描未完成（forge audit scan exit=$CODE，stderr: $STDERR_TAIL）。建议手动 'forge skills audit scan' 核查（forge 不阻塞）。"
  else
    echo "PASS [skill-scan] Advisory: skill 安全扫描未完成（forge audit scan exit=$CODE），建议手动 'forge skills audit scan' 核查（forge 不阻塞）。"
  fi
  exit 0
fi

# scan 成功：✗ 行 = 有 finding 的 skill（含 MEDIUM CAUTION，advisory 列全部风险）。
# ✗ 为辅助列举（风险存在性已由 exit code 保证 scan 确实执行了），依赖 forge audit
# 文本格式（内部耦合，可控）。不用 --json：bash 解析 JSON 不比 grep ✗ 更稳，且更复杂。
RISKS=$(printf '%s' "$OUTPUT" | grep -E '✗' || true)

if [ -n "$RISKS" ]; then
  # 截断避免超 AdditionalContext 限制（forge hook 包装上限 9500 字符）。
  RISKS_SUMMARY=$(printf '%s' "$RISKS" | head -10 | tr '\n' ' ' | cut -c1-600)
  echo "PASS [skill-scan] Advisory: 发现风险 skill——${RISKS_SUMMARY}请核查（forge 不阻塞，由 agent/用户自检是否使用）。"
else
  echo "PASS [skill-scan] all skills SAFE (advisory, 19 rules)"
fi
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

# 跑守护测试，捕获 exit code（不用 set -e，否则 go test 失败会杀脚本拿不到 CODE）。
OUTPUT=$(go test ./internal/ci/ -count=1 2>&1)
CODE=$?

if [ "$CODE" -eq 0 ]; then
  echo "PASS [workflow-test-guard] workflow 配置变更后 internal/ci 守护测试全绿"
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

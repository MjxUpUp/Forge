package hooks

// The hook scripts embedded by forge init.
// Written to .forge/hooks/ at project initialization.
//
// Protocol: bash scripts output plain text to stdout.
// - A line starting with PASS = check passed; the rest of the line is optional detail.
// - A line starting with FAIL = check failed; the rest of the line is the reason.
// - For multiple lines, the last PASS/FAIL line decides the result.
// - Any output to stderr is captured for debugging.
// The Go side wraps the result into structured JSON for Claude Code.
//
// The Go side extracts the tool_input fields into env vars (FORGE_FILE_PATH, FORGE_CONTENT,
// FORGE_TOOL_NAME), so bash scripts do not need to parse JSON themselves.
//
// forge init 嵌入的 hook 脚本。
// 在项目初始化时写入 .forge/hooks/。
//
// 协议：bash 脚本向 stdout 输出纯文本。
// - 以 PASS 起首的行 = 检查通过，行其余部分为可选 detail。
// - 以 FAIL 起首的行 = 检查失败，行其余部分为原因。
// - 多行时以最后一条 PASS/FAIL 行决定结果。
// - 任何到 stderr 的输出都会被捕获用于调试。
// Go 侧把结果包装成结构化 JSON 给 Claude Code。
//
// Go 侧把 tool_input 字段提取到 env var（FORGE_FILE_PATH、FORGE_CONTENT、
// FORGE_TOOL_NAME），bash 脚本无需自己解析 JSON。

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
_TOUCHED="${TMPDIR}/forge-source-touched-${SESSION_ID}"
if [ ! -f "$_TOUCHED" ]; then
  echo "PASS [auto-compile] research-mode session, advisory suppressed (set by Edit|Write of source)"
else
  if [ "$TOUCHED_SOURCE" = "1" ]; then
    echo "PASS [auto-compile] Advisory: 已修改源码——请用你技术栈的编译命令确认编译通过（go build ./... / cargo check / mvn -o compile / tsc --noEmit 等）。编译报错时加载 compile-fix-loop skill（/compile-fix-loop）：编译错误修复闭环方法论，按语言分类定位根因。forge 不再强制编译，适配 loop engineering，由 agent 自检。"
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
  MESSAGES="${MESSAGES}[task-gate] Task verify gate not yet passed. "
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
# and checklog (trace-queryable). Detail is a fixed string — MESSAGES may carry
# quotes/paths that would break the JSON line, so it goes to stderr only.
# level 显式写 "advisory"（checklog Level 字段）：Detail 无 ADVISORY: 前缀，
# 读取侧 derive 只会给 pass——本条目实为 advisory 语义，必须显式标注。
if [ -n "$MESSAGES" ]; then
  echo "[task-verify] ${MESSAGES}" >&2
  _NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)
  if [ -n "$_NOW" ]; then
    printf '{"check":"task-verify","passed":true,"checked":false,"detail":"advisory: non-blocking issues surfaced to stderr","level":"advisory","recorded_at":"%s"}\n' \
      "$_NOW" >> "$_DATA_DIR/checklog.jsonl" 2>/dev/null || true
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

# dogfood 5.1：session-level source-touched marker. Setting it here (when task-guard
# has confirmed FILE_PATH is source code) means auto-compile + bash-guard can
# distinguish research-only sessions (AgentFare pattern: no source touches ⇒
# silent) from dev sessions (marker set ⇒ normal advisory). Per-session, keyed
# by FORGE_SESSION_ID. The marker is set BEFORE the no-task decisions below so
# downstream hooks in the same invocation also see it as set.
: "${TMPDIR:=/tmp}"
_SESSION_ID="${FORGE_SESSION_ID:-default}"
_TOUCHED_MARKER="${TMPDIR}/forge-source-touched-${_SESSION_ID}"
touch "$_TOUCHED_MARKER" 2>/dev/null || true

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
  # On master/main or auto-creation failed: warn but allow.
  # dogfood 3.1：每源文件 Edit 注入 WARN 刷屏（AgentWorld 139 次）。会话级标记文件，
  # 每会话首条 WARN 提示改动不被任务追踪，之后静默。标记键控 FORGE_SESSION_ID 隔离并发会话。
  NOWARN_FILE="${TMPDIR:-/tmp}/forge-taskguard-nowarn-${FORGE_SESSION_ID:-default}"
  if [ ! -f "$NOWARN_FILE" ]; then
    touch "$NOWARN_FILE" 2>/dev/null || true
    echo "WARN [task-guard] No active task. Source changes are allowed but not tracked by a Forge task.（本会话仅提示一次）"
  fi
  exit 0
fi

echo "PASS"
`

const FreezeGuardHook = `#!/bin/bash
# freeze-guard.sh — PreToolUse hook for Write|Edit（硬阻断）。
# on-demand-guards /freeze 目录锁定的 forge 侧落地：` + "`forge freeze <路径>...`" + ` 激活后，
# 写入冻结路径之外的文件一律 BLOCKED——「只改这里别动其他」的 session 级硬护栏，
# 不依赖 agent 每回合自检（prompt 型护栏在长会话/压缩后必漂移，恰是它防的场景）。
#
# 判定与路径归一化全在 Go 端（forge freeze check → internal/freeze）：多路径、
# 相对路径归一化、Windows 大小写不敏感都有单测覆盖；bash 只做退出码转发——
# 路径比较逻辑进 bash 是 BSD/大小写/symlink 三重漂移源，故保持薄。
#
# 顺序契约：本 hook 在 ForgeHookSpec 的 PreToolUse Write|Edit matcher 里排在
# task-guard 之前——freeze 激活时优先给出 freeze 原因，而不是 task 告警。
#
# 退出码契约（与 freeze check 对齐）：0=放行；1=阻断（stdout 原因进 FAIL 行）；
# 其他=check 自身异常 → fail-open（护栏故障不得硬停每次编辑）。
# Protocol: stdout FAIL 行 → runHook 转 decision:block 进 additionalContext。
set -eo pipefail

FILE_PATH="${FORGE_FILE_PATH:-}"
# 无文件路径（非文件工具 / batch 模式）— 放行
[ -z "$FILE_PATH" ] && exit 0

CODE=0
OUT=$(forge freeze check --path "$FILE_PATH" 2>/dev/null) || CODE=$?
case "$CODE" in
  0) exit 0 ;;
  1)
    [ -z "$OUT" ] && OUT="目录已 freeze，该路径不在允许范围内（forge freeze --status 查看，forge freeze --off 解除）"
    echo "FAIL [freeze-guard] $OUT"
    exit 1
    ;;
esac

# check 自身异常（非 freeze 判定结论）→ fail-open 放行并留可见警告
echo "PASS [freeze-guard] forge freeze check 异常（exit $CODE），fail-open 放行"
exit 0
`

const ReadBeforeEditHook = `#!/bin/bash
# read-before-edit.sh — PreToolUse hook for Write|Edit (方案2 shift-left).
# Blocks editing a source file NOT Read this session — immediate feedback at
# Edit time, not deferred to the task-verify work-activity gate. Catches the
# edit-without-read anti-pattern (lazy-fix root cause: agent edited from
# memory; the Edit's old_string matched by luck; a wrong fix shipped).
#
# Why this differs from the former advisory read-check hook: that hook was advisory +
# used a GLOBAL edit/read ratio (>1.0) — high false-positive (read 2 files,
# tweak 5 = ratio 2.5 but fine), so it was noise and got sunk to skill text
# (which then never fired — advisory-only checks don't drive behavior in practice).
# THIS hook is per-file precise (was THIS exact file Read), HARD with
# exemptions, and honors the escape hatch. The disk reads-log side-channel
# also survives compaction within a session (reads before a compact still
# count), removing the biggest false positive of a context-based check.
#
# Exemptions mirror task-guard: non-source, test files, new files (Write
# creating). Escape: FORGE_WORK_ACTIVITY=disable (the dispatcher surfaces a
# per-task override as this env) — downscored to Strength Weak at the gate.
# Only enforces inside an active task: quick non-task edits aren't tracked.
set -eo pipefail

FILE_PATH="${FORGE_FILE_PATH:-}"
TASK_REF="${FORGE_TASK_REF:-}"
READS_FILE="${FORGE_READS_FILE:-}"

# Only enforce inside an active task (mirrors task-guard scoping; quick
# non-task edits aren't Forge-tracked and the reads log may be sparse).
[ -z "$TASK_REF" ] && exit 0
# Non-file tool — allow.
[ -z "$FILE_PATH" ] && exit 0

# Escape hatch: work-activity disabled (per-task override OR global env). The
# dispatcher surfaces a per-task override as FORGE_WORK_ACTIVITY so this single
# check honors both. Escape is downscored (UsedEscapeHatch → Strength Weak at
# the work-activity gate) so it isn't free.
[ "$FORGE_WORK_ACTIVITY" = "disable" ] && exit 0

# Not source code — allow (mirrors task-guard source extension set).
printf '%s' "$FILE_PATH" | grep -qE '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || exit 0
# Test files — always allow (TDD workflow).
printf '%s' "$FILE_PATH" | grep -qE '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/|__tests__/)' && exit 0

# New file (not yet on disk) — allow: Write creating a file can't have been
# Read, and the agent authors fresh content rather than editing blind.
[ -f "$FILE_PATH" ] || exit 0

# No reads log recorded this session → any Edit of an existing source file is
# editing blind → block (this IS the signal we want, not a false positive).
if [ -z "$READS_FILE" ] || [ ! -f "$READS_FILE" ]; then
  echo "FAIL [read-before-edit] $FILE_PATH 未在本会话 Read 过——Edit 需精确匹配旧文本，未读即凭记忆盲改——Edit 的 old_string 撞中即错改入库。先 Read 该文件再 Edit。批量/重构逃生：forge task override --work-activity disable（降 evidence 强度到 Weak）。"
  exit 1
fi

# Exact-line match against the per-session reads log. FORGE_FILE_PATH is the
# repo-relative path the dispatcher normalized (same form appended on Read).
if grep -qxF "$FILE_PATH" "$READS_FILE"; then
  echo "PASS"
  exit 0
fi

echo "FAIL [read-before-edit] $FILE_PATH 未在本会话 Read 过——Edit 需精确匹配旧文本，未读即凭记忆盲改——Edit 的 old_string 撞中即错改入库。先 Read 该文件再 Edit。批量/重构逃生：forge task override --work-activity disable（降 evidence 强度到 Weak）。"
exit 1
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

# Per-invocation pairing: the session-keyed base names above would be shared by
# every Bash call in the session, so two PARALLEL Bash calls would overwrite
# each other's snapshot / write-flag and file-sentinel could consume a
# sibling's pairing (wrong baseline, wrong secondary gate). Keying the
# authoritative copies with THIS hook process's PID ($$) makes each
# PreToolUse→PostToolUse pair distinct; file-sentinel globs the newest
# unconsumed per-invocation snapshot. This per-invocation channel is the ONLY
# one: both hooks run embedded from the same forge binary (forge hook <name>
# → EmbeddedContent), so there is no cross-version or external consumer to keep
# a legacy session-keyed copy in sync for.
SNAP_INV="${SNAPSHOT_FILE}-$$"
FLAG_INV="${WRITE_FLAG_FILE}-$$"

# forge_cfg_manifest records a content-hash manifest of the config files the
# self-protection layer guards — .forge/hooks/* and .claude/settings* — WITHOUT
# relying on git. These paths are gitignored and untracked by design, so the
# git-based snapshot below (git diff / git ls-files --others --exclude-standard)
# can never see a Bash rewrite of them: the CONFIG quarantine branch of
# file-sentinel would be blind on exactly its primary targets. file-sentinel
# regenerates this manifest and compares; any drift is treated as a config
# change. Hash: md5sum (GNU/Git-Bash) → md5 -q (BSD) → byte size (last resort).
forge_cfg_manifest() {
  local cf _h
  for cf in .forge/hooks/* .claude/settings*; do
    [ -f "$cf" ] || continue
    _h=$(md5sum "$cf" 2>/dev/null | awk '{print $1}')
    [ -z "$_h" ] && _h=$(md5 -q "$cf" 2>/dev/null)
    [ -z "$_h" ] && _h="size-$(wc -c < "$cf" 2>/dev/null | tr -d ' ')"
    [ -n "$_h" ] && printf '%s  %s\n' "$_h" "$cf"
  done
}

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
  # boundaries without ERE alternation. Tokens match by BASENAME (${tok##*/})
  # so absolute paths (/bin/cp, /usr/bin/tar) are caught; archive extractors
  # (tar/unzip/7z...) write whole trees without any redirect token, and the
  # decompressors (gunzip/bunzip2/unxz) rewrite the file in place.
  # set -f (noglob) inside a subshell: an unquoted * in the command would
  # otherwise glob-expand against the caller's cwd and corrupt the token scan;
  # the subshell confines the option change.
  if (
    set -f
    for tok in $cmd; do
      case "${tok##*/}" in
        cp|mv|dd|install|rsync|scp|cpio|wget|tee|tar|unzip|unrar|7z|7za|7zr|gunzip|bunzip2|unxz) exit 0 ;;
      esac
    done
    exit 1
  ); then
    return 0
  fi
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
  # Known gaps — contract: 识别不到 ⇒ 放行. This gate is a heuristic first line
  # (drives the no-task WARN + file-sentinel's secondary gate); file-sentinel's
  # snapshot diff is the backstop. Not detected by design: interpreter inline
  # writes (python -c / node -e / ruby -e writing files), pipes through another
  # process (curl | sh), base64 -d / xxd -r decoders, git checkout/restore of
  # tracked paths, and redirect targets carrying no dot or slash.
  return 1
}

# --- Forge command detection (for file-sentinel exemption) ---
# Strict whole-command form, not a substring: the whitespace-trimmed command
# must START with "forge " (or be exactly "forge") AND carry no redirect or
# command separator (> | ; & backtick $(). The old substring match (" forge "
# anywhere) let 'echo evil > .claude/settings.json && forge --version' walk
# straight through the CONFIG-quarantine exemption — harmless while the CONFIG
# branch was blind to the gitignored .claude/settings* / .forge/hooks/*, but a
# real bypass now that the .cfg manifest covers them. case-glob only, no
# grep -E — keeps this BSD-safe. (The backtick literal cannot appear in this
# Go raw string, so it is matched via a printf-built variable.)
IS_FORGE_CMD=0
_FORGE_TRIMMED="${COMMAND#"${COMMAND%%[![:space:]]*}"}"
_FORGE_TRIMMED="${_FORGE_TRIMMED%"${_FORGE_TRIMMED##*[![:space:]]}"}"
case "$_FORGE_TRIMMED" in
  forge|"forge "*)
    _BT=$(printf '\140')
    case "$_FORGE_TRIMMED" in
      *">"*|*"|"*|*";"*|*"&"*|*"${_BT}"*|*'$('*) : ;;
      *) IS_FORGE_CMD=1 ;;
    esac
    ;;
esac

# --- Snapshot file state (always — file-sentinel is defense-in-depth) ---
# Snapshot EVERY Bash command, not just detected writes. file-sentinel's
# unauthorized-change detection (an external process rewriting project-level
# ConfigDir config like .forge/hooks/*.sh or .claude/settings*.json, or planting
# untracked source, during an otherwise read-only ls/cat) needs a pre-command
# baseline for every command; gating the snapshot on write-detection blinds it
# (regression caught by TestHook_FileSentinel_QuarantinesBashWrittenSource). The
# 4 git calls per command are the cost of that defense — accepted over the false
# economy of skipping them.
#
# refactor-data-home commit D: gates/tasks/specs/reviews 迁用户级 DataDir
# （~/.forge 不在 git），file-sentinel 基于 git diff 管不到 DataDir 路径——A6
# （守 gates/status.json 不被 Bash 篡改）随之失效，见
# TestHook_FileSentinel_GateStatusBeyondGitDiff（负向，钉死该缺口）。
#
# git diff --cached is part of the snapshot AND of file-sentinel's current
# state (kept symmetric there): a write-then-git-add in one command must not
# escape detection through the staged tree.
#
# Three-way enumeration with PER-COMMAND exit codes captured (a piped brace
# group would run in a subshell and lose the status). The .ok completion
# marker below is written ONLY when every enumeration succeeded (plus a
# git-dir liveness probe): .ok must mean "git enumeration succeeded", not
# "bash-guard reached the marker line". A transient git failure at snapshot
# time (index.lock, cwd drift) with git healthy again at sentinel time would
# otherwise yield an EMPTY baseline WITH .ok → file-sentinel reads the entire
# working tree as new violations → mass quarantine — the exact P0 scenario the
# .ok distinction exists to prevent. On any failure the marker stays absent and
# file-sentinel takes its snapshot-failed fail-open WARN branch (its designed
# purpose).
SNAP_OK=1
G1=$(git diff --name-only 2>/dev/null) || SNAP_OK=0
G2=$(git diff --cached --name-only 2>/dev/null) || SNAP_OK=0
G3=$(git ls-files --others --exclude-standard 2>/dev/null) || SNAP_OK=0
git rev-parse --git-dir >/dev/null 2>&1 || SNAP_OK=0
{
  [ -n "$G1" ] && printf '%s\n' "$G1"
  [ -n "$G2" ] && printf '%s\n' "$G2"
  [ -n "$G3" ] && printf '%s\n' "$G3"
  true
} | sort -u > "$SNAP_INV" 2>/dev/null || true

# Snapshot completion marker. file-sentinel uses it to tell a legitimate EMPTY
# baseline (clean working tree — the common case) apart from a silently failed
# snapshot: marker present + empty list = clean baseline → normal NEW_CHANGES
# flow; marker missing + snapshot file present = failed snapshot → fail-open
# WARN. Written ONLY when SNAP_OK=1 (all three enumerations + the liveness
# probe succeeded) — see the enumeration comment above.
if [ "$SNAP_OK" -eq 1 ]; then
  : > "${SNAP_INV}.ok" 2>/dev/null || true
fi

# Git-independent config manifest (see forge_cfg_manifest above): covers the
# gitignored self-protection targets the git lists can never see.
forge_cfg_manifest > "${SNAP_INV}.cfg" 2>/dev/null || true

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
  printf '1' > "$FLAG_INV" 2>/dev/null || true
else
  : > "$FLAG_INV" 2>/dev/null || true
fi

# --- WARN on write without active task (allowed, just not tracked) ---
# dogfood 3.1：每写命令注入 WARN 刷屏。会话级标记文件，每会话首条提示，之后静默。
# 只读命令（ls/cat/grep/git status）IS_WRITE_CMD=0 不进此分支，本就静默。
#
# dogfood 5.1：source-touched session-level marker. 本会话从未 Edit|Write 源码
# （task-guard 不会进 no-task 分支因此不会设置 marker），此 Bash 写入发生在
# 纯调研/审查场景，直接静默——避免 AgentFare 模式每会话首条 WARN 噪音。
# 一旦看到 task-guard 设的 marker，下面的 NOWARN_FILE 抑制 + 首条 WARN 流程恢复。
_TOUCHED_MARKER="${TMPDIR:-/tmp}/forge-source-touched-${SESSION_ID}"
if [ $IS_WRITE_CMD -eq 1 ] && [ -z "$TASK_REF" ]; then
  if [ ! -f "$_TOUCHED_MARKER" ]; then
    exit 0
  fi
  NOWARN_FILE="${TMPDIR:-/tmp}/forge-bashguard-nowarn-${SESSION_ID}"
  if [ ! -f "$NOWARN_FILE" ]; then
    touch "$NOWARN_FILE" 2>/dev/null || true
    echo "WARN [bash-guard] Bash write without active task. Changes are allowed but not tracked.（本会话仅提示一次）"
  fi
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

# FORGE_ALLOW_HAZARD env 豁免已移除（周复盘 2026-08：agent 自我放行滥用——hook 只拦
# 别人拦不了自己，且行内前缀形式 FORGE_ALLOW_HAZARD=1 cmd 由宿主进程持有、hook 进程
# 拿不到 env，两种形式行为不一致）。confirm 链（events.jsonl 审计 + 5min TTL）是唯一
# 放行路径，测试/CI 同样走 forge hazard confirm。

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
    # dogfood 3.2：裸 "truncate" 子串误伤路径片段（cd truncate-dir / --no-truncate flag）。
    # 收窄到 SQL DDL 语境。MySQL/PG 的 TABLE 关键字可选（TRUNCATE users ≡ TRUNCATE TABLE
    # users，都破坏性清表），故第三分支匹配 "truncate " + 标识符首字符 [a-zA-Z_]（表名起首）；
    # 不匹配 coreutils truncate（-s/--size，- 非 alpha/_）与连字符路径片段（truncate-dir 无空格）。
    *"truncate table"*|*"truncate database"*|*"truncate "[a-zA-Z_]*) return 0 ;;
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
      sq=0; dq=0; out=""; prev=""
      for(i=1;i<=length($0);i++){
        c=substr($0,i,1)
        if(c=="\x27"){sq=!sq; prev=c; continue}
        if(c=="\""){dq=!dq; prev=c; continue}
        # dogfood 3.2：# 注释行（非引号内、词边界处）剥到行尾。electron-builder "# Clean up"
        # 含危险串的注释被当执行误拦。prev 判定词边界（空格/行首/tab/;/|/&/( ）——# 紧跟
        # 非空白（如 foo#bar）是字面 #，非注释，不剥（其后的危险串仍被 is_hazardous 命中）。
        if(!sq && !dq && c=="#" && (prev==" "||prev==""||prev=="\t"||prev==";"||prev=="|"||prev=="&"||prev=="(")) break
        if(!sq && !dq) out=out c
        prev=c
      }
      print out
    }'
}

# is_interp_delete_bypass 收窄解释器内联删除旁路：python/node/ruby/perl 的 -c/-e
# 代码体不含 rm 等危险串，is_hazardous 对 python -c "import os;os.remove(...)" 直接
# PASS——解释器旁路（周复盘失守 c：is_exec_wrapped 只在 is_hazardous 命中后才调，
# 覆盖不到无危险串的删除代码）。在 PASS 前前置判定：命中即视为 hazardous，落入下方
# STRIPPED / 拦截流程。保守方向与 is_exec_wrapped 同款：-c/-e 宽匹配只影响"已含文件
# 删除调用"的命令，正常 python script.py -v / node app.js 不命中。
# 已知限制：echo "python -c os.remove" 这类把解释器命令当数据的 echo 会被宽匹配
# 误拦（is_exec_wrapped 同款权衡，confirm 链可豁免，方向安全）。
is_interp_delete_bypass() {
  local lower
  # 引号归一（删除引号字符本身而非内容——与 strip_quotes 相反方向）：node 的
  # require('fs').rmSync 去掉引号成 require(fs).rmSync 才能匹配 fs.rm；python 的
  # os.remove 本来无引号不受影响。空白压缩对齐 is_hazardous——python<TAB>-c 这类
  # 制表符分隔也要命中 "python -c" 模式（\ 转义的空格只匹配字面空格）。
  lower=$(printf '%s' "$1" | tr -d "'\"" | tr '[:upper:]' '[:lower:]' | tr -s '[:space:]' ' ')
  # 解释器 + 内联代码 flag（-c/-e）
  case "$lower" in
    *python*\ -c*|*node*\ -e*|*ruby*\ -e*|*perl*\ -e*|*perl*\ -E*) ;;
    *) return 1 ;;
  esac
  # 代码体内的文件删除调用模式：os.remove/os.unlink/shutil.rmtree（python）、
  # fs.rm/fs.unlink/.unlink（node；Path().unlink 同属 .unlink）、rmdir（shell 与
  # python os.rmdir/shutil.rmdir 同串）。node 的 require('fs').rmSync 引号归一后是
  # require(fs).rmsync( —— "fs.rm" 中间隔了 ")"，故补 .rm(/.rmsync 两个方法调用形态。
  case "$lower" in
    *os.remove*|*os.unlink*|*shutil.rmtree*|*fs.rm*|*.rm\(*|*.rmsync*|*fs.unlink*|*.unlink*|*rmdir*) return 0 ;;
  esac
  return 1
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

if ! is_hazardous "$COMMAND" && ! is_interp_delete_bypass "$COMMAND"; then
  echo "PASS"
  exit 0
fi

# --- context classification：危险串是数据（引号内 / 注释行）还是执行 ---
# is_hazardous 命中后，剥离引号与注释再判一次：剥离后不再命中 → 危险串都在引号里或注释里
# （数据上下文），且命令非执行包裹（bash -c/eval/pipe-shell）→ 放行。根治 grep "rm -rf" /
# git commit -m "fix rm -rf bug" / make build 注释含 rm -rf 类误判。
STRIPPED=$(strip_quotes "$COMMAND")
if [ "$STRIPPED" != "$COMMAND" ] && ! is_hazardous "$STRIPPED" && ! is_exec_wrapped "$COMMAND"; then
  forge hazard log data "$COMMAND" >/dev/null 2>&1 || true
  echo "PASS [hazard-guard] 危险串仅在引号内或注释行（数据上下文），放行: $COMMAND"
  exit 0
fi

# --- 命中高危：查是否已 human-in-the-loop 确认（forge hazard confirm 登记） ---
# confirmed 的 stderr 不再全吞（周复盘失守 a：>/dev/null 2>&1 让确认链故障无迹可查）。
# confirmed exit 0=已确认放行；exit 1 多为"未确认"（正常路径），但若它打了 stderr
# （[hazard] 错误），说明确认链本身异常——kimi 30s 超时/autoSync 拖慢 forge 启动/环境
# 问题——把 stderr 前 200 字符带进 block 输出，失败可诊断。
FP=$(forge hazard fingerprint "$COMMAND" 2>/dev/null)
_CONFIRM_DIAG=""
if [ -n "$FP" ]; then
  if _CONFIRM_STDERR=$(forge hazard confirmed "$FP" 2>&1 >/dev/null); then
    forge hazard log release "$COMMAND" >/dev/null 2>&1 || true
    echo "PASS [hazard-guard] 已确认放行（5min 窗口内）: $COMMAND"
    exit 0
  fi
  if [ -n "$_CONFIRM_STDERR" ]; then
    _CONFIRM_DIAG=$(printf '%s' "$_CONFIRM_STDERR" | head -c 200)
  fi
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
if [ -n "$_CONFIRM_DIAG" ]; then
  echo "确认链诊断："
  echo "  forge hazard confirmed 查询异常（非单纯未确认），stderr 前 200 字符："
  echo "  $_CONFIRM_DIAG"
  echo "  可运行 forge hazard status 查看确认链状态；若反复异常请检查 forge 环境"
  echo "  （kimi 30s 超时内串行 fork 多次 forge，autoSync 拖慢启动是主要嫌疑）。"
fi
echo ""
echo "FORGE_ALLOW_HAZARD env 豁免已移除（可被 agent 自我放行滥用）——confirm 链"
echo "（events.jsonl 审计 + 5min TTL）是唯一放行路径，测试/CI 同样走 forge hazard confirm。"
exit 1
`

const FileSentinelHook = `#!/bin/bash
# file-sentinel.sh — PostToolUse hook for Bash.
# Detects unauthorized file changes after Bash execution.
# Compares against PreToolUse bash-guard snapshot and quarantines violations.
# NEVER deletes user files — always moves to <DataDir>/quarantine/ for recovery.
# Only checks source code files and Forge config — ignores all other changes.
#
# Trust boundary: the snapshot/.ok/.cfg baseline lives in the agent-writable
# TMPDIR, so an audited command could rewrite or plant the baseline between
# PreToolUse and PostToolUse. This defense assumes TMPDIR integrity — it stops
# accidental and runaway writes, NOT adversarial tampering by the command
# itself.
set -eo pipefail

TASK_REF="${FORGE_TASK_REF:-}"
SESSION_ID="${FORGE_SESSION_ID:-default}"
: "${TMPDIR:=/tmp}"
FORGE_CMD_FILE="${TMPDIR}/forge-cmd-${SESSION_ID}"

# Per-invocation pairing: bash-guard keys its snapshot with its own PID
# (forge-snapshot-<session>-<pid>, plus .ok/.cfg sidecars), so parallel Bash
# calls in one session no longer overwrite or mis-consume each other's
# pairing. Consume the NEWEST unconsumed per-invocation snapshot. This is the
# only channel — both hooks run embedded from the same forge binary, so there
# is no legacy session-keyed producer to fall back to.
# Heuristic limitation: under truly parallel Bash calls the newest snapshot may
# belong to a sibling tool call — mis-pairing then degrades to the old
# shared-file behavior (a wrong but fresh baseline), never worse. The serial
# single-command flow is exact and unchanged.
SNAPSHOT_FILE=""
for f in "${TMPDIR}/forge-snapshot-${SESSION_ID}"-*; do
  [ -f "$f" ] || continue
  case "$f" in *.ok|*.cfg) continue ;; esac
  if [ -z "$SNAPSHOT_FILE" ] || [ "$f" -nt "$SNAPSHOT_FILE" ]; then
    SNAPSHOT_FILE="$f"
  fi
done
# Matching write-flag: same PID suffix as the consumed snapshot. Absent (or no
# snapshot at all) → treated as a read-only command (IS_WRITE_CMD=0 below),
# which is the fail-open direction.
WRITE_FLAG_FILE=""
if [ -n "$SNAPSHOT_FILE" ]; then
  _INV_SUFFIX="${SNAPSHOT_FILE##*forge-snapshot-${SESSION_ID}-}"
  if [ -f "${TMPDIR}/forge-write-${SESSION_ID}-${_INV_SUFFIX}" ]; then
    WRITE_FLAG_FILE="${TMPDIR}/forge-write-${SESSION_ID}-${_INV_SUFFIX}"
  fi
fi
# Sidecars written by bash-guard next to the snapshot: .ok = snapshot completed
# (distinguishes clean baseline from failed snapshot), .cfg = git-independent
# config manifest of the self-protection targets.
SNAPSHOT_OK="${SNAPSHOT_FILE}.ok"
SNAPSHOT_CFG="${SNAPSHOT_FILE}.cfg"

# cleanup_sentinel_state removes every state file this invocation may consume.
cleanup_sentinel_state() {
  rm -f "$SNAPSHOT_FILE" "$SNAPSHOT_OK" "$SNAPSHOT_CFG" "$WRITE_FLAG_FILE" \
    "$FORGE_CMD_FILE" 2>/dev/null || true
}

# forge_cfg_manifest must produce byte-identical lines to the same function in
# bash-guard.sh (content hash of the gitignored self-protection targets:
# .forge/hooks/* and .claude/settings*). Keep the two in sync.
forge_cfg_manifest() {
  local cf _h
  for cf in .forge/hooks/* .claude/settings*; do
    [ -f "$cf" ] || continue
    _h=$(md5sum "$cf" 2>/dev/null | awk '{print $1}')
    [ -z "$_h" ] && _h=$(md5 -q "$cf" 2>/dev/null)
    [ -z "$_h" ] && _h="size-$(wc -c < "$cf" 2>/dev/null | tr -d ' ')"
    [ -n "$_h" ] && printf '%s  %s\n' "$_h" "$cf"
  done
}

# No snapshot from PreToolUse → nothing to compare (fail-open). Stale
# per-invocation write-flags cannot leak into a later invocation: pairing
# requires the exact PID suffix AND the matching snapshot file to exist.
if [ ! -f "$SNAPSHOT_FILE" ]; then
  echo "PASS"
  exit 0
fi

# Not a git repo → cannot diff, pass silently
git rev-parse --git-dir >/dev/null 2>&1 || {
  cleanup_sentinel_state
  echo "PASS"
  exit 0
}

# Source code extension pattern
SRC_EXT='\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$'
# Forge config pattern. refactor-data-home commit D: tasks/specs/reviews/gates 迁
# 用户级 DataDir（~/.forge/projects/<key>/，git 不跟踪），git diff 永远不返这些路径，
# 模式留之无用且误导。只守项目级 .forge/hooks/（ConfigDir/hooks 配置层，仍项目级）。
CFG_EXT='(\.forge/hooks/|\.claude/settings)'

# Get current changed source files only (not all files).
# git diff --cached stays symmetric with bash-guard's snapshot — a
# write-then-git-add in one command must not escape through the staged tree.
CURRENT_ALL=$(
  {
    git diff --name-only 2>/dev/null || true
    git diff --cached --name-only 2>/dev/null || true
    git ls-files --others --exclude-standard 2>/dev/null || true
  } | grep -E "${SRC_EXT}|${CFG_EXT}" | sort -u || true
)

# Read pre-Bash snapshot
BEFORE_ALL=$(cat "$SNAPSHOT_FILE" 2>/dev/null | grep -E "${SRC_EXT}|${CFG_EXT}" | sort -u || true)

# EMPTY snapshot + non-empty current tree: two cases.
# 1. bash-guard completed the snapshot and wrote the .ok marker: the empty list
#    is a LEGITIMATE clean baseline (fresh repo, nothing uncommitted — the
#    common case). Fall through to the normal NEW_CHANGES flow: every current
#    change is genuinely new. (Without this distinction, a first Bash source
#    write in a clean repo was always fail-opened by case 2 below.)
# 2. Snapshot file exists but NO .ok marker: the snapshot failed silently (git
#    errors swallowed by 2>/dev/null: cwd drift, index.lock, Windows newline)
#    or was planted without bash-guard. With no reliable baseline we CANNOT
#    compute a diff — the else-branch below would treat the ENTIRE working
#    tree as "new violations" and quarantine + run "git checkout --" to discard
#    the user's existing uncommitted source. That is fail-destructive on an
#    unprovable violation. Fail-open instead: WARN only.
# (P0 DevWorkbench incident 2026-06: 71 files moved to quarantine, working
# tree .tsx/.rs silently restored to HEAD because BEFORE_ALL was empty.)
if [ -z "$BEFORE_ALL" ] && [ -n "$CURRENT_ALL" ] && [ ! -f "$SNAPSHOT_OK" ]; then
  cleanup_sentinel_state
  echo "WARN [file-sentinel] PreToolUse snapshot empty WITHOUT completion marker (git failed silently or snapshot planted) while working tree has uncommitted source/config changes — cannot compute reliable diff, skipping quarantine to protect existing work."
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

# Git-independent config protection: .forge/hooks/*.sh and .claude/settings*
# are gitignored/untracked by design, so the git-based lists above NEVER see a
# Bash rewrite of them — the CONFIG quarantine branch below would be blind on
# exactly its primary self-protection targets. bash-guard recorded a hash
# manifest of these files (${SNAPSHOT}.cfg); compare against the current state
# and treat any drift (changed hash, added, or removed file) as a config
# change. bash-guard always writes the manifest next to its snapshot, so a
# missing manifest means a planted or truncated snapshot — skip the comparison.
CFG_FROM_MANIFEST=""
if [ -f "$SNAPSHOT_CFG" ]; then
  CFG_NOW=$(forge_cfg_manifest)
  # Symmetric difference of "hash  path" lines, reduced to paths: a changed
  # hash surfaces both the old and the new line (same path, deduped by sort -u);
  # an added/removed file surfaces its single line.
  CFG_FROM_MANIFEST=$(
    {
      printf '%s\n' "$CFG_NOW"
      cat "$SNAPSHOT_CFG"
    } | sort | uniq -u | awk '{$1=""; sub(/^ +/,""); print}' | sort -u || true
  )
fi

# Clean up
cleanup_sentinel_state

# No current changes at all → pass
[ -z "$CURRENT_ALL" ] && [ -z "$CFG_FROM_MANIFEST" ] && { echo "PASS"; exit 0; }

# Find NEW changes: lines in CURRENT but not in BEFORE
# Use grep -Fxv for reliable line-by-line exact match
if [ -n "$BEFORE_ALL" ]; then
  NEW_CHANGES=$(printf '%s\n' "$CURRENT_ALL" | grep -Fxvf <(printf '%s\n' "$BEFORE_ALL") 2>/dev/null || true)
else
  NEW_CHANGES="$CURRENT_ALL"
fi

# No new changes → pass (manifest-only config drift must still reach the
# CONFIG branch below, hence the CFG_FROM_MANIFEST disjunct).
[ -z "$NEW_CHANGES" ] && [ -z "$CFG_FROM_MANIFEST" ] && { echo "PASS"; exit 0; }

# Categorize new changes. CONFIG_CHANGES merges the git-visible set with the
# git-independent manifest drift (CFG_FROM_MANIFEST) — the latter carries
# exactly the gitignored .forge/hooks/*.sh / .claude/settings* rewrites git
# cannot report.
SOURCE_CHANGES=$(printf '%s' "$NEW_CHANGES" | grep -E "$SRC_EXT" || true)
CONFIG_CHANGES=$(
  {
    printf '%s\n' "$NEW_CHANGES"
    printf '%s\n' "$CFG_FROM_MANIFEST"
  } | grep -E "$CFG_EXT" | sort -u || true
)

# No protected changes → pass
[ -z "$SOURCE_CHANGES" ] && [ -z "$CONFIG_CHANGES" ] && { echo "PASS"; exit 0; }

# Helper: quarantine files — NEVER delete, always preserve for recovery.
# Tracked files: moved to quarantine, then restored to the HEAD version
# (worktree AND index). Untracked files: moved to quarantine (not in git, so
# can't restore from HEAD).
# All files are recoverable: cp -r <DataDir>/quarantine/<session-id>/* .
#
# restore_head <file>: return a tracked path to its HEAD state — worktree AND
# index. Plain "git checkout --" restores from the INDEX, so a staged violation
# (detected via git diff --cached) would be put right back into the worktree
# with its staged entry kept — the quarantine silently undone. checkout HEAD
# clears the staged entry too; staged-new files (absent in HEAD) are dropped
# from the index instead (their worktree content is already in quarantine).
restore_head() {
  local f="$1"
  if git cat-file -e "HEAD:$f" 2>/dev/null; then
    git checkout HEAD -- "$f" 2>/dev/null
  else
    git rm -q --cached -- "$f" 2>/dev/null
  fi
}

quarantine_files() {
  local files="$1"
  # refactor-data-home commit D: quarantine 进用户级 DataDir（forge data-dir 拿路径）；
  # FORGE_QUARANTINE_DIR 仍可显式覆盖（测试 / 自定义）。仅违规时调用，非每次 Bash fork。
  local quarantine_base="${FORGE_QUARANTINE_DIR:-}"
  if [ -z "$quarantine_base" ]; then
    # user-level-assets: quarantine 永不落项目树——forge data-dir 失败时
    # 也回落到用户级 home（旧版回落 ".forge" 会在项目里重建 .forge/ 目录）。
    quarantine_base="$(forge data-dir 2>/dev/null || echo "${FORGE_DATA_HOME:-$HOME/.forge}/quarantine-fallback")/quarantine"
  fi
  local qdir="${quarantine_base}/${SESSION_ID}"
  mkdir -p "$qdir" 2>/dev/null || true

  local quarantined=""
  local failed=""
  local restored=""
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
      # mv failed AND the file is currently missing (e.g. the command deleted
      # it): nothing left to quarantine, but RESTORE is an independent action —
      # a tracked file is still recovered from HEAD (worktree AND index, so a
      # staged variant cannot resurrect the violation), so a reported FAIL
      # never leaves the user's file deleted. Restore failures surface via
      # "failed".
      if [ ! -e "$f" ] && git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
        if restore_head "$f"; then
          restored="${restored} ${f}"
        fi
      fi
      continue
    fi

    # For tracked files, restore the HEAD version (worktree AND index — clears
    # any staged variant of the violation; staged-new entries are dropped).
    if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
      restore_head "$f" || true
    fi

    quarantined="${quarantined} ${f}"
  done <<< "$files"

  QUARANTINED="$quarantined"
  QUARANTINE_FAILED="$failed"
  QUARANTINE_RESTORED="$restored"
  QUARANTINE_DIR="$qdir"
}

# Self-protection: quarantine config changes (unless forge command was detected)
if [ -n "$CONFIG_CHANGES" ] && [ $IS_FORGE_CMD -eq 0 ]; then
  # forge 自部署豁免（2026-08-02 自伤事故）：被监控的非 forge Bash 命令触发 hook 链
  # 时，链上 forge 子进程 autoSync 恰好重写项目级 .forge/hooks/*.sh（team-mode/老项目
  # 残留），manifest drift + IS_FORGE_CMD=0 → 整目录被 quarantine。Go 侧部署写入前在
  # <DataDir>/stamps/hook-deploy 落 "<epoch> <tag>" marker（grace 窗口模式，与下方
  # task-complete _GRACE_FILE 同款）：drift 全部位于 .forge/hooks/ 且 marker 新鲜
  # （<120s）且 tag 匹配 → 视为 forge 自身部署写入，PASS 不 quarantine；窗口外/
  # 无 marker/混有非 hooks drift 时仍严格 quarantine。防伪造：marker 在 DataDir
  # （agent 可写，与 snapshot/.cfg 基线同一信任边界，可接受）+ project tag 校验。
  _HOOKS_ONLY=1
  while IFS= read -r _cf; do
    [ -z "$_cf" ] && continue
    if [[ $_cf != .forge/hooks/* ]]; then
      _HOOKS_ONLY=0
      break
    fi
  done <<< "$CONFIG_CHANGES"
  if [ "$_HOOKS_ONLY" -eq 1 ]; then
    _DEPLOY_DD="${FORGE_DATA_DIR:-}"
    if [ -z "$_DEPLOY_DD" ]; then
      _DEPLOY_DD="$(forge data-dir 2>/dev/null || true)"
    fi
    _DEPLOY_MARKER="${_DEPLOY_DD}/stamps/hook-deploy"
    if [ -n "$_DEPLOY_DD" ] && [ -f "$_DEPLOY_MARKER" ]; then
      _DEPLOY_STAMP=$(cat "$_DEPLOY_MARKER" 2>/dev/null || true)
      _DEPLOY_EPOCH="${_DEPLOY_STAMP%% *}"
      _DEPLOY_TAG="${_DEPLOY_STAMP#* }"
      # 纯数字校验（非数字置空，下游 -ge 比较不会炸）
      case "$_DEPLOY_EPOCH" in
        ""|*[!0-9]*) _DEPLOY_EPOCH="" ;;
      esac
      _NOW=$(date +%s 2>/dev/null || echo 0)
      if [ -n "$_DEPLOY_EPOCH" ] && [ "$_NOW" != "0" ] && [ $((_NOW - _DEPLOY_EPOCH)) -ge 0 ] && [ $((_NOW - _DEPLOY_EPOCH)) -lt 120 ]; then
        # tag 双保险：DataDir 本就按项目分桶，tag 不一致视为串项目不豁免；任一侧
        # 为空（老 marker 无 tag 字段）则跳过 tag 校验。
        _TAG_OK=1
        if [ -n "${FORGE_PROJECT_TAG:-}" ] && [ -n "$_DEPLOY_TAG" ] && [ "$_DEPLOY_TAG" != "${FORGE_PROJECT_TAG:-}" ]; then
          _TAG_OK=0
        fi
        if [ "$_TAG_OK" -eq 1 ]; then
          echo "PASS [file-sentinel] .forge/hooks drift within forge deploy window (<120s), treating as forge self-deploy:${CONFIG_CHANGES}"
          exit 0
        fi
      fi
    fi
  fi
  quarantine_files "$CONFIG_CHANGES"
  MSG="FAIL [file-sentinel] Quarantined unauthorized changes to Forge config:${QUARANTINED}."
  [ -n "$QUARANTINE_FAILED" ] && MSG="${MSG} FAILED to quarantine:${QUARANTINE_FAILED}."
  [ -n "$QUARANTINE_RESTORED" ] && MSG="${MSG} Restored from HEAD:${QUARANTINE_RESTORED}."
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
#
# dogfood 2.3 grace carve-out: forge task complete clears the active-task
# ref so the immediate 'git commit' (a source write) would otherwise hit the
# quarantine branch below. Within the post-complete grace window (5min default,
# written by runTaskComplete via taskpipeline.MarkCompleteGrace) we tolerate
# the write with WARN — bounded by the in-file epoch stamp, naturally expired.
# Existing fallback prompts the agent to start a new task on the NEXT source
# write outside the window.
if [ -z "$TASK_REF" ] && [ -n "$SOURCE_CHANGES" ]; then
  if [ $IS_FORGE_CMD -eq 0 ] && [ -n "$SESSION_ID" ] && [ "$SESSION_ID" != "default" ]; then
    _GRACE_BASE="$(forge data-dir 2>/dev/null || true)"
    if [ -n "$_GRACE_BASE" ]; then
      _GRACE_FILE="${_GRACE_BASE}/.task-complete-grace-${SESSION_ID}"
      if [ -f "$_GRACE_FILE" ]; then
        _MTIME=$(tr -d '[:space:]' < "$_GRACE_FILE" 2>/dev/null)
        _NOW=$(date +%s)
        if [ -n "$_MTIME" ] && [ "$_MTIME" -gt 0 ] 2>/dev/null && [ $((_NOW - _MTIME)) -lt 300 ]; then
          echo "WARN [file-sentinel] Source write within post-complete grace window (300s): ${SOURCE_CHANGES}. Run 'forge task start' before the next source write to restore strict checks."
          echo "PASS"
          exit 0
        fi
      fi
    fi
  fi
  if [ $IS_WRITE_CMD -eq 0 ]; then
    echo "WARN [file-sentinel] Source changes present but the Bash command was read-only — diff unreliable (partial snapshot / external interference), skipping quarantine to protect existing work:${SOURCE_CHANGES}"
    echo "PASS"
    exit 0
  fi
  quarantine_files "$SOURCE_CHANGES"
  MSG="FAIL [file-sentinel] Quarantined unauthorized code changes (no active task):${QUARANTINED}."
  [ -n "$QUARANTINE_FAILED" ] && MSG="${MSG} FAILED to quarantine:${QUARANTINE_FAILED}."
  [ -n "$QUARANTINE_RESTORED" ] && MSG="${MSG} Restored from HEAD:${QUARANTINE_RESTORED}."
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

const McpScanHook = `#!/bin/bash
# mcp-scan.sh — SessionStart hook (advisory, non-blocking, global).
# 会话开始扫描项目级 .mcp.json 的 server 配置安全性。补 skill-scan 盲区:
# skill-scan 只扫 ~/.claude/skills,但项目级 .mcp.json 在 SessionStart 被各 host
# 自动加载——攻击者可通过 PR/git 植入恶意 server,用户 clone 项目即自动连接,
# 是真实攻击面(2025 多起 MCP 供应链事件)。项目级聚焦:用户级 ~/.claude.json 等
# 是用户自装 server,风险自担,不在范围(全局扫用户级跨 host 路径不一且误报多)。
#
# 诚实边界(必读,不声称超出能力):.mcp.json 只含 server 连接配置(command/args/
# env/url),不含 tool descriptions。真正的 Tool Poisoning(恶意 tool description
# 注入 agent 上下文)live 在 server 运行时返回的 tool descriptions,config-layer
# 扫不到。本 hook 只审 config 层可检攻击面:管道执行(curl 管道到 sh)/任意包执行
# (npx/uvx 远程包)/内联代码(解释器 -c/-e)/非 https URL/env 明文凭证。runtime
# tool description 注入无 config 信号,只能在使用点察觉,不在本 hook 能力内。
#
# advisory:stdout PASS detail 列风险不阻塞(advisory 方向)。全局 hook:不依赖
# forge project(非 forge 项目的 .mcp.json 正是要发现的)。
# Protocol: stdout = PASS detail → additionalContext;exit 0 = 放行。

# 起点:FORGE_CWD(cli/hook.go 传 cwd)或回退 $PWD;Windows 反斜杠归一为正斜杠。
START="${FORGE_CWD:-$PWD}"
START="${START//\\//}"

# 找 git root(向上找 .git,与 init-suggest 同款;盘符根 %/* 返回原值时 break 防死循环)。
ROOT=""
D="$START"
while [ -n "$D" ] && [ "$D" != "/" ]; do
  if [ -e "$D/.git" ]; then ROOT="$D"; break; fi
  NEW="${D%/*}"
  if [ "$NEW" = "$D" ]; then break; fi
  D="$NEW"
done

# 无 git root → 非项目仓库,无项目级 .mcp.json 可审,静默。
if [ -z "$ROOT" ]; then
  echo "PASS [mcp-scan] no git project (no project-level .mcp.json to scan, advisory)"
  exit 0
fi

MCP_FILE="$ROOT/.mcp.json"
if [ ! -f "$MCP_FILE" ]; then
  echo "PASS [mcp-scan] no .mcp.json (advisory)"
  exit 0
fi

# 读取失败/空 → 无法判定,静默放行(不撒谎报"全部安全")。
CONTENT=$(cat "$MCP_FILE" 2>/dev/null)
if [ -z "$CONTENT" ]; then
  echo "PASS [mcp-scan] .mcp.json unreadable or empty (advisory)"
  exit 0
fi

# --- config-layer 风险检测 ---
# 全部 case-glob + grep -Fi/grep -qi(BSD 安全:不用 grep -E 交替,参 bash-guard/skill-scan,
# BSD/macOS grep 在 ERE 交替 abort "Unmatched (")。advisory 方向,宁误报勿漏。
# 大小写归一便于 URL/command/字段名匹配;[|] 字符类匹配字面管道符(pattern 间 | 才是 alternation)。
LOWER=$(printf '%s' "$CONTENT" | tr '[:upper:]' '[:lower:]')
RISKS=""

# 1. 管道执行:curl/wget 管道到 shell —— 远程下载即执行,经典植入形态。
# \| 转义为字面管道符(参 hazard-guard *git\ push* 的反斜杠转义);不能用 [|],
# bash case pattern parser 把 | 当 alternation separator,字符类内的 | 也被吞。
case "$LOWER" in
  *curl*\|*sh*|*wget*\|*sh*|*curl*\|*bash*|*wget*\|*bash*) RISKS="${RISKS}[pipe-exec] curl/wget 管道到 shell(远程下载即执行)。 ";;
esac

# 2. 任意包执行:npx/uvx/dlx/bunx 拉远程包执行——供应链/typosquat 风险(包名可仿冒)。
# 裸 token 不锚空格:JSON 里 command 值是 "npx",npx 后紧跟引号非空格,空格锚定会漏。
case "$LOWER" in
  *npx*|*uvx*|*dlx*|*bunx*) RISKS="${RISKS}[pkg-exec] npx/uvx/dlx/bunx 任意远程包(供应链/typosquat)。 ";;
esac

# 3. 内联代码:解释器 -c/-e 把字符串当代码执行(独立 grep -qi BRE,无 ERE 交替)。
INLINE=0
if printf '%s' "$CONTENT" | grep -qi 'python.*-c' || \
   printf '%s' "$CONTENT" | grep -qi 'node.*-e' || \
   printf '%s' "$CONTENT" | grep -qi 'ruby.*-e' || \
   printf '%s' "$CONTENT" | grep -qi 'perl.*-e'; then
  INLINE=1
fi
if [ "$INLINE" = "1" ]; then
  RISKS="${RISKS}[inline-code] 解释器 -c/-e 内联代码执行。 "
fi

# 4. 非 https URL:http:// 明文(中间人可篡改 server 响应)。
if printf '%s' "$CONTENT" | grep -qi 'http://'; then
  RISKS="${RISKS}[insecure-url] http:// 明文 URL(应为 https)。 "
fi

# 5. env 明文凭证:JSON key 形如 "token" / "secret" 等。grep -Fi 固定串,大小写不敏感,
#    多 -e 模式 OR;双引号用 printf 八进制 \042 运行时构造,避开源码 ASCII 双引号
#    被编辑器腐蚀成弯引号(memory: windows-input-quote-corruption)。
DQ=$(printf '\042')
if printf '%s' "$CONTENT" | grep -Fiq -e "${DQ}token${DQ}" -e "${DQ}secret${DQ}" -e "${DQ}api_key${DQ}" -e "${DQ}apikey${DQ}" -e "${DQ}password${DQ}" -e "${DQ}passwd${DQ}" -e "${DQ}credential${DQ}" -e "${DQ}access_key${DQ}"; then
  RISKS="${RISKS}[env-secret] JSON 含明文凭证字段名(token/secret/key/password,全文 grep -Fi 匹配 server 名/args 也命中,advisory 宁误报)。 "
fi

if [ -n "$RISKS" ]; then
  RISKS_SUMMARY=$(printf '%s' "$RISKS" | cut -c1-600)
  echo "PASS [mcp-scan] Advisory: 项目级 .mcp.json 发现风险信号——${RISKS_SUMMARY}请核查 server 来源是否可信(forge 不阻塞,agent/用户自检)。注:本扫描只审 config 层,runtime tool description 注入(Tool Poisoning)不在能力内。"
else
  echo "PASS [mcp-scan] 项目级 .mcp.json 无 config 层风险信号 (advisory; runtime tool description 注入不在扫描范围)"
fi
`

const InitSuggestHook = `#!/bin/bash
# init-suggest.sh — SessionStart hook (advisory, non-blocking, global).
# 用户级"项目自动 init"检测：装了 forge（plugin/npm）后，用户在任意 git 项目开
# Claude Code，若无 .forge/ → 首次输出提示给 agent，引导询问用户是否启用 forge。
# 拒绝则永久静默（forge suggest decline 写 declined 标记）。FORGE_AUTO_INIT=1 时
# 直接 forge init（处处无感模式，代价是污染每个 git 项目）。补"每项目手动 init"
# 缺口——plugin install 接用户级 hooks+MCP，但项目级 .forge/CLAUDE.md/skills 仍需
# forge init，本 hook 让这步从"用户记得敲"变"agent 主动询问"。advisory：默认不自动
# 写文件（除 FORGE_AUTO_INIT），exit 0 不阻塞会话。
#
# 全局 hook：在非 forge 项目正是要发现它们（isGlobalHook）。不依赖 forge project root。
# 一次标记（${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested/<tag>）避免重复提示：suggested=提示过不重复，
# declined=用户拒绝永久静默。tag=FORGE_CWD_TAG（cli/hook.go 算 suggestTagFor(cwd)，按 git root 键控而非 cwd——同项目任意子目录同 tag，decline 契约成立）。
#
# BSD-safe：全程 POSIX test ([ ])与参数扩展，不用 case-action 复杂命令（避 bash 3.2
# parse error，见 memory hazard-bash32-case-parser），不用 grep -E 交替。
#
# Protocol: stdout = PASS detail → additionalContext；exit 0 = 放行（advisory 不阻塞）。

# 起点：FORGE_CWD（cli/hook.go 传 cwd）或回退 $PWD。
START="${FORGE_CWD:-$PWD}"
# Windows 反斜杠 → 正斜杠（Git Bash 兼容 os.Getwd 的 E:\ 形式）。
START="${START//\\//}"

# 找 git root（向上找 .git；worktree/submodule 的 .git 可能是文件，用 -e）。
# 防死循环：盘符根（E:/Forge → E:）%/* 返回原值时 break。
ROOT=""
D="$START"
while [ -n "$D" ] && [ "$D" != "/" ]; do
  if [ -e "$D/.git" ]; then ROOT="$D"; break; fi
  NEW="${D%/*}"
  if [ "$NEW" = "$D" ]; then break; fi
  D="$NEW"
done
# 无 git root → 不是 git 仓库，但仍提示 agent 建议用户先 git init 再 forge init。
# tag 用目录路径（suggestTagFor 对非 git 回退到 projectTagFor(dir)），标记机制
# 与 git 项目一致：suggested=提示过不重复，declined=用户拒绝永久静默。
if [ -z "$ROOT" ]; then
  TAG="${FORGE_CWD_TAG:-}"
  SUGGEST_DIR="${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested"
  MARKER="$SUGGEST_DIR/$TAG"
  if [ -n "$TAG" ] && [ -f "$MARKER" ]; then
    exit 0
  fi
  # 自动模式：FORGE_AUTO_INIT=1 → git init + forge init（与 git 项目一致的无感体验）。
  if [ "${FORGE_AUTO_INIT}" = "1" ]; then
    INIT_OUT=$( { cd "$START" && git init && forge init; } 2>&1 )
    RC="$?"
    if [ "$RC" = "0" ]; then
      echo "PASS [init-suggest] FORGE_AUTO_INIT=1: 已在 $START 自动 git init + forge init。"
    else
      TAIL=$(printf '%s' "$INIT_OUT" | tail -c 400 | tr '\n' ' ')
      [ -z "$TAIL" ] && TAIL="(无 stderr 输出)"
      echo "PASS [init-suggest] Advisory: FORGE_AUTO_INIT=1 但 git init + forge init 失败（exit $RC），请手动 'git init && forge init'。错误尾部: $TAIL"
    fi
    exit 0
  fi
  mkdir -p "$SUGGEST_DIR" 2>/dev/null
  if [ -n "$TAG" ]; then
    echo "suggested" > "$MARKER" 2>/dev/null
  fi
  DIR_NAME=$(basename "$START")
  echo "PASS [init-suggest] Advisory: 当前目录 '${DIR_NAME}' 不是 Git 仓库。建议先运行 'git init' 初始化版本控制，再运行 'forge init' 启用质量门禁（task-gated 源码变更 + 断言守卫 + 评分）。如不需要，运行 'forge suggest decline' 永久不再提示。"
  exit 0
fi

# 已启用 forge（registry 成员或遗留 .forge/）→ 跳过建议。v1.22 起 init 默认
# 零项目写入（无 .forge/），成员资格必须问注册表——forge status 在已登记项目
# 里 exit 0，未登记 exit 非 0；[ -d .forge ] 兜底老项目/团队模式。
# 若 plugin 也 user-level 接管了 hooks+MCP，清理
# project-level 重复（plugin install 后存量项目残留的 settings.local.json hooks 与
# .mcp.json forge server，Claude Code 会双重加载）。幂等：dedupe 无重复时 no-op 无输出。
if forge status >/dev/null 2>&1 || [ -d "$ROOT/.forge" ]; then
  if forge plugin status >/dev/null 2>&1; then
    DEDUPE=$(forge plugin dedupe "$ROOT" --keep-empty 2>/dev/null)
    if [ -n "$DEDUPE" ]; then
      echo "PASS [init-suggest] $DEDUPE"
    fi
  fi
  exit 0
fi

# 自动模式：FORGE_AUTO_INIT=1 → 直接 forge init（污染换无感，用户显式 opt-in）。
# 捕获输出（不 >/dev/null 2>&1 全吞）：init 部分成功（.forge/ 建了但 state.json 写失败）
# 时下次 [ -d .forge ] 静默，用户会以为 init 完成实际拿到破损状态——回显 stderr 尾部
# 让 partial-state 可见。tail -c / tr 跨 BSD/GNU；用 POSIX [ ] 不用 case-action。
if [ "${FORGE_AUTO_INIT}" = "1" ]; then
  # 分组 { } 2>&1：cd 失败（权限/竞态删除/盘符形式）的 stderr 也进 INIT_OUT，
  # 不漏到 hook 外部致 TAIL 空白误导（R1：原只重定向 forge init，cd 失败时
  # INIT_OUT 空，用户看到"错误尾部:"空白却不知真错误）。
  INIT_OUT=$( { cd "$ROOT" && forge init; } 2>&1 )
  RC="$?"
  if [ "$RC" = "0" ]; then
    echo "PASS [init-suggest] FORGE_AUTO_INIT=1: 已在 $ROOT 自动初始化 forge。"
  else
    TAIL=$(printf '%s' "$INIT_OUT" | tail -c 400 | tr '\n' ' ')
    [ -z "$TAIL" ] && TAIL="(无 stderr 输出)"
    echo "PASS [init-suggest] Advisory: FORGE_AUTO_INIT=1 但 forge init 失败（exit $RC），请手动 'forge init'。错误尾部: $TAIL"
  fi
  exit 0
fi

# 提示模式：检查一次标记（declined/suggested 都静默）。
TAG="${FORGE_CWD_TAG:-}"
SUGGEST_DIR="${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested"
MARKER="$SUGGEST_DIR/$TAG"
if [ -n "$TAG" ] && [ -f "$MARKER" ]; then
  exit 0
fi

# 首次：写 suggested 标记（mkdir/echo 失败降级=可能重复提示，可接受；不 set -e）。
mkdir -p "$SUGGEST_DIR" 2>/dev/null
if [ -n "$TAG" ]; then
  echo "suggested" > "$MARKER" 2>/dev/null
fi

PROJ=$(basename "$ROOT")
echo "PASS [init-suggest] Advisory: 检测到 git 项目 '$PROJ' 未启用 forge。询问用户是否启用 forge 质量门禁（task-gated 源码变更 + 断言守卫 + 评分）。用户同意 → 运行 'forge init'；拒绝 → 运行 'forge suggest decline' 永久不再提示该项目。forge 默认不自动写文件（设 FORGE_AUTO_INIT=1 可处处自动 init）。"
exit 0
`

const TaskResumeHook = `#!/bin/bash
# task-resume.sh — SessionStart hook (advisory, non-blocking, project-scoped).
# 会话启动自动注入活跃任务的接续上下文 + 把当前 session 锚定到任务。接手方冷启动即知有
# 活跃任务、在哪一步、已确认哪些决策、有哪些阻塞，无需手动 forge task resume。
#
# Thin wrapper：实际逻辑在 forge task resume --hook（Go）。bash 仅 exec 转发——找任务
# （3 级 fallback：active-task-ref、分支、单一未完成任务）、attach session、renderResume、
# 无任务静默、PASS 前缀都在 Go 里，避开 bash 重写逻辑与 Windows 引号腐蚀（memory: quote）。
#
# 项目级 hook：runHook（cli/hook.go）已用 findProjectRoot 找到 root 并设为 cwd；非 forge
# 项目 runHook 对非全局 hook 直接 outputAllow exit，根本不跑本脚本。故脚本内不再判 .forge。
#
# resume --hook 永远 exit 0（不阻塞 SessionStart）：无活跃任务静默，有则输出 PASS+接续视图。
# Protocol: stdout = PASS detail → runHook 包成 additionalContext 注入会话；exit 0 = 放行。
exec forge task resume --hook
`

const CompactResumeHook = `#!/bin/bash
# compact-resume.sh — PostCompact hook (advisory, hosts with a compaction lifecycle, gap#2 根治层·设标志半边)。
# 压缩完成时为本 session 标记"刚压缩过"（有 session ID 写 per-session sentinel，无则置
# ResumeStale），等本 session 下个 UserPromptSubmit 的 resume-reinject 检测到即重注入完整
# handoff。PostCompact 本身不能注入 additionalContext（Claude Code 文档：PostCompact 不在注入点
# 列表，只能 follow-up），故此 hook 只设标志不输出。无 compaction lifecycle 的 host 由
# ForgeHookSpec 过滤不装本链。
#
# Thin wrapper：实际逻辑在 forge task resume --compact-flag（Go）。bash 仅 exec 转发——找任务、
# 设标志、幂等、静默都在 Go，避开 bash 重写逻辑与 Windows 引号腐蚀（memory: quote）。
#
# 项目级 hook：runHook（cli/hook.go）已用 findProjectRoot 设 cwd；非 forge 项目 runHook 对
# 非全局 hook outputAllow exit，根本不跑本脚本。
exec forge task resume --compact-flag
`

const ResumeReinjectHook = `#!/bin/bash
# resume-reinject.sh — UserPromptSubmit hook (advisory, hosts with a compaction lifecycle, gap#2 根治层·重注入半边)。
# 若本 session 刚压缩过（per-session sentinel 或 legacy ResumeStale）→ 输出 PASS+完整接续视图
# 并清标记；否则静默。UserPromptSubmit 的 stdout 进 context（Claude Code 文档：在
# additionalContext 注入点列表），runHook 包成 additionalContext 注入，协议同 SessionStart task-resume。
#
# 与 SessionStart task-resume 分工：task-resume 只在会话启动注入一次，会话中途压缩不补；
# resume-reinject 补这个缺口——压缩后本 session 第一个 user prompt 自动恢复完整接续上下文，
# 不靠 agent 主动 forge task resume。tl;dr tier 是跨 host 缓解层，本 hook 是压缩根治层。
#
# Thin wrapper：逻辑在 forge task resume --reinject（Go）。bash 仅 exec 转发。
exec forge task resume --reinject
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

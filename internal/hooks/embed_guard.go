package hooks

// embed_guard.go —— embed.go 同包分文件产物（2026-09 普查 P7：2322 行单文件按职能
// 拆分；内容逐字节不变——embeddedHooks 名册与 guard test 钉住等价性）。

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
# check honors both. work-activity is a rhythm gate, not verification — escaping
# it never caps evidence Strength (see checklog.isRhythmEscapeHatch); the escape
# is still audit-logged via CheckEscapeHatch.
[ "$FORGE_WORK_ACTIVITY" = "disable" ] && exit 0

# Not source code — allow (mirrors task-guard source extension set).
printf '%s' "$FILE_PATH" | grep -qE '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || exit 0
# Test files — always allow (TDD workflow).
printf '%s' "$FILE_PATH" | grep -qE '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/|__tests__/)' && exit 0

# New file (not yet on disk) — allow: Write creating a file can't have been
# Read, and the agent authors fresh content rather than editing blind.
[ -f "$FILE_PATH" ] || exit 0

# FAIL 文案单点维护（2026-08-24：此前无 reads-log 分支与文件不在 log 分支逐字
# 重复同一文案，双份维护必漂移）。
MSG_RBE="FAIL [read-before-edit] $FILE_PATH 未在本会话 Read 过——Edit 需精确匹配旧文本，未读即凭记忆盲改——Edit 的 old_string 撞中即错改入库。先 Read 该文件再 Edit。批量/重构逃生：forge task override --work-activity disable（记 checklog 审计；work-activity 是节奏门禁，不降 evidence 强度）。"

# No reads log recorded this session → any Edit of an existing source file is
# editing blind → block (this IS the signal we want, not a false positive).
if [ -z "$READS_FILE" ] || [ ! -f "$READS_FILE" ]; then
  echo "$MSG_RBE"
  exit 1
fi

# Exact-line match against the per-session reads log. FORGE_FILE_PATH is the
# repo-relative path the dispatcher normalized (same form appended on Read).
if grep -qxF "$FILE_PATH" "$READS_FILE"; then
  echo "PASS"
  exit 0
fi

echo "$MSG_RBE"
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
# Session-marker root — same migration as task-guard (2026-08-23): the touched/
# nowarn/cmd markers live under FORGE_DATA_DIR (fallback TMPDIR), in the markers/
# subdir, pruned after 7 days (the DataDir is permanent — no implicit OS
# cleanup). The SNAPSHOT/WRITE_FLAG per-invocation pairing below deliberately
# STAYS in TMPDIR — it is bound to the file-sentinel trust-boundary design (see
# FileSentinelHook's comment) and its failure mode differs from marker loss.
#
# 会话 marker 根目录——与 task-guard 同一迁移（2026-08-23）：touched/nowarn/cmd
# 标记落在 FORGE_DATA_DIR（兜底 TMPDIR）的 markers/ 子目录下。下方
# SNAPSHOT/WRITE_FLAG 的 per-invocation 配对通道**刻意留在 TMPDIR**——它绑定
# file-sentinel 的信任边界设计（见 FileSentinelHook 注释），失效模式也与
# marker 丢失不同。
_MARKER_DIR="${FORGE_DATA_DIR:-${TMPDIR:-/tmp}}/markers"
mkdir -p "$_MARKER_DIR" 2>/dev/null || true
find "$_MARKER_DIR" -maxdepth 1 -type f -name 'forge-*' -mtime +7 -delete 2>/dev/null || true
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
  # target (contains "." or "/"). A bare comparison like
  # "x > 0" is rejected because "0" carries no path character. Single BRE,
  # no ERE — portable across GNU/BSD grep.
  local s="${cmd//2>/··}"
  s="${s//=>/··}"
  if ! printf '%s' "$s" | grep -q '>[[:space:]]*[^[:space:]&][^[:space:]]*[./][^[:space:]]*'; then
    # Known gaps — contract: 识别不到 ⇒ 放行. This gate is a heuristic first line
    # (drives the no-task WARN + file-sentinel's secondary gate); file-sentinel's
    # snapshot diff is the backstop. Not detected by design: interpreter inline
    # writes (python -c / node -e / ruby -e writing files), pipes through another
    # process (curl | sh), base64 -d / xxd -r decoders, git checkout/restore of
    # tracked paths, and redirect targets carrying no dot or slash.
    return 1
  fi
  # 仓库外重定向豁免（2026-08-24）：go test ./... > /tmp/final.txt 2>&1 /
  # gh run watch ... > /tmp/ci.log 2>&1 触发 "[bash-guard] Bash write without
  # active task" 误报。本 gate 的意图是拦无任务时的源码变更——重定向到 /tmp、
  # $TMPDIR、~/.forge、/dev/null 等明确非源码路径不是源码变更。逐条提取
  # path-like 重定向目标（与探测器同口径：含 . 或 /），全部命中豁免前缀且不含
  # .. 穿越才判非 write；任一目标在仓库内、或引号内伪目标混入（如
  # 'echo "a > b.txt" > /tmp/x'）时保持 write 判定（保守方向=原行为）。
  # "> /dev/null" 旧特例由 /dev/null 豁免项吸收，顺带修掉其假阴性
  # （'cmd > /dev/null; cmd2 > real.txt' 旧版整体放行）。
  # 仅字面路径豁免（2026-08-25 review Major）：目标含 $ 或反引号（未展开的变量/
  # 命令替换）时静态不可判定真实落点——'cmd > /tmp/$(echo ../../repo)/x' 的提取
  # 目标在空白处截断成 '/tmp/$(echo'，.. 残留被丢弃会误判 not-write——一律保守判
  # write。副作用：'> $TMPDIR/x.log' 这类未展开形式也判 write（安全方向，可接受）。
  # （反引号字面量进不了 Go raw string，与下方 forge 命令检测同款 printf 构造。）
  local _rt _all_exempt=1 _t _BT
  _BT=$(printf '\140')
  _rt=$(printf '%s\n' "$s" | grep -oE '>>?[[:space:]]*[^[:space:]&|;]*[./][^[:space:]|;&]*' 2>/dev/null || true)
  if [ -n "$_rt" ]; then
    while IFS= read -r _t; do
      _t="${_t#>}"
      _t="${_t#>}"
      # 前导空白剥离（"> /tmp/x" 形态）
      _t="${_t#"${_t%%[![:space:]]*}"}"
      case "$_t" in
        *'$'*|*"${_BT}"*) _all_exempt=0; break ;;
        *..*) _all_exempt=0; break ;;
        /tmp/*|/private/tmp/*|/var/folders/*|/dev/null) ;;
        "${TMPDIR:-/tmp}"/*) ;;
        "$HOME/.forge"/*) ;;
        *) _all_exempt=0; break ;;
      esac
    done <<< "$_rt"
    [ "$_all_exempt" = "1" ] && return 1
  fi
  return 0
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
_TOUCHED_MARKER="${_MARKER_DIR}/forge-source-touched-${SESSION_ID}"
if [ $IS_WRITE_CMD -eq 1 ] && [ -z "$TASK_REF" ]; then
  if [ ! -f "$_TOUCHED_MARKER" ]; then
    exit 0
  fi
  NOWARN_FILE="${_MARKER_DIR}/forge-bashguard-nowarn-${SESSION_ID}"
  if [ ! -f "$NOWARN_FILE" ]; then
    touch "$NOWARN_FILE" 2>/dev/null || true
    echo "WARN [bash-guard] Bash write without active task. Changes are allowed but not tracked.（本会话仅提示一次）"
  fi
  exit 0
fi

# Mark as forge command for file-sentinel
if [ $IS_FORGE_CMD -eq 1 ]; then
  touch "${_MARKER_DIR}/forge-cmd-${SESSION_ID}" 2>/dev/null || true
fi

echo "PASS"`


const HazardGuardHook = `#!/bin/bash
# hazard-guard.sh — PreToolUse hook for Bash. on-demand-guards 自动挡：高危命令
# human-in-the-loop 拦截。
#
# 检测高危命令（rm -rf / git push --force / DROP TABLE / TRUNCATE / kubectl delete /
# DELETE 无 WHERE 等）→ block + 指引 agent 做授权判定（用户本回合已明确指令/确认过则
# 直接 confirm，无需二次确认；否则用所在工具的提问确认机制获用户确认）→
# forge hazard confirm 登记限时（5min）标记 → 重试原命令 → 本 hook 见标记放行。
#
# 免 HITL 的自动豁免（2026-08 两周 usage 复盘，详注在各判定函数上）：rm 目标全在
# 一次性临时区（/tmp、/var/folders、/private/tmp、$TMPDIR 子路径、本命令串可验证的
# mktemp 变量）；危险串仅在引号/注释/多行字符串内（数据上下文，exec 包裹除外）。
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

# --- rm 临时目录白名单 helpers（2026-08 两周 usage 复盘：mktemp/自建临时目录清理约占
# 1/3 误拦——agent 不走 confirm 而是悄悄删掉 rm 只跑后半截，留下 /tmp 垃圾，guard 赢了
# 命令输了意图；故把"可静态验证来源"的临时目录清理纳入白名单） ---
# safe_mktemp_vars 列出命令串中"安全的 mktemp 变量"（换行分隔）。提取在 strip_quotes
# 后的文本上做（杀引号内伪造赋值：echo 'd=$(mktemp -d)' 不是赋值）；含 << 时整体不豁免
# （heredoc 体内的赋值行不执行，静态分离不可靠——保守=不豁免）。候选还要求 X=$(mktemp
# 出现在可执行赋值位置（文本起点或 空格/;/&/| 之后）——x=d=$(mktemp -d) 里 = 后接的
# 伪赋值实际赋给 x。最后防线：剥掉全部 mktemp 赋值（不限于本变量——其他变量赋值的
# mktemp -d flag 含裸词 d，不剥会被词边界检查误判）与所有 $v/${v} 引用后，变量名以词
# 边界（两侧皆非标识符字符）出现在残余文本任何位置即作废——覆盖 for d in / 循环重绑、
# d[0]= 数组赋值（$d==${d[0]}）、d+= 累加、printf -v d / read d 无 = 重绑。
# 动态执行/重绑词面（eval / source / . / sh -c）→ 白名单整体作废（review C2）：
# eval 'd=/' / source f 都能在词法检查之后改写 d 的值，静态不可判定。词边界=两侧
# 空格/文本起点；误伤（文本里恰好提及 eval/source 字样）可接受——fail-closed 方向
# 安全，confirm 链兜底。sh -c 子串同时覆盖 bash -c（bash -c 含 "sh -c"）。
# BSD 安全：grep -oE 无 ERE 交替（bash-guard 同款教训）；边界检查三条独立 BRE grep；
# sed 用 BRE，( ) 在 BRE 里是字面。
safe_mktemp_vars() {
  local text v rest
  text=$(strip_quotes "$1" | tr '[:upper:]' '[:lower:]' | tr '\n\r' ';;' | tr -s '[:space:]' ' ')
  case "$text" in *'<<'*) return 0 ;; esac
  case "$text" in
    eval\ *|*\ eval\ *|source\ *|*\ source\ *|.\ *|*\ .\ *|sh\ -c*|*\ sh\ -c*) return 0 ;;
  esac
  {
    printf '%s' "$text" | grep -oE '^[a-z_][a-z0-9_]*=\$\(mktemp' 2>/dev/null
    printf '%s' "$text" | grep -oE '[ ;&|][a-z_][a-z0-9_]*=\$\(mktemp' 2>/dev/null
  } | sed 's/^[ ;&|]//; s/=\$(mktemp$//' | sort -u | while IFS= read -r v; do
    [ -z "$v" ] && continue
    rest=$(printf '%s' "$text" | sed "s/[a-z_][a-z0-9_]*=\$(mktemp[^)]*)//g")
    rest=$(printf '%s' "$rest" | sed "s/\$$v//g; s/\${$v}//g")
    printf '%s' "$rest" | grep -q "^$v[^a-z0-9_]" && continue
    printf '%s' "$rest" | grep -q "[^a-z0-9_]$v[^a-z0-9_]" && continue
    printf '%s' "$rest" | grep -q "[^a-z0-9_]$v$" && continue
    printf '%s\n' "$v"
  done
}

# is_tmp_rm_target 判定 rm 的一个目标 word 是否落在一次性临时区（白名单）：
#   1. 字面前缀 /tmp/、/var/folders/、/private/tmp/（原有，e2e/CI probe 清理形态）；
#   2. $TMPDIR 子路径——macOS 上 mktemp -d 默认落在 $TMPDIR，重建自建目录形态
#      rm -rf "$TMPDIR/pack" && mkdir "$TMPDIR/pack"。裸 $TMPDIR（无子路径）不豁免
#      ——那是清整个用户临时目录；
#   3. 本命令串内 mktemp 变量的**裸**引用（$d、${d}，含引号形态——分类前已剥双引号，
#      d 须过 safe_mktemp_vars 校验）——d=$(mktemp -d); ...; rm -rf "$d" 的建后即删
#      闭环。$d/sub 形态不豁免（review C1 闭合）：d 为空时 rm -rf $d 是缺操作数
#      无害，$d/x 才是 /x 危险形态——短路不执行（false && d=$(mktemp -d)）、函数体
#      （f(){ d=...; }）、env 前缀（d=$(mktemp -d) cmd）、管道子 shell、赋值在用后
#      等"赋值不生效"形态全因 d 空而在 $d/x 上显形，只认裸引用则全灭；原 $d/sub
#      自清理改走 confirm 链。
# 子路径共性规则（review M2）：前缀后的 sub 必须至少含一个非 / 非 . 字符——
# /tmp//、/tmp/./、$TMPDIR// 经内核折叠双斜杠/圆点后等于临时目录本身（清整个
# 临时区）。任何形态含 .. 穿越一律不豁免；$TMPDIR 子路径不得再含 $/反引号
# （未展开变量/命令替换可藏穿越，词面 .. 检查看不到展开值——bash-guard
# 2026-08-25 重定向目标同款教训）。rm -rf $(mktemp -d) 等直接 $() 目标仍拦。
# 调用方只传 is_hazardous 小写归一后的段文本（$lower），故 $TMPDIR 匹配小写形态。
# 残余已知限制（启发式边界，如实披露不逐一追堵——confirm 链是真门禁）：
# /tmp/$x 字面前缀+未展开变量后缀的穿越（x 展开可为 ../..）；双引号内 $(rm ...)
# 命令替换；eval/source 词面门控可被反斜杠转义（\eval）、引号化（'eval'）、
# 变量/命令替换间接调用绕过——属静态分析不可判定层。
is_tmp_rm_target() {
  local w="${1//\"/}"
  [[ $w == *..* ]] && return 1
  local v sub bt
  # 反引号字面量进不了 Go raw string（本脚本内嵌在 embed.go），用 printf 构造
  bt=$(printf '\140')
  if [[ $w == /tmp/* || $w == /var/folders/* || $w == /private/tmp/* ]]; then
    sub="${w#/tmp/}"
    sub="${sub#/var/folders/}"
    sub="${sub#/private/tmp/}"
    [[ $sub == *[!/.]* ]] && return 0
    return 1
  fi
  if [[ $w == \$tmpdir/?* || $w == \$\{tmpdir\}/?* ]]; then
    sub="${w#\$tmpdir/}"
    sub="${sub#\$\{tmpdir\}/}"
    [[ $sub == *'$'* || $sub == *"$bt"* || $sub != *[!/.]* ]] && return 1
    return 0
  fi
  for v in $mktemp_vars; do
    [[ $w == "\$$v" || $w == "\${$v}" ]] && return 0
  done
  return 1
}

# --- 高危命令检测 ---
# 第 2 参数 nowl（no-whitelist）：跳过 rm 临时目录白名单——data-context 判定时对
# STRIPPED 文本使用。STRIPPED 里引号内容已被剥走，rm -rf "$HOME" 剥成 rm -rf 零目标，
# 若套白名单的"零目标放行"会被当"危险串在引号内"误放——但危险的是 rm 本身，目标只是
# 恰好被引号包住（2026-08 复盘：rm -rf "$TMPDIR/../etc" 经 data-context 漏放）。
is_hazardous() {
  local cmd="$1"
  local nowl="${2:-}"
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
  # rm 临时目录白名单（/tmp、/var/folders、/private/tmp、$TMPDIR 子路径、可验证 mktemp
  # 变量——见 is_tmp_rm_target）下沉到 rm_hit 循环内按段隔离：
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
  # rm 白名单用的 mktemp 变量清单（is_tmp_rm_target 查用）：在段隔离循环前算好，
  # while read 子 shell 经动态作用域可见 $lower 同级的 local 变量。safe_mktemp_vars
  # 内部自行 strip_quotes + 小写归一（与段文本同口径），故传原始 $cmd。
  local mktemp_vars
  mktemp_vars=$(safe_mktemp_vars "$cmd")
  local rm_hit
  rm_hit=$(printf '%s\n' "$lower" | tr '&|;\n' '\n\n\n\n' | while IFS= read -r seg; do
    [ -z "$seg" ] && continue
    if printf '%s' "$seg" | grep -qi '^rm ' || printf '%s' "$seg" | grep -qi '[^a-z]rm '; then
      if printf '%s' "$seg" | grep -qi -- '-[a-z]*r[a-z]*f' || \
         printf '%s' "$seg" | grep -qi -- '-[a-z]*f[a-z]*r' || \
         { printf '%s' "$seg" | grep -qi -- ' -r' && printf '%s' "$seg" | grep -qi -- ' -f'; } || \
         { printf '%s' "$seg" | grep -qi -- '--recursive' && printf '%s' "$seg" | grep -qi -- '--force'; }; then
        # 白名单 arg-aware：rm -rf /tmp/x /important 会删 /important，不能因含 /tmp 子串整体放行。
        # 逐 word 查 rm 的目标参数，全部落在一次性临时区（is_tmp_rm_target：/tmp、/var/folders、
        # /private/tmp、$TMPDIR 子路径、本命令串可验证的 mktemp 变量）且无 .. 路径穿越才
        # continue；任一参数非白名单→不 continue，落 echo H block。只看目标路径不依赖
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
        # 全改 if [[ ]] + glob（bash 2.0+ 标准；glob 在 [[ ]] 内不进 case parser，绕开 bug）——
        # is_tmp_rm_target 内部同款，纯 [[ ]] 无 case。
        # -- 终止符置 past_dd=1（其后 word 按字面目标查，不跳过 - 开头文件名）；rm/sudo/flag 跳过。
        for word in $seg; do
          if [[ $past_dd = 1 ]]; then
            is_tmp_rm_target "$word" || all_tmp=0
            continue
          fi
          if [[ $word == -- ]]; then
            past_dd=1
            continue
          fi
          if [[ $word == rm || $word == sudo || $word == -* ]]; then
            continue
          fi
          is_tmp_rm_target "$word" || all_tmp=0
        done
        # nowl 模式跳过白名单 continue：零目标 rm -rf（目标被引号剥走）也落 echo H。
        [[ $all_tmp = 1 ]] && [ -z "$nowl" ] && continue
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
# 2026-08 两周 usage 复盘改进（只读分析脚本——python3 heredoc——因文本含 rm -rf 被
# 误拦 2 次；根因是字面 substring 规则无法区分"执行 rm"与"文本提到 rm"），review 复
# 审补强（critical：跨行持久若无转义/heredoc 感知，真危险可被当引号数据吞掉）：
#   - 引号状态跨行持久（sq/dq 在 BEGIN 初始化，不逐行重置）——多行引号字符串
#     （python heredoc 里的三引号字符串/docstring、多行 git commit -m 的 message 体）
#     里的危险串是数据不是执行，旧版逐行重置把跨行字符串的中间行当未引号文本误拦；
#   - 嵌套感知开合（对齐 bash 语义）：双引号内的单引号是字面量不切换 sq，反之亦然——
#     echo "don't"<换行>rm -rf /x 的撇号不得把 sq 状态泄漏到下一行（漏拦）；
#   - 反斜杠转义感知（esc 标志，跨行持久）：引号外 \" 是字面引号字符不开引号
#     （echo \"<换行>rm -rf /x 里 rm 真执行，误判开引号会吞掉它）；引号内 \" 不闭合
#     （echo "a\"b"）；sq 内 \ 是字面量（bash 语义）。转义字符原样输出（\" 两个字符
#     都保留）——STRIPPED 会被 safe_mktemp_vars 二次 strip，保持幂等；
#   - heredoc 感知：识别 <<[-]['"]?TAG['"]? opener（引号外、单 opener），体内行用独立
#     引号态（bsq/bdq）跟踪——python heredoc 的多行字符串仍按数据剥离；delimiter 行
#     （允许前导 tab，对齐 <<-）结束 heredoc 并重置体内状态——体内杂散引号
#     （He said "hi）绝不可泄漏到 delimiter 之后吞掉真危险命令。<<<（herestring，无
#     body）不触发 opener；数字开头 tag（cat <<2 与 $((1<<2)) 算术位移静态不可分）、
#     同行多 opener、<< 后无可解析 tag → fail-closed：后续行逐字透传（危险串保持
#     可见=维持拦截）。
# 保守方向不变：引号不平衡（未闭合）时其后文本全被剥掉——bash 对未闭合引号同样不
# 执行（语法错误/等待续行），方向安全；heredoc 体内未引号包裹的裸露危险串仍拦
# （可能是 cat > script.sh 在写可执行脚本，静态不可判定，维持拦截走 confirm 链）。
strip_quotes() {
  printf '%s' "$1" | awk '
    BEGIN{sq=0; dq=0; bsq=0; bdq=0; esc=0; hd=""; broken=0}
    {
      if(broken){print; next}
      line=$0
      # heredoc 体内：独立引号态处理；delimiter 行结束 heredoc 并重置体内状态
      if(hd!=""){
        rest=line
        sub(/^\t+/,"",rest)
        if(rest==hd){hd=""; bsq=0; bdq=0; print; next}
        out=""; prev=""
        for(i=1;i<=length(line);i++){
          c=substr(line,i,1)
          if(esc){esc=0; prev=c; if(!bsq && !bdq) out=out c; continue}
          if(!bsq && c=="\\"){esc=1; prev=c; if(!bdq) out=out c; continue}
          if(!bdq && c=="\x27"){bsq=!bsq; prev=c; continue}
          if(!bsq && c=="\""){bdq=!bdq; prev=c; continue}
          if(!bsq && !bdq && c=="#" && (prev==" "||prev==""||prev=="\t"||prev==";"||prev=="|"||prev=="&"||prev=="(")) break
          if(!bsq && !bdq) out=out c
          prev=c
        }
        print out
        next
      }
      # shell 层
      out=""; prev=""; lt=0; multi=0
      for(i=1;i<=length(line);i++){
        c=substr(line,i,1)
        if(esc){esc=0; prev=c; if(!sq && !dq) out=out c; continue}
        if(!sq && c=="\\"){esc=1; prev=c; if(!dq) out=out c; continue}
        if(!dq && c=="\x27"){sq=!sq; prev=c; continue}
        if(!sq && c=="\""){dq=!dq; prev=c; continue}
        # dogfood 3.2：# 注释行（非引号内、词边界处）剥到行尾。electron-builder "# Clean up"
        # 含危险串的注释被当执行误拦。prev 判定词边界（空格/行首/tab/;/|/&/( ）——# 紧跟
        # 非空白（如 foo#bar）是字面 #，非注释，不剥（其后的危险串仍被 is_hazardous 命中）。
        if(!sq && !dq && c=="#" && (prev==" "||prev==""||prev=="\t"||prev==";"||prev=="|"||prev=="&"||prev=="(")) break
        if(!sq && !dq){
          out=out c
          # heredoc opener 探测：引号外（且非转义）的 <<；同行第二个 << 置 multi
          if(c=="<" && substr(line,i+1,1)=="<"){
            if(lt>0){multi=1}else{lt=i}
          }
        }
        prev=c
      }
      print out
      if(multi){broken=1; next}
      if(lt>0){
        rest=substr(line,lt+2)
        # <<< herestring 无 body，不是 heredoc opener，不跟踪
        if(substr(rest,1,1)=="<"){next}
        sub(/^[ \t]+/,"",rest)
        sub(/^-/,"",rest)
        sub(/^[ \t]+/,"",rest)
        q=substr(rest,1,1)
        if(q=="\x27" || q=="\""){rest=substr(rest,2)}
        if(match(rest,/^[A-Za-z0-9_.-]+/)){
          tag=substr(rest,RSTART,RLENGTH)
          # 数字开头 tag（cat <<2）无法与 $((1<<2)) 算术位移静态区分——fail-closed
          # （review M1）：后续行逐字透传，危险串保持可见=维持拦截；算术位移命令
          # 本身无引号可剥，fail-closed 不改变其判定
          if(tag ~ /^[0-9]/){broken=1}else{hd=tag; bsq=0; bdq=0}
        } else {
          # << 后无可解析 tag——不完整/语法错误，fail-closed：后续行逐字透传
          broken=1
        }
      }
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
# git commit -m "fix rm -rf bug" / make build 注释含 rm -rf 类误判。strip_quotes 跨行
# 持久后，多行字符串（python heredoc 三引号串、多行 commit message）里的危险串同理放行。
# STRIPPED 用 nowl 复检（关 rm 白名单）：引号剥走目标后剩零目标的 rm -rf（原命令是
# rm -rf "$HOME" 这类"目标在引号里的执行"，不是"危险串在引号内"）必须维持拦截——
# 否则 rm -rf "$TMPDIR/../etc" / rm -rf "$HOME" 经此路径漏放。
STRIPPED=$(strip_quotes "$COMMAND")
if [ "$STRIPPED" != "$COMMAND" ] && ! is_hazardous "$STRIPPED" nowl && ! is_exec_wrapped "$COMMAND"; then
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
echo "  1. 授权判定：若用户在本回合已明确指令/确认过该操作（用户直接要求执行，或你"
echo "     此前已通过提问获得确认），无需二次确认——直接跳到第 2 步；否则先用你所在"
echo "     工具的提问确认机制向用户说明该操作的风险，获得用户的明确确认"
echo "  2. 运行: forge hazard confirm --last"
echo "     （确认最近一条被拦命令——从事件日志取指纹，零复制转写，不会抄错；"
echo "      也可用 forge hazard confirm --fingerprint \"$FP\" 回传上方 hex 指纹，"
echo "      勿传命令串——shell 会吃掉其中的引号致指纹失真）"
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
# itself. (The forge-cmd marker below moved to FORGE_DATA_DIR on 2026-08-23 —
# same agent-writable trust posture as TMPDIR, matching bash-guard's writer;
# the per-invocation snapshot baseline stays in TMPDIR by design.)
#
# 信任边界：snapshot/.ok/.cfg 基线在 agent 可写的 TMPDIR 里，被审计的命令可能
# 在 PreToolUse 与 PostToolUse 之间改写/伪造基线。本防御假设 TMPDIR 完整——
# 它拦意外与失控写入，不拦命令自身的对抗性篡改。（下方 forge-cmd 标记于
# 2026-08-23 迁至 FORGE_DATA_DIR——与 TMPDIR 同为 agent 可写的信任姿态，与
# bash-guard 写端一致；per-invocation snapshot 基线按设计留在 TMPDIR。）
set -eo pipefail

TASK_REF="${FORGE_TASK_REF:-}"
SESSION_ID="${FORGE_SESSION_ID:-default}"
: "${TMPDIR:=/tmp}"
FORGE_CMD_FILE="${FORGE_DATA_DIR:-${TMPDIR:-/tmp}}/markers/forge-cmd-${SESSION_ID}"

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
  # Read the manifest ONCE, then re-check existence: a concurrent file-sentinel
  # (parallel Bash tool calls in one session both glob the newest snapshot) can
  # run its cleanup rm between the -f test above and this read. Observed
  # 2026-08-17: the cat then printed "No such file or directory" to stderr AND
  # fed the diff an EMPTY manifest — whose symmetric difference flags every
  # current config file as drift (the false-quarantine direction). A sidecar
  # that vanished mid-read means the pairing is no longer ours to judge: honor
  # the "missing manifest = skip comparison" contract and skip. ($SNAPSHOT_FILE
  # and $SNAPSHOT_OK are not re-checked: their consumers already fail open on a
  # vanished file — empty BEFORE_ALL without .ok takes the WARN branch below.)
  CFG_MANIFEST_DATA=$(cat "$SNAPSHOT_CFG" 2>/dev/null || true)
  if [ -f "$SNAPSHOT_CFG" ]; then
    CFG_NOW=$(forge_cfg_manifest)
    # Symmetric difference of "hash  path" lines, reduced to paths: a changed
    # hash surfaces both the old and the new line (same path, deduped by sort -u);
    # an added/removed file surfaces its single line.
    CFG_FROM_MANIFEST=$(
      {
        printf '%s\n' "$CFG_NOW"
        printf '%s\n' "$CFG_MANIFEST_DATA"
      } | sort | uniq -u | awk '{$1=""; sub(/^ +/,""); print}' | sort -u || true
    )
  fi
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


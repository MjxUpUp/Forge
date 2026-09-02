package hooks

// embed_task.go —— embed.go 同包分文件产物（2026-09 普查 P7：2322 行单文件按职能
// 拆分；内容逐字节不变——embeddedHooks 名册与 guard test 钉住等价性）。

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

# Session-marker bootstrap（在 test/源码分流之前：测试分支也要写计数，bootstrap
# 须前移到分流之前；有活跃任务时两个分支都不写新标记）。dogfood 5.1：session-level
# source-touched marker. Setting it (when task-guard has confirmed FILE_PATH is
# source code) lets auto-compile + bash-guard distinguish research-only sessions
# from dev sessions. Per-session, keyed by FORGE_SESSION_ID.
: "${TMPDIR:=/tmp}"
_SESSION_ID="${FORGE_SESSION_ID:-default}"
# Session-marker root: FORGE_DATA_DIR first (injected by the Go layer — forge's
# own writable data home), ${TMPDIR:-/tmp} as fallback (script-level runs without
# the Go env). The old TMPDIR root silently lost markers on read-only-MSYS-/tmp
# machines (Git for Windows default install): "touch ... || true" swallowed the
# write error and the de-noise degraded to per-edit WARN spam (2026-08-23).
# mkdir -p closes the narrower window: DataDirFor is pure derivation — without
# it, a marker-writing event on a project whose DataDir doesn't exist yet (no
# prior hook created it) loses the marker the same silent way. Markers live in a
# dedicated markers/ subdir (not loose in the DataDir root next to managed
# state) and stale ones are pruned below — the DataDir is a permanent runtime
# home, unlike TMPDIR there is no implicit OS cleanup.
#
# 会话 marker 根目录：FORGE_DATA_DIR 优先（Go 层注入——forge 自己的可写 data
# home），${TMPDIR:-/tmp} 兜底（不经 Go 层 env 的脚本级运行）。旧 TMPDIR 根在
# MSYS /tmp 只读的机器上（Git for Windows 默认装法）静默丢标记："touch ...
# || true" 吞掉写错误，去噪退化成每次编辑 WARN 刷屏（2026-08-23）。mkdir -p
# 关闭更窄的窗口：DataDirFor 是纯推导不建目录——没有它，DataDir 尚不存在的
# 项目上写 marker 的事件会以同样方式静默丢标记。marker 收进专用 markers/
# 子目录（不散落在 DataDir 根与受管状态混放），并在下方清扫超龄——DataDir
# 是永久运行态 home，不像 TMPDIR 有系统隐式清理。
_MARKER_DIR="${FORGE_DATA_DIR:-${TMPDIR:-/tmp}}/markers"
mkdir -p "$_MARKER_DIR" 2>/dev/null || true
# Prune session markers older than 7 days: a session that old is long over, and
# its markers would otherwise accumulate forever.
#
# 清扫超过 7 天的会话 marker：那么老的会话早已结束，其标记否则会永久累积。
find "$_MARKER_DIR" -maxdepth 1 -type f -name 'forge-*' -mtime +7 -delete 2>/dev/null || true

# Test files — always allow (TDD workflow), but anchored: vNext P0-4 豁免≠不可见。
# 2026-08-30 事故里两个 _test.go 修复文件在无任务会话连 advisory 都未触发（本
# 检查先于一切 no-task 逻辑直接 exit 0）。放行语义不变；补一次性 FYI + 每会话
# 编辑计数（forge-test-edits-<session>，coverage 统计/审计数据源）。FYI 用独立
# [task-anchor] 谓词（主屏障——不会被 task-guard 的提升规则命中）且提升宿主
# 跳过输出（第二重保险——exit-2 语义下提示性文案不该出现），双保险防误阻断。
if printf '%s' "$FILE_PATH" | grep -qE '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/|__tests__/)'; then
  if [ -z "$TASK_REF" ]; then
    _TEST_CNT="${_MARKER_DIR}/forge-test-edits-${_SESSION_ID}"
    _n=$(cat "$_TEST_CNT" 2>/dev/null || true)
    case "$_n" in ''|*[!0-9]*) _n=0;; esac
    echo $(( _n + 1 )) > "$_TEST_CNT" 2>/dev/null || true
    if [ -z "${FORGE_TASKGUARD_PROMOTED:-}" ] && [ ! -f "${_MARKER_DIR}/forge-test-note-${_SESSION_ID}" ]; then
      touch "${_MARKER_DIR}/forge-test-note-${_SESSION_ID}" 2>/dev/null || true
      echo "WARN [task-anchor] FYI: test-file edits without an active task — kept allowed (TDD), untracked/unscored; notice once per session.（测试编辑已计数）"
    fi
  fi
  exit 0
fi

_TOUCHED_MARKER="${_MARKER_DIR}/forge-source-touched-${_SESSION_ID}"
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
  # On master/main or auto-creation failed: advisory spectrum with an ignore
  # counter (vNext P0-1/P0-3).
  #
  # FORGE_TASKGUARD_PROMOTED=1（Go 侧注入：本宿主把 task-guard advisory 提升为阻断
  # ——dsh 2026-08-22 与 zcode 2026-08-30 两例实证「通道送达但被无视」后入列，
  # hostcap PromoteAdvisory；kimi 的规则已于 2026-08-24 退役，改为 advisory 入队 +
  # UserPromptSubmit 攒发）时，本输出不再是 advisory 而是每次都发的 block reason：
  # 一次性标记在阻断语义下是新洞——模型重试同一编辑即静默放行；且许可式文案作
  # deny reason 自相矛盾。故提升路径每次输出指令式文案（含 Contains 谓词
  # [task-guard]、不含 Auto-created，仍照常提升）。
  #
  # FORGE_TASKGUARD_PROMOTED=1 (injected by the Go layer: this host promotes the
  # task-guard advisory to a block — dsh 2026-08-22 and zcode 2026-08-30, both
  # documented incidents where the delivered advisory was ignored; kimi's rules
  # were retired 2026-08-24 in favor of the advisory queue + UserPromptSubmit
  # drain). Under promotion this output is the block reason, emitted EVERY time.
  if [ -n "${FORGE_TASKGUARD_PROMOTED:-}" ]; then
    echo "WARN [task-guard] No active task. Source edit DENIED until one exists — run: forge task start --ref <ref> --branch --title <title> (creates branch + task on main/master), then retry the edit. Deliberate one-off fix instead: forge task wild \"<note>\", then retry."
    exit 0
  fi
  # 无视计数器（P0-3，取代一次性 NOWARN 去噪）：2026-08-30 事故里首条 advisory 被
  # 无视后，NOWARN 让后续二十余次编辑全部静默——「advisory 可无视」就此滑向偏差
  # 正常化。现在每次无任务源编辑 +1（首条之后的每次编辑即是对它的无视）：第 1 次
  # 发三段式 advisory（理由/期望行为/后果预告，清除 allowed 许可语义——8-30 转录
  # 实证「allowed」被 agent 读作放行声明）；第 2 次升档为指令级 STOP 文案；之后
  # 静默继续计数（保留 dogfood 3.1 的防刷屏约束；跨会话聚合与 promotion 队列属
  # P2）。计数文件 forge-taskguard-ignores-<session> 与其他会话 marker 同根同扫。
  _IGN="${_MARKER_DIR}/forge-taskguard-ignores-${_SESSION_ID}"
  _c=$(cat "$_IGN" 2>/dev/null || true)
  case "$_c" in ''|*[!0-9]*) _c=0;; esac
  _c=$(( _c + 1 ))
  echo "$_c" > "$_IGN" 2>/dev/null || true
  if [ "$_c" -eq 1 ]; then
    echo "WARN [task-guard] Untracked source edit — no active task. Why: changes outside a task skip verify/review/score gates. Do: run forge task start --ref <ref> --branch --title <title> (or forge task wild \"<note>\" for a deliberate one-off; forge next derives the step for you). Consequence: one more untracked edit this session escalates to a hard stop.（第 1 次提示）"
  elif [ "$_c" -eq 2 ]; then
    echo "WARN [task-guard] Second untracked source edit — stop editing and pick an exit first: forge task start --ref <ref> --branch --title <title>, or forge task wild \"<note>\" for a deliberate one-off (run forge next for guidance), then retry the edit. A grace window of 3 more edits is now open — remediate inside it or the breach is recorded.（已升档：第 2 次）"
    # 缓冲窗口开窗（vNext P3，设计 M2 定位置停止）：升档=拉绳，不是急停——线走到
    # 固定位（3 次编辑的窗口）才真停。开窗时刻单独立标记（window 计数器每编辑都
    # 改写，mtime 不可用作开窗时刻），补救判据要用它比对。
    : > "${_MARKER_DIR}/forge-taskguard-window-opened-${_SESSION_ID}" 2>/dev/null || true
    echo 0 > "${_MARKER_DIR}/forge-taskguard-window-${_SESSION_ID}" 2>/dev/null || true
  elif [ "$_c" -gt 2 ]; then
    # 窗口内：计数递增；补救判据 = wild 申报文件比开窗时刻新（task start 的补救
    # 走另一条路——建任务后本分支不再到达）。超窗一次落 violation 记录（agent 不
    # 可写路径由 forge enforcement 消费），文案一次性。
    _WIN="${_MARKER_DIR}/forge-taskguard-window-${_SESSION_ID}"
    _WOPEN="${_MARKER_DIR}/forge-taskguard-window-opened-${_SESSION_ID}"
    _VIOL="${_MARKER_DIR}/forge-taskguard-violation-${_SESSION_ID}"
    _w=$(cat "$_WIN" 2>/dev/null || true)
    case "$_w" in ''|*[!0-9]*) _w=0;; esac
    _w=$(( _w + 1 ))
    echo "$_w" > "$_WIN" 2>/dev/null || true
    _WILD="${FORGE_DATA_DIR:-${TMPDIR:-/tmp}}/wild/declarations.jsonl"
    _remediated=0
    if [ -f "$_WILD" ] && [ "$_WILD" -nt "$_WOPEN" ]; then
      _remediated=1
    fi
    if [ "$_remediated" -eq 0 ] && [ "$_w" -ge 3 ] && [ ! -f "$_VIOL" ]; then
      echo "$_c" > "$_VIOL" 2>/dev/null || true
      echo "WARN [task-guard] Grace window expired: ${_w} untracked edits after escalation with no task start and no wild declaration — breach recorded for audit (forge enforcement). Stop now and pick an exit: forge task start --ref <ref> --branch --title <title>, or forge task wild \"<note>\".（窗口超时：违规已记录）"
    fi
  fi
  exit 0
fi

echo "PASS"
`

const InitSuggestHook = `#!/bin/bash
# init-suggest.sh — SessionStart hook (advisory, non-blocking, global).
# 用户级"项目自动 init"检测：装了 forge（plugin/npm）后，用户在任意 git 项目开
# Claude Code，若无 .forge/ 且未登记 → 首次输出提示给 agent，引导询问用户是否启用 forge。
# 拒绝则永久静默（forge off / forge suggest decline 写 declined 标记 + 注册表状态，
# Project Policy Layer P1）。FORGE_AUTO_INIT=1 时直接 forge init（处处无感模式，
# 但 declined 不可被穿透——恢复唯一通道 forge on）。plugin auto-takeover：
# user-level plugin 已装时 git 项目静默自动 forge init（安装即 opt-in，declined
# 仍可每项目退出；v1.22 起 init 零项目写入，自动 init 不再污染项目）。补"每项目
# 手动 init"缺口——plugin install 接用户级 hooks+MCP+skills，项目注册表登记仍需
# forge init，本 hook 让这步从"用户记得敲"变"自动完成（plugin 用户）或 agent 主动
# 询问（npm 用户）"。advisory：默认不自动写文件（除 FORGE_AUTO_INIT 与 plugin
# auto-takeover），exit 0 不阻塞会话。
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
  echo "PASS [init-suggest] Advisory: 当前目录 '${DIR_NAME}' 不是 Git 仓库。建议先运行 'git init' 初始化版本控制，再运行 'forge init' 启用质量门禁（task-gated 源码变更 + 断言守卫 + 评分）。如不需要，运行 'forge off' 永久退出接管（兼容旧命令 'forge suggest decline'）。"
  exit 0
fi

# 已退出接管（declined）前置检查——先于成员检查 / FORGE_AUTO_INIT / plugin
# auto-takeover（Project Policy Layer P1）：declined 一票否决，任何默认开启路径
# （含 FORGE_AUTO_INIT——原"显式 env 不拦 declined"语义自 P1 起废除：退出不可被
# env 穿透，恢复唯一通道 forge on）都不得复活已退出项目。标记由 forge off /
# forge suggest decline 写入（注册表状态与标记双写，此检查在 P2 改为注册表驱动）。
# declined 检查读标记内容（= "declined"）而非仅存在——suggested 只静音询问，
# 不构成退出。
TAG0="${FORGE_CWD_TAG:-}"
DMARKER="${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested/$TAG0"
if [ -n "$TAG0" ] && [ "$(cat "$DMARKER" 2>/dev/null)" = "declined" ]; then
  exit 0
fi

# 已启用 forge（registry 成员或遗留 .forge/）→ 跳过建议。v1.22 起 init 默认
# 零项目写入（无 .forge/），成员资格必须问注册表——forge status 在已登记项目
# 里 exit 0，未登记 exit 非 0（declined 亦非 0：IsMember 不认 declined）；
# [ -d .forge ] 兜底老项目/团队模式。
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
# declined 已被上方前置检查拦截（P1 语义：退出不可被 env 穿透）。
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

# Plugin auto-takeover：user-level 装了 forge plugin = 显式 opt-in，git 项目静默自动
# forge init（v1.22 起 init 零项目写入，全部资产落 ~/.forge，自动 init 不污染仓库本身，
# "污染换无感"的历史顾虑已不成立）。declined 已被本函数顶部的前置检查拦截（P1：
# 每项目退出权高于一切默认开启路径）；suggested 标记不拦截（它只是静音询问，而这里
# 没有询问）。非成员才走到本分支（上方 forge status/[ -d .forge ] 已放行成员）。
# 失败回显 stderr 尾部（与 FORGE_AUTO_INIT 同款 partial-state 契约）；declined 项目
# 即便经旁路到达此处，Go 侧 init 门禁（ensureNotDeclined）也会拒绝——双层防线。
if forge plugin status >/dev/null 2>&1; then
  INIT_OUT=$( { cd "$ROOT" && forge init; } 2>&1 )
  RC="$?"
  if [ "$RC" = "0" ]; then
    echo "PASS [init-suggest] plugin auto-takeover: 已在 $ROOT 自动启用 forge（plugin 安装即接管；forge off 可退出该项目）。"
  else
    TAIL=$(printf '%s' "$INIT_OUT" | tail -c 400 | tr '\n' ' ')
    [ -z "$TAIL" ] && TAIL="(无 stderr 输出)"
    echo "PASS [init-suggest] Advisory: plugin 自动接管时 forge init 失败（exit $RC），请手动 'forge init'。错误尾部: $TAIL"
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
echo "PASS [init-suggest] Advisory: 检测到 git 项目 '$PROJ' 未启用 forge。询问用户是否启用 forge 质量门禁（task-gated 源码变更 + 断言守卫 + 评分）。用户同意 → 运行 'forge init'；拒绝 → 运行 'forge off' 永久退出该项目接管（兼容旧命令 'forge suggest decline'）。forge 默认不自动写文件（设 FORGE_AUTO_INIT=1 可处处自动 init；declined 项目不可被任何默认路径复活，恢复用 forge on）。"
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

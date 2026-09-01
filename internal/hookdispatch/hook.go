package hookdispatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/MjxUpUp/Forge/internal/worktree"
	"github.com/spf13/cobra"
)

// adoptPayloadCwd 在 hook payload 的 cwd 指向现存目录时把进程工作目录切过去。发生了
// chdir 则返回 true。原因见 runHook 调用点（kimi 插件 hook 从插件根启动，不是项目）。
// 拒绝相对路径：它会相对进程 cwd（kimi 下即插件根）解析，可能切到语义错误的位置
// ——各 host 实际都发绝对路径。chdir 失败（如 Windows 上的 UNC 路径）回落进程 cwd
// 并给 stderr 警告，绝不静默——静默失败正是本修复要消除的「项目级 hook 全空转」
// 盲点。
func adoptPayloadCwd(cwd string) bool {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return false
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return false
	}
	wd, err := os.Getwd()
	if err == nil {
		// 同目录 → 无事可做。先按 inode 同一性比较（os.SameFile），symlink 鲁棒：
		// macOS 上 os.TempDir/Getwd 横跨 /var → /private/var 符号链接，os.Getwd 返回
		// 的物理路径（/private/var/...）永不等 payload cwd 携带的未解析形式
		// （/var/...）——纯字符串比较会误判"不同"导致每次都 chdir，no-op 契约破裂
		// （v0.27.2 projectroot 同类）。下方 Clean 路径回落仍覆盖 Windows 大小写
		// 折叠（E:\Forge vs e:\forge）。
		if cur, e := os.Stat(wd); e == nil && os.SameFile(cur, info) {
			return false
		}
		a, _ := filepath.Abs(wd)
		b, _ := filepath.Abs(cwd)
		if runtime.GOOS == "windows" {
			a, b = strings.ToLower(a), strings.ToLower(b)
		}
		if filepath.Clean(a) == filepath.Clean(b) {
			return false
		}
	}
	if err := os.Chdir(cwd); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: adopt payload cwd %q failed: %v (falling back to process cwd)\n", cwd, err)
		return false
	}
	return true
}

// HookInput represents the JSON that Claude Code sends to a hook via stdin.
//
// HookInput 表示 Claude Code 通过 stdin 发给 hook 的 JSON。
type HookInput struct {
	SessionID     string          `json:"session_id"`
	HookEventName string          `json:"hook_event_name"`
	Cwd           string          `json:"cwd"` // 会话项目目录（kimi/Claude Code 均发送）：插件 hook 的进程 cwd 可能是插件根，项目根解析以它为准
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolOutput    json.RawMessage `json:"tool_response,omitempty"` // Claude Code PostToolUse 实际字段名是 tool_response（非 tool_output）；skill-trigger 是首个消费其内容的 hook
	Prompt        string          `json:"prompt,omitempty"`        // UserPromptSubmit 顶层 prompt（skill-trigger coding_intent condition 用）
	// ConversationID is cursor's session identity on tool/Stop/prompt events —
	// cursor's common hook schema carries session_id ONLY on sessionStart/
	// sessionEnd, everything else has conversation_id. Read as a fill-empty
	// fallback for SessionID (below), so cursor events get session-scoped keys
	// instead of collapsing onto the legacy global file. Host-agnostic: any
	// Claude-shape host sending conversation_id benefits.
	//
	// ConversationID 是 cursor 在工具/Stop/prompt 事件上的会话身份——cursor 的
	// 通用 hook schema 仅在 sessionStart/sessionEnd 携带 session_id，其余事件
	// 只有 conversation_id。作为 SessionID 的填空回落读取（见下），使 cursor
	// 事件获得 session-scoped 键而非全挤到 legacy 全局文件。宿主无关：任何发
	// conversation_id 的 Claude 形宿主都受益。
	ConversationID string `json:"conversation_id,omitempty"`
	// WorkspaceRoots is cursor's common-schema project locator (docs: array of
	// workspace folders, always present). Cursor's payload has NO cwd field and
	// its user-level hooks run from ~/.cursor — without this fill, findProjectRoot
	// resolves against ~/.cursor, fails, and every project-scoped hook silently
	// no-ops (review MAJOR-1, 2026-08-22). Read as a fill-empty for Cwd (first
	// root) in the payload-fallback block, BEFORE adoptPayloadCwd — same pattern
	// as cline (whose normalizer maps workspaceRoots[0], the camelCase variant,
	// for exactly this reason).
	//
	// WorkspaceRoots 是 cursor 通用 schema 的项目定位字段（文档：workspace 文件夹
	// 数组，恒在场）。cursor 的 payload **没有** cwd 字段、用户级 hook 从
	// ~/.cursor 运行——不填这一笔，findProjectRoot 按 ~/.cursor 解析必败、所有
	// 项目级 hook 静默空转（复审 MAJOR-1，2026-08-22）。在 payload 回落块里作为
	// Cwd 的填空（取首个 root）、位于 adoptPayloadCwd **之前**——与 cline 同模式
	// （其 normalizer 因同理映射 workspaceRoots[0]，camelCase 变体）。
	WorkspaceRoots []string `json:"workspace_roots,omitempty"`
	// ForgeAgent lets a host that constructs Claude-shape stdin in-process
	// declare its identity WITHOUT touching the hook command string — opencode's
	// TS plugin sets forge_agent:"opencode" in buildPayload (its wiring test
	// pins the `forge hook <name>` command roster, so an --agent suffix there
	// would be churn; a payload field is invisible to it). Lowest precedence in
	// resolveHookAgent's chain (after --agent and FORGE_HOOK_AGENT).
	//
	// ForgeAgent 让在进程内构造 Claude 形 stdin 的宿主无需改动 hook 命令串即可
	// 声明身份——opencode 的 TS plugin 在 buildPayload 里设
	// forge_agent:"opencode"（其 wiring 测试钉死 `forge hook <name>` 命令名册，
	// 在那里加 --agent 后缀是无谓 churn；payload 字段对它不可见）。在
	// resolveHookAgent 链中优先级最低（位于 --agent 与 FORGE_HOOK_AGENT 之后）。
	ForgeAgent string `json:"forge_agent,omitempty"`
	// Error is the top-level error text the host sends on PostToolUseFailure (Bash
	// failures carry "Exit code N" + stderr there) — consumed by the failure-track
	// hook for the compile/test failure heuristic and the checklog observation.
	//
	// Error 是宿主在 PostToolUseFailure 上发的顶层错误文本（Bash 失败携带
	// "Exit code N" + stderr）——供 failure-track hook 的编译/测试失败启发式与
	// checklog 观察记录消费。
	Error string `json:"error,omitempty"`
	// ErrorMessage is cursor's postToolUseFailure failure text (official docs:
	// "Description of the failure", sent ALONGSIDE the failure_type enum —
	// Claude/copilot carry the text as the top-level error instead). First in
	// Error's fill-empty chain below: real text always beats the enum class.
	//
	// ErrorMessage 是 cursor postToolUseFailure 的失败文本（官方文档："Description
	// of the failure"，与 failure_type 枚举**同发**——Claude/copilot 则把文本放在
	// 顶层 error）。是下方 Error 填空链的第一优先：真实文本恒胜过枚举分类。
	ErrorMessage string `json:"error_message,omitempty"`
	// FailureType is cursor's postToolUseFailure classification enum (official
	// docs: error/timeout/permission_denied; spec-research4 cross-host matrix).
	// Last in Error's fill-empty chain — a defensive fallback for payloads that
	// ship only the class. Enum values match no compile marker, so a class-only
	// payload records the class without firing a false nudge.
	//
	// FailureType 是 cursor postToolUseFailure 的分类枚举（官方文档：error/
	// timeout/permission_denied；spec-research4 跨宿主矩阵）。Error 填空链的最后
	// 兜底——对只带分类的 payload 的防御性回落。枚举值不命中任何编译 marker，
	// 仅分类的 payload 记录分类而不会误发提示。
	FailureType string `json:"failure_type,omitempty"`
	// AgentID/AgentTypeHook/LastAssistantMessage are SubagentStop fields (official
	// Claude Code hooks schema): the finishing sub-agent's identity and final message.
	// Consumed by subagent-track for attribution — sessions.jsonl missed agent_type
	// for ~53% of sessions before sub-agent activity had any forge-side record.
	// AgentTypeHook (not AgentType) to avoid colliding with the CLI's existing
	// agent-resolution vocabulary in this file.
	//
	// AgentID/AgentTypeHook/LastAssistantMessage 是 SubagentStop 字段（官方
	// Claude Code hooks schema）：结束中子 agent 的身份与最终消息。供
	// subagent-track 做归因——在子 agent 活动有 forge 侧记录之前，sessions.jsonl
	// 约 53% 会话缺 agent_type。命名 AgentTypeHook（非 AgentType）以避免与本
	// 文件既有的 agent 解析词汇冲突。
	AgentID              string `json:"agent_id,omitempty"`
	AgentTypeHook        string `json:"agent_type,omitempty"`
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
	// SubagentType/SubagentStatus/SubagentResult are cursor's subagentStop field
	// names (official docs: subagent_type, status "completed"/"error", result) —
	// cursor spells what CC/copilot call agent_type/last_assistant_message
	// differently. Fill-empty in the payload-fallback block, so cursor entries get
	// real attribution instead of a permanent agent_type=unknown; status rides in
	// subagent-track's Meta (completed vs error is funnel signal).
	//
	// SubagentType/SubagentStatus/SubagentResult 是 cursor subagentStop 的字段名
	// （官方文档：subagent_type、status "completed"/"error"、result）——cursor 对
	// CC/copilot 的 agent_type/last_assistant_message 换了拼法。在 payload 回落块
	// 填空，让 cursor 条目拿到真实归因而非永久 agent_type=unknown；status 随
	// subagent-track 记进 Meta（completed 与 error 之分是漏斗信号）。
	SubagentType   string `json:"subagent_type,omitempty"`
	SubagentStatus string `json:"status,omitempty"`
	SubagentResult string `json:"result,omitempty"`
}

// toolInputFields holds the fields extracted from the tool_input JSON.
//
// toolInputFields 持有从 tool_input JSON 抽取的字段。
type toolInputFields struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
	Command  string `json:"command"` // Bash 的 tool_input.command
	// OldString/NewString are Edit's tool_input fields — assertion-check's
	// per-edit mode analyzes exactly the change this call introduces
	// (old→new), instead of scanning the whole stale worktree diff (the
	// triple/quadruple-repeat advisory root cause, 2026-08-24).
	//
	// OldString/NewString 是 Edit 的 tool_input 字段——assertion-check 的
	// per-edit 模式只分析本次调用引入的变化（old→new），不再扫整个陈旧
	// 工作区 diff（三连发/四连发重复 advisory 的根因，2026-08-24）。
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// maxChecklogDetail is the truncation cap for a checklog entry detail.
//
// maxChecklogDetail 是 checklog entry detail 的截断上限。
const maxChecklogDetail = 500

// HookCmd is the hidden `forge hook <name>` command — the dispatcher every
// host's hook wiring invokes.
//
// HookCmd 是隐藏的 `forge hook <name>` 命令——所有 host 的 hook 接线调用的
// 分发器。
var HookCmd = &cobra.Command{
	Use:    "hook <name>",
	Short:  "Run an embedded hook script by name",
	Long:   "Executes the named hook script embedded in the forge binary. Extracts fields from Claude Code's stdin JSON into env vars, runs the script, and wraps its plain-text output into structured JSON.",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	// Silence cobra's own error/usage printing: on kimi a block's stderr IS the reason
	// shown to the model — cobra's "Error: ..." + usage dump would pollute it. runHook
	// prints what each host needs itself; Execute handles the exit code.
	//
	// 静默 cobra 自己的错误/usage 打印：kimi 下阻断的 stderr 就是展示给模型的
	// 原因——cobra 的 "Error: ..." + usage 会污染它。runHook 自己打印各宿主需要
	// 的内容；退出码由 Execute 处理。
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          RunHook,
}

// hookAgent 指定非 Claude Code 的宿主。由各 agent 的 translator 通过跨平台
// `--agent` flag 设置；它同时选择要 normalize 的 stdin 方言（windsurf/kimi/
// reasonix/cline 与 Claude 形状不同）**和**要输出的协议（见 EmitAgentOutput——
// codex/cursor/copilot 的 stdin 与 Claude 同形，但 stdout/退出码契约不同）。
// opencode/codebuddy 在进程内构造 Claude-shape stdin 且说 Claude 协议，
// 故不带 flag。FORGE_HOOK_AGENT 是已通过 env 接线的 translator（以及设 env 的
// TS 代码）的兜底。
// SkillTriggerHookFn is the seam for the skill-trigger in-process special path
// (defined in cli's skill_trigger.go — registered under cliskills.Root). The cli
// registrar injects it; nil (unit-test binaries without the registrar) silently
// passes.
//
// SkillTriggerHookFn 是 skill-trigger 进程内特例路径的接缝（实现在 cli 的
// skill_trigger.go——挂在 cliskills.Root 下、依赖 cli 会话上下文），由 cli
// 注册器注入；nil（无注册器的单测二进制）静默跳过。
var SkillTriggerHookFn func(hookInput HookInput, root, version, agent string) error

var hookAgent string

func init() {
	HookCmd.Flags().StringVar(&hookAgent, "agent", "", "host agent: selects the stdin dialect AND the output protocol (windsurf|kimi|reasonix|codex|cursor|copilot|cline|zcode)")
}

// resolveHookAgent 决定说话的宿主 agent。--agent flag（由 translator 设置，跨平台
// ——Windows cmd 无法解析 ENV=val cmd）优先；FORGE_HOOK_AGENT 是改走 env 接线的
// 调用方（以及在 spawn forge 前设 env 的 TS 扩展）的兜底。该值同时驱动 stdin
// normalizer（空 = Claude-Code-shape stdin、无需 normalize）与输出 emitter
// （emitAgentOutput）——codex/cursor/copilot 的 stdin 与 Claude 同形，但 stdout/
// 退出码协议不同，故为输出侧携带 flag。空串（claude-code，以及在进程内构造
// Claude stdin 的 opencode/codebuddy）表示两侧都按 Claude 处理。
func resolveHookAgent(flagVal, envVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return envVal
}

// isGlobalHook 判断某 hook 是否独立于 forge project 运行。Global hook 扫描
// $HOME 级别状态（skill-scan → ~/.claude/skills）或 cwd 级别状态
// （init-suggest → 检测 cwd 是否为 forge candidate；mcp-scan → 扫描项目级
// .mcp.json），这些在任何 project 都相关——所以 findProjectRoot 失败时
// （非 forge project）runHook 不可静默跳过它们。init-suggest 与 mcp-scan
// 必须在非 forge project 中运行：init-suggest 正是在那里发现 forge-candidate
// 项目，mcp-scan 则捕获用户 clone 的项目（可能永远不跑 forge init）里的
// 恶意 .mcp.json。Project-scoped hook（task-guard、file-sentinel 等）保持原有
// allow-and-exit 行为。
func isGlobalHook(name string) bool {
	return name == "skill-scan" || name == "init-suggest" || name == "mcp-scan" || name == "skill-trigger"
}

// isInProcessHook 列出完全在 runHook 内用 Go 处理的 hook（无 bash embed 脚本）：
// 它们需要 stdin 的实时 HookInput 字段（skill-trigger：event/prompt/tool_output；
// failure-track：error 文本；subagent-track：agent_id/agent_type；test-nudge：
// file_path；conventions-context/write：event/file_path + forge 数据），
// thin-wrapper bash 永远拿不到——runHook 已把 stdin 消费掉。各自的分发点在
// RunHook 里 skill-trigger 特例之后。
func isInProcessHook(name string) bool {
	return name == "skill-trigger" || name == "failure-track" || name == "subagent-track" || name == "test-nudge" ||
		name == "conventions-context" || name == "conventions-write"
}

// RunHook is the RunE of `forge hook <name>`: reads host stdin JSON, resolves
// the agent dialect, and dispatches to the named script or in-process hook.
//
// RunHook 是 `forge hook <name>` 的 RunE：读 host stdin JSON、解析 agent 方言，
// 分发到具名脚本或进程内 hook。
func RunHook(cmd *cobra.Command, args []string) error {
	name := args[0]
	content, ok := hooks.EmbeddedContent(name)
	// skill-trigger / failure-track / subagent-track / test-nudge 走 runHook 特例
	// （Go 内判定，不经 bash embed），无 embed script——放行其 name，特例在
	// hookInput 解析 + agent normalize 之后拦截 return（见 isInProcessHook）。
	if !ok && !isInProcessHook(name) {
		return fmt.Errorf("unknown hook: %s", name)
	}

	// Resolve the stdin dialect before parsing: a host whose StdinReplacesParse is
	// set (hostcap registry; today kimi — its prompt field is a content-block
	// array that would type-error a plain unmarshal into HookInput and warn on
	// every call) replaces the default Claude unmarshal entirely, so the agent
	// must be known first.
	//
	// 先解析 stdin 方言：StdinReplacesParse 置位的宿主（hostcap 注册表；目前仅
	// kimi——它的 prompt 字段是 content-block 数组，直接 unmarshal 进
	// HookInput 会类型错误并在每次调用时告警）完全替代默认的 Claude
	// unmarshal，所以必须先知道 agent。
	agent := resolveHookAgent(hookAgent, os.Getenv("FORGE_HOOK_AGENT"))
	host := hostcap.Lookup(agent)

	// 1. Read the host's stdin JSON.
	//
	// 1. 读取宿主 agent 的 stdin JSON。
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		// The read failure itself must stay visible: stdin is the hook's only input
		// channel, and an empty-input fail-open with zero trace is undiagnosable.
		fmt.Fprintf(os.Stderr, "[forge] warning: hook stdin read failed: %v\n", err)
		stdinData = []byte{}
	}

	var hookInput HookInput
	if host != nil && host.StdinReplacesParse {
		normalizeAgentStdin(agent, stdinData, &hookInput)
	} else {
		if len(stdinData) > 0 {
			if err := json.Unmarshal(stdinData, &hookInput); err != nil {
				// Log the parse failure for diagnosis, but continue with empty input.
				//
				// 记录解析失败以便诊断，但仍以空输入继续。
				fmt.Fprintf(os.Stderr, "[forge] warning: hook stdin JSON parse failed: %v\n", err)
			}
		}
	}

	// 1b. Normalize the stdin of non-Claude-Code agents BEFORE adopting the payload cwd
	// and resolving the project root. Windsurf/cline/reasonix use a different hook stdin
	// schema (Windsurf: {agent_action_name, trajectory_id, tool_info}; cline:
	// {hookName, taskId, workspaceRoots, ...}); without this step forge would extract
	// empty file_path/command and blocking hooks (task-guard/bash-guard) would fail open.
	// The ordering is load-bearing for cline: its payload has NO cwd field — the project
	// dir only reaches hookInput.Cwd when clineNormalize maps workspaceRoots[0] (and
	// taskId→SessionID likewise). Normalizing after adoptPayloadCwd/findProjectRoot (the
	// original position) left cline's Cwd mapping as dead code: findProjectRoot resolved
	// against the process cwd, and when cline spawns the wrapper outside the workspace
	// every project-scoped hook silently allowed — the exact fail-open class
	// adoptPayloadCwd was built to close for kimi. The `--agent` flag (cross-platform,
	// set by the translator) selects the dialect; FORGE_HOOK_AGENT is the fallback.
	// opencode are code-based and directly construct Claude stdin in TS, so no
	// normalizer runs for them. StdinReplacesParse dialects (kimi) already
	// normalized at parse time (see above); the other dialects (StdinDialect set
	// in the hostcap registry) normalize here.
	//
	// 1b. 在采用 payload cwd、解析项目根**之前**归一化非 Claude Code agent 的 stdin。
	// Windsurf/cline/reasonix 使用不同的 hook stdin schema（Windsurf:
	// {agent_action_name, trajectory_id, tool_info}；cline: {hookName, taskId,
	// workspaceRoots, ...}）；不做这步，forge 会抽出空的 file_path/command，拦截类
	// hook（task-guard/bash-guard）会 fail open。时序对 cline 是承重的：其 payload
	// 没有 cwd 字段——项目目录只有在 clineNormalize 映射 workspaceRoots[0] 时才
	// 进入 hookInput.Cwd（taskId→SessionID 同理）。若在 adoptPayloadCwd/
	// findProjectRoot 之后归一化（原位置），cline 的 Cwd 映射就是死代码：
	// findProjectRoot 按进程 cwd 解析，当 cline 在 workspace 之外拉起 wrapper 时
	// 所有项目级 hook 静默放行——正是 adoptPayloadCwd 为 kimi 堵上的那类 fail-open。
	// `--agent` flag（跨平台，由 translator 设置）选择方言；FORGE_HOOK_AGENT 是
	// 兜底。opencode 是 code-based，直接在 TS 里构造 Claude stdin，无需
	// normalizer。StdinReplacesParse 方言（kimi）已在 stdin 解析阶段完成
	// normalize（见上文）；其余方言（hostcap 注册表中 StdinDialect 非空的宿主）
	// 在此归一化。
	if host != nil && host.StdinDialect != "" && !host.StdinReplacesParse {
		normalizeAgentStdin(agent, stdinData, &hookInput)
	}

	// Payload-borne identity/dialect fallbacks (need the parsed stdin, so they run
	// after normalize): cursor's conversation_id fills an empty SessionID (its
	// tool/Stop/prompt events carry no session_id); opencode's forge_agent fills
	// an empty agent (its TS plugin declares identity in the payload — see
	// HookInput.ForgeAgent). All fill-empty: an explicit --agent, a real
	// session_id, or a real error string always wins. cursor's schema gaps fill
	// the same way (review 2026-08-22): workspace_roots[0] fills an empty Cwd
	// (cursor's payload has no cwd and its user-level hooks run from ~/.cursor —
	// without the fill, findProjectRoot fails and every project-scoped hook
	// silently no-ops); error_message then failure_type fill an empty Error (text
	// first, enum last); subagent_type/subagent_result fill empty SubagentStop
	// attribution fields (cursor's spellings of agent_type/last_assistant_message).
	//
	// 由 payload 携带的身份/方言回落（需要已解析的 stdin，故在 normalize 之后
	// 运行）：cursor 的 conversation_id 填空的 SessionID（其工具/Stop/prompt 事件
	// 不带 session_id）；opencode 的 forge_agent 填空的 agent（其 TS plugin 在
	// payload 里声明身份——见 HookInput.ForgeAgent）。全部填空：显式 --agent、
	// 真实 session_id、真实 error 文本恒优先。cursor 的 schema 缺口以同模式补
	// （复审 2026-08-22）：workspace_roots[0] 填空的 Cwd（cursor payload 无
	// cwd、用户级 hook 从 ~/.cursor 运行——不填则 findProjectRoot 失败、所有
	// 项目级 hook 静默空转）；error_message 再 failure_type 填空的 Error（文本
	// 优先，枚举兜底）；subagent_type/subagent_result 填空的 SubagentStop 归因
	// 字段（cursor 对 agent_type/last_assistant_message 的拼法）。
	if hookInput.SessionID == "" {
		hookInput.SessionID = hookInput.ConversationID
	}
	if agent == "" {
		agent = hookInput.ForgeAgent
	}
	if hookInput.Cwd == "" && len(hookInput.WorkspaceRoots) > 0 {
		hookInput.Cwd = hookInput.WorkspaceRoots[0]
	}
	if hookInput.Error == "" && hookInput.ErrorMessage != "" {
		hookInput.Error = hookInput.ErrorMessage
	}
	if hookInput.Error == "" {
		hookInput.Error = hookInput.FailureType
	}
	if hookInput.AgentTypeHook == "" {
		hookInput.AgentTypeHook = hookInput.SubagentType
	}
	if hookInput.LastAssistantMessage == "" {
		hookInput.LastAssistantMessage = hookInput.SubagentResult
	}

	// 1b-2. Session-id single entry point (fix/cleanup-batch, 2026-08-29): sanitize
	// the session id ONCE here, after the payload fallbacks (so a conversation_id
	// fill is sanitized too) and before ANY downstream consumption (registration,
	// attribution, checklog records/queries, env for thin wrappers). Before this,
	// record sites were split — hook.go sanitized at Record time while hook_track.go
	// recorded the RAW id — so any host id whose sanitized form differs (dots,
	// >64 chars) produced checklog rows the sanitized session-scoped readers
	// (LatestByCheckForSessionSince compares exact strings) could never match.
	// Downstream util.SanitizeSessionID calls stay (defense in depth): the function
	// converges after ≤2 applications — pinned by TestSanitizeSessionIDConverges —
	// so entry-sanitize + downstream-sanitize is uniform everywhere.
	// Empty stays empty: SanitizeSessionID("") would return "session", and the
	// empty-vs-nonempty distinction below (legacy global path, degraded-mode
	// skips) is load-bearing.
	//
	// 1b-2. session id 入口统一（fix/cleanup-batch，2026-08-29）：在 payload 回落
	// 之后（conversation_id 填空也被归一）、任何下游消费（登记/归因/checklog 记录
	// 与查询/thin wrapper 的 env）之前，把 session id 在此【一次性】归一。此前记录点
	// 分裂——hook.go 记录时归一而 hook_track.go 记【原始值】——凡 sanitized 形态与
	// 原值不同的宿主 id（点号、超 64 字符）产出的 checklog 行，按 sanitized 过滤的
	// 会话级读方（LatestByCheckForSessionSince 精确串比较）永远匹配不上。下游的
	// util.SanitizeSessionID 调用保留（纵深防御）：该函数至多 2 次应用后收敛——由
	// TestSanitizeSessionIDConverges 钉住——入口归一 + 下游归一处处一致。空保持空：
	// SanitizeSessionID("") 会返回 "session"，而下方空/非空的区分（legacy 全局路径、
	// 降级模式跳过）是承重的。
	if hookInput.SessionID != "" {
		hookInput.SessionID = util.SanitizeSessionID(hookInput.SessionID)
	}

	// Adopt the payload's cwd before resolving the project root. kimi plugin hooks are
	// spawned with the process cwd set to the plugin root (~/.kimi-code/plugins/managed/<id>)
	// — never the session project (verified on kimi 0.31.0; matches kimi docs "each hook
	// runs with its working directory set to the plugin root"). Resolving the project from
	// the process cwd then makes findProjectRoot fail and every project-scoped hook bail
	// with a silent allow — the whole gate layer (tool-track/auto-compile/task-guard/
	// read-before-edit/task-resume/...) silently no-ops, which is exactly the "kimi
	// PostToolUse 未分发" symptom. The payload's cwd is the session's real project dir
	// (kimi and Claude Code both send it) — the authoritative location. Adopted only when
	// it names an existing directory; otherwise the process cwd is used as before.
	//
	// 解析项目根之前先采用 payload 的 cwd。kimi 插件 hook 以插件根目录为进程 cwd 拉起
	// （~/.kimi-code/plugins/managed/<id>）——不是会话项目（kimi 0.31.0 实测，与 kimi
	// 文档「hook 以插件根为工作目录运行」一致）。按进程 cwd 解析会让 findProjectRoot
	// 失败、所有项目级 hook 静默放行——整个门禁层（tool-track/auto-compile/task-guard/
	// read-before-edit/task-resume/...）静默空转，正是「kimi PostToolUse 未分发」的
	// 表象。payload 的 cwd 是会话真实项目目录（kimi 与 Claude Code 均发送）——权威
	// 位置。仅当其指向现存目录时采用，否则回落进程 cwd（原行为）。
	adoptPayloadCwd(hookInput.Cwd)

	// Not in a forge project — output allow and exit silently.
	// Global hook (skill-scan scans $HOME/.claude/skills) is relevant in any project,
	// so it must run even without a forge project root.
	//
	// 不在 forge project 中——输出 allow 并静默退出。
	// Global hook（skill-scan 扫 $HOME/.claude/skills）在任何 project 都相关，
	// 故即便没有 forge project root 也要运行。
	root, err := projectroot.Find()
	if err != nil {
		if !isGlobalHook(name) {
			// Allow silently for every host: exit 0 with no stdout is a legal allow on
			// all supported protocols (claude/codex/cursor/copilot/windsurf/cline). The
			// old `{"decision":"approve"}` JSON envelope was noise on hosts that don't
			// parse stdout JSON, and decision:"approve" would bypass the permission
			// flow on Claude PreToolUse — an allow hook must not grant permissions.
			//
			// 对所有宿主静默放行：exit 0 且无 stdout 在全部受支持协议上都是合法
			// allow（claude/codex/cursor/copilot/windsurf/cline）。旧的
			// `{"decision":"approve"}` JSON envelope 在不解析 stdout JSON 的宿主上是
			// 噪声，且 decision:"approve" 会在 Claude PreToolUse 上绕过权限流程——
			// allow hook 不得授予权限。
			return nil
		}
		root = "" // global hook：无需 project root；shCmd.Dir="" 回退到 cwd
	}

	// Register the hook-observed session and stamp the resolved agent onto it,
	// best-effort. Previously this was stamp-ONLY (fill an empty AgentType on a
	// record created elsewhere) — but the only registration point was the CLI
	// path (`forge task start` → EnsureSession), and hosts whose agent drives
	// forge through a Bash tool without identity env (kimi/codex/cursor/...)
	// never reach it with a real session id, so their sessions were NEVER
	// registered (sessions.jsonl carried agent_type=claude-code only, fleet-wide,
	// 2026-08 attribution audit). EnsureHookSession closes that: any hook event
	// with a session id registers the session, with the declarative --agent as
	// AgentType (falling back to the project marker when agent==""). The legacy
	// global path (no session id) keeps the old stamp-only behavior — a hook
	// without a session id must not rotate legacy state.
	//
	// 登记 hook 观察到的会话并把解析出的 agent 盖上去，尽力而为。此前这里只做
	// 盖戳（在别处创建的记录上填空的 AgentType）——但唯一登记点是 CLI 路径
	// （`forge task start` → EnsureSession），而 agent 经无身份 env 的 Bash
	// 工具驱动 forge 的宿主（kimi/codex/cursor/...）从不以真实 session id 走到
	// 它，故其会话从未被登记（sessions.jsonl 全机只有 agent_type=claude-code，
	// 2026-08 归因审计）。EnsureHookSession 堵上此缺口：任何带 session id 的
	// hook 事件都会登记会话，AgentType 用声明式 --agent（agent=="" 时回落项目
	// 标记）。legacy 全局路径（无 session id）保持旧的只盖戳行为——无 session
	// id 的 hook 不得触发 legacy 轮换。
	if root != "" {
		if hookInput.SessionID != "" {
			taskpipeline.EnsureHookSession(root, hookInput.SessionID, agent)
			// Refresh the last-session pointer so the CLI path (task start,
			// continuity anchors) can attribute forge invocations made inside a
			// host's Bash tool — which carries no identity env on any host except
			// claude-code — back to this session (throttled; see
			// taskpipeline.TouchLastSession).
			//
			// 刷新 last-session 指针，使 CLI 路径（task start、接续锚定）能把
			// 在宿主 Bash 工具里发起的 forge 调用归回本会话——除 claude-code 外
			// 任何宿主的 Bash 工具都不带身份 env（已节流，见
			// taskpipeline.TouchLastSession）。
			taskpipeline.TouchLastSession(root, hookInput.SessionID, agent, hookInput.HookEventName)
		} else if agent != "" {
			taskpipeline.StampSessionAgent(root, hookInput.SessionID, agent)
		}
	}

	// Attribution ledger + Stop coverage metric (multi-task-concurrency §6, L3). Placed
	// BEFORE the early-returning in-process hooks: skill-trigger fires on
	// PostToolUse(Write|Edit|Bash) and returns before step 2's field extraction, so this
	// seam must be self-sufficient (RecordHookEvent does its own tool_input parse + patch
	// synthesis). Recording is silent-failure by contract and skips no-identity sessions
	// (empty sid = degraded mode). The Stop metric is throttled per workspace and is a
	// project-level observation (infrastructure health, not task verification).
	//
	// 归属台账 + Stop 覆盖率度量（multi-task-concurrency §6，L3）。放在早退的 Go 内
	// 特例 hook 之前：skill-trigger 挂在 PostToolUse(Write|Edit|Bash) 上且在步骤 2 的
	// 字段抽取前就返回，本挂点必须自给自足（RecordHookEvent 自行解析 tool_input 并
	// 合成 patch 路径）。记账按契约静默失败，空 sid（无身份宿主）跳过——降级模式。
	// Stop 度量按 workspace 节流，是项目级观察条目（基建健康度，非任务验证）。
	if root != "" {
		attribution.RecordHookEvent(root, hookInput.HookEventName, hookInput.SessionID, hookInput.ToolName, hookInput.ToolInput)
		worktree.Touch(root) // L1 绑定心跳（仅展示用；解析从不对它设门）
		if hookInput.HookEventName == "Stop" {
			attribution.RecordStopMetric(root, "")
		}
	}

	// skill-trigger 特例：Go 内直接判定 + 渲染（不经 bash embed）。
	// 原因：skill-trigger 需 HookInput 的 Event/Prompt/Tool/command/exit_code 实时字段（来自 stdin），
	// 而 thin-wrapper bash（exec forge X）拿不到 runHook 已消费的 stdin——task-resume/resume-reinject
	// 等 thin-wrapper 不依赖 stdin（用 forge data 渲染）故未暴露此问题。在 Go 内处理复用 runHook
	// 已 normalize 的 hookInput 与 agent stdin normalize，最干净。
	//
	// skill-trigger special-case: evaluate + render in Go (no bash embed). skill-trigger needs
	// live HookInput fields from stdin, which the thin-wrapper bash cannot reach (runHook consumed
	// stdin). Handling in Go reuses the already-normalized hookInput + agent stdin normalize.
	if name == "skill-trigger" {
		// skill-trigger 特例路径经接缝回调（2026-09 普查 A2-2）：判定+渲染核心住
		// cli 的 skill_trigger.go（挂在 cliskills.Root 下、依赖 cli 会话上下文），
		// 由 cli 注册器注入 SkillTriggerHookFn。
		if SkillTriggerHookFn == nil {
			return nil
		}
		return SkillTriggerHookFn(hookInput, root, cmd.Root().Version, agent)
	}
	// failure-track / subagent-track / test-nudge：与 skill-trigger 同类的 Go 内特例
	// （见 isInProcessHook）。都复用 runHook 已 normalize 的 hookInput 与已解析的
	// root/agent；全部 advisory（永不阻断——PostToolUseFailure/SubagentStop 上的
	// 阻断收益为负：失败循环需要的是提示不是拦截，子 agent 空交付阻断假阳性过高）。
	//
	// failure-track / subagent-track / test-nudge: Go-internal special cases of the
	// same class as skill-trigger (see isInProcessHook). All reuse runHook's
	// already-normalized hookInput and resolved root/agent; all advisory (never
	// block — blocking on PostToolUseFailure/SubagentStop has negative value: a
	// failure loop needs a nudge, not an interception, and empty-delivery
	// subagent blocks have too many false positives).
	if name == "failure-track" {
		return runFailureTrackHook(hookInput, root, cmd.Root().Version, agent)
	}
	if name == "subagent-track" {
		return runSubagentTrackHook(hookInput, root, cmd.Root().Version, agent)
	}
	if name == "test-nudge" {
		return runTestNudgeHook(hookInput, root, cmd.Root().Version, agent)
	}
	// conventions-context / conventions-write：conventions-profile 层 2 的注入
	// hook（hook_conventions.go），与上面同类——advisory、永不阻断、需要 stdin 的
	// 实时字段（event / file_path）。conventions-context 挂 SessionStart+PostCompact，
	// conventions-write 挂 PreToolUse Write|Edit（见 ForgeHookSpec）。
	//
	// conventions-context / conventions-write: the conventions-profile layer-2
	// injection hooks (hook_conventions.go), same class as the above — advisory,
	// never block, need live stdin fields (event / file_path). conventions-context
	// rides SessionStart+PostCompact; conventions-write rides PreToolUse Write|Edit
	// (see ForgeHookSpec).
	if name == "conventions-context" {
		return runConventionsContextHook(hookInput, root, cmd.Root().Version, agent)
	}
	if name == "conventions-write" {
		return runConventionsWriteHook(hookInput, root, cmd.Root().Version, agent)
	}

	// 1c. Patch-tool exemption for read-before-edit (codex reports file edits as
	// tool_name "apply_patch" — hostcap PatchToolName column — single tool, patch
	// text in tool_input.command), and the
	// per-session reads log only records ToolName=="Read" — codex's file reads go through
	// its own read tools, never named "Read", so the log is structurally empty on codex
	// and read-before-edit would false-block EVERY apply_patch. The patch itself carries
	// the old/new context. Silent allow (exit 0, no stdout) — see the non-forge branch
	// for why allow never emits an approve JSON. The check keys on the TOOL name (not
	// the agent) because the hook stdin is Claude-shape and agent may be empty;
	// hostcap.IsPatchTool scans the registry's PatchToolName column.
	//
	// 1c. patch 工具对 read-before-edit 的豁免（codex 的文件编辑以 tool_name
	// "apply_patch" 上报——hostcap PatchToolName 列——单工具，patch 文本在
	// tool_input.command），而 per-session
	// reads log 只记录 ToolName=="Read"——codex 的文件读走它自己的 read 工具，从不叫
	// "Read"，故该 log 在 codex 上结构性为空，read-before-edit 会假阻断每一次
	// apply_patch。patch 本身携带 old/new 上下文。静默放行（exit 0、无 stdout）——
	// 为何 allow 不发 approve JSON 见非 forge 分支注释。检查按**工具名**（而非
	// agent）触发，因为 hook stdin 是 Claude 形、agent 可能为空；
	// hostcap.IsPatchTool 扫描注册表的 PatchToolName 列。
	if name == "read-before-edit" && hostcap.IsPatchTool(hookInput.ToolName) {
		return nil
	}

	// 2. Extract tool_input fields on the Go side (reliable JSON parsing).
	//
	// 2. 在 Go 侧抽取 tool_input 字段（可靠的 JSON 解析）。
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: tool_input parse failed: %v\n", err)
		}
	}

	// 2a. Patch-tool file_path synthesis. codex's apply_patch tool_input (hostcap
	// PatchToolName column) carries
	// ONLY {command: <patch text>} — no file_path — so without this synthesis every
	// path-based gate (task-guard's .forge/* self-protection, freeze-guard) sees an
	// empty FORGE_FILE_PATH on codex file edits and fails open. Extract the FIRST
	// *** Add/Update/Delete File: header's path; multi-file patches get the first
	// target only (documented limitation — the common case is single-file).
	//
	// 2a. patch 工具的 file_path 合成。codex 的 apply_patch tool_input（hostcap
	// PatchToolName 列）只带
	// {command: <patch 文本>}——没有 file_path——不合成的话每个基于路径的门禁
	// （task-guard 的 .forge/* 自保护、freeze-guard）在 codex 文件编辑上都看到空的
	// FORGE_FILE_PATH 并 fail open。取第一个 *** Add/Update/Delete File: 头的路径；
	// 多文件 patch 只取第一个目标（已文档化的限制——常见情形是单文件）。
	if hostcap.IsPatchTool(hookInput.ToolName) && fields.FilePath == "" {
		fields.FilePath = applyPatchFilePath(fields.Command)
	}

	// 2b. Detect the active task as context for the task-guard hook.
	// Scope the lookup by the Claude Code session id from stdin so concurrent sessions each
	// resolve their own active task (rather than racing on whichever wrote the global file last).
	//
	// 2b. 检测 active task，作为 task-guard hook 的上下文。
	// 按来自 stdin 的 Claude Code session id 限定查找范围，使并发 session 各自
	// 解析到自己的 active task（而非看哪个最后写入全局文件）。
	var activeTaskRef string
	var activeTaskGate string
	// Scheme 5: the per-task work-activity override lives in state.json, which the bash
	// PreToolUse hook cannot read. Surface it to the upper layer here so read-before-edit
	// (scheme 2 shift-left) respects the per-task disable just like the work-activity gate —
	// an escape must work end-to-end or it is not an escape (fake hard gate backfire: the gate passes
	// but PreToolUse still rejects the edit).
	//
	// 方案5：per-task 的 work-activity override 存在 state.json 中，bash
	// PreToolUse hook 读不到。这里把它显式 surface 给上层，使 read-before-edit
	// （方案2 shift-left）和 work-activity gate 一样尊重 per-task 的 disable——
	// escape 必须端到端生效，否则算不上 escape（fake hard gate 反噬：gate 放行
	// 但 PreToolUse 仍拒绝 edit）。
	var workActivityOverride string
	if active, err := taskpipeline.ActiveTaskState(root, util.SanitizeSessionID(hookInput.SessionID)); err == nil && active != nil {
		activeTaskRef = active.TaskRef
		activeTaskGate = active.CurrentGate
		if active.Overrides.WorkActivity == "disable" {
			workActivityOverride = "disable"
		}
	}

	// 3. Write the embedded script to a temp file.
	//
	// 3. 把 embedded script 写入临时文件。
	//
	// Infra-exit unification (fix/cleanup-batch, 2026-08-29): these steps and
	// step 4 below are the same infrastructure-failure class as the bash-spawn
	// failure in step 5 (script could never run → fail-open). They previously
	// returned a bare error (Execute printed it and exited 1), which fail-opens
	// on every host protocol but with NO visibility on the hosts' context
	// channels. They now route through emitInfraAllow like step 5 — the warning
	// reaches the model on each host's own channel (kimi queues for the
	// UserPromptSubmit drain instead of a stdout print that would read as DENY).
	//
	// infra 出口统一（fix/cleanup-batch，2026-08-29）：本步与下方 step 4 与
	// step 5 的 bash 起不来同属基础设施失败类（脚本永远跑不起来 → fail-open）。
	// 此前它们返回裸 error（Execute 打印后 exit 1），在各宿主协议上同样 fail-open
	// 但在宿主上下文通道上【零】可见。现与 step 5 一致改走 emitInfraAllow——
	// 警告经各宿主自己的通道到达模型（kimi 入队等 UserPromptSubmit 攒发，而非
	// 会被读成 DENY 的 stdout 直印）。
	tmpFile, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		return emitInfraAllow(agent, hookInput.HookEventName, name, root, hookInput.SessionID,
			fmt.Sprintf("[forge] hook %s 基础设施失败（create temp file: %v），fail-open 放行", name, err))
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return emitInfraAllow(agent, hookInput.HookEventName, name, root, hookInput.SessionID,
			fmt.Sprintf("[forge] hook %s 基础设施失败（write script: %v），fail-open 放行", name, err))
	}
	tmpFile.Close()
	// No chmod needed — bash reads the file as an argument and does not execute it directly.
	//
	// 无需 chmod——bash 把文件作为参数读入，并不直接执行它。

	// 4. Run the script with the extracted fields as env vars.
	//
	// 4. 用抽出的字段作为 env var 执行该 script。
	// Same infra-failure class as step 3 — see the unification comment above.
	//
	// 与 step 3 同属 infra 失败类——见上方统一注释。
	bash, err := findBash()
	if err != nil {
		return emitInfraAllow(agent, hookInput.HookEventName, name, root, hookInput.SessionID,
			fmt.Sprintf("[forge] hook %s 基础设施失败（bash not found: %v），fail-open 放行", name, err))
	}

	// Pass the script path with forward slashes: safe for Git Bash/MSYS2/Cygwin, and
	// immune to any backslash-escape reparsing in the spawn chain.
	//
	// 脚本路径转正斜杠传递：Git Bash/MSYS2/Cygwin 都安全，且免疫 spawn 链上任何
	// 反斜杠转义重解析。
	shCmd := exec.Command(bash, filepath.ToSlash(tmpPath))
	shCmd.Dir = root
	cwd, _ := os.Getwd() // 真实 cwd，给 init-suggest global hook 用（FORGE_CWD / FORGE_CWD_TAG）
	// file-sentinel 自伤豁免用：项目 DataDir 的绝对路径，由 Go 侧解析后传入——
	// bash 侧自行拼接必分叉（bash 的 ${TMPDIR} 是 MSYS 路径、Go 的 os.TempDir()
	// 是 Windows 路径），与 FORGE_READS_FILE 同模式。global hook（root==""）时为
	// 空串，项目级 hook（file-sentinel 等）root 恒非空不受影响。
	//
	// Absolute project DataDir for the file-sentinel self-deploy exemption, resolved
	// on the Go side — bash-side reconstruction would diverge (MSYS ${TMPDIR} vs
	// Windows os.TempDir()), same pattern as FORGE_READS_FILE. Empty for global
	// hooks (root==""); project-scoped hooks always have a root.
	dataDirEnv := ""
	if root != "" {
		dataDirEnv = forgedata.DataDirFor(root)
	}
	shCmd.Env = append(os.Environ(),
		"FORGE_FILE_PATH="+sanitizeForShell(toRelPath(root, fields.FilePath)),
		"FORGE_CONTENT="+sanitizeForShell(fields.Content),
		"FORGE_COMMAND="+sanitizeForShell(fields.Command),
		// Edit 的 old_string/new_string（assertion-check per-edit 模式用，
		// 见 toolInputFields 注释）。
		//
		// Edit's old_string/new_string (consumed by assertion-check's per-edit
		// mode — see the toolInputFields comment).
		"FORGE_OLD_STRING="+sanitizeForShell(fields.OldString),
		"FORGE_NEW_STRING="+sanitizeForShell(fields.NewString),
		"FORGE_TOOL_NAME="+sanitizeForShell(hookInput.ToolName),
		"FORGE_TASK_REF="+sanitizeForShell(activeTaskRef),
		"FORGE_TASK_GATE="+sanitizeForShell(activeTaskGate),
		"FORGE_SESSION_ID="+sanitizeForShell(hookInput.SessionID),
		// The resolved host agent (from --agent / FORGE_HOOK_AGENT; "" for claude-code-shape
		// hosts). Thin wrappers (`exec forge task resume --hook`) inherit it so the spawned
		// forge process can attribute session anchors to the right tool (detectOriginTool).
		//
		// 解析出的 host agent（来自 --agent / FORGE_HOOK_AGENT；claude-code-shape host 为 ""）。
		// thin wrapper（`exec forge task resume --hook`）继承它，使派生的 forge 进程能把
		// session 锚定归属到正确的工具（detectOriginTool）。
		"FORGE_AGENT="+sanitizeForShell(agent),
		// Scheme 2 shift-left: absolute path of this session's reads log. The Go dispatcher
		// (tool-track) appends each Read's repo-relative path here; the PreToolUse
		// read-before-edit hook greps it to intercept Edit-without-Read. Passed as an absolute path
		// (not reconstructed in bash) so that Windows (os.TempDir = Windows AppData temp)
		// and Unix resolve the temp dir consistently — avoiding $TMPDIR vs /tmp divergence that would make the hook
		// silently never match.
		//
		// 方案2 shift-left：本 session reads log 的绝对路径。Go 分发器
		// （tool-track）把每次 Read 的 repo-relative 路径追加到这里；PreToolUse
		// read-before-edit hook grep 它来拦截 Edit-without-Read。以绝对路径传递
		// （不在 bash 里重建），让 Windows（os.TempDir = Windows AppData temp）
		// 与 Unix 在 temp dir 上一致解析——避免 $TMPDIR 与 /tmp 不一致导致 hook
		// 静默永远命中不到。
		"FORGE_READS_FILE="+readsFilePath(root, hookInput.SessionID),
		// Stable project tag (fnv hash of the canonical project root) so the hook can
		// bucket per-project state by it, not relying on $PWD/cksum — the latter is unstable across path case, drive letters,
		// and BSD/GNU cksum formats. For global hooks (init-suggest/skill-scan) root is empty, so this hashes the real cwd —
		// init-suggest must never depend on it (non-forge projects have no forge root); use FORGE_CWD_TAG below instead.
		//
		// 稳定的 project tag（canonical project root 的 fnv 哈希），让 hook
		// 据此为 per-project 状态分桶，不依赖 $PWD/cksum——后者在路径大小写、盘符、
		// BSD/GNU cksum 格式之间都不稳定。对 global hook（init-suggest/
		// skill-scan）来说 root="" ，于是这里哈希的是真实 cwd——init-suggest
		// 绝不能依赖它（非 forge project 没有 forge root）；改用下面的 FORGE_CWD_TAG。
		"FORGE_PROJECT_TAG="+ProjectTagFor(root),
		"FORGE_DATA_DIR="+sanitizeForShell(dataDirEnv),
		// The cwd and its git-root-keyed tag, for init-suggest (a global hook) to use:
		// the hook finds the git root from FORGE_CWD, then writes a per-project marker keyed by FORGE_CWD_TAG.
		// Keyed by git root (via SuggestTagFor), not cwd, so no matter which subdir runs
		// `forge suggest decline`, the tag written matches what the hook reads at the project root —
		// guarding the decline contract.
		//
		// cwd 及其按 git root 作 key 的 tag，给 init-suggest（global hook）用：
		// hook 从 FORGE_CWD 找 git root，再按 FORGE_CWD_TAG 写 per-project marker。
		// 以 git root 作 key（经 SuggestTagFor），不是 cwd，所以从任何 subdir
		// 跑 `forge suggest decline` 写出的 tag 与 hook 在 project root 读到的
		// 一致——守护 decline 契约。
		"FORGE_CWD="+cwd,
		"FORGE_CWD_TAG="+SuggestTagFor(cwd),
	)
	// Scheme 5: expose the active task's per-task work-activity override as
	// the FORGE_WORK_ACTIVITY env for the hook to check. Forced only when disable —
	// when the override is empty, leave the existing os.Environ() value untouched to preserve a user's global FORGE_WORK_ACTIVITY,
	// and to avoid falsely reporting the escape hatch as open on non-escaping tasks.
	//
	// 方案5：把 active task 的 per-task work-activity override 以
	// FORGE_WORK_ACTIVITY env 暴露给 hook 检查。仅在 disable 时强制写入——
	// override 为空时不碰 os.Environ() 的现有值，保留用户全局 FORGE_WORK_ACTIVITY，
	// 也避免给未 escape 的 task 误报逃生舱已开。
	if workActivityOverride == "disable" {
		shCmd.Env = append(shCmd.Env, "FORGE_WORK_ACTIVITY=disable")
	}
	// task-guard promotion pre-configuration: on hosts whose task-guard advisory
	// promotes to a block (hostcap PromoteAdvisory — dsh 2026-08-22 & zcode 2026-08-30, both incident-proven; kimi's rules were
	// retired 2026-08-24 in favor of the advisory queue, see
	// hook_kimi_advisory.go), the script must
	// drop its once-per-session NOWARN de-noise and emit the directive block
	// reason on EVERY no-task source edit — under promotion, the NOWARN marker is
	// a bypass (the model blind-retries the identical edit and passes silently
	// because the marker is already set). taskGuardPromotionActive shares the
	// escape-hatch check with promoteAdvisory so this env can never claim
	// promotion while the hatch is open (that would resurrect the 139-WARN spam
	// with no enforcement behind it).
	//
	// task-guard 提升预配置：在把 task-guard advisory 提升为阻断的宿主上
	// （hostcap PromoteAdvisory——dsh 2026-08-22 与 zcode 2026-08-30 双事故实证入列；kimi 的规则已于 2026-08-24 退役，改为
	// advisory 队列，见 hook_kimi_advisory.go），脚本必须放弃每会话一次的 NOWARN
	// 去噪，在**每次**无任务源码编辑上输出指令式 block reason——提升语义下
	// NOWARN 标记就是旁路（模型盲重试同一编辑，因标记已置而静默放行）。
	// taskGuardPromotionActive 与 promoteAdvisory 共享逃生舱检查，使本 env 绝不
	// 可能在逃生舱开着时声称已提升（否则 139 次 WARN 刷屏复活且背后无执法）。
	if name == "task-guard" {
		if taskGuardPromotionActive(agent) {
			shCmd.Env = append(shCmd.Env, "FORGE_TASKGUARD_PROMOTED=1")
		} else {
			// Scrub any inherited FORGE_TASKGUARD_PROMOTED (os/exec dedups env keys,
			// keeping the LAST occurrence, so the empty value wins over os.Environ).
			// This is a Go→script internal channel, NOT operator config — unlike
			// FORGE_WORK_ACTIVITY above, inheriting it from the operator shell is a
			// bug: on a non-promoted host a stray value makes the script emit the
			// DENIED directive text while the edit is actually allowed (and that
			// text rides additionalContext to the model) — the exact
			// claim-without-enforcement shape this change exists to remove.
			//
			// 清掉环境里可能继承的 FORGE_TASKGUARD_PROMOTED（os/exec 对 env 键去重
			// 保留**最后**出现，空值压过 os.Environ 的继承值）。这是 Go→脚本的内部
			// 通道，**不是**运维配置——与上面的 FORGE_WORK_ACTIVITY 不同：在非提升
			// 宿主上残留值会让脚本输出 DENIED 指令文案而编辑实际放行（且该文案经
			// additionalContext 注入模型）——正是本次变更要消灭的「有文案无执法」
			// 形状。
			shCmd.Env = append(shCmd.Env, "FORGE_TASKGUARD_PROMOTED=")
		}
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	shCmd.Stdout = &stdoutBuf
	shCmd.Stderr = &stderrBuf

	exitErr := shCmd.Run()

	stdout := strings.TrimSpace(stdoutBuf.String())
	stderr := strings.TrimSpace(stderrBuf.String())

	// Infrastructure failure is not a gate verdict: bash itself could not run the
	// script (spawn error, or exit 126/127 = script file unreadable/not found — e.g.
	// a WSL bash that cannot see the Windows temp path). Blocking here would hard-stop
	// every turn in kimi (exit 2) or every edit in Claude for an environment problem,
	// not a quality failure. Fail open with a visible warning instead.
	//
	// 基础设施失败不是门禁结论：bash 本身没能跑起脚本（spawn 错误，或 exit
	// 126/127 = 脚本文件不可读/不存在——例如 WSL bash 看不到 Windows 临时路径）。
	// 此时阻断会因环境问题硬停 kimi 的每一轮（exit 2）或 Claude 的每次编辑，而这
	// 并非质量失败。改为 fail-open 放行并给出可见警告。
	if isHookInfraFailure(exitErr) {
		warning := fmt.Sprintf("[forge] hook %s 基础设施失败（%v: %s），fail-open 放行", name, exitErr, firstNonEmpty(stderr, "no output"))
		return emitInfraAllow(agent, hookInput.HookEventName, name, root, hookInput.SessionID, warning)
	}

	passed := exitErr == nil
	// scriptPassed 钉住脚本自身结论（5b 的 promoteAdvisory 可能把 passed 翻转为
	// false——那只改变 emitted 结论）。step 6 的 checklog 记录以脚本结论为准：
	// advisory 被提升成阻断时不得记成 blocked/fail（见 step 6 注释）。
	scriptPassed := passed

	// 5. Parse the script output into the per-host verdict. The script outputs plain
	// text: PASS [detail] or FAIL [reason]; the protocol shaping (JSON shape, exit
	// code, which events may carry context) is deferred to emitAgentOutput (step 7),
	// which knows the host.
	//
	// 5. 把 script 输出解析成 per-host 结论。Script 输出纯文本：PASS [detail] 或
	// FAIL [reason]；协议塑形（JSON 形态、退出码、哪些事件可带上下文）推迟到知
	// 道宿主的 EmitAgentOutput（step 7）。
	eventName := hookInput.HookEventName
	var detail string
	if passed {
		detail = extractDetail(stdout, "PASS")
		// kimi-code installs the forge plugin by locking a repo tag and has no plugin
		// auto-update (CLI has no plugin management subcommands), so a kimi install drifts
		// behind the forge binary over time. Detect the drift here and prepend a remediation
		// advisory to resume-reinject's stdout — UserPromptSubmit, the ONE stdout channel
		// kimi 0.35.0 delivers to the model (delivered on the next prompt; see
		// internal/agentbridge/kimi-hook-routing.md). This is a MOVE off init-suggest
		// (SessionStart), whose ride was triple-invisible in production (2026-08-15 audit,
		// E:\AgentOffice): kimi drops SessionStart stdout, the checklog noise gate drops
		// init-suggest PASS, and nothing reached model/user/logs. It must stay a MOVE, not
		// a duplicate: SessionStart precedes the first UserPromptSubmit in every session,
		// so an inert init-suggest append would consume prependKimiStaleAdvisory's
		// once-daily marker before the visible channel ever fires. PREPEND, not append
		// (code-review F2): emitAgentOutput truncates detail's TAIL at 9500 runes — a
		// tail-appended advisory would be cut off after the marker was consumed and the
		// checklog entry recorded. When the advisory does fire, also record a
		// kimi-plugin-stale warn entry in the checklog — the third invisibility layer (log
		// visibility) closed; the noise gate would otherwise drop this hook's PASS and
		// logDetail is computed from the script's raw stdout, which never carries the
		// prepended advisory.
		//
		// kimi-code 装插件靠锁仓库 tag，且无 plugin 自动更新（CLI 无任何 plugin 管理
		// 子命令），故 kimi 安装会随时间落后于 forge 二进制。在此检测漂移，把修复
		// advisory 前置到 resume-reinject 的 stdout——UserPromptSubmit，kimi 0.35.0 唯一
		// 把 stdout 送达模型的通道（下一 prompt 送达；见 internal/agentbridge/
		// kimi-hook-routing.md）。这是从 init-suggest（SessionStart）**迁移**而非复制：
		// 后者在生产三重不可见（2026-08-15 E:\AgentOffice 审计实测）——kimi 丢
		// SessionStart stdout、checklog noise gate 丢 init-suggest PASS、模型/用户/日志
		// 三层全无信号。且必须只保留 UserPromptSubmit 一处：每个 session 里
		// SessionStart 先于首个 UserPromptSubmit，若 init-suggest 处仍追加，那个不可见
		// 的追加会先消耗掉 prependKimiStaleAdvisory 的按日 marker，可见通道反而永不触发。
		// 前置而非追加（code-review F2）：emitAgentOutput 在 9500 rune 处截 detail 尾部
		// ——尾接的 advisory 会在 marker 已消耗、checklog 条目已记录之后被截掉。
		// advisory 真触发时，同时往 checklog 记一条 kimi-plugin-stale warn——补上第三层
		// 不可见（日志可见性）；否则 noise gate 会丢掉本 hook 的 PASS，且 logDetail 取自
		// 脚本原始 stdout，本就不含这里前置的 advisory。
		if kimiStaleRidesHook(agent, name) {
			if prepended := prependKimiStaleAdvisory(detail, cmd.Root().Version); prepended != detail {
				detail = prepended
				if err := checklog.Record(root, &checklog.Entry{
					Check:     checklog.CheckKimiPluginStale,
					Passed:    true, // escape-hatch pattern: the warn rides Level, Passed stays neutral
					Checked:   true,
					Level:     checklog.LevelWarn,
					TaskRef:   activeTaskRef,
					SessionID: util.SanitizeSessionID(hookInput.SessionID),
					Detail:    util.TruncateRunes(detail, maxChecklogDetail),
				}); err != nil {
					fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
				}
			}
		}
	} else {
		detail = stdout
		if detail == "" {
			detail = stderr
		}
	}

	// 5b. Host advisory promotion (hostcap PromoteAdvisory column; dsh & zcode (incident-proven) —
	// kimi's rules were retired 2026-08-24: a promoted exit-2 deny whose reason
	// self-described as "allowed" was self-contradictory, and kimi reads ANY
	// PreToolUse stdout as a deny, so kimi advisories now queue per-project and
	// drain on UserPromptSubmit instead — emitAdvisoryRouted in
	// hook_kimi_advisory.go). dsh's channel delivers but the
	// no-task WARN was empirically ignored (2026-08-22). Promote the REAL advisory to a block
	// (passed true→false) here — BEFORE step 7's emitter — so the host's block emitter
	// (exit 2, stderr shown to the model) sees the promoted value. The checklog record
	// (step 6) deliberately does NOT inherit the flip: it records the script's own
	// verdict (Passed=true + LevelAdvisory for a promoted PASS-script) because scoring
	// consumes Passed as a quality verdict — see step 6's comment for the 2026-08
	// mislabeled-records incident. promoteAdvisory consults the hostcap
	// registry rules, which isolate real advisories from each hook's success/clean branch;
	// skill-trigger returned before step 5 and
	// is unaffected. Escape hatches: FORGE_ADVISORY_PROMOTION / FORGE_KIMI_ADVISORY =soft.
	//
	// 5b. 宿主 advisory 提升（hostcap PromoteAdvisory 列；dsh/zcode 双事故实证入列——kimi 的规则已于
	// 2026-08-24 退役：被提升的 exit-2 deny 的 reason 自述「allowed」，自相矛盾，
	// 且 kimi 把 PreToolUse 上**任何** stdout 当 deny，故 kimi 的 advisory 改为按
	// 项目入队、UserPromptSubmit 攒发——hook_kimi_advisory.go 的
	// emitAdvisoryRouted）。dsh
	// 通道送达但无任务 WARN 被实证无视（2026-08-22）。在此把
	// 真 advisory 提升为阻断（passed true→false）——在 step 7 的 emitter 之前——让
	// 宿主阻断 emitter（exit 2，stderr 展示给模型）拿到提升后的值。step 6 的
	// checklog 记录刻意**不**继承该翻转：它记脚本自身结论（被提升的 PASS 脚本记
	// Passed=true + LevelAdvisory），因为 scoring 把 Passed 当质量结论消费——
	// 2026-08 误标记录事件见 step 6 注释。promoteAdvisory 查 hostcap 注册表规则，
	// 规则把真 advisory 与各 hook 的成功/干净分支隔开；skill-trigger 在
	// step 5 之前已返回，不受影响。逃生舱：FORGE_ADVISORY_PROMOTION / FORGE_KIMI_ADVISORY =soft。
	if promoteAdvisory(agent, name, passed, detail) {
		passed = false
	}

	// 6. Record into checklog (noise-gated).
	//
	// 6. 记入 checklog（noise-gated）。
	checkName := checklog.CheckName(name)
	// No `completed` placeholder: assertion-check/auto-compile pass silently (no stderr/stdout)
	// in the common case, and a fake `completed` detail polluted checklog stats (~713 placeholder
	// entries/week, forge-weekly-audit-2026-08-09). Empty detail is honest — the entry still carries
	// Passed/Checked (what scoring's LatestByCheck reads) and TaskRef (forge trace bucketing); only the
	// meaningless Detail text is dropped.
	//
	// 无 `completed` 占位符：assertion-check/auto-compile 静默通过（无 stderr/stdout）是
	// 常态，假的 `completed` detail 污染 checklog 统计（每周 ~713 条占位条目，
	// forge-weekly-audit-2026-08-09）。空 detail 诚实——条目仍带 Passed/Checked（scoring 的
	// LatestByCheck 读这俩）与 TaskRef（forge trace 桶用）；只去掉无意义的 Detail 文本。
	logDetail := firstNonEmpty(stderr, stdout)

	// Reuse the task ref detected earlier for audit traceability.
	//
	// 复用前面检测到的 task ref，便于审计追溯。
	taskRef := activeTaskRef

	// On block (e.g. task-guard) clear tool_name to avoid producing ghost activity records.
	// A blocked Write should not inflate the WorkActivity count.
	//
	// 被拦截时（如 task-guard）清空 tool_name，避免产生 ghost activity 记录。
	// 被拦截的 Write 不应膨胀 WorkActivity 计数。
	recordedToolName := hookInput.ToolName
	if !passed {
		recordedToolName = ""
	}

	// Noise gate (axis A of checklog layered governance): scoring reads only the
	// LATEST entry per check (task.go scoreTask's LatestByCheckForSession), so writing PASS on every
	// tool call is pure audit noise — measured 15946 lines of checklog, 100% PASS, zero FAIL.
	// Only record FAIL (block/warn signal traceability and diagnostics that are actually needed) plus the
	// PASS of scoring-dependent checks (assertion-check/auto-compile) — their
	// LatestByCheck feeds CompilePassed/AssertionPassed. Non-scoring PASS is dropped,
	// cutting about 86% of the checklog volume. See shouldRecordCheck.
	//
	// Noise gate（checklog 分层治理的 axis A）：scoring 只读每个 check 的
	// LATEST 条目（task.go scoreTask 的 LatestByCheckForSession），所以每次
	// tool call 都写 PASS 纯属审计噪声——实测 15946 行 checklog 中 100% 是
	// PASS、零 FAIL。仅记录 FAIL（block/warn 信号追溯和诊断真正需要的）以及
	// scoring 依赖的 check（assertion-check/auto-compile）的 PASS——它们的
	// LatestByCheck 会喂给 CompilePassed/AssertionPassed。Non-scoring PASS 丢弃，
	// 削减约 86% 的 checklog 体积。参见 shouldRecordCheck。
	// Noise gate (axis A of checklog layered governance): scoring reads only the
	// LATEST entry per check (task.go scoreTask's LatestByCheckForSession), so writing PASS on every
	// tool call is pure audit noise — measured 15946 lines of checklog, 100% PASS, zero FAIL.
	// Only record FAIL (block/warn signal traceability and diagnostics that are actually needed) plus the
	// PASS of scoring-dependent checks (assertion-check/auto-compile) — their
	// LatestByCheck feeds CompilePassed/AssertionPassed. Non-scoring PASS is dropped,
	// cutting about 86% of the checklog volume. See shouldRecordCheck.
	//
	// Axis A refinement (weekly-hardening): a scoring check's PASS is recorded only
	// on STATE CHANGE — if the latest entry for the check is already a PASS, a
	// repeat PASS carries zero information (scoring's LatestByCheck still resolves
	// to that earlier PASS, so the semantics do not regress) but was 54% of the
	// checklog volume (auto-compile/assertion-check fire on every Write/Edit).
	// FAIL is always recorded (a FAIL→PASS transition is a state change and is
	// recorded; PASS→PASS is skipped). See scoringPassUnchanged.
	//
	// Noise gate（checklog 分层治理的 axis A）：scoring 只读每个 check 的
	// LATEST 条目（task.go scoreTask 的 LatestByCheckForSession），所以每次
	// tool call 都写 PASS 纯属审计噪声——实测 15946 行 checklog 中 100% 是
	// PASS、零 FAIL。仅记录 FAIL（block/warn 信号追溯和诊断真正需要的）以及
	// scoring 依赖的 check（assertion-check/auto-compile）的 PASS——它们的
	// LatestByCheck 会喂给 CompilePassed/AssertionPassed。Non-scoring PASS 丢弃，
	// 削减约 86% 的 checklog 体积。参见 shouldRecordCheck。
	//
	// axis A 细化（周复盘加固）：scoring check 的 PASS 只在状态变化时记录——
	// 该 check 最新条目已是 PASS 时，重复 PASS 零信息量（scoring 的
	// LatestByCheck 仍解析到那条更早的 PASS，语义不回归），却占 checklog
	// 体积 54%（auto-compile/assertion-check 每次 Write/Edit 都触发）。FAIL
	// 保持全记（FAIL→PASS 是状态变化会记录；PASS→PASS 跳过）。参见
	// scoringPassUnchanged。
	if shouldRecordCheck(checkName, passed) &&
		!(passed && scoringPassUnchanged(root, util.SanitizeSessionID(hookInput.SessionID), checkName)) {
		// Passed/Level 记录脚本自身结论，不被 5b promoteAdvisory 的翻转污染：提升
		// 只改变 emitted 结论（step 7 发出阻断），checklog 记的是 hook 检查本身的
		// verdict——脚本 PASS（exit 0）被提升为阻断时记 Passed=true + Level=advisory；
		// 只有脚本 FAIL（exit≠0）才记 Passed=false + Level=blocked（hook 的 FAIL 是
		// 真 block：decision:block 拦下工具调用，derive 只能从 Detail 前缀区分 gate 的
		// BLOCKED:/ADVISORY:，对 hook 输出会退化成 fail，故 Level 显式设置）。
		// scoring 的 LatestByCheck 直接消费 Passed（taskpipeline/scoring.go）——
		// advisory 记成 blocked/fail 会把 AssertionPassed 等维度打翻（2026-08 kimi
		// P0 提升期 7 条 assertion-check 记录：detail 自述 "PASS …Advisory…forge
		// 不再阻塞"，却 level=blocked/passed=false）。
		recordedPassed := passed
		level := checklog.LevelPass
		if !passed {
			level = checklog.LevelBlocked
			if scriptPassed {
				recordedPassed = true
				level = checklog.LevelAdvisory
			}
		}
		// 宿主把同一工具事件双发时的阻断记录去重（2026-08-24 实证：kimi
		// PreToolUse 对同一 Edit 在 98ms 内两次调用 read-before-edit，checklog seq
		// 连号双记；两周日志 6 组同 (session,file) 在 0.5~1.9s 内连发）。去重只
		// 抑制重复审计行——阻断发射（step 7）与脚本执行都不受影响。打戳在 Record
		// 成功之后（Record 失败不留戳，窗口内重试照常记录，不丢审计行）。
		if !passed && duplicateBlockRecord(root, hookInput.SessionID, string(checkName), logDetail) {
			// 窗口内同指纹重复：跳过记录
		} else if err := checklog.Record(root, &checklog.Entry{
			Check:     checkName,
			Passed:    recordedPassed,
			Checked:   true,
			Level:     level,
			ToolName:  recordedToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
			Detail:    util.TruncateRunes(logDetail, maxChecklogDetail),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
		} else if !passed {
			stampBlockRecord(root, hookInput.SessionID, string(checkName), logDetail)
		}
	}

	// 6b. Record tool usage for activity-ratio detection. auto-compile records Write/Edit;
	// tool-track records Read|Skill|Agent|Grep|Glob|Bash (matcher lives in ForgeHookSpec), giving the
	// read-before-edit gate (task-verify) Read data — otherwise that gate would always fail on any task
	// with an edit (644b142 deleted the original Read recorder).
	// tool_input population: auto-compile (Edit/Write) records file_path/content; tool-track's Skill/Agent
	// records the skill name/subagent_type (scheme C: lets toollog audits see which quality skill the agent loaded and what kind of
	// subagent it dispatched — root cause of zero quality-skill fires in advisory context is traceable). Bash records the
	// command (truncated) — the 2026-08-22 adherence audit found 27.7k Bash invocations with ZERO toollog rows because the
	// Bash matcher carried no tool-track; the audit (and any future hazard/behavior analysis) needs the command text,
	// same truncated treatment as Skill/Agent. Read records a minimal
	// {"file_path":...} (2026-08-16 review HIGH-1: the funnel join — skillmetrics.BuildTriggerFunnel — matches Read tool_input
	// suffixes to attribute "loaded the skill after the trigger hit"; omitting it made that join structurally dead on production
	// data while unit tests stayed green on hand-marshaled inputs. The lean-toollog tradeoff lost to the observability signal).
	//
	// 6b. 记录 tool usage 用于 activity-ratio 检测。auto-compile 记 Write/Edit；
	// tool-track 记 Read|Skill|Agent|Grep|Glob|Bash（matcher 在 ForgeHookSpec 中），让
	// read-before-edit gate（task-verify）有 Read 数据——否则该 gate 在任何带
	// edit 的 task 上恒失败（644b142 删过原来的 Read recorder）。
	// tool_input 填充：auto-compile（Edit/Write）记 file_path/content；tool-track 的 Skill/Agent
	// 记 skill 名/subagent_type（方案 C：让 toollog 审计能看到 agent 加载了哪个质量 skill、派了
	// 哪类子 agent——advisory 语境下质量 skill 0 触发的根因可追溯）。Bash 记命令（截断）——
	// 2026-08-22 遵循度审计发现 27.7k 次 Bash 调用在 toollog 零行（Bash matcher 没挂
	// tool-track）；审计（及未来的 hazard/行为分析）需要命令文本，与 Skill/Agent 同截断待遇。
	// Read 记最小 {"file_path":...}
	// （2026-08-16 审查 HIGH-1：漏斗 join——skillmetrics.BuildTriggerFunnel——靠 Read tool_input 的
	// 后缀匹配归因「命中后加载了该 skill」；省略它使该 join 在生产数据上结构性死亡，而单测用手工
	// marshal 的输入照样全绿。lean 权衡让位于可观测信号）。
	if name == "auto-compile" || name == "tool-track" {
		call := &toolusage.ToolCall{
			ToolName:  hookInput.ToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
		}
		if name == "auto-compile" || (name == "tool-track" && (hookInput.ToolName == "Skill" || hookInput.ToolName == "Agent" || hookInput.ToolName == "Bash" || hookInput.ToolName == "Grep" || hookInput.ToolName == "Glob")) {
			raw := string(hookInput.ToolInput)
			call.ToolInput = toolusage.TruncateInput(raw)
			call.InputLen = len(raw)
			call.EstTokens = toolusage.EstimateTokens(raw)
		} else if name == "tool-track" && hookInput.ToolName == "Read" && fields.FilePath != "" {
			// Minimal shape: ONLY file_path (not the full tool input) — the funnel join
			// (skillmetrics.BuildTriggerFunnel → readFilePath) suffix-matches this field; every other
			// Read field stays unrecorded. Pinned by TestHookToolTrackRecordsReadFilePath; the two
			// must not silently diverge again (that divergence was review HIGH-1).
			//
			// 最小形状：只记 file_path（非完整 tool input）——漏斗 join
			// （skillmetrics.BuildTriggerFunnel → readFilePath）按本字段做后缀匹配；Read 的其余
			// 字段照旧不记。由 TestHookToolTrackRecordsReadFilePath 钉死；两者不得再静默分叉
			// （该分叉即审查 HIGH-1）。
			minimal, _ := json.Marshal(map[string]string{"file_path": fields.FilePath})
			call.ToolInput = toolusage.TruncateInput(string(minimal))
			raw := string(hookInput.ToolInput)
			call.InputLen = len(raw)
			call.EstTokens = toolusage.EstimateTokens(raw)
		}
		if err := toolusage.Record(root, call); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: toollog record failed: %v\n", err)
		}
		// Scheme 2 shift-left: append this Read's file_path to the per-session reads log,
		// so the PreToolUse read-before-edit hook can intercept Edit-without-Read at Edit time.
		// toollog now also records Read's file_path (funnel join, see 6b above), but this side
		// channel stays: it stores the PROJECT-RELATIVE path (gate matching is project-anchored)
		// and is read at Edit time without parsing toollog JSON. PostToolUse fires after the Read
		// completes, so this round's Read is recorded before the subsequent Edit —
		// the Edit's PreToolUse hook can then see the path.
		//
		// 方案2 shift-left：把本次 Read 的 file_path 追加到 per-session reads log，
		// 让 PreToolUse read-before-edit hook 能在 Edit 时拦截 Edit-without-Read。
		// toollog 现在也记 Read 的 file_path（漏斗 join，见上 6b），但本 side-channel 保留：
		// 它存项目相对路径（gate 匹配锚定项目根），且 Edit 时直接读取、无需解析 toollog
		// JSON。PostToolUse 在 Read 完成之后触发，所以本回合的 Read 会先于随后的 Edit
		// 被记录——Edit 的 PreToolUse hook 就能看到该路径。
		if name == "tool-track" && hookInput.ToolName == "Read" && fields.FilePath != "" {
			rel := toRelPath(root, fields.FilePath)
			if rel != "" && rel != "." {
				appendSessionRead(readsFilePath(root, hookInput.SessionID), rel)
			}
		}
		// reads-log 对称补全（2026-08-24）：Write 落盘后 agent 当然知道文件内容
		// （刚写的），把该路径也计入 reads-log——否则 Write 创建文件后紧接着的 Edit
		// 必被 read-before-edit 拦（4 个 session 复发同一剧本：Write→Edit→FAIL→被迫
		// 补一次纯形式 Read）。Write 覆盖已存在文件同理：盲覆盖本身仍由
		// read-before-edit 在 PreToolUse 拦（那时 reads-log 无此路径），但 Write 落盘
		// 后的后续 Edit 不应再拦。auto-compile 挂在 PostToolUse Write|Edit 上，此处
		// 只收 Write（Edit 要落到这步本就已过 PreToolUse 的读门槛，无需再记）。
		//
		// Symmetric reads-log completion (2026-08-24): after a Write lands the agent
		// plainly knows the file's content (it just authored it), so record the path
		// too — otherwise the Edit right after a file-creating Write is always blocked
		// by read-before-edit (4 sessions replayed the same script: Write→Edit→FAIL→
		// forced ceremonial Read). Same for overwriting an existing file: the blind
		// overwrite itself is still blocked at PreToolUse (the path is not in the log
		// yet), but post-Write edits must not be. auto-compile rides PostToolUse
		// Write|Edit; only Write is recorded here (an Edit reaching this point already
		// passed the PreToolUse read gate, so recording it adds nothing).
		if name == "auto-compile" && hookInput.ToolName == "Write" && fields.FilePath != "" {
			rel := toRelPath(root, fields.FilePath)
			if rel != "" && rel != "." {
				appendSessionRead(readsFilePath(root, hookInput.SessionID), rel)
			}
		}
	}

	// 7. Output the result in the HOST's hook protocol (per-agent dispatch). The old
	// single-shape path printed Claude's JSON for every host and returned a generic
	// error on block — which Execute maps to exit 1, a code only Claude Code (via the
	// stdout decision JSON) treats as blocking; on codex/cursor/windsurf/copilot the
	// same block FAILED OPEN. Every per-agent emitter below returns *HookBlockError on
	// block → exit 2, the one non-zero code that codex (stderr+exit 2), cursor
	// (permission-deny equivalent) and copilot preToolUse (deny, fail-closed) all
	// honor. Host-specific context-injection channels are also keyed here (codex:
	// bare hookSpecificOutput.additionalContext on 4 events; cursor: top-level
	// snake_case additional_context; copilot: top-level camelCase additionalContext;
	// kimi: see internal/agentbridge/kimi-hook-routing.md).
	//
	// 7. 按**宿主**的 hook 协议输出结果（按 agent 分发）。旧的单一形态路径对所有
	// 宿主打 Claude 的 JSON、阻断时返回 generic error——Execute 把它映射成 exit 1，
	// 而只有 Claude Code（经 stdout decision JSON）把 exit 1 当阻断；在
	// codex/cursor/windsurf/copilot 上同一阻断会 FAIL OPEN。下方每个 per-agent
	// emitter 阻断时都返回 *HookBlockError → exit 2——codex（stderr+exit 2）、cursor
	// （等价 permission deny）、copilot preToolUse（deny、fail-closed）共同认可的
	// 唯一非零码。宿主特有的上下文注入通道也在此分流（codex：4 个事件上的裸
	// hookSpecificOutput.additionalContext；cursor：顶层 snake_case
	// additional_context；copilot：顶层 camelCase additionalContext；kimi：见
	// internal/agentbridge/kimi-hook-routing.md）。
	return EmitAdvisoryRouted(agent, eventName, name, root, hookInput.SessionID, passed, detail)
}

// shouldRecordCheck decides whether a hook result is worth writing a checklog entry. It is the
// noise gate for checklog's dual responsibility (scoring input + audit traceability): scoring reads only the
// latest entry per check name (LatestByCheckForSession), so writing PASS on every call is redundant. Any
// FAIL returns true (block/warn signal traceability and diagnostics need it); PASS returns true only for
// scoring-dependent checks.
//
// shouldRecordCheck 判断一次 hook 结果是否值得写 checklog 条目。它是 checklog
// 双重职责（scoring 输入 + 审计追溯）的 noise gate：scoring 只读每个 check name
// 的最新条目（LatestByCheckForSession），所以每次调用都写 PASS 是冗余的。任何
// FAIL 都返回 true（block/warn 信号追溯和诊断需要），PASS 仅在 scoring 依赖的
// check 上返回 true。
func shouldRecordCheck(name checklog.CheckName, passed bool) bool {
	if !passed {
		return true
	}
	return isScoringCheck(name)
}

// scoringPassUnchanged 报告某 scoring check 的 PASS 是否是当前状态的重复：
// 该 check 的最新条目（与 scoring 相同的 session 过滤）已是 PASS。此时跳过
// 重复 PASS——scoring 的 LatestByCheckForSession 仍解析到那条更早的 PASS，
// CompilePassed/AssertionPassed 语义不回归。无先前条目 / 最新是 FAIL
// （FAIL→PASS 状态变化）/ 非 scoring check / 查询失败时返回 false（记录——
// 查询出错不得静默丢审计数据，宁多记）。session 过滤的已知边界（可接受）：
// 上次 PASS 属于其他 session 时，本 session 的首个 PASS 仍会写——每个
// session 每个 check 一条的成本可接受。
func scoringPassUnchanged(root, sessionID string, name checklog.CheckName) bool {
	if !isScoringCheck(name) {
		return false
	}
	// M2（review）：since 取本任务 StartedAt——active 日志跨任务累积后，无界的会话
	// 级查询会让新任务继承上一任务的 PASS、跳过本该重写的证据条目。无活跃任务时
	// 零值回落无界（旧行为）。
	var since time.Time
	if st, err := taskpipeline.ActiveTaskState(root, sessionID); err == nil && st != nil {
		since = st.StartedAt
	}
	latest, err := checklog.LatestByCheckForSessionSince(root, sessionID, since)
	if err != nil {
		return false
	}
	e, ok := latest[name]
	return ok && e.Passed
}

// isScoringCheck 判断某 hook check 的 PASS 是否会被 task scoring 消费。
// scoreTask（task.go）对这些 check 读 LatestByCheckForSession 来填
// CompilePassed/AssertionPassed；它们的 PASS 必须写入 log，scoring 才能看到
// checked & passed。其他 check 的 PASS 被 noise gate 丢弃（只记 FAIL）。注意：
// test-coverage scoring 读的是 taskpipeline 在 task-verify 写的另一条
// test-coverage-gate 条目（不是这条 hook 路径），故 test-coverage-check 在此
// 无需写 PASS。
func isScoringCheck(name checklog.CheckName) bool {
	switch name {
	case checklog.CheckAssertion, checklog.CheckAutoCompile:
		return true
	}
	return false
}

// blockRecordDedupWindow 限定同一事件双发抑制的窗口：窗口内完全相同的阻断记录
// （同 check、session、detail 指纹）是宿主对**同一个**工具事件的重复投递，
// 不是新的阻断。窗口按 2026-08-24 生产证据定：kimi PreToolUse 对单个 Edit
// 98ms 内双发 read-before-edit（checklog seq 连号）；两周日志有 6 组同
// (session,file) 记录间隔 0.5~1.9s。
const blockRecordDedupWindow = 3 * time.Second

// blockRecordMarker resolves the dedupe marker path and the detail fingerprint
// for one (check, session, detail) triple. "" path when root is empty.
//
// blockRecordMarker 解析某 (check, session, detail) 三元组的去重 marker 路径与
// detail 指纹。root 为空时路径返回 ""。
func blockRecordMarker(root, sessionID, checkName, detail string) (path, fp string) {
	if root == "" {
		return "", ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(detail))
	fp = strconv.FormatUint(h.Sum64(), 16)
	path = filepath.Join(forgedata.DataDirFor(root), "markers", "forge-block-dedup-"+readsFileKey(sessionID)+"-"+checkName)
	return path, fp
}

// duplicateBlockRecord 报告完全相同的阻断条目（同 check、session、detail 指纹）
// 是否已在 blockRecordDedupWindow 内被记录过。只查不写——调用方在
// checklog.Record 成功后才经 stampBlockRecord 打戳（2026-08-25 review minor：
// 先打戳的话 Record 失败会留下戳，窗口内的重试随后被抑制——审计行丢失）。
// 尽力而为、宁多记：任何 I/O 错误返回 false——审计行绝不可因 marker 故障被
// 静默丢弃。
func duplicateBlockRecord(root, sessionID, checkName, detail string) bool {
	path, fp := blockRecordMarker(root, sessionID, checkName, detail)
	if path == "" {
		return false
	}
	if data, err := os.ReadFile(path); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) == 2 && parts[1] == fp {
			if ts, perr := strconv.ParseInt(parts[0], 10, 64); perr == nil {
				if since := time.Since(time.Unix(ts, 0)); since >= 0 && since < blockRecordDedupWindow {
					return true
				}
			}
		}
	}
	return false
}

// stampBlockRecord writes the dedupe marker for one blocked record. Called only
// after the corresponding checklog.Record succeeded (see duplicateBlockRecord).
// Best-effort: a stamp failure only means the next identical delivery is
// recorded again — the safe direction.
//
// stampBlockRecord 为一条阻断记录写去重 marker。仅在对应 checklog.Record 成功
// 后调用（见 duplicateBlockRecord）。尽力而为：打戳失败仅意味着下一次相同投递
// 会再记一条——安全方向。
func stampBlockRecord(root, sessionID, checkName, detail string) {
	path, fp := blockRecordMarker(root, sessionID, checkName, detail)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)+" "+fp+"\n"), 0644)
}

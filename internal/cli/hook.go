package cli

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
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/shellexec"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// projectTagFor returns a stable hex tag for a given project root. By hashing the canonical
// (absolute, cleaned) path, the tag stays invariant across path case, drive letter format, and symlinks —
// whereas $PWD cksum also depends on the host's cksum format (GNU vs BSD). The hook reads it via
// the FORGE_PROJECT_TAG env var to isolate state per project.
//
// projectTagFor 为给定 project root 返回稳定的 hex tag。通过对 canonical
// （绝对、clean 后的）路径做哈希，使 tag 在路径大小写、盘符格式、symlink 之间保持
// 不变——而 $PWD cksum 还依赖宿主的 cksum 格式（GNU vs BSD）。hook 通过
// FORGE_PROJECT_TAG env var 读取它来按 project 隔离状态。
func projectTagFor(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(abs)))
	return strconv.FormatUint(h.Sum64(), 16)
}

// suggestTagFor returns the init-suggest marker tag for a directory, keyed by its git root,
// so no matter which subdir the agent runs `forge suggest decline` from, the same project is tagged only
// once. This guards the decline contract: previously keyed by cwd, declining from a subdir would write a different tag
// than the hook read at the project root, silently making decline a no-op. Non-git directories fall back to
// projectTagFor(dir) (still a stable per-dir tag). Shared by the init-suggest hook
// (FORGE_CWD_TAG) and `forge suggest` — both must produce the same tag for the same project.
//
// suggestTagFor 返回某目录的 init-suggest marker tag，按其 git root 作 key，
// 这样无论 agent 从哪个 subdir 执行 `forge suggest decline`，同一 project 只会被
// tag 一次。这守护 decline 契约：此前按 cwd 作 key，从 subdir decline 会写出与
// hook 在 project root 读到的不同的 tag，使 decline 静默 no-op。非 git 目录回退到
// projectTagFor(dir)（仍是稳定的 per-dir tag）。由 init-suggest hook
// （FORGE_CWD_TAG）和 `forge suggest` 共用——两者对同一 project 必须产出相同的
// tag。
func suggestTagFor(dir string) string {
	if root := forgedata.FindGitRoot(dir); root != "" {
		return projectTagFor(root)
	}
	return projectTagFor(dir)
}

// adoptPayloadCwd switches the process working directory to the hook payload's cwd when
// it names an existing directory. Returns true when a chdir happened. See the call site
// in runHook for why (kimi plugin hooks start from the plugin root, not the project).
// Relative paths are rejected: they would resolve against the process cwd (the plugin
// root under kimi) and could chdir to a semantically wrong location — every host sends
// absolute paths. A chdir failure (e.g. a UNC path on Windows) degrades to the process
// cwd with a stderr warning, never silently — silent failure here is exactly the
// "every project hook no-ops" blind spot this fix exists to remove.
//
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
		// Same dir → nothing to do. Compare by inode identity (os.SameFile) first,
		// which is symlink-robust: on macOS os.TempDir/Getwd straddle the
		// /var → /private/var symlink, so the physical path os.Getwd returns
		// (/private/var/...) never string-equals the unresolved form the payload cwd
		// carries (/var/...) — a string-only compare mis-detects "different" and chdirs
		// every call, breaking the no-op contract (v0.27.2 projectroot, same class).
		// The cleaned-path fallback below still covers Windows case folding
		// (E:\Forge vs e:\forge).
		//
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
}

// HookOutput represents the structured JSON that Claude Code expects to receive on stdout.
// Field semantics: see the Claude Code hook documentation.
//
// HookOutput 表示 Claude Code 期望在 stdout 收到的结构化 JSON。
// 字段语义参见 Claude Code hook 文档。
type HookOutput struct {
	// Decision/reason are both omitempty: the allow path emits a bare
	// hookSpecificOutput (no decision) so the host's default flow is untouched —
	// decision:"approve" would bypass Claude's permission system on PreToolUse and
	// marks the hook as failed on codex (see emitAgentOutput).
	//
	// Decision/reason 均 omitempty：allow 路径发裸 hookSpecificOutput（无 decision），
	// 不触碰宿主默认流程——decision:"approve" 在 Claude PreToolUse 会绕过权限系统，
	// 在 codex 会被判 hook failed（见 emitAgentOutput）。
	Decision           string              `json:"decision,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput holds fields that steer Claude Code behavior.
//
// PermissionDecision/PermissionDecisionReason are the CURRENT PreToolUse schema
// (2026-08-22 #4-B migration, additive): official docs put the deny/ask/defer/allow
// verdict at hookSpecificOutput.permissionDecision — the legacy TOP-LEVEL
// decision:"block" on PreToolUse is no longer adopted there (community-verified
// breakage). exit 2 remains a first-class block channel that "routes the same way
// as deny", so existing blocks never depended on the legacy field; filling the
// current field makes the deny explicit under the live schema. Kept omitempty so
// non-PreToolUse events and allow paths serialize exactly as before.
//
// HookSpecificOutput 含控制 Claude Code 行为的字段。
//
// PermissionDecision/PermissionDecisionReason 是 PreToolUse 的现行 schema
// （2026-08-22 #4-B 迁移，additive）：官方文档把 deny/ask/defer/allow 判决放在
// hookSpecificOutput.permissionDecision——PreToolUse 上遗留的顶层
// decision:"block" 已不被采纳（社区实证旧 hook 因此静默失效）。exit 2 仍是
// 一等阻断通道（"routes the same way as deny"），既有阻断从不依赖遗留字段；
// 填现行字段让 deny 在活 schema 下显式化。保持 omitempty：非 PreToolUse 事件
// 与 allow 路径的序列化与之前逐字节一致。
type HookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// maxAdditionalContextLen is the upper bound of Claude Code additionalContext (10,000 chars).
// Here we use 9,500 to leave room for the JSON envelope.
//
// maxAdditionalContextLen 是 Claude Code additionalContext 的上限（10,000 字符）。
// 这里取 9,500 给 JSON envelope 留余量。
const maxAdditionalContextLen = 9500

// maxChecklogDetail is the truncation cap for a checklog entry detail.
//
// maxChecklogDetail 是 checklog entry detail 的截断上限。
const maxChecklogDetail = 500

// maxEnvValueLen is the maximum length of an env var value passed to the bash script,
// used to prevent memory issues.
//
// maxEnvValueLen 是传给 bash 脚本的 env var value 的最大长度，
// 用于防止内存问题。
const maxEnvValueLen = 100000

var hookCmd = &cobra.Command{
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
	RunE:          runHook,
}

// hookAgent specifies the non-Claude-Code host. Each agent's translator sets it via
// the cross-platform `--agent` flag; it selects BOTH the stdin dialect to normalize
// (windsurf/kimi/reasonix/cline differ from the Claude shape) AND the output protocol
// to emit (see emitAgentOutput — codex/cursor/copilot share Claude-shape stdin but
// parse different stdout/exit-code contracts). opencode/codebuddy construct
// Claude-shape stdin in-process and speak the Claude protocol, so they carry no flag.
// FORGE_HOOK_AGENT is the fallback for translators already wired via env (and for
// TS code that sets the env).
//
// hookAgent 指定非 Claude Code 的宿主。由各 agent 的 translator 通过跨平台
// `--agent` flag 设置；它同时选择要 normalize 的 stdin 方言（windsurf/kimi/
// reasonix/cline 与 Claude 形状不同）**和**要输出的协议（见 emitAgentOutput——
// codex/cursor/copilot 的 stdin 与 Claude 同形，但 stdout/退出码契约不同）。
// opencode/codebuddy 在进程内构造 Claude-shape stdin 且说 Claude 协议，
// 故不带 flag。FORGE_HOOK_AGENT 是已通过 env 接线的 translator（以及设 env 的
// TS 代码）的兜底。
var hookAgent string

func init() {
	hookCmd.Flags().StringVar(&hookAgent, "agent", "", "host agent: selects the stdin dialect AND the output protocol (windsurf|kimi|reasonix|codex|cursor|copilot|cline)")
	rootCmd.AddCommand(hookCmd)
}

// resolveHookAgent decides which host agent is speaking. The --agent flag
// (set by translators, cross-platform — Windows cmd cannot parse ENV=val cmd) takes precedence;
// FORGE_HOOK_AGENT is the fallback for callers wired via env (and for TS extensions that set the env before
// spawning forge). The value drives BOTH the stdin normalizer (empty = Claude-Code-shape stdin,
// no normalization needed) and the output emitter (emitAgentOutput) — codex/cursor/copilot share
// Claude-shape stdin but speak different stdout/exit-code protocols, so they carry the flag for
// the output side. An empty string (claude-code, and opencode/codebuddy which construct
// Claude stdin in-process) means Claude on both sides.
//
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

// isGlobalHook decides whether a hook runs independently of a forge project. A global hook scans
// $HOME-level state (skill-scan -> ~/.claude/skills) or cwd-level state
// (init-suggest -> detects whether cwd is a forge candidate; mcp-scan -> scans the project-level
// .mcp.json), all of which are relevant in any project — so when findProjectRoot fails
// (non-forge project) runHook must not silently skip them. init-suggest and mcp-scan
// must run inside non-forge projects: init-suggest is exactly where it discovers forge-candidate
// projects, and mcp-scan catches malicious .mcp.json in user-cloned projects (which may never run forge init).
// Project-scoped hooks (task-guard, file-sentinel, etc.) keep their original
// allow-and-exit behavior.
//
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

// isInProcessHook names the hooks handled entirely in Go inside runHook (no bash
// embed script): they need live HookInput fields from stdin (skill-trigger: event/
// prompt/tool_output; failure-track: error text; subagent-track: agent_id/agent_type;
// test-nudge: file_path), which the thin-wrapper bash can never reach — runHook already
// consumed the stdin. Each has its dispatch point right after the skill-trigger
// special case in runHook.
//
// isInProcessHook 列出完全在 runHook 内用 Go 处理的 hook（无 bash embed 脚本）：
// 它们需要 stdin 的实时 HookInput 字段（skill-trigger：event/prompt/tool_output；
// failure-track：error 文本；subagent-track：agent_id/agent_type；test-nudge：
// file_path），thin-wrapper bash 永远拿不到——runHook 已把 stdin 消费掉。各自的
// 分发点在 runHook 里 skill-trigger 特例之后。
func isInProcessHook(name string) bool {
	return name == "skill-trigger" || name == "failure-track" || name == "subagent-track" || name == "test-nudge"
}

func runHook(cmd *cobra.Command, args []string) error {
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
	root, err := findProjectRoot()
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
		return runSkillTriggerHook(hookInput, root, cmd.Root().Version, agent)
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
	tmpFile, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write script: %w", err)
	}
	tmpFile.Close()
	// No chmod needed — bash reads the file as an argument and does not execute it directly.
	//
	// 无需 chmod——bash 把文件作为参数读入，并不直接执行它。

	// 4. Run the script with the extracted fields as env vars.
	//
	// 4. 用抽出的字段作为 env var 执行该 script。
	bash, err := findBash()
	if err != nil {
		return fmt.Errorf("bash not found in PATH: %w", err)
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
		"FORGE_PROJECT_TAG="+projectTagFor(root),
		"FORGE_DATA_DIR="+sanitizeForShell(dataDirEnv),
		// The cwd and its git-root-keyed tag, for init-suggest (a global hook) to use:
		// the hook finds the git root from FORGE_CWD, then writes a per-project marker keyed by FORGE_CWD_TAG.
		// Keyed by git root (via suggestTagFor), not cwd, so no matter which subdir runs
		// `forge suggest decline`, the tag written matches what the hook reads at the project root —
		// guarding the decline contract.
		//
		// cwd 及其按 git root 作 key 的 tag，给 init-suggest（global hook）用：
		// hook 从 FORGE_CWD 找 git root，再按 FORGE_CWD_TAG 写 per-project marker。
		// 以 git root 作 key（经 suggestTagFor），不是 cwd，所以从任何 subdir
		// 跑 `forge suggest decline` 写出的 tag 与 hook 在 project root 读到的
		// 一致——守护 decline 契约。
		"FORGE_CWD="+cwd,
		"FORGE_CWD_TAG="+suggestTagFor(cwd),
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
	// promotes to a block (hostcap PromoteAdvisory — kimi, dsh), the script must
	// drop its once-per-session NOWARN de-noise and emit the directive block
	// reason on EVERY no-task source edit — under promotion, the NOWARN marker is
	// a bypass (the model blind-retries the identical edit and passes silently
	// because the marker is already set). taskGuardPromotionActive shares the
	// escape-hatch check with promoteAdvisory so this env can never claim
	// promotion while the hatch is open (that would resurrect the 139-WARN spam
	// with no enforcement behind it).
	//
	// task-guard 提升预配置：在把 task-guard advisory 提升为阻断的宿主上
	// （hostcap PromoteAdvisory——kimi、dsh），脚本必须放弃每会话一次的 NOWARN
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
		return emitInfraAllow(agent, hookInput.HookEventName, warning)
	}

	passed := exitErr == nil

	// 5. Parse the script output into the per-host verdict. The script outputs plain
	// text: PASS [detail] or FAIL [reason]; the protocol shaping (JSON shape, exit
	// code, which events may carry context) is deferred to emitAgentOutput (step 7),
	// which knows the host.
	//
	// 5. 把 script 输出解析成 per-host 结论。Script 输出纯文本：PASS [detail] 或
	// FAIL [reason]；协议塑形（JSON 形态、退出码、哪些事件可带上下文）推迟到知
	// 道宿主的 emitAgentOutput（step 7）。
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
					Detail:    truncate(detail, maxChecklogDetail),
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

	// 5b. Host advisory promotion (hostcap PromoteAdvisory column; kimi and dsh today).
	// kimi 0.35.0 drops allow-path (exit 0) stdout from the
	// model context, so forge's core advisories (task-guard/bash-guard/assertion-check)
	// silently evaporate and the agent runs untracked; dsh's channel delivers but the
	// no-task WARN was empirically ignored (2026-08-22). Promote the REAL advisory to a block
	// (passed true→false) here — BEFORE step 6's checklog record — so the promoted value flows
	// into both the audit trail (Passed=false / LevelBlocked) and the host's block emitter
	// (exit 2, stderr shown to the model). Placing this at step 7 instead would desync checklog
	// (recorded as PASS) from the actually-emitted block. promoteAdvisory consults the hostcap
	// registry rules, which isolate real advisories from each hook's success/clean branch;
	// skill-trigger returned before step 5 and
	// is unaffected. Escape hatches: FORGE_ADVISORY_PROMOTION / FORGE_KIMI_ADVISORY =soft.
	//
	// 5b. 宿主 advisory 提升（hostcap PromoteAdvisory 列；目前 kimi 与 dsh）。kimi
	// 0.35.0 丢弃放行路径（exit 0）的 stdout，forge 核心
	// advisory（task-guard/bash-guard/assertion-check）静默蒸发，agent 无任务裸奔；dsh
	// 通道送达但无任务 WARN 被实证无视（2026-08-22）。在此把
	// 真 advisory 提升为阻断（passed true→false）——在 step 6 的 checklog 记录之前——让提升
	// 后的值同时流入审计轨迹（Passed=false / LevelBlocked）与宿主阻断 emitter
	// （exit 2，stderr 展示给模型）。若放在 step 7，checklog（记 PASS）与实际发出的
	// block 会脱节。promoteAdvisory 查 hostcap 注册表规则，规则把真 advisory 与各 hook
	// 的成功/干净分支隔开；skill-trigger 在
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
		// Level 显式设置：hook 的 FAIL 是真 block（decision:block 拦下工具调用），
		// 不是普通 fail——derive 只能从 Detail 前缀区分 gate 的 BLOCKED:/ADVISORY:，
		// 对 hook 输出会退化成 fail，语义不够精确。
		level := checklog.LevelPass
		if !passed {
			level = checklog.LevelBlocked
		}
		if err := checklog.Record(root, &checklog.Entry{
			Check:     checkName,
			Passed:    passed,
			Checked:   true,
			Level:     level,
			ToolName:  recordedToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
			Detail:    truncate(logDetail, maxChecklogDetail),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
		}
	}

	// 6b. Record tool usage for activity-ratio detection. auto-compile records Write/Edit;
	// tool-track records Read|Skill|Agent|Bash (matcher lives in ForgeHookSpec), giving the
	// read-before-edit gate (task-verify) Read data — otherwise that gate would always fail on any task
	// with an edit (644b142 deleted the original Read recorder).
	// tool_input population: auto-compile (Edit/Write) records file_path/content; tool-track's Skill/Agent
	// records the skill name/subagent_type (scheme C: lets toollog audits see which quality skill the agent loaded and what kind of
	// subagent it dispatched — root cause of zero quality-skill fires in advisory context is traceable). Bash records the
	// command (truncated) — the 2026-08-22 adherence audit found 27.7k Bash invocations with ZERO toollog rows because the
	// Bash matcher carried no tool-track; the audit (and any future hazard/behavior analysis) needs the command text,
	// same truncated treatment as Skill/Agent. Read records a minimal
	// {"file_path":...} (2026-08-16 review HIGH-1: the funnel join — skillseval.BuildTriggerFunnel — matches Read tool_input
	// suffixes to attribute "loaded the skill after the trigger hit"; omitting it made that join structurally dead on production
	// data while unit tests stayed green on hand-marshaled inputs. The lean-toollog tradeoff lost to the observability signal).
	//
	// 6b. 记录 tool usage 用于 activity-ratio 检测。auto-compile 记 Write/Edit；
	// tool-track 记 Read|Skill|Agent|Bash（matcher 在 ForgeHookSpec 中），让
	// read-before-edit gate（task-verify）有 Read 数据——否则该 gate 在任何带
	// edit 的 task 上恒失败（644b142 删过原来的 Read recorder）。
	// tool_input 填充：auto-compile（Edit/Write）记 file_path/content；tool-track 的 Skill/Agent
	// 记 skill 名/subagent_type（方案 C：让 toollog 审计能看到 agent 加载了哪个质量 skill、派了
	// 哪类子 agent——advisory 语境下质量 skill 0 触发的根因可追溯）。Bash 记命令（截断）——
	// 2026-08-22 遵循度审计发现 27.7k 次 Bash 调用在 toollog 零行（Bash matcher 没挂
	// tool-track）；审计（及未来的 hazard/行为分析）需要命令文本，与 Skill/Agent 同截断待遇。
	// Read 记最小 {"file_path":...}
	// （2026-08-16 审查 HIGH-1：漏斗 join——skillseval.BuildTriggerFunnel——靠 Read tool_input 的
	// 后缀匹配归因「命中后加载了该 skill」；省略它使该 join 在生产数据上结构性死亡，而单测用手工
	// marshal 的输入照样全绿。lean 权衡让位于可观测信号）。
	if name == "auto-compile" || name == "tool-track" {
		call := &toolusage.ToolCall{
			ToolName:  hookInput.ToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
		}
		if name == "auto-compile" || (name == "tool-track" && (hookInput.ToolName == "Skill" || hookInput.ToolName == "Agent" || hookInput.ToolName == "Bash")) {
			raw := string(hookInput.ToolInput)
			call.ToolInput = toolusage.TruncateInput(raw)
			call.InputLen = len(raw)
			call.EstTokens = toolusage.EstimateTokens(raw)
		} else if name == "tool-track" && hookInput.ToolName == "Read" && fields.FilePath != "" {
			// Minimal shape: ONLY file_path (not the full tool input) — the funnel join
			// (skillseval.BuildTriggerFunnel → readFilePath) suffix-matches this field; every other
			// Read field stays unrecorded. Pinned by TestHookToolTrackRecordsReadFilePath; the two
			// must not silently diverge again (that divergence was review HIGH-1).
			//
			// 最小形状：只记 file_path（非完整 tool input）——漏斗 join
			// （skillseval.BuildTriggerFunnel → readFilePath）按本字段做后缀匹配；Read 的其余
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
		// the Edit's PreToolUse hook can then see the path. Only tool-track
		// (Read) records paths; auto-compile (Edit/Write) does not.
		//
		// 方案2 shift-left：把本次 Read 的 file_path 追加到 per-session reads log，
		// 让 PreToolUse read-before-edit hook 能在 Edit 时拦截 Edit-without-Read。
		// toollog 现在也记 Read 的 file_path（漏斗 join，见上 6b），但本 side-channel 保留：
		// 它存项目相对路径（gate 匹配锚定项目根），且 Edit 时直接读取、无需解析 toollog
		// JSON。PostToolUse 在 Read 完成之后触发，所以本回合的 Read 会先于随后的 Edit
		// 被记录——Edit 的 PreToolUse hook 就能看到该路径。只有 tool-track
		// （Read）记路径；auto-compile（Edit/Write）不记。
		if name == "tool-track" && hookInput.ToolName == "Read" && fields.FilePath != "" {
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
	return emitAgentOutput(agent, eventName, name, passed, detail)
}

// readsFilePath returns the absolute path of this session's reads log — the PreToolUse
// read-before-edit hook (scheme 2 shift-left) greps it to intercept Edit-without-Read via this
// on-disk side channel. Per-session (keyed by sanitized session id), ephemeral
// ($TMPDIR). Persisted to disk rather than carried in context so it SURVIVES compaction within a session:
// a Read before compaction still counts toward an Edit after it, eliminating the biggest false-positive source of context-based checks.
//
// readsFilePath 返回本 session 的 reads log 绝对路径——PreToolUse
// read-before-edit hook（方案2 shift-left）grep 它来拦截 Edit-without-Read 的
// 磁盘 side-channel。Per-session（按 sanitized session id 作 key）、ephemeral
// （$TMPDIR）。落盘而非存于 context，是为了在 session 内 SURVIVES compaction：
// compact 之前的 Read 仍计入之后的 Edit，消除基于 context 检查的最大假阳性来源。
func readsFilePath(root, sessionID string) string {
	// projectTagFor(root) buckets the reads log by project: $TMPDIR is shared across projects, and naming by session id alone
	// would let project A's reads log be read by project B under short/reused session ids (e.g. test sid-*) —
	// the read-before-edit hook would then falsely conclude an Edit had been Read (false-positive pass). The project tag is fnv hex
	// (filename-safe), sourced identically with FORGE_PROJECT_TAG.
	//
	// projectTagFor(root) 把 reads log 按 project 分桶：$TMPDIR 跨项目共享，仅按 session id
	// 命名会在短/复用 session id（如测试 sid-*）下让 A 项目的 reads log 被 B 项目读到——
	// read-before-edit hook 会误判 Edit 已 Read 过（假阳性放行）。project tag 是 fnv hex
	// （文件名安全），与 FORGE_PROJECT_TAG 同源。
	return filepath.Join(os.TempDir(), "forge-session-reads-"+projectTagFor(root)+"-"+readsFileKey(sessionID)+".log")
}

// readsFileKey collapses a session id into a filename-safe token. SanitizeSessionID
// preserves readability but may still contain characters that file systems treat specially on some platforms; any character
// outside [A-Za-z0-9._-] is collapsed to '_' so the temp file name is always safe and the original id is not leaked into $TMPDIR.
//
// readsFileKey 把 session id 收敛为 filename-safe 的 token。SanitizeSessionID
// 保留可读性，但仍可能含某些平台上被文件系统特殊对待的字符；将 [A-Za-z0-9._-]
// 之外的字符一律折叠为 '_'，使临时文件名始终安全，且不把原始 id 泄漏到 $TMPDIR。
func readsFileKey(sessionID string) string {
	// Defensive: pin the empty-input token at the cli layer. util.SanitizeSessionID
	// is shared with other packages whose fallback semantics may evolve (it now
	// returns "session" for ""); the reads-log filename contract here stays
	// "default" for an empty session id regardless.
	//
	// 防御式：空输入 token 钉在 cli 层。util.SanitizeSessionID 与其他包共享，
	// 其兜底语义可能演进（现在 "" 返回 "session"）；本处 reads-log 文件名契约
	// 对空 session id 保持 "default"。
	if sessionID == "" {
		return "default"
	}
	s := util.SanitizeSessionID(sessionID)
	if s == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

// appendSessionRead appends a repo-relative Read path to the per-session reads log.
// Best-effort (advisory side channel): a write failure only means the read-before-edit hook
// will not see this Read — it must never cause the tool call to fail.
//
// appendSessionRead 把 repo-relative 的 Read 路径追加到 per-session reads log。
// Best-effort（advisory side-channel）：写入失败仅意味着 read-before-edit hook
// 看不到这次 Read——绝不能让 tool call 因此失败。
func appendSessionRead(path, relPath string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(relPath + "\n")
}

// sanitizeForShell sanitizes a string into a form safe for use as a shell env var. Prevents
// shell injection when user-controlled content reaches a bash script via an env var.
//
// sanitizeForShell 把字符串净化为可安全用于 shell env var 的形式。防止
// user-controlled 内容经 env var 传入 bash 脚本时发生 shell injection。
//
// Strategy:
//   - Truncate to maxEnvValueLen to prevent memory exhaustion
//   - Replace NULL bytes and control characters (except tab, newline, carriage return)
//   - Unicode-safe validation (reject invalid UTF-8)
//   - No quoting or escaping — callers must use export VAR=$value themselves and double-quote the value
//
// 策略：
//   - 截断到 maxEnvValueLen，防内存耗尽
//   - 替换 NULL 字节和控制字符（tab、newline、carriage return 除外）
//   - Unicode-safe 校验（拒绝非法 UTF-8）
//   - 不做引号或转义——调用方须自行用 export VAR=$value 并给 value 加双引号
//
// Note: this is a defense-in-depth measure. The hook script itself should also validate input before use.
//
// 注意：这是 defense-in-depth 措施。hook 脚本自身在使用前也应校验输入。
func sanitizeForShell(value string) string {
	if value == "" {
		return ""
	}

	// Truncate to prevent memory issues.
	//
	// 截断以防内存问题
	if len(value) > maxEnvValueLen {
		// Truncate at a UTF-8 boundary.
		//
		// 在 UTF-8 边界处截断
		for offset := maxEnvValueLen - 10; offset < maxEnvValueLen; offset++ {
			if offset >= len(value) {
				break
			}
			if utf8.RuneStart(value[offset]) {
				value = value[:offset]
				break
			}
		}
		// Fallback: invalid UTF-8 in the 10-byte window can leave no RuneStart,
		// so the loop above finishes without truncating and the overlong value
		// would reach the env unchanged. Hard-truncate at the limit instead.
		//
		// 兜底：10 字节窗口内含非法 UTF-8 时可能找不到 RuneStart，循环走完不
		// 截断，超长 value 会原样进 env。改为在限制处硬截断。
		if len(value) > maxEnvValueLen {
			value = value[:maxEnvValueLen]
		}
	}

	// Validate UTF-8 and remove control characters.
	//
	// 校验 UTF-8 并移除控制字符
	var result strings.Builder
	result.Grow(len(value))

	for _, r := range value {
		// Check UTF-8 validity.
		//
		// 检查 UTF-8 合法性
		if r == utf8.RuneError {
			// Skip invalid runes.
			//
			// 跳过非法 rune
			continue
		}

		// Remove NULL bytes and most control characters.
		// Allow: tab (0x09), newline (0x0A), carriage return (0x0D).
		// Block: NULL (0x00) and other control characters (0x01-0x08, 0x0B-0x0C, 0x0E-0x1F).
		//
		// 移除 NULL 字节和大多数控制字符
		// 放行：tab (0x09)、newline (0x0A)、carriage return (0x0D)
		// 拦截：NULL (0x00) 及其他控制字符 (0x01-0x08、0x0B-0x0C、0x0E-0x1F)
		if r == 0 {
			// Replace NULL with a space.
			//
			// NULL 替换为空格
			result.WriteRune(' ')
			continue
		}
		if r < 0x20 && r != 0x09 && r != 0x0A && r != 0x0D {
			// Skip other control characters.
			//
			// 跳过其他控制字符
			continue
		}

		result.WriteRune(r)
	}

	return result.String()
}

// extractDetail parses output of PASS/WARN/FAIL with optional detail. Returns the
// detail part after the keyword; if the output does not start with a known prefix, returns the full output.
//
// extractDetail 解析 PASS/WARN/FAIL 加可选 detail 的输出。返回关键字之后的
// detail 部分；若不以已知前缀开头，则返回完整输出。
func extractDetail(stdout, prefix string) string {
	if stdout == "" {
		return ""
	}
	for _, p := range []string{prefix, "WARN"} {
		after, ok := strings.CutPrefix(stdout, p)
		if ok {
			return strings.TrimSpace(after)
		}
	}
	return stdout
}

// outputEmitters maps the host name to its output-protocol emitter. The emitter
// BODIES stay in cli (they write os.Stdout directly and return cli's
// *HookBlockError); the registry cannot hold them because hostcap is a
// data-only leaf that must not import cli (see the hostcap package doc). Unknown
// agents — claude-code and every Claude-JSON-compatible host without --agent
// (codebuddy/opencode) — take the emitClaudeOutput default in emitAgentOutput.
//
// outputEmitters 把宿主名映射到其输出协议 emitter。emitter **函数本体**留在
// cli（它们直接写 os.Stdout 并返回 cli 的 *HookBlockError）；注册表无法持有
// 它们——hostcap 是纯数据叶子包，不能 import cli（见 hostcap 包文档）。未知
// agent——claude-code 及所有不带 --agent 的 Claude-JSON 兼容宿主
// （codebuddy/opencode）——在 emitAgentOutput 里走 emitClaudeOutput 默认。
var outputEmitters = map[string]func(eventName, hookName string, passed bool, detail string) error{
	"kimi": func(_, _ string, passed bool, detail string) error { return emitKimiOutput(passed, detail) },
	"codex": func(eventName, _ string, passed bool, detail string) error {
		return emitCodexOutput(eventName, passed, detail)
	},
	"cursor": func(eventName, _ string, passed bool, detail string) error {
		return emitCursorOutput(eventName, passed, detail)
	},
	"copilot": emitCopilotOutput,
	"windsurf": func(_, hookName string, passed bool, detail string) error {
		return emitWindsurfOutput(hookName, passed, detail)
	},
	"cline": func(_, _ string, passed bool, detail string) error { return emitClineOutput(passed, detail) },
}

// emitAgentOutput dispatches the hook verdict to the host's output protocol. agent==""
// (claude-code and every Claude-JSON-compatible host that carries no --agent flag:
// codebuddy/opencode) takes the claude default. The load-bearing invariants:
//   - allow NEVER emits decision:"approve" — on Claude PreToolUse it bypasses the
//     permission system (an allow hook must not grant permissions), and codex parses
//     it but marks the hook as FAILED.
//   - block ALWAYS returns *HookBlockError → exit 2 (except copilot Stop, where exit 2
//     is a warning and the decision JSON + exit 0 is the only block channel). Exit 2
//     is the block code codex (stderr+exit2), cursor (deny-equivalent) and copilot
//     preToolUse (deny, fail-closed) all honor; the old generic error (exit 1) was
//     non-blocking on all of them.
//
// emitAgentOutput 把 hook 结论分发到宿主的输出协议。agent==""（claude-code 及所有
// 不带 --agent flag 的 Claude-JSON 兼容宿主：codebuddy/opencode）走 claude 默认。
// 关键不变式：
//   - allow 绝不发 decision:"approve"——Claude PreToolUse 上它会绕过权限系统
//     （allow hook 不得授予权限），codex 则解析它但把 hook 判为 FAILED。
//   - block 恒返回 *HookBlockError → exit 2（唯一例外 copilot Stop：那里 exit 2 是
//     warning，decision JSON + exit 0 才是唯一阻断通道）。exit 2 是 codex（stderr+
//     exit2）、cursor（等价 deny）、copilot preToolUse（deny、fail-closed）共同认可
//     的阻断码；旧 generic error（exit 1）在它们上面都不构成阻断。
func emitAgentOutput(agent, eventName, hookName string, passed bool, detail string) error {
	detail = truncate(detail, maxAdditionalContextLen)
	if emit, ok := outputEmitters[agent]; ok {
		return emit(eventName, hookName, passed, detail)
	}
	return emitClaudeOutput(eventName, passed, detail)
}

// contextChannelDelivered reports whether an ALLOW-path detail emission on (agent, event)
// actually reaches the model's context on that host, plus a short channel label. The
// per-host channel data lives in the hostcap registry (ContextChannels/DefaultChannel
// columns, each row citing its source emitter); this thin wrapper exists so the record
// site (recordSkillTriggerHits) can stamp Delivered/Channel into checklog without
// re-deriving the routing table, and so analysis (usage funnel) has a truthful delivery
// denominator instead of counting entries the model never saw. This generalizes the
// kimi 2026-08-15 false-prosperity fix (bail before recording on invisible channels) to
// every host: recording may stay (audit trail), but "delivered" must say the truth.
//
// contextChannelDelivered 报告 (agent, event) 上 allow 路径的 detail 输出是否真到达该
// 宿主的模型上下文，并给出简短通道标签。每宿主通道数据住在 hostcap 注册表
// （ContextChannels/DefaultChannel 列，各行注明出处 emitter）；保留这个薄包装，记录点
// （recordSkillTriggerHits）就能把 Delivered/Channel 落章进 checklog 而无需重推路由
// 表，分析侧（usage 漏斗）也拿到真实的送达分母，而不是把模型从未见过的条目也计成
// 送达。这是 kimi 2026-08-15 虚假繁荣修复（不可见通道先 bail 再记录）的泛化：记录
// 可以留（审计轨迹），但 delivered 必须说真话。
func contextChannelDelivered(agent, eventName string) (bool, string) {
	return hostcap.ContextChannel(agent, eventName)
}

// emitClaudeOutput renders the claude-code default (also codebuddy/opencode — every
// host that parses Claude's stdout JSON but carries no --agent flag): allow = silent
// (exit 0, default flow untouched; with detail, a bare hookSpecificOutput whose
// additionalContext Claude injects — no decision field); block = decision:block JSON +
// reason on stderr + HookBlockError (Execute maps it to exit 2, Claude's blocking
// error code, with stderr shown to the model).
//
// emitClaudeOutput 渲染 claude-code 默认形态（也覆盖 codebuddy/opencode——所有
// 解析 Claude stdout JSON 但不带 --agent flag 的宿主）：allow = 静默（exit 0，默认
// 流程不动；有 detail 时发裸 hookSpecificOutput，Claude 会注入其 additionalContext
// ——无 decision 字段）；block = decision:block JSON + 原因写 stderr +
// HookBlockError（Execute 映射为 exit 2——Claude 的阻断错误码，stderr 会展示给模型）。
func emitClaudeOutput(eventName string, passed bool, detail string) error {
	if passed {
		if detail == "" {
			return nil
		}
		out := HookOutput{HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     eventName,
			AdditionalContext: detail,
		}}
		data, _ := json.Marshal(out)
		fmt.Println(string(data))
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	// #4-B: on PreToolUse the deny ALSO rides the current hookSpecificOutput.
	// permissionDecision field — the legacy top-level decision:"block" is no longer
	// adopted there (still emitted for the hosts/events that read it; Stop keeps
	// using top-level decision). exit 2 below remains the load-bearing block channel
	// ("routes the same way as deny"), so this is additive schema alignment, not a
	// behavior change.
	//
	// #4-B：PreToolUse 上 deny 同时走现行 hookSpecificOutput.permissionDecision
	// 字段——遗留顶层 decision:"block" 在该事件上已不被采纳（仍为读它的宿主/事件
	// 发出；Stop 继续用顶层 decision）。下方 exit 2 仍是承重阻断通道
	// （"routes the same way as deny"），本改动是 additive 的 schema 对齐，非行为变更。
	hso := &HookSpecificOutput{
		HookEventName:     eventName,
		AdditionalContext: detail,
	}
	if eventName == "PreToolUse" {
		hso.PermissionDecision = "deny"
		hso.PermissionDecisionReason = detail
	}
	out := HookOutput{
		Decision:           "block",
		Reason:             detail,
		HookSpecificOutput: hso,
	}
	data, _ := json.Marshal(out)
	fmt.Println(string(data))
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitCodexOutput renders codex's protocol (developers.openai.com/codex/hooks):
// hookSpecificOutput.additionalContext is honored on SessionStart/PreToolUse/
// PostToolUse/UserPromptSubmit (SubagentStart — not a forge event); Stop/PostCompact
// have no context channel. decision:"approve" is parsed-but-UNSUPPORTED — codex marks
// the hook as failed — so the allow path emits a BARE hookSpecificOutput (Claude-legal
// and codex-legal). Block = stderr + exit 2 (codex's only reliable block channel;
// decision:"block" stdout is legacy and not relied on).
//
// emitCodexOutput 渲染 codex 协议（developers.openai.com/codex/hooks）：
// hookSpecificOutput.additionalContext 仅在 SessionStart/PreToolUse/PostToolUse/
// UserPromptSubmit 上被采纳（SubagentStart 非 forge 事件）；Stop/PostCompact 无上下文
// 通道。decision:"approve" 会被解析但**不支持**——codex 把 hook 判为 failed——故
// allow 路径发**裸** hookSpecificOutput（Claude 合法且 codex 合法）。阻断 =
// stderr + exit 2（codex 唯一可靠的阻断通道；stdout decision:"block" 是遗留行为，
// 不依赖它）。
func emitCodexOutput(eventName string, passed bool, detail string) error {
	if passed {
		switch eventName {
		case "SessionStart", "PreToolUse", "PostToolUse", "UserPromptSubmit":
			if detail != "" {
				out := HookOutput{HookSpecificOutput: &HookSpecificOutput{
					HookEventName:     eventName,
					AdditionalContext: detail,
				}}
				data, _ := json.Marshal(out)
				fmt.Println(string(data))
			}
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitCursorOutput renders cursor's protocol (cursor.com/docs/agent/hooks):
// postToolUse/sessionStart read a TOP-LEVEL snake_case additional_context; the other
// events' allow path has no context channel. Block = stderr + exit 2 (cursor treats
// exit 2 as the permission-deny equivalent on every event).
//
// emitCursorOutput 渲染 cursor 协议（cursor.com/docs/agent/hooks）：
// postToolUse/sessionStart 读**顶层** snake_case additional_context；其余事件的
// allow 路径无上下文通道。阻断 = stderr + exit 2（cursor 在所有事件上把 exit 2 当
// 等价 permission deny）。
func emitCursorOutput(eventName string, passed bool, detail string) error {
	if passed {
		switch eventName {
		case "PostToolUse", "SessionStart":
			if detail != "" {
				fmt.Printf(`{"additional_context":%s}`+"\n", jsonString(detail))
			}
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitCopilotOutput renders GitHub Copilot's protocol
// (docs.github.com/en/copilot/reference/hooks-reference — verified in full this
// session). Exit codes: 0 = success (stdout parsed as JSON), 2 = warning (stderr
// surfaced, run continues) EXCEPT preToolUse where exit 2 = deny merged with the
// stdout decision. agentStop/subagentStop block ONLY via stdout {"decision":"block"}
// + exit 0 — exit 2 there is a warning, not a block. Context channels: sessionStart/
// postToolUse top-level camelCase additionalContext (joined double-newline across
// hooks, 10KB cap); userPromptSubmitted stdout is DROPPED for command hooks.
// PascalCase event keys (as wired by the plugin pack) give Claude matcher semantics
// and snake_case payloads, so the stdin side needs no normalizer.
//
// emitCopilotOutput 渲染 GitHub Copilot 协议
// （docs.github.com/en/copilot/reference/hooks-reference——本会话全文核实）。退出码：
// 0 = 成功（stdout 按 JSON 解析）、2 = warning（stderr 上浮、继续执行）——唯一例外
// preToolUse 的 exit 2 = deny 并与 stdout decision 合并。agentStop/subagentStop 只能
// 经 stdout {"decision":"block"} + exit 0 阻断——那里的 exit 2 是 warning 不是阻断。
// 上下文通道：sessionStart/postToolUse 顶层 camelCase additionalContext（多 hook 以
// 双换行拼接、10KB 上限）；userPromptSubmitted 的 stdout 对 command hook 会被丢弃。
// PascalCase 事件键（plugin pack 的接线方式）给出 Claude matcher 语义与 snake_case
// payload，故 stdin 侧无需 normalizer。
func emitCopilotOutput(eventName, hookName string, passed bool, detail string) error {
	if passed {
		switch eventName {
		case "PostToolUse", "SessionStart":
			if detail != "" {
				fmt.Printf(`{"additionalContext":%s}`+"\n", jsonString(detail))
			}
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	switch eventName {
	case "PreToolUse":
		// Fail-closed event: any non-zero denies, and exit 2 merges with the stdout
		// deny decision — emit both so the reason survives the merge.
		//
		// fail-closed 事件：任何非零都 deny，且 exit 2 与 stdout deny decision 合并
		// ——两者都发，让原因在合并后仍可见。
		fmt.Printf(`{"permissionDecision":"deny","permissionDecisionReason":%s}`+"\n", jsonString(detail))
		return &HookBlockError{Reason: detail}
	case "Stop":
		// exit 2 on agentStop is only a warning — the decision JSON with exit 0 is the
		// ONLY block channel (block forces another turn; runaway guard after 8
		// consecutive blocks).
		//
		// agentStop 上 exit 2 只是 warning——decision JSON + exit 0 是唯一阻断通道
		// （block 强制再来一轮；连续 8 次阻断后有 runaway guard）。
		fmt.Printf(`{"decision":"block","reason":%s}`+"\n", jsonString(detail))
		return nil
	default:
		// Other events (postToolUse/...): no documented block channel; exit 2 behaves
		// as a warning whose stderr reaches the model. task-verify/review-stop (the
		// forge Stop hooks) are handled above; PostToolUse hooks rarely block.
		//
		// 其余事件（postToolUse/...）：无文档化阻断通道；exit 2 表现为 warning、
		// stderr 可达模型。task-verify/review-stop（forge 的 Stop hook）已在上面
		// 处理；PostToolUse hook 极少阻断。
		fmt.Fprintln(os.Stderr, detail)
		return &HookBlockError{Reason: detail}
	}
}

// emitWindsurfOutput renders Windsurf Cascade's protocol: there is NO stdout JSON
// protocol at all (hook entries run with show_output:false; stdout JSON would be noise
// if ever displayed) — allow is silent, block is stderr + exit 2. Exit 2 denies on the
// pre_* hooks; post_cascade_response (where the Stop group hangs) is an async
// post-hook that CANNOT block — there exit 2 only surfaces the stderr reason to the
// agent as an advisory (documented honestly in buildWindsurfHooks).
//
// emitWindsurfOutput 渲染 Windsurf Cascade 协议：完全没有 stdout JSON 协议（hook
// 条目以 show_output:false 运行；stdout JSON 即便被显示也只是噪声）——allow 静默、
// 阻断 = stderr + exit 2。pre_* hook 上 exit 2 deny；post_cascade_response（Stop 组
// 挂载处）是异步 post-hook，**无法阻断**——那里 exit 2 只把 stderr 原因以 advisory
// 形式上浮给 agent（已在 buildWindsurfHooks 诚实文档化）。
func emitWindsurfOutput(hookName string, passed bool, detail string) error {
	if passed {
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitClineOutput renders Cline's file-hook protocol (v3.36+ hooks blog): a hook
// speaks by printing {"cancel":bool,"errorMessage":...,"contextModification":...} —
// cancel blocks the action, contextModification injects text into the task. The forge
// wrapper script (~/Documents/Cline/Rules/Hooks/<Event>) fans one Cline event out to
// several forge hooks and merges verdicts, using the EXIT CODE as the robust block
// signal (block = exit 2 via HookBlockError, with this ready-made cancel JSON already
// on stdout); contextModification is forwarded on the allow path. Emission is compact
// JSON ({"cancel":true,…) on purpose — nothing parses it in-band, but the wrapper's
// context sniffing relies on the field name never appearing inside forge's own allow
// output shape by accident.
//
// emitClineOutput 渲染 Cline 的文件 hook 协议（v3.36+ hooks 博客）：hook 通过打印
// {"cancel":bool,"errorMessage":...,"contextModification":...} 表态——cancel 阻断动作、
// contextModification 向任务注入文本。forge 的 wrapper 脚本
// （~/Documents/Cline/Rules/Hooks/<Event>）把一个 Cline 事件扇出到多个 forge hook
// 并合并结论，用**退出码**作稳健的阻断信号（阻断 = 经 HookBlockError 的 exit 2，
// 且这份现成的 cancel JSON 已在 stdout）；allow 路径转发 contextModification。
// 刻意输出紧凑 JSON（{"cancel":true,…）——没有谁在带内解析它，但 wrapper 的上下文
// 嗅探依赖该字段名不会碰巧出现在 forge 自身的 allow 输出形态里。
func emitClineOutput(passed bool, detail string) error {
	if passed {
		if detail == "" {
			return nil
		}
		fmt.Printf(`{"cancel":false,"contextModification":%s}`+"\n", jsonString(detail))
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Printf(`{"cancel":true,"errorMessage":%s}`+"\n", jsonString(detail))
	return &HookBlockError{Reason: detail}
}

// jsonString marshals s as a JSON string literal (escaping, quotes) for embedding in
// hand-composed protocol envelopes. Never fails for a string input.
//
// jsonString 把 s 编组为 JSON 字符串字面量（转义、引号），用于嵌进手工组合的协议
// envelope。对字符串输入不会失败。
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// applyPatchFilePath extracts the first target path from a codex apply_patch payload.
// The patch headers are `*** Add File: <path>` / `*** Update File: <path>` /
// `*** Delete File: <path>`; the first header wins for multi-file patches (the common
// case is single-file). Returns "" when no header is present (malformed/unrelated
// command) — the hooks then see an empty path, same as today.
//
// applyPatchFilePath 从 codex apply_patch payload 抽取第一个目标路径。patch 头是
// `*** Add File: <path>` / `*** Update File: <path>` / `*** Delete File: <path>`；
// 多文件 patch 取第一个头（常见情形是单文件）。无头（畸形/无关命令）返回 ""——
// hook 于是看到空路径，与现状一致。
func applyPatchFilePath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		body := strings.TrimPrefix(strings.TrimSpace(line), "*** ")
		for _, header := range []string{"Add File:", "Update File:", "Delete File:"} {
			if strings.HasPrefix(body, header) {
				if p := strings.TrimSpace(strings.TrimPrefix(body, header)); p != "" {
					return p
				}
			}
		}
	}
	return ""
}

// HookBlockError signals an intentional hook block for hosts whose protocol selects
// behavior by exit code (kimi: 2 = intentional block, any other non-zero = fail-open
// allow). Execute maps it to os.Exit(2); the reason is written to stderr at the
// emission site (kimi shows stderr to the model as the block reason).
//
// HookBlockError 表示一次有意的 hook 阻断，面向按退出码区分行为的宿主
// （kimi：2 = 有意阻断，其他非零 = fail-open 放行）。Execute 把它映射为
// os.Exit(2)；原因在输出处写入 stderr（kimi 把 stderr 作为阻断原因展示给模型）。
type HookBlockError struct {
	Reason string
}

func (e *HookBlockError) Error() string { return e.Reason }

// promoteAdvisory reports whether an advisory (passed=true, non-empty detail) result for the
// given hook should be promoted to a block on the given host. The per-hook rules live in the
// hostcap registry (PromoteAdvisory column — kimi and dsh today: kimi 0.35.0 drops
// allow-path stdout from the model context, so its advisories silently evaporate; dsh's
// channel delivers but the task-guard advisory was empirically ignored — 2026-08-22, see
// the dsh registry row). The rules are declarative Contains/Excludes pairs
// (not a bare name allowlist) because each hook emits BOTH an advisory branch and a
// success/clean branch under the same hook name — a name allowlist would over-block
// (task-guard's "Auto-created task" is a SUCCESS path; assertion-check's clean branch is
// advisory-free). Promoting the REAL advisory to a block repoints it through the one
// PreToolUse channel every host honors: exit 2 (stderr shown to the model).
//
// Returns false for: the escape hatches (FORGE_ADVISORY_PROMOTION=soft covers every
// promoted host; FORGE_KIMI_ADVISORY=soft kept for back-compat, kimi-scoped — env knobs,
// kept here in
// cli rather than the registry because they are operator config, not host capabilities),
// already-blocked results (no double-flip), empty/whitespace detail (clean/silent PASS),
// and hosts without promotion rules.
//
// promoteAdvisory 报告给定 hook 的 advisory（passed=true、detail 非空）结果在给定宿主上
// 是否应提升为阻断。各 hook 的规则住在 hostcap 注册表（PromoteAdvisory 列——目前
// kimi 与 dsh：kimi 0.35.0 丢弃 allow 路径 stdout，advisory 在那里静默蒸发；dsh 通道
// 送达但 task-guard advisory 被实证无视——2026-08-22，见 dsh 注册表行）。规则是声明式
// Contains/Excludes 对（非裸名字白名单），因为每个 hook 在同一 hook 名下同时发
// advisory 分支与成功/干净分支——名字白名单会过度阻断（task-guard 的
// "Auto-created task" 是成功路径；assertion-check 的干净分支无 advisory）。把真
// advisory 提升为阻断让它改走所有宿主都认的 PreToolUse 通道：exit 2（stderr 展示给
// 模型）。
//
// 以下返回 false：逃生舱（FORGE_ADVISORY_PROMOTION=soft 覆盖所有提升宿主；
// FORGE_KIMI_ADVISORY=soft 保留向后兼容——env 开关，留在 cli 而非注册表——它们是
// 运维配置而非宿主能力）、已阻断结果（不二次翻转）、空/纯空白 detail（干净/静默
// PASS）、无提升规则的宿主。
func promoteAdvisory(agent, name string, passed bool, detail string) bool {
	if advisoryPromotionDisabled(agent) {
		return false
	}
	if !passed || strings.TrimSpace(detail) == "" {
		return false
	}
	h := hostcap.Lookup(agent)
	if h == nil {
		return false
	}
	return h.ShouldPromoteAdvisory(name, detail)
}

// advisoryPromotionDisabled is the single escape-hatch check shared by every
// advisory-promotion consumer (promoteAdvisory, taskGuardPromotionActive), so the
// hatch can never be open in one place and closed in another — e.g.
// FORGE_TASKGUARD_PROMOTED set (script drops its de-noise, WARN on every edit)
// while promotion itself is suppressed would resurrect the 139-WARN spam with no
// enforcement behind it (dogfood 3.1). Host-scoped: FORGE_ADVISORY_PROMOTION=soft
// covers every promoted host; the shipped FORGE_KIMI_ADVISORY=soft stays
// kimi-scoped — an operator softening kimi must not silently soften dsh (and a
// new host's promotion must not ride a hatch named for another host).
//
// advisoryPromotionDisabled 是所有 advisory 提升消费方（promoteAdvisory、
// taskGuardPromotionActive）共享的唯一逃生舱检查，使逃生舱不可能在一处开着
// 另一处关着——例如 FORGE_TASKGUARD_PROMOTED 已设（脚本放弃去噪、每次 edit 都
// WARN）而提升本身被抑制，会复活 139 次 WARN 刷屏（dogfood 3.1）且背后无执法。
// 按 host 划定：FORGE_ADVISORY_PROMOTION=soft 覆盖所有提升宿主；已发布的
// FORGE_KIMI_ADVISORY=soft 保持仅 kimi——软化 kimi 的运维意图不得静默波及 dsh
// （新宿主的提升也不得搭一个以别的宿主命名的逃生舱）。
func advisoryPromotionDisabled(host string) bool {
	if os.Getenv("FORGE_ADVISORY_PROMOTION") == "soft" {
		return true
	}
	return host == "kimi" && os.Getenv("FORGE_KIMI_ADVISORY") == "soft"
}

// taskGuardPromotionActive reports whether task-guard advisories on this host are
// currently promoted (a task-guard rule exists in hostcap AND the escape hatch is
// closed) — detail-independent, so runHook can set FORGE_TASKGUARD_PROMOTED for the
// script BEFORE any script output exists. See the runHook call site for why the
// script must know.
//
// taskGuardPromotionActive 报告该宿主的 task-guard advisory 当前是否被提升
// （hostcap 存在 task-guard 规则且逃生舱关闭）——与 detail 无关，让 runHook 能在
// 任何脚本输出产生前为脚本设置 FORGE_TASKGUARD_PROMOTED。脚本为何需要知道，见
// runHook 调用处注释。
func taskGuardPromotionActive(agent string) bool {
	if advisoryPromotionDisabled(agent) {
		return false
	}
	h := hostcap.Lookup(agent)
	return h != nil && h.PromotesHook("task-guard")
}

// promoteKimiAdvisory is the kimi-specialized wrapper of promoteAdvisory, kept so the
// existing unit tests (hook_kimi_test.go) exercise the registry rules without spinning up
// a full kimi runHook. Production call sites use promoteAdvisory directly.
//
// promoteKimiAdvisory 是 promoteAdvisory 的 kimi 特化包装，保留给既有单测
// （hook_kimi_test.go）不经完整 kimi runHook 即可检验注册表规则。生产调用处直接用
// promoteAdvisory。
func promoteKimiAdvisory(name string, passed bool, detail string) bool {
	return promoteAdvisory("kimi", name, passed, detail)
}

// emitKimiOutput renders the hook result in kimi's hook protocol: allow = exit 0 with
// the detail as plain stdout text; block = reason on stderr + HookBlockError (→ exit 2).
// Returning HookBlockError instead of calling os.Exit here keeps runHook's defers (temp
// script cleanup) running.
//
// CAVEAT — kimi 0.35.0 does NOT append allow-path stdout to the model context the way
// Claude Code treats additionalContext. Only UserPromptSubmit stdout reaches the model
// (delivered on the NEXT prompt); PreToolUse/Stop reach it via exit-2 BLOCK. PostToolUse/
// SessionStart allow-path stdout is observation-only (dropped). So the detail printed on
// the allow path here is model-visible ONLY on UserPromptSubmit; advisory hooks are
// rerouted per internal/agentbridge/kimi-hook-routing.md.
//
// emitKimiOutput 按 kimi 的 hook 协议渲染结果：放行 = exit 0，detail 以纯文本打
// stdout；阻断 = 原因写 stderr + HookBlockError（→ exit 2）。返回 HookBlockError 而非
// 在此 os.Exit，是为了让 runHook 的 defer（临时脚本清理）照常执行。
//
// 注意——kimi 0.35.0 并不像 Claude Code 那样把 allow 路径 stdout 注入模型上下文
// （additionalContext）。只有 UserPromptSubmit 的 stdout 能到模型（下一 prompt 送达）；
// PreToolUse/Stop 经 exit-2 阻断到模型。PostToolUse/SessionStart 的 allow 路径 stdout
// 是 observation-only（丢弃）。故此处 allow 路径打印的 detail 仅在 UserPromptSubmit 时
// 模型可见；advisory hook 按 internal/agentbridge/kimi-hook-routing.md 重路由。
func emitKimiOutput(passed bool, detail string) error {
	if passed {
		if detail != "" {
			fmt.Println(detail)
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// findBash resolves the bash interpreter for hook scripts. The implementation
// (including the Windows WSL-avoidance logic) lives in internal/shellexec and is
// shared with the gate path (taskpipeline.runEmbeddedHook) — a bare PATH lookup
// there resolved to WSL bash and failed every gate auto-compile with
// 'forge-gate-*.sh: No such file or directory'.
//
// findBash 解析 hook 脚本的 bash 解释器。实现（含 Windows WSL 规避逻辑）在
// internal/shellexec，与 gate 路径（taskpipeline.runEmbeddedHook）共用——那里
// 曾用裸 PATH 查找解析到 WSL bash，导致 gate 的 auto-compile 全部报
// 'forge-gate-*.sh: No such file or directory'。
func findBash() (string, error) {
	return shellexec.FindBash()
}

// isHookInfraFailure distinguishes "bash could not run the script" from "the
// script ran and reported FAIL" (spawn error or bash exit 126/127 → fail-open,
// not a gate verdict). Implementation shared with the gate path in
// internal/shellexec.
//
// isHookInfraFailure 区分"bash 没能跑起脚本"与"脚本跑了并报告 FAIL"（spawn
// 错误或 bash exit 126/127 → fail-open，非门禁结论）。实现与 gate 路径共用在
// internal/shellexec。
func isHookInfraFailure(err error) bool {
	return shellexec.IsHookInfraFailure(err)
}

// emitInfraAllow fails open for an infrastructure failure: the warning must be VISIBLE
// (a silently broken gate set is worse than a noisy one) without blocking the turn.
// Routed through emitAgentOutput's allow-with-detail path so every host gets its own
// context channel: kimi plain stdout (model-visible only on UserPromptSubmit — see
// internal/agentbridge/kimi-hook-routing.md); claude default a bare hookSpecificOutput
// (hookEventName present — Claude's schema requires it or additionalContext is
// dropped); codex the same bare shape on the four context-carrying events; cursor
// top-level additional_context; copilot top-level additionalContext; cline
// contextModification; windsurf silent (no stdout channel — unchanged visibility).
// No host receives decision:"approve" (see emitAgentOutput).
//
// emitInfraAllow 对基础设施失败 fail-open：警告必须可见（静默失效的门禁比吵闹的
// 更糟）但不阻断当轮。经 emitAgentOutput 的 allow-with-detail 路径分发，让每个
// 宿主走自己的上下文通道：kimi 纯文本 stdout（仅 UserPromptSubmit 时模型可见——
// 见 internal/agentbridge/kimi-hook-routing.md）；claude 默认裸 hookSpecificOutput
// （hookEventName 必在——Claude schema 要求它，否则 additionalContext 被丢弃）；
// codex 在四个可带上下文的事件上发同样的裸形态；cursor 顶层 additional_context；
// copilot 顶层 additionalContext；cline contextModification；windsurf 静默（无
// stdout 通道——可见性与之前一致）。任何宿主都不会收到 decision:"approve"
// （见 emitAgentOutput）。
func emitInfraAllow(agent, eventName, warning string) error {
	return emitAgentOutput(agent, eventName, "", true, warning)
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

// scoringPassUnchanged reports whether a scoring check's PASS would be a
// duplicate of the current state: the latest entry for the check (same session
// scope scoring uses) is already a PASS. In that case the repeat PASS is
// skipped — scoring's LatestByCheckForSession still resolves to the earlier
// PASS, so CompilePassed/AssertionPassed semantics do not regress. Returns
// false (record) when there is no prior entry, the latest is a FAIL (state
// change FAIL→PASS), the check is non-scoring, or the lookup fails (a lookup
// error must not silently drop audit data — fail toward recording).
// Session-filtering caveat (accepted): if the previous PASS belongs to a
// different session, this session's first PASS is still written — the cross
// process cost of one entry per session per check is fine.
//
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
	latest, err := checklog.LatestByCheckForSession(root, sessionID)
	if err != nil {
		return false
	}
	e, ok := latest[name]
	return ok && e.Passed
}

// isScoringCheck decides whether a hook check's PASS will be consumed by task scoring.
// scoreTask (task.go) reads LatestByCheckForSession for these checks to populate
// CompilePassed/AssertionPassed; their PASS must be written to the log so scoring sees
// checked & passed. Other checks' PASS is dropped by the noise gate (only FAIL is recorded). Note:
// test-coverage scoring reads a separate test-coverage-gate entry written by taskpipeline at task-verify
// (not this hook path), so test-coverage-check does not need to write PASS here.
//
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

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// toRelPath converts an absolute file path to a project-root-relative, slash-separated
// relative path. This way, patterns like .forge/* in shell scripts match correctly regardless of OS path format.
// On failure, returns the input unchanged.
// toRelPath returns the path of absPath relative to root, slash-separated. Both inputs are symlink-resolved first:
// on macOS, paths like t.TempDir() directories arrive via symlinks
// (/var/folders/... -> /private/var/folders/...), while findProjectRoot's
// os.Getwd() returns the physical form and tool_input's file_path arrives in symlink form.
// Without resolving both sides, filepath.Rel would cross the link boundary and produce ../../... paths that no longer
// match the hook glob patterns (.forge/*, .claude/settings*) — this is the root cause of task-guard uniquely
// failing to block .forge/state.json writes on macOS.
//
// toRelPath 把绝对文件路径转换为以 project root 为基准、用正斜杠分隔的相对
// 路径。这样 shell 脚本里的 .forge/* 等模式才能无视 OS 路径格式正确匹配。
// 转换失败时原样返回。
// toRelPath 返回 absPath 相对 root 的路径、用正斜杠分隔。两个入参都先做
// symlink 解析：在 macOS 上，类似 t.TempDir() 目录的路径会经由 symlink 到达
// （/var/folders/... → /private/var/folders/...），而 findProjectRoot 用的
// os.Getwd() 返回 physical 形式，tool_input 的 file_path 却以 symlink 形式
// 到达。不先解析两侧，filepath.Rel 会跨 link 边界产出 ../../... 路径，不再
// 匹配 hook glob 模式（.forge/*、.claude/settings*）——这是 task-guard 在
// macOS 上独有地拦不住 .forge/state.json 写入的根因。
func toRelPath(root, absPath string) string {
	if root == "" || absPath == "" {
		return absPath
	}
	root = resolveSymlinks(root)
	absPath = resolveSymlinks(absPath)
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	return filepath.ToSlash(rel)
}

// resolveSymlinks resolves symlinks on path. If path does not yet exist (e.g. a PreToolUse
// Write target before the file is created), it walks UP to the longest existing ancestor
// directory, resolves that, and joins the not-yet-existing tail back — so a new file deep
// under non-existent directories still gets the physical prefix (macOS /var symlinks;
// Windows 8.3 short names, where EvalSymlinks also expands ADMINI~1 → Administrator).
// The one-level climb was enough when .forge/ always existed, but after
// user-level-assets (zero project writes) .forge/ typically does NOT exist — the tail
// is two segments (`.forge/state.json`) and one level no longer reaches an existing
// dir, which silently broke the .forge/* self-protection glob (task-guard approved
// .forge/state.json writes). When no segment of path can be resolved,
// returns path unchanged, preserving the original fallback behavior on symlink-free systems.
//
// resolveSymlinks 对 path 求值 symlink。若 path 尚不存在（例如 PreToolUse
// Write 目标在文件创建之前），向上爬到最长已存在祖先目录、解析之、再把未存在的
// 尾部拼回——让深层新文件也能拿到 physical 前缀（macOS /var symlink；Windows
// 8.3 短名，EvalSymlinks 同时会把 ADMINI~1 展开为 Administrator）。爬一级在
// .forge/ 恒存在时够用，但 user-level-assets（零项目写入）后 .forge/ 通常不存在，
// 尾部有两段（`.forge/state.json`），一级上爬够不到已存在目录——曾静默击穿
// .forge/* 自保护 glob（task-guard 放行 .forge/state.json 写入）。当 path 上没有
// 任何可解析段时原样返回，保留无 symlink 系统上原有的 fallback 行为。
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	// Walk up to the longest existing ancestor, collecting the unresolved tail.
	//
	// 向上爬到最长已存在祖先，沿途收集未解析的尾部。
	var tail []string
	d := path
	for {
		parent := filepath.Dir(d)
		if parent == d {
			return path // 到卷根仍不可解析——原样返回
		}
		tail = append([]string{filepath.Base(d)}, tail...)
		d = parent
		if resolved, err := filepath.EvalSymlinks(d); err == nil {
			for _, seg := range tail {
				resolved = filepath.Join(resolved, seg)
			}
			return resolved
		}
	}
}

// Package hostcap is the host capability registry: a deliberately minimal LEAF
// package (stdlib-only, zero upward dependencies, same tier as agentsignals) where
// each supported host declaratively describes its identity signals and runtime
// capabilities. It exists because host differences were previously scattered as
// `agent == "kimi"`-style hardcodes across internal/cli (stdin normalize switch,
// output emitter switch, advisory promotion map, session-id env chains) — every new
// host had to be special-cased in N places, and any place missed became an
// attribution break (the kimi/cursor/reasonix session-attribution gaps of 2026-08).
//
// Phase 1 added the IDENTITY signals (which env var / stdin field carries the
// session id; which env var identifies the host in a shell). Phase 2 folds the
// runtime protocol knowledge in as additional declarative fields: ContextChannels
// (which events carry allow-path detail to the model), DroppedStdoutEvents,
// PromoteAdvisory, PatchToolName, StdinDialect and InstallIndicators. The behavior
// FUNCTIONS behind some of them (stdin normalizers, output emitters) stay in
// internal/cli — they are coupled to cli's HookInput/HookOutput protocol types and
// write os.Stdout directly, and hostcap cannot import cli without closing the
// import cycle — so cli keeps package-level map[string]func registries keyed by
// these fields and every host-name gate reads the registry instead of comparing
// literal names.
//
// Import direction: taskpipeline and cli may import hostcap; hostcap imports
// NOTHING above stdlib so it can never close the agentbridge→skillgen→taskpipeline
// cycle (see internal/agentsignals/agentsignals.go for the cycle rationale).
//
// The agent-name strings match agentbridge.AgentType verbatim, kept as plain
// strings (same convention as agentsignals) so this package never imports
// agentbridge.
//
// Package hostcap 是宿主能力注册表：刻意最小化的叶子包（仅依赖标准库、零向上依赖，
// 与 agentsignals 同级），每个受支持宿主以声明式结构描述其身份信号与运行时能力。
// 它存在的原因：宿主差异此前以 `agent == "kimi"` 式 hardcode 散落在 internal/cli
// 各处（stdin normalize switch、输出 emitter switch、advisory 提升 map、session-id
// env 链）——每接一个新宿主要在 N 处特判，漏任何一处就成为归因断点（2026-08 的
// kimi/cursor/reasonix 会话归因缺口）。
//
// 阶段 1 加入了身份信号（哪个 env 变量 / stdin 字段携带 session id；哪个 env 变量
// 能在 shell 里识别宿主）。阶段 2 把运行时协议知识作为附加声明式字段收编：
// ContextChannels（哪些事件能把 allow 路径 detail 送达模型）、DroppedStdoutEvents、
// PromoteAdvisory、PatchToolName、StdinDialect、InstallIndicators。其中部分能力背后
// 的行为**函数**（stdin normalizer、输出 emitter）仍留在 internal/cli——它们耦合
// cli 的 HookInput/HookOutput 协议类型且直接写 os.Stdout，hostcap import cli 会闭
// 合 import 环——故 cli 侧保留以这些字段为键的包级 map[string]func 注册表，所有
// 宿主名门控改为查注册表而非比较字面名。
package hostcap

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Channel describes whether an ALLOW-path detail emission on one hook event
// actually reaches the host's model context, plus a short channel label for
// checklog stamping (recordSkillTriggerHits) and usage-funnel analysis.
//
// Channel 描述某个 hook 事件上 allow 路径的 detail 输出是否真到达宿主的模型
// 上下文，并带一个简短通道标签，供 checklog 落章（recordSkillTriggerHits）与
// usage 漏斗分析。
type Channel struct {
	Delivered bool
	Label     string
}

// AdvisoryRule is the declarative form of one hook's "must-reach-the-model"
// advisory predicate: the detail must contain Contains, and must NOT contain
// Excludes (the exclusion separates a hook's success/clean branch — e.g.
// task-guard's "Auto-created task" — from its real advisory branch on the same
// hook name).
//
// AdvisoryRule 是单个 hook「必须送达模型」advisory 谓词的声明式形态：detail
// 须含 Contains，且不得含 Excludes（排除项把同一 hook 名下的成功/干净分支——
// 如 task-guard 的 "Auto-created task"——与真 advisory 分支隔开）。
type AdvisoryRule struct {
	Contains string
	Excludes string
}

// InstallIndicator describes one user-level install signal: the host's config
// home exists iff the tool is installed (user-level wiring is idempotent +
// machine-wide, so detecting the dir means "wire it").
//
// InstallIndicator 描述一个用户级安装信号：宿主的 config home 存在 = 该工具已
// 安装（用户级接线幂等 + 全机器生效，检测到目录即「接上它」）。
type InstallIndicator struct {
	// Env, when set in the process environment, overrides Path: the indicator
	// directory is Env's value joined with EnvSuffix (empty suffix = Env IS the
	// directory, e.g. CLAUDE_CONFIG_DIR/CODEX_HOME; opencode's XDG_CONFIG_HOME is
	// a BASE dir, so it carries EnvSuffix "opencode"). An env that is set but
	// points nowhere does NOT fall back to Path — same semantics as the
	// pre-registry detection.
	//
	// Env 在进程环境中被设置时覆盖 Path：指示目录为 Env 值 join EnvSuffix
	// （后缀为空 = Env 本身就是目录，如 CLAUDE_CONFIG_DIR/CODEX_HOME；opencode
	// 的 XDG_CONFIG_HOME 是**基**目录，故带 EnvSuffix "opencode"）。env 已设
	// 但指向不存在的位置时**不**回落 Path——与注册表之前的检测语义一致。
	Env       string
	EnvSuffix string

	// Path is the home-relative fallback ("~/.claude"); the "~/" prefix expands
	// to the user home dir at Resolve time.
	//
	// Path 是 home 相对的回落路径（"~/.claude"）；"~/" 前缀在 Resolve 时展开为
	// 用户 home 目录。
	Path string
}

// Resolve returns the absolute candidate directory for this indicator: Env
// (joined with EnvSuffix) when set, else Path with "~/" expanded to the user
// home. Returns "" when the home dir cannot be determined.
//
// Resolve 返回该指示的绝对候选目录：Env 已设时返回 Env（join EnvSuffix），
// 否则把 Path 的 "~/" 展开为用户 home。无法确定 home 目录时返回 ""。
func (i InstallIndicator) Resolve() string {
	if i.Env != "" {
		if v := os.Getenv(i.Env); v != "" {
			if i.EnvSuffix != "" {
				return filepath.Join(v, i.EnvSuffix)
			}
			return v
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, strings.TrimPrefix(i.Path, "~/"))
}

// Host describes one supported host's identity signals and runtime protocol
// capabilities. Pure data; the behavior functions keyed by some of these fields
// (stdin normalizers, output emitters) live in internal/cli's package-level
// registries (see the package doc for why they cannot move here).
//
// Host 描述一个受支持宿主的身份信号与运行时协议能力。纯数据；以其中部分字段
// 为键的行为函数（stdin normalizer、输出 emitter）住在 internal/cli 的包级
// 注册表里（为何不能迁来本包，见包文档）。
type Host struct {
	// Name matches agentbridge.AgentType verbatim ("claude-code", "kimi", ...).
	//
	// Name 与 agentbridge.AgentType 逐字一致（"claude-code"、"kimi"……）。
	Name string

	// ShellSessionEnv is the env var the host injects into every shell/tool
	// command it spawns, carrying the session id (claude-code:
	// CLAUDE_CODE_SESSION_ID). Empty for every other host — their shells carry
	// NO identity, which is precisely why the last-session pointer file
	// (taskpipeline.TouchLastSession / RecentHookSession) exists: it lets the
	// CLI side attribute a `forge task start` run inside a kimi/codex/... Bash
	// tool back to the host session that triggered it.
	//
	// ShellSessionEnv 是宿主注入其拉起的每条 shell/工具命令的 session id 环境
	// 变量（claude-code：CLAUDE_CODE_SESSION_ID）。其余宿主全为空——它们的
	// shell 不携带任何身份，这正是 last-session 指针文件
	// （taskpipeline.TouchLastSession / RecentHookSession）存在的原因：它让
	// CLI 侧能把 kimi/codex/... Bash 工具里跑的 `forge task start` 归回触发
	// 它的宿主会话。
	ShellSessionEnv string

	// StdinSessionFields lists the hook-stdin JSON fields that carry the session
	// id, in priority order. Most hosts send Claude-shape session_id; cursor's
	// tool/Stop/prompt events carry ONLY conversation_id (forge previously read
	// neither on those events → every cursor checklog/toollog entry landed on the
	// legacy global key); windsurf sends trajectory_id; cline sends taskId;
	// reasonix sends camelCase sessionId. The non-Claude-shape dialects are
	// mapped by their normalizers (cli/hook_normalize.go); this column is the
	// documented source field per host (the conversation_id fallback itself is
	// host-agnostic — cli/hook.go fills an empty SessionID from it for ANY
	// Claude-shape payload, no per-host branch needed).
	//
	// StdinSessionFields 按优先级列出 hook stdin 中携带 session id 的 JSON 字段。
	// 多数宿主发 Claude 形 session_id；cursor 的工具/Stop/prompt 事件只带
	// conversation_id（forge 此前在这些事件上两者都不读 → 每条 cursor 的
	// checklog/toollog 都落到 legacy 全局键）；windsurf 发 trajectory_id；
	// cline 发 taskId；reasonix 发 camelCase sessionId。非 Claude 形方言由各自
	// normalizer（cli/hook_normalize.go）映射；本列是各宿主来源字段的文档
	// （conversation_id 回落本身是宿主无关的——cli/hook.go 对任何 Claude 形
	// payload 都用它填空的 SessionID，无需按宿主分支）。
	StdinSessionFields []string

	// StdinDialect names the hook-stdin dialect: "" = Claude-shape (the default
	// unmarshal suffices). Non-empty selects the same-named normalizer in cli's
	// stdinNormalizers map (hook_normalize.go) — the keys there must match these
	// values.
	//
	// StdinDialect 命名 hook stdin 方言："" = Claude 形（默认 unmarshal 即可）。
	// 非空时选择 cli stdinNormalizers map（hook_normalize.go）里的同名
	// normalizer——该 map 的键必须与此处的值一致。
	StdinDialect string

	// StdinReplacesParse marks dialects whose payload cannot survive the default
	// unmarshal at all (kimi's UserPromptSubmit prompt is a content-block ARRAY,
	// which type-errors a plain unmarshal into HookInput): runHook runs the
	// normalizer INSTEAD of the default parse, and skips the post-parse
	// normalize pass. Other dialects normalize AFTER the default unmarshal
	// (fill-empty), so a Claude-shape payload is never clobbered.
	//
	// StdinReplacesParse 标记无法经受默认 unmarshal 的方言（kimi 的
	// UserPromptSubmit prompt 是 content-block **数组**，直接 unmarshal 进
	// HookInput 会类型错误）：runHook 用 normalizer **替代**默认解析，并跳过
	// 解析后的 normalize 阶段。其余方言在默认 unmarshal **之后**归一化（填空），
	// 故绝不覆盖 Claude 形 payload。
	StdinReplacesParse bool

	// ContextChannels maps hook event name → Channel for events whose allow-path
	// detail reaches the model. Events absent from the map fall back to
	// DefaultChannel. Rows cite their source emitter (emitXxxOutput in
	// cli/hook.go). Hosts with NEITHER field set (claude-code, codebuddy,
	// opencode, reasonix) take the Claude-compatible default row in
	// ContextChannel(): delivered on every event, "claude/additionalContext".
	//
	// ContextChannels 把 allow 路径 detail 能送达模型的 hook 事件名映射到
	// Channel。map 中缺失的事件回落 DefaultChannel。各行注明出处 emitter
	// （cli/hook.go 的 emitXxxOutput）。两个字段都未设的宿主（claude-code、
	// codebuddy、opencode、reasonix）在 ContextChannel() 里走 Claude 兼容默认
	// 行：全事件送达，"claude/additionalContext"。
	ContextChannels map[string]Channel

	// DefaultChannel is the fallback for events missing from ContextChannels
	// (windsurf: everything — no stdout JSON protocol at all; cline: everything
	// — contextModification injects on every fanned-out event).
	//
	// DefaultChannel 是 ContextChannels 缺失事件的回落（windsurf：所有事件——
	// 完全没有 stdout JSON 协议；cline：所有事件——contextModification 对每个
	// 扇出事件都注入）。
	DefaultChannel Channel

	// DroppedStdoutEvents lists hook events whose allow-path stdout the host
	// drops from the model context (kimi 0.35.0: PostToolUse/SessionStart/
	// PostCompact are observation-only, wire.jsonl-verified). Drives the
	// SessionStart handoff backfill (cli sessionStartOutputDropped), the stale
	// advisory's re-route onto the UserPromptSubmit-riding hook
	// (kimiStaleRidesHook), and skill-trigger's stdout print gate.
	//
	// DroppedStdoutEvents 列出 allow 路径 stdout 被宿主丢出模型上下文的 hook
	// 事件（kimi 0.35.0：PostToolUse/SessionStart/PostCompact 为
	// observation-only，wire.jsonl 实证）。驱动 SessionStart handoff 回填
	// （cli sessionStartOutputDropped）、stale advisory 改搭 UserPromptSubmit
	// 通道的 hook（kimiStaleRidesHook）、以及 skill-trigger 的 stdout 打印门控。
	DroppedStdoutEvents []string

	// PromoteAdvisory maps advisory hook name → rule for hosts whose allow-path
	// stdout never reaches the model: on such hosts the REAL advisory (isolated
	// from the hook's success/clean branch by the rule) is promoted to a block
	// (passed true→false → exit 2, stderr shown to the model). Nil for hosts
	// with a working advisory channel. cli keeps the FORGE_KIMI_ADVISORY=soft
	// escape hatch (an env knob, not a host capability).
	//
	// PromoteAdvisory 把 advisory hook 名映射到规则，面向 allow 路径 stdout 永远
	// 到不了模型的宿主：这类宿主上**真** advisory（由规则从该 hook 的成功/干净
	// 分支隔出）被提升为阻断（passed true→false → exit 2，stderr 展示给模型）。
	// 有可用 advisory 通道的宿主为 nil。FORGE_KIMI_ADVISORY=soft 逃生舱留在
	// cli（env 开关，非宿主能力）。
	PromoteAdvisory map[string]AdvisoryRule

	// PatchToolName names the host's patch-style edit tool whose tool_input
	// carries ONLY {command: <patch text>} — no file_path (codex: apply_patch).
	// Drives cli's read-before-edit exemption (the per-session reads log is
	// structurally empty for such hosts) and the file_path synthesis from the
	// patch header. The cli checks key on the TOOL name (not the agent) because
	// the hook stdin is Claude-shape and agent may be empty — IsPatchTool scans
	// this column.
	//
	// PatchToolName 命名宿主的 patch 式编辑工具：其 tool_input 只带
	// {command: <patch 文本>}——没有 file_path（codex：apply_patch）。驱动 cli
	// 的 read-before-edit 豁免（这类宿主的 per-session reads log 结构性为空）
	// 与从 patch 头合成 file_path。cli 的检查按**工具名**（而非 agent）触发，
	// 因为 hook stdin 是 Claude 形、agent 可能为空——IsPatchTool 扫描本列。
	PatchToolName string

	// InstallIndicators lists the user-level config dirs whose existence proves
	// the tool is installed (see InstallIndicator). Consumed by
	// agentbridge.DetectAgents; project-level markers live in agentsignals
	// (a different signal class, not duplicated here).
	//
	// InstallIndicators 列出其存在即证明工具已安装的用户级配置目录（见
	// InstallIndicator）。由 agentbridge.DetectAgents 消费；项目级标记住在
	// agentsignals（另一类信号，不在此重复）。
	InstallIndicators []InstallIndicator
}

// Hosts is the registry of all supported hosts, in no significant order (use
// Lookup). Identity rows verified 2026-08 against each translator and the kimi
// wire.jsonl routing study (internal/agentbridge/kimi-hook-routing.md).
//
// Hosts 是全部受支持宿主的注册表，顺序无意义（查表用 Lookup）。身份行于
// 2026-08 对照各 translator 与 kimi wire.jsonl 路由研究
// （internal/agentbridge/kimi-hook-routing.md）核实。
var Hosts = []Host{
	{
		Name: "claude-code", ShellSessionEnv: "CLAUDE_CODE_SESSION_ID", StdinSessionFields: []string{"session_id"},
		// CLAUDE_CONFIG_DIR overrides the whole dir (env-first convention shared
		// with hooks/plugin_detect.go); fallback ~/.claude.
		//
		// CLAUDE_CONFIG_DIR 覆盖整个目录（与 hooks/plugin_detect.go 共享的
		// env 优先约定）；回落 ~/.claude。
		InstallIndicators: []InstallIndicator{{Env: "CLAUDE_CONFIG_DIR", Path: "~/.claude"}},
	},
	{
		Name: "cursor", StdinSessionFields: []string{"session_id", "conversation_id"},
		// emitCursorOutput: top-level additional_context is read only on
		// PostToolUse/SessionStart.
		//
		// emitCursorOutput：顶层 additional_context 仅 PostToolUse/SessionStart 被读。
		ContextChannels: map[string]Channel{
			"PostToolUse":  {Delivered: true, Label: "cursor/additional_context"},
			"SessionStart": {Delivered: true, Label: "cursor/additional_context"},
		},
		DefaultChannel:    Channel{Delivered: false, Label: "cursor/no-channel"},
		InstallIndicators: []InstallIndicator{{Path: "~/.cursor"}},
	},
	{
		Name: "copilot", StdinSessionFields: []string{"session_id"},
		// emitCopilotOutput: camelCase additionalContext only on
		// sessionStart/postToolUse; userPromptSubmitted stdout is DROPPED for
		// command hooks.
		//
		// emitCopilotOutput：camelCase additionalContext 仅 sessionStart/
		// postToolUse；userPromptSubmitted 的 stdout 对 command hook 会被丢弃。
		ContextChannels: map[string]Channel{
			"PostToolUse":  {Delivered: true, Label: "copilot/additionalContext"},
			"SessionStart": {Delivered: true, Label: "copilot/additionalContext"},
		},
		DefaultChannel: Channel{Delivered: false, Label: "copilot/no-channel"},
	},
	{
		Name: "windsurf", StdinSessionFields: []string{"trajectory_id"},
		StdinDialect: "windsurf",
		// emitWindsurfOutput: no stdout JSON protocol at all — allow is silent
		// (show_output:false).
		//
		// emitWindsurfOutput：完全没有 stdout JSON 协议——allow 静默
		// （show_output:false）。
		DefaultChannel: Channel{Delivered: false, Label: "windsurf/no-context-channel"},
		// ~/.codeium holds windsurf/hooks.json and windsurf/memories/global_rules.md
		// (the paths WindsurfTranslator writes).
		//
		// ~/.codeium 下有 windsurf/hooks.json 与 windsurf/memories/global_rules.md
		// （即 WindsurfTranslator 写入的路径）。
		InstallIndicators: []InstallIndicator{{Path: "~/.codeium"}},
	},
	{
		Name: "codex", StdinSessionFields: []string{"session_id"},
		// emitCodexOutput: hookSpecificOutput.additionalContext honored on
		// SessionStart/PreToolUse/PostToolUse/UserPromptSubmit; Stop/PostCompact
		// have no context channel.
		//
		// emitCodexOutput：hookSpecificOutput.additionalContext 仅在 SessionStart/
		// PreToolUse/PostToolUse/UserPromptSubmit 被采纳；Stop/PostCompact 无上下文通道。
		ContextChannels: map[string]Channel{
			"SessionStart":     {Delivered: true, Label: "codex/hookSpecificOutput"},
			"PreToolUse":       {Delivered: true, Label: "codex/hookSpecificOutput"},
			"PostToolUse":      {Delivered: true, Label: "codex/hookSpecificOutput"},
			"UserPromptSubmit": {Delivered: true, Label: "codex/hookSpecificOutput"},
		},
		DefaultChannel: Channel{Delivered: false, Label: "codex/no-channel"},
		// codex reports file edits as tool_name "apply_patch" (single tool, patch
		// text in tool_input.command).
		//
		// codex 的文件编辑以 tool_name "apply_patch" 上报（单工具，patch 文本在
		// tool_input.command）。
		PatchToolName:     "apply_patch",
		InstallIndicators: []InstallIndicator{{Env: "CODEX_HOME", Path: "~/.codex"}},
	},
	{
		Name: "opencode", StdinSessionFields: []string{"session_id"},
		// XDG convention: $XDG_CONFIG_HOME is a BASE dir → EnvSuffix "opencode";
		// fallback ~/.config/opencode. Detection must use the same resolution as
		// OpenCodeConfigDir's write path, or XDG users get wired into a directory
		// opencode never reads.
		//
		// XDG 约定：$XDG_CONFIG_HOME 是**基**目录 → EnvSuffix "opencode"；回落
		// ~/.config/opencode。检测必须与 OpenCodeConfigDir 的写入路径同解析，
		// 否则 XDG 用户被接进 opencode 从不读的目录。
		InstallIndicators: []InstallIndicator{{Env: "XDG_CONFIG_HOME", EnvSuffix: "opencode", Path: "~/.config/opencode"}},
	},
	{
		Name: "cline", StdinSessionFields: []string{"taskId"},
		StdinDialect: "cline",
		// emitClineOutput: contextModification injects into the task on the allow
		// path (the wrapper forwards it for every fanned-out event).
		//
		// emitClineOutput：allow 路径的 contextModification 注入任务（wrapper 对
		// 每个扇出事件都转发）。
		DefaultChannel: Channel{Delivered: true, Label: "cline/contextModification"},
	},
	{
		Name: "kimi", StdinSessionFields: []string{"session_id"},
		// kimi's prompt field is a content-block array — the normalizer replaces
		// the default unmarshal entirely.
		//
		// kimi 的 prompt 字段是 content-block 数组——normalizer 完全替代默认
		// unmarshal。
		StdinDialect: "kimi", StdinReplacesParse: true,
		// emitKimiOutput prints detail to stdout; kimi only carries stdout into
		// model context on UserPromptSubmit (wire.jsonl-verified, see
		// internal/agentbridge/kimi-hook-routing.md).
		//
		// emitKimiOutput 把 detail 打 stdout；kimi 仅在 UserPromptSubmit 把
		// stdout 送进模型上下文（wire.jsonl 实证，见
		// internal/agentbridge/kimi-hook-routing.md）。
		ContextChannels: map[string]Channel{
			"UserPromptSubmit": {Delivered: true, Label: "kimi/stdout-UserPromptSubmit"},
		},
		DefaultChannel: Channel{Delivered: false, Label: "kimi/no-channel"},
		// kimi 0.35.0 drops allow-path stdout on these events (observation-only).
		//
		// kimi 0.35.0 在这些事件上丢弃 allow 路径 stdout（observation-only）。
		DroppedStdoutEvents: []string{"PostToolUse", "SessionStart", "PostCompact"},
		// kimi drops allow-path stdout from the model context, so these hooks'
		// real advisories promote to block (exit 2). The rules isolate the
		// advisory branch from each hook's success/clean branch: task-guard's
		// "Auto-created task" is a SUCCESS path (blocking it would hard-stop the
		// edit task-guard just enabled); assertion-check's clean branch carries
		// no "Advisory:" marker.
		//
		// kimi 丢弃 allow 路径 stdout，故这些 hook 的真 advisory 提升为阻断
		// （exit 2）。规则把 advisory 分支与各 hook 的成功/干净分支隔开：
		// task-guard 的 "Auto-created task" 是成功路径（阻断它会硬停
		// task-guard 刚放行的编辑）；assertion-check 的干净分支不带
		// "Advisory:" 标记。
		PromoteAdvisory: map[string]AdvisoryRule{
			"task-guard":      {Contains: "[task-guard]", Excludes: "Auto-created"},
			"bash-guard":      {Contains: "[bash-guard]"},
			"assertion-check": {Contains: "Advisory:"},
		},
	},
	{Name: "codebuddy", StdinSessionFields: []string{"session_id"}},
	{Name: "reasonix", StdinSessionFields: []string{"sessionId"}, StdinDialect: "reasonix"},
}

// Lookup returns the registry row for name, or nil when the host is unknown
// (callers treat unknown as the Claude-compatible default, mirroring the
// pre-registry switch defaults).
//
// Lookup 返回 name 对应的注册表行，宿主未知时返回 nil（调用方按 Claude 兼容
// 默认处理，与注册表之前的 switch default 语义一致）。
func Lookup(name string) *Host {
	for i := range Hosts {
		if Hosts[i].Name == name {
			return &Hosts[i]
		}
	}
	return nil
}

// ContextChannel reports whether an ALLOW-path detail emission on (hostName,
// event) actually reaches the model's context on that host, plus a short channel
// label. Hosts without channel rows and unknown hosts take the Claude-compatible
// default: emitClaudeOutput's bare hookSpecificOutput whose additionalContext
// Claude injects on every event (delivered, "claude/additionalContext").
//
// ContextChannel 报告 (hostName, event) 上 allow 路径的 detail 输出是否真到达
// 该宿主的模型上下文，并给出简短通道标签。无通道行的宿主与未知宿主走 Claude
// 兼容默认：emitClaudeOutput 的裸 hookSpecificOutput，其 additionalContext 在
// 每个事件上都被 Claude 注入（送达，"claude/additionalContext"）。
func ContextChannel(hostName, event string) (delivered bool, label string) {
	if h := Lookup(hostName); h != nil && (len(h.ContextChannels) > 0 || h.DefaultChannel.Label != "") {
		if ch, ok := h.ContextChannels[event]; ok {
			return ch.Delivered, ch.Label
		}
		return h.DefaultChannel.Delivered, h.DefaultChannel.Label
	}
	return true, "claude/additionalContext"
}

// ShouldPromoteAdvisory reports whether the given hook's detail qualifies as a
// "must-reach-the-model" advisory on this host per its PromoteAdvisory rules.
// Returns false for unknown hooks and hosts without promotion rules.
//
// ShouldPromoteAdvisory 按本宿主的 PromoteAdvisory 规则报告给定 hook 的
// detail 是否属于「必须送达模型」的 advisory。未知 hook 与无提升规则的宿主
// 返回 false。
func (h *Host) ShouldPromoteAdvisory(hookName, detail string) bool {
	rule, ok := h.PromoteAdvisory[hookName]
	if !ok {
		return false
	}
	if !strings.Contains(detail, rule.Contains) {
		return false
	}
	if rule.Excludes != "" && strings.Contains(detail, rule.Excludes) {
		return false
	}
	return true
}

// DropsStdoutEvent reports whether this host drops the given event's allow-path
// stdout from the model context.
//
// DropsStdoutEvent 报告本宿主是否把给定事件的 allow 路径 stdout 丢出模型上下文。
func (h *Host) DropsStdoutEvent(event string) bool {
	return slices.Contains(h.DroppedStdoutEvents, event)
}

// IsPatchTool reports whether toolName is any host's patch-style edit tool
// (PatchToolName column; codex: apply_patch). Callers key on the tool name
// because the hook stdin is Claude-shape and the agent may be empty.
//
// IsPatchTool 报告 toolName 是否为某宿主的 patch 式编辑工具（PatchToolName
// 列；codex：apply_patch）。调用方按工具名判断，因为 hook stdin 是 Claude 形、
// agent 可能为空。
func IsPatchTool(toolName string) bool {
	for _, h := range Hosts {
		if h.PatchToolName != "" && h.PatchToolName == toolName {
			return true
		}
	}
	return false
}

// InstallDir resolves the first install indicator for hostName ("" when the
// host is unknown, has no indicators, or the home dir cannot be determined).
// Single source for the env-first config-home conventions exported by
// agentbridge (ClaudeConfigHomeDir etc.).
//
// InstallDir 解析 hostName 的第一个安装指示（宿主未知、无指示、或无法确定
// home 目录时返回 ""）。是 agentbridge 导出的 env 优先 config-home 约定
// （ClaudeConfigHomeDir 等）的单一真相源。
func InstallDir(hostName string) string {
	h := Lookup(hostName)
	if h == nil || len(h.InstallIndicators) == 0 {
		return ""
	}
	return h.InstallIndicators[0].Resolve()
}

// ProbeShellIdentity inspects the process environment for a host-injected
// session id and returns (hostName, sessionID). Only hosts with a
// ShellSessionEnv can be found this way — today that is claude-code alone; the
// loop (not a hardcoded CLAUDE_CODE_SESSION_ID read) is the point: a host that
// starts injecting an env var needs a one-row registry change, not a code
// change in taskpipeline/cli. Returns ("", "") when no host env is present.
//
// ProbeShellIdentity 检查进程环境中的宿主注入 session id，返回 (宿主名,
// sessionID)。只有带 ShellSessionEnv 的宿主能这样被找到——目前仅
// claude-code；用循环（而非 hardcode 读 CLAUDE_CODE_SESSION_ID）正是意义所在：
// 某宿主开始注入 env 变量时只需改注册表一行，而非改 taskpipeline/cli 代码。
// 无宿主 env 时返回 ("", "")。
func ProbeShellIdentity() (host, sessionID string) {
	for _, h := range Hosts {
		if h.ShellSessionEnv == "" {
			continue
		}
		if sid := os.Getenv(h.ShellSessionEnv); sid != "" {
			return h.Name, sid
		}
	}
	return "", ""
}

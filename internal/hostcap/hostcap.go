// Package hostcap is the host-capability registry: a deliberately minimal leaf package with zero upward dependencies.
//
// Package hostcap 是宿主能力注册表：刻意最小化的叶子包（仅依赖标准库、零向上依赖，
// 与 agentsignals 同级），每个受支持宿主以声明式结构描述其身份信号与运行时能力。
// 它存在的原因：宿主差异此前以 `agent == "kimi"` 式 hardcode 散落在 internal/cli
// 各处（stdin normalize switch、输出 emitter switch、advisory 提升 map、session-id
// env 链）——每接一个新宿主要在 N 处特判，漏任何一处就成为归因断点（2026-08 的
// kimi/cursor/reasonix 会话归因缺口）。
// agent 名字符串与 agentbridge.AgentType 逐字一致，按纯字符串保留（与 agentsignals
// 同一约定），使本包永不 import agentbridge。
// import 方向：taskpipeline 与 cli 可 import hostcap；hostcap 不 import 标准库以外
// 任何包，故永不闭合 agentbridge→skillgen→taskpipeline 环（环的缘由见
// internal/agentsignals/agentsignals.go）。
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

// Channel describes whether an ALLOW-path detail emission on one hook event actually reaches the host's model context, plus a short channel label for checklog stamping (recordSkillTriggerHits) and usage-funnel analysis.
//
// Channel 描述某个 hook 事件上 allow 路径的 detail 输出是否真到达宿主的模型
// 上下文，并带一个简短通道标签，供 checklog 落章（recordSkillTriggerHits）与
// usage 漏斗分析。
type Channel struct {
	Delivered bool
	Label     string
}

// AdvisoryRule is the declarative form of one hook's "must-reach-the-model" advisory predicate: the detail must contain Contains, and must NOT contain Excludes (the exclusion separates a hook's success/clean branch — e.g. task-guard's "Auto-created task" — from its real advisory branch on the same hook name).
//
// AdvisoryRule 是单个 hook「必须送达模型」advisory 谓词的声明式形态：detail
// 须含 Contains，且不得含 Excludes（排除项把同一 hook 名下的成功/干净分支——
// 如 task-guard 的 "Auto-created task"——与真 advisory 分支隔开）。
type AdvisoryRule struct {
	Contains string
	Excludes string
}

// InstallIndicator describes one user-level install signal: the host's config home exists iff the tool is installed (user-level wiring is idempotent + machine-wide, so detecting the dir means "wire it").
//
// InstallIndicator 描述一个用户级安装信号：宿主的 config home 存在 = 该工具已
// 安装（用户级接线幂等 + 全机器生效，检测到目录即「接上它」）。
type InstallIndicator struct {
	// Env, when set in the process environment, overrides Path: the indicator directory is Env's value joined with EnvSuffix (empty suffix = Env IS the directory, e.g. CLAUDE_CONFIG_DIR/CODEX_HOME; opencode's XDG_CONFIG_HOME is a BASE dir, so it carries EnvSuffix "opencode").
	//
	// Env 在进程环境中被设置时覆盖 Path：指示目录为 Env 值 join EnvSuffix
	// （后缀为空 = Env 本身就是目录，如 CLAUDE_CONFIG_DIR/CODEX_HOME；opencode
	// 的 XDG_CONFIG_HOME 是**基**目录，故带 EnvSuffix "opencode"）。env 已设
	// 但指向不存在的位置时**不**回落 Path——与注册表之前的检测语义一致。
	Env       string
	EnvSuffix string

	// Path is the home-relative fallback ("~/.claude"); the "~/" prefix expands to the user home dir at Resolve time.
	//
	// Path 是 home 相对的回落路径（"~/.claude"）；"~/" 前缀在 Resolve 时展开为
	// 用户 home 目录。
	Path string
}

// Resolve returns the absolute candidate directory for this indicator: Env (joined with EnvSuffix) when set, else Path with "~/" expanded to the user home.
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

// Host describes one supported host's identity signals and runtime protocol capabilities.
//
// Host 描述一个受支持宿主的身份信号与运行时协议能力。纯数据；以其中部分字段
// 为键的行为函数（stdin normalizer、输出 emitter）住在 internal/cli 的包级
// 注册表里（为何不能迁来本包，见包文档）。
type Host struct {
	// Name matches agentbridge.AgentType verbatim ("claude-code", "kimi", ...).
	//
	// Name 与 agentbridge.AgentType 逐字一致（"claude-code"、"kimi"……）。
	Name string

	// ShellSessionEnv is the env var the host injects into every shell/tool command it spawns, carrying the session id (claude-code: CLAUDE_CODE_SESSION_ID).
	//
	// ShellSessionEnv 是宿主注入其拉起的每条 shell/工具命令的 session id 环境
	// 变量（claude-code：CLAUDE_CODE_SESSION_ID）。其余宿主全为空——它们的
	// shell 不携带任何身份，这正是 last-session 指针文件
	// （taskpipeline.TouchLastSession / RecentHookSession）存在的原因：它让
	// CLI 侧能把 kimi/codex/... Bash 工具里跑的 `forge task start` 归回触发
	// 它的宿主会话。
	ShellSessionEnv string

	// StdinSessionFields lists the hook-stdin JSON fields that carry the session id, in priority order.
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

	// StdinDialect names the hook-stdin dialect: "" = Claude-shape (the default unmarshal suffices).
	//
	// StdinDialect 命名 hook stdin 方言："" = Claude 形（默认 unmarshal 即可）。
	// 非空时选择 cli stdinNormalizers map（hook_normalize.go）里的同名
	// normalizer——该 map 的键必须与此处的值一致。
	StdinDialect string

	// StdinReplacesParse marks dialects whose payload cannot survive the default unmarshal at all (kimi's UserPromptSubmit prompt is a content-block ARRAY, which type-errors a plain unmarshal into HookInput): runHook runs the normalizer INSTEAD of the default parse, and skips the post-parse normalize pass.
	//
	// StdinReplacesParse 标记无法经受默认 unmarshal 的方言（kimi 的
	// UserPromptSubmit prompt 是 content-block **数组**，直接 unmarshal 进
	// HookInput 会类型错误）：runHook 用 normalizer **替代**默认解析，并跳过
	// 解析后的 normalize 阶段。其余方言在默认 unmarshal **之后**归一化（填空），
	// 故绝不覆盖 Claude 形 payload。
	StdinReplacesParse bool

	// ContextChannels maps hook event name → Channel for events whose allow-path detail reaches the model.
	//
	// ContextChannels 把 allow 路径 detail 能送达模型的 hook 事件名映射到
	// Channel。map 中缺失的事件回落 DefaultChannel。各行注明出处 emitter
	// （cli/hook.go 的 emitXxxOutput）。两个字段都未设的宿主（claude-code、
	// codebuddy、opencode、reasonix）在 ContextChannel() 里走 Claude 兼容默认
	// 行：全事件送达，"claude/additionalContext"。
	ContextChannels map[string]Channel

	// DefaultChannel is the fallback for events missing from ContextChannels (windsurf: everything — no stdout JSON protocol at all; cline: everything — contextModification injects on every fanned-out event).
	//
	// DefaultChannel 是 ContextChannels 缺失事件的回落（windsurf：所有事件——
	// 完全没有 stdout JSON 协议；cline：所有事件——contextModification 对每个
	// 扇出事件都注入）。
	DefaultChannel Channel

	// DroppedStdoutEvents lists hook events whose allow-path stdout the host drops from the model context (kimi 0.35.0: PostToolUse/SessionStart/ PostCompact are observation-only, wire.jsonl-verified).
	//
	// DroppedStdoutEvents 列出 allow 路径 stdout 被宿主丢出模型上下文的 hook
	// 事件（kimi 0.35.0：PostToolUse/SessionStart/PostCompact 为
	// observation-only，wire.jsonl 实证）。驱动 SessionStart handoff 回填
	// （cli sessionStartOutputDropped）、stale advisory 改搭 UserPromptSubmit
	// 通道的 hook（kimiStaleRidesHook）、以及 skill-trigger 的 stdout 打印门控。
	DroppedStdoutEvents []string

	// PromoteAdvisory maps advisory hook name → rule for hosts where an advisory does not constrain behavior, so the REAL advisory (isolated from the hook's success/clean branch by the rule) is promoted to a block (passed true→false → exit 2, stderr shown to the model).
	//
	// PromoteAdvisory 把 advisory hook 名映射到规则，面向 advisory 不构成行为
	// 约束的宿主：**真** advisory（由规则从该 hook 的成功/干净分支隔出）被提升
	// 为阻断（passed true→false → exit 2，stderr 展示给模型）。准入路径有二：
	// (a) advisory 通道物理性失效；(b) 通道送达但 advisory 被实证无视——dsh
	// 2026-08-22：task-guard 的 WARN 经 agent.inject 到达了模型 inbox，但其文案
	// 自述「allowed」（状态通知而非指令），下一个动作就是继续 edit，且所有下游
	// 门禁都 task-scoped（无任务 ⇒ 静默通过）。通道送达且 advisory 足够的宿主
	// 为 nil——kimi 也是 nil：其路径 (a) 提升已于 2026-08-24 **退役**——reason
	// 自述「allowed」的 deny 自相矛盾（且 kimi 把 PreToolUse 上**任何** stdout
	// 都当 deny，诚实的 advisory 文案也走不了 allow 路径）。kimi 的 advisory 改为
	// 按项目入队、UserPromptSubmit 时攒发（cli hook_kimi_advisory.go）。逃生舱
	// FORGE_KIMI_ADVISORY / FORGE_ADVISORY_PROMOTION 留在 cli（env 开关，非宿主能力）。
	PromoteAdvisory map[string]AdvisoryRule

	// PatchToolName names the host's patch-style edit tool whose tool_input carries ONLY {command: <patch text>} — no file_path (codex: apply_patch).
	//
	// PatchToolName 命名宿主的 patch 式编辑工具：其 tool_input 只带
	// {command: <patch 文本>}——没有 file_path（codex：apply_patch）。驱动 cli
	// 的 read-before-edit 豁免（这类宿主的 per-session reads log 结构性为空）
	// 与从 patch 头合成 file_path。cli 的检查按**工具名**（而非 agent）触发，
	// 因为 hook stdin 是 Claude 形、agent 可能为空——IsPatchTool 扫描本列。
	PatchToolName string

	// InstallIndicators lists the user-level config dirs whose existence proves the tool is installed (see InstallIndicator).
	//
	// InstallIndicators 列出其存在即证明工具已安装的用户级配置目录（见
	// InstallIndicator）。由 agentbridge.DetectAgents 消费；项目级标记住在
	// agentsignals（另一类信号，不在此重复）。
	InstallIndicators []InstallIndicator
}

// Hosts 是全部受支持宿主的注册表，顺序无意义（查表用 Lookup）。身份行于
// 2026-08 对照各 translator 与 kimi wire.jsonl 路由研究
// （internal/agentbridge/kimi-hook-routing.md）核实。
var Hosts = []Host{
	{
		Name: "claude-code", ShellSessionEnv: "CLAUDE_CODE_SESSION_ID", StdinSessionFields: []string{"session_id"},
		// CLAUDE_CONFIG_DIR 覆盖整个目录（与 hooks/plugin_detect.go 共享的
		// env 优先约定）；回落 ~/.claude。
		InstallIndicators: []InstallIndicator{{Env: "CLAUDE_CONFIG_DIR", Path: "~/.claude"}},
	},
	{
		Name: "cursor", StdinSessionFields: []string{"session_id", "conversation_id"},
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
		// emitWindsurfOutput：完全没有 stdout JSON 协议——allow 静默
		// （show_output:false）。
		DefaultChannel: Channel{Delivered: false, Label: "windsurf/no-context-channel"},
		// ~/.codeium 下有 windsurf/hooks.json 与 windsurf/memories/global_rules.md
		// （即 WindsurfTranslator 写入的路径）。
		InstallIndicators: []InstallIndicator{{Path: "~/.codeium"}},
	},
	{
		Name: "codex", StdinSessionFields: []string{"session_id"},
		// emitCodexOutput：hookSpecificOutput.additionalContext 仅在 SessionStart/
		// PreToolUse/PostToolUse/UserPromptSubmit 被采纳；Stop/PostCompact 无上下文通道。
		ContextChannels: map[string]Channel{
			"SessionStart":     {Delivered: true, Label: "codex/hookSpecificOutput"},
			"PreToolUse":       {Delivered: true, Label: "codex/hookSpecificOutput"},
			"PostToolUse":      {Delivered: true, Label: "codex/hookSpecificOutput"},
			"UserPromptSubmit": {Delivered: true, Label: "codex/hookSpecificOutput"},
		},
		DefaultChannel: Channel{Delivered: false, Label: "codex/no-channel"},
		// codex 的文件编辑以 tool_name "apply_patch" 上报（单工具，patch 文本在
		// tool_input.command）。
		PatchToolName:     "apply_patch",
		InstallIndicators: []InstallIndicator{{Env: "CODEX_HOME", Path: "~/.codex"}},
	},
	{
		Name: "opencode", StdinSessionFields: []string{"session_id"},
		// XDG 约定：$XDG_CONFIG_HOME 是**基**目录 → EnvSuffix "opencode"；回落
		// ~/.config/opencode。检测必须与 OpenCodeConfigDir 的写入路径同解析，
		// 否则 XDG 用户被接进 opencode 从不读的目录。
		InstallIndicators: []InstallIndicator{{Env: "XDG_CONFIG_HOME", EnvSuffix: "opencode", Path: "~/.config/opencode"}},
	},
	{
		Name: "cline", StdinSessionFields: []string{"taskId"},
		StdinDialect: "cline",
		// emitClineOutput：allow 路径的 contextModification 注入任务（wrapper 对
		// 每个扇出事件都转发）。
		DefaultChannel: Channel{Delivered: true, Label: "cline/contextModification"},
	},
	{
		Name: "kimi", StdinSessionFields: []string{"session_id"},
		// kimi 的 prompt 字段是 content-block 数组——normalizer 完全替代默认
		// unmarshal。
		StdinDialect: "kimi", StdinReplacesParse: true,
		// emitKimiOutput 把 detail 打 stdout；kimi 仅在 UserPromptSubmit 把
		// stdout 送进模型上下文（wire.jsonl 实证，见
		// internal/agentbridge/kimi-hook-routing.md）。
		ContextChannels: map[string]Channel{
			"UserPromptSubmit": {Delivered: true, Label: "kimi/stdout-UserPromptSubmit"},
		},
		DefaultChannel: Channel{Delivered: false, Label: "kimi/no-channel"},
		// kimi 0.35.0 在这些事件上丢弃 allow 路径 stdout（observation-only）。
		DroppedStdoutEvents: []string{"PostToolUse", "SessionStart", "PostCompact"},
		// 不设 PromoteAdvisory 规则（2026-08-24 退役）：P0 提升把
		// task-guard/bash-guard/assertion-check 的 advisory 变成 exit-2 deny，
		// 而 reason 自述「allowed」——自相矛盾，且编辑被实际拦截、直到盲重试才发现。
		// 生产 checklog 显示 kimi/no-channel 的 advisory 100% 丢失。advisory 现改为
		// 按项目入队、在 UserPromptSubmit（上方唯一送达通道）攒成**一条**注入——见
		// cli hook_kimi_advisory.go。阻断类 hook（read-before-edit、hazard-guard、
		// freeze-guard）仍走 exit 2 deny——那是设计内的阻断。
	},
	{Name: "codebuddy", StdinSessionFields: []string{"session_id"}},
	{Name: "reasonix", StdinSessionFields: []string{"sessionId"}, StdinDialect: "reasonix"},
	{
		Name: "dsh", StdinSessionFields: []string{"session_id"},
		// plugins/forge-dsh 的 Cordis 包装层在进程内构造 Claude 形 stdin
		// （forge_agent:"dsh"），无需 StdinDialect——与 opencode 同为 code-based
		// 形态。allow 路径上下文在每个已接事件上都能到达模型：包装层把
		// additionalContext 折进 DSH decision（post-execute 的
		// additionalContexts、pre-step 的 enter messages）或经 agent.inject
		// 排队（pre-execute / session-start / turn-stopping）；PostCompact 经
		// session-start 的 source=compact 触发。
		ContextChannels: map[string]Channel{
			"PreToolUse":       {Delivered: true, Label: "dsh/agent.inject"},
			"PostToolUse":      {Delivered: true, Label: "dsh/additionalContexts"},
			"UserPromptSubmit": {Delivered: true, Label: "dsh/enter-messages"},
			"SessionStart":     {Delivered: true, Label: "dsh/agent.inject"},
			"Stop":             {Delivered: true, Label: "dsh/agent.inject"},
			"PostCompact":      {Delivered: true, Label: "dsh/agent.inject"},
		},
		DefaultChannel: Channel{Delivered: false, Label: "dsh/unwired-event"},
		// dsh 的 advisory 通道**送达**（agent.inject——2026-08-22 事件里 WARN 到达了
		// 模型 inbox），却被实证无视：WARN 文案自述「allowed」，下一个动作就是继续
		// edit，且所有下游门禁都 task-scoped（无任务 ⇒ 静默通过）。属 PromoteAdvisory
		// 字段文档的准入路径 (b)——为执法硬度而非通道物理。范围：仅 task-guard；事件
		// 类别是「main 上无任务改源码」。bash-guard 的后果链在 dsh 上仍然有效
		// （file-sentinel 会 quarantine 无任务的 Bash 写文件），assertion-check 的
		// advisory 未显现同样的无视模式。
		PromoteAdvisory: map[string]AdvisoryRule{
			"task-guard": {Contains: "[task-guard]", Excludes: "Auto-created"},
		},
		// DSH_HOME 覆盖整个 home（社区 resolveDshHome 约定：env ?? ~/.dsh）；
		// 回落 ~/.dsh。
		InstallIndicators: []InstallIndicator{{Env: "DSH_HOME", Path: "~/.dsh"}},
	},
	{
		Name: "zcode", StdinSessionFields: []string{"session_id"},
		// ZCode（z.ai 的 agentic IDE）在 hook 层**刻意**与 Claude 兼容
		// （zcode.z.ai/en/docs/hooks，2026-08）：stdin 在 camelCase 字段旁携带
		// Claude 蛇形别名（session_id/tool_name/tool_input），stdout 在每个已接
		// 事件上读 hookSpecificOutput.additionalContext，exit 2 是阻断捷径——
		// 故不要 StdinDialect、不进 outputEmitters、不设 ContextChannels 行：
		// Claude 兼容默认（全事件送达，"claude/additionalContext"）是按文档
		// 诚实的路由。文档结论、未经 wire 验证——kimi-hook-routing.md 的教训
		// （文档 ≠ 线上行为）适用：信任 advisory 送达前应在真实 ZCode 安装上
		// 复查通道。ZCode 独有的执法差异随接线侧记录（agentbridge/zcode.go）：
		// 无 PostCompact/SubagentStop 事件，Stop 阻断连续 3 轮后强制结束。
		//
		// ~/.zcode 当且仅当桌面端跑过才存在；ZCode 未文档化 config home 的
		// env 覆盖。
		InstallIndicators: []InstallIndicator{{Path: "~/.zcode"}},
	},
}

// Lookup returns the registry row for name, or nil when the host is unknown (callers treat unknown as the Claude-compatible default, mirroring the pre-registry switch defaults).
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

// ContextChannel reports whether an ALLOW-path detail emission on (hostName, event) actually reaches the model's context on that host, plus a short channel label.
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

// ShouldPromoteAdvisory reports whether the given hook's detail qualifies as a "must-reach-the-model" advisory on this host per its PromoteAdvisory rules.
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

// PromotesHook reports whether this host carries ANY promotion rule for hookName (rule existence, detail-independent).
//
// PromotesHook 报告本宿主是否为 hookName 携带**任一**提升规则（规则存在性，
// 与 detail 无关）。须在具体 detail 尚不存在前配置 hook 行为的调用方用它而非
// ShouldPromoteAdvisory——cli 据此设置 FORGE_TASKGUARD_PROMOTED，让 task-guard
// 脚本在提升宿主上放弃每会话一次的去噪（模型盲重试即可绕过的 deny——NOWARN
// 标记让第二次相同 edit 静默放行——算不上执法）。
func (h *Host) PromotesHook(hookName string) bool {
	_, ok := h.PromoteAdvisory[hookName]
	return ok
}

// DropsStdoutEvent reports whether this host drops the given event's allow-path stdout from the model context.
//
// DropsStdoutEvent 报告本宿主是否把给定事件的 allow 路径 stdout 丢出模型上下文。
func (h *Host) DropsStdoutEvent(event string) bool {
	return slices.Contains(h.DroppedStdoutEvents, event)
}

// IsPatchTool reports whether toolName is any host's patch-style edit tool (PatchToolName column; codex: apply_patch).
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

// InstallDir resolves the first install indicator for hostName ("" when the host is unknown, has no indicators, or the home dir cannot be determined).
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

// ProbeShellIdentity inspects the process environment for a host-injected session id and returns (hostName, sessionID).
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

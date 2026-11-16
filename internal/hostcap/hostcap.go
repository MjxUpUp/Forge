// Package hostcap is the host-capability registry: a deliberately minimal leaf package with zero upward dependencies.
//
// Package hostcap 是宿主能力注册表：刻意最小化的叶子包（标准库之外零依赖，与
// agentsignals 同级），每个受支持宿主以声明式结构描述身份信号与运行时能力。
// 宿主差异集中在这里，消费侧用查表代替 `agent == "kimi"` 式特判，避免每接一个
// 新宿主就在多处判断、漏一处即成为归因断点。
//
// 身份信号回答两个问题：哪个 env 变量或 stdin 字段携带 session id（StdinSessionFields、
// ShellSessionEnv），以及哪个 env 变量能在 shell 里识别宿主。运行时协议以声明式
// 字段表达：ContextChannels（哪些事件能把 detail 送达模型）、DroppedStdoutEvents、
// PromoteAdvisory、PatchToolName、StdinDialect、InstallIndicators。
//
// 宿主名以纯字符串保存（"claude-code"、"cursor"…），与 agentsignals 同一约定；
// 本包只依赖标准库，被 taskpipeline、clitask 与 cli 导入，反向依赖不允许。
//
// # 接新宿主清单（ONBOARDING）
//
// 每一行对应一个机械执法点（依据：docs/design/hostcap-behavior-registry.md）：
// 新增 Host 时同步本注册表、本包守卫测试与 README 多 agent 支持表，并重钉
// forge compat snapshot（compat.snapshot.json 的变更进 PR diff 显式审阅）。
package hostcap

import (
	"os"
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

// Host describes one supported host's identity signals and runtime protocol capabilities.
//
// Host 描述一个受支持宿主的身份信号与运行时协议能力。纯数据；以其中部分字段
// 为键的行为函数（stdin normalizer、输出 emitter）住在 internal/cli 的包级
// 注册表里（为何不能迁来本包，见包文档）。
type Host struct {
	// Name 是宿主标识（"claude-code"、"kimi"……），与 agentsignals 的字符串约定一致。
	//
	// Name 是宿主标识（"claude-code"、"kimi"……）。
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
	// conversation_id；windsurf 发 trajectory_id；cline 发 taskId；reasonix 发
	// camelCase sessionId。本列是各宿主来源字段的文档，按宿主分支读取。
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
	// Channel。map 中缺失的事件回落 DefaultChannel。两个字段都未设的宿主
	// （claude-code、codebuddy、opencode、reasonix）在 ContextChannel() 里走
	// Claude 兼容默认：全事件送达，"claude/additionalContext"。
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
	// observation-only，wire.jsonl 实证）；消费方据此避免在这些事件上依赖 stdout。
	DroppedStdoutEvents []string

	// PromoteAdvisory maps advisory hook name → rule for hosts where an advisory does not constrain behavior, so the REAL advisory (isolated from the hook's success/clean branch by the rule) is promoted to a block (passed true→false → exit 2, stderr shown to the model).
	//
	// PromoteAdvisory 把 advisory hook 名映射到规则，面向 advisory 不构成行为
	// 约束的宿主：规则把**真** advisory 从成功/干净分支隔出并提升为阻断
	// （passed true→false → exit 2，stderr 展示给模型）。准入路径二选一：
	// (a) advisory 通道物理性失效；(b) 通道送达但被实证无视（文案自述「allowed」
	// 时模型当成状态通知而继续动作）。通道送达且 advisory 足够的宿主为 nil。
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
	// InstallIndicator）。项目级标记住在
	// agentsignals（另一类信号，不在此重复）。
	InstallIndicators []InstallIndicator
}

// Hosts 是全部受支持宿主的注册表，顺序无意义（查表用 Lookup）。身份行
// 对照各宿主的文档与实测路由核实。
var Hosts = []Host{
	{
		Name: "claude-code", ShellSessionEnv: "CLAUDE_CODE_SESSION_ID", StdinSessionFields: []string{"session_id"},
		// CLAUDE_CONFIG_DIR 覆盖整个目录（env 优先，缺省回落 ~/.claude）。
		InstallIndicators: []InstallIndicator{{Env: "CLAUDE_CONFIG_DIR", Path: "~/.claude"}},
	},
	{
		Name: "cursor", StdinSessionFields: []string{"session_id", "conversation_id"},
		// cursor 的顶层 additional_context 仅 PostToolUse/SessionStart 被读取。
		ContextChannels: map[string]Channel{
			"PostToolUse":  {Delivered: true, Label: "cursor/additional_context"},
			"SessionStart": {Delivered: true, Label: "cursor/additional_context"},
		},
		DefaultChannel:    Channel{Delivered: false, Label: "cursor/no-channel"},
		InstallIndicators: []InstallIndicator{{Path: "~/.cursor"}},
	},
	{
		Name: "copilot", StdinSessionFields: []string{"session_id"},
		// copilot 的 camelCase additionalContext 仅 sessionStart/
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
		// windsurf 没有 stdout JSON 协议——allow 静默
		// （show_output:false）。
		DefaultChannel: Channel{Delivered: false, Label: "windsurf/no-context-channel"},
		// ~/.codeium 下有 windsurf/hooks.json 与 windsurf/memories/global_rules.md
		// （即 WindsurfTranslator 写入的路径）。
		InstallIndicators: []InstallIndicator{{Path: "~/.codeium"}},
	},
	{
		Name: "codex", StdinSessionFields: []string{"session_id"},
		// codex 的 hookSpecificOutput.additionalContext 仅在 SessionStart/
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
		// cline 的 allow 路径走 contextModification 注入（wrapper 对
		// 每个扇出事件都转发）。
		DefaultChannel: Channel{Delivered: true, Label: "cline/contextModification"},
	},
	{
		Name: "kimi", StdinSessionFields: []string{"session_id"},
		// kimi 的 prompt 字段是 content-block 数组——normalizer 完全替代默认
		// unmarshal。
		StdinDialect: "kimi", StdinReplacesParse: true,
		// kimi 把 detail 打 stdout；仅 UserPromptSubmit 会把
		// stdout 送进模型上下文（wire.jsonl 实证）。
		ContextChannels: map[string]Channel{
			"UserPromptSubmit": {Delivered: true, Label: "kimi/stdout-UserPromptSubmit"},
		},
		DefaultChannel: Channel{Delivered: false, Label: "kimi/no-channel"},
		// kimi 0.35.0 在这些事件上丢弃 allow 路径 stdout（observation-only）。
		DroppedStdoutEvents: []string{"PostToolUse", "SessionStart", "PostCompact"},
		// 不设 PromoteAdvisory 规则：kimi 在 exit 2 之外的 stdout 通道只在
		// UserPromptSubmit 送达，提升为 deny 的 advisory 反而自相矛盾。
		// kimi 上的 advisory 在 UserPromptSubmit（上方唯一送达通道）攒成一条注入。
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
		// 诚实的路由。ZCode 独有的执法差异：
		// 无 PostCompact/SubagentStop 事件，Stop 阻断连续 3 轮后强制结束。
		//
		// 2026-08-31 wire 实证补记：通道已验证送达（model-io 转录中 WARN 经
		// additionalContext 附着于工具结果进入模型上下文），但 2026-08-30 事故
		// 实证 advisory 被无视——其文案自述「allowed」（许可声明而非指令），
		// agent 读后继续编辑，且所有下游门禁 task-scoped（无任务 ⇒ 静默通过）。
		// 与 dsh 2026-08-22 同款失效、同属 PromoteAdvisory 准入路径 (b)（通道
		// 送达但被实证无视），故入列。范围仅 task-guard 的「无任务改源码」事件
		// 类别（与 dsh 同一条规则）；bash-guard 的 quarantine 后果链在 zcode 上
		// 仍然有效。取证：~/.zcode/cli/rollout/model-io-sess_8647540f*.jsonl
		// （四层穿透分析，2026-08-31 会话）。
		//
		// ~/.zcode 当且仅当桌面端跑过才存在；ZCode 未文档化 config home 的
		// env 覆盖。
		PromoteAdvisory: map[string]AdvisoryRule{
			"task-guard": {Contains: "[task-guard]", Excludes: "Auto-created"},
		},
		InstallIndicators: []InstallIndicator{{Path: "~/.zcode"}},
	},
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

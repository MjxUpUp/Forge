// Package hostcap is the host capability registry: a deliberately minimal LEAF
// package (stdlib-only, zero upward dependencies, same tier as agentsignals) where
// each supported host declaratively describes its identity signals and runtime
// capabilities. It exists because host differences were previously scattered as
// `agent == "kimi"`-style hardcodes across internal/cli (stdin normalize switch,
// output emitter switch, advisory promotion map, session-id env chains) — every new
// host had to be special-cased in N places, and any place missed became an
// attribution break (the kimi/cursor/reasonix session-attribution gaps of 2026-08).
//
// Phase 1 holds only the IDENTITY signals (which env var / stdin field carries the
// session id; which env var identifies the host in a shell) — the data the
// attribution chain (taskpipeline session registration, cli detectOriginTool)
// consults. Phase 2 folds the runtime protocol switches (NormalizeStdin /
// EmitOutput / ContextChannel / AdvisoryPolicy) in as additional fields.
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
// 阶段 1 只持有身份信号（哪个 env 变量 / stdin 字段携带 session id；哪个 env 变量
// 能在 shell 里识别宿主）——归因链（taskpipeline 会话注册、cli detectOriginTool）
// 查的数据。阶段 2 把运行时协议 switch（NormalizeStdin / EmitOutput /
// ContextChannel / AdvisoryPolicy）作为附加字段收编。
package hostcap

import "os"

// Host describes one supported host's identity signals. Pure data; behavior
// functions arrive with the phase-2 migration of the cli switches.
//
// Host 描述一个受支持宿主的身份信号。纯数据；行为函数随阶段 2 的 cli switch
// 迁移到来。
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
	// mapped by their normalizers (cli/hook_normalize.go); this column documents
	// the source field per host and drives the conversation_id fallback for
	// Claude-shape hosts.
	//
	// StdinSessionFields 按优先级列出 hook stdin 中携带 session id 的 JSON 字段。
	// 多数宿主发 Claude 形 session_id；cursor 的工具/Stop/prompt 事件只带
	// conversation_id（forge 此前在这些事件上两者都不读 → 每条 cursor 的
	// checklog/toollog 都落到 legacy 全局键）；windsurf 发 trajectory_id；
	// cline 发 taskId；reasonix 发 camelCase sessionId。非 Claude 形方言由各自
	// normalizer（cli/hook_normalize.go）映射；本列记录各宿主的来源字段，并驱
	// 动 Claude 形宿主的 conversation_id 回落。
	StdinSessionFields []string
}

// Hosts is the registry of all supported hosts, in no significant order (use
// Lookup). Identity rows verified 2026-08 against each translator and the kimi
// wire.jsonl routing study (internal/agentbridge/kimi-hook-routing.md).
//
// Hosts 是全部受支持宿主的注册表，顺序无意义（查表用 Lookup）。身份行于
// 2026-08 对照各 translator 与 kimi wire.jsonl 路由研究
// （internal/agentbridge/kimi-hook-routing.md）核实。
var Hosts = []Host{
	{Name: "claude-code", ShellSessionEnv: "CLAUDE_CODE_SESSION_ID", StdinSessionFields: []string{"session_id"}},
	{Name: "cursor", StdinSessionFields: []string{"session_id", "conversation_id"}},
	{Name: "copilot", StdinSessionFields: []string{"session_id"}},
	{Name: "windsurf", StdinSessionFields: []string{"trajectory_id"}},
	{Name: "codex", StdinSessionFields: []string{"session_id"}},
	{Name: "opencode", StdinSessionFields: []string{"session_id"}},
	{Name: "cline", StdinSessionFields: []string{"taskId"}},
	{Name: "kimi", StdinSessionFields: []string{"session_id"}},
	{Name: "codebuddy", StdinSessionFields: []string{"session_id"}},
	{Name: "reasonix", StdinSessionFields: []string{"sessionId"}},
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

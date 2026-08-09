package agentsignals

import (
	"os"
	"path/filepath"
)

// agentsignals is a deliberately minimal LEAF package: it holds the project-level
// agent-marker table and helpers, depends only on the standard library (os/filepath),
// and carries NO upward dependency on agentbridge, skillgen, or taskpipeline.
//
// Why it exists: agentbridge already imports skillgen, and skillgen already imports
// taskpipeline (agentbridge→skillgen→taskpipeline). taskpipeline therefore cannot
// import agentbridge without closing an import cycle. But taskpipeline's session
// attribution (detectAgentType) needs the SAME marker table that agentbridge's
// DetectAgents uses for wiring — so the table must live somewhere both can reach
// without a cycle. A zero-upward-dep leaf package is that somewhere.
//
// The agent-name strings here ("claude-code", "cursor", ...) match the
// agentbridge.AgentType constants verbatim; they are kept as plain strings (not
// agentbridge.AgentType) precisely so this package never has to import agentbridge.
//
// agentsignals 是刻意最小化的叶子包：持有项目级 agent 标记表与助手函数，仅依赖标准库
// （os/filepath），零向上依赖 agentbridge/skillgen/taskpipeline。
//
// 为何存在：agentbridge 已导入 skillgen，skillgen 已导入 taskpipeline
// （agentbridge→skillgen→taskpipeline），故 taskpipeline 不能再导入 agentbridge，否则
// 闭合导入环。但 taskpipeline 的会话归因（detectAgentType）需要与 agentbridge 的
// DetectAgents 接线期扫描相同的标记表——该表必须放在二者都能触达、又不形成环的地方。
// 零向上依赖的叶子包即此处。
//
// 此处的 agent 名字符串（"claude-code"、"cursor"...）与 agentbridge.AgentType 常量逐字
// 一致；刻意用纯字符串（而非 agentbridge.AgentType）正是为了让本包永不导入 agentbridge。

// projectMarker pairs a project-level path (relative segments + whether it is a file
// vs a directory) with the agent name it signals.
//
// projectMarker 把项目级路径（相对分段 + 文件还是目录）与它所表示的 agent 名配对。
type projectMarker struct {
	segments []string
	isFile   bool
	agent    string
}

// projectMarkers mirrors agentbridge.DetectAgents' project-level checks in the same
// order, so first-match precedence (ProjectAgentMarker) and full-set detection
// (ProjectAgentMarkers) stay consistent with the wiring-time scan.
//
// Per-marker rationale lives next to the decision it documents, so this table is the
// single source of truth for both WHICH markers count and WHY. The recurring theme for
// kimi/reasonix/codex: these agents' USER-LEVEL config dir exists whenever the tool is
// installed, so it canNOT be an auto-detect signal (it would wire the agent on every
// forge init and break test hermeticity); only a PROJECT-level dir — present iff the
// user has run that agent in this project — is a legitimate "develops-with" signal.
//
// projectMarkers 与 agentbridge.DetectAgents 的项目级检查同序镜像，使首次匹配优先级
// （ProjectAgentMarker）与全集合检测（ProjectAgentMarkers）都与接线期扫描一致。
// 每条标记的设计理由随附于它记录的决策旁，使本表同时成为"哪些标记算数"与"为何算数"的
// 唯一真相源。kimi/reasonix/codex 的共同主题：这些 agent 的用户级配置目录在工具一装上
// 即存在，故不能作为 auto 检测信号（否则每次 forge init 都误接线且破坏测试密封性）；
// 只有项目级目录——当且仅当用户在此项目跑过该 agent 才存在——才是合法的"用此 agent
// 开发"信号。
var projectMarkers = []projectMarker{
	{[]string{`.claude`}, false, `claude-code`},
	{[]string{`.cursor`}, false, `cursor`},
	{[]string{`.github`, `instructions`}, false, `copilot`},
	{[]string{`.windsurfrules`}, true, `windsurf`},
	// codex: AGENTS.md is NOT a codex signal — forge init proactively generates
	// AGENTS.md as a universal cross-agent instruction source (codex/cursor/copilot/
	// windsurf/cline all read it); treating it as codex-only would make forge's own
	// AGENTS.md trigger codex wiring. Pure codex-CLI users (AGENTS.md, no .codex/)
	// pass --agents codex explicitly.
	// codex：AGENTS.md 不作 codex 信号——forge init 主动生成 AGENTS.md 作为跨 agent
	// 通用指令源；纯 codex CLI 用户（有 AGENTS.md 无 .codex/）显式 --agents codex。
	{[]string{`.codex`}, false, `codex`},
	{[]string{`.opencode`}, false, `opencode`},
	// cline is signalled by EITHER a .cline/ dir OR a .clinerules/ file/dir; both
	// map to the same agent (deduped in ProjectAgentMarkers).
	// cline 由 .cline/ 目录或 .clinerules/ 二者之一表示，均映射同一 agent（去重）。
	{[]string{`.cline`}, false, `cline`},
	{[]string{`.clinerules`}, false, `cline`},
	// kimi: user-level ~/.kimi-code always exists once installed → project marker only.
	// kimi：用户级 ~/.kimi-code 装上即存在 → 仅项目标记。
	{[]string{`.kimi-code`}, false, `kimi`},
	// reasonix: same philosophy as kimi — ~/.reasonix exists whenever reasonix is
	// installed, so only a project-level .reasonix/ (created once reasonix has run in
	// this project) counts. Users on a fresh project pass --agents reasonix explicitly.
	// reasonix：与 kimi 同一哲学——~/.reasonix 装上即存在，故仅项目级 .reasonix/
	// （reasonix 在此项目跑过一次即创建）算数。新项目用户显式 --agents reasonix。
	{[]string{`.reasonix`}, false, `reasonix`},
}

// markerExists reports whether the marker path exists in projectDir with the correct
// type (directory unless isFile).
//
// markerExists 报告标记路径在 projectDir 中是否存在且类型正确（除非 isFile，否则为目录）。
func markerExists(projectDir string, m projectMarker) bool {
	p := filepath.Join(projectDir, filepath.Join(m.segments...))
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	if m.isFile {
		return !info.IsDir()
	}
	return info.IsDir()
}

// ProjectAgentMarker returns the agent name of the FIRST matching project-level marker
// in projectDir, or "" if none match. Precedence follows projectMarkers order
// (claude-code first). Use this for single-shot session attribution where one agent
// per session is the model (the dominant marker wins).
//
// ProjectAgentMarker 返回 projectDir 中首个命中的项目级标记的 agent 名，无命中返回 ""。
// 优先级遵循 projectMarkers 顺序（claude-code 在前）。用于单会话单 agent 的归因模型
// （占优标记胜出）。
func ProjectAgentMarker(projectDir string) string {
	for _, m := range projectMarkers {
		if markerExists(projectDir, m) {
			return m.agent
		}
	}
	return ""
}

// ProjectAgentMarkers returns the deduplicated agent names of ALL matching project-level
// markers in projectDir, in projectMarkers order (first occurrence wins for dedup). Use
// this when wiring every detected agent (multiple agents can share a project).
//
// ProjectAgentMarkers 返回 projectDir 中所有命中的项目级标记的 agent 名（去重），
// 顺序遵循 projectMarkers（首次出现者保留）。用于给每个检测到的 agent 接线
// （一个项目可被多个 agent 共用）。
func ProjectAgentMarkers(projectDir string) []string {
	var agents []string
	seen := map[string]bool{}
	for _, m := range projectMarkers {
		if !markerExists(projectDir, m) {
			continue
		}
		if seen[m.agent] {
			continue
		}
		seen[m.agent] = true
		agents = append(agents, m.agent)
	}
	return agents
}

// KnownAgents returns the deduplicated list of agent names recognized by project markers,
// in projectMarkers order (first occurrence wins for dedup). This is the closed set a task
// can be delegated to — forge task assign validates --assignee against it so an unknown
// agent does not silently create a task no worker can match (a black hole). Agents that
// carry no project marker (e.g. codebuddy, signalled only via plugin config) are absent;
// assign warns but still accepts them when the user insists explicitly.
//
// KnownAgents 返回 project markers 识别的 agent 名（去重），顺序遵循 projectMarkers
// （首次出现保留）。这是 task 可被分派的封闭集——forge task assign 用它校验 --assignee，
// 避免未知 agent 静默创建无人认领的任务（黑洞）。无项目标记的 agent（如 codebuddy，仅经
// plugin 配置探测）不在内；用户显式坚持时 assign 警告但仍接受。
func KnownAgents() []string {
	var agents []string
	seen := map[string]bool{}
	for _, m := range projectMarkers {
		if seen[m.agent] {
			continue
		}
		seen[m.agent] = true
		agents = append(agents, m.agent)
	}
	return agents
}

// IsKnownAgent reports whether name is one of the recognized agent names (the closed set
// drawn from projectMarkers). Cheaper than KnownAgents() for single lookups; used by the
// assign command to decide warn-but-accept vs silently-valid.
//
// IsKnownAgent 报告 name 是否为已知 agent 名（projectMarkers 封闭集）。单次查找比
// KnownAgents() 更省；assign 命令用它区分「警告后接受」与「静默合法」。
func IsKnownAgent(name string) bool {
	for _, m := range projectMarkers {
		if m.agent == name {
			return true
		}
	}
	return false
}

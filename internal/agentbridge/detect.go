package agentbridge

import (
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentsignals"
	"github.com/MjxUpUp/Forge/internal/hostcap"
)

// DetectAgents scans for known agent config indicators — both project-level markers
// (legacy/team-mode projects) and USER-LEVEL install dirs (post user-level-assets:
// an agent's user config dir exists iff the tool is installed, and user-level wiring
// is idempotent + machine-wide, so detecting an installed agent means "wire it").
//
// Env overrides double as test isolation: CLAUDE_CONFIG_DIR / CODEX_HOME pointing at
// a temp dir both redirects detection away from the real home.
//
// DetectAgents 扫描已知 agent 的 config indicator——项目级标记（遗留/团队模式项目）
// 与用户级安装目录（user-level-assets 之后：agent 的用户配置目录存在 = 该工具已安装，
// 且用户级接线幂等 + 全机器生效，检测到已安装 agent 即"接上它"）。
//
// env 覆盖同时充当测试隔离：CLAUDE_CONFIG_DIR / CODEX_HOME 指向临时目录即可把
// 检测引离真实 home。
func DetectAgents(projectDir string) []AgentType {
	var agents []AgentType
	seen := map[AgentType]bool{}
	add := func(a AgentType) {
		if !seen[a] {
			seen[a] = true
			agents = append(agents, a)
		}
	}

	// Project-level markers (legacy/team-mode projects). Delegated to the shared
	// agentsignals table — the single source of truth for project-level agent markers,
	// shared with taskpipeline's session attribution (detectAgentType) so the wiring
	// scan and the attribution scan can never drift apart. The per-marker rationale
	// (why .codex but not AGENTS.md, why project-marker-only for kimi/reasonix, etc.)
	// lives next to each entry in agentsignals.projectMarkers.
	//
	// 项目级标记（遗留/团队模式项目）。委托给共享的 agentsignals 表——项目级 agent 标记的
	// 唯一真相源，与 taskpipeline 的会话归因（detectAgentType）共享，使接线扫描与归因
	// 扫描永不漂移。每条标记的设计理由（为何 .codex 而非 AGENTS.md、为何 kimi/reasonix
	// 仅项目标记等）见 agentsignals.projectMarkers 各条旁。
	for _, name := range agentsignals.ProjectAgentMarkers(projectDir) {
		add(AgentType(name))
	}

	// User-level install indicators: the agent's config home exists iff the tool is
	// installed. User-level wiring is idempotent + machine-wide — wiring a detected
	// agent once covers every project, which is exactly the post-refactor model.
	// The indicator data (env override / home-relative path per host) lives in the
	// hostcap registry (InstallIndicators column); the explicit iteration order
	// below preserves the pre-registry detection precedence so DetectAgents'
	// output order is unchanged.
	//
	// 用户级安装指示：agent 的 config home 存在 = 该工具已安装。用户级接线幂等 +
	// 全机器生效——给检测到的 agent 接线一次即覆盖所有项目，正是重构后的模型。
	// 指示数据（各宿主的 env 覆盖 / home 相对路径）住在 hostcap 注册表
	// （InstallIndicators 列）；下面显式的迭代顺序保持注册表之前的检测优先级，
	// 使 DetectAgents 的输出顺序不变。
	for _, name := range []string{"claude-code", "codex", "cursor", "windsurf", "opencode"} {
		h := hostcap.Lookup(name)
		if h == nil {
			continue
		}
		for _, ind := range h.InstallIndicators {
			if dir := ind.Resolve(); dir != "" && dirExists(dir) {
				add(AgentType(name))
				break
			}
		}
	}

	return agents
}

// claudeConfigHome resolves Claude Code's config home: CLAUDE_CONFIG_DIR first,
// else ~/.claude. The convention's single source is the hostcap registry row
// (InstallIndicators); this wrapper keeps the local call sites (and the
// ClaudeConfigHomeDir export below) on one resolver. Same convention as
// hooks/plugin_detect.go.
//
// claudeConfigHome 解析 Claude Code 的 config home：CLAUDE_CONFIG_DIR 优先，
// 否则 ~/.claude。该约定的单一真相源是 hostcap 注册表行（InstallIndicators）；
// 本 wrapper 让本地调用点（及下面的 ClaudeConfigHomeDir 导出）共用一个解析器。
// 与 hooks/plugin_detect.go 同约定。
func claudeConfigHome() string {
	return hostcap.InstallDir("claude-code")
}

// ClaudeConfigHomeDir exports claudeConfigHome for cross-agent environment auditing
// (forge doctor): the user-level Claude Code config home where settings.json hooks live.
// Single source of the CLAUDE_CONFIG_DIR-first convention — doctor must not carry a
// second copy that can drift.
//
// ClaudeConfigHomeDir 导出 claudeConfigHome 供跨 agent 环境审计（forge doctor）使用：
// user-level 的 Claude Code 配置目录，settings.json hooks 所在地。CLAUDE_CONFIG_DIR
// 优先约定的单一真相源——doctor 不得持有会漂移的第二份副本。
func ClaudeConfigHomeDir() string {
	return claudeConfigHome()
}

// codexConfigHome resolves codex's config home: CODEX_HOME first, else ~/.codex.
// Single source: the hostcap registry row (InstallIndicators).
//
// codexConfigHome 解析 codex 的 config home：CODEX_HOME 优先，否则 ~/.codex。
// 单一真相源：hostcap 注册表行（InstallIndicators）。
func codexConfigHome() string {
	return hostcap.InstallDir("codex")
}

// ParseAgentFlag parses a comma-separated agent flag value.
// auto triggers auto-detection; explicit names (e.g. claude-code,cursor) are used directly.
//
// ParseAgentFlag 解析逗号分隔的 agent flag 值。
// auto 触发自动检测；显式名（如 claude-code,cursor）直接使用。
func ParseAgentFlag(projectDir string, flag string) []AgentType {
	if flag == "" || flag == "auto" {
		return DetectAgents(projectDir)
	}

	var agents []AgentType
	for _, name := range strings.Split(flag, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		switch AgentType(name) {
		case AgentClaudeCode, AgentCursor, AgentCopilot, AgentWindsurf, AgentCodex, AgentOpencode, AgentCline, AgentKimi, AgentCodeBuddy, AgentReasonix:
			agents = append(agents, AgentType(name))
		}
	}
	return agents
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

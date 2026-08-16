package agentbridge

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentsignals"
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
	//
	// 用户级安装指示：agent 的 config home 存在 = 该工具已安装。用户级接线幂等 +
	// 全机器生效——给检测到的 agent 接线一次即覆盖所有项目，正是重构后的模型。
	if dirExists(claudeConfigHome()) {
		add(AgentClaudeCode)
	}
	if dirExists(codexConfigHome()) {
		add(AgentCodex)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if dirExists(filepath.Join(home, ".cursor")) {
			add(AgentCursor)
		}
		// windsurf's user-level config root (~/.codeium — holds windsurf/hooks.json
		// and windsurf/memories/global_rules.md, the paths WindsurfTranslator writes).
		//
		// windsurf 的用户级配置根（~/.codeium——下有 windsurf/hooks.json 与
		// windsurf/memories/global_rules.md，即 WindsurfTranslator 写入的路径）。
		if dirExists(filepath.Join(home, ".codeium")) {
			add(AgentWindsurf)
		}
	}
	// opencode resolves its global config dir via the XDG convention
	// ($XDG_CONFIG_HOME/opencode, else ~/.config/opencode) — detection must use the
	// same resolution as OpenCodeConfigDir's write path, or XDG_CONFIG_HOME users
	// get wired into a directory opencode never reads while detection looks at the
	// wrong one.
	//
	// opencode 按 XDG 约定解析全局配置目录（$XDG_CONFIG_HOME/opencode，否则
	// ~/.config/opencode）——检测必须与 OpenCodeConfigDir 的写入路径同解析，
	// 否则 XDG_CONFIG_HOME 用户被接进 opencode 从不读的目录，而检测看的又是
	// 另一个错误位置。
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		if dirExists(filepath.Join(base, "opencode")) {
			add(AgentOpencode)
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		if dirExists(filepath.Join(home, ".config", "opencode")) {
			add(AgentOpencode)
		}
	}

	return agents
}

// claudeConfigHome resolves Claude Code's config home: CLAUDE_CONFIG_DIR first,
// else ~/.claude. Local copy of the same convention in hooks/plugin_detect.go
// (kept local to avoid an agentbridge→hooks import just for detection).
//
// claudeConfigHome 解析 Claude Code 的 config home：CLAUDE_CONFIG_DIR 优先，
// 否则 ~/.claude。与 hooks/plugin_detect.go 同约定的本地副本（为避免仅为检测
// 引入 agentbridge→hooks 依赖而本地持有）。
func claudeConfigHome() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
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
//
// codexConfigHome 解析 codex 的 config home：CODEX_HOME 优先，否则 ~/.codex。
func codexConfigHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
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

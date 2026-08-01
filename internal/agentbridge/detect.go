package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
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

	// Project-level markers (legacy/team-mode projects).
	//
	// 项目级标记（遗留/团队模式项目）。
	if dirExists(filepath.Join(projectDir, ".claude")) {
		add(AgentClaudeCode)
	}
	if dirExists(filepath.Join(projectDir, ".cursor")) {
		add(AgentCursor)
	}
	if dirExists(filepath.Join(projectDir, ".github", "instructions")) {
		add(AgentCopilot)
	}
	if fileExists(filepath.Join(projectDir, ".windsurfrules")) {
		add(AgentWindsurf)
	}
	// codex is detected via the .codex/ directory. AGENTS.md is NOT a codex signal — forge
	// init proactively generates AGENTS.md as a universal cross-agent instruction source
	// (codex/cursor/copilot/windsurf/cline all read it); treating it as a codex signal
	// would make forge's own AGENTS.md trigger codex wiring (.codex/ cascade false positive).
	// Pure codex-CLI users (only AGENTS.md, no .codex/) use --agents codex explicitly.
	//
	// codex 靠 .codex/ 目录检测。AGENTS.md 不作为 codex 信号——forge init 会主动生成
	// AGENTS.md 作为跨 agent 通用指令源（codex/cursor/copilot/windsurf/cline 都读），若把它
	// 当 codex 信号，forge 自己写的 AGENTS.md 会触发自身给 codex 接线（.codex/ 级联误判）。
	// 纯 codex CLI 用户（仅 AGENTS.md 无 .codex/）用 --agents codex 显式声明。
	if dirExists(filepath.Join(projectDir, ".codex")) {
		add(AgentCodex)
	}
	if dirExists(filepath.Join(projectDir, ".opencode")) {
		add(AgentOpencode)
	}
	if dirExists(filepath.Join(projectDir, ".cline")) || dirExists(filepath.Join(projectDir, ".clinerules")) {
		add(AgentCline)
	}
	// kimi is detected via the project-level .kimi-code/ dir only. The user-level
	// ~/.kimi-code always exists once kimi is installed — using it as an auto-detect
	// signal would wire kimi on EVERY `forge init` (and break test hermeticity on any
	// machine with kimi installed). Kimi users without a project dir pass
	// `--agents kimi` explicitly; the wiring is user-level and idempotent, so one
	// explicit init covers all projects (same philosophy as codex, see above).
	//
	// kimi 只按项目级 .kimi-code/ 目录检测。user-level 的 ~/.kimi-code 装上 kimi
	// 后恒存在——拿它做 auto 检测信号会让每次 `forge init` 都接 kimi（并破坏任何
	// 装有 kimi 机器上的测试密封性）。没有项目目录的 kimi 用户显式传
	// `--agents kimi`；接线是 user-level 且幂等，显式 init 一次即覆盖所有项目
	// （与 codex 同一哲学，见上文）。
	if dirExists(filepath.Join(projectDir, ".kimi-code")) {
		add(AgentKimi)
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
		case AgentClaudeCode, AgentCursor, AgentCopilot, AgentWindsurf, AgentCodex, AgentOpencode, AgentCline, AgentKimi:
			agents = append(agents, AgentType(name))
		}
	}
	return agents
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

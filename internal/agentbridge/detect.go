package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectAgents scans the project directory for known agent config indicators.
//
// DetectAgents 扫描项目目录，检测已知 agent 的 config indicator。
func DetectAgents(projectDir string) []AgentType {
	var agents []AgentType

	if dirExists(filepath.Join(projectDir, ".claude")) {
		agents = append(agents, AgentClaudeCode)
	}
	if dirExists(filepath.Join(projectDir, ".cursor")) {
		agents = append(agents, AgentCursor)
	}
	if dirExists(filepath.Join(projectDir, ".github", "instructions")) {
		agents = append(agents, AgentCopilot)
	}
	if fileExists(filepath.Join(projectDir, ".windsurfrules")) {
		agents = append(agents, AgentWindsurf)
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
		agents = append(agents, AgentCodex)
	}
	if dirExists(filepath.Join(projectDir, ".opencode")) {
		agents = append(agents, AgentOpencode)
	}
	if dirExists(filepath.Join(projectDir, ".cline")) || dirExists(filepath.Join(projectDir, ".clinerules")) {
		agents = append(agents, AgentCline)
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
		agents = append(agents, AgentKimi)
	}

	return agents
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

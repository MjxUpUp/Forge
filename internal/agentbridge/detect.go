package agentbridge

import (
	"os"
	"path/filepath"
)

// DetectAgents scans the project directory for known agent config indicators.
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

	return agents
}

// ParseAgentFlag parses a comma-separated agent flag value.
// "auto" triggers detection; explicit names like "claude-code,cursor" are used directly.
func ParseAgentFlag(projectDir string, flag string) []AgentType {
	if flag == "" || flag == "auto" {
		return DetectAgents(projectDir)
	}

	var agents []AgentType
	for _, name := range splitComma(flag) {
		switch AgentType(name) {
		case AgentClaudeCode, AgentCursor, AgentCopilot, AgentWindsurf, AgentCodex, AgentOpencode, AgentCline:
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

func splitComma(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := trimSpaces(s[start:i])
			if part != "" {
				result = append(result, part)
			}
			start = i + 1
		}
	}
	return result
}

func trimSpaces(s string) string {
	start := 0
	for start < len(s) && s[start] == ' ' {
		start++
	}
	end := len(s)
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

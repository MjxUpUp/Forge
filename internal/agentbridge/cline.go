package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClineTranslator generates .clinerules/forge-quality.md (guidance rules only).
// Cline (VS Code extension) has NO lifecycle hooks — no PreToolUse/Stop — so it
// cannot enforce gates the way claude-code/codex/cursor do. Like copilot/windsurf,
// Cline gets guidance text only. Cline's project-level .cline/ directory supports
// rules/skills/hooks (per docs.cline.bot/getting-started/config), so .clinerules/
// is the official channel and forge-quality.md rides Cline's auto-merge of every
// .md/.txt there.
//
// MCP is NOT auto-wired: Cline loads MCP servers only from the global file
// ~/.cline/data/settings/cline_mcp_settings.json (opened via the Cline panel:
// MCP Servers → Configure → Configure MCP Servers), not from any project-level
// file (project-scoped MCP is open feature request cline/cline#2418, unshipped as
// of 2026-07). Writing .cline/mcp.json would mislead users into thinking forge
// MCP is wired when Cline ignores it; instead the rules instruct the user to add
// the forge server via the Cline panel manually.
type ClineTranslator struct{}

func (t *ClineTranslator) Detect(projectDir string) bool {
	return dirExists(filepath.Join(projectDir, ".cline")) ||
		dirExists(filepath.Join(projectDir, ".clinerules"))
}

func (t *ClineTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("cline: protocol is required")
	}

	// .clinerules/ — Cline auto-loads every .md/.txt here as persistent rules.
	rulesDir := filepath.Join(projectDir, ".clinerules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("cline: failed to create .clinerules dir: %w", err)
	}
	content := buildClineRules(input)
	if err := os.WriteFile(filepath.Join(rulesDir, "forge-quality.md"), []byte(content), 0644); err != nil {
		return fmt.Errorf("cline: write forge-quality.md: %w", err)
	}

	// No .cline/mcp.json — Cline does not auto-load project-level MCP (global
	// only; see the translator doc comment + docs.cline.bot). The rules instruct
	// the user to wire the forge server via the Cline panel manually.
	return nil
}

func (t *ClineTranslator) AgentType() AgentType {
	return AgentCline
}

// buildClineRules renders the Forge quality protocol as a Cline rule file.
// Mirrors cursor's .mdc guidance (quality standards + session rules), but adds
// a Cline-specific integration note: since Cline has no hooks, the agent must
// drive the workflow via forge MCP tools + the AGENTS.md protocol rather than
// expecting automatic gate enforcement.
func buildClineRules(input *TranslationInput) string {
	var sb strings.Builder

	sb.WriteString("# Forge 质量协议\n\n")
	sb.WriteString("本项目使用 Forge 进行质量保障。Cline 无 lifecycle hooks（无 PreToolUse/Stop），门禁无法自动拦截，以下为 guidance 规则——请主动遵守，并通过 forge MCP 工具结构化驱动质量流程。\n\n")

	// Quality standards
	sb.WriteString("## 质量标准\n\n")
	for _, s := range input.Protocol.Standards {
		if !s.Enabled {
			continue
		}
		icon := "🔴"
		switch s.Severity {
		case "warning":
			icon = "🟡"
		case "info":
			icon = "🔵"
		}
		hookInfo := ""
		if s.EnforceHook != "" {
			hookInfo = fmt.Sprintf(" (enforced: %s)", s.EnforceHook)
		}
		sb.WriteString(fmt.Sprintf("- %s **%s**: %s%s\n", icon, s.Name, s.Description, hookInfo))
	}
	sb.WriteString("\n")

	// Session rules
	sb.WriteString("## 会话行为规则\n\n")
	for _, r := range input.Protocol.SessionRules {
		prefix := "[MUST]"
		if !r.Mandatory {
			prefix = "[SHOULD]"
		}
		sb.WriteString(fmt.Sprintf("- %s %s\n", prefix, r.Instruction))
	}
	sb.WriteString("\n")

	// Cline-specific integration
	sb.WriteString("## Forge 集成（Cline 专属）\n\n")
	sb.WriteString("Cline 不支持 lifecycle hooks，因此 forge 门禁（task-guard/file-sentinel 等）无法自动拦截工具调用。替代方案：\n")
	sb.WriteString("- 通过 forge MCP 工具（task resume/decide/attach + gate/complete + experience）结构化驱动质量流程，而非靠人工记忆\n")
	sb.WriteString("- 阅读项目根 AGENTS.md 获取完整质量协议（task 工作流、门禁顺序、安全机制、常见错误）\n")
	sb.WriteString("- 源码变更前启动 forge 任务；测试伴随变更；提交前过门禁（task-implement → task-verify → task-complete）\n")
	sb.WriteString("- 不弱化断言（t.Fatal/assert!）；编译必须通过\n\n")

	// Manual MCP wiring — Cline does not auto-load project-level MCP (global only,
	// opened via the Cline panel). The JSON snippet uses a raw-string literal so
	// the embedded ASCII double-quotes survive Windows input corruption (see
	// memory windows-input-quote-corruption — same reason codexMCPServerTOML uses
	// rune(34)).
	sb.WriteString("## 接入 forge MCP（手动，一次性）\n\n")
	sb.WriteString("Cline 不自动加载项目级 MCP（仅读全局 ~/.cline/data/settings/cline_mcp_settings.json）。为获得 forge 的 15 个结构化工具（task resume/decide/attach + gate/complete + experience），在 Cline 面板手动添加 forge server：\n")
	sb.WriteString("1. Cline 面板 → MCP Servers 图标 → Configure → Configure MCP Servers\n")
	sb.WriteString("2. 在打开的 cline_mcp_settings.json 的 mcpServers 下加入：\n")
	sb.WriteString(`   "forge": { "command": "forge", "args": ["mcp", "serve"], "disabled": false, "autoApprove": [] }
`)
	sb.WriteString("3. 保存后 Cline 自动连接；确保 forge 在 PATH（hooks 同样 spawn forge）。\n\n")

	return sb.String()
}

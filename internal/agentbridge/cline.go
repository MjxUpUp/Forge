package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/protocol"
)

// ClineTranslator generates .clinerules/forge-quality.md (guidance rules only).
// Cline (a VS Code extension) has no lifecycle hooks—no PreToolUse/Stop—so it cannot
// enforce gates like claude-code/codex/cursor. Like copilot/windsurf, Cline only gets
// guidance text. Cline project-level .cline/ directory supports rules/skills/hooks
// (see docs.cline.bot/getting-started/config), so .clinerules/ is the official channel
// and forge-quality.md takes effect via Cline auto-merging all .md/.txt in that dir.
//
// ClineTranslator 生成 .clinerules/forge-quality.md（仅 guidance 规则）。
// Cline（VS Code 扩展）没有 lifecycle hooks——无 PreToolUse/Stop——故无法像
// claude-code/codex/cursor 那样 enforce gate。与 copilot/windsurf 一样，Cline 只拿到
// guidance 文本。Cline 的 project-level .cline/ 目录支持 rules/skills/hooks
// （见 docs.cline.bot/getting-started/config），故 .clinerules/ 是官方渠道，forge-quality.md
// 借 Cline 自动合并该目录下所有 .md/.txt 的机制生效。
type ClineTranslator struct{}

func (t *ClineTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("cline: protocol is required")
	}

	// .clinerules/——Cline auto-loads all .md/.txt in this dir as persistent rules.
	//
	// .clinerules/——Cline 自动加载此目录下所有 .md/.txt 作为 persistent rules。
	rulesDir := filepath.Join(projectDir, ".clinerules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("cline: failed to create .clinerules dir: %w", err)
	}
	content := buildClineRules(input)
	if err := os.WriteFile(filepath.Join(rulesDir, "forge-quality.md"), []byte(content), 0644); err != nil {
		return fmt.Errorf("cline: write forge-quality.md: %w", err)
	}

	// Do not write .cline/mcp.json—Cline does not auto-load project-level MCP (global only;
	// see translator doc comment + docs.cline.bot). The rules instruct manual wiring of the
	// forge server via the Cline panel.
	//
	// 不写 .cline/mcp.json——Cline 不自动加载 project-level MCP（仅 global；
	// 见 translator doc comment + docs.cline.bot）。规则中指引通过 Cline panel 手动接线
	// forge server。
	return nil
}

func (t *ClineTranslator) AgentType() AgentType {
	return AgentCline
}

// buildClineRules renders the Forge quality protocol into a Cline rule file.
// It mirrors cursor .mdc guidance (quality standards + session rules) and adds a Cline-
// specific integration note: since Cline has no hooks, the agent must drive the workflow
// via the forge CLI + AGENTS.md protocol rather than relying on automatic gate enforcement.
//
// buildClineRules 把 Forge 质量协议渲染成 Cline rule 文件。
// 镜像 cursor 的 .mdc guidance（质量标准 + 会话规则），并加一段 Cline 专属集成说明：
// 因 Cline 无 hooks，agent 须通过 forge CLI + AGENTS.md 协议驱动 workflow，
// 不能指望自动 gate enforcement。
func buildClineRules(input *TranslationInput) string {
	var sb strings.Builder

	sb.WriteString("# Forge 质量协议\n\n")
	sb.WriteString("本项目使用 Forge 进行质量保障。Cline 无 lifecycle hooks（无 PreToolUse/Stop），门禁无法自动拦截，以下为 guidance 规则——请主动遵守，并通过 forge CLI 结构化驱动质量流程。\n\n")

	// quality standards
	//
	// 质量标准
	sb.WriteString("## 质量标准\n\n")
	protocol.RenderStandards(&sb, input.Protocol.Standards, protocol.StandardRenderStyle{
		SeverityLabel:  protocol.EmojiSeverityLabel,
		HookInfoFormat: " (enforced: %s)",
		LineFormat:     "- %s **%s**: %s%s\n",
	})
	sb.WriteString("\n")

	// session rules
	//
	// 会话规则
	sb.WriteString("## 会话行为规则\n\n")
	protocol.RenderSessionRules(&sb, input.Protocol.SessionRules, protocol.SessionRuleRenderStyle{
		MandatoryLabel: protocol.MustShouldLabel,
		LineFormat:     "- %[1]s %[2]s\n",
	})
	sb.WriteString("\n")

	// Cline-specific integration
	//
	// Cline 专属集成
	sb.WriteString("## Forge 集成（Cline 专属）\n\n")
	sb.WriteString("Cline 不支持 lifecycle hooks，因此 forge 门禁（task-guard/file-sentinel 等）无法自动拦截工具调用。替代方案：\n")
	sb.WriteString("- 通过 forge CLI（task/gate/complete 命令）结构化驱动质量流程，而非靠人工记忆\n")
	sb.WriteString("- 阅读项目根 AGENTS.md 获取完整质量协议（task 工作流、门禁顺序、安全机制、常见错误）\n")
	sb.WriteString("- 源码变更前启动 forge 任务；测试伴随变更；提交前过门禁（task-implement → task-verify → task-complete）\n")
	sb.WriteString("- 不弱化断言（t.Fatal/assert!）；编译必须通过\n\n")

	return sb.String()
}

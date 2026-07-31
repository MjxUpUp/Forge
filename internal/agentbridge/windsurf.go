package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WindsurfTranslator generates .windsurf/hooks.json (real, blockable Cascade hooks)
// and updates .windsurfrules (guidance fallback). Cascade has built-in lifecycle hooks; exit-code-2 means deny,
// so it stands alongside claude-code/codex/cursor as an agent that Forge gates can truly enforce. Its stdin
// schema differs from Claude Code, so hook commands carry `--agent windsurf` and are normalized by forge
// (see internal/cli/hook_normalize.go).
//
// WindsurfTranslator 生成 .windsurf/hooks.json（真实、可 block 的 Cascade hooks）
// 并更新 .windsurfrules（guidance 兜底）。Cascade 内置 lifecycle hooks，exit-code-2 即 deny，
// 故与 claude-code/codex/cursor 并列为 Forge gate 真正能 enforce 的 agent。其 stdin
// schema 与 Claude Code 不同，故 hook 命令带 `--agent windsurf`，由 forge 归一化
// （见 internal/cli/hook_normalize.go）。
type WindsurfTranslator struct{}

func (t *WindsurfTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("windsurf: protocol is required")
	}

	// Real Cascade lifecycle hooks — the enforcement interface. Windsurf's .windsurf/hooks.json
	// is a flat structure: hooks.<event>[].{command,show_output}, with snake_case event names and a stdin
	// schema (tool_info/agent_action_name) different from Claude Code, so commands carry `--agent windsurf`,
	// normalized by forge (internal/cli/hook_normalize.go). pre-event exit 2 = deny.
	//
	// 真实的 Cascade lifecycle hooks——enforcement 接口。Windsurf 的 .windsurf/hooks.json
	// 是扁平结构：hooks.<event>[].{command,show_output}，event 名为 snake_case，stdin
	// schema（tool_info/agent_action_name）与 Claude Code 不同，故命令带 `--agent windsurf`，
	// 由 forge 归一化（internal/cli/hook_normalize.go）。pre-event exit 2 = deny。
	hooksDir := filepath.Join(projectDir, ".windsurf")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("windsurf: create .windsurf dir: %w", err)
	}
	hooksData, err := json.MarshalIndent(buildWindsurfHooks(), "", "  ")
	if err != nil {
		return fmt.Errorf("windsurf: marshal hooks.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), append(hooksData, '\n'), 0644); err != nil {
		return fmt.Errorf("windsurf: write hooks.json: %w", err)
	}

	// Guidance rules, fallback for Windsurf versions that do not support hooks.
	//
	// Guidance 规则，作为不支持 hook 的 Windsurf 版本的兜底。
	content := buildWindsurfSection(input)
	path := filepath.Join(projectDir, ".windsurfrules")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		// A read error other than NotExist (permissions, IO) must not fall
		// through to the whole-file overwrite below — that would silently
		// destroy the user's existing rules. Same contract as kimi.go.
		return fmt.Errorf("windsurf: failed to read .windsurfrules: %w", err)
	}
	if len(existing) > 0 {
		updated := replaceForgeRules(string(existing), content)
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// Create a new file.
	//
	// 创建新文件
	return os.WriteFile(path, []byte(content), 0644)
}

func (t *WindsurfTranslator) AgentType() AgentType {
	return AgentWindsurf
}

type windsurfHookEntry struct {
	Command    string `json:"command"`
	ShowOutput bool   `json:"show_output"`
}

// buildWindsurfHooks corresponds to GenerateSettings in hooks/settings.go, generating the
// native Cascade hook format for Windsurf. Windsurf's hooks.json is a flat structure —
// hooks.<event>[].{command,show_output} — with snake_case event names
// (pre_write_code/post_write_code/pre_run_command/post_run_command/
// post_read_code/session_start/session_end), in contrast to Claude Code's PascalCase.
// Multiple hooks on the same event (task-guard + assertion-check on pre_write_code) run in order;
// pre-event exit 2 = deny. Kept in sync with settings.go manually — TestWindsurfWiringMirrorsClaudeSettings
// guards against drift.
//
// buildWindsurfHooks 对应 hooks/settings.go 的 GenerateSettings，针对 Windsurf 原生
// Cascade hook 格式生成。Windsurf 的 hooks.json 是扁平结构——
// hooks.<event>[].{command,show_output}——event 名为 snake_case
// （pre_write_code/post_write_code/pre_run_command/post_run_command/
// post_read_code/session_start/session_end），与 Claude Code 的 PascalCase 相对。
// 同 event 多 hook（pre_write_code 上的 task-guard + assertion-check）按顺序执行；
// pre-event exit 2 = deny。与 settings.go 手动保持同步——TestWindsurfWiringMirrorsClaudeSettings
// 守卫 drift。
func buildWindsurfHooks() map[string]any {
	// Both task-guard and assertion-check gate write operations; Windsurf runs all entries
	// under the same event in order, so putting both in a single pre_write_code list is correct
	// (Windsurf matches by command rather than by event, unlike Claude Code's per-event independent matchers).
	//
	// task-guard 与 assertion-check 都 gate 写操作；Windsurf 按顺序跑同 event 的所有
	// 条目，故单个 pre_write_code 列表里放两者是正确的（Windsurf 用 command 而非 event
	// 匹配，与 Claude Code 的 per-event 独立 matcher 不同）。
	return map[string]any{
		"hooks": map[string][]windsurfHookEntry{
			"pre_write_code": {
				{Command: "forge hook task-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook assertion-check --agent windsurf", ShowOutput: false},
				{Command: "forge hook read-before-edit --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
			"pre_run_command": {
				{Command: "forge hook bash-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook hazard-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
			"post_write_code": {
				{Command: "forge hook auto-compile --agent windsurf", ShowOutput: false},
				{Command: "forge hook workflow-test-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
			"post_run_command": {
				{Command: "forge hook file-sentinel --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
			"post_read_code": {
				{Command: "forge hook tool-track --agent windsurf", ShowOutput: false},
			},
			"session_start": {
				{Command: "forge hook skill-scan", ShowOutput: false},
				{Command: "forge hook mcp-scan", ShowOutput: false},
				{Command: "forge hook init-suggest", ShowOutput: false},
				{Command: "forge hook task-resume", ShowOutput: false},
				{Command: "forge hook skill-trigger", ShowOutput: false},
			},
			"session_end": {
				{Command: "forge hook task-verify", ShowOutput: false},
				{Command: "forge hook review-stop", ShowOutput: false},
				{Command: "forge hook skill-trigger", ShowOutput: false},
			},
		},
	}
}

const (
	forgeRulesStart = "<!-- FORGE:START -->"
	forgeRulesEnd   = "<!-- FORGE:END -->"
)

func buildWindsurfSection(input *TranslationInput) string {
	var sb strings.Builder

	sb.WriteString(forgeRulesStart + "\n\n")
	sb.WriteString("# Forge Quality Standards\n\n")

	// Quality standards.
	//
	// 质量标准
	for _, s := range input.Protocol.Standards {
		if !s.Enabled {
			continue
		}
		severity := "ERROR"
		switch s.Severity {
		case "warning":
			severity = "WARNING"
		case "info":
			severity = "INFO"
		}
		hookInfo := ""
		if s.EnforceHook != "" {
			hookInfo = fmt.Sprintf(" (enforced: %s)", s.EnforceHook)
		}
		sb.WriteString(fmt.Sprintf("- [%s] **%s**: %s%s\n", severity, s.Name, s.Description, hookInfo))
	}
	sb.WriteString("\n")

	// Session rules.
	//
	// 会话规则
	for _, r := range input.Protocol.SessionRules {
		prefix := "ALWAYS"
		if !r.Mandatory {
			prefix = "PREFER"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", prefix, r.Instruction))
	}
	sb.WriteString("\n")

	sb.WriteString(forgeRulesEnd + "\n")
	return sb.String()
}

// replaceForgeRules replaces the content between FORGE:START and FORGE:END markers; content outside the markers is preserved as-is.
// Same pattern as skillgen/claudemd.go.
//
// replaceForgeRules 替换 FORGE:START 与 FORGE:END 标记之间的内容，标记外的内容原样保留。
// 与 skillgen/claudemd.go 同一模式。
func replaceForgeRules(content, newSection string) string {
	startIdx := strings.Index(content, forgeRulesStart)
	endIdx := strings.Index(content, forgeRulesEnd)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		// No markers — append.
		//
		// 无标记——追加
		return content + "\n" + newSection
	}

	before := content[:startIdx]
	after := content[endIdx+len(forgeRulesEnd):]

	section := strings.TrimRight(newSection, "\n")
	result := before + section + "\n"

	after = strings.TrimLeft(after, "\n")
	if after != "" {
		result += "\n" + after
	}
	return result
}

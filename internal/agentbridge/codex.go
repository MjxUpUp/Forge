package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// CodexTranslator generates .codex/hooks.json mirroring the Claude Code hook
// wiring. Codex's lifecycle hooks (PreToolUse/PostToolUse/Stop) are
// schema-compatible with Claude Code's — same matcher/hooks/type/command shape
// and same stdin/stdout JSON protocol — so the identical `forge hook <name>`
// commands run unchanged. Alongside claude-code and cursor, codex is one of
// the agents whose hooks actually enforce the Forge gates; copilot/windsurf
// still emit guidance text only. See CursorTranslator for cursor's flat-schema
// variant of the same hook command surface.
//
// Matcher note: Codex compiles matcher as a regex over tool_name, whereas
// Claude Code treats it as a tool-name match. Plain names ("Bash") and
// alternation ("Write|Edit") are valid regex and match identically in both,
// so the Claude wiring transfers directly. Forge never emits the glob-style
// `Bash(...)` form, which would be an invalid Codex matcher.
type CodexTranslator struct{}

func (t *CodexTranslator) Detect(projectDir string) bool {
	// .codex/ only — AGENTS.md is not a codex signal (forge generates AGENTS.md
	// universally as cross-agent instructions; see DetectAgents note).
	return dirExists(filepath.Join(projectDir, ".codex"))
}

func (t *CodexTranslator) Translate(projectDir string, input *TranslationInput) error {
	codexDir := filepath.Join(projectDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("codex: failed to create .codex dir: %w", err)
	}
	data, err := json.MarshalIndent(buildCodexHooks(), "", "  ")
	if err != nil {
		return fmt.Errorf("codex: failed to marshal hooks.json: %w", err)
	}
	path := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("codex: failed to write hooks.json: %w", err)
	}
	// Append forge MCP server to .codex/config.toml — idempotent (see mcpconfig.go).
	if err := writeCodexMCP(projectDir); err != nil {
		return fmt.Errorf("codex: failed to generate config.toml mcp_servers: %w", err)
	}
	return nil
}

func (t *CodexTranslator) AgentType() AgentType {
	return AgentCodex
}

// buildCodexHooks derives codex's hooks.json from hooks.ForgeHookSpec — the
// single source of truth shared with settings.local.json and the plugin pack.
// Codex's hook schema is identical to Claude Code's nested
// {matcher, hooks:[{type,command}]} shape (Codex compiles matcher as a regex
// over tool_name; Forge emits only plain names and alternation, both valid
// regex), so the spec marshals to a valid codex hooks.json unchanged. Codex
// lacks a SessionStart lifecycle hook, so that event is filtered out (skill-scan
// is Claude-Code-only). No hand-maintained copy → no drift.
// TestCodexWiringMirrorsClaudeSettings guards command-set parity.
func buildCodexHooks() map[string]any {
	spec := hooks.ForgeHookSpec()
	codex := make(map[string][]hooks.HookMatcher, len(spec))
	for event, matchers := range spec {
		// 白名单：codex 只支持 PreToolUse/PostToolUse/Stop（无 SessionStart/PostCompact/
		// UserPromptSubmit 等会话/压缩/prompt lifecycle）。其余 claude-code 特有 event——含
		// gap#2 的 PostCompact/UserPromptSubmit 重注入链——自动跳过。
		if event != "PreToolUse" && event != "PostToolUse" && event != "Stop" {
			continue
		}
		codex[event] = matchers
	}
	return map[string]any{
		`hooks`: codex,
	}
}

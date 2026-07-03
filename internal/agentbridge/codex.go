package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	return dirExists(filepath.Join(projectDir, ".codex")) ||
		fileExists(filepath.Join(projectDir, "AGENTS.md"))
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

type codexHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type codexHookMatcher struct {
	Matcher string           `json:"matcher,omitempty"`
	Hooks   []codexHookEntry `json:"hooks"`
}

// buildCodexHooks mirrors hooks/settings.go GenerateSettings. Codex and Claude
// Code share the same hook command surface (`forge hook <name>`); only the
// config file location differs. Kept in sync manually with settings.go — if a
// hook is added there, add it here too. (A future refactor could expose a
// shared registration table so the two stay aligned by construction.)
func buildCodexHooks() map[string]any {
	hook := func(cmd string) codexHookEntry {
		return codexHookEntry{Type: "command", Command: cmd}
	}
	return map[string]any{
		"hooks": map[string][]codexHookMatcher{
			"PostToolUse": {
				{Matcher: "Write|Edit", Hooks: []codexHookEntry{
					hook("forge hook auto-compile"),
					hook("forge hook workflow-test-guard"),
				}},
				{Matcher: "Bash", Hooks: []codexHookEntry{
					hook("forge hook file-sentinel"),
				}},
				{Matcher: "Read", Hooks: []codexHookEntry{
					hook("forge hook tool-track"),
				}},
			},
			"PreToolUse": {
				{Matcher: "Write|Edit", Hooks: []codexHookEntry{
					hook("forge hook task-guard"),
					hook("forge hook assertion-check"),
				}},
				{Matcher: "Bash", Hooks: []codexHookEntry{
					hook("forge hook bash-guard"),
					hook("forge hook hazard-guard"),
				}},
			},
			"Stop": {
				{Hooks: []codexHookEntry{
					hook("forge gate --current --silent"),
					hook("forge hook task-verify"),
					hook("forge hook review-stop"),
				}},
			},
		},
	}
}

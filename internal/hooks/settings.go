package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// embeddedHooks maps script names (without .sh suffix) to their embedded content.
var embeddedHooks = map[string]string{
	"auto-compile":     AutoCompileHook,
	"assertion-check":  AssertionCheckHook,
	"experience-check": ExperienceCheckHook,
	"task-verify":      TaskVerifyHook,
	"task-guard":       TaskGuardHook,
	"tool-track":       ToolTrackHook,
	"bash-guard":       BashGuardHook,
	"file-sentinel":    FileSentinelHook,
}

// EmbeddedContent returns the hook script content for the given name
// (e.g. "auto-compile"). Returns the content and true if found.
func EmbeddedContent(name string) (string, bool) {
	content, ok := embeddedHooks[name]
	return content, ok
}

// GenerateSettings creates .claude/settings.local.json with hook integration.
// All hook commands route through "forge hook <name>" so they work
// regardless of Claude Code's CWD and auto-sync on every invocation.
func GenerateSettings(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	type hookEntry struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}

	type hookMatcher struct {
		Matcher string      `json:"matcher,omitempty"`
		Hooks   []hookEntry `json:"hooks"`
	}

	settings := map[string]interface{}{
		"hooks": map[string][]hookMatcher{
			"PostToolUse": {
				{
					Matcher: "Write|Edit",
					Hooks: []hookEntry{
						{Type: "command", Command: "forge hook auto-compile"},
					},
				},
				{
					Matcher: "Bash",
					Hooks: []hookEntry{
						{Type: "command", Command: "forge hook file-sentinel"},
					},
				},
				{
					Matcher: "Bash|Read|Grep|Glob|Skill|Agent",
					Hooks: []hookEntry{
						{Type: "command", Command: "forge hook tool-track"},
					},
				},
			},
			"PreToolUse": {
				{
					Matcher: "Write|Edit",
					Hooks: []hookEntry{
						{Type: "command", Command: "forge hook task-guard"},
						{Type: "command", Command: "forge hook assertion-check"},
						{Type: "command", Command: "forge hook experience-check"},
					},
				},
				{
					Matcher: "Bash",
					Hooks: []hookEntry{
						{Type: "command", Command: "forge hook bash-guard"},
					},
				},
			},
			"Stop": {
				{
					Hooks: []hookEntry{
						{Type: "command", Command: "forge gate --current --silent"},
						{Type: "command", Command: "forge hook task-verify"},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	path := filepath.Join(claudeDir, "settings.local.json")
	return os.WriteFile(path, data, 0644)
}

// WriteHookTemplates writes embedded hook scripts to .forge/hooks/.
func WriteHookTemplates(forgeDir string) error {
	hooksDir := filepath.Join(forgeDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return err
	}

	fileHooks := map[string]string{
		"auto-compile.sh":     AutoCompileHook,
		"assertion-check.sh":  AssertionCheckHook,
		"experience-check.sh": ExperienceCheckHook,
		"task-verify.sh":      TaskVerifyHook,
		"task-guard.sh":       TaskGuardHook,
		"tool-track.sh":       ToolTrackHook,
		"bash-guard.sh":       BashGuardHook,
		"file-sentinel.sh":    FileSentinelHook,
	}

	for name, content := range fileHooks {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return fmt.Errorf("failed to write hook %s: %w", name, err)
		}
	}
	return nil
}

// HookNames returns the list of hook script filenames managed by Forge.
func HookNames() []string {
	return []string{
		"auto-compile.sh",
		"assertion-check.sh",
		"experience-check.sh",
		"task-verify.sh",
		"task-guard.sh",
		"tool-track.sh",
		"bash-guard.sh",
		"file-sentinel.sh",
	}
}

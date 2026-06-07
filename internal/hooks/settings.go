package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GenerateSettings creates .claude/settings.local.json with hook integration.
func GenerateSettings(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	type hookEntry struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}

	type hookMatcher struct {
		Matcher string       `json:"matcher,omitempty"`
		Hooks   []hookEntry  `json:"hooks"`
	}

	settings := map[string]interface{}{
		"hooks": map[string][]hookMatcher{
			"PostToolUse": {
				{
					Matcher: "Write|Edit",
					Hooks: []hookEntry{
						{Type: "command", Command: "bash .forge/hooks/auto-compile.sh"},
					},
				},
			},
			"PreToolUse": {
				{
					Matcher: "Bash(git commit*)",
					Hooks: []hookEntry{
						{Type: "command", Command: "bash .forge/hooks/assertion-check.sh"},
						{Type: "command", Command: "bash .forge/hooks/experience-check.sh"},
					},
				},
			},
			"Stop": {
				{
					Hooks: []hookEntry{
						{Type: "command", Command: "forge gate --current --silent"},
						{Type: "command", Command: "forge task gate task-verify --silent"},
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

	hooks := map[string]string{
		"auto-compile.sh":     AutoCompileHook,
		"assertion-check.sh":  AssertionCheckHook,
		"experience-check.sh": ExperienceCheckHook,
		"task-verify.sh":      TaskVerifyHook,
	}

	for name, content := range hooks {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return fmt.Errorf("failed to write hook %s: %w", name, err)
		}
	}
	return nil
}

package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// embeddedHooks maps script names (without .sh suffix) to their embedded content.
var embeddedHooks = map[string]string{
	"auto-compile":        AutoCompileHook,
	"assertion-check":     AssertionCheckHook,
	"task-verify":         TaskVerifyHook,
	"review-stop":         ReviewStopHook,
	"task-guard":          TaskGuardHook,
	"bash-guard":          BashGuardHook,
	"hazard-guard":        HazardGuardHook,
	"file-sentinel":       FileSentinelHook,
	"tool-track":          ToolTrackHook,
	"skill-scan":          SkillScanHook,
	"workflow-test-guard": WorkflowTestGuardHook,
}

// EmbeddedContent returns the hook script content for the given name
// (e.g. "auto-compile"). Returns the content and true if found.
func EmbeddedContent(name string) (string, bool) {
	content, ok := embeddedHooks[name]
	return content, ok
}

// ForgeHookSpec is the single source of truth for which forge hooks run at
// which Claude Code tool event. It returns the hooks object exactly as it
// appears under the "hooks" key of .claude/settings.local.json. The plugin-pack
// generator (internal/agentbridge/pluginpack.go) writes the SAME object as
// plugins/forge/hooks/hooks.json, so `claude plugin install forge` produces
// byte-identical hook wiring to `forge init` — mirroring the superpowers
// pattern (one shared payload, per-host thin manifests pointing at it). Any
// wiring change here propagates to both paths; do not duplicate the
// matcher→hook roster elsewhere. Drift is guarded by
// TestPluginPack_HooksMirrorSettings (plugin pack) and TestOpencodePluginWiring
// (opencode's TS roster mirrors this set).
func ForgeHookSpec() map[string]interface{} {
	type hookEntry struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}

	type hookMatcher struct {
		Matcher string      `json:"matcher,omitempty"`
		Hooks   []hookEntry `json:"hooks"`
	}

	return map[string]interface{}{
		"PostToolUse": []hookMatcher{
			{
				Matcher: "Write|Edit",
				Hooks: []hookEntry{
					{Type: "command", Command: "forge hook auto-compile"},
					{Type: "command", Command: "forge hook workflow-test-guard"},
				},
			},
			{
				Matcher: "Bash",
				Hooks: []hookEntry{
					{Type: "command", Command: "forge hook file-sentinel"},
				},
			},
			{
				Matcher: "Read",
				Hooks: []hookEntry{
					{Type: "command", Command: "forge hook tool-track"},
				},
			},
		},
		"PreToolUse": []hookMatcher{
			{
				Matcher: "Write|Edit",
				Hooks: []hookEntry{
					{Type: "command", Command: "forge hook task-guard"},
					{Type: "command", Command: "forge hook assertion-check"},
				},
			},
			{
				Matcher: "Bash",
				Hooks: []hookEntry{
					{Type: "command", Command: "forge hook bash-guard"},
					{Type: "command", Command: "forge hook hazard-guard"},
				},
			},
		},
		"Stop": []hookMatcher{
			{
				Hooks: []hookEntry{
					{Type: "command", Command: "forge gate --current --silent"},
					{Type: "command", Command: "forge hook task-verify"},
					{Type: "command", Command: "forge hook review-stop"},
				},
			},
		},
		"SessionStart": []hookMatcher{
			{
				Hooks: []hookEntry{
					{Type: "command", Command: "forge hook skill-scan"},
				},
			},
		},
	}
}

// GenerateSettings creates .claude/settings.local.json with hook integration.
func GenerateSettings(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	settings := map[string]interface{}{
		"hooks": ForgeHookSpec(),
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
		"auto-compile.sh":        AutoCompileHook,
		"assertion-check.sh":     AssertionCheckHook,
		"task-verify.sh":         TaskVerifyHook,
		"review-stop.sh":         ReviewStopHook,
		"task-guard.sh":          TaskGuardHook,
		"bash-guard.sh":          BashGuardHook,
		"hazard-guard.sh":        HazardGuardHook,
		"file-sentinel.sh":       FileSentinelHook,
		"tool-track.sh":          ToolTrackHook,
		"skill-scan.sh":          SkillScanHook,
		"workflow-test-guard.sh": WorkflowTestGuardHook,
	}

	// Remove stale hook scripts no longer in the embedded set. This directory is
	// Forge-managed (populated only by WriteHookTemplates), so any .sh not in the
	// current set is leftover from a prior version — e.g. read-check.sh /
	// scope-guard.sh / clone-check.sh after they were sunk to skill text, or
	// experience-check.sh after deletion. Without this, removed hooks linger on
	// disk forever (WriteHookTemplates otherwise only writes the current set).
	keep := make(map[string]bool, len(fileHooks))
	for name := range fileHooks {
		keep[name] = true
	}
	if entries, err := os.ReadDir(hooksDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".sh") || keep[name] {
				continue
			}
			os.Remove(filepath.Join(hooksDir, name))
		}
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
		"task-verify.sh",
		"review-stop.sh",
		"task-guard.sh",
		"bash-guard.sh",
		"hazard-guard.sh",
		"file-sentinel.sh",
		"tool-track.sh",
		"skill-scan.sh",
		"workflow-test-guard.sh",
	}
}

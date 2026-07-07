package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/pipeline"
)

// writeSyncState writes a minimal .forge/state.json with the given
// last_sync_version, so autoSync can load it past its first early-return guard.
func writeSyncState(t *testing.T, dir, lastSyncVersion string) {
	t.Helper()
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	state := &pipeline.State{
		PipelineVersion: "2.0",
		Mode:            "medium",
		LastSyncVersion: lastSyncVersion,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(forgeDir, "state.json"), data, 0644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}
}

// hookWritten reports whether autoSync got past its early-return and ran
// WriteHookTemplates (the first sync step), by checking for bash-guard.sh.
// autoSync's later steps (settings/skill generation) may error in a bare temp
// dir, but hook writing happens first and is what the early-return guards.
func hookWritten(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, ".forge", "hooks", "bash-guard.sh"))
	return err == nil
}

// Dev builds must re-sync on every command even when last_sync_version already
// equals "dev", because embedded templates change between dev builds.
// Regression: .forge/hooks/ was observed stuck at 8 files while embed.go had 15.
func TestAutoSyncDevVersionResyncsWhenEqual(t *testing.T) {
	dir := t.TempDir()
	writeSyncState(t, dir, "dev")

	// Ignore error: later sync steps (skill/settings gen) may fail in a bare
	// temp dir, but we only care whether the early-return was taken.
	_ = autoSync(dir, "dev", false)

	if !hookWritten(t, dir) {
		t.Fatal("dev version with last_sync_version=\"dev\" must still resync (early-return should be skipped), but hook was not written")
	}
}

// Non-dev builds must skip resync when last_sync_version already matches —
// this is the normal idempotent behavior for released versions.
func TestAutoSyncReleasedVersionSkipsWhenEqual(t *testing.T) {
	dir := t.TempDir()
	writeSyncState(t, dir, "v0.17.0")

	_ = autoSync(dir, "v0.17.0", false)

	if hookWritten(t, dir) {
		t.Fatal("released version with matching last_sync_version should skip resync (early-return), but hook was written")
	}
}

// Non-dev builds must resync when the version differs (upgrade path).
func TestAutoSyncResyncsWhenVersionDiffers(t *testing.T) {
	dir := t.TempDir()
	writeSyncState(t, dir, "v0.16.0")

	_ = autoSync(dir, "v0.17.0", false)

	if !hookWritten(t, dir) {
		t.Fatal("version mismatch should trigger resync, but hook was not written")
	}
}

// writeSettingsWithStaleBinding writes a settings.local.json that binds
// task-verify under PostToolUse with a wide matcher — the legacy mis-binding
// that caused runaway hook invocations (100+/session) in real heavy-use
// projects before the generator was fixed to bind task-verify only under Stop.
func writeSettingsWithStaleBinding(t *testing.T, dir string) {
	t.Helper()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	stale := `{
  "hooks": {
    "PostToolUse": [
      {"matcher": "Bash|Read|Glob|Skill|Agent", "hooks": [{"type": "command", "command": "forge hook task-verify"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "forge hook task-verify"}]}
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte(stale), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// A settings file carrying a legacy task-verify PostToolUse binding must force a
// resync even when last_sync_version matches the binary — the stale binding has
// to be regenerated away regardless of version. Regression for the DevWorkbench
// case where a stale settings file persisted across upgrades.
func TestAutoSyncHealsStaleBindingEvenWhenVersionMatches(t *testing.T) {
	dir := t.TempDir()
	writeSyncState(t, dir, "v0.17.0")
	writeSettingsWithStaleBinding(t, dir)

	if !settingsHasStaleBinding(dir) {
		t.Fatal("settingsHasStaleBinding should detect task-verify under PostToolUse")
	}

	_ = autoSync(dir, "v0.17.0", false)

	if !hookWritten(t, dir) {
		t.Fatal("stale binding must force resync even when version matches, but hook was not written")
	}
}

// settingsHasStaleBinding must NOT flag a clean settings file (task-verify only
// under Stop) — otherwise every command would pointlessly resync.
func TestSettingsHasStaleBindingFalseForCleanSettings(t *testing.T) {
	dir := t.TempDir()
	writeSyncState(t, dir, "v0.17.0")
	// Generate the canonical clean settings via the generator itself.
	if err := hooks.GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	if settingsHasStaleBinding(dir) {
		t.Fatal("clean generated settings must not be flagged as stale")
	}
}

// When plugin is installed at user level, autoSync must NOT write project-level
// hooks via GenerateSettings — the "write then immediately strip" pattern corrupts
// settings.local.json if the process is interrupted between the two steps.
// dedupeProjectLevelIfPlugin (deferred) still cleans up legacy hooks.
func TestAutoSyncSkipsGenerateSettingsWhenPluginInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)

	dir := t.TempDir()
	writeSyncState(t, dir, "v0.16.0") // version mismatch → would trigger full sync

	// Pre-populate settings.local.json with user fields only (no hooks).
	userSettings := `{"env":{"KEY":"val"},"model":"gpt-4"}`
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte(userSettings), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// autoSync with version mismatch — would normally call GenerateSettings.
	_ = autoSync(dir, "v0.17.0", false)

	// Verify: settings.local.json must be untouched (no hooks written).
	data, err := os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings after autoSync: %v", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if _, hasHooks := parsed["hooks"]; hasHooks {
		t.Error("plugin installed: GenerateSettings must not write hooks to settings.local.json")
	}
	if string(parsed["env"]) != `{"KEY":"val"}` {
		t.Errorf("user env field was modified: got %s", string(parsed["env"]))
	}
	if string(parsed["model"]) != `"gpt-4"` {
		t.Errorf("user model field was modified: got %s", string(parsed["model"]))
	}
}

// A missing state.json must not silently skip sync — old code returned nil
// here, leaving stale settings unhealed. With the heal-on-bad-state fix, the
// state-independent artifacts (hooks + settings) still regenerate so a stale
// binding gets repaired even when state.json is gone.
func TestAutoSyncHealsArtifactsWhenStateMissing(t *testing.T) {
	dir := t.TempDir()
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	// Intentionally NO state.json — simulates a half-initialized or corrupted project.

	_ = autoSync(dir, "v0.17.0", false)

	if !hookWritten(t, dir) {
		t.Fatal("missing state.json must still heal hooks+settings, but hook was not written")
	}
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Harness/forge/internal/pipeline"
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
	_ = autoSync(dir, "dev")

	if !hookWritten(t, dir) {
		t.Fatal("dev version with last_sync_version=\"dev\" must still resync (early-return should be skipped), but hook was not written")
	}
}

// Non-dev builds must skip resync when last_sync_version already matches —
// this is the normal idempotent behavior for released versions.
func TestAutoSyncReleasedVersionSkipsWhenEqual(t *testing.T) {
	dir := t.TempDir()
	writeSyncState(t, dir, "v0.17.0")

	_ = autoSync(dir, "v0.17.0")

	if hookWritten(t, dir) {
		t.Fatal("released version with matching last_sync_version should skip resync (early-return), but hook was written")
	}
}

// Non-dev builds must resync when the version differs (upgrade path).
func TestAutoSyncResyncsWhenVersionDiffers(t *testing.T) {
	dir := t.TempDir()
	writeSyncState(t, dir, "v0.16.0")

	_ = autoSync(dir, "v0.17.0")

	if !hookWritten(t, dir) {
		t.Fatal("version mismatch should trigger resync, but hook was not written")
	}
}

package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpencodeTranslator_Translate_CreatesFile verifies Translate installs forge.ts
// into the XDG global plugin dir even when the directory tree does not exist yet.
func TestOpencodeTranslator_Translate_CreatesFile(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, ".config", "opencode", "plugins", "forge.ts")

	if err := (&OpencodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("forge.ts not created at XDG global plugin path: %v", err)
	}
	if !strings.Contains(string(data), "tool.execute.before") {
		t.Error("forge.ts missing the pre-tool entry point")
	}
}

// TestOpencodeTranslator_XDGConfigHomeRespected pins the XDG convention: with
// XDG_CONFIG_HOME set the plugin lands under it; without it the fallback is
// ~/.config/opencode/plugins/forge.ts.
func TestOpencodeTranslator_XDGConfigHomeRespected(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path, err := OpencodePluginPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(xdg, "opencode", "plugins", "forge.ts") {
		t.Errorf("XDG_CONFIG_HOME not respected: %s", path)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	path, err = OpencodePluginPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".config", "opencode", "plugins", "forge.ts") {
		t.Errorf("~/.config fallback wrong: %s", path)
	}
}

// TestOpencodeTranslator_Idempotent verifies Translate is a byte-identical
// overwrite (forge.ts is forge-owned — overwrite, not merge).
func TestOpencodeTranslator_Idempotent(t *testing.T) {
	home := isolateHome(t)
	assertTranslateIdempotent(t, &OpencodeTranslator{},
		filepath.Join(home, ".config", "opencode", "plugins", "forge.ts"))
}

// TestStripOpenCodeUserPlugin covers the strip roundtrip: Translate then Strip
// deletes the forge-owned file; a second Strip and a missing file are clean no-ops.
func TestStripOpenCodeUserPlugin(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, ".config", "opencode", "plugins", "forge.ts")

	// Missing file → clean no-op.
	removed, err := StripOpenCodeUserPlugin()
	if err != nil || removed {
		t.Fatalf("missing file: removed=%v err=%v, want false/nil", removed, err)
	}

	if err := (&OpencodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	removed, err = StripOpenCodeUserPlugin()
	if err != nil || !removed {
		t.Fatalf("strip after Translate: removed=%v err=%v, want true/nil", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("forge.ts still exists after strip (err=%v)", err)
	}

	// Second strip → no-op.
	removed, err = StripOpenCodeUserPlugin()
	if err != nil || removed {
		t.Fatalf("second strip: removed=%v err=%v, want false/nil", removed, err)
	}
}

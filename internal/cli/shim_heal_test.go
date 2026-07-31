package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// seedNpmLayout builds <tmp>/prefix/{forge, node_modules/@agent_forge/forge-win32-x64/bin/forge.exe}
// with the given shim/exe contents and returns (prefix, exePath).
func seedNpmLayout(t *testing.T, shimContent, exeContent string) (string, string) {
	t.Helper()
	prefix := t.TempDir()
	exePath := filepath.Join(prefix, "node_modules", "@agent_forge", "forge-win32-x64", "bin", "forge.exe")
	if err := os.MkdirAll(filepath.Dir(exePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath, []byte(exeContent), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "forge"), []byte(shimContent), 0644); err != nil {
		t.Fatal(err)
	}
	return prefix, exePath
}

// TestHealNpmShim_ReplacesFragileShim pins the core behavior: an sh-script shim in an
// npm layout is replaced by the real binary, idempotently.
func TestHealNpmShim_ReplacesFragileShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shim heal is Windows-only")
	}
	prefix, exePath := seedNpmLayout(t, "#!/bin/sh\nbasedir=$(dirname \"$0\")\nexec \"$basedir/node\" \"$basedir/run.js\" \"$@\"\n", "MZ-fake-binary")

	healed, err := healNpmShim(exePath)
	if err != nil || !healed {
		t.Fatalf("healNpmShim = (%v, %v), want (true, nil)", healed, err)
	}
	data, err := os.ReadFile(filepath.Join(prefix, "forge"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "MZ-fake-binary" {
		t.Errorf("shim content = %q, want the real binary copy", string(data))
	}

	// Idempotent: already-binary shim is a clean no-op.
	healed, err = healNpmShim(exePath)
	if err != nil || healed {
		t.Errorf("second healNpmShim = (%v, %v), want (false, nil)", healed, err)
	}
}

// TestHealNpmShim_NestedLayout pins the real-world nested install (platform subpackage
// under the main package): the heal must reach the OUTERMOST prefix's forge shim, and
// must not write anything into the main package dir.
func TestHealNpmShim_NestedLayout(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shim heal is Windows-only")
	}
	prefix := t.TempDir()
	shimContent := "#!/bin/sh\nbasedir=$(dirname \"$0\")\nexec \"$basedir/node\" \"$basedir/run.js\" \"$@\"\n"
	exePath := filepath.Join(prefix, "node_modules", "@agent_forge", "forge", "node_modules", "@agent_forge", "forge-win32-x64", "bin", "forge.exe")
	if err := os.MkdirAll(filepath.Dir(exePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath, []byte("MZ-fake-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "forge"), []byte(shimContent), 0644); err != nil {
		t.Fatal(err)
	}

	healed, err := healNpmShim(exePath)
	if err != nil || !healed {
		t.Fatalf("nested healNpmShim = (%v, %v), want (true, nil)", healed, err)
	}
	data, _ := os.ReadFile(filepath.Join(prefix, "forge"))
	if string(data) != "MZ-fake-binary" {
		t.Errorf("outermost prefix shim not replaced, content = %q", string(data))
	}
	// Nothing may be written inside the main package dir.
	mainPkgForge := filepath.Join(prefix, "node_modules", "@agent_forge", "forge", "forge")
	if _, err := os.Stat(mainPkgForge); !os.IsNotExist(err) {
		t.Errorf("heal must not write into the main package dir: %s exists", mainPkgForge)
	}
}

// TestHealNpmShim_UserScriptUntouched pins the signature sniff: a script in the shim
// slot that is NOT npm's cmd-shim template (no $basedir / node_modules reference) must
// never be replaced.
func TestHealNpmShim_UserScriptUntouched(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shim heal is Windows-only")
	}
	userScript := "#!/bin/sh\necho my own wrapper\n"
	prefix, exePath := seedNpmLayout(t, userScript, "MZ-fake-binary")

	healed, err := healNpmShim(exePath)
	if err != nil || healed {
		t.Fatalf("user script healNpmShim = (%v, %v), want (false, nil)", healed, err)
	}
	data, _ := os.ReadFile(filepath.Join(prefix, "forge"))
	if string(data) != userScript {
		t.Errorf("user script was modified:\n%q", string(data))
	}
}

// TestHealNpmShim_NonNpmLayout verifies GitHub-release / go-install binaries never
// trigger the heal (no node_modules ancestor).
func TestHealNpmShim_NonNpmLayout(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shim heal is Windows-only")
	}
	dir := t.TempDir()
	exePath := filepath.Join(dir, "forge.exe")
	if err := os.WriteFile(exePath, []byte("MZ"), 0755); err != nil {
		t.Fatal(err)
	}
	healed, err := healNpmShim(exePath)
	if err != nil || healed {
		t.Errorf("non-npm layout healNpmShim = (%v, %v), want (false, nil)", healed, err)
	}
}

// TestNpmPrefixFor pins the node_modules walk (scoped and unscoped packages).
func TestNpmPrefixFor(t *testing.T) {
	p := filepath.Join("D:", "nodejs", "npm-global", "node_modules", "@agent_forge", "forge-win32-x64", "bin", "forge.exe")
	got, ok := npmPrefixFor(p)
	if !ok || got != filepath.Join("D:", "nodejs", "npm-global") {
		t.Errorf("npmPrefixFor(scoped) = (%q, %v)", got, ok)
	}
	if _, ok := npmPrefixFor(filepath.Join("C:", "tools", "forge.exe")); ok {
		t.Error("no node_modules ancestor must be ok=false")
	}
}

// TestNpmPrefixFor_Nested pins the real-world nested layout (npm hoisting the platform
// subpackage under the main package): the topmost node_modules wins, so the prefix is
// the npm-global root that actually holds the bin shims — not the main package dir.
func TestNpmPrefixFor_Nested(t *testing.T) {
	p := filepath.Join("D:", "nodejs", "npm-global", "node_modules", "@agent_forge", "forge", "node_modules", "@agent_forge", "forge-win32-x64", "bin", "forge.exe")
	got, ok := npmPrefixFor(p)
	want := filepath.Join("D:", "nodejs", "npm-global")
	if !ok || got != want {
		t.Errorf("npmPrefixFor(nested) = (%q, %v), want (%q, true)", got, ok, want)
	}
}

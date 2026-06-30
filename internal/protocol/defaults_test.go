package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultProtocolSmall(t *testing.T) {
	p := DefaultProtocol("small")
	if p.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", p.Version)
	}
	if len(p.Standards) != 3 {
		t.Errorf("Standards count = %d, want 3", len(p.Standards))
	}
	if len(p.SessionRules) != 3 {
		t.Errorf("SessionRules count = %d, want 3 for small mode", len(p.SessionRules))
	}
}

func TestDefaultProtocolMedium(t *testing.T) {
	p := DefaultProtocol("medium")
	if len(p.SessionRules) != 4 {
		t.Errorf("SessionRules count = %d, want 4 for medium mode", len(p.SessionRules))
	}
	// Last rule should be the design-for-complex rule
	last := p.SessionRules[len(p.SessionRules)-1]
	if last.ID != "design-for-complex" {
		t.Errorf("Last rule ID = %q, want design-for-complex", last.ID)
	}
}

func TestDefaultProtocolLarge(t *testing.T) {
	p := DefaultProtocol("large")
	if len(p.SessionRules) != 5 {
		t.Errorf("SessionRules count = %d, want 5 for large mode", len(p.SessionRules))
	}
}

func TestDefaultProtocolAllStandardsEnabled(t *testing.T) {
	p := DefaultProtocol("medium")
	for _, s := range p.Standards {
		if !s.Enabled {
			t.Errorf("Standard %q should be enabled by default", s.ID)
		}
	}
}

func TestErrorSeverityStandards(t *testing.T) {
	// v0.25 advisory rewrite: compile-gate and no-assertion-weaken dropped from
	// "error" to "warning" because auto-compile.sh / assertion-check.sh no longer
	// block — they only advise (agent self-checks). No shipped standard sits at
	// error severity, so ErrorSeverityStandards returns nothing. This guards
	// against severity drifting back to "error" while the Description says
	// "advisory" — the half-fix that left Severity untouched last time.
	p := DefaultProtocol("small")
	errs := p.ErrorSeverityStandards()
	for _, s := range errs {
		t.Errorf("standard %q still at severity %q — advisory standards must be warning/info, not error (v0.25 advisory rewrite)", s.ID, s.Severity)
	}
}

func TestMandatoryRules(t *testing.T) {
	p := DefaultProtocol("small")
	mandatory := p.MandatoryRules()
	if len(mandatory) != 3 {
		t.Errorf("Mandatory rules = %d, want 3", len(mandatory))
	}
	for _, r := range mandatory {
		if !r.Mandatory {
			t.Errorf("Rule %q should be mandatory", r.ID)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	original := DefaultProtocol("medium")

	if err := Save(dir, original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version mismatch: got %q, want %q", loaded.Version, original.Version)
	}
	if len(loaded.Standards) != len(original.Standards) {
		t.Errorf("Standards count mismatch: got %d, want %d", len(loaded.Standards), len(original.Standards))
	}
	if len(loaded.SessionRules) != len(original.SessionRules) {
		t.Errorf("SessionRules count mismatch: got %d, want %d", len(loaded.SessionRules), len(original.SessionRules))
	}

	// Check specific standards
	foundCompile := false
	foundTestAccompany := false
	for _, s := range loaded.Standards {
		if s.ID == "compile-gate" {
			foundCompile = true
			if s.EnforceHook != "auto-compile.sh" {
				t.Errorf("compile-gate hook = %q, want auto-compile.sh", s.EnforceHook)
			}
		}
		if s.ID == "test-accompany" {
			foundTestAccompany = true
			// test-accompany enforcement lives in the task-verify gate
			// (taskpipeline/testcoverage.go), not a runtime hook — the old
			// test-coverage-check.sh hook was deleted as advisory noise.
			if s.EnforceHook != "" {
				t.Errorf("test-accompany EnforceHook = %q, want empty (enforced by task-verify gate, not a hook)", s.EnforceHook)
			}
		}
	}
	if !foundCompile {
		t.Error("compile-gate standard not found in loaded protocol")
	}
	if !foundTestAccompany {
		t.Error("test-accompany standard not found in loaded protocol")
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing protocol.yml")
	}
}

func TestSaveCreatesForgeDir(t *testing.T) {
	dir := t.TempDir()
	// .forge/ doesn't exist yet
	forgeDir := filepath.Join(dir, ".forge")
	if _, err := os.Stat(forgeDir); !os.IsNotExist(err) {
		t.Fatal(".forge/ should not exist yet")
	}

	p := DefaultProtocol("small")
	if err := Save(dir, p); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(forgeDir, "protocol.yml")); err != nil {
		t.Fatalf("protocol.yml not created: %v", err)
	}
}

// TestDefaultProtocolStandardsAreAdvisory guards the v0.25 advisory rewrite: the
// compile-gate and no-assertion-weaken standard Descriptions must reflect that
// the hooks only advise (agent self-checks), because auto-compile.sh and
// assertion-check.sh no longer block. The EnforceHook field is retained as a
// display string, but the Description semantics shifted to advisory so the
// protocol.yml a project ships matches the non-blocking hook behavior.
func TestDefaultProtocolStandardsAreAdvisory(t *testing.T) {
	p := DefaultProtocol("medium")
	for _, s := range p.Standards {
		switch s.ID {
		case "compile-gate", "no-assertion-weaken":
			if !strings.Contains(s.Description, "advisory") {
				t.Errorf("standard %q Description = %q, must mention advisory (v0.25: hooks no longer block)", s.ID, s.Description)
			}
			// Severity must match the advisory behavior. auto-compile.sh /
			// assertion-check.sh only advise (agent self-checks, non-blocking),
			// so shipping them at "error" severity is a contradiction: it renders
			// 🔴 in SKILL.md and the Cursor/Windsurf/Copilot bridges, misleading
			// users into thinking the hooks hard-block. Guards the v0.25 half-fix
			// that left Severity="error" after the Description became advisory.
			if s.Severity == "error" {
				t.Errorf("standard %q Severity = %q, must be warning/info to match advisory Description", s.ID, s.Severity)
			}
		}
	}
}

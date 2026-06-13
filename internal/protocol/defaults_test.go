package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultProtocolSmall(t *testing.T) {
	p := DefaultProtocol("small")
	if p.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", p.Version)
	}
	if len(p.Standards) != 6 {
		t.Errorf("Standards count = %d, want 6", len(p.Standards))
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
	p := DefaultProtocol("small")
	errs := p.ErrorSeverityStandards()
	if len(errs) != 4 {
		t.Errorf("Error severity standards = %d, want 4", len(errs))
	}
	for _, s := range errs {
		if s.Severity != "error" {
			t.Errorf("Got severity %q for standard %q, want error", s.Severity, s.ID)
		}
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

	// Check a specific standard
	found := false
	for _, s := range loaded.Standards {
		if s.ID == "compile-gate" {
			found = true
			if s.EnforceHook != "auto-compile.sh" {
				t.Errorf("compile-gate hook = %q, want auto-compile.sh", s.EnforceHook)
			}
		}
	}
	if !found {
		t.Error("compile-gate standard not found in loaded protocol")
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

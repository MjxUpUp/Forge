package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

func TestDefaultProtocol(t *testing.T) {
	p := DefaultProtocol()
	if p.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", p.Version)
	}
	if len(p.Standards) != 3 {
		t.Errorf("Standards count = %d, want 3", len(p.Standards))
	}
	if len(p.SessionRules) != 4 {
		t.Errorf("SessionRules count = %d, want 4", len(p.SessionRules))
	}
	// Last rule should be the design-for-complex rule (moved into the base set
	// unconditionally after the project-pipeline mode parameter was removed).
	last := p.SessionRules[len(p.SessionRules)-1]
	if last.ID != "design-for-complex" {
		t.Errorf("Last rule ID = %q, want design-for-complex", last.ID)
	}
	// Mandatory-rule count (formerly via the deleted Protocol.MandatoryRules dead-code
	// method, now inlined): 3 of the 4 default rules are mandatory, and every rule
	// flagged Mandatory is actually true by construction.
	//
	// mandatory 规则计数（原先经已删除的死代码方法 Protocol.MandatoryRules 断言，
	// 现改为内联遍历）：默认 4 条规则中 3 条 mandatory。
	mandatory := 0
	for _, r := range p.SessionRules {
		if r.Mandatory {
			mandatory++
		}
	}
	if mandatory != 3 {
		t.Errorf("Mandatory rules = %d, want 3", mandatory)
	}
}

func TestDefaultProtocolAllStandardsEnabled(t *testing.T) {
	p := DefaultProtocol()
	for _, s := range p.Standards {
		if !s.Enabled {
			t.Errorf("Standard %q should be enabled by default", s.ID)
		}
	}
}

// TestNoStandardAtErrorSeverity (formerly via the deleted Protocol.ErrorSeverityStandards
// dead-code method, now inlined): no shipped standard sits at "error" severity — the
// v0.25 advisory rewrite dropped compile-gate and no-assertion-weaken to warning because
// auto-compile.sh / assertion-check.sh no longer block, they only advise. Guards against
// severity drifting back to "error" while the Description says "advisory" — the half-fix
// that left Severity untouched last time.
//
// TestNoStandardAtErrorSeverity（原先经已删除的死代码方法 Protocol.ErrorSeverityStandards
// 断言，现改为内联遍历）：出厂标准不得处于 error 档——v0.25 advisory 重写把
// compile-gate 与 no-assertion-weaken 降为 warning，因为 auto-compile.sh /
// assertion-check.sh 不再阻断、只提醒。防 severity 漂回 error 而 Description 仍写
// advisory——即上次只改一半留下的 Severity 未动问题。
func TestNoStandardAtErrorSeverity(t *testing.T) {
	p := DefaultProtocol()
	for _, s := range p.Standards {
		if s.Enabled && s.Severity == "error" {
			t.Errorf("standard %q still at severity %q — advisory standards must be warning/info, not error (v0.25 advisory rewrite)", s.ID, s.Severity)
		}
	}
}

func TestSaveProjectLevelAndLoad(t *testing.T) {
	dir := t.TempDir()
	original := DefaultProtocol()

	if err := SaveProjectLevel(dir, original); err != nil {
		t.Fatalf("SaveProjectLevel failed: %v", err)
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
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing protocol.yml")
	}
}

// TestSaveDataDir_ZeroProjectWrite: SaveDataDir creates the user-level
// DataDir copy (via util.AtomicWrite) — and must NOT create a project-level .forge/.
//
// TestSaveDataDir_ZeroProjectWrite：SaveDataDir 创建用户级 DataDir 副本
// （经 util.AtomicWrite）——且不得创建项目级 .forge/。
func TestSaveDataDir_ZeroProjectWrite(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	// .forge/ doesn't exist yet
	forgeDir := filepath.Join(dir, ".forge")
	if _, err := os.Stat(forgeDir); !os.IsNotExist(err) {
		t.Fatal(".forge/ should not exist yet")
	}

	p := DefaultProtocol()
	if err := SaveDataDir(dir, p); err != nil {
		t.Fatalf("SaveDataDir failed: %v", err)
	}

	if _, err := os.Stat(forgeDir); !os.IsNotExist(err) {
		t.Fatal("SaveDataDir must not create project-level .forge/ (zero-project-write)")
	}
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")); err != nil {
		t.Fatalf("protocol.yml not created in DataDir: %v", err)
	}
}

// TestDefaultProtocolStandardsAreAdvisory guards the v0.25 advisory rewrite: the
// compile-gate and no-assertion-weaken standard Descriptions must reflect that
// the hooks only advise (agent self-checks), because auto-compile.sh and
// assertion-check.sh no longer block. The EnforceHook field is retained as a
// display string, but the Description semantics shifted to advisory so the
// protocol.yml a project ships matches the non-blocking hook behavior.
func TestDefaultProtocolStandardsAreAdvisory(t *testing.T) {
	p := DefaultProtocol()
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

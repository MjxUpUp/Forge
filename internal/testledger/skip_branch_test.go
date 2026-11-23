package testledger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/testledger"
)

const skipFixture = `module example.com/skipfx

go 1.21
`

const skipSrc = `package skipfx

import (
	"runtime"
	"testing"
)

func TestSwitchGuard(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		t.Skip("windows only")
	}
}

func TestLoopGuard(t *testing.T) {
	for i := 0; i < 1; i++ {
		t.Skip("loop guard")
	}
}

func skipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
}

func TestViaHelper(t *testing.T) {
	skipIfShort(t)
}

func TestBare(t *testing.T) {
	t.Skip("gone")
}
`

// TestSkipClassificationEdgeCases 钉住"什么才算被跳过的测试"的边界：
// switch/循环里的 Skip 是条件闸门，经 helper 的 Skip 也算条件闸门（平时照常跑），
// 只有函数体顶层无条件的那一句才是"被静默关掉"。
func TestSkipClassificationEdgeCases(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(skipFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skipfx_test.go"), []byte(skipSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	decls, err := testledger.ScanDecls(dir)
	if err != nil {
		t.Fatalf("ScanDecls: %v", err)
	}
	byName := map[string]testledger.Decl{}
	for _, d := range decls {
		byName[d.Test] = d
	}
	if len(decls) != 4 {
		t.Fatalf("want 4 decls, got %d: %+v", len(decls), decls)
	}
	cases := []struct {
		test    string
		guarded bool
	}{
		{"TestSwitchGuard", true},
		{"TestLoopGuard", true},
		{"TestViaHelper", true},
		{"TestBare", false},
	}
	for _, c := range cases {
		d, ok := byName[c.test]
		if !ok {
			t.Fatalf("missing decl %s", c.test)
		}
		if !d.HasSkip {
			t.Errorf("%s: HasSkip = false, want true", c.test)
		}
		if d.SkipGuarded != c.guarded {
			t.Errorf("%s: SkipGuarded = %v, want %v (reason=%q)", c.test, d.SkipGuarded, c.guarded, d.SkipReason)
		}
	}
	if d := byName["TestViaHelper"]; !strings.Contains(d.SkipReason, "helper") {
		t.Errorf("TestViaHelper reason = %q, want mention of helper", d.SkipReason)
	}

	ledger := testledger.Build(decls, nil, testledger.Options{})
	violations := ledger.Violations()
	if len(violations) != 1 {
		t.Fatalf("want exactly 1 violation (the bare skip), got %d: %v", len(violations), violations)
	}
	if !strings.Contains(violations[0], "TestBare") {
		t.Errorf("violation should blame TestBare, got %q", violations[0])
	}
	if ledger.Counts[testledger.StateSkipped] != 1 {
		t.Errorf("want exactly the bare skip counted as skipped, got %+v", ledger.Counts)
	}
	for _, e := range ledger.Entries {
		switch e.Test {
		case "TestSwitchGuard", "TestLoopGuard", "TestViaHelper":
			if e.State == testledger.StateSkipped {
				t.Errorf("guarded skip %s leaked into skipped state", e.Test)
			}
			if !e.GuardedSkip {
				t.Errorf("entry %s: GuardedSkip = false, want true", e.Test)
			}
		}
	}
}

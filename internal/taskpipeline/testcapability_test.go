package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestCheckTestCapability_GoUnitTests(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module test\n\ngo 1.21\n")
	writeRepoFile(t, root, "pkg/foo.go", "package pkg\n\nfunc Foo() int { return 1 }\n")
	writeRepoFile(t, root, "pkg/foo_test.go", "package pkg\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n")

	cap := CheckTestCapability(root)

	if !cap.HasTests {
		t.Fatalf("HasTests=false, want true (pkg/foo_test.go present)")
	}
	if cap.UnitCount < 1 {
		t.Errorf("UnitCount=%d, want >=1", cap.UnitCount)
	}
	if cap.E2ECount != 0 {
		t.Errorf("E2ECount=%d, want 0", cap.E2ECount)
	}
	if cap.Stack != "go" {
		t.Errorf("Stack=%q, want \"go\"", cap.Stack)
	}
	if cap.Recommend != "go test ./..." {
		t.Errorf("Recommend=%q, want \"go test ./...\"", cap.Recommend)
	}
	if !strings.Contains(cap.Advisory(), "go test ./...") {
		t.Errorf("Advisory=%q missing recommended command", cap.Advisory())
	}
	if !strings.Contains(cap.Detail(), "go") {
		t.Errorf("Detail=%q missing stack", cap.Detail())
	}
}

func TestCheckTestCapability_E2EClassified(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module test\n\ngo 1.21\n")
	writeRepoFile(t, root, "integration/handler_test.go", "package integration\n\nimport \"testing\"\n\nfunc TestHandler(t *testing.T) {}\n")
	writeRepoFile(t, root, "playwright/login.e2e.test.ts", "test('login', () => {})\n")

	cap := CheckTestCapability(root)

	if !cap.HasTests {
		t.Fatalf("HasTests=false, want true")
	}
	if cap.E2ECount < 2 {
		t.Errorf("E2ECount=%d, want >=2 (integration/ + .e2e.)", cap.E2ECount)
	}
	if cap.UnitCount != 0 {
		t.Errorf("UnitCount=%d, want 0 — all test files here are e2e", cap.UnitCount)
	}
	if !strings.Contains(cap.Advisory(), "e2e/integration") {
		t.Errorf("Advisory=%q should mention e2e/integration count", cap.Advisory())
	}
}

func TestCheckTestCapability_NoTests(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module test\n\ngo 1.21\n")
	writeRepoFile(t, root, "pkg/foo.go", "package pkg\n\nfunc Foo() int { return 1 }\n")

	cap := CheckTestCapability(root)

	if cap.HasTests {
		t.Errorf("HasTests=true, want false (source only, no tests)")
	}
	if cap.UnitCount != 0 || cap.E2ECount != 0 {
		t.Errorf("counts = %d/%d, want 0/0", cap.UnitCount, cap.E2ECount)
	}
	if !strings.Contains(cap.Detail(), "no test files") {
		t.Errorf("Detail=%q should say no test files", cap.Detail())
	}
	// Advisory() is only called when HasTests; verify it doesn't claim tests exist.
	if strings.Contains(cap.Advisory(), "仓库存在测试") {
		t.Errorf("Advisory=%q should not claim tests exist when none found", cap.Advisory())
	}
}

func TestCheckTestCapability_Disabled(t *testing.T) {
	t.Setenv("FORGE_TEST_COVERAGE", "disable")
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module test\n\ngo 1.21\n")
	writeRepoFile(t, root, "pkg/foo_test.go", "package pkg\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n")

	cap := CheckTestCapability(root)

	if !cap.Disabled {
		t.Fatalf("Disabled=false, want true (FORGE_TEST_COVERAGE=disable skips scan)")
	}
	if cap.HasTests {
		t.Errorf("HasTests=true under disable, want false (scan skipped)")
	}
	if !strings.Contains(cap.Detail(), "disable") {
		t.Errorf("Detail=%q should note disable", cap.Detail())
	}
}

func TestCheckTestCapability_NodeVitestStack(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "package.json", `{
		"scripts": {"test": "vitest run"},
		"devDependencies": {"vitest": "^1.0.0"}
	}`)
	writeRepoFile(t, root, "src/app.test.ts", "test('app', () => {})\n")

	cap := CheckTestCapability(root)

	if cap.Stack != "node" {
		t.Errorf("Stack=%q, want \"node\"", cap.Stack)
	}
	if cap.Recommend != "npx vitest run" {
		t.Errorf("Recommend=%q, want \"npx vitest run\" (vitest detected)", cap.Recommend)
	}
	if !cap.HasTests {
		t.Errorf("HasTests=false, want true (app.test.ts present)")
	}
}

func TestCheckTestCapability_GitLsFilesPath(t *testing.T) {
	// Exercise the git ls-files branch in isolation: a freshly-init'd temp repo
	// is a real git repo, so CheckTestCapability takes the primary (git) path
	// rather than the filepath.Walk fallback. No t.Skip — runs everywhere.
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	writeRepoFile(t, root, "go.mod", "module test\n\ngo 1.21\n")
	writeRepoFile(t, root, "internal/handler/handler_test.go",
		"package handler\n\nimport \"testing\"\n\nfunc TestHandler(t *testing.T) {}\n")
	// Stage so ls-files reports them as tracked (index entries; no commit needed).
	if out, err := exec.Command("git", "-C", root, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	cap := CheckTestCapability(root)

	if !cap.HasTests {
		t.Fatalf("HasTests=false, want true (handler_test.go is tracked)")
	}
	if cap.UnitCount < 1 {
		t.Errorf("UnitCount=%d, want >=1", cap.UnitCount)
	}
	if cap.Stack != "go" {
		t.Errorf("Stack=%q, want \"go\"", cap.Stack)
	}
	if cap.Recommend != "go test ./..." {
		t.Errorf("Recommend=%q, want \"go test ./...\"", cap.Recommend)
	}
	if cap.Scanned == 0 {
		t.Errorf("Scanned=0, want >0 (git ls-files returned tracked files)")
	}
}

// The next three cover the detectStackAndCmd branches the go/node-vitest tests
// don't reach (rust, python, and a node project whose only test script is the
// npm placeholder). Without them those recommendation branches could regress
// silently — coverage-erosion for the stack switch.

func TestCheckTestCapability_RustStack(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
	writeRepoFile(t, root, "src/math.rs", "pub fn add(a: i32, b: i32) -> i32 { a + b }\n")
	writeRepoFile(t, root, "src/math_test.rs", "#[test]\nfn add_works() { assert_eq!(add(1, 2), 3); }\n")

	cap := CheckTestCapability(root)

	if cap.Stack != "rust" {
		t.Errorf("Stack=%q, want \"rust\"", cap.Stack)
	}
	if cap.Recommend != "cargo test" {
		t.Errorf("Recommend=%q, want \"cargo test\"", cap.Recommend)
	}
	if !cap.HasTests {
		t.Errorf("HasTests=false, want true (math_test.rs present)")
	}
}

func TestCheckTestCapability_PythonStack(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "pytest.ini", "[pytest]\ntestpaths = tests\n")
	writeRepoFile(t, root, "tests/test_app.py", "def test_app():\n    assert True\n")

	cap := CheckTestCapability(root)

	if cap.Stack != "python" {
		t.Errorf("Stack=%q, want \"python\"", cap.Stack)
	}
	if cap.Recommend != "python -m pytest" {
		t.Errorf("Recommend=%q, want \"python -m pytest\"", cap.Recommend)
	}
	if !cap.HasTests {
		t.Errorf("HasTests=false, want true (tests/test_app.py present)")
	}
}

func TestCheckTestCapability_NodePlaceholderNoRunner(t *testing.T) {
	// package.json with only npm init's placeholder test script: Stack is node,
	// but there is no real runner, so Recommend must be "" (recommending npm test
	// here would run `echo "Error: no test specified" && exit 1` and just fail).
	root := t.TempDir()
	writeRepoFile(t, root, "package.json", `{
		"name": "x",
		"scripts": {
			"test": "echo \"Error: no test specified\" && exit 1"
		}
	}`)

	cap := CheckTestCapability(root)

	if cap.Stack != "node" {
		t.Errorf("Stack=%q, want \"node\"", cap.Stack)
	}
	if cap.Recommend != "" {
		t.Errorf("Recommend=%q, want \"\" — npm placeholder is not a real runner", cap.Recommend)
	}
}

func TestCheckTestCapability_NodeGenericTestScript(t *testing.T) {
	// A configured, non-placeholder test script with no framework keyword →
	// nodeTestCmd should fall through to a generic "npm test".
	root := t.TempDir()
	writeRepoFile(t, root, "package.json", `{
		"name": "x",
		"scripts": {
			"test": "node ./bin/run-tests.js"
		}
	}`)

	cap := CheckTestCapability(root)

	if cap.Stack != "node" {
		t.Errorf("Stack=%q, want \"node\"", cap.Stack)
	}
	if cap.Recommend != "npm test" {
		t.Errorf("Recommend=%q, want \"npm test\"", cap.Recommend)
	}
}

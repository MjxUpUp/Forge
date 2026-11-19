package change

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclaredPaths(t *testing.T) {
	md := "# Change\n\n## 改动\n\n1. `internal/foo/bar.go` 加实现\n2. `forge change drift` 命令\n3. `docs/reference/cli.md` 一行\n\n## 影响\n\n`internal/other.go` 不在改动段里\n"
	got := DeclaredPaths(md)
	want := []string{"internal/foo/bar.go", "docs/reference/cli.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("DeclaredPaths = %v, want %v", got, want)
	}
}

func TestDrift_ReportsUndeclaredAndUntouched(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	mustWrite := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("spec/changes/demo/goal.md", "# Goal\n\n## Objective\n\nx\n\n## Out of Scope\n\n-\n\n## Check\n\n- ok\n")
	mustWrite("spec/changes/demo/change.md", "# Change\n\n## 改动\n\n1. `internal/declared.go`\n2. `internal/never.go`\n")
	mustWrite("spec/changes/demo/acceptance.md", "# Acceptance\n\n```bash\ntrue\n```\n")
	mustWrite("internal/declared.go", "package internal\n")
	git("add", "-A")
	git("commit", "-qm", "base")

	// 实际改了声明过的、声明过但没改的、以及完全没声明的文件。
	mustWrite("internal/declared.go", "package internal\n\n// changed\n")
	mustWrite("internal/sneaky.go", "package internal\n")

	res, err := Drift(dir, "demo")
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if len(res.Undeclared) != 1 || res.Undeclared[0] != "internal/sneaky.go" {
		t.Fatalf("Undeclared = %v, want [internal/sneaky.go]", res.Undeclared)
	}
	if len(res.Untouched) != 1 || res.Untouched[0] != "internal/never.go" {
		t.Fatalf("Untouched = %v, want [internal/never.go]", res.Untouched)
	}
}

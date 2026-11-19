package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupEvalSuiteRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, _, code := runForge(t, dir, "init"); code != 0 {
		t.Fatalf("forge init failed: %d", code)
	}
	writeArchiveAcceptance(t, dir, "20260101-alpha", "```bash\nprintf 'alpha-ok'\n```\n")
	writeArchiveAcceptance(t, dir, "20260102-beta", "```bash\n# suite: skip\ngo test ./...\n```\n\n```bash\nprintf 'beta-ok'\n```\n")
	return dir
}

func writeArchiveAcceptance(t *testing.T, root, dirName, body string) {
	t.Helper()
	d := filepath.Join(root, "spec", "changes", "archive", dirName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "acceptance.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEvalSuite_RunsArchivedCriteria(t *testing.T) {
	dir := setupEvalSuiteRepo(t)
	out, _, code := runForgeStreams(t, dir, "eval", "suite")
	if code != 0 {
		t.Fatalf("want 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "回归基线：2/2 通过") {
		t.Fatalf("missing summary: %s", out)
	}
	if !strings.Contains(out, "alpha#1") || !strings.Contains(out, "beta#2") {
		t.Fatalf("missing case lines: %s", out)
	}

	only, _, code := runForgeStreams(t, dir, "eval", "suite", "--only", "alpha", "--json")
	if code != 0 || !strings.Contains(only, "\"change\": \"alpha\"") {
		t.Fatalf("only filter json failed (%d): %s", code, only)
	}
}

func TestEvalSuite_FailingCriteriaExitNonZero(t *testing.T) {
	dir := setupEvalSuiteRepo(t)
	writeArchiveAcceptance(t, dir, "20260103-broken", "```bash\nfalse\n```\n")
	out, _, code := runForgeStreams(t, dir, "eval", "suite")
	if code == 0 {
		t.Fatalf("failing criteria must exit non-zero: %s", out)
	}
	if !strings.Contains(out, "回归基线：2/3 通过") {
		t.Fatalf("missing summary: %s", out)
	}
}

func TestEvalTrend_ReadsRecordedRuns(t *testing.T) {
	dir := setupEvalSuiteRepo(t)
	if _, _, code := runForgeStreams(t, dir, "eval", "suite", "--record"); code != 0 {
		t.Fatalf("record run failed: %d", code)
	}
	out, _, code := runForgeStreams(t, dir, "eval", "trend")
	if code != 0 {
		t.Fatalf("trend failed: %d", code)
	}
	if !strings.Contains(out, "2/2 通过") {
		t.Fatalf("trend missing row: %s", out)
	}
}

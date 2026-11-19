package cli

import (
	"encoding/json"
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
	writeArchiveAcceptance(t, dir, "20260101-alpha", "```bash\nprintf 'alpha-ok\\n'\n```\n")
	writeArchiveAcceptance(t, dir, "20260102-beta", "```bash\n# suite: skip\ngo test ./...\n```\n\n```bash\n# 两行脚本必须都执行\nprintf 'beta-ok\\n'\nprintf 'beta-two\\n'\n```\n")
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
}

func TestEvalSuite_JSONIsPureJSON(t *testing.T) {
	dir := setupEvalSuiteRepo(t)
	out, _, code := runForgeStreams(t, dir, "eval", "suite", "--max", "0", "--json")
	if code != 0 {
		t.Fatalf("want 0, got %d: %s", code, out)
	}
	var rec struct {
		Total   int `json:"total"`
		Passed  int `json:"passed"`
		Results []struct {
			Change string `json:"change"`
			Output string `json:"output"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("--json output is not pure JSON: %v\n%s", err, out)
	}
	if rec.Total != 2 || rec.Passed != 2 {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

func TestEvalSuite_FailingCriteriaShowsReason(t *testing.T) {
	dir := setupEvalSuiteRepo(t)
	writeArchiveAcceptance(t, dir, "20260103-broken", "```bash\necho exploded >&2\nexit 3\n```\n")
	out, _, code := runForgeStreams(t, dir, "eval", "suite")
	if code == 0 {
		t.Fatalf("failing criteria must exit non-zero: %s", out)
	}
	if !strings.Contains(out, "回归基线：2/3 通过") {
		t.Fatalf("missing summary: %s", out)
	}
	if !strings.Contains(out, "exploded") {
		t.Fatalf("failure reason missing: %s", out)
	}
}

func TestEvalSuite_UnknownOnlyIsAnError(t *testing.T) {
	dir := setupEvalSuiteRepo(t)
	out, errOut, code := runForgeStreams(t, dir, "eval", "suite", "--only", "20260101-alpha")
	if code == 0 {
		t.Fatalf("unknown --only must fail, got: %s", out)
	}
	if !strings.Contains(errOut, "alpha") {
		t.Fatalf("error should list available ids: stdout=%s stderr=%s", out, errOut)
	}
}

func TestEvalSuite_JSONRecordWritesFile(t *testing.T) {
	dir := setupEvalSuiteRepo(t)
	if _, _, code := runForgeStreams(t, dir, "eval", "suite", "--json", "--record"); code != 0 {
		t.Fatalf("json record run failed: %d", code)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".forge", "evals"))
	if err != nil || len(entries) < 2 {
		t.Fatalf("--json --record must still write records: %v %v", err, entries)
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
	jsonOut, _, code := runForgeStreams(t, dir, "eval", "trend", "--json")
	if code != 0 {
		t.Fatalf("trend --json failed: %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &rows); err != nil {
		t.Fatalf("trend --json not pure JSON: %v", err)
	}
}

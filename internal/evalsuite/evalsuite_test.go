package evalsuite

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAcceptance(t *testing.T, root, id, body string) {
	t.Helper()
	dir := filepath.Join(root, "spec", "changes", "archive", "20260101-"+id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acceptance.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_CollectsFencesAndSkipsSuiteSkip(t *testing.T) {
	root := t.TempDir()
	writeAcceptance(t, root, "alpha", "```bash\nprintf 'alpha-ok'\n```\n\n```bash\n# suite: skip\ngo test ./...\n```\n")
	writeAcceptance(t, root, "beta", "```bash\nprintf 'beta-ok'\n```\n")

	cases, err := Load(root, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("want 2 cases, got %d: %+v", len(cases), cases)
	}
	// 归档目录按倒序扫描，beta 在前
	if cases[0].Change != "beta" || cases[0].Command != "printf 'beta-ok'" {
		t.Fatalf("unexpected first case: %+v", cases[0])
	}
	if cases[1].Change != "alpha" {
		t.Fatalf("unexpected second case: %+v", cases[1])
	}

	only, err := Load(root, "alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].Change != "alpha" {
		t.Fatalf("only filter failed: %+v", only)
	}

	maxed, err := Load(root, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(maxed) != 1 {
		t.Fatalf("max not honored: %+v", maxed)
	}
}

func TestRun_ReportsPassAndFail(t *testing.T) {
	root := t.TempDir()
	rec := Run(root, []Case{
		{Change: "ok", Fence: 1, Command: "true"},
		{Change: "bad", Fence: 1, Command: "false"},
	}, "abc1234")
	if rec.Total != 2 || rec.Passed != 1 {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if !rec.Results[0].Passed || rec.Results[0].DurationMS < 0 {
		t.Fatalf("pass result wrong: %+v", rec.Results[0])
	}
	if rec.Results[1].Passed || rec.Results[1].Exit == 0 {
		t.Fatalf("fail result wrong: %+v", rec.Results[1])
	}
}

func TestSaveAndTrend(t *testing.T) {
	root := t.TempDir()
	rec := Run(root, []Case{{Change: "ok", Fence: 1, Command: "true"}}, "abc1234")
	path, err := Save(root, rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("record not written: %v", err)
	}
	rows, err := Trend(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 trend row, got %d", len(rows))
	}
	if got := int(rows[0]["passed"].(float64)); got != 1 {
		t.Fatalf("trend passed=%d", got)
	}
}

func TestSplitCommand_QuotesAndUnbalanced(t *testing.T) {
	args, err := splitCommand(`printf "a b" 'c d' e`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"printf", "a b", "c d", "e"}
	if len(args) != len(want) {
		t.Fatalf("got %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d = %q, want %q", i, args[i], want[i])
		}
	}
	if _, err := splitCommand(`echo "unterminated`); err == nil {
		t.Fatal("unbalanced quote must error")
	}
}

package evalsuite

import (
	"os"
	"path/filepath"
	"strings"
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
	writeAcceptance(t, root, "alpha", "```bash\necho alpha-ok\n```\n\n```bash\n# suite: skip\ngo test ./...\n```\n")
	writeAcceptance(t, root, "beta", "```bash\necho beta-ok\n```\n")

	cases, err := Load(root, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("want 2 cases, got %d: %+v", len(cases), cases)
	}
	// 归档目录按倒序扫描，beta 在前
	if cases[0].Change != "beta" || !strings.Contains(cases[0].Command, "beta-ok") {
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

func TestLoad_UnknownOnlyListsAvailable(t *testing.T) {
	root := t.TempDir()
	writeAcceptance(t, root, "alpha", "```bash\ntrue\n```\n")
	if _, err := Load(root, "nope", 0); err == nil || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("unknown --only must error and list ids, got %v", err)
	}
}

func TestLoad_MultiLineFenceIsOneScript(t *testing.T) {
	root := t.TempDir()
	writeAcceptance(t, root, "multi", "```bash\n# 1. 注释行\nprintf 'one\\n'\nprintf 'two\\n'\n```\n\n```sh\nprintf 'sh-ok\\n'\n```\n")
	cases, err := Load(root, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("want 2, got %d", len(cases))
	}
	if cases[0].Shell != "bash" || !strings.Contains(cases[0].Command, "# 1. 注释行") || !strings.Contains(cases[0].Command, "printf 'two") {
		t.Fatalf("multi-line fence not kept intact: %+v", cases[0])
	}
	if cases[1].Shell != "sh" {
		t.Fatalf("sh fence not recognized: %+v", cases[1])
	}
}

func TestRun_ExecutesWholeScriptInShell(t *testing.T) {
	root := t.TempDir()
	rec := Run(root, []Case{
		{Change: "ok", Fence: 1, Shell: "bash", Command: "printf 'a\\n'; printf 'b\\n'"},
		{Change: "comment", Fence: 1, Shell: "bash", Command: "# 注释行\ntest -d . && echo dir-ok"},
		{Change: "bad", Fence: 1, Shell: "bash", Command: "false"},
	}, "abc1234")
	if rec.Total != 3 || rec.Passed != 2 {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if !strings.Contains(rec.Results[0].Output, "b") {
		t.Fatalf("script not fully executed: %+v", rec.Results[0])
	}
	if rec.Results[2].Passed || rec.Results[2].Exit == 0 {
		t.Fatalf("fail result wrong: %+v", rec.Results[2])
	}
}

func TestRun_ReportsMissingCommandAsError(t *testing.T) {
	rec := Run(t.TempDir(), []Case{{Change: "x", Fence: 1, Shell: "bash", Command: "definitely-not-a-command-xyz"}}, "")
	if rec.Passed != 0 {
		t.Fatalf("missing command must fail: %+v", rec.Results[0])
	}
	if rec.Results[0].Exit == 0 && rec.Results[0].Error == "" {
		t.Fatalf("failure must carry exit or error: %+v", rec.Results[0])
	}
}

func TestSaveAndTrend(t *testing.T) {
	root := t.TempDir()
	rec := Run(root, []Case{{Change: "ok", Fence: 1, Shell: "bash", Command: "true"}}, "abc1234")
	path, err := Save(root, rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("record not written: %v", err)
	}
	// 同一秒重复记录不得覆盖
	path2, err := Save(root, rec)
	if err != nil {
		t.Fatal(err)
	}
	if path2 == path {
		t.Fatalf("same-second save overwrote %s", path)
	}
	rows, err := Trend(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 trend rows, got %d", len(rows))
	}
	if got := int(rows[0]["passed"].(float64)); got != 1 {
		t.Fatalf("trend passed=%d", got)
	}
}

func TestCapOutputKeepsValidUTF8(t *testing.T) {
	s := strings.Repeat("中", 20)
	out := capOutput(s, 10)
	if !strings.Contains(out, "截断") {
		t.Fatalf("missing truncate marker: %q", out)
	}
	trimmed := strings.TrimSuffix(out, "…（截断）")
	for _, r := range trimmed {
		if r == '\uFFFD' {
			t.Fatalf("truncation split a rune: %q", out)
		}
	}
}

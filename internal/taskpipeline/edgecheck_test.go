package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// edgecheck_test.go — L2c 引擎行为钉（配对 edgecheck.go）。

// TestGenerateEdgeChecklist: exported funcs × five dimensions land in the
// artifact; no-exported changes yield ok=false.
//
// TestGenerateEdgeChecklist：导出函数 × 五维落进产物；无可枚举导出时
// ok=false。
func TestGenerateEdgeChecklist(t *testing.T) {
	dir, state := questioningFixture(t, false)
	art, ok := GenerateEdgeChecklist(dir, state)
	if !ok {
		t.Fatal(`有导出函数的改动应生成清单`)
	}
	body, err := os.ReadFile(filepath.Join(dataHome(dir), filepath.FromSlash(art.Path)))
	if err != nil {
		t.Fatalf(`读产物: %v`, err)
	}
	text := string(body)
	for _, want := range []string{"## calc.go", "### Max", "### Clamp", "**基数**", "**值域**", "**时序**", "**环境**", "**故障**", "- [ ]"} {
		if !strings.Contains(text, want) {
			t.Errorf("清单应含 %q\n%s", want, text)
		}
	}
	if art.Items < art.Funcs*5 {
		t.Errorf(`条目数应 ≥ 函数×5（%d 函数 got %d 条）`, art.Funcs, art.Items)
	}
}

// TestGenerateEdgeChecklist_NoExportedChanges: a changed file with no exported
// funcs yields ok=false (negative path pin).
//
// TestGenerateEdgeChecklist_NoExportedChanges：改动文件无导出函数时
// ok=false（负路径钉）。
func TestGenerateEdgeChecklist_NoExportedChanges(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "t@t.com")
	run("git", "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module n\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package n\n\nfunc helper() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	headOut, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	state := &TaskState{TaskRef: "feat/noexp", HeadCommit: strings.TrimSpace(string(headOut))}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package n\n\nfunc helper() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := GenerateEdgeChecklist(dir, state); ok {
		t.Error(`无导出函数的改动应 ok=false（清单空转）`)
	}
}

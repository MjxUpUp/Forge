package mutprobe_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/mutprobe"
)

// TestChangedGoSourcesSince：增量探针只看 diff 命中的非测试 Go 源文件。
func TestChangedGoSourcesSince(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package m\n\nfunc A(x int) bool { return x == 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package m\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { if !A(1) { t.Fatal(\"want true\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package m\n\nfunc A(x int) bool { return x != 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package m\n\nfunc B() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "change")
	files, err := mutprobe.ChangedGoSources(dir, "HEAD~1")
	if err != nil {
		t.Fatalf("ChangedGoSources: %v", err)
	}
	want := map[string]bool{"a.go": true, "b.go": true}
	if len(files) != 2 {
		t.Fatalf("files = %v，期望 a.go 与 b.go（不含 a_test.go）", files)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("意外文件 %q（测试文件或未改动文件混入）", f)
		}
	}
	if _, err := mutprobe.ChangedGoSources(dir, "不存在的rev"); err == nil {
		t.Errorf("非法 rev 应报错，未报错")
	}
}

// TestChangedGoSourcesUntrackedAndTestFilter 钉住两个容易静默漏掉的口径：
//   - 未 git add 的新 .go 文件也算"改动"（否则新代码恰好不进探针）；
//   - 改动里出现的 _test.go 必须被过滤（探针只对生产代码做变异）。
func TestChangedGoSourcesUntrackedAndTestFilter(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "core.autocrlf", "false")
	for name, body := range map[string]string{
		"go.mod":    "module example.com/m\n\ngo 1.25.0\n",
		"a.go":      "package m\n\nfunc A() int { return 1 }\n",
		"a_test.go": "package m\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { _ = A() }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-qm", "base")

	// 未跟踪的新源文件 + 改动测试文件（不 add）。
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package m\n\nfunc New() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package m\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { if A() != 1 { t.Fatal() } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := mutprobe.ChangedGoSources(dir, "HEAD")
	if err != nil {
		t.Fatalf("ChangedGoSources: %v", err)
	}
	if len(files) != 1 || files[0] != "new.go" {
		t.Fatalf("files = %v，期望只有未跟踪的 new.go（a_test.go 被过滤）", files)
	}
}

package impact

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// newRepo 建一个真实 git 仓库：影响面判定依赖 git 的真实输出，
// 用假数据测只会测到我们的假设。
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用，跳过依赖 git 的影响面测试")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "impact-test")
	git(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
}

func TestCollect_NoChanges(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "README.md", "# base\n")
	commitAll(t, dir, "base")

	rep, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(rep.Changed) != 0 || len(rep.Docs) != 0 || len(rep.SourcesWithoutTest) != 0 || len(rep.SuggestedTests) != 0 {
		t.Fatalf("干净工作区应返回空报告，得到 %+v", rep)
	}
}

// 一次改动同时覆盖四类判定：文档、无测试源码、有测试源码、spec/changes 例外。
func TestCollect_ClassifiesChangedFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "README.md", "# base\n")
	write(t, dir, "docs/guide.md", "# guide\n")
	write(t, dir, "internal/solo/solo.go", "package solo\n")
	write(t, dir, "internal/paired/paired.go", "package paired\n")
	write(t, dir, "internal/paired/paired_test.go", "package paired\n")
	write(t, dir, "spec/changes/demo/goal.md", "# goal\n")
	commitAll(t, dir, "base")

	// 修改已跟踪文件；另在全新目录里加一个未跟踪源文件（-uall 才能逐文件看到）。
	write(t, dir, "README.md", "# base\nmore\n")
	write(t, dir, "docs/guide.md", "# guide\nmore\n")
	write(t, dir, "internal/solo/solo.go", "package solo\n\n// changed\n")
	write(t, dir, "internal/paired/paired.go", "package paired\n\n// changed\n")
	write(t, dir, "spec/changes/demo/goal.md", "# goal\nmore\n")
	write(t, dir, "internal/fresh/feature.go", "package fresh\n")

	rep, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	wantChanged := []string{
		"README.md",
		"docs/guide.md",
		"internal/fresh/feature.go",
		"internal/paired/paired.go",
		"internal/solo/solo.go",
		"spec/changes/demo/goal.md",
	}
	if !reflect.DeepEqual(rep.Changed, wantChanged) {
		t.Errorf("Changed = %v, want %v", rep.Changed, wantChanged)
	}
	wantDocs := []string{"README.md", "docs/guide.md"}
	if !reflect.DeepEqual(rep.Docs, wantDocs) {
		t.Errorf("Docs = %v, want %v（spec/changes 下的 .md 不计入）", rep.Docs, wantDocs)
	}
	wantNoTest := []string{"internal/fresh/feature.go", "internal/solo/solo.go"}
	if !reflect.DeepEqual(rep.SourcesWithoutTest, wantNoTest) {
		t.Errorf("SourcesWithoutTest = %v, want %v", rep.SourcesWithoutTest, wantNoTest)
	}
	wantDirs := []string{"internal/paired"}
	if !reflect.DeepEqual(rep.SuggestedTests, wantDirs) {
		t.Errorf("SuggestedTests = %v, want %v", rep.SuggestedTests, wantDirs)
	}
}

// 重命名取新路径；删除项整体跳过（否则"删了文档"会被要求同步文档）。
func TestCollect_RenameAndDelete(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "README.md", "# base\n")
	write(t, dir, "internal/solo/old.go", "package solo\n")
	commitAll(t, dir, "base")

	git(t, dir, "mv", "internal/solo/old.go", "internal/solo/new.go")
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("remove README: %v", err)
	}

	rep, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !reflect.DeepEqual(rep.Changed, []string{"internal/solo/new.go"}) {
		t.Errorf("Changed = %v, want 只有重命名后的新路径", rep.Changed)
	}
	if len(rep.Docs) != 0 {
		t.Errorf("Docs = %v, want 空（README 是删除项）", rep.Docs)
	}
	if !reflect.DeepEqual(rep.SourcesWithoutTest, []string{"internal/solo/new.go"}) {
		t.Errorf("SourcesWithoutTest = %v", rep.SourcesWithoutTest)
	}
}

// 已暂存的重命名（R）与未暂存的重命名（工作区）都要取新路径，且不能把
// 旧路径当成第二条改动。
func TestCollect_StagedRename(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "internal/solo/old.go", "package solo\n")
	commitAll(t, dir, "base")

	git(t, dir, "mv", "internal/solo/old.go", "internal/solo/new.go")
	git(t, dir, "add", "-A")

	rep, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !reflect.DeepEqual(rep.Changed, []string{"internal/solo/new.go"}) {
		t.Fatalf("Changed = %v, want [internal/solo/new.go]", rep.Changed)
	}
}

// 非 git 仓库：返回错误而不是空报告；不能 panic。
func TestCollect_NotGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	dir := t.TempDir()
	write(t, dir, "a.txt", "hello\n")

	rep, err := Collect(dir)
	if err == nil {
		t.Fatalf("非 git 仓库应报错，得到 %+v", rep)
	}
	if rep != nil {
		t.Errorf("出错时应返回 nil 报告，得到 %+v", rep)
	}
}

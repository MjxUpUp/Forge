package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// reviewGit runs a git command in dir and fails the test on error.
//
// reviewGit 在 dir 里运行 git 命令，出错即让测试失败。
func reviewGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// reviewCommit writes files and commits them in dir.
//
// reviewCommit 在 dir 写入文件并提交。
func reviewCommit(t *testing.T, dir, msg string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	reviewGit(t, dir, "add", "-A")
	reviewGit(t, dir, "commit", "-q", "-m", msg)
}

// TestReviewWindowFiles_ExcludesMergedBaseFiles: with a review baseline, test files that arrived only by merging another branch after the baseline are not in the review window (so test-diff does not disclose them); the task's own test file still is.
//
// TestReviewWindowFiles_ExcludesMergedBaseFiles：有审查基线时，基线之后只经合并别的
// 分支带入的测试文件不进审查窗口（test-diff 不再披露它们）；本任务自己的测试文件
// 仍在窗口里。
func TestReviewWindowFiles_ExcludesMergedBaseFiles(t *testing.T) {
	dir := t.TempDir()
	reviewGit(t, dir, "init", "-q")
	reviewGit(t, dir, "config", "user.email", "t@t.com")
	reviewGit(t, dir, "config", "user.name", "T")
	reviewCommit(t, dir, "init", map[string]string{"main.go": "package main\n"})
	reviewGit(t, dir, "checkout", "-q", "-b", "task")
	reviewGit(t, dir, "branch", "-f", "main")
	baseline := reviewGit(t, dir, "rev-parse", "HEAD")

	reviewCommit(t, dir, "task own test", map[string]string{"own_test.go": "package main\n"})
	reviewGit(t, dir, "checkout", "-q", "main")
	reviewCommit(t, dir, "base test", map[string]string{"base_test.go": "package main\n"})
	reviewGit(t, dir, "checkout", "-q", "task")
	reviewGit(t, dir, "merge", "-q", "--no-edit", "main")

	got := reviewTestTouchedFiles(dir, &taskpipeline.TaskState{TaskRef: "t"}, baseline)
	if !slices.Contains(got, "own_test.go") {
		t.Errorf("task's own test file must stay in the review window, got %v", got)
	}
	if slices.Contains(got, "base_test.go") {
		t.Errorf("test file merged in from base must not be in the review window, got %v", got)
	}
}

// TestReviewWindowFiles_NoMergeUnchanged: with a review baseline and no merge after it, the review window is exactly the plain diff plus untracked files, as before the merged-in filtering existed.
//
// TestReviewWindowFiles_NoMergeUnchanged：有审查基线、之后没有合并时，审查窗口与
// 引入合并剔除前逐字相同：净 diff 加 untracked 文件。
func TestReviewWindowFiles_NoMergeUnchanged(t *testing.T) {
	dir := t.TempDir()
	reviewGit(t, dir, "init", "-q")
	reviewGit(t, dir, "config", "user.email", "t@t.com")
	reviewGit(t, dir, "config", "user.name", "T")
	reviewCommit(t, dir, "init", map[string]string{"main.go": "package main\n", "old_test.go": "package main\n\nfunc TestOld() {}\n"})
	reviewGit(t, dir, "checkout", "-q", "-b", "task")
	reviewGit(t, dir, "branch", "-f", "main")
	baseline := reviewGit(t, dir, "rev-parse", "HEAD")

	reviewCommit(t, dir, "task own test", map[string]string{"own_test.go": "package main\n\nfunc TestOwn() {}\n"})
	reviewGit(t, dir, "rm", "-q", "old_test.go")
	reviewGit(t, dir, "commit", "-q", "-m", "drop old test")
	if err := os.WriteFile(filepath.Join(dir, "new_test.go"), []byte("package main\n\nfunc TestNew() {}\n"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}

	got := reviewWindowFiles(dir, &taskpipeline.TaskState{TaskRef: "t"}, baseline)
	want := []string{"old_test.go", "own_test.go", "new_test.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("no merge after baseline → window must be plain diff + untracked, got %v want %v", got, want)
	}
}

// reviewMergedRepo builds a task branch whose baseline is followed by a task commit (from files) and a merge of main, where main committed baseFiles meanwhile. It returns the repo dir and the review baseline.
//
// reviewMergedRepo 构造任务分支：审查基线之后本任务提交 files，同期 main 提交
// baseFiles，再把 main 合进任务分支。返回仓库目录与审查基线。
func reviewMergedRepo(t *testing.T, seed, files, baseFiles map[string]string, rm ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	reviewGit(t, dir, "init", "-q")
	reviewGit(t, dir, "config", "user.email", "t@t.com")
	reviewGit(t, dir, "config", "user.name", "T")
	reviewCommit(t, dir, "init", seed)
	reviewGit(t, dir, "checkout", "-q", "-b", "task")
	reviewGit(t, dir, "branch", "-f", "main")
	baseline := reviewGit(t, dir, "rev-parse", "HEAD")
	for _, f := range rm {
		reviewGit(t, dir, "rm", "-q", f)
	}
	reviewCommit(t, dir, "task work", files)
	reviewGit(t, dir, "checkout", "-q", "main")
	reviewCommit(t, dir, "base work", baseFiles)
	reviewGit(t, dir, "checkout", "-q", "task")
	reviewGit(t, dir, "merge", "-q", "--no-edit", "main")
	return dir, baseline
}

// TestReviewWindowFiles_KeepsWorktreeEditedMergedFile: a file merged in from base and then edited again in the working tree is the task's work under review, so it stays in the window.
//
// TestReviewWindowFiles_KeepsWorktreeEditedMergedFile：从基线合并带入、之后又在工作树
// 里改过的文件属于本任务待审的工作，留在审查窗口里。
func TestReviewWindowFiles_KeepsWorktreeEditedMergedFile(t *testing.T) {
	dir, baseline := reviewMergedRepo(t,
		map[string]string{"main.go": "package main\n"},
		map[string]string{"own_test.go": "package main\n\nfunc TestOwn() {}\n"},
		map[string]string{"base_test.go": "package main\n\nfunc TestBase() {}\n"})
	if err := os.WriteFile(filepath.Join(dir, "base_test.go"), []byte("package main\n\nfunc TestBase() { /* weakened */ }\n"), 0o644); err != nil {
		t.Fatalf("edit merged file: %v", err)
	}

	got := reviewWindowFiles(dir, &taskpipeline.TaskState{TaskRef: "t"}, baseline)
	for _, want := range []string{"own_test.go", "base_test.go"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s must be in the review window, got %v", want, got)
		}
	}
}

// TestReviewWindowFiles_KeepsDeletionPairedAsRename: the task deletes a test while base adds an identical copy, which the window's diff pairs as a rename; the deleted path is restored into the window and the base copy is not shown.
//
// TestReviewWindowFiles_KeepsDeletionPairedAsRename：本任务删掉测试，同期基线新增一份
// 相同内容的拷贝，窗口的 diff 把两者配成重命名；被删的路径补回窗口，基线的拷贝不
// 出现。
func TestReviewWindowFiles_KeepsDeletionPairedAsRename(t *testing.T) {
	content := "package main\n\nfunc TestA() {\n\tif 1+1 != 2 {\n\t\tpanic(\"math\")\n\t}\n}\n"
	dir, baseline := reviewMergedRepo(t,
		map[string]string{"main.go": "package main\n", "x_test.go": content},
		map[string]string{"own.go": "package main\n\nfunc own() {}\n"},
		map[string]string{"copy_test.go": content},
		"x_test.go")

	got := reviewWindowFiles(dir, &taskpipeline.TaskState{TaskRef: "t"}, baseline)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("task's test deletion must be in the review window, got %v", got)
	}
	if slices.Contains(got, "copy_test.go") {
		t.Errorf("base copy must not be in the review window, got %v", got)
	}
}

// TestReviewWindowFiles_KeepsWorktreeDeletionPairedAsRename: after merging base that adds a copy of a test, the task deletes the original only in the working tree; the window's diff pairs the two as a rename, and the deleted path must still be shown while the base copy is not.
//
// TestReviewWindowFiles_KeepsWorktreeDeletionPairedAsRename：合并了新增测试拷贝的基线
// 之后，本任务只在工作树里删掉原测试（未提交）；窗口的 diff 把两者配成重命名，被删的
// 路径仍要出现在窗口里，基线的拷贝不出现。
func TestReviewWindowFiles_KeepsWorktreeDeletionPairedAsRename(t *testing.T) {
	content := "package main\n\nfunc TestA() {\n\tif 1+1 != 2 {\n\t\tpanic(\"math\")\n\t}\n}\n"
	dir, baseline := reviewMergedRepo(t,
		map[string]string{"main.go": "package main\n", "x_test.go": content},
		map[string]string{"own.go": "package main\n\nfunc own() {}\n"},
		map[string]string{"copy_test.go": content})
	reviewGit(t, dir, "rm", "-q", "x_test.go")
	if ns := reviewGit(t, dir, "diff", "--name-status", baseline); !strings.Contains(ns, "x_test.go\tcopy_test.go") {
		t.Fatalf("fixture: window diff must pair the deletion with the base copy, got %q", ns)
	}

	got := reviewWindowFiles(dir, &taskpipeline.TaskState{TaskRef: "t"}, baseline)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("uncommitted test deletion must be in the review window, got %v", got)
	}
	if slices.Contains(got, "copy_test.go") {
		t.Errorf("base copy must not be in the review window, got %v", got)
	}
}

// TestReviewWindowFiles_BaseSideRenameNotRestored: base renames a test and the task merges it without touching either path; the old path is base's own change and must not be restored into the review window, under every diff.renames setting.
//
// TestReviewWindowFiles_BaseSideRenameNotRestored：基线把某个测试改名，本任务合并进来、
// 两个路径都没动；旧路径是基线自己的改动，不能补回审查窗口，diff.renames 取哪种配置
// 都一样。
func TestReviewWindowFiles_BaseSideRenameNotRestored(t *testing.T) {
	for _, renames := range []string{"true", "false", "copies"} {
		t.Run("renames="+renames, func(t *testing.T) {
			content := "package main\n\nfunc TestA() {\n\tif 1+1 != 2 {\n\t\tpanic(\"math\")\n\t}\n}\n"
			dir := t.TempDir()
			reviewGit(t, dir, "init", "-q")
			reviewGit(t, dir, "config", "user.email", "t@t.com")
			reviewGit(t, dir, "config", "user.name", "T")
			reviewGit(t, dir, "config", "diff.renames", renames)
			reviewCommit(t, dir, "init", map[string]string{"main.go": "package main\n", "a_test.go": content})
			reviewGit(t, dir, "checkout", "-q", "-b", "task")
			reviewGit(t, dir, "branch", "-f", "main")
			baseline := reviewGit(t, dir, "rev-parse", "HEAD")
			reviewCommit(t, dir, "task own", map[string]string{"own.go": "package main\n\nfunc own() {}\n"})
			reviewGit(t, dir, "checkout", "-q", "main")
			reviewGit(t, dir, "mv", "a_test.go", "b_test.go")
			reviewGit(t, dir, "commit", "-q", "-m", "base renames test")
			reviewGit(t, dir, "checkout", "-q", "task")
			reviewGit(t, dir, "merge", "-q", "--no-edit", "main")

			got := reviewWindowFiles(dir, &taskpipeline.TaskState{TaskRef: "t"}, baseline)
			if want := []string{"own.go"}; !slices.Equal(got, want) {
				t.Fatalf("base-side rename must not leak into the review window, got %v want %v", got, want)
			}
		})
	}
}

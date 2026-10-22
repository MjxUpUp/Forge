package taskpipeline

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// headCommit returns the current HEAD sha of the repo at dir.
//
// headCommit 返回 dir 仓库当前 HEAD 的 sha。
func headCommit(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// committedSince lists the files changed in from..HEAD (the committed part of a task window).
//
// committedSince 列出 from..HEAD 内改动的文件（任务窗口的已提交部分）。
func committedSince(t *testing.T, dir, from string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "diff", "--name-only", from+"..HEAD").Output()
	if err != nil {
		t.Fatalf("git diff %s..HEAD: %v", from, err)
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files
}

// mergedBaseRepo builds a task branch that, after starting, commits own.go while a "base" branch commits base_only.go, then merges base into the task branch. It returns the repo dir and the task start commit.
//
// mergedBaseRepo 构造任务分支：开工后提交 own.go；同时 base 分支提交 base_only.go；
// 再把 base 合并进任务分支。返回仓库目录与任务起点提交。
func mergedBaseRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := setupRecurrentRepo(t)
	runGit(t, dir, "branch", "base")
	start := headCommit(t, dir)
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n"}, "task own work")
	runGit(t, dir, "checkout", "base")
	writeCommitSource(t, dir, map[string]string{"base_only.go": "package main\n"}, "base work")
	runGit(t, dir, "checkout", "feat/testcov")
	runGit(t, dir, "merge", "--no-edit", "base")
	return dir, start
}

// TestExcludeMergedInFiles_DropsBaseOnlyFiles: after merging base into the task branch, files that came only from base are dropped; the task's own file stays.
//
// TestExcludeMergedInFiles_DropsBaseOnlyFiles：任务分支合并 base 后，只从 base 带入的
// 文件被剔除，本任务自己的文件保留。
func TestExcludeMergedInFiles_DropsBaseOnlyFiles(t *testing.T) {
	dir, start := mergedBaseRepo(t)
	committed := committedSince(t, dir, start)
	if !slices.Contains(committed, "base_only.go") {
		t.Fatalf("fixture: net diff must include the merged-in base_only.go, got %v", committed)
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	if !slices.Contains(got, "own.go") {
		t.Errorf("task's own file must be kept, got %v", got)
	}
	if slices.Contains(got, "base_only.go") {
		t.Errorf("file brought in only by merging base must be dropped, got %v", got)
	}
}

// TestExcludeMergedInFiles_KeepsConflictResolution: a file both sides changed and that was resolved in the merge commit still counts as the task's change.
//
// TestExcludeMergedInFiles_KeepsConflictResolution：两侧都改、在合并提交里解决冲突的
// 文件仍算本任务改动；只从 base 带入的文件照样剔除。
func TestExcludeMergedInFiles_KeepsConflictResolution(t *testing.T) {
	dir := setupRecurrentRepo(t)
	writeCommitSource(t, dir, map[string]string{"shared.go": "package main\n// v0\n"}, "seed shared")
	runGit(t, dir, "branch", "base")
	start := headCommit(t, dir)
	runGit(t, dir, "checkout", "base")
	writeCommitSource(t, dir, map[string]string{"shared.go": "package main\n// base\n", "base_only.go": "package main\n", "base_edited.go": "package main\n// base\n"}, "base edits shared")
	runGit(t, dir, "checkout", "feat/testcov")
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n", "shared.go": "package main\n// task\n"}, "task own work")
	// 两侧都改了 shared.go：git merge 必须以冲突失败（夹具自检），解决后在合并提交里落盘。
	if err := exec.Command("git", "-C", dir, "merge", "--no-edit", "base").Run(); err == nil {
		t.Fatal("fixture: merge must conflict on shared.go")
	}
	// 解决时顺手改了只属于 base 的 base_edited.go：它只在合并提交里被本任务改动，
	// 第一父链的普通提交里没有它——只能靠合并提交的 --cc 文件识别为本任务改动。
	writeCommitSource(t, dir, map[string]string{"shared.go": "package main\n// resolved\n", "base_edited.go": "package main\n// edited in merge\n"}, "merge base with resolution")
	if out, err := exec.Command("git", "-C", dir, "rev-list", "--merges", "--count", start+"..HEAD").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Fatalf("fixture: resolution must land in the merge commit, merges=%q err=%v", out, err)
	}

	got := ExcludeMergedInFiles(dir, start, committedSince(t, dir, start))
	if !slices.Contains(got, "shared.go") {
		t.Errorf("conflict-resolution file must be kept as the task's change, got %v", got)
	}
	if !slices.Contains(got, "own.go") {
		t.Errorf("task's own file must be kept, got %v", got)
	}
	if !slices.Contains(got, "base_edited.go") {
		t.Errorf("file edited only inside the merge commit must be kept (merge --cc), got %v", got)
	}
	if slices.Contains(got, "base_only.go") {
		t.Errorf("file brought in only by merging base must be dropped, got %v", got)
	}
}

// TestExcludeMergedInFiles_NoMergeUnchanged: with no merge commit in the range the input is returned unchanged, element for element.
//
// TestExcludeMergedInFiles_NoMergeUnchanged：区间内没有合并提交时原样返回，逐项不变。
func TestExcludeMergedInFiles_NoMergeUnchanged(t *testing.T) {
	dir := setupRecurrentRepo(t)
	start := headCommit(t, dir)
	writeCommitSource(t, dir, map[string]string{"a.go": "package main\n", "b.go": "package main\n"}, "task work")
	in := []string{"a.go", "b.go", "not_in_range.go"}
	got := ExcludeMergedInFiles(dir, start, in)
	if !slices.Equal(got, in) {
		t.Fatalf("no merge in range → input must be returned unchanged, got %v want %v", got, in)
	}
}

// TestTaskChangedFiles_MergedMainNotDrift pins the end-to-end symptom: after merging base into the task branch, the task's changed files exclude base's files, so scope-drift against a plan that names only the task's file is empty.
//
// TestTaskChangedFiles_MergedMainNotDrift 钉住端到端症状：任务分支合并 base 后，任务
// 改动集不含 base 的文件，按只列本任务文件的 PlanScope 计算 scope-drift 为空。
func TestTaskChangedFiles_MergedMainNotDrift(t *testing.T) {
	dir, start := mergedBaseRepo(t)
	state := &TaskState{TaskRef: "merged-main", Branch: "feat/testcov", HeadCommit: start, PlanScope: []string{"own.go"}}
	changed := taskChangedFiles(dir, state)
	if !slices.Contains(changed, "own.go") {
		t.Fatalf("task changed files must include own.go, got %v", changed)
	}
	if drift := ScopeDrift(changed, state.PlanScope); len(drift) != 0 {
		t.Fatalf("files merged in from base must not count as scope drift, got %v", drift)
	}
}

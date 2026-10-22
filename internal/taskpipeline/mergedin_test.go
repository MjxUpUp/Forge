package taskpipeline

import (
	"fmt"
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

// mergedBaseRepo builds a task branch that, after starting, commits own.go while the "main" base branch commits base_only.go, then merges main into the task branch. It returns the repo dir and the task start commit.
//
// mergedBaseRepo 构造任务分支：开工后提交 own.go；同时基线分支 main 提交 base_only.go；
// 再把 main 合并进任务分支（无远端，基线 ref 回落本地 main，见 mergedInLocalBaseRefs）。返回仓库目录与任务起点提交。
func mergedBaseRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := setupRecurrentRepo(t)
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n"}, "task own work")
	runGit(t, dir, "checkout", "main")
	writeCommitSource(t, dir, map[string]string{"base_only.go": "package main\n"}, "base work")
	runGit(t, dir, "checkout", "feat/testcov")
	runGit(t, dir, "merge", "--no-edit", "main")
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
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	runGit(t, dir, "checkout", "main")
	writeCommitSource(t, dir, map[string]string{"shared.go": "package main\n// base\n", "base_only.go": "package main\n", "base_edited.go": "package main\n// base\n"}, "base edits shared")
	runGit(t, dir, "checkout", "feat/testcov")
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n", "shared.go": "package main\n// task\n"}, "task own work")
	// 两侧都改了 shared.go：git merge 必须以冲突失败（夹具自检），解决后在合并提交里落盘。
	if err := exec.Command("git", "-C", dir, "merge", "--no-edit", "main").Run(); err == nil {
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

// TestExcludeMergedInFiles_KeepsOwnSideBranch: the task's own work done on a side branch and merged back with --no-ff is not base, so all of it stays in the change set (otherwise a test deletion could hide behind one merge).
//
// TestExcludeMergedInFiles_KeepsOwnSideBranch：本任务在侧分支上做的改动经 --no-ff
// 合回，另一侧不是基线，整段计入改动集（否则删测试之类的改动能借一次合并躲过门禁）。
func TestExcludeMergedInFiles_KeepsOwnSideBranch(t *testing.T) {
	dir := setupRecurrentRepo(t)
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc TestSeed() {}\n"}, "seed test")
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	runGit(t, dir, "checkout", "-b", "side")
	runGit(t, dir, "rm", "-q", "x_test.go")
	writeCommitSource(t, dir, map[string]string{"sideown.go": "package main\n\nfunc side() {}\n"}, "side work deletes test")
	runGit(t, dir, "checkout", "feat/testcov")
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n\nfunc own() {}\n"}, "task own work")
	runGit(t, dir, "merge", "--no-ff", "--no-edit", "side")

	committed := committedSince(t, dir, start)
	if !slices.Contains(committed, "x_test.go") {
		t.Fatalf("fixture: net diff must show x_test.go deleted, got %v", committed)
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	for _, want := range []string{"own.go", "sideown.go", "x_test.go"} {
		if !slices.Contains(got, want) {
			t.Errorf("task's own side-branch work must stay in the change set, missing %s in %v", want, got)
		}
	}
}

// TestExcludeMergedInFiles_InvertedFirstParentUnchanged: when the task branch was merged into another branch and then fast-forwarded to it, the first-parent chain is no longer the task's; the input is returned unchanged instead of inverting the split. Covers both guards: the other branch forked before from (from off the chain), and the other branch is main itself (a merge on the chain is itself reachable from base), with or without further task commits after the fast-forward.
//
// TestExcludeMergedInFiles_InvertedFirstParentUnchanged：任务分支被合并进别的分支、
// 再快进回来时，第一父链已不是本任务的；原样返回，而不是把区分整个反过来。两道守卫
// 都有用例：别的分支在 from 之前分叉（from 不在链上）；别的分支就是 main（链上的
// 合并提交本身可从基线到达），快进后再提交一次也一样。
func TestExcludeMergedInFiles_InvertedFirstParentUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		invert func(t *testing.T, dir, start string) // ends with HEAD on the other branch's history
	}{
		{"merged into main", func(t *testing.T, dir, start string) {
			runGit(t, dir, "checkout", "main")
			writeCommitSource(t, dir, map[string]string{"xonly.go": "package main\n\nfunc x() {}\n"}, "base work")
			runGit(t, dir, "merge", "--no-ff", "--no-edit", "feat/testcov")
			runGit(t, dir, "checkout", "feat/testcov")
			runGit(t, dir, "merge", "--ff-only", "main")
		}},
		{"merged into main, then continued", func(t *testing.T, dir, start string) {
			// 快进回来后再提交一次：HEAD 已不在基线上，靠「链上合并提交本身在基线上」拦住。
			runGit(t, dir, "checkout", "main")
			writeCommitSource(t, dir, map[string]string{"xonly.go": "package main\n\nfunc x() {}\n"}, "base work")
			runGit(t, dir, "merge", "--no-ff", "--no-edit", "feat/testcov")
			runGit(t, dir, "checkout", "feat/testcov")
			runGit(t, dir, "merge", "--ff-only", "main")
			writeCommitSource(t, dir, map[string]string{"own2.go": "package main\n\nfunc own2() {}\n"}, "task continues")
		}},
		{"forked before from", func(t *testing.T, dir, start string) {
			// main 先收下任务，再被一条在 start 之前分叉的分支合并；HEAD 不在基线上，
			// 只有第一父链守卫能拦住（否则 own.go 被当作基线带入剔掉）。
			runGit(t, dir, "checkout", "main")
			runGit(t, dir, "merge", "--no-ff", "--no-edit", "feat/testcov")
			runGit(t, dir, "checkout", "-b", "other", start+"~1")
			writeCommitSource(t, dir, map[string]string{"xonly.go": "package main\n\nfunc x() {}\n"}, "other work")
			runGit(t, dir, "merge", "--no-edit", "main")
			runGit(t, dir, "checkout", "feat/testcov")
			runGit(t, dir, "merge", "--ff-only", "other")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupRecurrentRepo(t)
			writeCommitSource(t, dir, map[string]string{"seed.go": "package main\n\nfunc seed() {}\n"}, "seed")
			runGit(t, dir, "branch", "-f", "main")
			start := headCommit(t, dir)
			writeCommitSource(t, dir, map[string]string{"own.go": "package main\n\nfunc own() {}\n"}, "task own work")
			tc.invert(t, dir, start)

			in := committedSince(t, dir, start)
			if !slices.Contains(in, "own.go") {
				t.Fatalf("fixture: net diff must include own.go, got %v", in)
			}
			got := ExcludeMergedInFiles(dir, start, in)
			if !slices.Equal(got, in) {
				t.Fatalf("inverted first-parent chain → input must be returned unchanged, got %v want %v", got, in)
			}
		})
	}
}

// TestExcludeMergedInFiles_KeepsDeletedPathOfHeavyRename: a test file renamed and then rewritten shows in the net diff as a deletion of the old path; that deletion must stay (the log side runs with --no-renames).
//
// TestExcludeMergedInFiles_KeepsDeletedPathOfHeavyRename：测试文件改名后又大改，净 diff
// 里旧路径显示为删除；这条删除必须保留（log 侧用 --no-renames）。
func TestExcludeMergedInFiles_KeepsDeletedPathOfHeavyRename(t *testing.T) {
	dir := setupRecurrentRepo(t)
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc a() {}\nfunc b() {}\nfunc c() {}\n"}, "seed test")
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	// 先纯改名再重写，分两个提交：log 不加 --no-renames 时只列新路径，正是要防的形态。
	runGit(t, dir, "mv", "x_test.go", "y_test.go")
	runGit(t, dir, "commit", "-m", "rename test")
	writeCommitSource(t, dir, map[string]string{"y_test.go": "package main\n\n// fully rewritten\nfunc z() {}\n"}, "rewrite test")
	runGit(t, dir, "checkout", "main")
	writeCommitSource(t, dir, map[string]string{"base_only.go": "package main\n"}, "base work")
	runGit(t, dir, "checkout", "feat/testcov")
	runGit(t, dir, "merge", "--no-edit", "main")

	committed := committedSince(t, dir, start)
	if !slices.Contains(committed, "x_test.go") {
		t.Fatalf("fixture: net diff must show the old path as deleted, got %v", committed)
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("deleted old path of a rewritten test must be kept, got %v", got)
	}
	if slices.Contains(got, "base_only.go") {
		t.Errorf("file brought in only by merging main must be dropped, got %v", got)
	}
}

// TestExcludeMergedInFiles_GitFailureUnchanged: an unknown from commit makes git fail; the input is returned unchanged (conservative: nothing is dropped without proof).
//
// TestExcludeMergedInFiles_GitFailureUnchanged：from 是不存在的提交，git 调用失败时
// 原样返回（保守：没有证据就不剔除）。
func TestExcludeMergedInFiles_GitFailureUnchanged(t *testing.T) {
	dir, _ := mergedBaseRepo(t)
	in := []string{"own.go", "base_only.go"}
	got := ExcludeMergedInFiles(dir, "0000000000000000000000000000000000000000", in)
	if !slices.Equal(got, in) {
		t.Fatalf("git failure → input must be returned unchanged, got %v want %v", got, in)
	}
}

// TestExcludeMergedInFiles_KeepsOctopusSideBranch: an octopus merge of base and the task's own side branch is not a base-side merge; the side branch's test deletion stays, whatever the parent order.
//
// TestExcludeMergedInFiles_KeepsOctopusSideBranch：同时合并基线与本任务侧分支的章鱼
// 合并不算基线合并；不论父提交顺序，侧分支上的测试删除都保留。
func TestExcludeMergedInFiles_KeepsOctopusSideBranch(t *testing.T) {
	for _, order := range [][]string{{"main", "side"}, {"side", "main"}} {
		t.Run(strings.Join(order, "+"), func(t *testing.T) {
			dir := setupRecurrentRepo(t)
			writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc TestSeed() {}\n"}, "seed test")
			runGit(t, dir, "branch", "-f", "main")
			start := headCommit(t, dir)
			runGit(t, dir, "checkout", "-b", "side")
			runGit(t, dir, "rm", "-q", "x_test.go")
			runGit(t, dir, "commit", "-m", "side deletes test")
			runGit(t, dir, "checkout", "main")
			writeCommitSource(t, dir, map[string]string{"base_only.go": "package main\n\nfunc base() {}\n"}, "base work")
			runGit(t, dir, "checkout", "feat/testcov")
			writeCommitSource(t, dir, map[string]string{"own.go": "package main\n\nfunc own() {}\n"}, "task own work")
			runGit(t, dir, append([]string{"merge", "--no-edit"}, order...)...)
			out, err := exec.Command("git", "-C", dir, "rev-list", "--parents", "-n", "1", "HEAD").Output()
			if err != nil {
				t.Fatalf("git rev-list --parents: %v", err)
			}
			if got := strings.Fields(string(out)); len(got) != 4 {
				t.Fatalf("fixture: HEAD must be an octopus merge with 3 parents, got %v", got)
			}

			committed := committedSince(t, dir, start)
			if !slices.Contains(committed, "x_test.go") {
				t.Fatalf("fixture: net diff must show x_test.go deleted, got %v", committed)
			}
			got := ExcludeMergedInFiles(dir, start, committed)
			if !slices.Contains(got, "x_test.go") {
				t.Errorf("test deletion from the task's side branch must stay despite the octopus merge, got %v", got)
			}
		})
	}
}

// TestExcludeMergedInFiles_LocalBaseUntrustedWithRemote: with an origin/main present, local main is not trusted as base — a test deletion committed on local main and merged in stays — while files merged from origin/main are still dropped.
//
// TestExcludeMergedInFiles_LocalBaseUntrustedWithRemote：有 origin/main 时不把本地
// main 当基线——在本地 main 上提交的测试删除合并进来后仍保留；从 origin/main 合并
// 带入的文件照常剔除。
func TestExcludeMergedInFiles_LocalBaseUntrustedWithRemote(t *testing.T) {
	dir := setupRecurrentRepo(t)
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc TestSeed() {}\n"}, "seed test")
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n\nfunc own() {}\n"}, "task own work")
	runGit(t, dir, "checkout", "-b", "upstream", start)
	writeCommitSource(t, dir, map[string]string{"base_only.go": "package main\n\nfunc base() {}\n"}, "upstream work")
	runGit(t, dir, "update-ref", "refs/remotes/origin/main", "upstream")
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "merge", "--ff-only", "upstream")
	runGit(t, dir, "rm", "-q", "x_test.go")
	runGit(t, dir, "commit", "-m", "local main deletes test")
	runGit(t, dir, "checkout", "feat/testcov")
	runGit(t, dir, "merge", "--no-edit", "origin/main")
	runGit(t, dir, "merge", "--no-edit", "main")

	committed := committedSince(t, dir, start)
	for _, f := range []string{"x_test.go", "base_only.go"} {
		if !slices.Contains(committed, f) {
			t.Fatalf("fixture: net diff must include %s, got %v", f, committed)
		}
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("deletion merged from local main must stay when origin/main exists, got %v", got)
	}
	if slices.Contains(got, "base_only.go") {
		t.Errorf("file merged in from origin/main must be dropped, got %v", got)
	}
}

// mustGitOut runs git in dir and returns its trimmed stdout, failing the test on error.
//
// mustGitOut 在 dir 运行 git 并返回去空白的标准输出，出错即让测试失败。
func mustGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// TestExcludeMergedInFiles_KeepsDeletionAgainstStaleBaseParent: a "base merge" whose second parent is an old base commit that predates a test lets the merge drop the test while the result equals that parent (so --cc would not list it); base never touched the test since the fork, so the deletion stays the task's.
//
// TestExcludeMergedInFiles_KeepsDeletionAgainstStaleBaseParent：「基线合并」的第二父
// 选一个测试还不存在的旧基线提交，在合并里删掉测试，合并结果等于该父（--cc 不会列
// 出）；基线自分叉以来没碰过这个测试，所以删除仍算本任务的。
func TestExcludeMergedInFiles_KeepsDeletionAgainstStaleBaseParent(t *testing.T) {
	dir := setupRecurrentRepo(t)
	old := headCommit(t, dir)
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc TestSeed() {}\n"}, "seed test")
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n\nfunc own() {}\n"}, "task own work")
	runGit(t, dir, "rm", "-q", "x_test.go")
	tree := mustGitOut(t, dir, "write-tree")
	merge := mustGitOut(t, dir, "commit-tree", tree, "-p", "HEAD", "-p", old, "-m", "merge stale base")
	runGit(t, dir, "reset", "-q", "--hard", merge)

	committed := committedSince(t, dir, start)
	if !slices.Contains(committed, "x_test.go") {
		t.Fatalf("fixture: net diff must show x_test.go deleted, got %v", committed)
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("deletion hidden in a merge against a stale base parent must stay, got %v", got)
	}
}

// TestExcludeMergedInFiles_KeepsDeletionPairedAsRename: the task deletes a test while base adds an identical copy; the caller's rename-detecting net diff pairs them as a rename and lists only the base copy. The deleted path must come back and the base copy must be dropped.
//
// TestExcludeMergedInFiles_KeepsDeletionPairedAsRename：本任务删掉测试，同期基线新增
// 一份相同内容的拷贝；调用方开着重命名检测的净 diff 把两者配成重命名、只列出基线
// 的拷贝。被删的路径必须补回，基线的拷贝必须剔除。
func TestExcludeMergedInFiles_KeepsDeletionPairedAsRename(t *testing.T) {
	dir := setupRecurrentRepo(t)
	content := "package main\n\nfunc TestA() {\n\tif 1+1 != 2 {\n\t\tpanic(\"math\")\n\t}\n}\n"
	writeCommitSource(t, dir, map[string]string{"x_test.go": content}, "seed test")
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	runGit(t, dir, "rm", "-q", "x_test.go")
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n\nfunc own() {}\n"}, "task deletes test")
	runGit(t, dir, "checkout", "main")
	writeCommitSource(t, dir, map[string]string{"copy_test.go": content}, "base copies test")
	runGit(t, dir, "checkout", "feat/testcov")
	runGit(t, dir, "merge", "--no-edit", "main")

	committed := committedSince(t, dir, start)
	if slices.Contains(committed, "x_test.go") || !slices.Contains(committed, "copy_test.go") {
		t.Fatalf("fixture: rename detection must pair the deletion with the base copy, got %v", committed)
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("task's test deletion must be restored from the rename-free net diff, got %v", got)
	}
	if slices.Contains(got, "copy_test.go") {
		t.Errorf("base copy must be dropped, got %v", got)
	}
}

// TestExcludeMergedInFiles_KeepsTaskSideEditOverwrittenByBase: an earlier task on the same branch strengthened a test before from; the current task merges base, which changed the same test, and resolves the conflict by taking base's side. The result equals base's version, but the task side had changed the file, so dropping the strengthening is the task's change.
//
// TestExcludeMergedInFiles_KeepsTaskSideEditOverwrittenByBase：同分支上前序任务在 from
// 之前加强了测试；本任务合并也改过该测试的基线，冲突时整体取基线一侧。合并结果等于
// 基线版本，但任务一侧改过这个文件，丢掉那次加强算本任务的改动。
func TestExcludeMergedInFiles_KeepsTaskSideEditOverwrittenByBase(t *testing.T) {
	dir := setupRecurrentRepo(t)
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc TestX() { a(1) }\n"}, "seed test")
	runGit(t, dir, "branch", "-f", "main")
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc TestX() { a(1); a(2); a(3) }\n"}, "earlier task strengthens test")
	start := headCommit(t, dir)
	writeCommitSource(t, dir, map[string]string{"own.go": "package main\n\nfunc own() {}\n"}, "task own work")
	runGit(t, dir, "checkout", "main")
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\nfunc TestX() { b(1) }\n", "base_only.go": "package main\n\nfunc base() {}\n"}, "base edits test")
	runGit(t, dir, "checkout", "feat/testcov")
	if err := exec.Command("git", "-C", dir, "merge", "--no-edit", "main").Run(); err == nil {
		t.Fatal("fixture: merging main must conflict on x_test.go")
	}
	runGit(t, dir, "checkout", "--theirs", "x_test.go")
	runGit(t, dir, "add", "x_test.go")
	runGit(t, dir, "commit", "--no-edit")

	committed := committedSince(t, dir, start)
	if !slices.Contains(committed, "x_test.go") {
		t.Fatalf("fixture: net diff must include x_test.go, got %v", committed)
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("overwriting a task-side test edit with base's version must stay the task's change, got %v", got)
	}
	if slices.Contains(got, "base_only.go") {
		t.Errorf("file brought in only by merging main must be dropped, got %v", got)
	}
}

// TestExcludeMergedInFiles_CrissCrossChecksEveryMergeBase: with two merge bases between the task side and base (an earlier merge plus a task commit that reached main through a merge PR), reverting a test to base's version inside the merge must stay the task's change no matter which merge base git would pick alone. Commit dates are pinned so the single merge base git picks is the one that would hide it.
//
// TestExcludeMergedInFiles_CrissCrossChecksEveryMergeBase：任务一侧与基线之间有两个
// merge-base（一次早先的合并，加一个经合并式 PR 进了 main 的任务提交）时，在合并里把
// 测试改回基线版本，无论 git 单独挑哪个 merge-base，都要算本任务的改动。固定提交
// 时间，使 git 单独挑出的恰好是会把它藏起来的那个。
func TestExcludeMergedInFiles_CrissCrossChecksEveryMergeBase(t *testing.T) {
	dir := setupRecurrentRepo(t)
	tick := 0
	at := func() {
		tick++
		date := fmt.Sprintf("2026-01-01T00:00:%02d +0000", tick)
		t.Setenv("GIT_AUTHOR_DATE", date)
		t.Setenv("GIT_COMMITTER_DATE", date)
	}
	at()
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\n// v1\n"}, "seed test")
	runGit(t, dir, "branch", "-f", "main")
	runGit(t, dir, "checkout", "main")
	at()
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\n// v2\n"}, "b1: base edits test")
	runGit(t, dir, "checkout", "feat/testcov")
	at()
	writeCommitSource(t, dir, map[string]string{"own1.go": "package main\n\nfunc own1() {}\n"}, "t1")
	t1 := headCommit(t, dir)
	at()
	runGit(t, dir, "merge", "--no-commit", "main")
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\n// v3 strongest\n"}, "M1: strengthen test while merging")
	start := headCommit(t, dir)
	runGit(t, dir, "checkout", "main")
	at()
	runGit(t, dir, "merge", "--no-ff", "--no-edit", t1)
	at()
	writeCommitSource(t, dir, map[string]string{"base2.go": "package main\n\nfunc base2() {}\n"}, "b2")
	runGit(t, dir, "checkout", "feat/testcov")
	at()
	writeCommitSource(t, dir, map[string]string{"own2.go": "package main\n\nfunc own2() {}\n"}, "t2")
	if bases := strings.Fields(mustGitOut(t, dir, "merge-base", "--all", "HEAD", "main")); len(bases) != 2 {
		t.Fatalf("fixture: need two merge bases, got %v", bases)
	}
	if got := mustGitOut(t, dir, "merge-base", "HEAD", "main"); got != t1 {
		t.Fatalf("fixture: git must pick t1 as the single merge base, got %s", got)
	}
	at()
	runGit(t, dir, "merge", "--no-commit", "main")
	writeCommitSource(t, dir, map[string]string{"x_test.go": "package main\n\n// v2\n"}, "M2: revert test to base version")

	committed := committedSince(t, dir, start)
	if !slices.Contains(committed, "x_test.go") {
		t.Fatalf("fixture: net diff must include x_test.go, got %v", committed)
	}
	got := ExcludeMergedInFiles(dir, start, committed)
	if !slices.Contains(got, "x_test.go") {
		t.Errorf("reverting the task's test strengthening inside a criss-cross merge must stay, got %v", got)
	}
	if slices.Contains(got, "base2.go") {
		t.Errorf("file brought in only by merging main must be dropped, got %v", got)
	}
}

// TestExcludeMergedInFiles_PureRenameMatchesNoMerge: a plain rename by the task lists only the new path, with or without a later merge of base; restoring old paths applies only when the new path was dropped.
//
// TestExcludeMergedInFiles_PureRenameMatchesNoMerge：本任务的纯重命名只列新路径，之后
// 合不合并基线都一样；补回旧路径只在新路径被剔掉时发生。
func TestExcludeMergedInFiles_PureRenameMatchesNoMerge(t *testing.T) {
	dir := setupRecurrentRepo(t)
	writeCommitSource(t, dir, map[string]string{"old.go": "package main\n\nfunc a() {}\nfunc b() {}\nfunc c() {}\n"}, "seed")
	runGit(t, dir, "branch", "-f", "main")
	start := headCommit(t, dir)
	runGit(t, dir, "mv", "old.go", "new.go")
	runGit(t, dir, "commit", "-m", "task renames")
	runGit(t, dir, "checkout", "main")
	writeCommitSource(t, dir, map[string]string{"base_only.go": "package main\n\nfunc base() {}\n"}, "base work")
	runGit(t, dir, "checkout", "feat/testcov")
	runGit(t, dir, "merge", "--no-edit", "main")

	committed := committedSince(t, dir, start)
	got := ExcludeMergedInFiles(dir, start, committed)
	if want := []string{"new.go"}; !slices.Equal(got, want) {
		t.Fatalf("pure rename after a merge must list only the new path, got %v want %v (committed %v)", got, want, committed)
	}
}

package taskpipeline

import (
	"testing"
	"time"
)

// saveCompletedSibling 持久化一个【已完成】兄弟任务 state，其记录跨度为
// (headCommit → finalHead)——excludeForeignCommitted 的归属锚。
func saveCompletedSibling(t *testing.T, dir, ref, headCommit, finalHead string) {
	t.Helper()
	now := time.Now()
	s := &TaskState{
		TaskRef:     ref,
		Branch:      "feat/testcov",
		HeadCommit:  headCommit,
		CompletedAt: &now,
	}
	s.History = append(s.History,
		TaskGateResult{Gate: "task-implement", Passed: true, CompletedAt: now, HeadCommit: finalHead},
		TaskGateResult{Gate: "task-complete", Passed: true, CompletedAt: now, HeadCommit: finalHead},
	)
	if err := SaveTaskState(dir, s); err != nil {
		t.Fatalf("SaveTaskState(%s): %v", ref, err)
	}
}

func containsFile(files []string, name string) bool {
	for _, f := range files {
		if f == name {
			return true
		}
	}
	return false
}

// TestTaskChangedFiles_ExcludesCompletedSiblingCommits 是 scope-drift 记账漏洞的
// 回归钉（2026-08 usage 实证，finding：feat/eval-mutex-set）：任务 B 在 C0
// 启动；兄弟任务 A 随后在同分支提交 foo.go/bar.go 并【已完成】。B 的已提交
// diff（C0..HEAD）字面上含 A 的文件，但这些文件在 A 完成时已记过账——B 的
// 改动集合必须排除它们（不再有 scope-drift 噪音、不再双重 test-coverage 计
// 费），而 B 自己提交的文件保留。
func TestTaskChangedFiles_ExcludesCompletedSiblingCommits(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	c0 := headShort(t, dir)

	// 兄弟任务 A：C0 启动，提交 foo.go+bar.go，已完成。
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
		"bar.go": "package main\n\nfunc Bar() int { return 1 }\n",
	}, "task A work")
	c1 := headShort(t, dir)
	saveCompletedSibling(t, dir, "task-a", c0, c1)

	// 任务 B 在 C0 启动（A 的 commit 落地之前）。其裸已提交 diff 含 A 的
	// 文件；归属必须剥掉它们。
	stateB := &TaskState{TaskRef: "task-b", Branch: "feat/testcov", HeadCommit: c0}
	changed := taskChangedFiles(dir, stateB)
	if containsFile(changed, "foo.go") || containsFile(changed, "bar.go") {
		t.Fatalf("A's completed-task files must be excluded from B's change set, got %v", changed)
	}

	// B 自己的 commit 保留。
	writeCommitSource(t, dir, map[string]string{
		"baz.go": "package main\n\nfunc Baz() int { return 1 }\n",
	}, "task B own work")
	changed = taskChangedFiles(dir, stateB)
	if !containsFile(changed, "baz.go") {
		t.Fatalf("B's own committed file must be kept, got %v", changed)
	}
	if containsFile(changed, "foo.go") || containsFile(changed, "bar.go") {
		t.Fatalf("foreign files must stay excluded after B's own commit, got %v", changed)
	}

	// B 随后自己真的改了 foo.go：触及 commit 不再全是外来 → foo.go 回到 B 的
	// 改动集合（真实越界检出保留）。
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 2 }\n",
	}, "task B touches foo.go")
	changed = taskChangedFiles(dir, stateB)
	if !containsFile(changed, "foo.go") {
		t.Fatalf("foo.go must re-enter B's change set once B touches it, got %v", changed)
	}
}

// TestTaskChangedFiles_InProgressSiblingNotAnAnchor 钉住反覆盖黑洞规则：只有
// 【已完成】任务可作归属锚。进行中兄弟（无 CompletedAt）跨度开口——让它抢
// 归属会让两个并行任务互相吞掉对方文件、两边都不记账。兄弟完成前，其文件
// 留在当前任务的集合内。
func TestTaskChangedFiles_InProgressSiblingNotAnAnchor(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	c0 := headShort(t, dir)

	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "in-progress sibling work")
	c1 := headShort(t, dir)

	// 进行中兄弟：跨度锚已记录但未完成。
	s := &TaskState{TaskRef: "task-open", Branch: "feat/testcov", HeadCommit: c0}
	s.History = append(s.History,
		TaskGateResult{Gate: "task-implement", Passed: true, CompletedAt: time.Now(), HeadCommit: c1})
	if err := SaveTaskState(dir, s); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	stateB := &TaskState{TaskRef: "task-b", Branch: "feat/testcov", HeadCommit: c0}
	changed := taskChangedFiles(dir, stateB)
	if !containsFile(changed, "foo.go") {
		t.Fatalf("in-progress sibling must NOT anchor attribution (coverage hole), got %v", changed)
	}

	// 兄弟一旦完成，归属锚生效、文件离开 B 的集合——顺序工作流的噪音恰在
	// 完成时停止。
	saveCompletedSibling(t, dir, "task-open", c0, c1)
	changed = taskChangedFiles(dir, stateB)
	if containsFile(changed, "foo.go") {
		t.Fatalf("completed sibling's file must be excluded, got %v", changed)
	}
}

// TestTaskChangedFiles_AttributionDegradesSafely 钉住保守方向：无兄弟 state /
// 兄弟缺锚 / 跨度 commit 已死，全部回落未过滤 diff（旧行为）——归属逻辑绝不
// 能意外藏掉当前任务的文件。
func TestTaskChangedFiles_AttributionDegradesSafely(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	c0 := headShort(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "work")
	headShort(t, dir)

	// 无任何兄弟 state → 行为不变。
	stateB := &TaskState{TaskRef: "task-b", Branch: "feat/testcov", HeadCommit: c0}
	if changed := taskChangedFiles(dir, stateB); !containsFile(changed, "foo.go") {
		t.Fatalf("no siblings: foo.go must be present, got %v", changed)
	}

	// 兄弟已完成但无记录锚（老 state）→ 无跨度。
	now := time.Now()
	legacy := &TaskState{TaskRef: "task-legacy", Branch: "feat/testcov", CompletedAt: &now}
	if err := SaveTaskState(dir, legacy); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	if changed := taskChangedFiles(dir, stateB); !containsFile(changed, "foo.go") {
		t.Fatalf("anchorless sibling: foo.go must be present, got %v", changed)
	}

	// 兄弟跨度已死（start commit 从不存在）→ 跨度跳过。
	saveCompletedSibling(t, dir, "task-dead", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "cafebabecafebabecafebabecafebabecafebabe")
	if changed := taskChangedFiles(dir, stateB); !containsFile(changed, "foo.go") {
		t.Fatalf("dead span: foo.go must be present, got %v", changed)
	}
}

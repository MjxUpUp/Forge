package taskpipeline

import (
	"testing"
)

// TestTaskChangedFiles_InterleavedFirstToComplete pins the design tradeoff for
// interleaved timing (review minor #1): B commits qux.go BEFORE sibling task A
// completes, and A's final head descends from B's commit (same branch) — B's
// commit then falls INSIDE A's recorded span (rev-list is a range, blind to
// authorship), so qux.go is attributed to A and excluded from B's change set.
// This "first-to-complete accounts it" rule is deliberate: the file was part
// of A's own diff when A completed, so its scope/test-coverage demand fired
// exactly once (at A's completion) — never twice, never zero. The cost is
// authorship imprecision on the interleaved edge; the alternative (letting
// in-progress spans steal attribution) lets two open tasks mutually swallow
// each other's files so NEITHER accounts them — a coverage hole, worse than
// the imprecision. B's commits made AFTER A's completion are not in A's span
// and stay attributed to B.
//
// TestTaskChangedFiles_InterleavedFirstToComplete 钉住交错时序下的设计取舍
// （review minor #1）：B 在兄弟任务 A 完成【之前】提交 qux.go，且 A 的最终
// head 是 B 该 commit 的后代（同分支）——B 的 commit 随即落入 A 的记录跨度
// （rev-list 是区间、不辨作者），qux.go 归 A 记账、从 B 的改动集合排除。
// 「先完成者记账」是刻意取舍：A 完成时该文件本就在 A 自己的 diff 里，其
// scope/test-coverage 要求在 A 完成时恰已触发一次——不双重计费、不零计费。
// 代价是交错棱上的作者归属不精确；反面方案（让进行中跨度抢归属）会让两个
// 开口任务互吞对方文件、两边都不记账——覆盖黑洞，比不精确更糟。A 完成之后
// B 的 commit 不在 A 的跨度内，仍归 B。
func TestTaskChangedFiles_InterleavedFirstToComplete(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	c0 := headShort(t, dir)

	// B starts at C0 and commits qux.go FIRST (before A completes).
	//
	// B 在 C0 启动并【先】提交 qux.go（A 完成之前）。
	stateB := &TaskState{TaskRef: "task-b", Branch: "feat/testcov", HeadCommit: c0}
	writeCommitSource(t, dir, map[string]string{
		"qux.go": "package main\n\nfunc Qux() int { return 1 }\n",
	}, "task B early commit")
	cB := headShort(t, dir)

	// A commits on top (descending from B's commit) and completes with final
	// head cA — its span c0..cA contains B's commit cB.
	//
	// A 在其上提交（以 B 的 commit 为祖先）并以最终 head cA 完成——其跨度
	// c0..cA 含 B 的 commit cB。
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "task A work")
	cA := headShort(t, dir)
	saveCompletedSibling(t, dir, "task-a", c0, cA)

	// The tradeoff: B's own early commit qux.go is swallowed by A's completed
	// span — attributed to A (which accounted for it at A's completion).
	//
	// 取舍点：B 自己的早期 commit qux.go 被 A 的已完成跨度吞没——归 A 记账
	//（A 完成时已为它计过账）。
	changed := taskChangedFiles(dir, stateB)
	if containsFile(changed, "qux.go") {
		t.Fatalf("interleaved: B's pre-completion commit qux.go should be attributed to first-completer A (span %s..%s contains %s), got %v", c0, cA, cB, changed)
	}
	if containsFile(changed, "foo.go") {
		t.Fatalf("A's own file foo.go must be excluded from B's set, got %v", changed)
	}

	// B's commit AFTER A's completion is outside A's span → stays with B.
	//
	// A 完成【之后】B 的 commit 在 A 的跨度外 → 仍归 B。
	writeCommitSource(t, dir, map[string]string{
		"late.go": "package main\n\nfunc Late() int { return 1 }\n",
	}, "task B commit after A completed")
	changed = taskChangedFiles(dir, stateB)
	if !containsFile(changed, "late.go") {
		t.Fatalf("B's post-completion commit must stay attributed to B, got %v", changed)
	}
}

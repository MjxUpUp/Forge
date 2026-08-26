package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnrichFinding pins the review-context stamping on findings (2026-08 review-
// observability): Round derives from completed review rounds (len(ReviewRounds)+1 —
// 1 before the first pass, 2 after it), ChangeHash is the worktree-vs-HEAD source
// fingerprint at raise time (same computation as the review-pass binding, so a
// finding's hash is comparable with ReviewedChangeHash), identical code yields an
// identical hash across findings, explicit values survive (import/backfill), and a
// non-git root degrades to an empty hash without blocking the record.
//
// TestEnrichFinding 钉住 finding 的审查上下文打戳（2026-08 评审可观测性）：
// Round 从已完成审查轮次推导（len(ReviewRounds)+1——首次 pass 前为 1，其后为 2）；
// ChangeHash 是提出时工作区相对 HEAD 的源码指纹（与 review-pass 绑定同一算法，
// 可与 ReviewedChangeHash 比较）；代码不变则两次打戳 hash 相同；显式值保留
// （导入/回填）；非 git 目录降级为空 hash 且不阻断记录。
func TestEnrichFinding(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
	}, "add a")

	state := &TaskState{TaskRef: "t-enrich"}

	// Clean worktree right after a commit: no source delta vs HEAD → empty hash
	// (fail-open), Round still derived.
	//
	// 提交后的干净工作区：相对 HEAD 无源码增量 → hash 为空（fail-open），
	// Round 照常推导。
	f0 := Finding{Content: "clean"}
	EnrichFinding(dir, state, &f0)
	if f0.Round != 1 {
		t.Errorf(`无 ReviewRounds 时 Round 应为 1, got %d`, f0.Round)
	}
	if f0.ChangeHash != "" {
		t.Errorf(`干净工作区 ChangeHash 应为空（无源码增量）, got %q`, f0.ChangeHash)
	}

	// Uncommitted source change → non-empty hash; a second finding on unchanged
	// code gets the identical hash (the stability join key).
	//
	// 未提交源码变更 → hash 非空；代码不再变动时第二条 finding 拿到相同 hash
	//（稳定性比对 join key）。
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n\nfunc A() { println(1) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f1 := Finding{Content: "x"}
	EnrichFinding(dir, state, &f1)
	if f1.ChangeHash == "" {
		t.Error(`有未提交源码变更时 ChangeHash 应非空`)
	}
	f2 := Finding{Content: "y"}
	EnrichFinding(dir, state, &f2)
	if f2.ChangeHash != f1.ChangeHash {
		t.Errorf(`代码未变，两次打戳 hash 应一致: %q vs %q`, f1.ChangeHash, f2.ChangeHash)
	}

	// One completed review round → the next finding belongs to round 2.
	//
	// 完成一轮审查后 → 下一条 finding 属第 2 轮。
	state.ReviewRounds = append(state.ReviewRounds, ReviewRound{HeadCommit: GetHeadCommit(dir), ChangeHash: f1.ChangeHash})
	f3 := Finding{Content: "z"}
	EnrichFinding(dir, state, &f3)
	if f3.Round != 2 {
		t.Errorf(`一轮 ReviewRounds 后 Round 应为 2, got %d`, f3.Round)
	}

	// Explicit values are respected (import/backfill of findings from another cycle).
	//
	// 显式值保留（跨周期导入/回填 finding 的场景）。
	f4 := Finding{Content: "manual", Round: 5, ChangeHash: "manual-hash"}
	EnrichFinding(dir, state, &f4)
	if f4.Round != 5 || f4.ChangeHash != "manual-hash" {
		t.Errorf(`显式 Round/ChangeHash 不得被覆盖, got round=%d hash=%q`, f4.Round, f4.ChangeHash)
	}

	// Non-git root → empty hash, round still derived, never an error.
	//
	// 非 git 目录 → hash 留空，Round 照常推导，不报错。
	f5 := Finding{Content: "nogit"}
	EnrichFinding(t.TempDir(), state, &f5)
	if f5.ChangeHash != "" {
		t.Errorf(`非 git 目录 ChangeHash 应为空, got %q`, f5.ChangeHash)
	}
	if f5.Round != 2 {
		t.Errorf(`非 git 目录 Round 仍应推导为 2, got %d`, f5.Round)
	}
}

package taskpipeline

// complete_prereqs_test.go — 两道门禁共用前置的钉子（review-ruthless-3 的 P1-1/P2-1）：
// task complete 自己也复核 review 快照，gate task-complete 也复核验收已登记且新鲜——
// 否则 gate 绿、complete 才拦，或审后改码还能完结。

import (
	"os/exec"
	"strings"
	"testing"
)

func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestCheckCompleteReviewPrereqs_BlocksStaleSnapshot(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{"main.go": "package main\n"}, "init")

	state := &TaskState{TaskRef: "feat/stale", ReviewPassed: true, ReviewedHeadCommit: headOf(t, dir), ReviewedChangeHash: "not-the-real-hash"}
	err := CheckCompleteReviewPrereqs(dir, state)
	if err == nil {
		t.Fatal("审查快照不一致必须拒绝完成（审后改码未复审）")
	}
	if !strings.Contains(err.Error(), "审查通过后检测到源码变更") {
		t.Fatalf("错误文案应指出审后源码变更，got: %v", err)
	}
}

func TestCheckCompleteReviewPrereqs_RequiresReviewPassed(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	if err := CheckCompleteReviewPrereqs(dir, &TaskState{TaskRef: "feat/norev"}); err == nil {
		t.Fatal("未 review pass 必须拒绝")
	}
}

func TestCheckCompleteAcceptancePrereqs_BlocksZeroAcceptance(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	err := CheckCompleteAcceptancePrereqs(dir, &TaskState{TaskRef: "feat/zero"})
	if err == nil {
		t.Fatal("零验收标准时 task-complete 门禁必须与 complete 同口径拒绝")
	}
}

func TestCheckCompleteAcceptancePrereqs_BlocksStaleRun(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{"main.go": "package main\n"}, "init")
	state := &TaskState{TaskRef: "feat/stale", Acceptance: ParseAcceptance([]string{"true ::"})}
	if err := CheckCompleteAcceptancePrereqs(dir, state); err == nil {
		t.Fatal("验收从未实跑时 task-complete 门禁必须拒绝")
	}
}

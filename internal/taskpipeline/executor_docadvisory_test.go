package taskpipeline

import (
	"os"
	"strings"
	"testing"
)

// TestExecuteTaskGate_DocAdvisoryPointsToDocReviewSkill 钉住 task-verify 的 L2 回检
// advisory 文案：任务改了 markdown 且尚无 fresh 回检时，指引必须是「按 doc-review
// skill 评审」，不得回退到 code-review-gate 时代的内部路径（rubric-docs.md 已迁至
// skills/doc-review/references/，skill 是流程真相源——forge 二进制依赖 skill）。
//
// TestExecuteTaskGate_DocAdvisoryPointsToDocReviewSkill pins the task-verify L2
// re-check advisory text: with task-changed markdown and no fresh review, the
// guidance must say "按 doc-review skill 评审" and never the pre-migration
// code-review-gate internal path (the skill is the process truth source).
func TestExecuteTaskGate_DocAdvisoryPointsToDocReviewSkill(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	base := GetHeadCommit(dir)
	if err := os.WriteFile(dir+"/notes.md", []byte("# 笔记\n干净内容，`forge docs lint` 通过。\n"), 0644); err != nil {
		t.Fatal(err)
	}

	state := newVerifyState(t, dir, "doc-adv")
	state.HeadCommit = base

	stderr := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf(`task-verify 应 PASS（doc advisory 不阻塞）, got err: %v`, err)
		}
	})
	if !strings.Contains(stderr, "按 doc-review skill 评审") {
		t.Fatalf("doc advisory 应指引按 doc-review skill 评审, got stderr: %s", stderr)
	}
	if strings.Contains(stderr, "code-review-gate/references/rubric-docs.md") {
		t.Fatalf("doc advisory 不得引用已迁移旧路径, got stderr: %s", stderr)
	}
}

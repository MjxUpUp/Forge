package cli

// skills_revert_test.go — pure-logic tests for scoped revert (decision filtering + locate-by-id).
// The git revert part is git integration, not covered by unit tests (verified manually/e2e).
//
// skills_revert_test.go — scoped revert 纯逻辑测试（决策筛选 + 按 id 定位）。
// git revert 部分是 git integration，不在单元测试覆盖（manual/e2e 验证）。

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
)

func TestDecisionsWithCommit(t *testing.T) {
	in := []skillsdecisions.SkillDecision{
		{ID: "d1", CommitHash: "abc123"},
		{ID: "d2", CommitHash: ""}, // 无 commit → 过滤
		{ID: "d3", CommitHash: "def456"},
	}
	got := decisionsWithCommit(in)
	if len(got) != 2 {
		t.Fatalf("got %d decisions, want 2", len(got))
	}
	if got[0].ID != "d1" || got[1].ID != "d3" {
		t.Errorf("order/content wrong: got %s, %s", got[0].ID, got[1].ID)
	}
}

func TestDecisionsWithCommit_Empty(t *testing.T) {
	if got := decisionsWithCommit(nil); len(got) != 0 {
		t.Errorf("nil input got %v", got)
	}
	if got := decisionsWithCommit([]skillsdecisions.SkillDecision{{ID: "x"}}); len(got) != 0 {
		t.Errorf("all-no-commit got %v", got)
	}
}

func TestFindDecisionByID(t *testing.T) {
	in := []skillsdecisions.SkillDecision{
		{ID: "d1", CommitHash: "abc"},
		{ID: "d3", CommitHash: "def"},
	}
	if got := findDecisionByID(in, "d3"); got == nil || got.CommitHash != "def" {
		t.Errorf("find d3: got %+v", got)
	}
	if got := findDecisionByID(in, "missing"); got != nil {
		t.Errorf("missing should return nil, got %+v", got)
	}
}

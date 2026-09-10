package tasktypes

import (
	"testing"
	"time"
)

// TestTaskState_SealedAt 钉住证据封印点语义（docs/design/harness-fixes-a-g-2026-09.md E）：
// 封印 = task-complete 门禁通过那一刻，不是 forge task complete 成功那一刻——两者之间
// （doc-gate 卡住的两天）落到任务名下的行不属于完成声明的证据。
func TestTaskState_SealedAt(t *testing.T) {
	t0 := time.Date(2026, 9, 7, 22, 50, 0, 0, time.UTC)
	gateAt := t0.Add(56 * time.Minute)
	doneAt := t0.Add(46 * time.Hour)

	t.Run("unsealed while gates incomplete", func(t *testing.T) {
		s := &TaskState{TaskRef: "fix/x", StartedAt: t0}
		s.History = append(s.History, TaskGateResult{Gate: GateImplement, Passed: true, CompletedAt: t0.Add(time.Minute)})
		if s.EvidenceSealed() {
			t.Fatal("task with only implement gate passed must not be sealed")
		}
		if !s.SealedAt().IsZero() {
			t.Fatalf("SealedAt must be zero before seal, got %v", s.SealedAt())
		}
	})

	t.Run("sealed at task-complete gate even before CompletedAt", func(t *testing.T) {
		s := &TaskState{TaskRef: "fix/x", StartedAt: t0}
		for i, g := range []string{GateImplement, GateVerify, GateComplete} {
			s.History = append(s.History, TaskGateResult{Gate: g, Passed: true, CompletedAt: t0.Add(time.Duration(i+1) * time.Minute)})
		}
		s.History[2].CompletedAt = gateAt
		if !s.EvidenceSealed() {
			t.Fatal("all gates passed must seal evidence")
		}
		if got := s.SealedAt(); !got.Equal(gateAt) {
			t.Fatalf("SealedAt = %v, want task-complete gate time %v", got, gateAt)
		}
		// CompletedAt 两天后才写入：封印时间不能漂到 CompletedAt。
		s.CompletedAt = &doneAt
		if got := s.SealedAt(); !got.Equal(gateAt) {
			t.Fatalf("SealedAt after CompletedAt = %v, want gate time %v (not %v)", got, gateAt, doneAt)
		}
	})

	t.Run("generic task seals at CompletedAt", func(t *testing.T) {
		s := &TaskState{TaskRef: "research/y", Kind: TaskKindGeneric, StartedAt: t0, CompletedAt: &doneAt}
		if !s.EvidenceSealed() {
			t.Fatal("completed generic task must be sealed")
		}
		if got := s.SealedAt(); !got.Equal(doneAt) {
			t.Fatalf("generic SealedAt = %v, want CompletedAt %v", got, doneAt)
		}
	})
}

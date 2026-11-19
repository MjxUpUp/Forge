package taskpipeline

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

func TestBuildTaskEvidence(t *testing.T) {
	score := 88
	state := &TaskState{
		TaskRef: "feat/x",
		Acceptance: []tasktypes.AcceptanceCriterion{
			{Run: "go test ./a", Passed: true},
			{Run: "go test ./b", Passed: true},
			{Run: "go test ./c"},
		},
		RedAttempts: []tasktypes.RedAttempt{
			{Run: "go test ./a", Exit: 1},
			{Run: "go test ./zzz", Exit: 1},
		},
		ReviewRounds: []tasktypes.ReviewRound{{Reviewer: "rev-1"}, {Reviewer: "rev-1"}},
		ReviewScore:  &score,
		Overrides:    tasktypes.TaskOverrides{WorkActivity: "disable"},
		AllowGreen:   true,
		Findings: []tasktypes.Finding{
			{Severity: "P1", Content: "still open", Status: "open"},
			{Severity: "P2", Content: "fixed", Status: "fixed"},
		},
	}
	ev := BuildTaskEvidence(".", state)
	if ev.Criteria != 3 || ev.CriteriaPassed != 2 {
		t.Fatalf("criteria: %+v", ev)
	}
	if ev.RedFirstTrials != 2 || ev.RedFirstClosed != 1 {
		t.Fatalf("red first: trials=%d closed=%d", ev.RedFirstTrials, ev.RedFirstClosed)
	}
	if ev.ReviewRounds != 2 || ev.ReviewScore == nil || *ev.ReviewScore != 88 {
		t.Fatalf("review: %+v", ev)
	}
	// 一个 override + allow-green = 2 次逃生
	if ev.Escapes != 2 {
		t.Fatalf("escapes=%d", ev.Escapes)
	}
	if len(ev.RemainingRisks) != 1 {
		t.Fatalf("risks=%v", ev.RemainingRisks)
	}
}

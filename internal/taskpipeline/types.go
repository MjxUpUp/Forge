package taskpipeline

import (
	"time"

	"github.com/Harness/forge/internal/scoringtypes"
	"github.com/Harness/forge/internal/toolusage"
)

// TaskGate defines a lightweight task-level quality gate.
type TaskGate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Auto        bool   `json:"auto"` // true = checked automatically by hooks
}

// TaskState tracks the state of a single task's pipeline.
// Stored in .forge/tasks/{sanitized-ref}.json.
type TaskState struct {
	TaskRef     string           `json:"task_ref"`
	Branch      string           `json:"branch"`
	Source      string           `json:"source"` // "explicit", "branch"
	Summary     string           `json:"summary"`
	CurrentGate string           `json:"current_gate"`
	History     []TaskGateResult        `json:"history"`
	StartedAt   time.Time               `json:"started_at"`
	CompletedAt *time.Time              `json:"completed_at,omitempty"`
	Score       *scoringtypes.ScoreResult `json:"score,omitempty"`
	HeadCommit  string                    `json:"head_commit,omitempty"` // for duplicate detection
	SessionID   string                    `json:"session_id,omitempty"`  // agent session that created this task
	ToolUsage   *toolusage.ToolUsageSummary `json:"tool_usage,omitempty"`  // tool usage stats for scoring
}

// TaskGateResult records the outcome of a single task gate.
type TaskGateResult struct {
	Gate        string    `json:"gate"`
	Passed      bool      `json:"passed"`
	CompletedAt time.Time `json:"completed_at"`
	HeadCommit  string    `json:"head_commit,omitempty"` // git HEAD at gate pass time
}

// IsComplete returns true if all task gates have passed.
func (s *TaskState) IsComplete() bool {
	if len(s.History) == 0 {
		return false
	}
	gates := DefaultGates()
	for _, g := range gates {
		if !s.gatePassed(g.ID) {
			return false
		}
	}
	return true
}

// NextGate returns the next incomplete gate in sequence, or "" if all done.
func (s *TaskState) NextGate() string {
	gates := DefaultGates()
	for _, g := range gates {
		if !s.gatePassed(g.ID) {
			return g.ID
		}
	}
	return ""
}

// MarkComplete records task completion time.
func (s *TaskState) MarkComplete() {
	now := time.Now()
	s.CompletedAt = &now
	s.CurrentGate = ""
}

// RecordGateResult adds a gate result and advances CurrentGate.
// If the gate was already passed, this is a no-op (prevents duplicate history
// entries from stop hook re-verification). A previously failed gate can be
// retried and will add a new entry.
func (s *TaskState) RecordGateResult(gateID string, passed bool, headCommit string) {
	// Skip if this gate was already passed — prevents 25x duplicate entries
	// from stop hook repeatedly verifying the same gate.
	if passed && s.gatePassed(gateID) {
		return
	}

	s.History = append(s.History, TaskGateResult{
		Gate:        gateID,
		Passed:      passed,
		CompletedAt: time.Now(),
		HeadCommit:  headCommit,
	})
	if passed {
		s.CurrentGate = s.NextGate()
	} else {
		s.CurrentGate = gateID
	}
}

// gatePassed checks if a specific gate has passed.
func (s *TaskState) gatePassed(gateID string) bool {
	for _, r := range s.History {
		if r.Gate == gateID && r.Passed {
			return true
		}
	}
	return false
}

// designGateCommit returns the HEAD commit recorded when task-design was passed.
// Returns empty string if task-design has not been passed or no commit was recorded.
func (s *TaskState) designGateCommit() string {
	for i := len(s.History) - 1; i >= 0; i-- {
		if s.History[i].Gate == "task-design" && s.History[i].Passed {
			return s.History[i].HeadCommit
		}
	}
	return ""
}

// CompletedGates returns a list of passed gate IDs.
func (s *TaskState) CompletedGates() []string {
	var result []string
	for _, g := range DefaultGates() {
		if s.gatePassed(g.ID) {
			result = append(result, g.ID)
		}
	}
	return result
}

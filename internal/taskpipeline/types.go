package taskpipeline

import (
	"time"

	"github.com/Harness/forge/internal/scoringtypes"
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
}

// TaskGateResult records the outcome of a single task gate.
type TaskGateResult struct {
	Gate        string    `json:"gate"`
	Passed      bool      `json:"passed"`
	CompletedAt time.Time `json:"completed_at"`
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
func (s *TaskState) RecordGateResult(gateID string, passed bool) {
	s.History = append(s.History, TaskGateResult{
		Gate:        gateID,
		Passed:      passed,
		CompletedAt: time.Now(),
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

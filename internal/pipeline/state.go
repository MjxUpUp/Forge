package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// State represents the pipeline state machine (state.json).
type State struct {
	PipelineVersion string        `json:"pipeline_version"`
	Mode            string        `json:"mode"`
	CurrentGate     string        `json:"current_gate"`
	StartedAt       time.Time     `json:"started_at"`
	History         []GateHistory `json:"history"`
	Overrides       []Override    `json:"overrides"`

	// Snapshot records the project state detected at init time.
	// Used by forge init to auto-skip gates for existing projects.
	Snapshot *SnapshotData `json:"snapshot,omitempty"`

	// LastSyncVersion records which binary version last synced .forge/ files.
	// When the running binary version differs, autoSync rewrites hooks/skill/settings.
	LastSyncVersion string `json:"last_sync_version,omitempty"`
}

// SnapshotData records project signals and inferred gates at init time.
type SnapshotData struct {
	TakenAt       time.Time `json:"taken_at"`
	InferredGates []string  `json:"inferred_gates,omitempty"`
}

// GateHistory records one gate execution attempt. History is append-only.
type GateHistory struct {
	Gate        string    `json:"gate"`
	Passed      bool      `json:"passed"`
	Attempt     int       `json:"attempt"`
	CompletedAt time.Time `json:"completed_at"`
	DurationMs  int64     `json:"duration_ms"`
}

// Override records when a gate was force-skipped.
type Override struct {
	Gate   string `json:"gate"`
	Reason string `json:"reason"`
}

// LoadState reads state.json from .forge/ directory.
func LoadState(dir string) (*State, error) {
	path := filepath.Join(dir, ".forge", "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("state.json not found: run 'forge init' first")
		}
		return nil, fmt.Errorf("failed to read state.json: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse state.json: %w", err)
	}
	return &s, nil
}

// Save writes the state back to state.json.
func (s *State) Save(dir string) error {
	path := filepath.Join(dir, ".forge", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create .forge directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	if err := util.AtomicWrite(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state.json: %w", err)
	}
	return nil
}

// MarkGateStarted records the current gate being executed.
func (s *State) MarkGateStarted(gateID string) {
	s.CurrentGate = gateID
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now()
	}
}

// MarkGateComplete adds a gate result to history and clears CurrentGate.
func (s *State) MarkGateComplete(gateID string, passed bool, durationMs int64) {
	attempt := s.AttemptCount(gateID) + 1
	s.History = append(s.History, GateHistory{
		Gate:        gateID,
		Passed:      passed,
		Attempt:     attempt,
		CompletedAt: time.Now(),
		DurationMs:  durationMs,
	})
	s.CurrentGate = ""
}

// AttemptCount returns how many times a gate has been executed.
func (s *State) AttemptCount(gateID string) int {
	count := 0
	for _, h := range s.History {
		if h.Gate == gateID {
			count++
		}
	}
	return count
}

// AddOverride records a force override. Dedups by gate ID (A5): repeated --force
// on the same gate previously appended duplicate entries, so Overrides grew
// without bound — polluting state.json and inflating `forge status` output.
func (s *State) AddOverride(gateID, reason string) {
	if s.IsOverridden(gateID) {
		return
	}
	s.Overrides = append(s.Overrides, Override{Gate: gateID, Reason: reason})
}

// RemoveOverride clears a force override for a gate (A5). Called when a gate
// subsequently PASSES without --force: at that point the override is stale (the
// gate no longer needs force to satisfy its dependents — History records the
// real pass), so removing it keeps state clean instead of a one-time force
// permanently substituting for genuine passage on every future run.
func (s *State) RemoveOverride(gateID string) {
	var kept []Override
	for _, o := range s.Overrides {
		if o.Gate != gateID {
			kept = append(kept, o)
		}
	}
	s.Overrides = kept
}

// IsOverridden checks if a gate has been force-overridden.
func (s *State) IsOverridden(gateID string) bool {
	for _, o := range s.Overrides {
		if o.Gate == gateID {
			return true
		}
	}
	return false
}

// PrerequisitesPassed checks if all depends_on gates passed.
func (s *State) PrerequisitesPassed(dependsOn []string) error {
	// Build map of latest result per gate
	latestResult := make(map[string]bool)
	for _, h := range s.History {
		latestResult[h.Gate] = h.Passed
	}

	for _, depID := range dependsOn {
		// Overridden gates count as passed
		if s.IsOverridden(depID) {
			continue
		}
		passed, ok := latestResult[depID]
		if !ok {
			return fmt.Errorf("prerequisite gate '%s' has not been executed", depID)
		}
		if !passed {
			return fmt.Errorf("prerequisite gate '%s' failed: fix failures or use --force to skip", depID)
		}
	}
	return nil
}

// CompletedGates returns a map of gate IDs that have passed (latest run).
func (s *State) CompletedGates() map[string]bool {
	final := make(map[string]bool)
	for _, h := range s.History {
		final[h.Gate] = h.Passed
	}
	return final
}

// HasGateCompleted checks if a gate's latest run passed.
func HasGateCompleted(state *State, gateID string) bool {
	for i := len(state.History) - 1; i >= 0; i-- {
		if state.History[i].Gate == gateID {
			return state.History[i].Passed
		}
	}
	return false
}

// LatestGateResult returns the latest history entry for a gate.
func LatestGateResult(state *State, gateID string) *GateHistory {
	for i := len(state.History) - 1; i >= 0; i-- {
		if state.History[i].Gate == gateID {
			return &state.History[i]
		}
	}
	return nil
}

// GateFailures counts failed attempts for a gate in history.
// 用于 iteration circuit breaker——同一 gate 反复失败说明 loop 卡死，
// 闷头重试只烧 token 不解决问题，breaker 提示换策略（查 root cause / 拆小步）。
func GateFailures(state *State, gateID string) int {
	n := 0
	for i := range state.History {
		if state.History[i].Gate == gateID && !state.History[i].Passed {
			n++
		}
	}
	return n
}

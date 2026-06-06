package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GateStatus is the status.json output for a single gate execution.
type GateStatus struct {
	Gate            string        `json:"gate"`
	Passed          bool          `json:"passed"`
	Attempt         int           `json:"attempt"`
	Timestamp       time.Time     `json:"timestamp"`
	DurationSeconds float64       `json:"duration_seconds"`
	DurationMs      int64         `json:"duration_ms"`
	Mode            string        `json:"mode"`
	InputArtifacts  []string      `json:"input_artifacts,omitempty"`
	OutputArtifacts []string      `json:"output_artifacts,omitempty"`
	Checks          []CheckResult `json:"checks,omitempty"`
	Warnings        []string      `json:"warnings,omitempty"`
	Errors          []CheckError  `json:"errors,omitempty"`
}

// CheckResult is a single check outcome.
type CheckResult struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// CheckError records a failed check with a message.
type CheckError struct {
	Check   string `json:"check"`
	Message string `json:"message"`
}

// SaveStatus writes status.json to the gate's output directory.
func SaveStatus(dir, gateID string, s *GateStatus) error {
	gateDir := filepath.Join(dir, ".forge", "gates", gateID)
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		return fmt.Errorf("failed to create gate dir: %w", err)
	}
	path := filepath.Join(gateDir, "status.json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write status.json: %w", err)
	}
	return nil
}

// LoadStatus reads status.json from a gate directory.
func LoadStatus(dir, gateID string) (*GateStatus, error) {
	path := filepath.Join(dir, ".forge", "gates", gateID, "status.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("status.json not found for gate %s: %w", gateID, err)
	}
	var s GateStatus
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse status.json: %w", err)
	}
	return &s, nil
}

package taskpipeline

import (
	"testing"
)

func TestInferDesignPhases(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected []DesignPhase
	}{
		{
			name:     "requirement phase - PRD file",
			files:    []string{"docs/prd/feature.md"},
			expected: []DesignPhase{PhaseRequirement},
		},
		{
			name:     "api phase - openapi yaml",
			files:    []string{"api/openapi/spec.yaml"},
			expected: []DesignPhase{PhaseAPI},
		},
		{
			name:     "api phase - proto file",
			files:    []string{"proto/helloworld.proto"},
			expected: []DesignPhase{PhaseAPI},
		},
		{
			name:     "database phase - migration sql",
			files:    []string{"migrations/001_create_users.sql"},
			expected: []DesignPhase{PhaseDatabase},
		},
		{
			name:     "database phase - schema sql",
			files:    []string{"schema.sql"},
			expected: []DesignPhase{PhaseDatabase},
		},
		{
			name:     "frontend phase - tsx component",
			files:    []string{"components/Button.tsx"},
			expected: []DesignPhase{PhaseFrontend},
		},
		{
			name:     "frontend phase - vue file",
			files:    []string{"pages/Login.vue"},
			expected: []DesignPhase{PhaseFrontend},
		},
		{
			name:     "test phase - test file",
			files:    []string{"service_test.go"},
			expected: []DesignPhase{PhaseTest},
		},
		{
			name:     "backend phase - go service",
			files:    []string{"services/user.go"},
			expected: []DesignPhase{PhaseBackend},
		},
		{
			name:     "backend phase - domain file",
			files:    []string{"domain/order.go"},
			expected: []DesignPhase{PhaseBackend},
		},
		{
			name:     "multi-phase - full stack",
			files:    []string{"docs/prd/feature.md", "api/openapi/spec.yaml", "components/App.tsx"},
			expected: []DesignPhase{PhaseRequirement, PhaseAPI, PhaseFrontend},
		},
		{
			name:     "empty - no matching files",
			files:    []string{"README.md", "package.json"},
			expected: nil,
		},
		{
			name:     "empty - no files",
			files:    []string{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferDesignPhases(tt.files)

			// Compare as sets (order-independent)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d phases, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			seen := make(map[DesignPhase]bool)
			for _, p := range result {
				seen[p] = true
			}
			for _, want := range tt.expected {
				if !seen[want] {
					t.Errorf("missing expected phase %s in result %v", want, result)
				}
			}
		})
	}
}

func TestAllDesignPhases(t *testing.T) {
	all := AllDesignPhases()
	if len(all) != 6 {
		t.Errorf("expected 6 design phases, got %d", len(all))
	}
}

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads and parses a pipeline.yml v2 file, then validates the DAG.
func Load(dir string) (*Pipeline, error) {
	path := filepath.Join(dir, ".forge", "pipeline.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("pipeline.yml not found: run 'forge init' first")
		}
		return nil, fmt.Errorf("failed to read pipeline.yml: %w", err)
	}

	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse pipeline.yml: %w", err)
	}

	// Reject non-v2 format
	if p.Version != "2.0" {
		return nil, fmt.Errorf("pipeline.yml version must be \"2.0\", got %q. Run 'forge init' to regenerate", p.Version)
	}

	// Validate structure
	if len(p.PipelineDef.Gates) == 0 {
		return nil, fmt.Errorf("pipeline.yml has no gates defined")
	}

	// Validate DAG (cycle detection, missing deps, duplicate IDs)
	if err := p.ValidateDAG(); err != nil {
		return nil, fmt.Errorf("pipeline.yml validation failed: %w", err)
	}

	return &p, nil
}

// ValidateOnly loads and validates the pipeline without returning it.
// Returns all validation errors found.
func ValidateOnly(dir string) []error {
	path := filepath.Join(dir, ".forge", "pipeline.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return []error{fmt.Errorf("failed to read pipeline.yml: %w", err)}
	}

	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return []error{fmt.Errorf("failed to parse pipeline.yml: %w", err)}
	}

	var errs []error

	if p.Version != "2.0" {
		errs = append(errs, fmt.Errorf("expected version \"2.0\", got %q", p.Version))
	}

	if len(p.PipelineDef.Gates) == 0 {
		errs = append(errs, fmt.Errorf("no gates defined"))
	}

	if err := p.ValidateDAG(); err != nil {
		errs = append(errs, err)
	}

	// Validate all check types are known
	for _, g := range p.PipelineDef.Gates {
		for _, c := range g.Checks {
			if c.Type == "" {
				errs = append(errs, fmt.Errorf("gate '%s': check '%s' has empty type", g.ID, c.Name))
			}
		}
		// Validate referenced hooks exist
		for _, hook := range g.Hooks {
			hookPath := filepath.Join(dir, ".forge", "hooks", hook)
			if _, err := os.Stat(hookPath); os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("gate '%s': hook '%s' not found in .forge/hooks/", g.ID, hook))
			}
		}
	}

	return errs
}

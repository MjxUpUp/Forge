package protocol

import "github.com/MjxUpUp/Forge/internal/scoringtypes"

// Protocol represents the project's quality protocol configuration.
// Stored as .forge/protocol.yml — defines quality standards and session behavior
// that apply to every Claude Code session, regardless of pipeline state.
type Protocol struct {
	Version      string                      `yaml:"version"      json:"version"`
	Standards    []Standard                  `yaml:"standards"    json:"standards"`
	SessionRules []SessionRule               `yaml:"session_rules" json:"session_rules"`
	Scoring      *scoringtypes.ScoringConfig `yaml:"scoring,omitempty" json:"scoring,omitempty"`
}

// Standard is a named quality standard with enforcement configuration.
type Standard struct {
	ID          string `yaml:"id"           json:"id"`
	Name        string `yaml:"name"         json:"name"`
	Description string `yaml:"description"  json:"description"`
	EnforceHook string `yaml:"enforce_hook,omitempty" json:"enforce_hook,omitempty"`
	Severity    string `yaml:"severity"     json:"severity"` // error, warning, info
	Enabled     bool   `yaml:"enabled"      json:"enabled"`
}

// SessionRule is a behavioral rule for the AI agent during every session.
type SessionRule struct {
	ID          string `yaml:"id"          json:"id"`
	Trigger     string `yaml:"trigger"     json:"trigger"` // always, on_edit, on_commit
	Instruction string `yaml:"instruction" json:"instruction"`
	Mandatory   bool   `yaml:"mandatory"   json:"mandatory"`
}

// ErrorSeverityStandards returns standards with severity "error".
func (p *Protocol) ErrorSeverityStandards() []Standard {
	var result []Standard
	for _, s := range p.Standards {
		if s.Enabled && s.Severity == "error" {
			result = append(result, s)
		}
	}
	return result
}

// MandatoryRules returns session rules that are mandatory.
func (p *Protocol) MandatoryRules() []SessionRule {
	var result []SessionRule
	for _, r := range p.SessionRules {
		if r.Mandatory {
			result = append(result, r)
		}
	}
	return result
}

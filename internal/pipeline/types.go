package pipeline

import (
	"github.com/MjxUpUp/Forge/internal/rules"
)

// Pipeline is the top-level pipeline definition from pipeline.yml v2.
type Pipeline struct {
	Version     string      `yaml:"version" json:"version"`
	Project     string      `yaml:"project" json:"project"`
	Mode        string      `yaml:"mode"    json:"mode"` // small, medium, large
	PipelineDef PipelineDef `yaml:"pipeline" json:"pipeline"`

	// topoOrder is cached after ValidateDAG(). Maps gate ID -> execution rank.
	topoOrder map[string]int
}

// PipelineDef wraps the gates array under the "pipeline" key.
type PipelineDef struct {
	Gates []Gate `yaml:"gates" json:"gates"`
}

// Gate is a single quality gate definition.
type Gate struct {
	ID        string   `yaml:"id"         json:"id"`
	Name      string   `yaml:"name"       json:"name"`
	Enabled   bool     `yaml:"enabled"    json:"enabled"`
	DependsOn []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`

	// Prompt is the instruction template sent to the AI subagent.
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`

	// Timeout in seconds for gate execution.
	Timeout int `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Hooks are script names in .forge/hooks/ that run during gate validation.
	Hooks []string `yaml:"hooks,omitempty" json:"hooks,omitempty"`

	// Artifacts declare what goes in and out of this gate.
	Artifacts GateArtifacts `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`

	// Checks are structured quality rules evaluated by the rule engine.
	// Each check has a mandatory Type field. Unknown types cause FAIL.
	Checks []rules.Check `yaml:"checks,omitempty" json:"checks,omitempty"`

	// OnFailure: "abort" or "warn".
	OnFailure string `yaml:"on_failure" json:"on_failure"`

	// AutoPublishFeishu publishes .md outputs to Feishu wiki on pass.
	AutoPublishFeishu bool `yaml:"auto_publish_feishu" json:"auto_publish_feishu"`

	// RequiresHumanApproval pauses before this gate until a human approves.
	RequiresHumanApproval bool `yaml:"requires_human_approval,omitempty" json:"requires_human_approval,omitempty"`
}

// GateArtifacts declares inputs and outputs for a gate.
type GateArtifacts struct {
	Inputs  []string `yaml:"inputs,omitempty"  json:"inputs,omitempty"`
	Outputs []string `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

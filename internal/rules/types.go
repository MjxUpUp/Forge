package rules

// Check is a single structured quality check rule.
// Type is mandatory and must be one of the known rule types.
// Unknown types cause the gate to FAIL (fail-safe behavior).
type Check struct {
	Name   string      `yaml:"name"   json:"name"`
	Type   string      `yaml:"type"   json:"type"`
	Params CheckParams `yaml:"params" json:"params"`
}

// CheckParams is a flexible parameter bag. Each rule type reads
// the specific fields it needs; unknown fields are ignored.
type CheckParams struct {
	// File is the artifact file path, relative to the gate directory.
	File string `yaml:"file,omitempty" json:"file,omitempty"`

	// Keyword to search for (used by file_contains / file_not_contains).
	Keyword string `yaml:"keyword,omitempty" json:"keyword,omitempty"`

	// CaseSensitive keyword matching (default: false).
	CaseSensitive bool `yaml:"case_sensitive,omitempty" json:"case_sensitive,omitempty"`

	// Field is the dot-path to a JSON field (used by json_equals, json_gte, json_lte).
	Field string `yaml:"field,omitempty" json:"field,omitempty"`

	// Value is the expected value for json_equals, or the numeric threshold for json_gte/json_lte.
	Value interface{} `yaml:"value,omitempty" json:"value,omitempty"`

	// MinCount is the minimum array length for json_array_min_count.
	MinCount int `yaml:"min_count,omitempty" json:"min_count,omitempty"`

	// In specifies where to look for the file: "gate_dir" (default) or "project_root".
	In string `yaml:"in,omitempty" json:"in,omitempty"`

	// Script is the hook script name for custom_script checks.
	Script string `yaml:"script,omitempty" json:"script,omitempty"`

	// PassOnMissing governs the missing-file behavior of file_not_contains.
	// Default false (fail-closed): a missing file FAILS the check, because absence
	// of the file cannot verify it does not contain the keyword, and the prior
	// pass-on-missing default let an agent delete the target file to dodge a
	// "must not contain forbidden keyword" gate (D1).
	// Set true ONLY when the rule's intent is "this artifact should not exist"
	// (e.g. a debug.log the build must not produce) — there, missing = desired.
	PassOnMissing bool `yaml:"pass_on_missing,omitempty" json:"pass_on_missing,omitempty"`
}

// Context provides everything a rule evaluator needs.
// No imports from pipeline package — rules never depend on pipeline.
type Context struct {
	// GateDir is the directory for the current gate's artifacts: DataDir/gates/<gate-id>/
	GateDir string

	// GatesDir is the directory holding all gate subdirs (DataDir/gates/).
	// Used by all_gates_passed to scan sibling gate statuses. Decoupled from
	// ProjectRoot so rules need not depend on forgedata (caller fills it).
	GatesDir string

	// ProjectRoot is the root directory of the project.
	ProjectRoot string

	// GateID is the ID of the gate being evaluated.
	GateID string

	// EnabledGateIDs lists all enabled gate IDs in the pipeline.
	// Used by all_gates_passed to detect unexecuted gates.
	EnabledGateIDs []string
}

// Result is the outcome of evaluating a single check.
type Result struct {
	Name    string // matches Check.Name
	Type    string // matches Check.Type
	Passed  bool
	Detail  string // human-readable explanation of what was checked
	Message string // error message (non-empty when Passed=false)
}

// Evaluator is the interface that each rule type implements.
type Evaluator interface {
	Evaluate(ctx Context, params CheckParams) Result
}

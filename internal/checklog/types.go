package checklog

import "time"

// CheckName identifies a specific hook check.
type CheckName string

const (
	CheckAutoCompile CheckName = "auto-compile"
	CheckAssertion   CheckName = "assertion-check"
	CheckExperience  CheckName = "experience-check"
	CheckTaskVerify  CheckName = "task-verify"
	CheckTaskGuard   CheckName = "task-guard"
)

// Entry records the outcome of a single hook execution.
type Entry struct {
	Check      CheckName `json:"check"`
	Passed     bool      `json:"passed"`
	Checked    bool      `json:"checked"`           // false if check was skipped
	ToolName   string    `json:"tool_name"`         // from Claude Code stdin
	TaskRef    string    `json:"task_ref,omitempty"` // task this check belongs to
	Detail     string    `json:"detail"`            // human-readable summary
	RecordedAt time.Time `json:"recorded_at"`
}

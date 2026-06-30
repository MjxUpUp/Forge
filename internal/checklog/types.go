package checklog

import "time"

// CheckName identifies a specific hook check.
type CheckName string

const (
	CheckAutoCompile CheckName = "auto-compile"
	CheckAssertion   CheckName = "assertion-check"
	CheckTaskVerify  CheckName = "task-verify"
	CheckTaskGuard    CheckName = "task-guard"
	CheckBashGuard    CheckName = "bash-guard"
	CheckFileSentinel CheckName = "file-sentinel"
	// CheckEscapeHatch records use of a gate-bypass escape hatch
	// (FORGE_TEST_COVERAGE / FORGE_WORK_ACTIVITY / FORGE_SKIP_VERIFY). These
	// hatches are legitimate tools, but their use must be AUDITED, not silent —
	// an agent dodging the test-coverage gate by exporting FORGE_TEST_COVERAGE=
	// disable should leave a visible trail. A4: recorded so `forge trace` and
	// scoring can surface hatch usage. Passed=true (the bypass took effect),
	// Checked=true, Detail names the hatch.
	CheckEscapeHatch CheckName = "escape-hatch"
)

// Entry records the outcome of a single hook execution.
type Entry struct {
	Check      CheckName `json:"check"`
	Passed     bool      `json:"passed"`
	Checked    bool      `json:"checked"`            // false if check was skipped
	ToolName   string    `json:"tool_name"`          // from Claude Code stdin
	TaskRef    string    `json:"task_ref,omitempty"` // task this check belongs to
	SessionID  string    `json:"session_id,omitempty"` // Claude Code session — isolates concurrent sessions
	Detail     string    `json:"detail"`             // human-readable summary
	RecordedAt time.Time `json:"recorded_at"`
}

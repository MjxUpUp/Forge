package taskpipeline

import (
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// TaskGate defines a lightweight task-level quality gate.
type TaskGate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Auto        bool   `json:"auto"` // true = checked automatically by hooks
}

// AcceptanceCriterion 是一条可执行的验收标准（来自 dev-workflow Plan 的
// "Run: <cmd>, Expected: <output>"）。持久化进 TaskState，使验收标准不随 plan 文本
// 消失；verify-acceptance 实跑 Run、比对 Expected，记 deterministic 证据——把 spec
// 变成不可伪造的验证，对冲 agent 自述"满足验收"的盲区。
type AcceptanceCriterion struct {
	Run      string `json:"run"`                // 实跑的命令（如 "go test ./..."）
	Expected string `json:"expected,omitempty"` // 期望输出的子串；空=只看退出码 0
	Passed   bool   `json:"passed,omitempty"`   // 上次 verify-acceptance 的结果
	Output   string `json:"output,omitempty"`   // 上次实跑的输出（截断），供排查
}

// TaskState tracks the state of a single task's pipeline.
// Stored in .forge/tasks/{sanitized-ref}.json.
type TaskState struct {
	TaskRef      string                    `json:"task_ref"`
	Branch       string                    `json:"branch"`
	Source       string                    `json:"source"` // "explicit", "branch"
	Summary      string                    `json:"summary"`
	CurrentGate  string                    `json:"current_gate"`
	History      []TaskGateResult          `json:"history"`
	StartedAt    time.Time                 `json:"started_at"`
	CompletedAt  *time.Time                `json:"completed_at,omitempty"`
	Score        *scoringtypes.ScoreResult `json:"score,omitempty"`
	HeadCommit   string                    `json:"head_commit,omitempty"`   // for duplicate detection
	SessionID    string                    `json:"session_id,omitempty"`    // agent session that created this task
	ReviewPassed bool                      `json:"review_passed,omitempty"` // code-review-gate 通过标记；task-complete 门禁的硬前置
	Acceptance   []AcceptanceCriterion     `json:"acceptance,omitempty"`    // 验收标准（dev-workflow Plan 的 Run+Expected），verify-acceptance 实跑回扣
	// PlanScope 是任务开工前声明的"计划改动文件"白名单（glob，repo-relative 正斜杠路径）。
	// 对应 Terraform desired state / Copilot Workspace plan 的"打算改哪些文件"——把规划前置
	// 变成可度量契约。advisory：实改文件（TaskChangedFiles）与之的差集记 scope-drift 供 review，
	// 不阻塞。学术界变更影响分析召回率仅 ~44%（PASTE），故 scope 当 prediction 而非 contract，
	// drift 是常态信号。task start --scope 声明，task scope add 中途迭代追加（Agentless 分层定位）。
	PlanScope []string `json:"plan_scope,omitempty"`
}

// TaskGateResult records the outcome of a single task gate.
type TaskGateResult struct {
	Gate        string    `json:"gate"`
	Passed      bool      `json:"passed"`
	CompletedAt time.Time `json:"completed_at"`
	HeadCommit  string    `json:"head_commit,omitempty"` // git HEAD at gate pass time
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

// MarkReviewPassed records that code-review-gate has been run and passed for this
// task. It is the hard prerequisite the task-complete gate enforces (see
// executor.go)——确保提交前子 agent 审查真的跑过，而非 agent 自称完成。
func (s *TaskState) MarkReviewPassed() {
	s.ReviewPassed = true
}

// RecordGateResult adds a gate result and advances CurrentGate.
// If the gate was already passed, this is a no-op (prevents duplicate history
// entries from stop hook re-verification). A previously failed gate can be
// retried and will add a new entry.
func (s *TaskState) RecordGateResult(gateID string, passed bool, headCommit string) {
	// Skip if this gate was already passed — prevents 25x duplicate entries
	// from stop hook repeatedly verifying the same gate.
	if passed && s.gatePassed(gateID) {
		return
	}

	s.History = append(s.History, TaskGateResult{
		Gate:        gateID,
		Passed:      passed,
		CompletedAt: time.Now(),
		HeadCommit:  headCommit,
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

// HasAcceptance reports whether the task has any persisted acceptance criteria.
func (s *TaskState) HasAcceptance() bool {
	return len(s.Acceptance) > 0
}

// AllAcceptancePassed reports whether every acceptance criterion has Passed=true.
// Empty acceptance returns true (nothing to reconcile). task-verify 据此决定是否提醒回扣。
func (s *TaskState) AllAcceptancePassed() bool {
	for i := range s.Acceptance {
		if !s.Acceptance[i].Passed {
			return false
		}
	}
	return true
}

package checklog

import "time"

// CheckName identifies a specific hook check.
type CheckName string

const (
	CheckAutoCompile  CheckName = "auto-compile"
	CheckAssertion    CheckName = "assertion-check"
	CheckTaskVerify   CheckName = "task-verify"
	CheckTaskComplete CheckName = "task-complete"
	CheckTaskGuard    CheckName = "task-guard"
	CheckBashGuard    CheckName = "bash-guard"
	CheckFileSentinel CheckName = "file-sentinel"
	// CheckScopeDrift 记录 advisory scope 偏差：agent 改了未在 PlanScope 声明的源码文件。
	// 对应 Terraform drift detection（声明态 vs 实际态的差集）。deterministic（hook 实算
	// MatchesScope/ScopeDrift，agent 无法伪造）。Passed 语义：无偏差=true，有偏差=false——
	// 但永远 Checked=true 且绝不阻断工具调用（advisory）。变更影响分析召回率仅 ~44%，
	// scope 是 prediction 非 contract，偏差是常态信号；本记录供 review/看板度量，不作门禁。
	CheckScopeDrift CheckName = "scope-drift"
	// CheckEscapeHatch records use of a gate-bypass escape hatch
	// (FORGE_TEST_COVERAGE / FORGE_WORK_ACTIVITY / FORGE_SKIP_VERIFY). These
	// hatches are legitimate tools, but their use must be AUDITED, not silent —
	// an agent dodging the test-coverage gate by exporting FORGE_TEST_COVERAGE=
	// disable should leave a visible trail. A4: recorded so `forge trace` and
	// scoring can surface hatch usage. Passed=true (the bypass took effect),
	// Checked=true, Detail names the hatch.
	CheckEscapeHatch CheckName = "escape-hatch"
)

// EvidenceSource 标注一条 checklog 证据的来源，区分 deterministic（hook/外部
// 工具实跑或 gate 代码判定，不可被 agent 伪造）与 agent-claim（agent 自述的
// 验证）。
//
// 用途：review 子 agent 和评分据此对冲 LLM-judge 盲区——业界反复证实（Tenure
// "0.85 vs 0.000" 案例）LLM judge 看不出"agent 跳过前置就声明完成"的最严重
// 失败模式；只有 deterministic 证据能照出。EvidenceChain 按 Source 分桶，
// review 时优先采信 deterministic，agent-claim 仅作初筛信号。
type EvidenceSource string

const (
	// EvidenceDeterministic: hook/gate 代码实跑或判定产生（auto-compile、
	// assertion-check、file-sentinel、test-coverage-gate 等）。agent 无法伪造。
	EvidenceDeterministic EvidenceSource = "deterministic"
	// EvidenceAgentClaim: agent 自述的验证（如"我跑过端到端测试了"但未由 hook
	// 确认）。可信度低于 deterministic，评分/review 应区别对待。
	EvidenceAgentClaim EvidenceSource = "agent-claim"
)

// SourceForCheck 返回一个 CheckName 的默认证据来源。hook/gate 代码实跑的检查
// （auto-compile、assertion-check、file-sentinel、test-coverage 等）默认 deterministic；
// task-verify / task-complete gate 的"推进"记录是 agent 的声明（agent 自述验证/完成），
// 归 agent-claim——对冲 LLM-judge 看不出"agent 跳过前置就声明完成"的盲区。
// 调用方显式设置 Entry.Source 时优先于本默认值。
func SourceForCheck(c CheckName) EvidenceSource {
	if c == CheckTaskVerify || c == CheckTaskComplete {
		return EvidenceAgentClaim
	}
	return EvidenceDeterministic
}

// Entry records the outcome of a single hook execution.
type Entry struct {
	Check     CheckName `json:"check"`
	Passed    bool      `json:"passed"`
	Checked   bool      `json:"checked"`              // false if check was skipped
	ToolName  string    `json:"tool_name"`            // from Claude Code stdin
	TaskRef   string    `json:"task_ref,omitempty"`   // task this check belongs to
	SessionID string    `json:"session_id,omitempty"` // Claude Code session — isolates concurrent sessions
	Detail    string    `json:"detail"`               // human-readable summary
	// Source 标注证据来源（deterministic vs agent-claim）。Record 时若留空，
	// 按 SourceForCheck 兜底推断，故历史记录点无需逐个改造也能进证据链分桶。
	Source     EvidenceSource `json:"source,omitempty"`
	RecordedAt time.Time      `json:"recorded_at"`
}

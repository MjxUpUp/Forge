package checklog

import "time"

// CheckName identifies a specific hook check.
//
// CheckName 标识一次具体的 hook 检查。
type CheckName string

const (
	CheckAutoCompile  CheckName = "auto-compile"
	CheckAssertion    CheckName = "assertion-check"
	CheckTaskVerify   CheckName = "task-verify"
	CheckTaskComplete CheckName = "task-complete"
	CheckTaskGuard    CheckName = "task-guard"
	CheckBashGuard    CheckName = "bash-guard"
	CheckFileSentinel CheckName = "file-sentinel"
	// CheckScopeDrift records advisory scope drift: an agent modified source files not declared in PlanScope.
	// Mirrors Terraform drift detection (diff between declared and actual state). deterministic (the hook computes
	// MatchesScope/ScopeDrift, agent cannot forge). Passed semantics: no drift=true, drift=false—
	// but always Checked=true and never blocks tool calls (advisory). Change-impact recall is only ~44%,
	// scope is a prediction not a contract, drift is a normal signal; this record feeds review/dashboard metrics, not gates.
	//
	// CheckScopeDrift 记录 advisory scope 偏差：agent 改了未在 PlanScope 声明的源码文件。
	// 对应 Terraform drift detection（声明态 vs 实际态的差集）。deterministic（hook 实算
	// MatchesScope/ScopeDrift，agent 无法伪造）。Passed 语义：无偏差=true，有偏差=false——
	// 但永远 Checked=true 且绝不阻断工具调用（advisory）。变更影响分析召回率仅 ~44%，
	// scope 是 prediction 非 contract，偏差是常态信号；本记录供 review/看板度量，不作门禁。
	CheckScopeDrift CheckName = "scope-drift"
	// CheckCheatScan records advisory AI cheat-pattern scan results: at task-verify, mechanically detects new-line hits across 5 categories
	// (type-suppression/error-swallow/dead-branch/comment-only-fix/comment-as-debt).
	// comment-as-debt catches comments that flag an issue without fixing it (ladder-of-laziness level 0, the root of code rot).
	// deterministic (the gate computes ScanCheatPatterns, agent cannot forge). Passed semantics: no hit
	// =true, hit=false—but always Checked=true and never blocks (advisory; heuristics may false-positive,
	// the trail is left for review inspection). This record lifts mechanically-detectable cheats from per-round LLM-review resampling
	// into a one-shot deterministic verdict—countering the root cause of each review round surfacing new issues.
	//
	// CheckCheatScan 记录 advisory AI 作弊模式扫描结果：task-verify 时机械检测 5 类
	// （type-suppression/error-swallow/dead-branch/comment-only-fix/comment-as-debt）的新增行命中。
	// comment-as-debt 抓"注释标识问题但不解决"（懒惰阶梯反第 0 级，屎山根源）。
	// deterministic（gate 实算 ScanCheatPatterns，agent 无法伪造）。Passed 语义：无命中
	// =true，有命中=false——但永远 Checked=true 且绝不阻断（advisory；启发式有假阳性
	// 可能，留痕供 review 核查）。本记录把"机械可检的作弊"从 LLM-review 每轮重采样
	// 抽到一次性 deterministic 判决——对冲"每轮 review 冒新问题"的根因。
	CheckCheatScan CheckName = "cheat-scan"
	// CheckEscapeHatch records usage of gate-bypass escape hatches (FORGE_TEST_COVERAGE /
	// FORGE_WORK_ACTIVITY / FORGE_SKIP_VERIFY). These escape hatches are legitimate tools, but their use must
	// leave an auditable trail and cannot be silent—when an agent bypasses the
	// test-coverage gate via export FORGE_TEST_COVERAGE=disable, a visible trail should be left. A4: recorded so forge trace and scoring can surface
	// escape-hatch usage. Passed=true (bypass effective), Checked=true, Detail marks the escape-hatch name.
	//
	// CheckEscapeHatch 记录 gate-bypass 逃生舱的使用（FORGE_TEST_COVERAGE /
	// FORGE_WORK_ACTIVITY / FORGE_SKIP_VERIFY）。这些逃生舱是合法工具，但其使用必须
	// 留痕可审计、不能静默——agent 通过 export FORGE_TEST_COVERAGE=disable 绕过
	// test-coverage gate 时，应留下可见轨迹。A4：记录以便 forge trace 与评分能展示
	// 逃生舱使用。Passed=true（bypass 已生效）、Checked=true、Detail 标注逃生舱名。
	CheckEscapeHatch CheckName = "escape-hatch"
)

// EvidenceSource marks the source of a checklog evidence entry, distinguishing deterministic (hook/external
// tool actually run or gate code verdict, cannot be forged by agent) from agent-claim (agent self-reported
// verification).
//
// Purpose: review sub-agents and scoring use this to counter LLM-judge blind spots—the industry has repeatedly confirmed (the Tenure
// `0.85 vs 0.000` case) that LLM judges cannot see the most severe failure mode of `agent skips prerequisites then claims completion`;
// only deterministic evidence exposes it. EvidenceChain buckets by Source,
// review trusts deterministic first; agent-claim is only an initial-filter signal.
//
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
	// EvidenceDeterministic: produced by hook/gate code actually running or verdicting (auto-compile,
	// assertion-check, file-sentinel, test-coverage-gate, etc.). Agent cannot forge.
	//
	// EvidenceDeterministic: hook/gate 代码实跑或判定产生（auto-compile、
	// assertion-check、file-sentinel、test-coverage-gate 等）。agent 无法伪造。
	EvidenceDeterministic EvidenceSource = "deterministic"
	// EvidenceAgentClaim: agent self-reported verification (e.g. `I ran the end-to-end tests` but not
	// confirmed by a hook). Lower confidence than deterministic; scoring/review should treat differently.
	//
	// EvidenceAgentClaim: agent 自述的验证（如"我跑过端到端测试了"但未由 hook
	// 确认）。可信度低于 deterministic，评分/review 应区别对待。
	EvidenceAgentClaim EvidenceSource = "agent-claim"
)

// SourceForCheck returns the default evidence source for a CheckName. Checks actually run by hook/gate code
// (auto-compile, assertion-check, file-sentinel, test-coverage, etc.) default to deterministic;
// the `advance` records of task-verify / task-complete gates are agent claims (agent self-reports verification/completion),
// classified as agent-claim—countering the LLM-judge blind spot of `agent skips prerequisites then claims completion`.
// Caller's explicit Entry.Source takes precedence over this default.
//
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

// Entry records the result of a single hook execution.
//
// Entry 记录一次 hook 执行的结果。
type Entry struct {
	Check     CheckName `json:"check"`
	Passed    bool      `json:"passed"`
	Checked   bool      `json:"checked"`              // check 被跳过时为 false
	ToolName  string    `json:"tool_name"`            // 来自 Claude Code stdin
	TaskRef   string    `json:"task_ref,omitempty"`   // 该 check 所属的 task
	SessionID string    `json:"session_id,omitempty"` // Claude Code session——隔离并发 session
	Detail    string    `json:"detail"`               // 人类可读的摘要
	// Source marks the evidence source (deterministic vs agent-claim). If left empty at Record time,
	// SourceForCheck is used as fallback, so historical record sites need no per-site retrofit to enter evidence-chain bucketing.
	//
	// Source 标注证据来源（deterministic vs agent-claim）。Record 时若留空，
	// 按 SourceForCheck 兜底推断，故历史记录点无需逐个改造也能进证据链分桶。
	Source     EvidenceSource `json:"source,omitempty"`
	RecordedAt time.Time      `json:"recorded_at"`
}

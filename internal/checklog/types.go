package checklog

import (
	"strings"
	"time"
)

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
	// CheckUnusedScan records advisory unreferenced-export scan results: at task-verify, mechanically
	// detects newly-added exported symbols (Go func/type/method, TS export, Rust pub) that no
	// production line in this task references — suspected "implemented but never wired" (layer-1
	// wiring detection). Unit tests verify the implementation, not the wiring; a broken wire leaves
	// tests green and the feature dead (Forge's own BUG-1: inferDesignPhases had zero production
	// callers). deterministic (the gate computes ScanUnusedSymbols, agent cannot forge). Passed
	// semantics: no unreferenced export=true, hit=false—but always Checked=true and never blocks
	// (advisory; library/reflection/external-consumer exports legitimately have no in-repo caller,
	// the trail is left for review). Layer-2 (referenced but semantically unwired) is not mechanically
	// decidable → stays with the LLM reviewer / code-review-gate.
	//
	// CheckUnusedScan 记录 advisory 未引用导出符号扫描结果：task-verify 时机械检测本次新增
	// 的导出符号（Go func/type/method、TS export、Rust pub）在本任务生产代码里零引用——疑似
	// "实现了但没接线"（层 1 接线检测）。单测验实现不验接线；接线一断测试照绿、功能已死
	// （Forge 自己的 BUG-1：inferDesignPhases 零生产调用方）。deterministic（gate 实算
	// ScanUnusedSymbols，agent 无法伪造）。Passed 语义：无未引用导出=true，命中=false——但
	// 永远 Checked=true 且绝不阻塞（advisory；库/反射/外部消费的导出合法地无仓内调用方，
	// 留痕供 review 核查）。层 2（引用了但语义没接通）机械不可判 → 仍归 LLM reviewer /
	// code-review-gate。
	CheckUnusedScan CheckName = "unused-scan"
	// CheckEscapeHatch records usage of gate-bypass escape hatches (FORGE_TEST_COVERAGE /
	// FORGE_WORK_ACTIVITY / FORGE_RECURRENT_HARDEN). These escape hatches are legitimate tools, but their use must
	// leave an auditable trail and cannot be silent—when an agent bypasses the
	// test-coverage gate via export FORGE_TEST_COVERAGE=disable, a visible trail should be left. A4: recorded so forge trace and scoring can surface
	// escape-hatch usage. Passed=true (bypass effective), Checked=true, Detail marks the escape-hatch name.
	//
	// CheckEscapeHatch 记录 gate-bypass 逃生舱的使用（FORGE_TEST_COVERAGE /
	// FORGE_WORK_ACTIVITY / FORGE_RECURRENT_HARDEN）。这些逃生舱是合法工具，但其使用必须
	// 留痕可审计、不能静默——agent 通过 export FORGE_TEST_COVERAGE=disable 绕过
	// test-coverage gate 时，应留下可见轨迹。A4：记录以便 forge trace 与评分能展示
	// 逃生舱使用。Passed=true（bypass 已生效）、Checked=true、Detail 标注逃生舱名。
	CheckEscapeHatch CheckName = "escape-hatch"
	// CheckSkillTrigger records a canonical skill that the skill-trigger framework fired (passive injection via
	// AdditionalContext) — making skill reach observable downstream. Without it, skill-trigger injected silently and
	// `forge skills usage`/`effectiveness` could not answer "which canonical skills actually fired" (the dogfood
	// 0-trigger blind spot). deterministic (the engine evaluates declared triggers, agent cannot forge). Passed=true,
	// Checked=true, Detail marks the skill name + firing reason. Excluded from evidence strength (it is an observation
	// of a skill firing, not verification evidence) — see BuildEvidenceChain.
	//
	// CheckSkillTrigger 记录 skill-trigger 框架触发（经 AdditionalContext 被动注入）的 canonical skill——
	// 让 skill 触达在下游可观测。无此记录，skill-trigger 静默注入，`forge skills usage`/`effectiveness`
	// 无法回答"哪些 canonical skill 真触发过"（dogfood 0 触发盲区）。deterministic（引擎实算声明式触发，
	// agent 无法伪造）。Passed=true、Checked=true、Detail 标 skill 名 + 触发原因。不计入证据强度
	// （它是 skill 触发的观测，非验证证据）——见 BuildEvidenceChain。
	CheckSkillTrigger CheckName = "skill-trigger"
	// CheckKimiPluginStale records that the kimi-installed forge plugin lags behind the
	// running forge binary (tag-locked install, no auto-update). Passed=true with
	// Level=LevelWarn (escape-hatch pattern: Passed stays neutral so evidence aggregation
	// is unaffected; the warn signal rides Level), Checked=true, Detail carries the
	// remediation advisory. Recorded once per day at most (kimiStaleMarker throttle) when
	// the advisory actually fires on the resume-reinject (UserPromptSubmit) channel.
	// Exists because the drift was triple-invisible in production (2026-08-15 audit):
	// kimi drops SessionStart stdout (the advisory's old ride), the noise gate drops the
	// hook's PASS, and model/user/logs all stayed silent while the plugin drifted two
	// releases behind. Without this entry `forge trace`/dashboard cannot see plugin drift.
	//
	// CheckKimiPluginStale 记录 kimi 已装 forge plugin 落后于运行中的 forge 二进制
	// （tag 锁定安装、无自动更新）。Passed=true 且 Level=LevelWarn（escape-hatch 模式：
	// Passed 保持中性不影响证据聚合，warn 信号走 Level），Checked=true，Detail 带修复
	// 提示。仅在 advisory 真正于 resume-reinject（UserPromptSubmit）通道触发时记录，
	// 每日至多一条（kimiStaleMarker 节流）。存在理由：该漂移在生产曾三重不可见
	// （2026-08-15 审计）——kimi 丢 SessionStart stdout（advisory 旧通道）、noise gate
	// 丢该 hook 的 PASS、plugin 落后两个 release 期间模型/用户/日志全静默。无此条目，
	// `forge trace`/看板看不到 plugin 漂移。
	CheckKimiPluginStale CheckName = "kimi-plugin-stale"
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

// Level classifies a checklog entry's severity in one structured field, so
// consumers (dashboard/trace/review) no longer parse the Detail prose prefixes
// (BLOCKED: / ADVISORY:) to tell a hard block from a soft signal. The text
// prefixes stay — task-verify's hook greps -F 'ADVISORY:' as a cross-process
// contract — Level is additive metadata, not a replacement.
//
// Level 用一个结构化字段标注 checklog 条目的级别，消费方
// （dashboard/trace/review）不必再解析 Detail 散文前缀（BLOCKED: / ADVISORY:）
// 来区分硬阻断与软信号。文本前缀保留——task-verify 的 hook 用 grep -F
// 'ADVISORY:' 是跨进程契约——Level 是增量元数据，非替代。
type Level string

const (
	// LevelPass: the check ran and passed.
	//
	// LevelPass：检查实跑且通过。
	LevelPass Level = "pass"
	// LevelFail: the check ran and failed (hard signal, gate-relevant).
	//
	// LevelFail：检查实跑且失败（硬信号，门禁相关）。
	LevelFail Level = "fail"
	// LevelWarn: a noteworthy but tolerated condition (e.g. escape-hatch usage,
	// infrastructure degraded but fail-open).
	//
	// LevelWarn：值得注意但被容忍的状况（如逃生舱使用、基建降级但 fail-open）。
	LevelWarn Level = "warn"
	// LevelBlocked: a hard block (gate BLOCKED: verdict / hook blocked the tool call).
	//
	// LevelBlocked：硬阻断（gate 的 BLOCKED: 裁定 / hook 拦截了工具调用）。
	LevelBlocked Level = "blocked"
	// LevelAdvisory: a soft, non-blocking signal (gate ADVISORY: verdict).
	//
	// LevelAdvisory：软性不阻塞信号（gate 的 ADVISORY: 裁定）。
	LevelAdvisory Level = "advisory"
)

// Detail prefixes mirrored from taskpipeline/gate_message.go (blockedPrefix /
// advisoryPrefix). Duplicated as literals because checklog is a leaf package —
// importing taskpipeline would create a cycle (taskpipeline imports checklog).
// The derivation is a best-effort fallback for entries whose caller left Level
// empty; explicit Level always wins.
const (
	blockedDetailPrefix  = "BLOCKED: "
	advisoryDetailPrefix = "ADVISORY: "
)

// DeriveLevel infers the Level of an entry from Passed + Detail prefixes when
// the caller did not set one explicitly. Mirrors the Source fallback pattern
// (SourceForCheck): legacy recording points and old archived lines (written
// before the field existed) still classify correctly with no per-site retrofit.
//
// DeriveLevel 在调用方未显式设置时，从 Passed + Detail 前缀推导条目的 Level。
// 与 Source 兜底模式（SourceForCheck）同款：历史记录点与旧归档行（字段引入前
// 写入）无需逐点改造也能正确分级。显式 Level 恒优先。
func DeriveLevel(e *Entry) Level {
	if e == nil {
		return ""
	}
	if strings.HasPrefix(e.Detail, blockedDetailPrefix) {
		return LevelBlocked
	}
	if strings.HasPrefix(e.Detail, advisoryDetailPrefix) {
		return LevelAdvisory
	}
	if e.Passed {
		return LevelPass
	}
	return LevelFail
}

// EffectiveLevel returns the entry's Level, deriving it from Passed + Detail
// when the field is empty (old archived lines have no level — history is not
// rewritten; the fallback is applied at read time).
//
// EffectiveLevel 返回条目的 Level；字段为空时（旧归档行无 level——不改写
// 历史，读取时兜底）从 Passed + Detail 推导。
func (e *Entry) EffectiveLevel() Level {
	if e.Level != "" {
		return e.Level
	}
	return DeriveLevel(e)
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
	// Level is the structured severity (pass/fail/warn/blocked/advisory). If left
	// empty at Record time, DeriveLevel fills it from Passed + Detail prefixes;
	// readers use EffectiveLevel for the same fallback on old lines.
	//
	// Level 是结构化级别（pass/fail/warn/blocked/advisory）。Record 时若留空，
	// 由 DeriveLevel 从 Passed + Detail 前缀兜底推导；读取侧用 EffectiveLevel
	// 对旧行做同样的兜底。
	Level Level `json:"level,omitempty"`
	// Source marks the evidence source (deterministic vs agent-claim). If left empty at Record time,
	// SourceForCheck is used as fallback, so historical record sites need no per-site retrofit to enter evidence-chain bucketing.
	//
	// Source 标注证据来源（deterministic vs agent-claim）。Record 时若留空，
	// 按 SourceForCheck 兜底推断，故历史记录点无需逐个改造也能进证据链分桶。
	Source     EvidenceSource `json:"source,omitempty"`
	RecordedAt time.Time      `json:"recorded_at"`
}

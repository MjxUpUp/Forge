package checklog

import (
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/nodestamp"
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
	// CheckCheatScan records advisory AI cheat-pattern scan results: at task-verify, mechanically detects new-line hits across 7 categories
	// (type-suppression/error-swallow/dead-branch/comment-only-fix/comment-as-debt/phantom-import/path-assumption).
	// comment-as-debt catches comments that flag an issue without fixing it (ladder-of-laziness level 0, the root of code rot);
	// phantom-import catches relative imports that resolve to no file on disk (the mechanical subset of mock-of-hallucination);
	// path-assumption catches the OS path separator used as a content matcher (the cross-platform breakage fingerprint).
	// deterministic (the gate computes ScanCheatPatterns, agent cannot forge). Passed semantics: no hit
	// =true, hit=false—but always Checked=true and never blocks (advisory; heuristics may false-positive,
	// the trail is left for review inspection). This record lifts mechanically-detectable cheats from per-round LLM-review resampling
	// into a one-shot deterministic verdict—countering the root cause of each review round surfacing new issues.
	//
	// CheckCheatScan 记录 advisory AI 作弊模式扫描结果：task-verify 时机械检测 7 类
	// （type-suppression/error-swallow/dead-branch/comment-only-fix/comment-as-debt/phantom-import/path-assumption）的新增行命中。
	// comment-as-debt 抓"注释标识问题但不解决"（懒惰阶梯反第 0 级，屎山根源）；
	// phantom-import 抓解析不到磁盘文件的相对 import（mock-of-hallucination 的机械子集）；
	// path-assumption 抓把 OS 路径分隔符当内容匹配器的写法（跨平台崩溃指纹）。
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
	// CheckReviewPass records one `forge review pass` event (task mode): the reviewed snapshot
	// (HEAD + change hash) plus the round number. deterministic by default (SourceForCheck:
	// recorded by the CLI command with a gate-computed hash, agent cannot forge the hash).
	// It is an OBSERVATION (a marker that review was claimed-and-stamped), not verification
	// evidence — excluded from evidence-strength bucketing like cheat-scan (see BuildEvidenceChain).
	// Its value is making the review-rework loop measurable (round count per task).
	//
	// CheckReviewPass 记录一次 `forge review pass` 事件（task 模式）：审过的快照
	// （HEAD + 变更 hash）与第几轮。默认 deterministic（SourceForCheck：由 CLI 命令
	// 以 gate 实算的 hash 落盘，agent 无法伪造 hash）。它是 OBSERVATION（"审查已被
	// 声明并打戳"的标记），不是验证证据——与 cheat-scan 同类的排除出证据强度分桶
	// （见 BuildEvidenceChain）。价值在于让审查-返工循环可度量（每任务轮次数）。
	CheckReviewPass CheckName = "review-pass"
	// CheckPlanFirst records the advisory that a code task reached task-implement with neither
	// Plan nor Goal recorded (no --plan-file/--goal at task start). Plan-first lowers review
	// rework (shift-left: catching direction errors at proposal stage is cheaper than at diff
	// stage), so the gate leaves a soft trail. Passed semantics: plan/goal present=true,
	// absent=false — always Checked=true, never blocks (advisory). Observation class, excluded
	// from evidence-strength bucketing (see BuildEvidenceChain).
	//
	// CheckPlanFirst 记录 advisory：代码任务到达 task-implement 时 Plan/Goal 均未记录
	// （task start 时没带 --plan-file/--goal）。方案先行能降低审查返工（shift-left：
	// 方向错误在方案阶段拦比 diff 阶段拦便宜），故门禁留软痕。Passed 语义：有
	// 方案/目标=true，无=false——永远 Checked=true 且绝不阻断（advisory）。属
	// observation 类，排除出证据强度分桶（见 BuildEvidenceChain）。
	CheckPlanFirst CheckName = "plan-first"
	// CheckToolFailure records one PostToolUseFailure observation (2026-08-22 failure-track
	// hook, #4-A): a Bash/PowerShell command failed and the host reported the error text.
	// deterministic (host-stdin-sourced, agent cannot forge) but an OBSERVATION, not
	// verification — a tool failing says nothing about whether the task's own gates ran,
	// so it must not feed evidence strength (excluded in BuildEvidenceChain). Its value is
	// making the compile/test failure loop observable: before this, a failed `go build`
	// inside a Bash tool left zero forge-side trace, and compile-fix-loop skill reach
	// could not be correlated with actual failures.
	//
	// CheckToolFailure 记录一次 PostToolUseFailure 观察（2026-08-22 failure-track
	// hook，#4-A）：Bash/PowerShell 命令失败、宿主上报了错误文本。deterministic
	// （宿主 stdin 来源，agent 无法伪造）但是 OBSERVATION 而非验证——工具失败
	// 不代表任务自身的门禁跑没跑，故不得喂给 evidence strength（BuildEvidenceChain
	// 排除）。价值在于让编译/测试失败循环可观测：此前 Bash 工具里失败的 `go build`
	// 在 forge 侧零痕迹，compile-fix-loop skill 的触达无法与真实失败关联。
	CheckToolFailure CheckName = "tool-failure"
	// CheckSubagentStop records one SubagentStop observation (2026-08-22 subagent-track
	// hook, #4-A): a sub-agent finished, carrying agent_id/agent_type and a delivery
	// summary. v1 is observe-only (no blocking — an empty-delivery block has more false
	// positives than value). deterministic (host-stdin-sourced) but an OBSERVATION —
	// excluded from evidence strength like the others. Its value is closing the
	// attribution gap: sessions.jsonl missed agent_type for ~53% of sessions (2026-08
	// attribution audit) because sub-agent activity had no forge-side record at all.
	//
	// CheckSubagentStop 记录一次 SubagentStop 观察（2026-08-22 subagent-track
	// hook，#4-A）：子 agent 结束，携带 agent_id/agent_type 与交付摘要。v1 仅观察
	// 不阻断（空交付阻断的假阳性大于收益）。deterministic（宿主 stdin 来源）但属
	// OBSERVATION——与其他条目一样排除出 evidence strength。价值在补归因缺口：
	// 子 agent 活动此前在 forge 侧零记录，sessions.jsonl 约 53% 会话缺 agent_type
	// （2026-08 归因审计）。
	CheckSubagentStop CheckName = "subagent-stop"
	// CheckTestNudge records one mid-task test reminder fired by the test-nudge hook
	// (2026-08-22, #4-E): the session counter saw >=3 non-test source writes with zero
	// paired test writes since the last reset. It is the in-flight companion of the
	// task-verify test-coverage gate (which fires only at verify time, often hours
	// after the code was written); the nudge catches the drift while the agent can
	// still fix it cheaply. deterministic (counter is hook-side, agent cannot forge)
	// but an OBSERVATION of process drift, not any verification — excluded from
	// evidence strength like the others.
	//
	// CheckTestNudge 记录 test-nudge hook 发出的一次事中测试提醒（2026-08-22，
	// #4-E）：会话计数器看到自上次重置以来 >=3 次非测试源码写入且 0 次配对测试
	// 写入。它是 task-verify test-coverage 门禁的事中伴随（门禁只在 verify 时刻
	// 触发，往往在代码写完数小时后）；nudge 在 agent 还能便宜修复的时机抓住漂移。
	// deterministic（计数器在 hook 侧，agent 无法伪造）但属过程漂移的 OBSERVATION
	// 而非任何验证——与其他条目一样排除出 evidence strength。
	CheckTestNudge CheckName = "test-nudge"
	// CheckBundleVerify records one bundle-signature verification verdict at import
	// time (node-identity.md §3) — the trust decision that previously reached only the
	// importing terminal's stdout/stderr. deterministic (the verdict is computed by CLI
	// code against the trust store; the agent cannot forge it) but an OBSERVATION of
	// the trust surface, excluded from evidence-strength bucketing like the other
	// observation-class checks. The verdict string rides Meta[MetaKeyVerdict] and the
	// signer's node_id Meta[MetaKeySigner], so readers never parse Detail prose.
	//
	// CheckBundleVerify 记录导入时的一次 bundle 验签判定（node-identity.md §3）——
	// 此前只到达导入终端 stdout/stderr 的信任决策。deterministic（判定由 CLI 代码
	// 对照 trust store 实算，agent 无法伪造）但属信任面的 OBSERVATION——与其他
	// observation 类 check 一样排除出证据强度分桶。verdict 字符串走
	// Meta[MetaKeyVerdict]、签名者 node_id 走 Meta[MetaKeySigner]——读方永不解析
	// Detail 散文。
	CheckBundleVerify CheckName = "bundle-verify"
	// CheckProjectSync records one git-transport sync op outcome (init/push/pull —
	// sync-convergence Phase 1). The machine-local sync-remote.json stamps SUCCESSFUL
	// ops only, so a failed push left the old timestamp standing and was invisible
	// anywhere but the terminal; this entry is the failure-visible record. Observation
	// class (says nothing about any task's verification) — excluded from
	// evidence-strength bucketing. The op rides Meta[MetaKeySyncOp].
	//
	// CheckProjectSync 记录一次 git 通道同步操作的结果（init/push/pull——
	// sync-convergence Phase 1）。机器本地的 sync-remote.json 只给成功操作打戳，
	// 失败的 push 留着旧时间戳、终端之外完全不可见；本条目是让失败可见的记录。
	// observation 类（与任何任务的验证是否实跑无关）——排除出证据强度分桶。操作
	// 名走 Meta[MetaKeySyncOp]。
	CheckProjectSync CheckName = "project-sync"
	// CheckCrossRepoImpact records the task-verify cross-repo-impact declaration check
	// (multi-repo workspace, docs/design/multi-repo-workspace.md): whether a task whose
	// repo belongs to a multi-repo workspace declared its impact (none|multi) via
	// `forge task impact`. deterministic (the gate reads the TaskState declaration +
	// the workspace manifest, the agent cannot forge the verdict). Observation class —
	// an undeclared/malformed declaration is a process signal about cross-repo
	// discipline, not verification evidence of THIS task — excluded from
	// evidence-strength bucketing like scope-drift. Advisory by default; protocol
	// cross_repo_impact: required promotes the undeclared case to a gate block.
	//
	// CheckCrossRepoImpact 记录 task-verify 的跨仓影响声明检查（多仓 workspace，
	// 见 docs/design/multi-repo-workspace.md）：所属 repo 属于多仓 workspace 的
	// 任务是否经 `forge task impact` 声明了影响（none|multi）。deterministic
	//（门禁实读 TaskState 声明 + workspace 清单，agent 无法伪造判定）。
	// observation 类——未声明/声明畸形是跨仓纪律的流程信号，非本任务的验证证据——
	// 与 scope-drift 一样排除出证据强度分桶。默认 advisory；protocol
	// cross_repo_impact: required 把未声明升级为门禁阻断。
	CheckCrossRepoImpact CheckName = "cross-repo-impact"
)

const (
	// MetaKeyVerdict / MetaKeySigner namespace bundle-verify's machine payload
	// (Entry.Meta) at the single source of truth — writer (cli/bundle_sig.go) and
	// reader (dashboard feed) cannot drift apart, the same contract-seam discipline
	// as skill-trigger's MetaKey* (skill_trigger_detail.go).
	//
	// MetaKeyVerdict / MetaKeySigner 在单一真相源处给 bundle-verify 的机器载荷
	// （Entry.Meta）命名空间——写方（cli/bundle_sig.go）与读方（dashboard feed）
	// 不可能漂移，与 skill-trigger 的 MetaKey*（skill_trigger_detail.go）同款
	// 契约缝纪律。
	MetaKeyVerdict = "verdict"
	MetaKeySigner  = "signer"
	// MetaKeySyncOp namespaces project-sync's op name (init/push/pull) — same
	// contract-seam discipline as above: writer cli/project_sync.go, reader the
	// dashboard feed.
	//
	// MetaKeySyncOp 给 project-sync 的操作名（init/push/pull）命名空间——同款契约
	// 缝纪律：写方 cli/project_sync.go，读方 dashboard feed。
	MetaKeySyncOp = "sync_op"
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
	// Delivered reports whether an advisory injection actually reached the model's context on
	// that host's channel (skill-trigger L1 delivery observability). nil = unknown (legacy entries
	// written before this field existed, or record sites that do not stamp it) — readers must treat
	// nil as "delivery unknown", NOT as delivered: hosts with dead advisory channels (kimi non-
	// UserPromptSubmit, codex Stop, cursor/copilot non-PostToolUse, windsurf always) would otherwise
	// keep inflating delivery counts from entries the model never saw — the false-prosperity
	// observability bug the kimi 2026-08-15 fix addressed for one host; this field generalizes it.
	// A pointer so false is serialized (omitempty only skips nil).
	//
	// Delivered 报告一条 advisory 注入是否真到达该宿主通道的模型上下文（skill-trigger L1 送达
	// 可观测）。nil = 未知（字段引入前的旧条目，或不落章的记录点）——读取方必须把 nil 当
	// 「送达未知」而非「已送达」：死 advisory 通道的宿主（kimi 非 UserPromptSubmit、codex Stop、
	// cursor/copilot 非 PostToolUse、windsurf 恒死）否则会继续用模型从未见过的条目虚增送达计数
	// ——即 kimi 2026-08-15 修掉的虚假繁荣观测 bug；本字段把它泛化到所有宿主。用指针使 false
	// 也能被序列化（omitempty 只跳过 nil）。
	Delivered *bool `json:"delivered,omitempty"`
	// Channel labels the host channel used for the injection (e.g. "claude/additionalContext",
	// "kimi/stdout-UserPromptSubmit", "codex/no-channel"). Written alongside Delivered by the
	// skill-trigger record site; one glance answers "which channel carried it" without re-deriving
	// the per-host routing table at analysis time.
	//
	// Channel 标注注入所走的宿主通道（如 "claude/additionalContext"、
	// "kimi/stdout-UserPromptSubmit"、"codex/no-channel"）。由 skill-trigger 记录点与 Delivered
	// 同时落章；分析时一眼可答「走的哪条通道」，无需重推每宿主路由表。
	Channel string `json:"channel,omitempty"`
	// ForgeVersion is the forge binary version that produced this entry (skill-trigger funnel
	// groups analyses by version; production-staleness questions — "which trigger set was live
	// when these hits happened" — become a join instead of archaeology).
	//
	// ForgeVersion 是产出本条目的 forge 二进制版本（skill-trigger 漏斗按版本分组分析；
	// 「这些命中发生时生产判定集是哪版」这类生产滞后问题从考古变成 join）。
	ForgeVersion string `json:"forge_version,omitempty"`
	// Meta carries check-specific structured key/values. Detail stays the human-readable
	// summary; Meta is the machine payload for analysis surfaces (per-keyword trigger stats,
	// suppression backfill, mining). Keys are namespaced per Check at the single source of
	// truth — skill-trigger keys live in skill_trigger_detail.go (MetaKey*) — so writers and
	// readers cannot drift apart, the same contract-seam discipline as DetailForSkillTrigger.
	// Values must be short strings (human-scale, not document-scale); anything larger belongs
	// in a sidecar store. omitempty: legacy entries (pre-Meta) decode with nil — readers treat
	// absent key as "unknown", never as zero-value semantics.
	//
	// Meta 携带 check 专属的结构化键值。Detail 保持人类可读摘要；Meta 是分析面的机器
	// 载荷（per-keyword 触发统计、抑制回填、挖矿）。键按 Check 在单一真相源处命名空间
	// 化——skill-trigger 的键在 skill_trigger_detail.go（MetaKey*）——写读两侧不可能漂移，
	// 与 DetailForSkillTrigger 同款契约缝纪律。值必须是短字符串（人类尺度，非文档尺度）；
	// 更大的载荷属旁路存储。omitempty：旧条目（Meta 前）解码为 nil——读方把缺键当
	// 「未知」，绝不当零值语义。
	Meta map[string]string `json:"meta,omitempty"`
	// Stamp carries the machine-attribution fields (node_id/seq/ts_hlc/sig), filled by
	// Record via nodestamp.Next — zero on legacy lines and on fail-open (stamping must
	// never block the event it rides on). Flattened into this JSON object.
	//
	// Stamp 携带机器归因字段（node_id/seq/ts_hlc/sig），由 Record 经 nodestamp.Next
	// 落章——存量行与 fail-open 时为零值（打戳绝不阻塞它依附的事件）。拍平进本
	// JSON 对象。
	nodestamp.Stamp
}

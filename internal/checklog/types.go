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
	// CheckCheatScan records advisory AI cheat-pattern scan results: at task-verify, mechanically detects new-line hits across 7 categories.
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
	// CheckEscapeHatch records usage of gate-bypass escape hatches (FORGE_TEST_COVERAGE / FORGE_WORK_ACTIVITY).
	//
	// CheckEscapeHatch 记录 gate-bypass 逃生舱的使用（FORGE_TEST_COVERAGE /
	// FORGE_WORK_ACTIVITY）。这些逃生舱是合法工具，但其使用必须
	// 留痕可审计、不能静默——agent 通过 export FORGE_TEST_COVERAGE=disable 绕过
	// test-coverage gate 时，应留下可见轨迹。A4：记录以便 forge trace 能展示
	// 逃生舱使用。Passed=true（bypass 已生效）、Checked=true、Detail 标注逃生舱名。
	CheckEscapeHatch CheckName = "escape-hatch"
	// CheckSkillTrigger records a canonical skill that the skill-trigger framework fired (passive injection via AdditionalContext) — making skill reach observable downstream.
	//
	// CheckSkillTrigger 记录 skill-trigger 框架触发（经 AdditionalContext 被动注入）的 canonical skill——
	// 让 skill 触达在下游可观测。无此记录，skill-trigger 静默注入，`forge skills usage`/`effectiveness`
	// 无法回答"哪些 canonical skill 真触发过"（dogfood 0 触发盲区）。deterministic（引擎实算声明式触发，
	// agent 无法伪造）。Passed=true、Checked=true、Detail 标 skill 名 + 触发原因。不计入证据强度
	// （它是 skill 触发的观测，非验证证据）——见 BuildEvidenceChain。
	CheckSkillTrigger CheckName = "skill-trigger"
	// CheckReviewPass records one `forge review pass` event.
	//
	// CheckReviewPass 记录一次 `forge review pass` 事件。task 模式：审过的快照
	// （HEAD + 变更 hash）与第几轮，带 TaskRef。非 task 模式（2026-08 评审可观测性）：
	// branch + diff 指纹上下文，无 TaskRef/轮次——stamp 文件按分支原子覆写，本条目是
	// 非 task 盖章唯一可回溯的历史。默认 deterministic（SourceForCheck：由 CLI 命令
	// 以 gate 实算的 hash 落盘，agent 无法伪造 hash）。它是 OBSERVATION（"审查已被
	// 声明并打戳"的标记），不是验证证据——与 cheat-scan 同类的排除出证据强度分桶
	// （见 BuildEvidenceChain）。价值在于让审查-返工循环可度量（每任务轮次数）。
	CheckReviewPass CheckName = "review-pass"
	// CheckGoalContract records the task-implement goal-contract check (task-goal-contract).
	//
	// CheckGoalContract 记录 task-implement 的 goal 契约校验（task-goal-contract）：
	// 任务启动时钉了 goal.md 哈希则重算比对——不一致即 BLOCKED（改 goal 等于改判据，
	// 必须重开任务）；未钉契约（老任务 / 自由文本 goal）只记 advisory。属 observation
	// 类，未列入 evidence 白名单，不进证据强度分桶。
	CheckGoalContract CheckName = "goal-contract"
	// CheckToolFailure records one PostToolUseFailure observation (2026-08-22 failure-track hook, #4-A).
	//
	// CheckToolFailure 记录一次 PostToolUseFailure 观察（2026-08-22 failure-track
	// hook，#4-A）：Bash/PowerShell 命令失败、宿主上报了错误文本。deterministic
	// （宿主 stdin 来源，agent 无法伪造）但是 OBSERVATION 而非验证——工具失败
	// 不代表任务自身的门禁跑没跑，故不得喂给 evidence strength（BuildEvidenceChain
	// 排除）。价值在于让编译/测试失败循环可观测：此前 Bash 工具里失败的 `go build`
	// 在 forge 侧零痕迹，compile-fix-loop skill 的触达无法与真实失败关联。
	CheckToolFailure CheckName = "tool-failure"
	// CheckSubagentStop records one SubagentStop observation (2026-08-22 subagent-track hook, #4-A).
	//
	// CheckSubagentStop 记录一次 SubagentStop 观察（2026-08-22 subagent-track
	// hook，#4-A）：子 agent 结束，携带 agent_id/agent_type 与交付摘要。v1 仅观察
	// 不阻断（空交付阻断的假阳性大于收益）。deterministic（宿主 stdin 来源）但属
	// OBSERVATION——与其他条目一样排除出 evidence strength。价值在补归因缺口：
	// 子 agent 活动此前在 forge 侧零记录，sessions.jsonl 约 53% 会话缺 agent_type
	// （2026-08 归因审计）。
	CheckSubagentStop CheckName = "subagent-stop"
	// CheckConventionsLint records the conventions-profile layer-3 advisory at task-verify (2026-08-28).
	//
	// CheckConventionsLint 记录 conventions-profile 层 3 在 task-verify 的
	// advisory（2026-08-28，conventions-followups）：任务 Bash 历史（toollog）
	// 里是否出现过档案声明的 lint 命令？仅在可判定时落盘（档案+lint 命令
	// 在场且任务有工具遥测）——Passed=true 表示见到 lint 签名，false 表示
	// 发过一次提醒。deterministic（toollog + 签名匹配；没记录过的东西 agent
	// 伪造不了）但属过程观察，绝非「lint 通过」的验证——与其他观察 check
	// 一样排除出 evidence strength（见到的 lint 命令跑挂了仍归 agent 修）。
	CheckConventionsLint CheckName = "conventions-lint"
	// CheckBundleVerify records one bundle-signature verification verdict at import.
	//
	// CheckBundleVerify 记录导入时的一次 bundle 验签判定（node-identity.md §3）——
	// 此前只到达导入终端 stdout/stderr 的信任决策。deterministic（判定由 CLI 代码
	// 对照 trust store 实算，agent 无法伪造）但属信任面的 OBSERVATION——与其他
	// observation 类 check 一样排除出证据强度分桶。verdict 字符串走
	// Meta[MetaKeyVerdict]、签名者 node_id 走 Meta[MetaKeySigner]——读方永不解析
	// Detail 散文。
	CheckBundleVerify CheckName = "bundle-verify"
	// CheckTaskStarted records the task-start boundary event (multi-task-concurrency design §5).
	//
	// CheckTaskStarted 记录任务启动边界事件（multi-task-concurrency 设计 §5，L2 事件
	// 化）：`forge task start` 不再 Clear 日志——破坏性截断会抹掉并发任务在途的证据
	// 链、断掉崩溃审计——改为追加本边界标记，消费侧一律按 TaskRef 过滤
	//（LoadForTask / LatestByCheckForSession 本就如此）。观察类的典型：边界是时间线
	// 标记而非任何验证结果——排除出证据强度分桶。该标记也是后续日志滚动（janitor
	// 按 task_started 边界归档）与跨机 ts 归并的分段锚点。
	CheckTaskStarted CheckName = "task-started"
	// CheckTakeoverPolicy records per-project takeover state flips (forge on/off,
	// Project Policy Layer P1). Observation class: the audit trail of "who turned
	// takeover off/on and when" — never task verification, excluded from evidence-
	// strength bucketing. Written only when the project already has a DataDir; for
	// never-initialized projects the registry Entry decision fields are the audit.
	//
	// CheckTakeoverPolicy 记录 per-project 接管状态翻转（forge on/off，Project
	// Policy Layer P1）。观察类："谁在何时开/关了接管"的审计轨迹——绝非任务验证，
	// 排除出证据强度分桶。仅在项目已有 DataDir 时落盘；从未 init 的项目以注册表
	// Entry 决策字段为审计。
	CheckTakeoverPolicy CheckName = "takeover-policy"
	// CheckSelfReport records the task-complete self-report consistency check:
	// verify-class commands claimed as done in the checklist are matched against
	// the Bash commands the toollog actually recorded for this task (focus-batches
	// §1b, arXiv 2605.29442 "inaccurate self-reporting"). Deterministic (gate
	// compares two local ledgers, the agent cannot forge the toollog side).
	// Verdicts: pass (all claims evidenced) / warn (non-test claims unmatched) /
	// fail (test-class claims with zero matching Bash evidence across the whole
	// task — the inaccurate-self-reporting shape). Observation class about the
	// task's own honesty, feeding review and scoring context.
	//
	// CheckSelfReport 记录 task-complete 的自报一致性检查：checklist 已勾选项里
	// 声称执行过的验证类命令，与 toollog 为本任务实际记录的 Bash 命令集比对
	//（focus-batches §1b，arXiv 2605.29442 "inaccurate self-reporting"）。
	// deterministic（门禁比对两份本地台账，agent 无法伪造 toollog 侧）。
	// 判定：pass（全部声称有据）/ warn（非测试类声称未匹配）/ fail（测试类声称
	// 在任务全程零匹配——虚报进度的形态）。关于任务自身诚实度的观察类，喂给
	// review 上下文。
	CheckSelfReport CheckName = "self-report-consistency"
	// CheckGatePush records one `forge gate push` outcome (push-boundary gate:
	// cheat-scan re-run over base...HEAD + unresolved BLOCKED tasks on the
	// branch; focus-batches §1c). Deterministic (gate re-computes from git and
	// the ledgers, independent of whether local hooks ever fired — the Codex
	// #28365 telemetry-spoof lesson: local self-report needs upper-layer
	// re-verification). Push evidence snapshot lands in DataDir/pushes/.
	//
	// CheckGatePush 记录一次 `forge gate push` 的结果（推送边界门禁：对
	// base...HEAD 重跑 cheat-scan + 本分支未消解 BLOCKED 任务；focus-batches
	// §1c）。deterministic（门禁从 git 与台账重算，不依赖本地 hook 是否生效——
	// Codex #28365 遥测欺骗的教训：本地自我报告需上层复核）。推送证据快照落
	// DataDir/pushes/。
	CheckGatePush CheckName = "gate-push"
	// CheckTaskStalled records a watchdog stall observation (focus-batches §2d):
	// an incomplete task whose last ledger activity (checklog/toollog rows with
	// its TaskRef) is older than the stall threshold. Observation class — the
	// always-on governance signal for zombie claims and disconnected overnight
	// sessions; marker-throttled to at most one row per task per hour.
	//
	// CheckTaskStalled 记录 watchdog 的停滞观察（focus-batches §2d）：未完成任务
	// 在两份台账（checklog/toollog 带 TaskRef 的行）里的最后活动时间超过停滞阈值。
	// 观察类——僵尸认领与 overnight 会话失联的 always-on 治理信号；marker 节流每
	// 任务每小时至多一条。
	CheckTaskStalled CheckName = "task-stalled"
	// CheckLoopExhausted records the review→implement loop exhaustion state
	// (artifact-chain-workflow.md「回边语义」节): Passed=false rows are the
	// escalation record when complete is blocked (round budget spent on an open
	// finding, or a resolved finding revived); Passed=true Level=warn rows are
	// the human reset decision (`forge task finding --reset-loop --note`).
	// Deterministic — both rows come from forge's own ledger arithmetic, not
	// agent claims.
	//
	// CheckLoopExhausted 记录审查回环耗尽状态（「回边语义」节）：Passed=false 为
	// complete 被拦时的升级记录（轮次预算耗尽或已解决 finding 复活）；
	// Passed=true + warn 为人工重置裁决（forge task finding --reset-loop --note）。
	// deterministic——两类行都出自 forge 自身账本运算，非 agent 自述。
	CheckLoopExhausted CheckName = "loop-exhausted"
	// CheckAcceptanceRecurrence records a same-cause acceptance recurrence
	// (loop-contract B1-1): the same criterion failed with the same normalized
	// fingerprint across consecutive runs that each followed a code change.
	// Advisory (Passed=false, Level=advisory); not in the evidence whitelist —
	// it is a stop signal for the outer loop, never a verification claim.
	//
	// CheckAcceptanceRecurrence 记录一次验收同因复发（loop 契约 B1-1）：同一验收
	// 在每次之间都改过代码的连续实跑里以同一规范化指纹失败。advisory
	// （Passed=false、Level=advisory）；不进 evidence 白名单——它是给外层循环的
	// 停止信号，不是验证声明。
	CheckAcceptanceRecurrence CheckName = "acceptance-recurrence"
	// CheckNextHint records one "→ next:" line appended to gate/status/complete output; Meta[MetaKeySuggested] carries the suggested command.
	//
	// CheckNextHint 记录一次挂在 gate/status/complete 输出末尾的「→ next:」行：
	// Meta[MetaKeySuggested] 携带建议命令。
	// deterministic（forge 自身渲染）但属引导层 OBSERVATION——排除出证据强度分桶。
	CheckNextHint CheckName = "next-hint"
	// CheckTreeGuard records one off-tree decision from the task-tree guard
	// (docs/design/task-tree-guard.md §5): the BLOCK (Passed=false, Meta carries
	// anchor/wtid/resolve_path) or the same-off-tree announce emitted by the hook
	// layer before the guard script runs. The escape-hatch path under
	// FORGE_TASK_TREE_GUARD=0 is NOT this check — it is recorded as CheckEscapeHatch
	// with Meta escape.gate=task-tree-guard.
	//
	// CheckTreeGuard 记录一次错树守护判定（docs/design/task-tree-guard.md §5）：门禁侧的
	// BLOCK（Passed=false，Meta 带 anchor/wtid/resolve_path），或 hook 侧在 guard 脚本
	// 之前的同一次错树留痕。**关闭守护的逃生不属于这条 check**——它记在
	// CheckEscapeHatch（Meta escape.gate=task-tree-guard），否则 escape 会同时算进
	// 「有多少门禁在错树上拦下」与「多少人绕过」，把两个方向相反的口径混成一个。
	// deterministic（gate/guard 自算，agent 无法伪造）；没有这条记录，错树守护是静默的。
	CheckTreeGuard CheckName = "tree-guard"
)

const (
	// MetaKeyVerdict / MetaKeySigner namespace bundle-verify's machine payload.
	//
	// MetaKeyVerdict / MetaKeySigner 在单一真相源处给 bundle-verify 的机器载荷
	// （Entry.Meta）命名空间——写方（cli/bundle_sig.go）与读方（dashboard feed）
	// 不可能漂移，与 skill-trigger 的 MetaKey*（skill_trigger_detail.go）同款
	// 契约缝纪律。
	MetaKeyVerdict = "verdict"
	MetaKeySigner  = "signer"
)

// MetaKeyResolvePath / MetaKeyPostSeal namespace the task-attribution probe payload: any check row may carry them, so attribution stays in Meta instead of widening Entry. Writers: taskpipeline annotate marks rows landing after the evidence seal (MetaKeyPostSeal), tree-guard gate rows name the detection path that resolved the task (MetaKeyResolvePath). Consumers read Meta by key — one source of truth, same contract-seam discipline as the MetaKey* constants above.
//
// MetaKeyResolvePath / MetaKeyPostSeal 是任务归因探针的机器载荷命名空间：任何 check 行
// 都可携带，归因因此留在 Meta 里而不必扩宽 Entry。写方：taskpipeline 的 annotate 给
// 封印之后的行打 MetaKeyPostSeal，tree-guard 门禁行写 MetaKeyResolvePath（解析到任务的
// 探测路径）；消费侧按 key 读取。单一真相源，与上方 MetaKey* 同款契约缝。
const (
	// MetaKeyResolvePath records which detection path resolved the active task: active-file / workspace / branch / legacy (taskpipeline.ResolvePath*).
	//
	// MetaKeyResolvePath 记录 active task 经哪条路径解析到：active-file / workspace /
	// branch / legacy（taskpipeline.ResolvePath* 常量）。
	MetaKeyResolvePath = "resolve_path"
	// MetaKeyPostSeal = "true" marks a row landing after the task's evidence seal (task-complete gate passed) — kept for trace, excluded from scoring/conclusion by the TaskState.SealedAt window.
	//
	// MetaKeyPostSeal = "true" 标记该行落在任务证据封印（task-complete 门禁通过）之后
	// ——行保留供 trace，但结论按 TaskState.SealedAt 截断不计入。
	MetaKeyPostSeal = "post_seal"
	// MetaKeySuggested carries the command a next-hint row proposed; writer is taskpipeline.NextHint, consumers read it from Meta alongside the row.
	//
	// MetaKeySuggested 携带 next-hint 行建议的命令：写方 taskpipeline.NextHint，
	// 消费侧随行从 Meta 读取。
	MetaKeySuggested = "suggested"
)

// EvidenceSource marks the source of a checklog evidence entry, distinguishing deterministic from agent-claim.
//
// EvidenceSource 标注一条 checklog 证据的来源，区分 deterministic（hook/外部
// 工具实跑或 gate 代码判定，不可被 agent 伪造）与 agent-claim（agent 自述的
// 验证）。
//
// 用途：review 子 agent 据此对冲 LLM-judge 盲区——业界反复证实（Tenure
// "0.85 vs 0.000" 案例）LLM judge 看不出"agent 跳过前置就声明完成"的最严重
// 失败模式；只有 deterministic 证据能照出。EvidenceChain 按 Source 分桶，
// review 时优先采信 deterministic，agent-claim 仅作初筛信号。
type EvidenceSource string

const (
	// EvidenceDeterministic: produced by hook/gate code actually running or verdicting (auto-compile, assertion-check, file-sentinel, test-coverage-gate, etc.).
	//
	// EvidenceDeterministic: hook/gate 代码实跑或判定产生（auto-compile、
	// assertion-check、file-sentinel、test-coverage-gate 等）。agent 无法伪造。
	EvidenceDeterministic EvidenceSource = "deterministic"
	// EvidenceAgentClaim: agent self-reported verification (e.g. `I ran the end-to-end tests` but not confirmed by a hook).
	//
	// EvidenceAgentClaim: agent 自述的验证（如"我跑过端到端测试了"但未由 hook
	// 确认）。可信度低于 deterministic，review 应区别对待。
	EvidenceAgentClaim EvidenceSource = "agent-claim"
)

// SourceForCheck returns the default evidence source for a CheckName.
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

// Level classifies a checklog entry's severity in one structured field, so consumers (dashboard/trace/review) no longer parse the Detail prose prefixes (BLOCKED: / ADVISORY:) to tell a hard block from a soft signal.
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
	// LevelWarn: a noteworthy but tolerated condition (e.g. escape-hatch usage, infrastructure degraded but fail-open).
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

// GateOutcome classifies a FAILED verify-time gate entry by who surfaced the fact
// first — the discipline-first measurement axis (discipline-first-gates 2026-09
// P1-A). The metric it enables: discovery rate must fall as first-line signals
// (nudges, selfcheck) take hold; a rising confirmation share means signals are
// delivered but not acted on.
//
// GateOutcome 按「谁先披露事实」分类**失败**的 verify 期门禁条目——纪律优先
// 度的度量轴（discipline-first-gates 2026-09 P1-A）。它点亮的指标：discovery
// rate 应随第一防线信号（nudge、selfcheck）扎根而下降；confirmation 占比上升
// 则说明信号送达了但没有被执行。
type GateOutcome string

const (
	// OutcomeDiscovery: the gate is the first disclosure of this fact to the agent —
	// no prior delivered first-line signal exists in the task. First line missing.
	//
	// OutcomeDiscovery：门禁是对 agent 的第一披露点——task 内不存在已送达的第一
	// 防线信号。第一防线缺位。
	OutcomeDiscovery GateOutcome = "discovery"
	// OutcomeConfirmation: a first-line signal (e.g. test-nudge) was already
	// delivered for this fact and not acted on — the gate confirms realized
	// discipline debt. First line present, discipline not executed.
	//
	// OutcomeConfirmation：该事实的第一防线信号（如 test-nudge）已送达但未行动——
	// 门禁确认的是已兑现的纪律债。第一防线在，纪律未执行。
	OutcomeConfirmation GateOutcome = "confirmation"
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

// DeriveLevel infers the Level of an entry from Passed + Detail prefixes when the caller did not set one explicitly.
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

// EffectiveLevel returns the entry's Level, deriving it from Passed + Detail when the field is empty (old archived lines have no level — history is not rewritten; the fallback is applied at read time).
//
// EffectiveLevel 返回条目的 Level；字段为空时（旧归档行无 level——不改写
// 历史，读取时兜底）从 Passed + Detail 推导。
func (e *Entry) EffectiveLevel() Level {
	if e.Level != "" {
		return e.Level
	}
	return DeriveLevel(e)
}

// IsFailure reports whether the level denotes a failed verdict (fail or blocked) — the read-side predicate analysis surfaces use so they never spell the blocked level themselves (the compat blocking-sites face scans source for producers of blocked outcomes; readers must not look like producers).
//
// IsFailure 报告该级别是否为失败判定（fail 或 blocked）——分析面的读侧谓词。读方统一走
// 本方法而不自己拼 blocked 级别：compat 快照的 blocking-sites 面按源码扫描「产出阻断」的
// 位点，读方若出现同名 token 会被误计为新阻断位点。
func (l Level) IsFailure() bool {
	return l == LevelFail || l == LevelBlocked
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
	// Level is the structured severity (pass/fail/warn/blocked/advisory). If left empty at Record time, DeriveLevel fills it from Passed + Detail prefixes; readers use EffectiveLevel for the same fallback on old lines.
	//
	// Level 是结构化级别（pass/fail/warn/blocked/advisory）。Record 时若留空，
	// 由 DeriveLevel 从 Passed + Detail 前缀兜底推导；读取侧用 EffectiveLevel
	// 对旧行做同样的兜底。
	Level Level `json:"level,omitempty"`
	// Source marks the evidence source (deterministic vs agent-claim).
	//
	// Source 标注证据来源（deterministic vs agent-claim）。Record 时若留空，
	// 按 SourceForCheck 兜底推断，故历史记录点无需逐个改造也能进证据链分桶。
	Source EvidenceSource `json:"source,omitempty"`
	// Outcome classifies a FAILED verify-time gate entry by who surfaced the fact
	// first (see GateOutcome). Empty on passing and legacy entries; readers must
	// treat empty as unclassified, never as discovery.
	//
	// Outcome 按「谁先披露」分类**失败**的 verify 期门禁条目（见 GateOutcome）。
	// 通过与历史条目留空；读方必须把空当未分类，绝不当 discovery。
	Outcome    GateOutcome `json:"outcome,omitempty"`
	RecordedAt time.Time   `json:"recorded_at"`
	// Delivered reports whether an advisory injection actually reached the model's context on that host's channel.
	//
	// Delivered 报告一条 advisory 注入是否真到达该宿主通道的模型上下文（skill-trigger L1 送达
	// 可观测）。nil = 未知（字段引入前的旧条目，或不落章的记录点）——读取方必须把 nil 当
	// 「送达未知」而非「已送达」：死 advisory 通道的宿主（kimi 非 UserPromptSubmit、codex Stop、
	// cursor/copilot 非 PostToolUse、windsurf 恒死）否则会继续用模型从未见过的条目虚增送达计数
	// ——即 kimi 2026-08-15 修掉的虚假繁荣观测 bug；本字段把它泛化到所有宿主。用指针使 false
	// 也能被序列化（omitempty 只跳过 nil）。
	Delivered *bool `json:"delivered,omitempty"`
	// Channel labels the host channel used for the injection (skill-trigger delivery observability).
	//
	// Channel 标注注入所走的宿主通道（如 "claude/additionalContext"、
	// "kimi/stdout-UserPromptSubmit"、"codex/no-channel"）。由 skill-trigger 记录点与 Delivered
	// 同时落章；分析时一眼可答「走的哪条通道」，无需重推每宿主路由表。
	Channel string `json:"channel,omitempty"`
	// ForgeVersion is the forge binary version that produced this entry (skill-trigger funnel analytics).
	//
	// ForgeVersion 是产出本条目的 forge 二进制版本（skill-trigger 漏斗按版本分组分析；
	// 「这些命中发生时生产判定集是哪版」这类生产滞后问题从考古变成 join）。
	ForgeVersion string `json:"forge_version,omitempty"`
	// Meta carries check-specific structured key/values. Detail stays the human-readable summary; Meta is the machine payload for analysis surfaces (per-keyword trigger stats, suppression backfill, mining).
	//
	// Meta 携带 check 专属的结构化键值。Detail 保持人类可读摘要；Meta 是分析面的机器
	// 载荷（per-keyword 触发统计、抑制回填、挖矿）。键按 Check 在单一真相源处命名空间
	// 化——skill-trigger 的键在 skill_trigger_detail.go（MetaKey*）——写读两侧不可能漂移，
	// 与 DetailForSkillTrigger 同款契约缝纪律。值必须是短字符串（人类尺度，非文档尺度）；
	// 更大的载荷属旁路存储。omitempty：旧条目（Meta 前）解码为 nil——读方把缺键当
	// 「未知」，绝不当零值语义。
	Meta map[string]string `json:"meta,omitempty"`
	// DurationMS is how long the checked thing actually took (0 = not measured).
	//
	// DurationMS 是这次检查实际耗时（毫秒；0 = 未测）。轨迹观测（2026-11-20）把它落进
	// checklog，report 才能回答「门禁/验收/评审各花多久」，而不是只能看墙钟推断。
	DurationMS int64 `json:"duration_ms,omitempty"`
	// Attempt is the 1-based occurrence of this check for the same task (0 = legacy).
	//
	// Attempt 是同一任务下该检查的第几次（从 1 起；0 = 旧条目）。重试次数此前只能从
	// 多条相同 check 的条数反推，落成字段后可直接读。
	Attempt int `json:"attempt,omitempty"`
	// Steps is the number of units the check covered (criteria, checks, rounds).
	//
	// Steps 是该检查覆盖的单元数（判据条数 / 检查项数 / 轮次）。耗时没有分母时无法
	// 解释——3 秒跑 1 条和 3 秒跑 40 条是两回事。
	Steps int `json:"steps,omitempty"`
	// Stamp 携带机器归因字段（node_id/seq/ts_hlc/sig），由 Record 经 nodestamp.Next
	// 落章——存量行与 fail-open 时为零值（打戳绝不阻塞它依附的事件）。拍平进本
	// JSON 对象。
	nodestamp.Stamp
}

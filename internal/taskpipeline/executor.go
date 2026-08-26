package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/docsconsistency"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/review"
	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/shellexec"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// CheckNameDocsConsistency is the checklog name for task-complete's docs-consistency advisory,
// letting trace surface drift signals even when the gate passes (advisory, never blocks).
//
// CheckNameDocsConsistency 是 task-complete 的 docs-consistency advisory 的 checklog 名，
// 让 trace 即使 gate 通过（advisory，永不阻塞）也能照出 drift 信号。
const CheckNameDocsConsistency checklog.CheckName = "docs-consistency-gate"

// CheckNameReviewSnapshot is the checklog name for the review-snapshot fail-open case
// (review baseline commit unreachable, amend/rebase rewrote history). fail-open is by design
// (amend is a normal workflow; forced re-review would loop forever), but the event must be
// persisted so score/dashboard can reflect that the task passed via fail-open rather than a
// true review, leaving a traceable record instead of a transient stderr message.
//
// CheckNameReviewSnapshot 是 review-snapshot fail-open 场景的 checklog 名——
// fail-open case (审查基线 commit 不可达，amend/rebase 改写历史）。fail-open 是设计本意
// （amend 是正常工作流，强复审会死循环），但必须落盘留痕——让 score/dashboard 能反映
// 「该任务靠 fail-open 而非真复审通过」，事后可追溯，而非只 stderr 一闪而过。
const CheckNameReviewSnapshot checklog.CheckName = "review-snapshot-failopen"

// CheckNameTelemetryMissing is the checklog name for the work-activity telemetry-missing
// degrade (hosts whose PostToolUse dispatch is not wired, e.g. kimi): both telemetry
// channels (toollog + hook-dispatched checklog entries) are empty, so the work-activity
// hard gate would be a 100% false positive and degrades to advisory. Persisted so
// score/dashboard/trace can see the gate passed on degraded telemetry, not real activity.
//
// CheckNameTelemetryMissing 是 work-activity 遥测缺失降级的 checklog 名（host 的
// PostToolUse 分发未接，如 kimi）：两条遥测通道（toollog + hook 分发的 checklog 条目）
// 都为空，此时 work-activity 硬门禁是 100% 误报，降级为 advisory。落盘让
// score/dashboard/trace 能看到该 gate 是在遥测降级下放行的，而非有真实工作活动。
const CheckNameTelemetryMissing checklog.CheckName = "telemetry-missing"

// CheckNameBranchUnmerged is the checklog name for the task-complete branch-merged
// advisory: the task's feature branch is not yet merged into the mainline at complete
// time — 'complete' is not 'delivered'. Advisory only, never blocks.
//
// CheckNameBranchUnmerged 是 task-complete 分支归属 advisory 的 checklog 名：完成时
// 任务的 feature 分支尚未合入主干——「完成」不等于「交付」。仅 advisory，永不阻塞。
const CheckNameBranchUnmerged checklog.CheckName = "branch-unmerged"

// CheckNameGoalOutputMismatch is the checklog name for the task-complete goal↔output
// coarse-match advisory: the task title and the actually-changed files share no keyword
// at all — a smell of delivering the wrong content. Advisory only, never blocks.
//
// CheckNameGoalOutputMismatch 是 task-complete 目标↔产出粗匹配 advisory 的 checklog
// 名：任务标题与实改文件零关键词交集——交付内容可能有误的信号。仅 advisory，永不阻塞。
const CheckNameGoalOutputMismatch checklog.CheckName = "goal-output-mismatch"

// CheckNameUncommittedAtComplete is the checklog name for the task-complete
// commit-ordering advisory: the working tree still has uncommitted changes at
// complete time — the documented protocol order is gates → git commit →
// forge task complete (complete clears the active task ref, so post-complete
// source writes lose task tracking/protection). Observed 2026-08-17: a session
// passed all three gates, ran complete, and only committed 32 minutes later —
// zero material damage by luck (no writes in between), but the window is real.
// Advisory only, never blocks.
//
// CheckNameUncommittedAtComplete 是 task-complete 提交顺序 advisory 的 checklog
// 名：complete 时工作区仍有未提交变更——协议规定的顺序是三门禁 → git commit →
// forge task complete（complete 会清空 active task ref，之后的源码写入脱离任务
// 追踪/保护）。2026-08-17 实证：某会话三门禁全过后先 complete、32 分钟后才
// commit——侥幸零实质损害（其间无写入），但窗口真实存在。仅 advisory，永不阻塞。
const CheckNameUncommittedAtComplete checklog.CheckName = "uncommitted-at-complete"

// CheckNameDependencyGate is the checklog name recorded when task-verify/task-complete BLOCKS on an
// undelivered upstream dependency. Matches the established "BLOCKED 必落盘" pattern (skill-decisions /
// test-coverage also record before blocking) so score / dashboard / forge trace can see a task
// repeatedly stalling at the gate waiting on an upstream — otherwise that repeated-stall signal is
// invisible and indistinguishable from "never attempted".
//
// CheckNameDependencyGate 是 task-verify/task-complete 因上游依赖未交付而 BLOCKED 时记的 checklog 名。
// 对齐既有的「BLOCKED 必落盘」模式（skill-decisions / test-coverage 阻断前也落盘），使 score /
// dashboard / forge trace 能照出任务反复卡在门禁等上游——否则该反复停滞信号不可见，与「从未尝试」无法区分。
const CheckNameDependencyGate checklog.CheckName = "dependency-gate"

// skillDecisionsEscapeAdvisoryFmt is the ADVISORY printed when the skill-decisions
// guardrail is bypassed via the escape hatch (per-task override or env). Package-level
// single source so the wording can be guard-tested (TestTaskVerify_SkillDecisionsGuardrail_
// EscapeHatchBypasses asserts it mentions the 2026-08 evidence-scaled carve-out — an
// advisory omitting it understates heavy-evidence tasks' Strength standing).
//
// skillDecisionsEscapeAdvisoryFmt 是经逃生舱（per-task override 或 env）绕过
// skill-decisions guardrail 时打印的 ADVISORY。包级单一来源，供文案守卫测试断言
// （TestTaskVerify_SkillDecisionsGuardrail_EscapeHatchBypasses 断言它提及 2026-08
// 证据缩放豁免——遗漏它的提示会低估重证据任务的 Strength 状态）。
const skillDecisionsEscapeAdvisoryFmt = `skill %s 的 SKILL.md 改动已用 --skill-decisions disable 逃生舱绕过 guardrail——evidence 强度降级到 Weak（重证据任务按证据缩放豁免）。仍建议跑 'forge skills decide --skill <name> --outcome <accept|reject> --diagnosis <为何改> --revision <改了啥> --evidence <依据>' 记决策，避免下一轮 agent 重复探索已失败方向`

// recordAudit persists a checklog entry and makes a persistence failure audible:
// checklog.Record's error is otherwise easy to drop (the call reads like a pure
// side effect), but the persisted evidence is indispensable — score/dashboard/trace
// all read these entries, so a silent write failure leaves 'why did the task stall
// at this gate' with no signal. Warn on stderr (same style as the other gate
// warnings) and continue: audit itself must never block the gate.
//
// recordAudit 落盘 checklog 条目并让落盘失败可听见：checklog.Record 的 error 极易
// 被丢掉（调用读起来像纯副作用），但落盘证据不可缺——score/dashboard/trace 都读
// 这些条目，静默写失败会让「task 为何卡在某门禁」无信号。按包内既有 warning 风格
// 打 stderr 后继续：审计自身绝不阻断门禁。
func recordAudit(root string, entry *checklog.Entry) {
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
	}
}

// taskStartReadGraceWindow bounds how long before a task's StartedAt a Read still counts
// toward read-before-edit, recovering the task-start/Read race (see
// toolusage.ReadEditCountsGraceWindow). 60s covers the parallel-tool-call window — Reads
// sent alongside `forge task start` may land under the previous task's ref and/or carry
// timestamps earlier than StartedAt, which would otherwise be excluded from
// ReadEditCounts(taskRef).
//
// taskStartReadGraceWindow 限定 task 的 StartedAt 之前多久的 Read 仍计作
// read-before-edit，用于恢复 task-start/Read race（见
// toolusage.ReadEditCountsGraceWindow）。60s 覆盖并行工具调用窗口——与
// `forge task start` 同时发出的 Read 可能落到前一 task 的 ref 下，和/或时间戳
// 早于 StartedAt，从而被排除出 ReadEditCounts(taskRef)。
const taskStartReadGraceWindow = time.Minute

// ExecuteResult carries the result of a single task gate execution.
//
// ExecuteResult 承载一次 task gate 执行的结果。
type ExecuteResult struct {
	GateID  string
	Passed  bool
	Message string
}

// ExecuteTaskGate runs the checks for a single task gate.
// auto gates (task-implement) execute the corresponding hook script here.
// non-auto gates only verify that the gate has been marked passed (the actual work is done by the AI agent).
//
// ExecuteTaskGate 执行单个 task gate 的检查。
// auto gate（task-implement）由本函数跑对应 hook 脚本。
// 非 auto gate 只校验该 gate 是否已被标记为通过（实际工作由 AI agent 完成）。
func ExecuteTaskGate(root string, gateID string, state *TaskState) (*ExecuteResult, error) {
	gate := GateByID(gateID)
	if gate == nil {
		return nil, fmt.Errorf("unknown task gate: %s", gateID)
	}

	// generic kind (research/design/pure-continuation tasks) bypasses gate checks — these tasks
	// have no code changes to compile/test/review, and forcing 3 gates would turn opening a
	// research/continuation task into a gate burden, contradicting the low-friction persistence
	// positioning of the continuity source of truth.
	// kind default empty/'code' still goes through the full gate flow (backward compatibility:
	// old tasks have no Kind field). Mark passed directly, skipping prerequisite gates / review
	// snapshot / work-activity / all advisory checks — the value of a generic task lies in the
	// persisted plan/decisions, not in the gates. task-complete scoring also branches on this
	// (see cli/runTaskComplete).
	//
	// generic kind（调研/设计/纯接续任务）不走门禁检查——这些任务没有代码变更可编译/测试/审查，
	// 强制 3 道门禁会把「开个调研/接续任务」变成过门禁负担，与接续真相源低摩擦持久化的定位相悖。
	// kind 默认空/"code"仍走完整门禁（向后兼容：老 task 无 Kind 字段）。直接标通过，跳过前置
	// gate / 审查快照 / 工作活动 / advisory 全部检查——generic task 的价值在持久化的 plan/决策，
	// 不在门禁。complete 评分也据此分流（见 cli/runTaskComplete）。
	if state.IsGeneric() {
		return &ExecuteResult{
			GateID:  gateID,
			Passed:  true,
			Message: fmt.Sprintf("%s - skipped (generic task: 走接续不走门禁)", gate.Name),
		}, nil
	}

	// DependsOn gate (design phase 3): a task with unsatisfied upstream dependencies cannot pass
	// task-verify or task-complete — the worker could not have verified/completed work whose inputs
	// were never delivered. Checked only at verify/complete: implement may proceed while an upstream
	// is still in flight (writing code against an expected interface is fine; blocking there would
	// needlessly serialize the graph). A missing upstream ref counts as pending (not delivered) so
	// an aborted or typo'd dependency cannot be silently bypassed. generic tasks already returned.
	//
	// DependsOn 门禁（设计阶段3）：上游依赖未满足的 task 不能过 task-verify/task-complete——工作方无法
	// 验收/完成一个输入从未交付的工作。仅在 verify/complete 查：implement 可在上游进行中时推进（针对
	// 预期接口写代码无妨，那时阻塞只会无谓串行化依赖图）。缺失的上游 ref 计为 pending（未交付），故
	// abort/拼错的依赖无法被静默绕过。generic task 已在上方 return。
	if len(state.DependsOn) > 0 && state.CompletedAt == nil &&
		(gateID == `task-verify` || gateID == `task-complete`) {
		pending := PendingDependencies(root, state.DependsOn)
		if len(pending) > 0 {
			// BLOCKED 必落盘（与 skill-decisions / test-coverage 阻断前落盘一致）：否则任务反复卡在
			// 依赖门禁的停滞信号对 score/dashboard/forge trace 不可见，与「从未尝试」无法区分。
			//
			// BLOCKED must hit disk (matching skill-decisions / test-coverage recording before a
			// block): otherwise the signal that a task repeatedly stalls at the dependency gate is
			// invisible to score/dashboard/forge trace, indistinguishable from "never attempted".
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameDependencyGate,
				Passed:  false,
				Checked: true,
				TaskRef: state.TaskRef,
				// 显式标 LevelBlocked：Detail 不以 "BLOCKED: " 起头（是「%s 拒绝：…」），DeriveLevel 不会判
				// 为 blocked，导致这条 HARD 阻断在 score/dashboard/forge trace 里被分桶成普通告警——与「它确实
				// 是硬阻断」不符。caller-set Level 恒优先于 DeriveLevel（store.go Record）。
				Level:  checklog.LevelBlocked,
				Detail: fmt.Sprintf(`%s 拒绝：上游依赖未交付或不存在（%s）`, gateID, strings.Join(pending, `, `)),
			})
			return nil, GateBlocked(`%s 拒绝（HARD stop）：上游 task 未交付或不存在（%s，可能是未创建/已 abort/拼错；key:ref 跨仓依赖还会因目标仓未交付或数据不可读而阻塞）；forge task mine --blocked 查看详情，或先推进上游交付`, gateID, strings.Join(pending, `, `))
		}
	}

	// Prerequisite check: all earlier gates must have passed.
	//
	// 校验前置：所有更早的 gate 必须已通过
	gates := DefaultGates()
	for _, g := range gates {
		if g.ID == gateID {
			break
		}
		if !state.gatePassed(g.ID) {
			return nil, GateBlocked("prerequisite gate %q has not passed — complete earlier gates first (HARD stop, not a reminder)", g.ID)
		}
	}

	// task-complete hard prerequisite: code-review-gate must have passed (ReviewPassed=true).
	// Prevents an agent from self-declaring completion to skip the sub-agent review — this is
	// the enforcement point on the task path of the 'review before commit' dual-path design
	// (the non-task path is intercepted by the review-stop hook). The agent must dispatch a
	// read-only sub-agent to review the current diff and then run `forge review pass` to mark
	// it before this gate (and thus task-complete) can pass.
	// Re-checking an already-completed task (CompletedAt set) skips this — history is not retroactive.
	//
	// task-complete 硬前置：code-review-gate 必须已通过（ReviewPassed=true）。
	// 防 agent 自称完成跳过子 agent 审查——这是「提交前必审」双路径里 task 路径的强制点
	// （非 task 路径由 review-stop hook 拦）。agent 须派只读子 agent 审查后运行
	// `forge review pass` 标记，才能过此门禁进而 task complete。
	// 复检已完成任务（CompletedAt 已设）时跳过——历史任务不追溯。
	if gateID == "task-complete" && !state.ReviewPassed && state.CompletedAt == nil {
		return nil, GateBlocked("task-complete requires code-review-gate: 派只读子 agent 审查当前 diff 后运行 `forge review pass`（HARD stop，非提醒）")
	}

	// Review-snapshot consistency (task-complete hard gate): review pass binds
	// (ReviewedHeadCommit, ReviewedChangeHash); here we recompute SourceChangesSince(ReviewedHeadCommit)
	// and compare — a mismatch means source code changed after review → reject and force re-review
	// (review-fix-re-review loop, no longer relying on agent self-discipline). Orthogonal to the
	// ReviewPassed hard prerequisite above — that one rejects 'never reviewed', this one rejects
	// 'reviewed but code changed since'; together they form the complete loop.
	// ReviewedHeadCommit=="" → commit-then-review flow (clean working tree at review time, empty hash)
	// or old-state compatibility, skip this check (keeping only the ReviewPassed hard-prerequisite
	// semantics). Base unreachable (amend/rebase rewrote history so the git object vanished)
	// → fail-open pass + warning: amend is a normal workflow and forced re-review would loop;
	// aligns with the fail-open philosophy of review/stamp.go (strict-when-reachable, lenient-when-not
	// is intentional asymmetry).
	//
	// 审查快照一致性（task-complete 硬门禁）：review pass 时绑定 (ReviewedHeadCommit,
	// ReviewedChangeHash)；此处重算 SourceChangesSince(ReviewedHeadCommit) 比对，不一致说明审查
	// 通过后改了源码 → 拒绝、强制复审（审查-修复-复审闭环，不再靠 agent 自律重审）。与上面的
	// ReviewPassed 硬前置正交——上面拒「没审过」，这里拒「审过但代码又变了」，两者叠加才构成完整闭环。
	// ReviewedHeadCommit=="" → commit-then-review 流（审查时工作区干净，hash 空）或老 state 兼容，
	// 跳过本检查（仅留 ReviewPassed 硬前置语义）。base 不可达（amend/rebase 改写历史致 git 对象消失）
	// → fail-open 放行 + 警告：amend 是正常工作流，强复审会死循环；对齐 review/stamp.go 的 fail-open 哲学
	// （可达则严、不可达则松的非对称是设计本意）。
	if gateID == "task-complete" && state.ReviewPassed && state.CompletedAt == nil && state.ReviewedHeadCommit != "" {
		cur, _, err := review.SourceChangesSince(root, state.ReviewedHeadCommit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[task-complete] 警告：审查基线 commit %s 不可达（%v）——历史可能被改写（amend/rebase），advisory 放行；建议重新 forge review pass 刷新基线\n", state.ReviewedHeadCommit, err)
			// fail-open persisted as a trace (non-blocking, Passed=true means the gate still passes):
			// amend-skip-review is a design tradeoff, but score/dashboard must surface 'passed via
			// fail-open rather than a true review' for after-the-fact traceability — not stderr only.
			//
			// fail-open 落盘留痕（非阻塞，Passed=true 表 gate 仍过）：amend 逃审是设计权衡，但
			// score/dashboard 必须能照出「靠 fail-open 而非真复审通过」，事后可追溯，不能只 stderr。
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameReviewSnapshot,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  fmt.Sprintf("fail-open: 审查基线 %s 不可达（%v）——amend/rebase 致历史改写，放行未重审", state.ReviewedHeadCommit, err),
			})
		} else if cur != state.ReviewedChangeHash {
			return nil, GateBlocked("task-complete 拒绝：审查通过后检测到源码变更（基线 HEAD=%s）。HARD stop——请重新派只读子 agent 审查当前代码，再用 `forge review pass --note \"<复审结论>\"` 刷新审查基线（基线有源码变更时裸 `forge review pass` 会被拒：须 --note 记复审结论或 --acknowledge-changes 自我承担并留 self-refresh 审计）", state.ReviewedHeadCommit)
		}
	}

	// test-coverage backstop hard prerequisite (task-complete): fills the gap left by the
	// task-verify advisory — under advisory semantics an agent self-report is enough (eval
	// evidence: 0/19, 0/25 coverage still passed complete; test-discipline loaded 0 times).
	// Here we hard-block 'large uncovered change, zero assertions': ≥3 changed source files
	// WITHOUT paired tests and zero assertions anywhere = slam-dunk corrupt success (large
	// change with no verification trail). Partial coverage (<3 files missing tests, fudge
	// factor, aligned with the spirit of Sonar's <20-line exemption) or 'has assertions but
	// 0 paired coverage' (tests live elsewhere / refactor scenarios) still passes as
	// advisory — avoid false positives. The threshold counts MISSING files (matching the
	// testCoverageHardGateThreshold doc), not all changed source files — otherwise a
	// well-tested change with one missing file would be hard-blocked and the BLOCKED text
	// would lie.
	// escape (per-task override / FORGE_TEST_COVERAGE) is handled inside CheckTestCoverage which
	// returns ok=true, naturally passing here (downgrade evidence to Weak with a trace).
	// Plan B: BLOCKED messages reuse formatMissing's skill routing — under advisory semantics
	// it has no effect (0 triggers), but under blocking semantics the agent must address it to
	// pass (skill driving relies on the advisory→blocking shift, not on a new mechanism).
	// The assertion signal reuses scoring.CollectAssertionDensity (already injected into
	// EvaluateInput; taskpipeline→scoring is one-way with no cyclic dependency). checklog records
	// the final state — superseding the task-verify record (the agent may have added tests between
	// the two gates; Latest should reflect coverage at task-complete for score/trace).
	//
	// test-coverage 兜底硬前置（task-complete）：补 task-verify advisory 的缺口——advisory
	// 语境下 agent 自述即过（eval 证据：0/19、0/25 覆盖照过 complete，0 次加载 test-discipline）。
	// 此处对「大面积未覆盖且零断言」硬阻断：≥3 个改动源文件无配对测试、且全仓零断言
	// = corrupt success 铁证（大改无任何验证痕迹）。部分覆盖（缺测试文件 <3 个，
	// fudge factor，对齐业界 Sonar <20 行豁免精神）或「有断言但 0 配对覆盖」（测试在
	// 别处/重构场景）仍 advisory 放行——避免误伤。阈值按「无配对测试的文件数」计
	// （与 testCoverageHardGateThreshold 文档一致），而非全部改动源文件数——否则改 3
	// 文件只缺 1 个测试也会被硬阻断，且 BLOCKED 文案说谎。
	// escape（per-task override / FORGE_TEST_COVERAGE）由 CheckTestCoverage 内部返回 ok=true
	// 处理，此处天然放行（验证类逃生，降 evidence Weak 留痕；重证据任务按 2026-08 证据
	// 缩放豁免，见 checklog.EscapeDowngradedStrength）。
	// 方案 B：BLOCKED 消息复用 formatMissing 的 skill 路由——advisory 语境失效（0 触发），
	// blocking 语境下 agent 必须处理才能过（skill 驱动靠 advisory→blocking 转变，非新机制）。
	// 断言信号复用 scoring.CollectAssertionDensity（已注入 EvaluateInput；taskpipeline→scoring
	// 单向无循环依赖）。checklog 记最终态——覆盖 task-verify 的记录（agent 可能在两 gate 间补了
	// 测试，Latest 应反映 task-complete 时覆盖状态供 score/trace）。
	if gateID == "task-complete" && state.CompletedAt == nil {
		ok, missing, total := CheckTestCoverage(root, state)
		recordAudit(root, &checklog.Entry{
			Check:   CheckNameTestCoverage,
			Passed:  ok,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  testCoverageDetail(ok, missing),
		})
		if !ok && len(missing) > 0 {
			assertN, _ := scoring.CollectAssertionDensity(root, state.Branch, state.HeadCommit)
			if testCoverageShouldBlock(len(missing), assertN) {
				return nil, GateBlocked("task-complete 拒绝（HARD stop）：改了 %d 个源文件其中 %d 个无配对测试且零断言（assertN=0）——corrupt success 风险（大改无任何验证痕迹）。%s", total, len(missing), formatMissing(missing))
			}
			fmt.Fprintf(os.Stderr, "%s%s\n", GateAdvisory("[task-complete] "), formatMissing(missing))
		}
	}

	// docs-consistency advisory (task-complete): scans backtick-quoted forge command references
	// in the user project's README; on drift, emits a stderr reminder + checklog record without
	// blocking the gate. Earlier than the CI guard — surfaces at local complete time rather than
	// waiting for CI after push. Detection lives in internal/docsconsistency (cli init registers
	// the command-tree callback to break the cycle). Passed=true means the gate still passes; the
	// trace preserves the drift signal.
	//
	// docs-consistency advisory (task-complete)：扫用户项目 README 的反引号 forge 命令引用，
	// drift 时 stderr 提醒 + checklog 记录，不阻塞 gate。比 CI 守卫更早——本地 complete 时
	// 就发现，不用等 push 后 CI 才报。检测逻辑在 internal/docsconsistency（cli init 注册
	// 命令树回调打破循环）。Passed=true 表 gate 仍通过，trace 保留 drift 信号。
	if gateID == "task-complete" && state.CompletedAt == nil {
		if drifted := docsconsistency.DriftedInProject(root); len(drifted) > 0 {
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameDocsConsistency,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  "docs drift: " + strings.Join(drifted, ", "),
			})
			fmt.Fprintf(os.Stderr, "%s文档一致性 drift——README 反引号引用了不存在的 forge 命令：%s（提交前修复，详见 skills/docs-consistency-guard%s）\n", GateAdvisory("[task-complete] "), strings.Join(drifted, ", "), docsconsistency.StaleBinaryHint())
		}

		// Behavior-surface advisory: the doc guards only cover command/flag
		// REFERENCES, never behavioural prose — so a diff touching the
		// user-visible behaviour surface (init/sync/uninstall, agent bridges,
		// instruction generators, protocol/registry) can ship stale README/
		// homepage descriptions without tripping any guard (observed 2026-08:
		// the user-level-assets rewrite left "forge init creates .forge/" in
		// the README until the user noticed). Remind at complete time, when
		// the diff is known. Advisory only.
		//
		// 行为面 advisory：文档守卫只覆盖命令/flag【引用】，从不覆盖行为【描述】
		// ——触及用户可见行为面（init/sync/uninstall、agent bridge、指令生成器、
		// protocol/registry）的 diff 可以让 README/homepage 的过时描述不穿任何
		// 守卫上线（2026-08 实证：user-level-assets 重构后 README 仍写
		// "forge init 创建 .forge/"，直到用户发现）。在 complete 时（diff 已知）
		// 提醒。仅 advisory。
		// taskChangedFiles is needed twice below (behavior surface + goal↔output match) —
		// compute once: it spawns several git subprocesses.
		//
		// taskChangedFiles 下面要用两次（行为面 + 目标↔产出匹配）——算一次：它会起多个
		// git 子进程。
		changedFiles := taskChangedFiles(root, state)
		if surface := behaviorSurfaceHits(changedFiles); len(surface) > 0 {
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameDocsConsistency,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  "behavior surface: " + strings.Join(surface, ", "),
			})
			fmt.Fprintf(os.Stderr, "%s行为面变更（%s）——文档守卫只覆盖命令引用不覆盖行为描述，提交前请确认 README/homepage/插件文档与新行为一致\n", GateAdvisory("[task-complete] "), strings.Join(surface, ", "))
		}

		// Commit-ordering advisory: uncommitted changes at complete time mean the
		// documented order (gates → git commit → forge task complete) was inverted.
		// Advisory only — complete still succeeds; the point is to surface the
		// ordering slip at the exact moment it can still be repaired (before the
		// active task ref is cleared), not to punish. Fail-open when the git probe
		// is not judgeable (non-repo, probe failure).
		//
		// 提交顺序 advisory：complete 时工作区还有未提交变更 = 文档规定的顺序
		// （三门禁 → git commit → forge task complete）被倒置。仅 advisory——
		// complete 照常成功；意义在把顺序滑落在还能补救的精确时刻（active task
		// ref 清空之前）照出来，而非惩罚。git 探测不可判定时 fail-open（非仓库/
		// 探测失败）。
		if IsGitRepo(root) {
			if n, determinable := uncommittedChanges(root); determinable && n > 0 {
				recordAudit(root, &checklog.Entry{
					Check:   CheckNameUncommittedAtComplete,
					Passed:  true,
					Checked: true,
					Level:   checklog.LevelAdvisory,
					TaskRef: state.TaskRef,
					Detail:  fmt.Sprintf("%d uncommitted changes in working tree at task complete", n),
				})
				fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[task-complete] 工作区有 %d 处未提交变更——协议顺序：三门禁通过 → git commit → forge task complete（complete 会清空 active task ref，之后的源码写入脱离任务追踪）", n))
			}
		}

		// Branch-merged advisory: completing a task whose feature branch has not been
		// merged into the mainline means 'complete' but not 'delivered'. Skipped when
		// the branch is empty or IS the mainline; fail-open when the repo/mainline ref
		// is not judgeable (git error, no main/master ref). Advisory only.
		//
		// 分支归属 advisory：feature 分支尚未合入主干就完成任务 = 「完成」不等于
		// 「交付」。分支为空或本身就是主干时跳过；仓库/主干 ref 不可判定时
		// fail-open（git 出错、无 main/master ref）。仅 advisory。
		if state.Branch != "" && state.Branch != "main" && state.Branch != "master" && IsGitRepo(root) {
			if mainline := resolveMainlineRef(root); mainline != "" {
				if merged, determinable := branchMergedInto(root, state.Branch, mainline); determinable && !merged {
					recordAudit(root, &checklog.Entry{
						Check:   CheckNameBranchUnmerged,
						Passed:  true,
						Checked: true,
						Level:   checklog.LevelAdvisory,
						TaskRef: state.TaskRef,
						Detail:  fmt.Sprintf("branch %s not merged into %s at task complete", state.Branch, mainline),
					})
					fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[task-complete] 任务分支 %s 尚未合入主干 %s——完成不等于交付（合入后再算交付）", state.Branch, mainline))
				}
			}
		}

		// Goal↔output coarse-match advisory: the task title and the changed files share
		// no keyword at all — a smell of delivering the wrong content. Coarse by design
		// (ASCII words >=4 chars from the title vs path-segment tokens of changed files
		// and PlanScope globs; CJK title words are skipped, no segmentation dependency).
		// Skipped when the title yields no keywords or no files changed. Advisory only —
		// false positives must be absorbed, never block (宁缺毋滥).
		//
		// 目标↔产出粗匹配 advisory：任务标题与实改文件零关键词交集——交付内容可能有误
		// 的信号。刻意粗粒度（标题取 >=4 字符 ASCII 词，对比变更文件与 PlanScope glob
		// 的路径 segment token；中文词跳过，不引入分词依赖）。标题切不出关键词或无
		// 变更文件时跳过。仅 advisory——误报必须被吸收，永不阻断（宁缺毋滥）。
		if goalWords := goalKeywords(state.Summary); len(goalWords) > 0 && len(changedFiles) > 0 {
			if !hasIntersection(goalWords, pathSegmentKeywords(changedFiles, state.PlanScope)) {
				recordAudit(root, &checklog.Entry{
					Check:   CheckNameGoalOutputMismatch,
					Passed:  true,
					Checked: true,
					Level:   checklog.LevelAdvisory,
					TaskRef: state.TaskRef,
					Detail:  fmt.Sprintf("goal %q shares no keyword with changed files", state.Summary),
				})
				fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[task-complete] 任务目标（%s）与变更文件无明显关联——确认交付内容", state.Summary))
			}
		}
	}

	// auto gate: run the actual checks.
	//
	// auto gate：跑实际检查
	if gate.Auto {
		result, err := runAutoChecks(root, gateID, state)
		if err != nil {
			return nil, fmt.Errorf("auto-check failed: %w", err)
		}
		return result, nil
	}

	// Non-auto gate: simply mark it passed.
	// The actual work is done by the AI agent following SKILL.md guidance.
	//
	// 非 auto gate：仅标记为通过
	// 实际工作由 AI agent 经 SKILL.md 指引完成

	// Work-activity check for non-auto gates: real work between gates is required to pass.
	// Skipped for: completed tasks (re-check) and the last gate (no work phase afterwards).
	// Note: this check is intentionally NOT skipped after an auto gate. Under the 3-gate
	// pipeline task-verify immediately follows the auto gate task-implement, and the
	// implement→verify stretch is exactly the interval where read-before-edit must be enforced.
	// Skipping after auto was the old rule from the 5-gate era and would make this check
	// ineffective under the 3-gate flow (activity would never run).
	//
	// 非 auto gate 的工作活动检查：gate 之间必须有真实工作才能通过。
	// 跳过：已完成 task（复检）+ 最后一个 gate（之后无工作阶段）。
	// 注意：此处故意不在 auto gate 之后跳过本检查。3-gate 流水线下 task-verify
	// 紧跟 auto gate task-implement，implement→verify 这段正是必须强制
	// read-before-edit 的区间。auto gate 之后跳过是 5-gate 时代的旧规则，会让
	// 本检查在 3-gate 流程下失效（activity 永不运行）。
	if !gate.Auto && state.CompletedAt == nil && len(state.History) > 0 && !isLastGate(gateID) {
		// Work activity is measured across the whole task span (since task start), not since the
		// previous gate. Under the 3-gate pipeline the previous gate (task-implement) is auto and
		// instantaneous; measuring 'since previous gate' would show zero activity even if the
		// agent has done substantial work beforehand.
		//
		// 工作活动按整个 task 跨度计量（自 task 起算），而非自上一 gate 起。
		// 3-gate 流水线下前一 gate（task-implement）是 auto 且瞬时完成，若按
		// 「自上一 gate 起」会看到零活动，即便 agent 此前已做大量工作。
		since := state.StartedAt

		if state.TaskRef != "" && !getDisableWorkActivity(state) {
			reads, edits, rerr := toolusage.ReadEditCounts(root, state.TaskRef, since)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "[forge] warning: activity check failed: %v\n", rerr)
			}
			// Exploration axis (2026-08-23 doc-implementation drift fix): Grep/Glob
			// count toward "was there real work between gates" (CLAUDE.md's error
			// table advises exploring with Read/Grep/Glob; before this, a
			// pure-exploration stretch counted as zero work). Strictly separate
			// from read-before-edit below: browsing matches never substitutes for
			// reading the file you edit — that check stays Read-only.
			//
			// 探索轴（2026-08-23 文档-实现漂移修复）：Grep/Glob 计入「门禁间有无
			// 真实工作」（CLAUDE.md 错误表建议用 Read/Grep/Glob 探索；此前纯探索
			// 段落被计为零工作）。与下方 read-before-edit 严格分离：浏览匹配绝不
			// 替代「读过要改的文件」——那个检查保持 Read-only。
			explores, eerr := 0, error(nil)
			if rerr == nil { // rerr!=nil already decided the pass — skip the second toollog load (m4)
				explores, eerr = toolusage.ExploreCounts(root, state.TaskRef, since)
				if eerr != nil {
					fmt.Fprintf(os.Stderr, "[forge] warning: explore count failed: %v\n", eerr)
				}
			}
			if rerr == nil && reads+edits+explores > 0 {
				// toollog has data — when anything was EDITED, require at least one Read:
				// the agent must understand code before editing it. 'Edit without read' is
				// exactly the failure mode to catch. A pure-exploration stretch (edits==0,
				// explores>0) passes without a Read — there is nothing edited that needed
				// reading first. Edit-heavy work is allowed; the read/edit ratio is
				// reflected by scoring (scope / activity) and is not gated on — a strict
				// ratio would reject normal edit-heavy tasks. The old read-check WARN has
				// been demoted to the Red Flags text in forge-quality as a layered
				// noise-reduction measure.
				//
				// toollog 有数据——发生过任何编辑时，至少要求一次 Read：agent 改
				// 代码前必须先理解它。「只改不读」就是要拦的失败模式。纯探索段落
				// （edits==0、explores>0）无需 Read 即放行——没有编辑过需要先读的
				// 东西。允许 edit-heavy 工作；read/edit ratio 由评分（scope /
				// activity）反映，不当 gate——严格的 ratio 会拦掉正常的 edit-heavy
				// 任务。旧的 read-check WARN 按分层降噪处理已下沉到 forge-quality
				// 的 Red Flags 文本。
				if edits > 0 && reads == 0 {
					// race recovery: Reads sent concurrently with `forge task start` may be
					// logged under the previous task's ref (the active ref switches only after
					// task start commits) and/or carry timestamps earlier than StartedAt — both
					// would exclude them from ReadEditCounts(taskRef, StartedAt). Within the
					// grace window we re-count Reads across all tasks; if any nearby Read
					// happened, treat the agent as having read before edit and pass. Only when
					// the grace window is also empty do we hard-block (a true edit-without-read).
					// The stderr note makes the recovery visible.
					//
					// race 恢复：与 `forge task start` 并发发出的 Read 可能被记到前一
					// task 的 ref 下（active ref 在 task start 提交后才切换），和/或
					// 时间戳早于 StartedAt——两者都会让它从 ReadEditCounts(taskRef,
					// StartedAt) 中排除。在 grace window 内跨所有 task 重计 Read；只要
					// 附近有 Read 发生，就视作 agent 改前读过，判满足。仅在 grace
					// window 也空时硬拦（真正的 edit-without-read）。stderr 备注让恢复
					// 过程可见。
					if grace, gerr := toolusage.ReadEditCountsGraceWindow(root, since, taskStartReadGraceWindow); gerr != nil {
						fmt.Fprintf(os.Stderr, "[forge] warning: grace read check failed: %v\n", gerr)
					} else if grace > 0 {
						fmt.Fprintf(os.Stderr, "[forge] note: read-before-edit satisfied via grace window (%d nearby Read(s) logged outside this task — task-start/Read race)\n", grace)
					} else {
						return nil, GateBlocked(
							"gate %q cannot pass without reading any code during this task (edits=%d). "+
								"HARD stop, not a reminder — Read the file(s) you edit, then re-run `forge task gate %s`",
							gateID, edits, gateID,
						)
					}
				}
			} else if rerr == nil {
				// (rerr != nil falls through as a pass — the historical error behavior:
				// a broken toollog must not hard-block the gate on a read error.
				// Explore-count errors (eerr) degrade the explore axis to zero but do
				// not reroute the branch: reads/edits still decide.)
				//
				// （rerr != nil 时落空放行——历史错误行为：toollog 读取故障不得因
				// 读错误硬拦门禁。探索计数错误（eerr）仅把探索轴降为零，不改道：
				// 仍由 reads/edits 决定。）
				//
				// toollog has no entries for this task. Before enforcing, distinguish two
				// cases that both surface as zero counts:
				//  (a) telemetry channel missing: the host's PostToolUse dispatch is not
				//      wired (e.g. kimi) — toollog file absent/empty AND no hook-dispatched
				//      checklog entries (ToolName set) for this task. Counts are then
				//      structurally 0, so the hard gate is a 100% false positive and
				//      degrades to advisory (with an audit trail).
				//  (b) telemetry alive but genuinely no calls — enforce via the checklog
				//      fallback as before.
				// Only hook-dispatched entries (ToolName != "") count for signal (a):
				// gate-written audit entries carry no ToolName, so the advisory audit below
				// does not flip the signal on a re-run (idempotent). The determination must
				// also complete BEFORE recordAudit — writing first would make checklog
				// non-empty and mask telemetry loss.
				//
				// toollog 无本任务条目。强制前先区分两种都表现为零计数的情形：
				//  (a) 遥测通道缺失：host 的 PostToolUse 分发未接（如 kimi）——toollog
				//      文件缺失/为空 且 checklog 中本任务无 hook 分发条目（带 ToolName）。
				//      此时计数恒为 0，硬门禁是 100% 误报，降级为 advisory（落审计留痕）。
				//  (b) 遥测在工作但确实无调用——照旧走 checklog 回退强制。
				// 信号 (a) 只数 hook 分发条目（ToolName != ""）：gate 写的审计条目不带
				// ToolName，故下方 advisory 审计不会在重跑时翻转信号（幂等）。判定也必须
				// 在 recordAudit 之前完成——先写会让 checklog 变非空，掩盖遥测缺失。
				toollogHas := toolusage.ToollogHasData(root)
				taskEntries, lerr := checklog.LoadForTask(root, state.TaskRef)
				if lerr != nil {
					fmt.Fprintf(os.Stderr, "[forge] warning: checklog load failed: %v\n", lerr)
				}
				hookEntries := 0
				for _, e := range taskEntries {
					if e.ToolName != "" {
						hookEntries++
					}
				}
				// Session-scoped cross-check (code-review 2026-08): toollog.jsonl lives in the
				// agent-writable DataDir — deleting it fabricates signal (a) for a fresh task
				// with no entries yet. But a session whose PostToolUse dispatch works
				// accumulates hook-dispatched entries across tasks; any such entry proves
				// telemetry is alive and the degrade must not fire. kimi-style hosts
				// (dispatch never wired) have zero such entries, so the degrade still
				// triggers there. Two pitfalls, both caught in review:
				//  - full scan, NOT LatestByCheckForSession: that map folds to latest-per-
				//    check, and gate audit entries (ToolName="") written after hook entries
				//    would mask them, resurrecting the forged degrade in a clean session.
				//  - empty state.SessionID (legacy/kimi): match empty-session entries ONLY.
				//    Session-filtering treats "" as "no filter", which in a mixed-host
				//    project would count Claude's entries as kimi's telemetry and
				//    re-introduce the 100% false-positive BLOCK this degrade exists to fix.
				//
				// session 级交叉验证（code-review 2026-08）：toollog.jsonl 在 agent 可写的
				// DataDir——删掉它即可为一个还没有条目的新任务伪造信号 (a)。但分发正常的
				// session 会跨任务累积 hook 分发条目；任一此类条目即证明遥测存活，不得
				// 降级。kimi 类 host（分发从未接通过）无此类条目，降级照常触发。两个坑
				// 均为复审所擒：
				//  - 全量扫描而非 LatestByCheckForSession：那个 map 按 check 折叠到最新
				//    一条，gate 审计条目（ToolName=""）写在 hook 条目之后会遮蔽它们，
				//    让干净 session 里的伪造降级复活。
				//  - state.SessionID 为空（legacy/kimi）：只认空 session 条目。session
				//    过滤把 "" 当「不过滤」，混合 host 项目里会把 Claude 的条目误算成
				//    kimi 的遥测，让本降级要消除的 100% 误 BLOCK 复活。
				sessionAlive := false
				if all, serr := checklog.LoadAll(root); serr == nil {
					for i := range all {
						e := &all[i]
						if e.ToolName == "" {
							continue
						}
						if e.SessionID == "" || (state.SessionID != "" && e.SessionID == state.SessionID) {
							sessionAlive = true
							break
						}
					}
				}
				if !toollogHas && lerr == nil && hookEntries == 0 && !sessionAlive {
					recordAudit(root, &checklog.Entry{
						Check:   CheckNameTelemetryMissing,
						Passed:  true,
						Checked: true,
						Level:   checklog.LevelAdvisory,
						TaskRef: state.TaskRef,
						Detail:  "telemetry unavailable: toollog empty and zero hook-dispatched checklog entries for this task — work-activity enforcement skipped (host hook dispatch not wired)",
					})
					fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[%s] telemetry unavailable（toollog 与 checklog 均无本任务 hook 数据，host hook 分发未通）——work-activity 强制跳过，本次放行不代表已验证工作活动", gateID))
				} else {
					// toollog is empty (old projects with no auto-compile logs) — fall back to checklog.
					//
					// toollog 为空（老项目无 auto-compile 日志）——回退到 checklog。
					activity, err := checklog.WorkActivity(root, state.TaskRef, since)
					if err != nil {
						fmt.Fprintf(os.Stderr, "[forge] warning: WorkActivity check failed: %v\n", err)
					} else if activity < 1 {
						return nil, GateBlocked(
							"gate %q cannot pass without sufficient work activity during this task (%d tool uses, minimum 1). "+
								"HARD stop, not a reminder — Read files, explore code, or write design notes before advancing",
							gateID, activity,
						)
					}
				}
			}
		} else if state.TaskRef != "" && getDisableWorkActivity(state) {
			// A4: the work-activity gate is bypassed via FORGE_WORK_ACTIVITY=disable.
			// Persist an audit trail — the escape hatch exists for testing/escape use, but its
			// use must be visible.
			//
			// A4：work-activity gate 经 FORGE_WORK_ACTIVITY=disable 绕过。
			// 落审计——逃生舱为测试/escape 而设，但使用必须可见。
			recordAudit(root, &checklog.Entry{
				Check:   checklog.CheckEscapeHatch,
				Passed:  true,
				Checked: true,
				Level:   checklog.LevelWarn,
				TaskRef: state.TaskRef,
				Detail:  "escape-hatch: work-activity gate bypassed (per-task override or FORGE_WORK_ACTIVITY=disable)",
			})
		}
	}

	// test-coverage gate (v0.25 advisory): detects 'tests accompany changes' (CLAUDE.md rule 4);
	// when tests are missing it only emits a stderr reminder + checklog record and no longer
	// blocks the gate — adapting to loop engineering, where adding unit tests is the agent's
	// self-check. CheckTestCoverage is still invoked: scoreTask's fallback reuses its verdict
	// and the reminder content comes from `missing`. The checklog Passed field truthfully
	// reflects the detection result (Passed=false when tests are missing), so forge trace
	// retains the signal; it just is no longer used to block the session.
	// Only task-verify runs this check — task-complete is the last gate (no work phase after).
	//
	// test-coverage gate (v0.25 advisory): 检测「测试伴随变更」（CLAUDE.md rule 4），
	// 缺测试时只 stderr 提醒 + checklog 记录，不再阻塞 gate——适配 loop engineering，
	// 补单测由 agent 自检。CheckTestCoverage 仍调用：scoreTask 的 fallback 复用其判定，
	// 且提醒内容来自 missing。checklog 的 Passed 字段如实反映检测结果（缺测试时
	// Passed=false），让 forge trace 保留信号，只是不再用它阻断会话。
	// 仅 task-verify 跑此检查——task-complete 是最后一个 gate（之后无工作阶段）。
	if gateID == "task-verify" && state.CompletedAt == nil {
		// phase-aware (design 3.2/3.6): infer the design phase from changed files, write it back
		// to state.DesignPhases and persist. This is the loop completion point — downstream
		// phaseKeys→Conclusion.DesignPhases→health.PhasePassRate is populated from it; the review
		// sub-agent reads state.DesignPhases to load the matching references/phase-X.md checklist.
		// Zero friction (path inference, no declaration required). Persist only when phases change
		// to avoid per-verify IO churn. inferDesignPhases previously had zero production callers,
		// leaving the entire loop as dead code (review BUG-1); wiring it here makes it real.
		// taskChangedFiles spawns multiple git subprocesses (testcoverage.go); compute once here.
		// gitignore blind spot: all three git sources use --exclude-standard, so ignored design
		// artifacts like docs/ are invisible → PhaseRequirement cannot be inferred (loop broken at
		// the first link). scanDesignArtifacts reads the filesystem directly to fill the gap, but
		// feeds phase inference only — ScopeDrift still uses a pure git view, preventing historical
		// design files from being mis-counted as 'this-time actual changes' and triggering drift
		// false positives.
		//
		// phase-aware（设计 3.2/3.6）：按改动文件推断设计阶段，写回 state.DesignPhases 并持久化。
		// 回路接通点——下游 phaseKeys→Conclusion.DesignPhases→health.PhasePassRate 据此填充；
		// review 子 agent 读 state.DesignPhases 加载对应 references/phase-X.md checklist。零摩擦
		// （路径推断，不要求声明）。仅 phases 变化时写盘，避免每次 verify 无谓 IO。inferDesignPhases
		// 此前零生产调用致整条回路死代码（review BUG-1），此处接通让它名副其实。
		// taskChangedFiles 跑多个 git 子进程（testcoverage.go），在此块算一次。
		// gitignore 盲区：git 三源都 --exclude-standard，docs/ 等被忽略的设计产物
		// 看不到→PhaseRequirement 推不出（回路断第一环）。scanDesignArtifacts 直接读
		// 文件系统补上，但只喂 phase 推断——ScopeDrift 仍用纯 git 视角，避免历史设计
		// 文件被误算进「本次实改」触发 drift 误报。
		gitChanged := taskChangedFiles(root, state)
		scanned := scanDesignArtifacts(root)
		combined := make([]string, 0, len(gitChanged)+len(scanned))
		combined = append(combined, gitChanged...)
		combined = append(combined, scanned...)
		inferred := inferDesignPhases(combined)
		if !designPhasesEqual(state.DesignPhases, inferred) {
			state.DesignPhases = inferred
			if err := SaveTaskState(root, state); err != nil {
				fmt.Fprintln(os.Stderr, "[task-verify] DesignPhases persist failed:", err)
			}
		}

		ok, missing, _ := CheckTestCoverage(root, state)
		recordAudit(root, &checklog.Entry{
			Check:   CheckNameTestCoverage,
			Passed:  ok,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  testCoverageDetail(ok, missing),
		})
		if !ok {
			// Recurrence-driven hardening (recurrent.go): advisory→hard promotion fires only when
			// BOTH axes hold — the project's testing dimension has gone low ≥threshold times in
			// history (advisory self-discipline proven to fail here) AND this task still has untested
			// source. test-coverage's own escape (FORGE_TEST_COVERAGE / per-task override) is handled
			// inside CheckTestCoverage which returns ok=true, so an escaped task never reaches here;
			// the only ways out of a hardened block are to add the tests (intended) or lower the
			// recurrence floor (FORGE_RECURRENT_HARDEN=disable, no Strength penalty).
			//
			// 复发驱动升硬（recurrent.go）：advisory→hard 仅当两轴皆真才触发——项目 testing 维度历史
			// 低分 ≥阈值次（advisory 自律在此已被证明失效）且本任务仍有未测源码。test-coverage 自身
			// 逃生（FORGE_TEST_COVERAGE / per-task override）由 CheckTestCoverage 内部返回 ok=true 处理，
			// 逃生任务永不进此分支；升硬后唯一出路是真补测试（预期）或下调复发门槛
			// （FORGE_RECURRENT_HARDEN=disable，无 Strength 惩罚）。
			if recurrentHardenEnabled() {
				if cs := loadConclusions(root); dimRecurrent(cs, dimTesting, recurrentThreshold()) && len(missing) > 0 {
					return nil, GateBlocked(`task-verify 拒绝（复发升 HARD stop）：项目 testing 维度已 %d 次低分（达到阈值 %d）——advisory 靠自律在此项目已被证明失效，本次 %d 个源文件仍无配对测试。%s出路：补测试后重跑；或 FORGE_TEST_COVERAGE=disable（降 evidence Weak；重证据任务按证据缩放豁免）；或 FORGE_RECURRENT_HARDEN=disable 回退纯 advisory`, lowDimCounts(cs)[dimTesting], recurrentThreshold(), len(missing), formatMissing(missing))
				}
			}
			fmt.Fprintf(os.Stderr, "%s%s\n", GateAdvisory("[task-verify] "), formatMissing(missing))
		}

		// cross-repo-impact declaration (multi-repo workspace, crossrepo.go): when this
		// repo belongs to a multi-repo workspace, the task must carry an explicit impact
		// declaration — "none" included (declaration forces explicit thought). Advisory by
		// default (fail-open philosophy); protocol cross_repo_impact: required promotes the
		// undeclared case to a hard block. Repos with no multi-repo membership are skipped
		// silently (no log). CheckCrossRepoImpact is excluded from BuildEvidenceChain — an
		// observation of declaration state, not verification evidence.
		//
		// cross-repo-impact 声明（多仓 workspace，crossrepo.go）：本 repo 属于多仓
		// workspace 时，任务必须带显式影响声明——包括 "none"（声明强迫显式思考）。
		// 默认 advisory（fail-open 哲学）；protocol cross_repo_impact: required 把
		// 未声明升级为硬阻断。无多仓成员资格的 repo 静默跳过（不记日志）。
		// CheckCrossRepoImpact 在 BuildEvidenceChain 中排除——它是声明状态的观测，
		// 非验证证据。
		if err := checkCrossRepoImpact(root, state); err != nil {
			return nil, err
		}

		// scope-drift advisory (PlanScope whitelist): when a task declares a planned-change
		// whitelist, detect whether actually-changed source exceeds the declaration. drift =
		// taskChangedFiles (actual state) vs PlanScope (declared state) set difference —
		// analogous to Terraform drift detection (desired vs actual). Purely advisory:
		// change-impact analysis recall is only ~44%, scope is a prediction not a contract, and
		// drift is a normal signal; here we merely make it measurable and reviewable
		// (forge trace / task scope show) — never blocking.
		// deterministic (the gate computes ScopeDrift, the agent cannot fake it). CheckScopeDrift
		// is excluded from BuildEvidenceChain — it is an advisory observation, not 'verification
		// evidence'; counting it would inflate Strength.
		//
		// scope-drift advisory (PlanScope whitelist)：任务声明了计划改动白名单时，检测
		// 实改源码是否超出声明。drift = taskChangedFiles(实改态) vs PlanScope(声明态) 的
		// 差集——对应 Terraform drift detection（desired vs actual）。纯 advisory：变更影响
		// 变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract，drift 是常态信号；
		// 这里只把它从隐性变可度量、可回顾（forge trace / task scope show），绝不阻塞。
		// deterministic（gate 实算 ScopeDrift，agent 无法伪造）。CheckScopeDrift 在
		// BuildEvidenceChain 中被排除——它是 advisory 观测非「验证证据」，计入会虚高 Strength。
		if len(state.PlanScope) > 0 {
			drift := ScopeDrift(gitChanged, state.PlanScope)
			recordAudit(root, &checklog.Entry{
				Check:   checklog.CheckScopeDrift,
				Passed:  len(drift) == 0,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  scopeDriftDetail(drift),
			})
			if len(drift) > 0 {
				// Recurrence-driven hardening (recurrent.go): scope-drift is advisory by design
				// (impact-prediction recall ~44%, hardening would reject half of legitimate changes).
				// Promote to BLOCKED only when BOTH the project is recurrent on scope AND this drift is
				// material (≥severe threshold files) — single-file drift stays advisory even on
				// recurrent projects (a normal prediction miss).
				//
				// 复发驱动升硬（recurrent.go）：scope-drift 设计上 advisory（影响预测召回率 ~44%，硬拦会
				// 拒一半合法改动）。仅当项目 scope 复发 且 本次 drift 实质（≥严重阈值文件）两者皆真时升
				// BLOCKED——单文件 drift 即便在复发项目也保持 advisory（正常预测失误）。
				if recurrentHardenEnabled() && scopeDriftSevere(drift) {
					if cs := loadConclusions(root); dimRecurrent(cs, dimScope, recurrentThreshold()) {
						return nil, GateBlocked(`task-verify 拒绝（复发升 HARD stop）：项目 scope 维度已 %d 次低分（达到阈值 %d）——计划漂移已成系统性问题，本次 %d 个源文件超出 PlanScope 声明。出路：forge task scope add <glob> 收编实改；或 FORGE_RECURRENT_HARDEN=disable 回退纯 advisory`, lowDimCounts(cs)[dimScope], recurrentThreshold(), len(drift))
					}
				}
				fmt.Fprintf(os.Stderr, "%sscope-drift——%d 个源码文件超出 PlanScope 声明（advisory 不阻塞；收编: forge task scope add <glob>）\n", GateAdvisory("[task-verify] "), len(drift))
				for _, f := range drift {
					fmt.Fprintf(os.Stderr, "  ⚠ %s\n", f)
				}
			}
		}

		// cheat-scan (advisory): mechanically detect the AI-cheat patterns enumerated in
		// ScanCheatPatterns (cheatscan.go — the single source of truth for the count and list;
		// currently 7: type-suppression / error-swallow / dead-branch / comment-only-fix /
		// comment-as-debt / phantom-import / path-assumption). The first 4 were
		// previously judged by an LLM sub-agent at code-review time — the LLM re-samples the same
		// diff each round and catches different subsets, which is the source of the 'every review
		// round raises new issues' perception; this scan pulls them into deterministic detection.
		// comment-as-debt catches 'the comment flags a problem but does not solve it'
		// (the anti-pattern at level 0 of the laziness ladder, the root of code rot) — the nudge
		// below spells out the handling path (convert to a forge task or fix it inline) for the
		// agent. Scans task-added lines (+ lines); hits go to checklog:cheat-scan. Purely
		// advisory (heuristics can false-positive — comment-only especially) and never blocking;
		// the trace is left for review verification.
		// CheckCheatScan is excluded from BuildEvidenceChain — it is an observation, not
		// 'verification evidence'; counting it would inflate Strength. The LLM-reviewer accordingly
		// retreats to semantic judgments only (design / architecture / whether mocks are illusory).
		//
		// cheat-scan (advisory)：机械检测 ScanCheatPatterns 枚举的 AI 作弊模式（数量与清单
		// 以 cheatscan.go 为唯一真相源，当前 7 类：type-suppression / error-swallow /
		// dead-branch / comment-only-fix / comment-as-debt / phantom-import / path-assumption）。
		// 前 4 类此前全靠 LLM 子 agent 在 code-review 时判断——LLM 每轮对同一 diff 重新采样
		// 抓不同子集，是「每轮 review 冒新问题」的体感来源；本扫描把它们抽到
		// deterministic。comment-as-debt 抓「注释标识问题但不解决」（懒惰阶梯
		// 反第 0 级，屎山根源）——下方 nudge 把处置路径（转 forge task 或当场修）明确
		// 告诉 agent。扫任务新增行（+ 行），命中记 checklog:cheat-scan。纯 advisory
		// （启发式有假阳性可能——comment-only 尤甚）绝不阻塞，留痕供 review 核查。
		// CheckCheatScan 在 BuildEvidenceChain 中被排除——它是观测非「验证证据」，计入
		// 会虚高 Strength。LLM-reviewer 据此退到只做语义判断（设计/架构/mock 是否幻觉）。
		cheats := ScanCheatPatterns(root, state)
		// findingsDirty 汇总 cheat/unused 两段是否有新指纹入集合，两段之后统一持久化一次。
		//
		// findingsDirty accumulates whether either scan section added new fingerprints;
		// the state is persisted once after both sections.
		findingsDirty := false
		// Same-finding suppression (2026-08 noise audit) — run BEFORE the checklog record so
		// the audit entry carries the fresh/suppressed breakdown (dedupSuffix): the
		// entry stays full-truth (Passed/Detail reflect the actual scan) while repeat FAILs on
		// an unchanged diff become distinguishable from genuinely new hits. The agent-facing
		// advisory below still only renders fresh findings.
		//
		// 同 finding 抑制（2026-08 噪音审计）——先于 checklog 记录执行，使审计条目带上
		// 新发现/被抑制拆分（dedupSuffix）：条目保持全量真相（Passed/Detail 反映当次
		// 真实扫描），同时让重扫同一 diff 的重复 FAIL 与真正的新命中可区分。下方
		// agent 面向的 advisory 仍只渲染新 finding。
		var freshCheats []CheatFinding
		if len(cheats) > 0 {
			cheatKeys := make([]string, len(cheats))
			for i, c := range cheats {
				cheatKeys[i] = cheatFindingKey(c)
			}
			fresh, dirty := filterUnreported(state, cheatKeys)
			findingsDirty = findingsDirty || dirty
			freshSet := keySet(fresh)
			for _, c := range cheats {
				if freshSet[cheatFindingKey(c)] {
					freshCheats = append(freshCheats, c)
				}
			}
		}
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckCheatScan,
			Passed:  len(cheats) == 0,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  cheatScanDetail(cheats) + dedupSuffix(len(cheats), len(freshCheats)),
		})
		if len(cheats) > 0 {
			// 同 finding 抑制（2026-08 噪音审计）：指纹（规则|文件：行）已报告过的 finding 不
			// 再逐条重发——修复后 verify 重试重扫同一 diff，否则会逐字重发（Translate(method)
			// 8 次）。checklog 条目保持全量（审计真相），抑制的只是 agent 面向的重复提醒。
			//
			// Same-finding suppression (2026-08 noise audit): findings whose fingerprint
			// (rule|file:line) was already reported are not re-emitted line by line — a
			// post-fix verify retry re-scans the same diff and would otherwise re-emit
			// them verbatim (Translate(method) 8 times). The checklog entry stays full
			// (audit truth); only the agent-facing repeat nudge is suppressed.
			if len(freshCheats) == 0 {
				fmt.Fprintf(os.Stderr, "%scheat-scan 命中 %d 处疑似 AI 作弊模式%s（advisory 不阻塞）\n", GateAdvisory("[task-verify] "), len(cheats), allReportedNote())
			} else {
				fmt.Fprintf(os.Stderr, "%scheat-scan 命中 %d 处疑似 AI 作弊模式%s（advisory 不阻塞；机械检测供 review 核查）\n", GateAdvisory("[task-verify] "), len(freshCheats), suppressNote(len(cheats)-len(freshCheats)))
				for _, c := range freshCheats {
					loc := c.File
					if c.Line > 0 {
						loc = c.File + ":" + strconv.Itoa(c.Line)
					}
					fmt.Fprintf(os.Stderr, "  ⚠ [%s|%s] %s — %s\n", c.Severity, c.Pattern, loc, c.Snippet)
				}
				// comment-as-debt specific nudge (plan B): commenting a problem ≠ solving it (the
				// anti-pattern at level 0 of the laziness ladder). Spell out the handling path for the
				// agent — convert to a forge task for tracking (intent preserved, gated) or fix it
				// inline. Without this, the agent easily dismisses low-severity comment-as-debt as
				// noise, 'marking it counts as doing it'. Raw string avoids Windows input double-quote
				// corrosion.
				//
				// comment-as-debt 专属 nudge（B 方案）：注释标识问题 ≠ 解决（懒惰阶梯反第 0
				// 级）。把处置路径明确告诉 agent——转 forge task 跟踪（保留意图、被门禁
				// 追踪）或当场修。不加则 agent 易把低 severity 的 comment-as-debt 当噪音
				// 忽略，「标注了就当做了」。raw string 规避 Windows 输入双引号腐蚀。
				debtCount := 0
				for _, c := range freshCheats {
					if c.Pattern == CheatCommentDebt {
						debtCount++
					}
				}
				if debtCount > 0 {
					fmt.Fprintf(os.Stderr, "%s"+`%d 处 comment-as-debt——注释标识问题 ≠ 解决（懒惰阶梯反第 0 级）。处置：当场修掉；或转 forge task start --ref <ref> --title <描述> 跟踪（本任务完结后开）。加载 implementation-discipline skill 按懒惰阶梯归位。advisory 不阻塞。`+"\n", GateAdvisory("[task-verify] "), debtCount)
				}
			}
		}

		// doc-gate advisory (output→re-check loop, docs/design/output-readability-gates.md):
		// when the task changed markdown deliverables but no fresh L2 review is
		// recorded, remind at task-verify — BEFORE complete hard-blocks on it.
		// Enforcement stays at CheckDocGate (task-complete pre-flight); this is
		// the early nudge so the agent fixes L1 and runs the rubric review while
		// still in verify, not after hitting the BLOCKED. Pure advisory.
		//
		// doc-gate advisory（输出→回检循环，docs/design/output-readability-gates.md）：
		// 任务变更了 markdown 产物但无 fresh 的 L2 回检记录时，在 task-verify 提醒——
		// 抢在 complete 硬拦截之前。执法仍在 CheckDocGate（task-complete pre-flight）；
		// 这是提前量 nudge，让 agent 还在 verify 阶段就修 L1、跑 rubric 评审，而不是
		// 撞上 BLOCKED 才回头。纯 advisory。
		if docs := changedMarkdownDocs(root, state); len(docs) > 0 {
			head := GetHeadCommit(root)
			stale := state.DocReview == nil || state.DocReview.ReviewedAt.IsZero() ||
				(state.DocReview.HeadCommit != "" && head != "" && state.DocReview.HeadCommit != head)
			if stale {
				fmt.Fprintf(os.Stderr, "%s"+`文档产物 %d 个（%s）尚无 fresh 的 L2 回检——complete 前须：forge docs lint 修 L1 → 按 code-review-gate/references/rubric-docs.md 评审（产出者不能自检）→ forge task doc-review 记录。advisory 不阻塞，complete 的 doc gate 会拦。`+"\n", GateAdvisory("[task-verify] "), len(docs), strings.Join(docs, ", "))
			}
		}

		// unused-scan (advisory): layer-1 wiring detection — newly-added exported symbols (Go
		// func/type/method, TS export, Rust pub) that no production line in this task references.
		// Catches "implemented but never wired" (Forge's own BUG-1: inferDesignPhases had zero
		// production callers — dead code, surfaced only by review). Unit tests verify the
		// implementation, not the wiring — a broken wire leaves tests green and the feature dead;
		// this scan exposes it mechanically. Advisory (library/reflection/external-consumer exports
		// legitimately have no in-repo caller); never blocks. CheckUnusedScan is excluded from
		// BuildEvidenceChain — an observation, not verification evidence (same as cheat-scan).
		// Layer-2 (referenced but semantically unwired — registered but never instantiated) is not
		// mechanically decidable → stays with the LLM reviewer / code-review-gate.
		//
		// unused-scan (advisory)：层 1 接线检测——本次新增的导出符号（Go func/type/method、
		// TS export、Rust pub）在本任务生产代码里零引用。抓"实现了但没接线"（Forge 自己的
		// BUG-1：inferDesignPhases 零生产调用方——dead code，靠 review 才浮出）。单测验实现
		// 不验接线——接线一断测试照绿、功能已死；本扫描机械暴露。advisory（库/反射/外部消费
		// 的导出合法地无仓内调用方）；绝不阻塞。CheckUnusedScan 在 BuildEvidenceChain 中排除——
		// 观测非验证证据（同 cheat-scan）。层 2（引用了但语义没接通——注册了但从未实例化）
		// 机械不可判 → 仍归 LLM reviewer / code-review-gate。
		unused := ScanUnusedSymbols(root, state)
		// 同 finding 抑制先于 checklog 记录执行（与 cheat-scan 段同一模式）：审计条目带上
		// 新发现/被抑制拆分（dedupSuffix），重扫同一 diff 的重复 FAIL 与真新命中可区分。
		//
		// Same-finding suppression runs BEFORE the checklog record (same pattern as the
		// cheat-scan section): the audit entry carries the fresh/suppressed
		// breakdown (dedupSuffix), so repeat FAILs on an unchanged diff are
		// distinguishable from genuinely new hits.
		var freshUnused []UnusedFinding
		if len(unused) > 0 {
			unusedKeys := make([]string, len(unused))
			for i, u := range unused {
				unusedKeys[i] = unusedFindingKey(u)
			}
			fresh, dirty := filterUnreported(state, unusedKeys)
			findingsDirty = findingsDirty || dirty
			freshSet := keySet(fresh)
			for _, u := range unused {
				if freshSet[unusedFindingKey(u)] {
					freshUnused = append(freshUnused, u)
				}
			}
		}
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckUnusedScan,
			Passed:  len(unused) == 0,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  unusedScanDetail(unused) + dedupSuffix(len(unused), len(freshUnused)),
		})
		if len(unused) > 0 {
			// 同 finding 抑制（同 cheat-scan 段注释）：指纹 规则|文件：行|符号。
			//
			// Same-finding suppression (see the cheat-scan section's comment):
			// fingerprint = rule|file:line|symbol.
			if len(freshUnused) == 0 {
				fmt.Fprintf(os.Stderr, "%s"+`unused-scan 发现 %d 个疑似未接线的导出符号%s（advisory 不阻塞）`+"\n", GateAdvisory("[task-verify] "), len(unused), allReportedNote())
			} else {
				fmt.Fprintf(os.Stderr, "%s"+`unused-scan 发现 %d 个疑似未接线的导出符号%s（advisory 不阻塞；层1机械检测：确认已挂入调用链/registry/路由，或属外部消费者则忽略）`+"\n", GateAdvisory("[task-verify] "), len(freshUnused), suppressNote(len(unused)-len(freshUnused)))
				for _, u := range freshUnused {
					loc := u.File
					if u.Line > 0 {
						loc = u.File + ":" + strconv.Itoa(u.Line)
					}
					fmt.Fprintf(os.Stderr, "  ⚠ [%s] %s — %s\n", u.Kind, loc, u.Symbol)
				}
			}
		}
		// 两段扫描若有新指纹入集合，持久化一次（best-effort；失败最坏下次重报一遍，
		// 优于阻塞 gate）。与 DesignPhases 持久化同款模式。
		//
		// Persist once if either scan added new fingerprints (best-effort; a failure at
		// worst re-reports once next run — better than blocking the gate). Same pattern
		// as the DesignPhases persistence.
		if findingsDirty {
			if err := SaveTaskState(root, state); err != nil {
				fmt.Fprintln(os.Stderr, "[task-verify] reported-findings persist failed:", err)
			}
		}

		// test-capability scan (advisory): when the repo has runnable tests, suggest the agent
		// actually execute them before verify. Supplements task-verify's 'did you run them'
		// dimension — the test-coverage above only checks 'tests accompany changes' (wrote a
		// test ≠ ran a test); this scan checks 'does the repo have runnable tests': yes → emit
		// the recommended command (purely advisory, non-blocking); no → silent. The earlier
		// verify-before-stop.sh Stop hook (forcefully ran the full suite at session end) has
		// since been removed; this advisory scan is the remaining capability signal.
		// Passed is always true — 'the repo has tests'
		// is not itself a verdict; the trace only retains the capability signal.
		// Plan-5 consistency: the test-coverage escape hatch (per-task override or env) must also
		// skip the capability scan — otherwise users on --test-coverage disable still receive the
		// 'repo has tests, consider running them' nag, contradicting the 'I am not doing test
		// discipline' signal. CheckEscapeHatch is already recorded by CheckTestCoverage above;
		// here we only skip the scan + advisory and do not double-record the escape-hatch entry.
		//
		// test-capability scan (advisory): 仓库存在可跑的测试时，建议 agent 过 verify
		// 前实际执行。补 task-verify 的「测过没」维度——上面的 test-coverage 只查「测试
		// 伴随变更」（写了测试≠跑过测试），本扫描查「仓库有没有可跑的测试」：有→给推荐
		// 命令建议执行（纯 advisory 不阻塞）；无→静默。早期 verify-before-stop.sh（Stop
		// hook 实跑全量）已删除，本 advisory 扫描是现存的能力信号。Passed 恒 true——
		// 「仓库有测试」本身不是判定，trace 只保留能力信号。
		// 方案5 一致性：test-coverage 逃生舱（per-task override 或 env）须同时跳过
		// capability 扫描——否则 --test-coverage disable 的用户仍收「仓库有测试，建议跑」
		// nag，与「我不做测试纪律」信号矛盾。CheckEscapeHatch 已由上方 CheckTestCoverage
		// 记录，此处仅跳过扫描+advisory，不重复记 escape-hatch 条目。
		if !escapeDisabled(state, escapeTestCoverage, testCoverageDisableEnv) {
			cap := CheckTestCapability(root)
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameTestCapability,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  cap.Detail(),
			})
			if cap.HasTests {
				fmt.Fprintf(os.Stderr, "%s%s\n", GateAdvisory("[task-verify] "), cap.Advisory())
			}
		}

		// skill-eval advisory: changes touching skills/<name>/ where the skill has an eval case
		// set → suggest running regression. Editing the description invalidates DescHash of the
		// old case set (submit refuses); remind to eval-gen --save to rebuild the baseline. Purely
		// advisory, non-blocking (Passed always true — 'has case set' is not itself a verdict;
		// the trace only retains the signal for agent self-check).
		//
		// skill-eval advisory：变更涉及 skills/<name>/ 且该 skill 有 eval case 集 →
		// 建议跑回归。改 description 会让旧 case 集的 DescHash 失配（submit 拒绝），
		// 提醒先 eval-gen --save 重建基准。纯 advisory 不阻塞（Passed 恒 true——
		// 「有 case 集」本身非判定，trace 只留信号让 agent 自检）。
		if affected := skillEvalAffected(gitChanged); len(affected) > 0 {
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameSkillEval,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  formatSkillEvalAdvisory(affected),
			})
			fmt.Fprintf(os.Stderr, "%s%s\n", GateAdvisory("[task-verify] "), formatSkillEvalAdvisory(affected))
		}

		// skill-decisions dual-tier (component B: advisory promoted to guardrail):
		//  - guardrail (blocking): a skill whose SKILL.md (behavioral contract) was changed must
		//    add a new decisions.md entry in this task, otherwise BLOCKED. SKILL.md is the behavior
		//    definition (Use when/SKIP/flow); changing it must record the why (dogfood iron rule:
		//    advisory 0 triggers, must be blocking).
		//  - advisory (non-blocking): changes to auxiliary resources (scripts/references/cases)
		//    only remind — trivial changes concentrate in auxiliary resources, keeping advisory
		//    to avoid false positives.
		// Decision anchor: new `## [d-` entries in decisions.md from base..HEAD (a deterministic
		// signal, not semantic guessing). base = state.HeadCommit. escape (per-task override /
		// FORGE_SKILL_DECISIONS) → guardrail downgrades to advisory + CheckEscapeHatch (Weak
		// ceiling). fail-open: empty/unreachable base does not block.
		//
		// skill-decisions 双档（B 组件：advisory 升 guardrail）：
		//  - guardrail（阻断）：改 SKILL.md（行为契约）的 skill 必须在本 task 新增 decisions.md
		//    条目，否则 BLOCKED。SKILL.md 是行为定义（Use when/SKIP/流程），改它必留 why（dogfood
		//    铁律：advisory 0 触发，必须 blocking）。
		//  - advisory（不阻断）：改辅助资源（scripts/references/cases）只提醒——trivial 改动
		//    集中在辅助资源，保持 advisory 不误伤。
		// 判定锚点：decisions.md base..HEAD 新增 `## [d-` 条目（确定信号，非语义猜测）。base=
		// state.HeadCommit。escape（per-task override / FORGE_SKILL_DECISIONS）→ guardrail 降
		// advisory + CheckEscapeHatch（Weak ceiling）。fail-open：base 空/不可达不阻断。
		if blocking := skillDecisionsBlockingAffected(gitChanged); len(blocking) > 0 {
			if escapeDisabled(state, escapeSkillDecisions, envSkillDecisions) {
				recordAudit(root, &checklog.Entry{
					Check:   checklog.CheckEscapeHatch,
					Passed:  true,
					Checked: true,
					Level:   checklog.LevelWarn,
					TaskRef: state.TaskRef,
					Detail:  `escape-hatch: skill-decisions guardrail bypassed (per-task override or FORGE_SKILL_DECISIONS=disable): ` + strings.Join(blocking, ", "),
				})
				// escape-copy specialization: the blocking set covers skills whose SKILL.md
				// (behavioral contract) changed, not auxiliary resources — do not reuse
				// formatSkillDecisionsAdvisory (that is the auxiliary-resource / trivial-scenario
				// copy; semantic mismatch).
				//
				// escape 文案专用：blocking 集是改了 SKILL.md（行为契约）的 skill，非辅助资源——
				// 不能复用 formatSkillDecisionsAdvisory（那是辅助资源/trivial 场景文案，语义错位）。
				fmt.Fprintf(os.Stderr, "%s%s\n", GateAdvisory("[task-verify] "), fmt.Sprintf(skillDecisionsEscapeAdvisoryFmt, strings.Join(blocking, ", ")))
			} else {
				// Three buckets: recorded (verified, decision recorded) / unrecorded (not recorded
				// → BLOCKED) / failopen (base unreachable, check skipped). The pass-path checklog
				// Detail only claims the recorded ones; fail-open is annotated separately — avoid
				// 'concatenating the entire blocking list and claiming decisions were recorded'
				// misleading audit (structured-log accuracy: when base is unreachable, sources 2/3
				// of taskChangedFiles still capture SKILL.md changes, so blocking is non-empty but
				// recorded was not actually verified; audit must distinguish 'truly recorded' vs
				// 'slipped through via fail-open').
				//
				// 分三类：recorded（真验证记了决策）/ unrecorded（未记→BLOCKED）/ failopen（base 不可达
				// 跳过校验）。通过路径 checklog Detail 只宣称 recorded 的，fail-open 单独标注——避免
				// 「拼整个 blocking 列表宣称已记决策」误导 audit（结构化日志准确性：fail-open 是 base
				// 不可达时 taskChangedFiles 的源2/3 仍捕获 SKILL.md 改动，blocking 非空但 recorded
				// 未真验证，audit 须能区分「真记」vs「fail-open 溜过」）。
				var recorded, unrecorded, failopenSkills []string
				for _, sk := range blocking {
					rec, fo := skillDecisionsRecorded(root, state.HeadCommit, sk)
					if fo {
						failopenSkills = append(failopenSkills, sk)
						continue
					}
					if rec {
						recorded = append(recorded, sk)
					} else {
						unrecorded = append(unrecorded, sk)
					}
				}
				if len(unrecorded) > 0 {
					// BLOCKED must be persisted — let score/dashboard/audit surface that
					// 'skill-decisions blocked this' (aligned with test-coverage recording
					// checklog before BLOCKED). stderr is emitted at runtime, but the persisted
					// evidence is indispensable; otherwise 'why did the task stall at verify
					// multiple times' has no signal.
					//
					// BLOCKED 必落盘——让 score/dashboard/audit 照出「skill-decisions 阻断过」
					//（对齐 test-coverage BLOCKED 前先记 checklog）。运行时有 stderr，但落盘证据
					// 不可缺，否则「task 为何多次卡在 verify」无信号。
					blockedDetail := fmt.Sprintf(`guardrail BLOCKED：改了 %s 的 SKILL.md（行为变更）但本 task 未在 decisions.md 新增决策`, strings.Join(unrecorded, ", "))
					if len(failopenSkills) > 0 {
						// Mixed scenario (some unrecorded + some base-unreachable): the BLOCKED
						// detail appends the fail-open skills so audit sees the full picture at
						// once (no need to wait for unrecorded to be fixed and re-run to see
						// fail-open on the pass path).
						//
						// 混合场景（部分未记 + 部分 base 不可达）：BLOCKED detail 补 fail-open skill，
						// 让 audit 一次看全（不必等修了 unrecorded 重跑才在通过路径见 fail-open）。
						blockedDetail += `；` + strings.Join(failopenSkills, ", ") + ` fail-open 跳过校验（base 不可达）`
					}
					recordAudit(root, &checklog.Entry{
						Check:   CheckNameSkillDecisions,
						Passed:  false,
						Checked: true,
						TaskRef: state.TaskRef,
						Detail:  blockedDetail,
					})
					return nil, GateBlocked(`task-verify 拒绝（HARD stop）：改了 skill %s 的 SKILL.md（行为变更）但本任务未在 decisions.md 新增决策——跑 'forge skills decide --skill <name> --outcome <accept|reject> --diagnosis <为何改> --revision <改了啥> --evidence <依据>' 记录四元组（让下一轮 agent 理解 why）；trivial 改动用 'forge task override --skill-decisions disable' 逃生舱（降 evidence 到 Weak；重证据任务按证据缩放豁免）`, strings.Join(unrecorded, ", "))
				}
				// Pass path: Detail honestly distinguishes recorded (truly recorded) vs failopen
				// (base unreachable, not verified).
				//
				// 通过路径：Detail 诚实区分 recorded（真记）vs failopen（base 不可达未验证）。
				detail := `skill-decisions guardrail 满足`
				if len(recorded) > 0 {
					detail += `：` + strings.Join(recorded, ", ") + ` 已在本 task 记决策`
				}
				if len(failopenSkills) > 0 {
					if len(recorded) > 0 {
						detail += `；`
					} else {
						detail += `：`
					}
					detail += strings.Join(failopenSkills, ", ") + ` fail-open 跳过校验（base 不可达，未真验证 recorded）`
				}
				recordAudit(root, &checklog.Entry{
					Check:   CheckNameSkillDecisions,
					Passed:  true,
					Checked: true,
					TaskRef: state.TaskRef,
					Detail:  detail,
				})
			}
		}
		if advisorySkills := skillDecisionsAdvisoryAffected(gitChanged); len(advisorySkills) > 0 {
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameSkillDecisions,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  formatSkillDecisionsAdvisory(advisorySkills),
			})
			fmt.Fprintf(os.Stderr, "%s%s\n", GateAdvisory("[task-verify] "), formatSkillDecisionsAdvisory(advisorySkills))
		}

		// acceptance advisory (spec-as-gate): the task registered acceptance criteria
		// (task start --accept) but they are not all passed → remind to run
		// 'forge task verify-acceptance' to turn the spec into actual-run evidence. Purely
		// advisory, non-blocking, no return error. Key: this only **reads the last result on
		// state to remind** and never records a CheckNameAcceptance entry — that entry is
		// exclusive to verify-acceptance's real execution (deterministic, unforgeable); a gate
		// that does not run the command must not falsely claim it ran.
		//
		// acceptance advisory（spec-as-gate）：任务登记了验收标准（task start --accept）
		// 但未全部通过 → 提醒先跑 'forge task verify-acceptance' 把 spec 变成实跑证据。
		// 纯 advisory 不阻塞、不 return error。关键：这里**只读 state 上次结果提醒**，
		// 绝不记 CheckNameAcceptance 条目——该条目专属于 verify-acceptance 的真实实跑
		// （deterministic 不可伪造），gate 里不跑命令就不能伪称跑过。
		if state.HasAcceptance() && !state.AllAcceptancePassed() {
			fmt.Fprintf(os.Stderr, "%s任务登记了 %d 条验收标准但未全部通过——先跑 'forge task verify-acceptance' 实跑回扣（spec-as-gate）\n", GateAdvisory("[task-verify] "), len(state.Acceptance))
		}
	}

	// Evidence-chain agent-claim data source: an agent advancing a non-auto gate 'declares'
	// that stage complete (task-verify = verification claim, task-complete = completion claim).
	// Complementary to deterministic hook/gate execution checks — EvidenceChain buckets by
	// Source, and the ratio = how much deterministic evidence backs a completion claim,
	// surfacing the LLM-judge blind spot of 'agent skips prerequisites and still declares done'.
	// Recorded only when the gate actually passes and the task is not yet complete (re-checking
	// a completed task does not re-declare).
	//
	// 证据链 agent-claim 数据源：agent 推进一个非自动 gate 即「声明」该阶段完成
	//（task-verify=验证声明，task-complete=完成声明）。与 deterministic 的 hook/gate
	// 实跑检查互补——EvidenceChain 据 Source 分桶，ratio=完成声明背后有多少
	// deterministic 证据支撑，照出「agent 跳过前置就声明完成」的 LLM-judge 盲区。
	// 仅在 gate 实际通过且任务未完成时记录（重检 completed 任务不重复声明）。
	if !gate.Auto && state.CompletedAt == nil {
		switch gateID {
		case "task-verify":
			recordAudit(root, &checklog.Entry{
				Check:   checklog.CheckTaskVerify,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  `agent-claim: 通过 task-verify gate（agent 自述验证完成）`,
			})
		case "task-complete":
			recordAudit(root, &checklog.Entry{
				Check:   checklog.CheckTaskComplete,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  `agent-claim: 通过 task-complete gate（agent 自述任务完成）`,
			})
		}
	}

	return &ExecuteResult{
		GateID:  gateID,
		Passed:  true,
		Message: fmt.Sprintf("%s - passed (verified by AI agent)", gate.Name),
	}, nil
}

// PendingDependencies returns the subset of refs not yet delivered — the DependsOn gate's block
// list. A ref that fails to load counts as pending: a dependency that was never created, was
// aborted, or is typo'd is not "delivered", and treating it as such would let a task verify past
// a broken edge. The returned strings are the refs VERBATIM (joined in the BLOCKED message); mine
// --blocked is where status detail is expanded for the human.
//
// Cross-repo semantics (multi-repo workspace Option B, depref.go): a `<key>:<ref>` entry resolves
// to forgedata.RootDir(key)/tasks via LoadDepState — read-only, no locking. The conservative rule
// extends across repos: target task missing/unreadable, or the key having no data dir at all, all
// count as PENDING (never a silent release); the verbatim key:ref is what lands in the block list
// so the user can see which repo the gate is waiting on.
//
// PendingDependencies 返回尚未交付的 ref 子集——DependsOn 门禁的阻断清单。加载失败的 ref 计为
// pending：从未创建、已 abort、或拼错的依赖都非「已交付」，若放过会让 task 校验过一个断裂的依赖边。
// 返回的字符串原样保留入参 ref（在 BLOCKED 信息里拼接）；状态细节在 mine --blocked 处为人展开。
//
// 跨仓语义（多仓 workspace Option B，见 depref.go）：`<key>:<ref>` 条目经 LoadDepState 解析到
// forgedata.RootDir(key)/tasks——只读、无锁。保守规则延伸到跨仓：目标 task 缺失/不可读、或
// key 根本没有数据目录，一律计 PENDING（绝不静默放行）；阻断清单里落的是原样 key:ref，让用户
// 看清门禁在等哪个仓。
//
// API 分工（与 health.DeadlockedDependency 的双形态是有意为之，非 DRY 违反）：本函数接收 root + refs，
// 内部对每个 ref 跑 LoadDepState（per-ref 磁盘读、无缓存），返回裸 ref 列表——契合门禁侧的调用点
// （门禁已有 root，只需知道「卡住没」+阻断清单）。DeadlockedDependency 接收单个 TaskState + lookup 回调，
// 返回 (ref, bool)——契合 health 扫描侧（调用方可缓存/限定 scope/复用 state map）。门禁侧无须 state map
// （它只判本任务），故采 root 形态更直接；若未来门禁也需 deadlock 判定，可在调用点自建 lookup 复用，
// 不必在此统一两 API（强行统一会迫使门禁侧为「只判本任务」而构建无用 state map）。
func PendingDependencies(root string, refs []string) []string {
	var pending []string
	for _, ref := range refs {
		if ref == `` {
			continue
		}
		st, err := LoadDepState(root, ref)
		if err != nil || st == nil {
			pending = append(pending, ref)
			continue
		}
		if !st.IsDelivered() {
			pending = append(pending, ref)
		}
	}
	return pending
}

// runAutoChecks runs the automated checks for a task gate.
//
// runAutoChecks 跑 task gate 的自动化检查。
func runAutoChecks(root string, gateID string, state *TaskState) (*ExecuteResult, error) {
	switch gateID {
	case "task-implement":
		return checkImplement(root, state)
	default:
		return &ExecuteResult{
			GateID:  gateID,
			Passed:  true,
			Message: "no auto-checks defined",
		}, nil
	}
}

// hasCodeChanges reports whether there are real code changes since the task started.
// It checks the working-tree changes, then new commits since the task's recorded base.
// The base is state.HeadCommit (recorded at task start) when available — branch-agnostic,
// so main/master tasks that commit mid-task (the AGENTS.md flow itself encourages an
// in-task commit) are detected exactly like feature-branch tasks; only when HeadCommit
// is empty (legacy state) does it fall back to probing base branches on a feature
// branch. Graceful degradation for non-git repositories (returns true to avoid false
// negatives).
//
// hasCodeChanges 自 task 起算是否真有代码变更。
// 先查工作树变更，再查自 task 记录基准以来的新 commit。基准优先用 state.HeadCommit
// （task start 时记录）——与分支无关，main/master 上的 task 中途 commit（AGENTS.md
// 流程本身鼓励中段 commit）与 feature 分支走同一路径；HeadCommit 为空（legacy
// state）时才回落到 feature 分支的 base 分支探测。非 git 仓库优雅退化（返回 true
// 以免误判）。
func hasCodeChanges(root string, state *TaskState) bool {
	// Check 1: working-tree changes (including staged-but-uncommitted).
	//
	// 检查 1：工作树变更（含 staged 未 commit）
	cmd := exec.Command("git", "-C", root, "diff", "--stat", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return true // 非 git 仓库——放行
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true
	}

	// Check 2: new commits since the task's recorded base.
	//
	// 检查 2：自 task 记录基准以来的新 commit
	if state != nil {
		if state.HeadCommit != "" {
			// HeadCommit (recorded at task start) is the precise base — the same one
			// taskChangedFiles/scoring use — and works identically on main/master and
			// feature branches. gating on Branch != main/master here used to deadlock
			// main-branch tasks: after a mid-task commit the gate fell through to
			// 'no code changes' forever.
			//
			// HeadCommit（task start 时记录）是精确基准——与 taskChangedFiles/scoring
			// 同源——且在 main/master 与 feature 分支上行为一致。此前此处按
			// Branch != main/master 设卡，main 分支 task 中途 commit 后会永远落入
			// 「no code changes」死锁。
			cmd = exec.Command("git", "-C", root, "diff", "--name-only", state.HeadCommit+"..HEAD")
			out, err = cmd.Output()
			if err != nil {
				// Base unreachable (amend/rebase rewrote history) — pass rather than
				// falsely reporting 'no code changes'.
				//
				// 基准不可达（amend/rebase 改写历史）——放行，不谎报「无代码改动」。
				return true
			}
			return len(strings.TrimSpace(string(out))) > 0
		}
		if state.Branch != "" && state.Branch != "main" && state.Branch != "master" {
			// Legacy state without HeadCommit: probe base branches on a feature branch.
			//
			// 无 HeadCommit 的 legacy state：feature 分支上探测 base 分支
			for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
				cmd = exec.Command("git", "-C", root, "rev-list", "--count", base+"..HEAD")
				out, err = cmd.Output()
				if err == nil {
					return strings.TrimSpace(string(out)) != "0"
				}
			}
			// No base branch found — pass.
			//
			// 找不到任何 base 分支——放行
			return true
		}
	}

	// On main/master with no HeadCommit and no uncommitted changes.
	//
	// main/master 上、无 HeadCommit 且无未 commit 改动
	return false
}

// checkImplement runs the (v0.25 advisory) auto-compile + assertion-check hooks, records
// the results to checklog, and verifies that code changes actually exist. Both hooks are now
// non-blocking — they only emit advisory reminders and never FAIL — so the only remaining
// hard failure here is 'no code change since task start' (task semantics, not a tech-stack
// check). The compilePassed/assertPassed branches below are kept as defense-in-depth against
// a future hook regression re-introducing FAIL.
//
// checkImplement 跑（v0.25 advisory）auto-compile + assertion-check hook，把结果
// 记到 checklog，并校验代码改动确实存在。两个 hook 现已非阻塞——只发 advisory
// 提醒、永不 FAIL——所以这里剩下的唯一硬失败是「task 起算无代码改动」（task
// 语义，而非 tech-stack 检查）。下面的 compilePassed/assertPassed 分支保留作
// defense-in-depth，防未来 hook 回归重新引入 FAIL。
func checkImplement(root string, state *TaskState) (*ExecuteResult, error) {
	taskRef := ""
	if state != nil {
		taskRef = state.TaskRef
	}

	// 1. Compile check — via the EMBEDDED auto-compile script, the same source used by the
	// write-time PostToolUse hook (cli.runHook). Reading the on-disk .forge/hooks/auto-compile.sh
	// instead would let the gate check a tamperable copy while the write-time hook checks the
	// trusted embed, and the two would drift (a tampered disk script could let the gate pass a
	// build the write-time hook would still flag as broken).
	//
	// 1. 编译检查——经 EMBEDDED auto-compile 脚本，与 write-time PostToolUse hook
	//（cli.runHook）使用同一份源。改读磁盘上的 .forge/hooks/auto-compile.sh 会让
	// gate 检查可篡改副本、而 write-time hook 检查受信 embed，两者会漂移（被篡改
	// 的磁盘脚本可能让 gate 通过一个 write-time hook 仍会标记为坏的构建）。
	compilePassed, compileInfra, compileOutput := runEmbeddedHook(root, "auto-compile")

	compileDetail := fmt.Sprintf("auto-compile.sh: %s", compileOutput)
	if compileInfra {
		// 基建故障（bash spawn error / exit 126/127——WSL bash 看不到 Windows 临时
		// 路径的特征）：Detail 加 INFRA: 前缀（Passed=false 保留，便于统计基建故障率），
		// 但不判 gate fail——fail-open 对齐 hook 路径 isHookInfraFailure 哲学（环境
		// 问题不是质量失败）。真实编译错误（脚本正常执行、编译器报错）语义不变。
		compileDetail = "INFRA: " + compileDetail
	}
	compileEntry := &checklog.Entry{
		Check:   checklog.CheckAutoCompile,
		Passed:  compilePassed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  compileDetail,
	}
	if compileInfra {
		// 基建故障 fail-open 不是质量失败——Level warn（trace 渲染 ⚠ 而非 ✗）；
		// scoring 侧按 INFRA: 前缀跳过（scoring.go），Passed=false 仅用于故障率统计。
		compileEntry.Level = checklog.LevelWarn
	}
	recordAudit(root, compileEntry)

	if !compilePassed && !compileInfra {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: fmt.Sprintf("build failed: %s", compileOutput),
		}, nil
	}

	// 2. Assertion-weakening check — same embedded source as the write-time PreToolUse hook.
	// No disk fallback: the embed is canonical, so a tampered .forge/hooks/assertion-check.sh
	// cannot weaken what the gate enforces.
	//
	// 2. 断言弱化检查——与 write-time PreToolUse hook 同源 embed。无磁盘回退：
	// embed 即 canonical，故被篡改的 .forge/hooks/assertion-check.sh 无法削弱
	// gate 强制的内容。
	assertPassed, assertInfra, assertOutput := runEmbeddedHook(root, "assertion-check")

	assertDetail := fmt.Sprintf("assertion-check.sh: %s", assertOutput)
	if assertInfra {
		// 基建故障分级同 auto-compile（见上）：INFRA: 前缀记录，不判 gate fail。
		assertDetail = "INFRA: " + assertDetail
	}
	assertEntry := &checklog.Entry{
		Check:   checklog.CheckAssertion,
		Passed:  assertPassed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  assertDetail,
	}
	if assertInfra {
		// 同 auto-compile：基建故障 Level warn，非质量失败。
		assertEntry.Level = checklog.LevelWarn
	}
	recordAudit(root, assertEntry)

	if !assertPassed && !assertInfra {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: fmt.Sprintf("assertion check failed: %s", assertOutput),
		}, nil
	}

	// 3. Verify code changes actually exist (not just a precompiled base).
	//
	// 3. 校验确有代码改动（不仅是预编译的 base）。
	if !hasCodeChanges(root, state) {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: "no code changes detected - build passed but no files modified",
		}, nil
	}

	// 4. Plan-first record (shift-left): a code task that produced real changes with neither
	// Plan nor Goal recorded skipped the proposal stage — direction errors then surface at
	// review time, the expensive end of the implement→review→rework loop. Runs AFTER the
	// code-changes check so failed/empty implement attempts don't spam the log on every
	// retry. Absence is advisory, never blocks (small fixes legitimately skip a written plan).
	//
	// Recorded ONCE PER TASK: the first real implementation records the entry (plan present
	// = Passed:true) and fires the nudge; later implement retries skip both. Once-per-task
	// (2026-08 noise audit): the identical advisory re-fired up to 3 times on one task
	// (30 identical entries across 8 tasks) — after the first firing the message adds
	// nothing. PlanFirstAdvisoryFired persists the firing in task state, so retries
	// (each reloading state from disk) skip both the entry and the stderr nudge.
	//
	// 4. 方案前置记录（shift-left）：产出了真实改动但 Plan/Goal 皆空的代码任务 = 跳过
	// 方案阶段——方向错误会拖到审查环节才暴露，那是 实现→审查→返工 循环最贵的一端。
	// 放在代码改动校验之后：失败/空改的 implement 重试不会每轮刷日志。无方案仅
	// advisory，绝不阻塞（小修复合法地不写方案）。
	//
	// 每任务记录一次：实现首次坐实时落条目（有方案=Passed:true）并发提示，之后的
	// implement 重试两者都跳过。每任务一次（2026-08 噪音审计）：同一 advisory 在单
	// 任务上最多重复 3 次（8 个任务 30 条全同文案）——首次之后信息量不再增长。
	// PlanFirstAdvisoryFired 把已发标记持久化进 task state，重试（每次从磁盘重载
	// state）跳过条目与 stderr 提示。
	if state != nil && !state.PlanFirstAdvisoryFired {
		hasPlan := state.Plan != "" || state.Goal != ""
		entry := &checklog.Entry{
			Check:   checklog.CheckPlanFirst,
			Passed:  hasPlan,
			Checked: true,
			TaskRef: taskRef,
			Level:   checklog.LevelAdvisory,
		}
		if hasPlan {
			entry.Detail = "plan/goal recorded before implementation"
		} else {
			entry.Detail = GateAdvisory("[task-implement] 无方案记录（task start 未带 --plan-file/--goal）——方案先行能在方案阶段拦下方向错误，降低审查-返工轮次；小修复可忽略本提示")
			fmt.Fprintf(os.Stderr, "%s\n", entry.Detail)
		}
		recordAudit(root, entry)
		// 先置标记再持久化（best-effort）：即便落盘失败，本进程内的重试也不会重发；
		// 跨进程重试最坏重发一次，优于静默丢失「已记录」的语义。与 DesignPhases 持久化
		// 同款模式（executor.go task-verify 段）。
		//
		// Flag first, persist second (best-effort): even if the write fails, retries
		// within this process stay silent; a cross-process retry re-fires at most once —
		// better than silently losing the "already recorded" semantics. Same pattern as
		// the DesignPhases persistence (executor.go task-verify section).
		state.PlanFirstAdvisoryFired = true
		if err := SaveTaskState(root, state); err != nil {
			fmt.Fprintln(os.Stderr, "[task-implement] plan-first marker persist failed:", err)
		}
	}

	return &ExecuteResult{
		GateID:  "task-implement",
		Passed:  true,
		Message: "code changes present (compile/assertion advisory via hooks)",
	}, nil
}

// runEmbeddedHook executes an embedded hook script (hooks.EmbeddedContent): it writes a
// temp file and runs it with bash — mirroring how the write-time path (cli.runHook) runs
// hooks. The gate layer uses the same embedded source as the write-time check; reading
// on-disk .forge/hooks/*.sh would let the gate check a tamperable copy that could drift
// from the trusted embed. root is passed as $1 and the working directory, aligning with the
// previous disk-hook invocation so scripts resolving the project via $1 or $PWD behave
// consistently.
//
// The infra return marks infrastructure failures (bash spawn error / exit 126/127 —
// the WSL-bash signature, script file invisible): NOT a gate verdict. checkImplement
// records them with an INFRA: Detail prefix (Passed=false, so the infra-failure rate
// stays countable) but does not fail the gate — the fail-open philosophy of the
// write-time hook path (cli.isHookInfraFailure). A real compile failure (script ran,
// compiler errored) keeps passed=false, infra=false and fails the gate as before.
//
// bash resolution goes through shellexec.FindBash — the same WSL-avoidance logic the
// write-time path uses. The pre-shellexec bare exec.Command("bash", ...) resolved to
// WSL bash under native parents, and 45/53 gate auto-compile FAILs were
// 'forge-gate-*.sh: No such file or directory'.
//
// runEmbeddedHook 执行 embed 的 hook 脚本（hooks.EmbeddedContent）：写临时文件后
// 用 bash 跑——镜像 write-time 路径（cli.runHook）跑 hook 的方式。gate 层使用与
// write-time 检查同源的 embed 源；改读磁盘 .forge/hooks/*.sh 会让 gate 检查可
// 篡改副本、可能与受信 embed 漂移。root 作为 $1 与工作目录传入，对齐此前磁盘
// hook 调用方式，让脚本经 $1 或 $PWD 解析项目时表现一致。
//
// infra 返回值标记基础设施故障（bash spawn 错误 / exit 126/127——WSL bash 特征，
// 脚本文件不可见）：不是门禁结论。checkImplement 用 INFRA: Detail 前缀记录它们
// （Passed=false 保留，基建故障率可统计）但不判 gate fail——与 write-time hook
// 路径（cli.isHookInfraFailure）的 fail-open 哲学对齐。真实编译失败（脚本正常
// 执行、编译器报错）保持 passed=false, infra=false，gate 照旧 fail。
//
// bash 解析走 shellexec.FindBash——与 write-time 路径同源的 WSL 规避逻辑。
// shellexec 之前的裸 exec.Command("bash", ...) 在原生父进程下解析到 WSL bash，
// 45/53 次 gate auto-compile FAIL 都是 'forge-gate-*.sh: No such file or directory'。
func runEmbeddedHook(root, name string) (passed bool, infra bool, output string) {
	content, ok := hooks.EmbeddedContent(name)
	if !ok {
		// Unknown hook name is a programming error, not infrastructure — fail closed.
		//
		// 未知 hook 名是编程错误而非基建故障——fail closed。
		return false, false, fmt.Sprintf("embedded hook %q not found", name)
	}
	tmp, err := os.CreateTemp("", "forge-gate-*.sh")
	if err != nil {
		return false, true, fmt.Sprintf("create temp hook file: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return false, true, fmt.Sprintf("write temp hook file: %v", err)
	}
	tmp.Close()
	// bash reads the file as an argument; no chmod needed (not exec'd directly).
	//
	// bash 把文件作为参数读；无需 chmod（不直接 exec）。

	bashPath, err := findBashForHook()
	if err != nil {
		return false, true, fmt.Sprintf("resolve bash: %v", err)
	}

	// Windows: os.CreateTemp returns a backslash path (C:\Users\...\forge-gate-*.sh); bash
	// treats backslashes as escape characters and swallows them → 'No such file or directory',
	// causing task-implement's build check to fail spuriously. filepath.ToSlash converts to
	// forward slashes (Git Bash can parse them). cmd.Dir still uses the native root — Go exec
	// needs the native path as cwd when launching the bash subprocess on Windows, and bash
	// itself handles a Windows cwd fine.
	//
	// Windows: os.CreateTemp 返回反斜杠路径（C:\Users\...\forge-gate-*.sh），bash 把反斜杠当
	// 转义吃掉 → "No such file or directory"，task-implement 的 build 检查因此误判失败。
	// filepath.ToSlash 转正斜杠（Git Bash 可解析）。cmd.Dir 仍用原生 root——Go exec 在
	// Windows 启动 bash 子进程要原生路径做 cwd，bash 自身能处理 Windows cwd。
	cmd := exec.Command(bashPath, filepath.ToSlash(tmpPath), filepath.ToSlash(root))
	cmd.Dir = root
	// 剔除继承自父进程的 tool-input env（2026-08-25 review minor）：cmd.Env 未设时
	// 子进程继承 os.Environ()——父 shell 恰好 export 了 FORGE_FILE_PATH 会把
	// assertion-check 从 batch（门禁全量扫描，以 FILE_PATH 为空判别）静默降级成
	// per-edit 分析，门禁职责落空。正常注入路径（cli.runHook）是显式设置这些 env
	// 的，不经本函数，不受影响。FORGE_CONTENT/FORGE_COMMAND 同样会被继承但无消费
	// 者：前者只在 FILE_PATH 非空时读取，后者只被不经本路径的 bash-guard/
	// hazard-guard 读取。
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		switch k {
		case "FORGE_FILE_PATH", "FORGE_TOOL_NAME", "FORGE_OLD_STRING", "FORGE_NEW_STRING":
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if shellexec.IsHookInfraFailure(err) {
		return false, true, strings.TrimSpace(fmt.Sprintf("%v: %s", err, string(out)))
	}
	return err == nil, false, strings.TrimSpace(string(out))
}

// findBashForHook resolves the bash interpreter for embedded gate hooks. Seamed
// as a package var so tests can simulate infrastructure failures (spawn error /
// exit 127). Production value: shellexec.FindBash — the same WSL-avoidance
// resolution the write-time hook path (cli.runHook) uses; single source, one fix
// locus.
//
// findBashForHook 解析 embedded gate hook 的 bash 解释器。以包级变量留缝，
// 供测试模拟基建故障（spawn 错误 / exit 127）。生产值为 shellexec.FindBash——
// 与 write-time hook 路径（cli.runHook）同源的 WSL 规避解析；单一真相源，
// 一个修复落点。
var findBashForHook = shellexec.FindBash

// getDisableWorkActivity returns whether work-activity / read-before-edit checks are
// disabled for this task. Plan-5: per-task Overrides (forge task override) take precedence
// over the process-level FORGE_WORK_ACTIVITY env — escaping one task does not leak to other
// tasks in the same shell. The env remains as a CI/test fallback.
//
// getDisableWorkActivity 返回本 task 是否禁用 work-activity / read-before-edit 检查。方案5: per-task Overrides (forge task override) take
// 优先于进程级 FORGE_WORK_ACTIVITY env——某 task 逃生不会泄漏到同 shell 的其他
// task。env 留作 CI/测试 fallback。
func getDisableWorkActivity(state *TaskState) bool {
	return escapeDisabled(state, escapeWorkActivity, envWorkActivity)
}

// isLastGate returns whether the given gate ID is the last gate in the pipeline. After the
// last gate (task-complete) there is no work phase, so the work-activity check is skipped —
// nothing to 'spend time on'.
//
// isLastGate 返回给定 gate ID 是否是流水线的最后一个 gate。最后一个 gate
// （task-complete）之后无工作阶段，故跳过工作活动检查——没有可「花时间」的内容。
func isLastGate(gateID string) bool {
	gates := DefaultGates()
	return len(gates) > 0 && gates[len(gates)-1].ID == gateID
}

// behaviorSurfacePrefixes are the repo paths whose changes alter user-visible
// behaviour that the doc guards cannot see (behavioural prose in README/
// homepage/plugin docs). Matched by exact file or directory prefix.
//
// behaviorSurfacePrefixes 是改变用户可见行为、而文档守卫看不到（README/
// homepage/插件文档里的行为描述）的仓库路径。按精确文件或目录前缀匹配。
var behaviorSurfacePrefixes = []string{
	"internal/cli/init.go",
	"internal/cli/sync.go",
	"internal/cli/uninstall.go",
	"internal/agentbridge/",
	"internal/skillgen/",
	"internal/hooks/settings.go",
	"internal/protocol/",
	"internal/registry/",
	"internal/forgedata/",
	"internal/projectroot/",
}

// behaviorSurfaceHits returns the changed files that touch the user-visible
// behaviour surface (behaviorSurfacePrefixes), for the task-complete docs
// advisory. Nil when nothing matches.
//
// behaviorSurfaceHits 返回触及用户可见行为面（behaviorSurfacePrefixes）的
// 改动文件，供 task-complete 文档 advisory 使用。无命中返回 nil。
func behaviorSurfaceHits(changed []string) []string {
	var out []string
	for _, f := range changed {
		for _, p := range behaviorSurfacePrefixes {
			if f == p || strings.HasPrefix(f, p) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

package taskpipeline

// executor_check_activity.go — ExecuteTaskGate 拆分（refactor/executor-pipeline 第一步）：
// 非 auto gate 的工作活动检查（read-before-edit / 探索轴 / 遥测缺失降级 / checklog 回退）。
// 代码体自 executor.go 的 ExecuteTaskGate 原样提取，行为等价——仅变量引用改为参数名。
//
// executor_check_activity.go — ExecuteTaskGate decomposition (refactor/executor-pipeline
// step 1): the work-activity check for non-auto gates (read-before-edit / the exploration
// axis / the telemetry-missing degrade / the checklog fallback). The body was extracted
// verbatim from ExecuteTaskGate in executor.go — behavior-equivalent; only variable
// references became parameter names.

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// checkWorkActivity is the work-activity check for non-auto gates: real work between gates
// is required to pass. Skipped for: completed tasks (re-check) and the last gate (no work
// phase afterwards) — those conditions are judged by the caller (ExecuteTaskGate).
// Note: this check is intentionally NOT skipped after an auto gate. Under the 3-gate
// pipeline task-verify immediately follows the auto gate task-implement, and the
// implement→verify stretch is exactly the interval where read-before-edit must be enforced.
// Skipping after auto was the old rule from the 5-gate era and would make this check
// ineffective under the 3-gate flow (activity would never run).
//
// checkWorkActivity 是非 auto gate 的工作活动检查：gate 之间必须有真实工作才能通过。
// 跳过：已完成 task（复检）+ 最后一个 gate（之后无工作阶段）——这两条件由调用方
// （ExecuteTaskGate）判定。注意：此处故意不在 auto gate 之后跳过本检查。3-gate 流水线下
// task-verify 紧跟 auto gate task-implement，implement→verify 这段正是必须强制
// read-before-edit 的区间。auto gate 之后跳过是 5-gate 时代的旧规则，会让本检查在
// 3-gate 流程下失效（activity 永不运行）。
func checkWorkActivity(root string, gateID string, state *TaskState) error {
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
					// Fail closed: this branch is only reached with a READABLE toollog
					// (rerr==nil above) and edits>0/reads==0 — the grace reread failing
					// is a transient IO condition on the same file, not missing
					// telemetry. Silently passing turned a certain hard-block candidate
					// into an invisible fail-open (2026-08-29 review round).
					//
					// 失败关闭：本分支仅在 toollog 可读（上方 rerr==nil）且
					// edits>0/reads==0 时可达——grace 二次读失败是对同一文件的
					// 瞬时 IO，不是遥测缺失。静默放行会把确定的硬拦候选变成
					// 不可见 fail-open（2026-08-29 审查轮）。
					fmt.Fprintf(os.Stderr, "[forge] warning: grace read check failed: %v\n", gerr)
					return GateBlocked(
						"gate %q cannot pass without reading any code during this task (edits=%d; grace-window probe failed: %v). "+
							"HARD stop, not a reminder — Read the file(s) you edit, then re-run `forge task gate %s`",
						gateID, edits, gerr, gateID,
					)
				} else if grace > 0 {
					fmt.Fprintf(os.Stderr, "[forge] note: read-before-edit satisfied via grace window (%d nearby Read(s) logged outside this task — task-start/Read race)\n", grace)
				} else {
					return GateBlocked(
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
					return GateBlocked(
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
	return nil
}

package taskpipeline

// executor_check_complete.go — ExecuteTaskGate 拆分（refactor/executor-pipeline 第一步）：
// task-complete 专属检查（review 硬前置与快照一致性 / test-coverage 兜底硬前置 / complete 时
// 的全部 advisory）。代码体自 executor.go 的 ExecuteTaskGate 原样提取，行为等价——仅变量引用
// 改为参数名。
//
// executor_check_complete.go — ExecuteTaskGate decomposition (refactor/executor-pipeline
// step 1): the task-complete-specific checks (review hard prerequisite + snapshot
// consistency / the test-coverage backstop hard prerequisite / all complete-time
// advisories). Bodies were extracted verbatim from ExecuteTaskGate in executor.go —
// behavior-equivalent; only variable references became parameter names.

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/docsconsistency"
	"github.com/MjxUpUp/Forge/internal/scoring"
)

// checkCompleteReviewPrereqs bundles task-complete's two review hard prerequisites:
// the code-review-gate hard prerequisite and the review-snapshot consistency check.
//
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
//
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
func checkCompleteReviewPrereqs(root string, state *TaskState) error {
	if !state.ReviewPassed {
		return GateBlocked("task-complete requires code-review-gate: 派只读子 agent 审查当前 diff 后运行 `forge review pass`（HARD stop，非提醒）")
	}

	if state.ReviewPassed && state.ReviewedHeadCommit != "" {
		cur, _, err := TaskFingerprint(root, state, state.ReviewedHeadCommit)
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
			return GateBlocked("task-complete 拒绝：审查通过后检测到源码变更（基线 HEAD=%s）。HARD stop——请重新派只读子 agent 审查当前代码，再用 `forge review pass --note \"<复审结论>\"` 刷新审查基线（基线有源码变更时裸 `forge review pass` 会被拒：须 --note 记复审结论或 --acknowledge-changes 自我承担并留 self-refresh 审计）", state.ReviewedHeadCommit)
		}
	}
	return nil
}

// checkTestCoverageBackstop is the test-coverage backstop hard prerequisite (task-complete):
// it fills the gap left by the task-verify advisory — under advisory semantics an agent
// self-report is enough (eval evidence: 0/19, 0/25 coverage still passed complete;
// test-discipline loaded 0 times). Here we hard-block 'large uncovered change, zero
// assertions': ≥3 changed source files WITHOUT paired tests and zero assertions anywhere
// = slam-dunk corrupt success (large change with no verification trail). Partial coverage
// (<3 files missing tests, fudge factor, aligned with the spirit of Sonar's <20-line
// exemption) or 'has assertions but 0 paired coverage' (tests live elsewhere / refactor
// scenarios) still passes as advisory — avoid false positives. The threshold counts
// MISSING files (matching the testCoverageHardGateThreshold doc), not all changed source
// files — otherwise a well-tested change with one missing file would be hard-blocked and
// the BLOCKED text would lie.
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
// It returns the task's changed files: taskChangedFiles spawns several git subprocesses,
// and its result feeds BOTH this coverage gate (via the precomputed-list variant — no
// second taskChangedFiles run inside CheckTestCoverage) and the later behavior-surface /
// goal↔output checks (2026-08-29 review round: the gate previously recomputed it,
// double-running the multi-subprocess probe).
//
// checkTestCoverageBackstop 是 test-coverage 兜底硬前置（task-complete）：补 task-verify
// advisory 的缺口——advisory 语境下 agent 自述即过（eval 证据：0/19、0/25 覆盖照过 complete，
// 0 次加载 test-discipline）。此处对「大面积未覆盖且零断言」硬阻断：≥3 个改动源文件无配对
// 测试、且全仓零断言 = corrupt success 铁证（大改无任何验证痕迹）。部分覆盖（缺测试文件 <3 个，
// fudge factor，对齐业界 Sonar <20 行豁免精神）或「有断言但 0 配对覆盖」（测试在别处/重构场景）
// 仍 advisory 放行——避免误伤。阈值按「无配对测试的文件数」计（与 testCoverageHardGateThreshold
// 文档一致），而非全部改动源文件数——否则改 3 文件只缺 1 个测试也会被硬阻断，且 BLOCKED 文案说谎。
// escape（per-task override / FORGE_TEST_COVERAGE）由 CheckTestCoverage 内部返回 ok=true
// 处理，此处天然放行（验证类逃生，降 evidence Weak 留痕；重证据任务按 2026-08 证据
// 缩放豁免，见 checklog.EscapeDowngradedStrength）。
// 方案 B：BLOCKED 消息复用 formatMissing 的 skill 路由——advisory 语境失效（0 触发），
// blocking 语境下 agent 必须处理才能过（skill 驱动靠 advisory→blocking 转变，非新机制）。
// 断言信号复用 scoring.CollectAssertionDensity（已注入 EvaluateInput；taskpipeline→scoring
// 单向无循环依赖）。checklog 记最终态——覆盖 task-verify 的记录（agent 可能在两 gate 间补了
// 测试，Latest 应反映 task-complete 时覆盖状态供 score/trace）。
//
// 返回任务的变更文件列表：taskChangedFiles 起多个 git 子进程，其结果同时喂本覆盖门禁
// （走预计算列表变体——CheckTestCoverage 内不再第二次跑 taskChangedFiles）与后续行为面/
// goal↔output 检查（2026-08-29 审查轮：门禁此前会重算一遍，多个 git 子进程双跑）。
func checkTestCoverageBackstop(root string, state *TaskState) (changedFiles []string, err error) {
	changedFiles = taskChangedFiles(root, state)
	ok, missing, total := checkTestCoverageChanged(root, state, changedFiles)
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
	return changedFiles, nil
}

// adviseCompleteDelivery runs task-complete's advisory bundle — docs-consistency drift,
// behavior-surface, commit-ordering, branch-merged and goal↔output coarse-match. None of
// these block; they surface delivery smells at the exact moment they can still be repaired.
// changedFiles is the precomputed list from checkTestCoverageBackstop — no second
// taskChangedFiles run.
//
// adviseCompleteDelivery 跑 task-complete 的 advisory 组——docs-consistency drift、行为面、
// 提交顺序、分支归属、目标↔产出粗匹配。全部不阻塞；在还能补救的精确时刻照出交付异味。
// changedFiles 是 checkTestCoverageBackstop 预计算的列表——不再第二次跑 taskChangedFiles。
func adviseCompleteDelivery(root string, state *TaskState, changedFiles []string) {
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
	// the README until the user noticed). Remind at complete time, when the
	// diff is known. Advisory only.
	//
	// 行为面 advisory：文档守卫只覆盖命令/flag【引用】，从不覆盖行为【描述】
	// ——触及用户可见行为面（init/sync/uninstall、agent bridge、指令生成器、
	// protocol/registry）的 diff 可以让 README/homepage 的过时描述不穿任何
	// 守卫上线（2026-08 实证：user-level-assets 重构后 README 仍写
	// "forge init 创建 .forge/"，直到用户发现）。在 complete 时（diff 已知）
	// 提醒。仅 advisory。
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

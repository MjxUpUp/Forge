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
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// CheckNameDocsConsistency is the checklog entry name for the task-complete
// docs-consistency advisory, so trace surfaces the drift signal even though the
// gate passes (advisory, never blocking).
const CheckNameDocsConsistency checklog.CheckName = "docs-consistency-gate"

// CheckNameReviewSnapshot is the checklog entry name for the review-snapshot
// fail-open case (审查基线 commit 不可达，amend/rebase 改写历史）。fail-open 是设计本意
// （amend 是正常工作流，强复审会死循环），但必须落盘留痕——让 score/dashboard 能反映
// "该任务靠 fail-open 而非真复审通过"，事后可追溯，而非只 stderr 一闪而过。
const CheckNameReviewSnapshot checklog.CheckName = "review-snapshot-failopen"

// taskStartReadGraceWindow bounds how far before a task's StartedAt a Read still
// counts toward read-before-edit when recovering the task-start/Read race (see
// toolusage.ReadEditCountsGraceWindow). 60s covers the parallel-tool-call window
// where a Read fired alongside `forge task start` lands under the previous task's
// ref and/or just before StartedAt — excluding it from ReadEditCounts(taskRef).
const taskStartReadGraceWindow = time.Minute

// ExecuteResult holds the outcome of a task gate execution.
type ExecuteResult struct {
	GateID  string
	Passed  bool
	Message string
}

// ExecuteTaskGate runs a single task gate's checks.
// For auto-gates (task-implement), it runs the relevant hook scripts.
// For non-auto gates, it verifies the gate was previously marked passed
// (the AI agent is responsible for doing the actual work).
func ExecuteTaskGate(root string, gateID string, state *TaskState) (*ExecuteResult, error) {
	gate := GateByID(gateID)
	if gate == nil {
		return nil, fmt.Errorf("unknown task gate: %s", gateID)
	}

	// generic kind（调研/设计/纯接续任务）不走门禁检查——这些任务没有代码变更可编译/测试/审查，
	// 强制 3 道门禁会把"开个调研/接续任务"变成过门禁负担，与接续真相源低摩擦持久化的定位相悖。
	// kind 默认空/"code" 仍走完整门禁（向后兼容：老 task 无 Kind 字段）。直接标通过，跳过前置
	// gate / 审查快照 / 工作活动 / advisory 全部检查——generic task 的价值在持久化的 plan/决策，
	// 不在门禁。complete 评分也据此分流（见 cli/runTaskComplete）。
	if state.IsGeneric() {
		return &ExecuteResult{
			GateID:  gateID,
			Passed:  true,
			Message: fmt.Sprintf("%s - skipped (generic task: 走接续不走门禁)", gate.Name),
		}, nil
	}

	// Check prerequisites: all previous gates must have passed
	gates := DefaultGates()
	for _, g := range gates {
		if g.ID == gateID {
			break
		}
		if !state.gatePassed(g.ID) {
			return nil, fmt.Errorf("prerequisite gate %q has not passed", g.ID)
		}
	}

	// task-complete 硬前置：code-review-gate 必须已通过（ReviewPassed=true）。
	// 防 agent 自称完成跳过子 agent 审查——这是"提交前必审"双路径里 task 路径的强制点
	// （非 task 路径由 review-stop hook 拦）。agent 须派只读子 agent 审查后运行
	// `forge review pass` 标记，才能过此门禁进而 task complete。
	// 复检已完成任务（CompletedAt 已设）时跳过——历史任务不追溯。
	if gateID == "task-complete" && !state.ReviewPassed && state.CompletedAt == nil {
		return nil, fmt.Errorf("task-complete requires code-review-gate: 派只读子 agent 审查当前 diff 后运行 `forge review pass`")
	}

	// 审查快照一致性（task-complete 硬门禁）：review pass 时绑定 (ReviewedHeadCommit,
	// ReviewedChangeHash)；此处重算 SourceChangesSince(ReviewedHeadCommit) 比对，不一致说明审查
	// 通过后改了源码 → 拒绝、强制复审（审查-修复-复审闭环，不再靠 agent 自律重审）。与上面的
	// ReviewPassed 硬前置正交——上面拒"没审过"，这里拒"审过但代码又变了"，两者叠加才构成完整闭环。
	// ReviewedHeadCommit=="" → commit-then-review 流（审查时工作区干净，hash 空）或老 state 兼容，
	// 跳过本检查（仅留 ReviewPassed 硬前置语义）。base 不可达（amend/rebase 改写历史致 git 对象消失）
	// → fail-open 放行 + 警告：amend 是正常工作流，强复审会死循环；对齐 review/stamp.go 的 fail-open 哲学
	// （可达则严、不可达则松的非对称是设计本意）。
	if gateID == "task-complete" && state.ReviewPassed && state.CompletedAt == nil && state.ReviewedHeadCommit != "" {
		cur, _, err := review.SourceChangesSince(root, state.ReviewedHeadCommit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[task-complete] 警告：审查基线 commit %s 不可达（%v）——历史可能被改写（amend/rebase），advisory 放行；建议重新 forge review pass 刷新基线\n", state.ReviewedHeadCommit, err)
			// fail-open 落盘留痕（非阻塞，Passed=true 表 gate 仍过）：amend 逃审是设计权衡，但
			// score/dashboard 必须能照出"靠 fail-open 而非真复审通过"，事后可追溯，不能只 stderr。
			checklog.Record(root, &checklog.Entry{
				Check:   CheckNameReviewSnapshot,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  fmt.Sprintf("fail-open: 审查基线 %s 不可达（%v）——amend/rebase 致历史改写，放行未重审", state.ReviewedHeadCommit, err),
			})
		} else if cur != state.ReviewedChangeHash {
			return nil, fmt.Errorf("task-complete 拒绝：审查通过后检测到源码变更（基线 HEAD=%s）。请重新派只读子 agent 审查当前代码后运行 `forge review pass` 刷新审查基线", state.ReviewedHeadCommit)
		}
	}

	// docs-consistency advisory (task-complete)：扫用户项目 README 的反引号 forge 命令引用，
	// drift 时 stderr 提醒 + checklog 记录，不阻塞 gate。比 CI 守卫更早——本地 complete 时
	// 就发现，不用等 push 后 CI 才报。检测逻辑在 internal/docsconsistency（cli init 注册
	// 命令树回调打破循环）。Passed=true 表 gate 仍通过，trace 保留 drift 信号。
	if gateID == "task-complete" && state.CompletedAt == nil {
		if drifted := docsconsistency.DriftedInProject(root); len(drifted) > 0 {
			checklog.Record(root, &checklog.Entry{
				Check:   CheckNameDocsConsistency,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  "docs drift: " + strings.Join(drifted, ", "),
			})
			fmt.Fprintf(os.Stderr, "[task-complete] Advisory: 文档一致性 drift——README 反引号引用了不存在的 forge 命令：%s（提交前修复，详见 skills/docs-consistency-guard）\n", strings.Join(drifted, ", "))
		}
	}

	// For auto-gates, run the actual checks
	if gate.Auto {
		result, err := runAutoChecks(root, gateID, state)
		if err != nil {
			return nil, fmt.Errorf("auto-check failed: %w", err)
		}
		return result, nil
	}

	// For non-auto gates, just mark as passed
	// The AI agent is responsible for the actual work via SKILL.md instructions

	// Work activity check for non-auto gates.
	// Gates must not be passed without real work happening between them.
	// Skip for: completed tasks (re-verification) and the final gate (no work phase after it).
	// Note: we intentionally do NOT skip after auto gates. In the 3-gate pipeline,
	// task-verify follows the auto task-implement, and the implement→verify span is exactly
	// where read-before-edit must be enforced. Skipping after auto gates was a 5-gate-era
	// rule that left this check dead in the 3-gate flow (activity never ran).
	if !gate.Auto && state.CompletedAt == nil && len(state.History) > 0 && !isLastGate(gateID) {
		// Measure activity across the whole task span (since task start), not just
		// since the last gate. In the 3-gate pipeline the prior gate (task-implement)
		// is auto and instantaneous, so "since last gate" would see zero activity
		// even when the agent did substantial work earlier in the task.
		since := state.StartedAt

		if state.TaskRef != "" && !getDisableWorkActivity() {
			reads, edits, rerr := toolusage.ReadEditCounts(root, state.TaskRef, since)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "[forge] warning: activity check failed: %v\n", rerr)
			} else if reads+edits > 0 {
				// toollog has data — require at least one Read: the agent must have
				// understood the code before modifying it. Pure edit-without-read is
				// the failure mode. Edit-heavy work is allowed; the read/edit RATIO
				// is reflected in scoring (scope / activity), not a gate —
				// a strict ratio would block normal edit-heavy tasks. The old
				// read-check WARN was sunk to forge-quality Red Flags text per the
				// layered noise treatment.
				if reads == 0 {
					// Race recovery: a Read fired concurrently with `forge task start`
					// may be logged under the previous task's ref (active ref switches
					// only after task start commits) and/or with a timestamp just before
					// StartedAt — both exclude it from ReadEditCounts(taskRef, StartedAt).
					// Re-count Reads across all tasks in a grace window; if any Read
					// happened nearby the agent did read before editing, so treat as
					// satisfied. Hard-fail only when the grace window is also empty
					// (genuine edit-without-read). stderr note keeps the recovery visible.
					if grace, gerr := toolusage.ReadEditCountsGraceWindow(root, since, taskStartReadGraceWindow); gerr != nil {
						fmt.Fprintf(os.Stderr, "[forge] warning: grace read check failed: %v\n", gerr)
					} else if grace > 0 {
						fmt.Fprintf(os.Stderr, "[forge] note: read-before-edit satisfied via grace window (%d nearby Read(s) logged outside this task — task-start/Read race)\n", grace)
					} else {
						return nil, fmt.Errorf(
							"gate %q passed without reading any code during this task (edits=%d). "+
								"Read and understand the code before modifying it",
							gateID, edits,
						)
					}
				}
			} else {
				// toollog empty (older project without auto-compile logging) — fall back to checklog.
				activity, err := checklog.WorkActivity(root, state.TaskRef, since)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[forge] warning: WorkActivity check failed: %v\n", err)
				} else if activity < 1 {
					return nil, fmt.Errorf(
						"gate %q passed without sufficient work activity during this task (%d tool uses, minimum 1). "+
							"Read files, explore code, or write design notes before advancing",
						gateID, activity,
					)
				}
			}
		} else if state.TaskRef != "" && getDisableWorkActivity() {
			// A4: the work-activity gate was bypassed via FORGE_WORK_ACTIVITY=disable.
			// Audit it — the hatch is for testing/escape, but its use must be visible.
			checklog.Record(root, &checklog.Entry{
				Check:   checklog.CheckEscapeHatch,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  "escape-hatch: FORGE_WORK_ACTIVITY=disable (work-activity gate bypassed)",
			})
		}
	}

	// test-coverage gate (v0.25 advisory): 检测"测试伴随变更"（CLAUDE.md rule 4），
	// 缺测试时只 stderr 提醒 + checklog 记录，不再阻塞 gate——适配 loop engineering，
	// 补单测由 agent 自检。CheckTestCoverage 仍调用：scoreTask 的 fallback 复用其判定，
	// 且提醒内容来自 missing。checklog 的 Passed 字段如实反映检测结果（缺测试时
	// Passed=false），让 forge trace 保留信号，只是不再用它阻断会话。
	// Only task-verify runs this — task-complete is the last gate (no work phase).
	if gateID == "task-verify" && state.CompletedAt == nil {
		ok, missing, _ := CheckTestCoverage(root, state)
		checklog.Record(root, &checklog.Entry{
			Check:   CheckNameTestCoverage,
			Passed:  ok,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  testCoverageDetail(ok, missing),
		})
		if !ok {
			fmt.Fprintf(os.Stderr, "[task-verify] Advisory: %s\n", formatMissing(missing))
		}

		// scope-drift advisory (PlanScope whitelist)：任务声明了计划改动白名单时，检测
		// 实改源码是否超出声明。drift = taskChangedFiles(实改态) vs PlanScope(声明态) 的
		// 差集——对应 Terraform drift detection（desired vs actual）。纯 advisory：变更影响
		// 分析召回率仅 ~44%（PASTE 论文），scope 是 prediction 非 contract，drift 是常态信号；
		// 这里只把它从隐性变可度量、可回顾（forge trace / task scope show），绝不阻塞。
		// deterministic（gate 实算 ScopeDrift，agent 无法伪造）。CheckScopeDrift 在
		// BuildEvidenceChain 中被排除——它是 advisory 观测非"验证证据"，计入会虚高 Strength。
		if len(state.PlanScope) > 0 {
			drift := ScopeDrift(taskChangedFiles(root, state), state.PlanScope)
			checklog.Record(root, &checklog.Entry{
				Check:   checklog.CheckScopeDrift,
				Passed:  len(drift) == 0,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  scopeDriftDetail(drift),
			})
			if len(drift) > 0 {
				fmt.Fprintf(os.Stderr, "[task-verify] Advisory: scope-drift——%d 个源码文件超出 PlanScope 声明（advisory 不阻塞；收编: forge task scope add <glob>）\n", len(drift))
				for _, f := range drift {
					fmt.Fprintf(os.Stderr, "  ⚠ %s\n", f)
				}
			}
		}

		// cheat-scan (advisory)：机械检测 5 类 AI 作弊模式（type-suppression /
		// error-swallow / dead-branch / comment-only-fix / comment-as-debt）。前 4 类
		// 此前全靠 LLM 子 agent 在 code-review 时判断——LLM 每轮对同一 diff 重新采样
		// 抓不同子集，是"每轮 review 冒新问题"的体感来源；本扫描把它们抽到
		// deterministic。第 5 类 comment-as-debt 抓"注释标识问题但不解决"（懒惰阶梯
		// 反第 0 级，屎山根源）——下方 nudge 把处置路径（转 forge task 或当场修）明确
		// 告诉 agent。扫任务新增行（+ 行），命中记 checklog:cheat-scan。纯 advisory
		// （启发式有假阳性可能——comment-only 尤甚）绝不阻塞，留痕供 review 核查。
		// CheckCheatScan 在 BuildEvidenceChain 中被排除——它是观测非"验证证据"，计入
		// 会虚高 Strength。LLM-reviewer 据此退到只做语义判断（设计/架构/mock 是否幻觉）。
		cheats := ScanCheatPatterns(root, state)
		checklog.Record(root, &checklog.Entry{
			Check:   checklog.CheckCheatScan,
			Passed:  len(cheats) == 0,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  cheatScanDetail(cheats),
		})
		if len(cheats) > 0 {
			fmt.Fprintf(os.Stderr, "[task-verify] Advisory: cheat-scan 命中 %d 处疑似 AI 作弊模式（advisory 不阻塞；机械检测供 review 核查）\n", len(cheats))
			for _, c := range cheats {
				loc := c.File
				if c.Line > 0 {
					loc = c.File + ":" + strconv.Itoa(c.Line)
				}
				fmt.Fprintf(os.Stderr, "  ⚠ [%s|%s] %s — %s\n", c.Severity, c.Pattern, loc, c.Snippet)
			}
			// comment-as-debt 专属 nudge（B 方案）：注释标识问题 ≠ 解决（懒惰阶梯反第 0
			// 级）。把处置路径明确告诉 agent——转 forge task 跟踪（保留意图、被门禁
			// 追踪）或当场修。不加则 agent 易把低 severity 的 comment-as-debt 当噪音
			// 忽略，"标注了就当做了"。raw string 规避 Windows 输入双引号腐蚀。
			debtCount := 0
			for _, c := range cheats {
				if c.Pattern == CheatCommentDebt {
					debtCount++
				}
			}
			if debtCount > 0 {
				fmt.Fprintf(os.Stderr, `[task-verify] Advisory: %d 处 comment-as-debt——注释标识问题 ≠ 解决（懒惰阶梯反第 0 级）。处置：当场修掉；或转 forge task start --ref <ref> --title <描述> 跟踪（本任务完结后开）。advisory 不阻塞。`+"\n", debtCount)
			}
		}

		// test-capability scan (advisory): 仓库存在可跑的测试时，建议 agent 过 verify
		// 前实际执行。补 task-verify 的"测过没"维度——上面的 test-coverage 只查"测试
		// 伴随变更"（写了测试≠跑过测试），本扫描查"仓库有没有可跑的测试"：有→给推荐
		// 命令建议执行（纯 advisory 不阻塞）；无→静默。与 verify-before-stop.sh（Stop
		// hook 实跑全量）互补：gate 在会话中段提醒，stop 兜底强跑。Passed 恒 true——
		// "仓库有测试"本身不是判定，trace 只保留能力信号。
		cap := CheckTestCapability(root)
		checklog.Record(root, &checklog.Entry{
			Check:   CheckNameTestCapability,
			Passed:  true,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  cap.Detail(),
		})
		if cap.HasTests {
			fmt.Fprintf(os.Stderr, "[task-verify] Advisory: %s\n", cap.Advisory())
		}

		// skill-eval advisory：变更涉及 skills/<name>/ 且该 skill 有 eval case 集 →
		// 建议跑回归。改 description 会让旧 case 集的 DescHash 失配（submit 拒绝），
		// 提醒先 eval-gen --save 重建基准。纯 advisory 不阻塞（Passed 恒 true——
		// "有 case 集"本身非判定，trace 只留信号让 agent 自检）。
		if affected := skillEvalAffected(root, state); len(affected) > 0 {
			checklog.Record(root, &checklog.Entry{
				Check:   CheckNameSkillEval,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  formatSkillEvalAdvisory(affected),
			})
			fmt.Fprintf(os.Stderr, "[task-verify] Advisory: %s\n", formatSkillEvalAdvisory(affected))
		}

		// acceptance advisory（spec-as-gate）：任务登记了验收标准（task start --accept）
		// 但未全部通过 → 提醒先跑 'forge task verify-acceptance' 把 spec 变成实跑证据。
		// 纯 advisory 不阻塞、不 return error。关键：这里**只读 state 上次结果提醒**，
		// 绝不记 CheckNameAcceptance 条目——该条目专属于 verify-acceptance 的真实实跑
		// （deterministic 不可伪造），gate 里不跑命令就不能伪称跑过。
		if state.HasAcceptance() && !state.AllAcceptancePassed() {
			fmt.Fprintf(os.Stderr, "[task-verify] Advisory: 任务登记了 %d 条验收标准但未全部通过——先跑 'forge task verify-acceptance' 实跑回扣（spec-as-gate）\n", len(state.Acceptance))
		}
	}

	// 证据链 agent-claim 数据源：agent 推进一个非自动 gate 即"声明"该阶段完成
	//（task-verify=验证声明，task-complete=完成声明）。与 deterministic 的 hook/gate
	// 实跑检查互补——EvidenceChain 据 Source 分桶，ratio=完成声明背后有多少
	// deterministic 证据支撑，照出"agent 跳过前置就声明完成"的 LLM-judge 盲区。
	// 仅在 gate 实际通过且任务未完成时记录（重检 completed 任务不重复声明）。
	if !gate.Auto && state.CompletedAt == nil {
		switch gateID {
		case "task-verify":
			checklog.Record(root, &checklog.Entry{
				Check:   checklog.CheckTaskVerify,
				Passed:  true,
				Checked: true,
				TaskRef: state.TaskRef,
				Detail:  `agent-claim: 通过 task-verify gate（agent 自述验证完成）`,
			})
		case "task-complete":
			checklog.Record(root, &checklog.Entry{
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

// runAutoChecks executes automated checks for task gates.
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

// hasCodeChanges checks whether there are actual code changes since the task started.
// It checks working-tree changes and, on feature branches, new commits beyond the base branch.
// Gracefully degrades in non-git repos (returns true to avoid false positives).
func hasCodeChanges(root string, state *TaskState) bool {
	// Check 1: working-tree changes (including staged but uncommitted)
	cmd := exec.Command("git", "-C", root, "diff", "--stat", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return true // non-git repo - allow pass
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true
	}

	// Check 2: new commits on feature branch beyond base
	if state != nil && state.Branch != "" && state.Branch != "main" && state.Branch != "master" {
		for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
			cmd = exec.Command("git", "-C", root, "rev-list", "--count", base+"..HEAD")
			out, err = cmd.Output()
			if err == nil {
				return strings.TrimSpace(string(out)) != "0"
			}
		}
		// Could not find any base branch - allow pass
		return true
	}

	// On main/master with no uncommitted changes
	return false
}

// checkImplement runs the (v0.25 advisory) auto-compile + assertion-check hooks,
// records their results to checklog, and verifies code changes exist. The hooks
// are now non-blocking — they emit advisory reminders, never FAIL — so the only
// hard failure left here is "no code changes since task start" (a task semantic,
// not a tech-stack check). The compilePassed/assertPassed branches below are
// retained as defense-in-depth in case a future hook regression reintroduces FAIL.
func checkImplement(root string, state *TaskState) (*ExecuteResult, error) {
	taskRef := ""
	if state != nil {
		taskRef = state.TaskRef
	}

	// 1. Compilation check — via the EMBEDDED auto-compile script, the same source
	// the write-time PostToolUse hook uses (cli.runHook). Reading
	// .forge/hooks/auto-compile.sh from disk instead left the gate inspecting a
	// tamperable copy while the write-time hook inspected the trusted embed, so
	// the two could diverge (a doctored disk script could make the gate pass a
	// broken build the write-time hook would still flag).
	compilePassed, compileOutput := runEmbeddedHook(root, "auto-compile")

	checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckAutoCompile,
		Passed:  compilePassed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  fmt.Sprintf("auto-compile.sh: %s", compileOutput),
	})

	if !compilePassed {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: fmt.Sprintf("build failed: %s", compileOutput),
		}, nil
	}

	// 2. Assertion weakening check — same embedded source as the write-time
	// PreToolUse hook. No disk fallback: the embed is canonical, so a tampered
	// .forge/hooks/assertion-check.sh cannot weaken what the gate enforces.
	assertPassed, assertOutput := runEmbeddedHook(root, "assertion-check")

	checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckAssertion,
		Passed:  assertPassed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  fmt.Sprintf("assertion-check.sh: %s", assertOutput),
	})

	if !assertPassed {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: fmt.Sprintf("assertion check failed: %s", assertOutput),
		}, nil
	}

	// 3. Verify actual code changes exist (not just a pre-compiled base).
	if !hasCodeChanges(root, state) {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: "no code changes detected - build passed but no files modified",
		}, nil
	}

	return &ExecuteResult{
		GateID:  "task-implement",
		Passed:  true,
		Message: "code changes present (compile/assertion advisory via hooks)",
	}, nil
}

// runEmbeddedHook executes an embedded hook script (hooks.EmbeddedContent) by
// writing it to a temp file and running bash on it — mirroring how the
// write-time path (cli.runHook) runs hooks. The gate layer uses the SAME
// embedded source the write-time checks use; reading .forge/hooks/*.sh from
// disk instead left the gate inspecting a tamperable copy that could diverge
// from the trusted embed. root is passed as $1 and the working directory,
// matching the prior disk-hook invocation so scripts resolving the project via
// $1 or $PWD behave identically.
func runEmbeddedHook(root, name string) (passed bool, output string) {
	content, ok := hooks.EmbeddedContent(name)
	if !ok {
		return false, fmt.Sprintf("embedded hook %q not found", name)
	}
	tmp, err := os.CreateTemp("", "forge-gate-*.sh")
	if err != nil {
		return false, fmt.Sprintf("create temp hook file: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return false, fmt.Sprintf("write temp hook file: %v", err)
	}
	tmp.Close()
	// bash reads the file as an argument; no chmod needed (not exec'd directly).

	// Windows: os.CreateTemp 返回反斜杠路径（C:\Users\...\forge-gate-*.sh），bash 把反斜杠当
	// 转义吃掉 → "No such file or directory"，task-implement 的 build 检查因此误判失败。
	// filepath.ToSlash 转正斜杠（Git Bash 可解析）。cmd.Dir 仍用原生 root——Go exec 在
	// Windows 启动 bash 子进程要原生路径做 cwd，bash 自身能处理 Windows cwd。
	cmd := exec.Command("bash", filepath.ToSlash(tmpPath), filepath.ToSlash(root))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return err == nil, strings.TrimSpace(string(out))
}

// getDisableWorkActivity returns whether work activity checking is disabled.
// Set FORGE_WORK_ACTIVITY=disable to skip the check (for testing only).
func getDisableWorkActivity() bool {
	return os.Getenv("FORGE_WORK_ACTIVITY") == "disable"
}

// isPreviousGateAuto returns true if the most recently passed gate is auto.
// Auto gates (e.g. task-implement) are instantaneous system checks - the next
// gate should not require work activity checks since no "work phase" elapsed.
func isPreviousGateAuto(state *TaskState) bool {
	if len(state.History) == 0 {
		return false
	}
	last := state.History[len(state.History)-1]
	g := GateByID(last.Gate)
	return g != nil && g.Auto
}

// isLastGate returns true if the given gate ID is the final gate in the pipeline.
// The final gate (task-complete) has no work phase after it, so
// work activity checks are skipped - there's nothing to "spend time on".
func isLastGate(gateID string) bool {
	gates := DefaultGates()
	return len(gates) > 0 && gates[len(gates)-1].ID == gateID
}

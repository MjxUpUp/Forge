package taskpipeline

// executor_check_verify_advisories.go — ExecuteTaskGate 拆分（refactor/executor-pipeline
// 第一步）：task-verify 的能力/技能/验收 advisory 段（test-capability / skill-eval /
// skill-decisions 双档 / acceptance）。代码体自 executor.go 的 ExecuteTaskGate 原样提取，
// 行为等价——仅变量引用改为参数名。
//
// executor_check_verify_advisories.go — ExecuteTaskGate decomposition
// (refactor/executor-pipeline step 1): task-verify's capability/skill/acceptance advisory
// section (test-capability / skill-eval / the skill-decisions dual tier / acceptance).
// Bodies were extracted verbatim from ExecuteTaskGate in executor.go — behavior-equivalent;
// only variable references became parameter names.

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// adviseTestCapability is the test-capability scan (advisory): when the repo has runnable
// tests, suggest the agent actually execute them before verify. Supplements task-verify's
// 'did you run them' dimension — the test-coverage check only verifies 'tests accompany
// changes' (wrote a test ≠ ran a test); this scan checks 'does the repo have runnable
// tests': yes → emit the recommended command (purely advisory, non-blocking); no → silent.
// The earlier verify-before-stop.sh Stop hook (forcefully ran the full suite at session
// end) has since been removed; this advisory scan is the remaining capability signal.
// Passed is always true — 'the repo has tests' is not itself a verdict; the trace only
// retains the capability signal.
// Plan-5 consistency: the test-coverage escape hatch (per-task override or env) must also
// skip the capability scan — otherwise users on --test-coverage disable still receive the
// 'repo has tests, consider running them' nag, contradicting the 'I am not doing test
// discipline' signal. CheckEscapeHatch is already recorded by CheckTestCoverage above;
// here we only skip the scan + advisory and do not double-record the escape-hatch entry.
//
// adviseTestCapability 是 test-capability 扫描（advisory）：仓库存在可跑的测试时，建议
// agent 过 verify 前实际执行。补 task-verify 的「测过没」维度——test-coverage 只查「测试
// 伴随变更」（写了测试≠跑过测试），本扫描查「仓库有没有可跑的测试」：有→给推荐
// 命令建议执行（纯 advisory 不阻塞）；无→静默。早期 verify-before-stop.sh（Stop
// hook 实跑全量）已删除，本 advisory 扫描是现存的能力信号。Passed 恒 true——
// 「仓库有测试」本身不是判定，trace 只保留能力信号。
// 方案5 一致性：test-coverage 逃生舱（per-task override 或 env）须同时跳过
// capability 扫描——否则 --test-coverage disable 的用户仍收「仓库有测试，建议跑」
// nag，与「我不做测试纪律」信号矛盾。CheckEscapeHatch 已由上方 CheckTestCoverage
// 记录，此处仅跳过扫描+advisory，不重复记 escape-hatch 条目。
func adviseTestCapability(root string, state *TaskState) {
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
}

// adviseSkillEval is the skill-eval advisory: changes touching skills/<name>/ where the
// skill has an eval case set → suggest running regression. Editing the description
// invalidates DescHash of the old case set (submit refuses); remind to eval-gen --save to
// rebuild the baseline. Purely advisory, non-blocking (Passed always true — 'has case set'
// is not itself a verdict; the trace only retains the signal for agent self-check).
//
// adviseSkillEval 是 skill-eval advisory：变更涉及 skills/<name>/ 且该 skill 有 eval case 集 →
// 建议跑回归。改 description 会让旧 case 集的 DescHash 失配（submit 拒绝），
// 提醒先 eval-gen --save 重建基准。纯 advisory 不阻塞（Passed 恒 true——
// 「有 case 集」本身非判定，trace 只留信号让 agent 自检）。
func adviseSkillEval(root string, state *TaskState, gitChanged []string) {
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
}

// checkSkillDecisions is the skill-decisions dual-tier check (component B: advisory
// promoted to guardrail):
//   - guardrail (blocking): a skill whose SKILL.md (behavioral contract) was changed must
//     add a new decisions.md entry in this task, otherwise BLOCKED. SKILL.md is the behavior
//     definition (Use when/SKIP/flow); changing it must record the why (dogfood iron rule:
//     advisory 0 triggers, must be blocking).
//   - advisory (non-blocking): changes to auxiliary resources (scripts/references/cases)
//     only remind — trivial changes concentrate in auxiliary resources, keeping advisory
//     to avoid false positives.
//
// Decision anchor: new `## [d-` entries in decisions.md from base..HEAD (a deterministic
// signal, not semantic guessing). base = state.HeadCommit. escape (per-task override /
// FORGE_SKILL_DECISIONS) → guardrail downgrades to advisory + CheckEscapeHatch (Weak
// ceiling). fail-open: empty/unreachable base does not block.
//
// checkSkillDecisions 是 skill-decisions 双档（B 组件：advisory 升 guardrail）：
//   - guardrail（阻断）：改 SKILL.md（行为契约）的 skill 必须在本 task 新增 decisions.md
//     条目，否则 BLOCKED。SKILL.md 是行为定义（Use when/SKIP/流程），改它必留 why（dogfood
//     铁律：advisory 0 触发，必须 blocking）。
//   - advisory（不阻断）：改辅助资源（scripts/references/cases）只提醒——trivial 改动
//     集中在辅助资源，保持 advisory 不误伤。
//
// 判定锚点：decisions.md base..HEAD 新增 `## [d-` 条目（确定信号，非语义猜测）。base=
// state.HeadCommit。escape（per-task override / FORGE_SKILL_DECISIONS）→ guardrail 降
// advisory + CheckEscapeHatch（Weak ceiling）。fail-open：base 空/不可达不阻断。
func checkSkillDecisions(root string, state *TaskState, gitChanged []string) error {
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
				return GateBlocked(`task-verify 拒绝（HARD stop）：改了 skill %s 的 SKILL.md（行为变更）但本任务未在 decisions.md 新增决策——跑 'forge skills decide --skill <name> --outcome <accept|reject> --diagnosis <为何改> --revision <改了啥> --evidence <依据>' 记录四元组（让下一轮 agent 理解 why）；trivial 改动用 'forge task override --skill-decisions disable' 逃生舱（降 evidence 到 Weak；重证据任务按证据缩放豁免）`, strings.Join(unrecorded, ", "))
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
	return nil
}

// adviseAcceptance is the acceptance advisory (spec-as-gate): the task registered
// acceptance criteria (task start --accept) but they are not all passed → remind to run
// 'forge task verify-acceptance' to turn the spec into actual-run evidence. Purely
// advisory, non-blocking, no return error. Key: this only **reads the last result on
// state to remind** and never records a CheckNameAcceptance entry — that entry is
// exclusive to verify-acceptance's real execution (deterministic, unforgeable); a gate
// that does not run the command must not falsely claim it ran.
//
// adviseAcceptance 是 acceptance advisory（spec-as-gate）：任务登记了验收标准（task start
// --accept）但未全部通过 → 提醒先跑 'forge task verify-acceptance' 把 spec 变成实跑证据。
// 纯 advisory 不阻塞、不 return error。关键：这里**只读 state 上次结果提醒**，
// 绝不记 CheckNameAcceptance 条目——该条目专属于 verify-acceptance 的真实实跑
// （deterministic 不可伪造），gate 里不跑命令就不能伪称跑过。
func adviseAcceptance(state *TaskState) {
	if state.HasAcceptance() && !state.AllAcceptancePassed() {
		fmt.Fprintf(os.Stderr, "%s任务登记了 %d 条验收标准但未全部通过——先跑 'forge task verify-acceptance' 实跑回扣（spec-as-gate）\n", GateAdvisory("[task-verify] "), len(state.Acceptance))
	}
}

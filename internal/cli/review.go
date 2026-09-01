package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/review"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// forge review 让 code-review-gate 从"靠人手动喊"变成"门禁/hook 自动挡"。
//
// 双路径触发（用户 2026-06-27 point 4 设计）：
//   - task 流程：`forge review pass` 写 TaskState.ReviewPassed，由 task-complete
//     门禁强制（见 executor.go 硬前置）。Stop hook 在 task 模式不拦（避免每次改代码都拦）。
//   - 非 task 流程：Stop hook 调 `forge review gate` 判定，未审源码变更则 block。
//
// gate 是判定引擎（纯逻辑 + exit code）；review-stop hook 脚本只做 Claude Code
// Stop 协议适配（见 hooks/embed.go ReviewStopHook）。

func init() {
	rootCmd.AddCommand(reviewCmd)
	reviewCmd.AddCommand(reviewPassCmd)
	reviewCmd.AddCommand(reviewGateCmd)
	reviewCmd.AddCommand(reviewStatusCmd)
	reviewPassCmd.Flags().String("ref", "", "指定任务引用（不依赖活跃任务检测；ref 不存在直接报错，不回落分支 stamp）")
	reviewPassCmd.Flags().String("note", "", "审查结论文本（记入 ReviewRound/stamp 与 checklog 审计留痕）")
	reviewPassCmd.Flags().Bool("acknowledge-changes", false, "距上次审查基线有源码变更时显式确认重盖章（自我承担，checklog 记 self-refresh WARN 审计；正规路径是重派只读子 agent 复审后用 --note 记复审结论）")
	reviewGateCmd.Flags().String("ref", "", "指定任务引用（不依赖活跃任务检测）")
	reviewStatusCmd.Flags().String("ref", "", "指定任务引用（不依赖活跃任务检测）")
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "代码审查通过标记与门禁",
	Long: `forge review 管理 code-review-gate 的"审查通过"状态，支撑自动触发。

两条路径：
  - task 流程：审查通过标记写入任务状态，task-complete 门禁强制（提交前必审）。
  - 非 task 流程：Stop hook 自动拦截未审的源码变更。

子命令：
  pass    标记当前变更已通过 code-review-gate（task 模式写任务状态，否则写分支 stamp）
  gate    判定当前是否需要审查（Stop hook 调用；exit 0=放行，1=需审 block）
  status  显示当前审查状态`,
}

var reviewPassCmd = &cobra.Command{
	Use:   "pass [--ref <ref>] [--note <text>] [--acknowledge-changes]",
	Short: "标记当前变更已通过 code-review-gate（--note 记审查结论到审计留痕；距上次基线有源码变更时需 --note 或 --acknowledge-changes 显式确认）",
	RunE:  runReviewPass,
}

var reviewGateCmd = &cobra.Command{
	Use:   "gate",
	Short: "判定当前是否需要审查（Stop hook 调用）",
	Long: `判定引擎：task 模式放行（审查由 task-complete 门禁管）；非 task 模式按 diff
stamp 决策。输出 PASS/ADVISORY/FAIL 到 stdout，exit 0=放行、1=需审 block。
评估失败 fail-open 放行（forge 安全原则）。`,
	RunE: runReviewGate,
}

var reviewStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "显示当前审查状态",
	RunE:  runReviewStatus,
}

// gateGuidance 是 gate 判定 NEEDS_REVIEW 时输出给 agent 的 Stop additionalContext——
// 指引它加载 skill、派独立子 agent 审查、通过后标记。这是"自动挡"的核心闭环指令。
// 用双引号拼接而非 raw string：内部的反引号（`forge review pass`）在 raw string 里
// 会提前终止字符串（forge-security-hook-fail-open 记的坑），双引号串里则是普通字符。
const gateGuidance = "检测到未审查的源码变更。请按序完成：\n" +
	"1. 加载 code-review-gate skill（经宿主 skill 机制）\n" +
	"2. 按其策略派【只读】子 agent 审查当前 diff——独立上下文是底线，不可自审绕过\n" +
	"3. 审查通过后运行 `forge review pass` 标记，再结束会话\n" +
	"审查发现的问题按类型接 skill：运行时 bug/逻辑错误 → systematic-debugging；编译错误 → compile-fix-loop；断言弱化/假测试 → test-discipline。\n" +
	"注：同一 diff 反复未审最多 block 3 次后 advisory 放行（防 Stop 死循环）。"

func runReviewPass(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")
	note, _ := cmd.Flags().GetString("note")
	acknowledgeChanges, _ := cmd.Flags().GetBool("acknowledge-changes")
	return runReviewPassAt(root, explicitRef, note, acknowledgeChanges)
}

// shortHash 截断 commit hash 用于单行 detail 输出（空保持空）。
func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// runReviewPassAt 是 `forge review pass` 的 root/ref 注入核心（对齐 runTaskComplete
// 的 --ref 模式）：显式 ref 直接加载该任务，ref 不存在直接报错——绝不回落分支
// stamp 分支（那会静默标错对象）。不给 ref 保持旧的活跃任务检测。note（--note）
// 是可选审查结论文本，持久化到追加的 ReviewRound（task 模式）/ stamp（非 task
// 模式）与 checklog review-pass 条目——审计留痕记结论而非只记盖章。
// acknowledgeChanges（--acknowledge-changes）显式确认对变更源码的重盖章（见下方
// 自助刷新守卫）；有内容增量而 note 与确认皆无的重盖章被拒绝。
func runReviewPassAt(root, explicitRef, note string, acknowledgeChanges bool) error {
	// task 模式：写任务状态字段，由 task-complete 门禁消费
	var state *taskpipeline.TaskState
	if explicitRef != "" {
		var err error
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return err
		}
	} else {
		state, _ = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	}
	if state != nil {
		// 绑定审查时的代码快照 (HEAD, 工作区相对 HEAD 的源码变化指纹)——task-complete 门禁据此
		// 强制"审查后改码必复审"。head 取不到 → 传空跳过快照检查（仅留 ReviewPassed 硬前置），
		// pass 是 agent 主导动作故 fail-open。hash 出错同样取空（不阻塞 pass）。
		head := taskpipeline.GetHeadCommit(root)
		hash, _, _ := taskpipeline.TaskFingerprint(root, state, head)

		// 自助刷新基线守卫（2026-08-25 gate-loopholes）：过去距上次盖章基线源码内容
		// 已变时重新盖章会静默刷新基线——agent 被 task-complete「审查通过后检测到
		// 源码变更」HARD 拦下后，自己再跑一遍 `forge review pass` 即可放行，全程无
		// 复审（防君子不防小人）。现在距上次盖章的内容增量（与 task-complete 门禁同
		// 算法：SourceChangesSince(ReviewedHeadCommit) 比对 ReviewedChangeHash）
		// 必须显式确认：--note "<复审结论>"（诚实复审流记结论）或
		// --acknowledge-changes（自我承担，记 WARN self-refresh 审计）。对变更内容
		// 裸盖章一律【拒绝】。非源码变更（amend commit message、保内容 rebase）
		// 指纹不变、无需确认；同状态重复盖章（瞬态重试）保持静默。基线不可达
		// （历史改写）→ fail-open，与 executor 的快照检查一致——但 fail-open
		// 的裸重盖章记 WARN 级 baseline-unreachable 审计（review minor #2：
		// fail-open 不能零留痕，对齐 executor fail-open 落 checklog 的做法）。
		//
		// 丢失更新加固（2026-08-29 审查轮）：load→守卫→盖章→保存 整段改在 per-task
		// 锁内执行（MutateTaskState 锁内重载 state）——此前对锁前快照裸 SaveTaskState
		// 会回滚并发写入的 session-links/DesignPhases，或让盖章本身被并发写者覆盖。
		// 这是 ReviewPassed 硬前置的写入路径。
		selfRefresh := false
		selfRefreshViaNote := false
		baselineUnreachable := ""
		markErr := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
			if s.ReviewedHeadCommit != "" {
				cur, _, err := taskpipeline.TaskFingerprint(root, s, s.ReviewedHeadCommit)
				switch {
				case err != nil:
					baselineUnreachable = s.ReviewedHeadCommit
				case cur != s.ReviewedChangeHash:
					if !acknowledgeChanges && note == "" {
						return fmt.Errorf("review pass 拒绝：距上次审查基线（HEAD=%s）源码已变更——按协议先重派【只读】子 agent 复审当前代码，再 `forge review pass --note \"<复审结论>\"` 盖章；确认变更无需复审（自我承担，记 self-refresh WARN 审计）用 `forge review pass --acknowledge-changes`", s.ReviewedHeadCommit)
					}
					// 对【已变更内容】的任何重盖章都是基线刷新、都必须可审计：
					// --note 与 --acknowledge-changes 都是 forge 无法验证的自供文本，
					// 只标记后者会让对抗 agent 用 --note 把刷新洗成正常 PASS 级条目
					//（2026-08-29 功能探针）。措辞上仍可区分两条路径。
					selfRefresh = true
					selfRefreshViaNote = !acknowledgeChanges
				}
			}
			s.MarkReviewPassedWithNote(head, hash, note)
			state = s
			return nil
		})
		if markErr != nil {
			return markErr
		}
		// 记录 review-pass 事件（第 N 轮 + 审过的快照）——返工轮次度量的原料。observation
		// 类（排除出证据强度分桶）。记录失败不阻塞 pass（fail-open，与打戳本身一致）。
		// 自助刷新（对变更源码确认重盖章）升级为 WARN——审计留痕把自我承担的基线刷新与
		// 普通轮次分开。
		detail := fmt.Sprintf("review round %d passed (head=%s, change=%s)", len(state.ReviewRounds), shortHash(head), shortHash(hash))
		if selfRefresh {
			if selfRefreshViaNote {
				detail = "self-refresh: baseline re-stamped over changed source via --note (self-supplied conclusion, unverified); " + detail
			} else {
				detail = "self-refresh: baseline re-stamped over changed source via --acknowledge-changes; " + detail
			}
		}
		if baselineUnreachable != "" {
			detail = fmt.Sprintf("baseline-unreachable: prior review baseline %s not reachable (history rewritten) — re-stamped fail-open; ", shortHash(baselineUnreachable)) + detail
		}
		if note != "" {
			detail += "; note: " + note
		}
		entry := &checklog.Entry{
			Check:   checklog.CheckReviewPass,
			Passed:  true,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  detail,
		}
		if selfRefresh || baselineUnreachable != "" {
			entry.Level = checklog.LevelWarn
		}
		if recErr := checklog.Record(root, entry); recErr != nil {
			fmt.Fprintf(os.Stderr, "⚠ checklog 记录失败（review-pass 未落盘）: %v\n", recErr)
		}
		fmt.Printf("✅ task %s: code-review-gate 已通过（task-complete 门禁前置满足，基线 HEAD=%s）\n", state.TaskRef, head)
		if selfRefresh {
			if selfRefreshViaNote {
				fmt.Println("⚠ 本次为带 --note 的基线刷新（内容已变更）：--note 是自供文本、forge 无法验证复审真发生过，已记 self-refresh WARN 审计区分于普通轮次。")
			} else {
				fmt.Println("⚠ 本次为自我承担的基线刷新（--acknowledge-changes）：已记 self-refresh WARN 审计。协议要求修复后重派只读子 agent 复审——下次用 --note 记录复审结论。")
			}
		}
		if baselineUnreachable != "" {
			fmt.Printf("⚠ 上次审查基线 %s 不可达（历史可能被 amend/rebase 改写）——本次按 fail-open 重盖章，已记 WARN 级 baseline-unreachable 审计；建议确认当前代码已经过审查\n", shortHash(baselineUnreachable))
		}
		// 上方盖章前自助刷新守卫取代了旧的盖章后复审 ADVISORY（2026-08 协议缺口
		// 修复，2026-08-25 被取代）：对变更内容裸盖章现在直接【拒绝】而非盖完再
		// 念叨；非源码增量（amend、同内容 commit）保持静默，不再只因 head 移动
		// 就发提示。
		//
		// 方案3（blind_spot 触发 · review.go critic 角色）：审查通过是决定性动作。此刻校准证据
		// 强度——若"完成"声明主要靠 agent 自述（Weak/Unverified），发 ADVISORY 提醒本次 review
		// 是盖在盲区证据上的戳。review status 只在被主动查看时显示，agent 可能跳过它直接 pass，
		// 故在 stamp 这一刻再触发：reviewer 须已做 critic 级核验（核声称的验证真跑过），而非只读
		// diff。exit 0（pass 仍成功，逃生合法）；ADVISORY 前缀让 rubber-stamp 可见。方案5 联动：
		// 用了逃生舱的任务 Strength 被 cap 到 Weak，此处自动触发 critic ADVISORY——逃生有代价的
		// 另一面。详见 code-review-gate 步骤 2 前置。
		if ec, err := checklog.ForTask(root, state.TaskRef); err == nil {
			if adv := renderReviewPassBlindSpot(ec); adv != "" {
				fmt.Println(adv)
			}
		}
		return nil
	}

	// 非 task 模式：写分支 stamp
	if err := review.MarkPassedWithNote(root, note); err != nil {
		return fmt.Errorf("failed to mark review passed: %w", err)
	}
	// 记录非 task 模式的 review-pass 事件——stamp 文件每分支只存最新态（原子覆写），
	// 不落 checklog 则非 task 盖章历史完全不可回溯（2026-08 证据：一次 1 分钟完成的
	// 盖章与正常 9-11 分钟的审查深度无从区分）。与 task 模式条目同属 observation
	// 类（排除出证据强度分桶）；与打戳本身一致 fail-open。
	detail := fmt.Sprintf("non-task review passed (branch=%s, diff=%s)", review.CurrentBranch(root), shortHash(currentDiffHash(root)))
	if note != "" {
		detail += "; note: " + note
	}
	if recErr := checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckReviewPass,
		Passed:  true,
		Checked: true,
		Detail:  detail,
	}); recErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ checklog 记录失败（review-pass 未落盘）: %v\n", recErr)
	}
	fmt.Println("✅ 当前 diff: code-review-gate 已通过")
	return nil
}

// currentDiffHash 返回工作区相对 HEAD 的源码指纹，供审计 detail 使用
// （review.SourceChangesSince(root, "")——与打戳背后的 computeDiffHash 同一计算）。
// best-effort：出错返回 ""（detail 降级，绝不阻断）。
func currentDiffHash(root string) string {
	h, _, err := review.SourceChangesSince(root, "")
	if err != nil {
		return ""
	}
	return h
}

// renderReviewPassBlindSpot 产出 `forge review pass` task 模式下、证据弱时的 ADVISORY
// （方案3 blind_spot 触发）。Strong/NoData 返空串（证据可信/无证据可校准 → 不噪声）；
// Weak/Unverified 返 ADVISORY: 前缀行，提醒本次 review stamp 盖在盲区证据上——reviewer 须已
// critic 级核验（核声称的验证真跑过），而非只读 diff。纯函数便于单测（不依赖 findProjectRoot/
// cwd）；runReviewPass 调它，非空则打印。方案5 联动：逃生舱 cap 把本该 Strong 的降为 Weak
// （单一真相源：checklog.EvidenceChain.EscapeDowngradedStrength——本 ADVISORY 从该谓词派生，
// 绝不重复编码规则），故用逃生舱过的任务此处自动触发——逃生不再免费。
func renderReviewPassBlindSpot(ec checklog.EvidenceChain) string {
	if ec.Total() == 0 {
		return ""
	}
	switch ec.Strength() {
	case checklog.Unverified:
		return taskpipeline.GateAdvisory("[review] 审查通过，但本任务零 deterministic 验证证据（agent-claim=%d）——rubber-stamp 高风险。reviewer 须已按 code-review-gate 步骤2前置「必核」做 critic 级核验（逐条确认声称的 test-run/gate 实跑过），否则撤回 pass 补审", ec.AgentClaim)
	case checklog.Weak:
		if ec.EscapeDowngradedStrength() {
			// 方案5 联动：Strength 被 escape-hatch 从 Strong cap 到 Weak——ratio 实际不低
			//（>=0.5），此时"占比低"是假声明（ratio 明明过半）。仅此真 cap 子情形用逃生措辞，
			// 点出真正原因：「完成」靠跳过 gate 撑住，必须 critic 级核验。exit 0（逃生合法）。
			return taskpipeline.GateAdvisory("[review] 审查通过，但本任务用了逃生舱（ratio=%.2f agent-claim=%d 本不弱，「完成」靠跳过 gate 撑住）——reviewer 须已「加核」声称的验证真跑过；建议升级跨模型 critic", ec.Ratio(), ec.AgentClaim)
		}
		// ratio<0.5（无论是否叠加逃生舱）——"占比低"为真声明，不构成假claim。叠加逃生舱时
		// UsedEscapeHatch 已在 checklog 落盘（CheckEscapeHatch 条目）+ review status 可见，此处
		// 不重复以免噪声；pass 刻 ratio 低是完成可信度的主信号。
		return taskpipeline.GateAdvisory("[review] 审查通过，但 deterministic 占比低（ratio=%.2f agent-claim=%d）——reviewer 须已「加核」声称的验证真跑过；证据弱时建议升级跨模型 critic", ec.Ratio(), ec.AgentClaim)
	default:
		return ""
	}
}

func runReviewGate(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		// 非 forge 项目：无 hook 语境意义，放行
		fmt.Println("PASS 非项目根，放行")
		return nil
	}

	// task 模式：审查由 task-complete 门禁强制（ReviewPassed 硬前置），Stop 不拦——
	// 否则 task 流程里每次改代码都被拦，与门禁重复且扰人。
	explicitRef, _ := cmd.Flags().GetString("ref")
	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return err
		}
	} else {
		state, _ = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	}
	if state != nil {
		passed := "未通过"
		if state.ReviewPassed {
			passed = "已通过"
		}
		fmt.Printf("PASS task 模式（%s: ReviewPassed=%s）——审查由 task-complete 门禁强制\n", state.TaskRef, passed)
		return nil
	}

	// 并发会话检测：当前 session 无活跃任务，但存在其他 session 的活跃任务时，
	// 全局 diff 可能来自那个 session 的变更——审查由该任务的 task-complete 门禁
	// 强制，此处不应重复 block。否则调研 session 被要求 review 其他 session 的
	// 代码变更才能结束会话（用户报告的并发问题）。
	if taskpipeline.HasActiveTaskFromOtherSession(root, taskpipeline.CurrentSessionID()) {
		fmt.Println("PASS 检测到其他会话的活跃任务——审查由该任务的 task-complete 门禁强制，此处放行")
		return nil
	}

	// 非 task 模式：diff stamp 决策
	dec, reason, err := review.Evaluate(root)
	if err != nil {
		// fail-open：评估失败不阻塞会话（forge 安全 hook 原则）
		fmt.Printf("PASS 评估失败放行：%v\n", err)
		return nil
	}
	switch dec {
	case review.DecisionPass:
		fmt.Printf("PASS %s\n", reason)
		return nil
	case review.DecisionPassAdvisory:
		fmt.Printf("ADVISORY %s\n", reason)
		return nil
	case review.DecisionNeedReview:
		fmt.Printf("FAIL %s\n", reason)
		fmt.Println()
		fmt.Println(gateGuidance)
		// exit 1 = block（Stop hook 据此 decision:block）；用 os.Exit 绕过 cobra 的
		// "Error:" stderr 噪声，保证 stdout 干净供 hook 当 additionalContext。
		os.Exit(1)
	}
	return nil
}

func runReviewStatus(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")
	return renderReviewStatus(root, explicitRef)
}

// renderReviewStatus 是 `forge review status` 的 root 注入核心，独立出来
// 让 task 模式的证据强度渲染可在临时项目上单测，不依赖 findProjectRoot / cwd。
// 显式 ref 直接加载该任务（ref 不存在即报错，与 review pass --ref 同契约）；
// 空 ref 保持旧的活跃任务检测。
func renderReviewStatus(root, explicitRef string) error {
	var state *taskpipeline.TaskState
	if explicitRef != "" {
		var err error
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return err
		}
	} else {
		state, _ = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	}
	if state != nil {
		fmt.Println("Mode:         task")
		fmt.Printf("Task:         %s\n", state.TaskRef)
		passed := "false"
		if state.ReviewPassed {
			passed = "true"
		}
		fmt.Printf("ReviewPassed: %s\n", passed)
		if state.ReviewPassed && state.ReviewedHeadCommit != "" {
			fmt.Printf("Reviewed at:  HEAD=%s\n", state.ReviewedHeadCommit)
		}
		fmt.Println()
		if state.ReviewPassed {
			// 快照一致性：重算 SourceChangesSince(ReviewedHeadCommit) 比对审查基线——
			// 让"审查后改了码"在 status 就可见（不必等 task-complete 被拒才发现）。
			if state.ReviewedHeadCommit != "" {
				cur, _, err := taskpipeline.TaskFingerprint(root, state, state.ReviewedHeadCommit)
				switch {
				case err != nil:
					fmt.Printf("→ ⚠ 审查基线 HEAD=%s 不可达（%v）——历史可能被改写，建议重新 forge review pass\n", state.ReviewedHeadCommit, err)
				case cur == state.ReviewedChangeHash:
					fmt.Println("→ task-complete 门禁的 review 前置已满足，且审查后源码未变更（✅ 一致）")
				default:
					fmt.Println("→ ⚠ 审查通过后检测到源码变更：task-complete 会拒绝，请重新派只读子 agent 审查后用 `forge review pass --note \"<复审结论>\"` 刷新基线（裸 pass 会被拒；--acknowledge-changes 自我承担并留 self-refresh 审计）")
				}
			} else {
				fmt.Println("→ task-complete 门禁的 review 前置已满足（无审查基线，commit-then-review 流或老 state）")
			}
		} else {
			fmt.Println("→ 未通过：task-complete 前会要求 code-review-gate；运行 `forge review pass` 标记")
		}
		// 证据强度（deterministic 占比）——把 ratio 从可观测升级为驱动 review 校准。
		// Weak/Unverified 时给 reviewer 注入指令：核验声称的验证是否真跑过，对冲 agent
		// 跳过前置就声明完成的盲区。Strong 时静默只报数字（避免噪声）。
		if ec, err := checklog.ForTask(root, state.TaskRef); err == nil && ec.Total() > 0 {
			fmt.Printf(`证据强度:     ratio=%.2f %s（deterministic=%d agent-claim=%d）`+"\n",
				ec.Ratio(), ec.Strength(), ec.Deterministic, ec.AgentClaim)
			switch ec.Strength() {
			case checklog.Unverified:
				fmt.Println(`→ ⚠ 完成声明无 deterministic 证据：review 必须核验声称的验证是否真发生过（test-run / gate 实跑条目），不能只信 agent 自述`)
			case checklog.Weak:
				fmt.Println(`→ ⚠ deterministic 占比低：review 重点核验声称的验证是否真跑过，对冲 agent 跳过前置就声明完成的盲区`)
			}
		}
		return nil
	}

	fmt.Println("Mode:         non-task (branch stamp)")
	out, err := review.CurrentState(root)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

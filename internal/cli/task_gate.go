package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/worktree"
	"github.com/spf13/cobra"
)

// completeGenericTask 完成 generic kind 任务（调研/设计/纯接续）：自动标 3 道门禁 passed
// （History 完整供 list/dashboard 显示，但不跑任何检查——ExecuteTaskGate 对 generic 秒过）+
// MarkComplete + 清 active-task-ref。不评分、不创建 review——generic 任务的价值在持久化的
// 接续字段，不在代码质量门禁。
func completeGenericTask(root string, state *taskpipeline.TaskState) error {
	// Design §5 orchestration completion trigger: a generic task that is an orchestrator (has child
	// tasks pointing at it via ParentTaskRef) is "可 complete" when all children are delivered or
	// terminal (failed/canceled). "不全 delivered 不强制 complete" → this is an ADVISORY warn to
	// stderr, never a block: the orchestrator may legitimately synthesize partial results, but the
	// pending children are surfaced so completing with unfinished work is a deliberate choice, not
	// a silent one. A generic task with NO children (ordinary research/design/handoff) is unaffected.
	// ListTaskState failure is non-fatal — completion must not be gated on the readiness probe.
	//
	// 设计 §5 编排完成触发：作为编排器的 generic 任务（经 ParentTaskRef 有子任务指向它）在全子任务
	// delivered 或终态（failed/canceled）时「可 complete」。「不全 delivered 不强制 complete」→ 此处是
	// 到 stderr 的 advisory 告警，绝不阻断：编排器可合理地综合部分成果，但上浮未交付子任务使「带未完成
	// 工作完成」成为显式选择而非静默。无子任务的 generic 任务（普通调研/设计/接续）不受影响。
	// ListTaskState 失败非致命——完成不应被就绪探测门禁住。
	if states, err := taskpipeline.ListTaskStates(root); err == nil {
		if _, pending := taskpipeline.OrchestrationReady(states, state.TaskRef); len(pending) > 0 {
			fmt.Fprintf(os.Stderr, `⚠ 编排任务 %s 尚有 %d 个子任务未交付/终态: %s；仍可 complete（设计：不强制），建议先综合子任务成果`, state.TaskRef, len(pending), strings.Join(pending, `, `))
			fmt.Fprintln(os.Stderr)
		}
	}
	head := taskpipeline.GetHeadCommit(root)
	// 锁内完成写入：对调用前快照裸 SaveTaskState 会回滚并发写者（本文件已
	// 记录的 §13 丢失更新类）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		for _, g := range taskpipeline.DefaultGates() {
			s.RecordGateResult(g.ID, true, head)
		}
		s.MarkComplete()
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}
	sid := taskpipeline.CurrentSessionID()
	if err := taskpipeline.ClearActiveTaskRef(root, sid); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
	}
	// L1 绑定处理分形态：主检出任务 complete 即解绑（新窗口不再解析到已完结
	// 任务）；worktree 任务【保留】绑定——finish 收尾依赖它（合并 + 清理 + 解绑）。
	// complete 时解绑 worktree 绑定会让 finish 判定「无本目录绑定」而永不清理
	// （review B2 关联缺陷的完整修复）。ActiveTaskState 对已完成任务本就跳过绑定
	// 命中，保留不产生误挂。best-effort。
	if taskpipeline.IsMainCheckout(root) {
		if err := worktree.Clear(root, state.TaskRef); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clear workspace binding: %v\n", err)
		}
	}
	HarnessCommitBestEffort("task completed: " + state.TaskRef)
	return nil
}

func runTaskGate(cmd *cobra.Command, args []string) error {
	gateID := args[0]
	silent, _ := cmd.Flags().GetBool("silent")
	explicitRef, _ := cmd.Flags().GetString("ref")

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			if silent {
				return nil
			}
			return err
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			if silent {
				return nil
			}
			return fmt.Errorf("failed to load task state: %w", err)
		}
	}
	if state == nil {
		if silent {
			return nil // No task — silent exit (for hook compatibility)
		}
		return fmt.Errorf("no active task. Run 'forge task start' first")
	}

	// 校验 gate 存在
	gate := taskpipeline.GateByID(gateID)
	if gate == nil {
		return fmt.Errorf("unknown task gate: %s (valid: %s)", gateID, strings.Join(taskpipeline.GateIDs(), ", "))
	}

	result, err := taskpipeline.ExecuteTaskGate(root, gateID, state)
	if err != nil {
		return err
	}

	// Token 成本熔断（advisory）：task 累计估算 token 超阈值则警示。让 token 计量不止于
	// forge trace 可观测，而是 task gate 推进时的成本上限信号（loop engineering 成本治理）。
	if w, _ := toolusage.TaskTokenBreaker(root, state.TaskRef); w != "" {
		fmt.Fprintf(os.Stderr, "⚠️ [breaker] %s\n", w)
	}

	// Assignment advisory (P2 of the 2026-08-18 脱节修复): gating a task that is offered to
	// ANOTHER agent and never claimed is the pipeline/assignment drift precursor — warn but
	// never block (orchestrator proxying is legitimate).
	//
	// 分派 advisory（2026-08-18 脱节修复的 P2）：给「分派给另一个 agent 且从未认领」的任务
	// 过门禁正是管线/分派脱节的前兆——提醒但绝不阻断（编排器代跑合法）。
	adviseUnclaimedAssignment(root, gateID, result.Passed, state)

	// 节点租约 advisory（sync-convergence.md §4）：给他机持有活跃租约的任务过门禁
	// 是双机互踩的前兆——提醒但绝不阻断（TTL 租约管 UX 不管正确性）。
	if ls := taskpipeline.LeaseStatusForCurrentNode(state); ls.ForeignActive {
		fmt.Fprintf(os.Stderr, "⚠️ [lease] %s\n", ls.Message)
	}

	// 刻意不在此处 MarkComplete（dogfood 2026-08-18 死锁修复）：曾经「最后一道 gate 通过
	// 即 MarkComplete」，而 ActiveTaskState 对 CompletedAt!=nil 返回 nil —— 紧随其后的
	// `forge task complete` acceptance pre-flight 恰要求快照新鲜（时为 AcceptedHeadCommit==HEAD；
	// 2026-08-25 起为任务 HeadCommit 锚定的源码内容指纹，见 acceptance.go），
	// 刷新只能由 verify-acceptance（默认认 active task，可用 --ref 显式指定）完成。门一过
	// 任务即失活 → 验收刷新死锁（本次 v2 任务实测踩中：review 修复 commit 移动 HEAD 后
	// complete 永久 BLOCKED，且无任何 CLI 路径可复活）。完成 = `forge task complete` 的
	// 整个动作（pre-flight → MarkComplete → 评分 → 反馈 → 清 ref）；门禁全过只是它的
	// 前置条件，不是完成本身。
	//
	// 锁内门禁结果写入：runTaskGate 顶部的 load 早于可能长达分钟级的 ExecuteTaskGate
	// 执行——保存那个陈旧快照会静默回滚期间落盘的并发 session-link/盖章/决策写入
	//（§13 丢失更新；写入路径必须合并进锁内状态）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		// GetHeadCommit（短 hash）是记录 head 的单一真相源——归因与 doc-gate 对
		// History head 做精确字符串比较。
		s.RecordGateResult(gateID, result.Passed, taskpipeline.GetHeadCommit(root))
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}

	if !silent {
		if result.Passed {
			fmt.Printf("  ✅ %s — passed\n", gate.Name)
		} else {
			fmt.Printf("  ❌ %s — BLOCKED: %s\n", gate.Name, result.Message)
		}
	}

	if !result.Passed {
		return fmt.Errorf("task gate %s failed", gateID)
	}

	return nil
}

// runTaskVerifyAcceptance 实跑任务登记的每条验收标准（task start --accept），按
// 「退出码 0 + Expected 子串」判定，回填 Passed/Output 到 TaskState 并记一条
// checklog:acceptance（deterministic——forge 自己跑命令看结果，不可伪造）。这是把
// dev-workflow Plan 的"Run: <cmd>, Expected: <out>"变成不可伪造实跑证据的入口，
// 对冲 agent 自述「满足验收」却没真跑的盲区（spec-as-gate）。失败不阻塞会话，仅返回 error
// 让调用方/脚本感知退出码；Passed 字段如实落盘 + checklog，forge trace 可见。
func runTaskVerifyAcceptance(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	trustForeign, _ := cmd.Flags().GetBool(`trust-foreign`)
	explicitRef, _ := cmd.Flags().GetString("ref")
	return runTaskVerifyAcceptanceAt(root, explicitRef, trustForeign)
}

// stdinIsHumanTerminal 报告 stdin 是否挂在真人终端（char device）上——--trust-foreign 受信门
// 用来阻止 LLM agent（其 Bash 生成的 stdin 是管道）照拒绝文案自己的指引自我受信外来验收
// 命令的判别器。用变量（非函数）以便测试注入两侧。
//
// 已知局限（2026-08-16 复审）：mintty（Git Bash 默认终端）给原生进程的 stdin 是命名管道而非
// char device，真人在该终端下同样会被拒。刻意不提供就地逃生舱——任何 env/flag 旁路被注入的
// agent 同样设得了，等于重新打开本门要堵的自我受信洞。拒绝信息会探测 TERM_PROGRAM=mintty
// 并指引用户改用 ConPTY 终端（Windows Terminal / PowerShell）重跑。
var stdinIsHumanTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// runTaskVerifyAcceptanceAt 是 runTaskVerifyAcceptance 的 root 注入核心，独立出来便于
// 在临时项目上单测（不经 findProjectRoot / cobra）。实跑任务登记的每条验收标准
// （task start --accept），按「退出码 0 + Expected 子串」判定，回填 Passed/Output 到
// TaskState 并记一条 checklog:acceptance（deterministic——forge 自己跑命令看结果，不可伪造）。
// 这是把 dev-workflow Plan 的"Run: <cmd>, Expected: <out>"变成不可伪造实跑证据的入口，
// 对冲 agent 自述「满足验收」却没真跑的盲区（spec-as-gate）。explicitRef 直接指定任务
// （门禁族 --ref 一致性）；空串保持旧的活跃任务检测。
func runTaskVerifyAcceptanceAt(root, explicitRef string, trustForeign bool) error {
	var state *taskpipeline.TaskState
	var err error
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return fmt.Errorf("加载任务 %q 失败: %w", explicitRef, err)
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return fmt.Errorf("failed to load task state: %w", err)
		}
	}
	if state == nil {
		return fmt.Errorf("no active task. Run 'forge task start' first（或用 --ref 指定任务）")
	}
	if !state.HasAcceptance() {
		fmt.Println("本任务未登记验收标准（forge task start --accept \"run :: expected\"）。")
		return nil
	}

	// 外来验收受信门：经 task import 或 .forge migrate 进入本 TaskState 的 Run 命令是攻击者可
	// 书写的可执行字符串（clone 恶意仓库 / 敌意 bundle）。verify-acceptance 以完整环境执行它们
	// ——未审阅就跑外来命令等于被本工具自己的 BLOCKED 指引导向的任意命令执行。故首次执行要求
	// 显式的、基于审阅的受信：无 --trust-foreign 时打印命令清单供人工审阅并拒绝（标记已在盘上，
	// 未受信的拒绝天然持久——该分支无须落盘）；带 flag 则在 per-task 锁内一次性清除标记（之后
	// 的重跑是本机验证证据，非外来载荷）。
	//
	// --trust-foreign 额外要求真人终端（stdin 是 char device）：本修复的威胁模型正是恶意内容
	// 操纵 LLM agent，而被注入的 agent 大可直接照拒绝文案的指引加上 flag——char device 检查是
	// 区分人类 shell 与 agent 管道 stdin 的唯一判别器。
	if state.AcceptanceForeign && !trustForeign {
		fmt.Println("⚠ 本任务的验收命令来自外来源（task import / .forge migrate），未审阅前拒绝执行。命令清单：")
		for i, c := range state.Acceptance {
			fmt.Printf("  [%d] %s\n", i+1, c.Run)
		}
		fmt.Println("人工审阅以上命令后，在真人终端中加 --trust-foreign 重新执行；确认无害前不要受信。")
		return fmt.Errorf("acceptance commands are foreign-marked; review them then re-run with --trust-foreign in a human terminal")
	}
	trusted := state.AcceptanceForeign && trustForeign
	if trusted && !stdinIsHumanTerminal() {
		// mintty（Git Bash 默认终端）给原生进程的 stdin 是命名管道而非 char device——真人
		// 在此同样被拒，且不提供就地逃生舱：agent 也能设置的 env/flag 旁路会重新打开本门
		// 要堵的自我受信洞。可行动路径是换 ConPTY 终端。
		if os.Getenv(`TERM_PROGRAM`) == `mintty` {
			return fmt.Errorf("--trust-foreign 须真人在终端运行：Git Bash/mintty 的 stdin 是命名管道（非 char device），无法与 agent 管道区分——请改用 Windows Terminal / PowerShell 等 ConPTY 终端执行本命令")
		}
		return fmt.Errorf("--trust-foreign 是人工审阅决策：须由真人在终端中运行（当前 stdin 非终端——agent/管道环境不得自我受信外来命令）")
	}
	// 注意：外来标记改为实跑之后、随下方结果合并一并清除——先清后跑会让跑到一半崩溃的下次
	// 尝试在无受信的情况下执行已去标记的外来命令；改为 fail-closed（崩溃后标记仍在，重跑
	// 重新要求 --trust-foreign）。

	taskpipeline.VerifyAcceptance(root, state)
	allPassed := state.AllAcceptancePassed()

	if recErr := checklog.Record(root, &checklog.Entry{
		Check:   taskpipeline.CheckNameAcceptance,
		Passed:  allPassed,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  formatAcceptanceDetail(state.Acceptance),
	}); recErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ checklog 记录失败（验收证据未落盘）: %v\n", recErr)
	}

	// 在 per-task 锁内把验收「结果」合并到最新盘上状态（设计§13）：裸 SaveTaskState 回写实跑前
	// 快照会覆盖并发 resume/decide 的接续写入——丢的恰是 import 要保的数据。MutateTaskState
	// 锁内重载；结果按 (Run, Expected) 二元组匹配到最新 acceptance spec 上，并发改过的 spec
	// 不会被盖上另一条命令的结果；外来标记（受信分支）也在此翻——实跑之后、fail-closed（见上 NOTE）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		taskpipeline.MergeAcceptanceResults(s, state.Acceptance, trusted)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save acceptance results: %w", err)
	}
	if trusted {
		fmt.Println("已受信外来验收命令（--trust-foreign）——验收实跑完成、外来标记已清除，后续重跑按本机证据处理。")
	}

	fmt.Println("验收标准实跑结果：")
	for i, c := range state.Acceptance {
		mark := "✅"
		if !c.Passed {
			mark = "❌"
		}
		exp := c.Expected
		if exp == "" {
			exp = "(退出码 0)"
		}
		fmt.Printf("  %s [%d] %s :: %s\n", mark, i+1, c.Run, exp)
		if !c.Passed && c.Output != "" {
			for _, line := range splitLines(c.Output) {
				fmt.Printf("     %s\n", line)
			}
		}
	}
	fmt.Println(strings.Repeat("─", 40))
	if allPassed {
		fmt.Printf("✅ 全部通过 — 真实结果已记为 deterministic 证据（checklog: %s）\n", taskpipeline.CheckNameAcceptance)
		return nil
	}
	fmt.Printf("❌ 存在未通过项 — 失败结果已记入 checklog（%s）\n", taskpipeline.CheckNameAcceptance)
	return fmt.Errorf("acceptance verification failed")
}

// formatAcceptanceDetail 生成 checklog:acceptance 的 Detail 摘要——「PASS/FAIL — k/n 通过」，
// 让 forge trace 不展开每条也能一眼看出验收整体结果。
func formatAcceptanceDetail(cs []taskpipeline.AcceptanceCriterion) string {
	passed := 0
	for _, c := range cs {
		if c.Passed {
			passed++
		}
	}
	word := `FAIL`
	if passed == len(cs) {
		word = `PASS`
	}
	return fmt.Sprintf("%s — %d/%d 验收标准通过", word, passed, len(cs))
}

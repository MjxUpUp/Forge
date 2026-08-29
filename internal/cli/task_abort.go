package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/worktree"
	"github.com/spf13/cobra"
)

// runTaskAbort 移除任务但不评分。删除 task state 文件（DataDir/tasks/<ref>.json），
// 当 session-scoped active-task-ref 指向被 abort 的 task 时也清掉。这是给卡死或
// 永远过不了门禁的 ghost task 的逃生舱——如在非 git 项目启动的 task，或半途放弃的
// task。与 `task complete` 不同，abort 绝不评分、绝不创建 review，项目质量记录不被
// 放弃的尝试污染。
//
// task 实际做的 code/commit 改动不动——abort 只回收 forge state。后续可用同 ref 重新 start。
func runTaskAbort(cmd *cobra.Command, args []string) error {
	explicitRef, _ := cmd.Flags().GetString("ref")
	asJSON, _ := cmd.Flags().GetBool("json")

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	// 解析要 abort 的 task：显式 --ref 优先，否则取 session 的 active task。
	// 两者皆无则无可识别。
	taskRef := explicitRef
	if taskRef == "" {
		state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return fmt.Errorf("failed to load task state: %w", err)
		}
		if state != nil {
			taskRef = state.TaskRef
		}
	}
	if taskRef == "" {
		return fmt.Errorf("no task to abort. Specify --ref <task-ref> or run on a branch with an active task")
	}

	cascade, _ := cmd.Flags().GetBool(`cascade`)
	detachDeps, _ := cmd.Flags().GetBool(`detach-deps`)
	// The two flags are the two non-default branches of the design §4 "three-way" abort of a task
	// others depend on. Mutually exclusive: --cascade deletes the dependents, --detach-deps keeps
	// them but removes the now-dangling edge. Default = neither: abort only + warn, the milestone
	// "废弃链被提示" behavior. The agent-neutral CLI surface replaces the design's interactive
	// three-way prompt (an agent driving forge cannot answer an interactive stdin prompt reliably).
	//
	// 两个 flag 是设计§4 abort 被依赖任务「三选一」的两个非默认分支，互斥：--cascade 删依赖方，
	// --detach-deps 留依赖方但摘掉悬空边。默认两者皆不传：仅 abort + warn，里程碑「废弃链被提示」行为。
	// agent-neutral CLI 表层替代了设计的交互式三选一 prompt（驱动 forge 的 agent 无法可靠回答交互 stdin）。
	if cascade && detachDeps {
		return fmt.Errorf(`--cascade 与 --detach-deps 互斥：--cascade 一并 abort 依赖方，--detach-deps 仅摘依赖边保留依赖方`)
	}

	// 删除前 load，让报告能说明 task 是否完成、并保留 branch 给用户心智模型。
	// 文件缺失不致命：stale active-task-ref 可能指向已不存在的 task，仍需清掉悬空指针。
	var state *taskpipeline.TaskState
	if loaded, err := taskpipeline.LoadTaskState(root, taskRef); err == nil {
		state = loaded
	}

	// 反向依赖扫描（设计阶段3 + §4 三选一）：abort 一个被其他 task DependsOn 的 task，会让依赖方永远阻塞
	// （门禁报该 ref 缺失/未交付且永不放行）。默认不级联 abort——依赖方在上游重指后可能仍有价值——但暴露
	// 悬空边。--cascade abort 整个传递闭包；--detach-deps 仅摘边。delete 前算，使刚 abort 的 state 仍可扫。
	//
	// 已知限制（多仓 workspace Option B）：扫描只读本仓 task——位于另一个成员仓的依赖方（其
	// DependsOn 经 key:ref 指向本仓）对 abort 不可见：不警告、不级联、不摘边；其门禁会把被
	// abort 的 key:ref 永久计 pending，直到有人到那个仓摘掉该边。跨仓清理刻意不做（反向索引
	// 需要跨 DataDir 扫描 + 远端改写）；下方在本 repo 属于多仓 workspace 时打一行提示暴露该
	// 盲区。
	dependentsMap := map[string][]string{}
	allStates, listErr := taskpipeline.ListTaskStates(root)
	if listErr == nil {
		for _, t := range allStates {
			if t == nil {
				continue
			}
			// 已完成的依赖方早已过门禁，承载项目已沉淀的评分/质量记录——级联的
			// 存在意义是清掉永远过不了门的链，不是销毁已完成的历史（abort 自己
			// 的文档承诺质量记录不被放弃的尝试污染）。
			if t.CompletedAt != nil {
				continue
			}
			for _, d := range t.DependsOn {
				dependentsMap[d] = append(dependentsMap[d], t.TaskRef)
			}
		}
	}
	// H1: --cascade/--detach-deps 是明确的「处理依赖」意图（非默认 warn 流程）。ListTaskStates 失败时
	// 若静默跳过，空的 dependentsMap 会让级联/解绑降级为 no-op，而主 abort 仍成功——用户误以为依赖已处理，
	// 实际停滞的依赖任务原样留存（JSON 还省略 cascaded 字段）。故扫描失败时对这两个 flag 明确报错，让用户
	// 知道未执行并重试；默认 warn 流程不受影响（仍尽力 abort 主任务）。
	if listErr != nil && (cascade || detachDeps) {
		return fmt.Errorf(`扫描反向依赖失败，未执行 --cascade/--detach-deps：%v（请重试）`, listErr)
	}
	var dependents []string
	for _, dep := range dependentsMap[taskRef] {
		dependents = append(dependents, dep)
	}
	// cascade 闭包：传递依赖方（直接 + 间接），对反向 map 广度优先。依赖方上游已没，门禁永远过不了，
	// 故 cascade 清除死链。cascaded = 试图集（BFS 闭包，驱动删除循环）；cascadedDone = 实际删除成功的。
	// 删除可能因权限/Windows 文件锁失败——该依赖方在循环内发一条内联 per-item stderr Warning（并非后续回读
	// `cascaded`），且不计入 cascadedDone，故绝不能报进 JSON 的已 abort，否则 JSON 消费者会以为一个仍在
	// 盘上的任务已没了。
	var cascaded, cascadedDone []string
	if cascade {
		visited := map[string]bool{}
		queue := append([]string{}, dependents...)
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if visited[cur] {
				continue
			}
			visited[cur] = true
			cascaded = append(cascaded, cur)
			queue = append(queue, dependentsMap[cur]...)
		}
	}

	// 删除 task state 文件。ENOENT 可接受（已删除 / stale ref）。
	if err := taskpipeline.DeleteTaskState(root, taskRef); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete task state: %w", err)
	}

	// 若 active-task-ref 仍指向被 abort 的 task 则清掉。
	// session-scoped，并发 session 不受干扰。
	sid := taskpipeline.CurrentSessionID()
	if ref := taskpipeline.ReadActiveTaskRef(root, sid); ref == taskRef {
		if err := taskpipeline.ClearActiveTaskRef(root, sid); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
		}
	}
	// L1（#4 深挖修订）：abort 按任务清扫【全部】绑定——任务即将删除，任何指向
	// 它的绑定（含其 worktree 的 wtid 键控绑定，abort 常从主检出发起、cwd 键控
	// 的 Clear 够不到）都是死锚。best-effort。
	if err := worktree.ClearAllForTask(root, taskRef); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear workspace bindings: %v\n", err)
	}

	// --cascade：abort 每个传递依赖方并清其 active-task-ref。在主 delete 之后；各 DeleteTaskState 容忍 ENOENT。
	//
	// M3 已知限制（follow-up）：DeleteTaskState 是 os.Remove 无任务锁，与并发的 MutateTaskState 存在 TOCTOU——
	// 若另一 forge 进程正持有某级联目标的任务锁、已 load 准备 save，我们的 remove 后其 save 会重建文件，
	// 级联「成功」但目标复现。主 abort 同样如此；--cascade 把窗口放大 N 倍。彻底修复需 DeleteTaskState 走
	// LockTask+remove+unlock，本任务不扩范围。
	for _, depRef := range cascaded {
		if err := taskpipeline.DeleteTaskState(root, depRef); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, `Warning: failed to cascade-abort %s: %v`+"\n", depRef, err)
			continue // 删除失败：留 stderr Warning，不计入 cascadedDone（JSON 只报实际已删）
		}
		cascadedDone = append(cascadedDone, depRef)
		if ref := taskpipeline.ReadActiveTaskRef(root, sid); ref == depRef {
			// 清依赖方的 active-task-ref 失败时记 stderr 而非吞掉：该 ref 仍指向一个已删任务，会让
			// 下一次 `forge task status` / resume 锚定到幽灵。删除本身已成功，故不计入 cascade 失败，
			// 只作为非致命告警暴露出来。
			if err := taskpipeline.ClearActiveTaskRef(root, sid); err != nil {
				fmt.Fprintf(os.Stderr, `Warning: cascade-aborted %s but failed to clear its active task ref: %v`+"\n", depRef, err)
			}
		}
	}

	// --detach-deps：从每个直接依赖方摘掉这条悬空边，保留依赖方任务（它可能仍有价值，只是不再等这个已
	// abort 的上游）。
	var detached []string
	if detachDeps {
		for _, depRef := range dependents {
			err := taskpipeline.MutateTaskState(root, depRef, func(s *taskpipeline.TaskState) error {
				var kept []string
				for _, d := range s.DependsOn {
					if d != taskRef {
						kept = append(kept, d)
					}
				}
				s.DependsOn = kept
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, `Warning: failed to detach dep edge on %s: %v`+"\n", depRef, err)
				continue
			}
			detached = append(detached, depRef)
		}
	}

	// M1: dependents/cascaded/detached 的原始顺序随 ListTaskStates 的 ReadDir（跨平台 FS 顺序不定），
	// 排序后输出稳定——JSON 消费者不因遍历顺序间歇失败，stderr warn 也确定。级联最终状态与顺序无关。
	slices.Sort(dependents)
	// cascaded（试图集）此处不排序：它仅作删除循环的迭代源（task.go:823，循环在排序之前），删除失败者
	// 已在循环内发内联 per-item stderr Warning，排序后既不入 JSON（JSON 只取 cascadedDone）也不入任何汇总行。
	slices.Sort(cascadedDone)
	slices.Sort(detached)

	if asJSON {
		out := map[string]any{
			"task_ref": taskRef,
			"aborted":  true,
		}
		if state != nil {
			out["was_complete"] = state.IsComplete()
			out["branch"] = state.Branch
		}
		if len(dependents) > 0 {
			out[`dependents_blocked`] = dependents
		}
		// JSON 的 `cascaded` 只报实际删除成功的（cascadedDone）——失败的已走 stderr Warning，
		// 报进 JSON 会让编排 agent 误以为一个仍在盘上的任务已 abort。
		if len(cascadedDone) > 0 {
			out[`cascaded`] = cascadedDone
		}
		if len(detached) > 0 {
			out[`detached`] = detached
		}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("Task aborted: %s\n", taskRef)
	if state != nil {
		if state.IsComplete() {
			fmt.Printf("Note: task had already passed all gates; its scored state was removed.\n")
		}
		if state.Branch != "" {
			fmt.Printf("Branch: %s (left untouched — abort only removes forge state)\n", state.Branch)
		}
	}
	// 汇总行只报成功删除（cascadedDone），与 JSON 路径一致——失败的删除上方已有独立的"Warning: failed to
	// cascade-abort X"；此处再当"已 abort"列出会让一个仍在盘上的任务看起来已完成（正是 cascaded/cascadedDone
	// 拆分要堵的泄露）。
	if len(cascadedDone) > 0 {
		fmt.Fprintf(os.Stderr, `Cascade-aborted %d dependent(s): %s`+"\n", len(cascadedDone), strings.Join(cascadedDone, `, `))
	}
	if len(detached) > 0 {
		fmt.Fprintf(os.Stderr, `Detached dep edge on %d dependent(s): %s`+"\n", len(detached), strings.Join(detached, `, `))
	}
	if len(dependents) > 0 && !cascade && !detachDeps {
		fmt.Fprintf(os.Stderr, `Warning: %d task(s) depend on this one (%s); their gate will now block on a missing upstream. Re-run with --cascade to abort them too, or --detach-deps to unlink them.`+"\n", len(dependents), strings.Join(dependents, `, `))
	}
	// 跨仓盲区（见上方扫描处的 KNOWN LIMITATION 注释）：仅当本 repo 属于多仓 workspace 时
	// 提示一句——扫描本身仍只覆盖本仓。
	if multiRepoMembership(root) {
		fmt.Fprintf(os.Stderr, "Note: 跨仓依赖方（他仓 task 以 key:ref 依赖 %s）不在本次扫描内；若存在，其门禁会把本 ref 永久计 pending——需到对应 repo 摘掉依赖边（forge workspace doctor 可检跨仓环）\n", taskRef)
	}
	fmt.Println("Code changes are untouched. Re-start with: forge task start --ref " + taskRef)
	return nil
}

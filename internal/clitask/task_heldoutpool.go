package clitask

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_heldoutpool.go — oracle-pipeline L3 的池操作入口：`forge task
// heldout-pool`（动作互斥 flag 族，task artifact 同款命令面纪律）。刻意不给
// task start 加 flag（命令面冻结）——抽取经 --apply 写当前任务的 held-out
// 侧车，与 start --heldout 文件形态同汇合点（SaveHeldout）。

var taskHeldoutPoolCmd = &cobra.Command{
	Use:   "heldout-pool --add <file> --name <名> [--tag <组>] | --list [--tag <组>] | --draw N [--tag <组>] [--apply]",
	Short: "held-out 池操作：沉淀业务坏天气集 / 列池 / 确定性抽取并入当前任务保留集（L3 独立出题）",
	Long: `held-out 池（oracle-pipeline L3）：把「出保留题」从每任务一次性变成项目资产——

  --add <file> --name <名> [--tag <组>]：沉淀一套保留题（文件形态同
    task start --heldout：每行 "run :: expected"，# 注释）；同名拒绝静默覆盖
  --list [--tag <组>]：列池（名/标签/条数）
  --draw N [--tag <组>] [--apply]：名序确定性抽 N 套；--apply 并入当前活跃
    任务的 held-out 侧车（按 Run 去重；verify-acceptance 实跑双套件记 gap）

「测试的测试」是人工边界：池里的题必须人出（业务坏天气：金额边界/并发/
退款链），forge 只负责沉淀与抽取的确定性。`,
	RunE: runTaskHeldoutPool,
}

func init() {
	Root.AddCommand(taskHeldoutPoolCmd)
	taskHeldoutPoolCmd.Flags().String("ref", "", "指定任务（--apply 并入该任务侧车；缺省取活跃任务）")
	taskHeldoutPoolCmd.Flags().String("add", "", "沉淀：<file>（每行 run :: expected）")
	taskHeldoutPoolCmd.Flags().String("name", "", "--add 的池集名（必填）")
	taskHeldoutPoolCmd.Flags().String("tag", "", "分组标签（money/concurrency/…；--list/--draw 过滤用）")
	taskHeldoutPoolCmd.Flags().Bool("list", false, "列池")
	taskHeldoutPoolCmd.Flags().Int("draw", 0, "抽取套数（名序确定性取前 N）")
	taskHeldoutPoolCmd.Flags().Bool("apply", false, "--draw 并入当前活跃任务的 held-out 侧车")
}

func runTaskHeldoutPool(cmd *cobra.Command, args []string) error {
	addFile, _ := cmd.Flags().GetString("add")
	name, _ := cmd.Flags().GetString("name")
	tag, _ := cmd.Flags().GetString("tag")
	list, _ := cmd.Flags().GetBool("list")
	draw, _ := cmd.Flags().GetInt("draw")
	apply, _ := cmd.Flags().GetBool("apply")
	actions := 0
	for _, on := range []bool{addFile != "", list, draw > 0} {
		if on {
			actions++
		}
	}
	if actions != 1 {
		return fmt.Errorf("恰好一个动作：--add <file> --name <名> | --list | --draw N [--apply]")
	}
	// 项目级动作（--add/--list/--draw）不依赖任务存在（审查 P2-5：池的目标用户
	// 是业务方/验收人——无任务状态下沉淀/列池是主路径）；仅 --apply 需要任务。
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	switch {
	case addFile != "":
		raw, rerr := os.ReadFile(addFile)
		if rerr != nil {
			return fmt.Errorf("读取 %q 失败: %w", addFile, rerr)
		}
		var lines []string
		for _, l := range strings.Split(string(raw), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			return fmt.Errorf("%q 无有效条目（每行 \"run :: expected\"，# 注释）", addFile)
		}
		criteria := taskpipeline.ParseAcceptance(lines)
		if err := taskpipeline.AddHeldoutSet(root, taskpipeline.HeldoutSet{Name: name, Tag: tag, Criteria: criteria}); err != nil {
			return err
		}
		fmt.Printf("✓ 已沉淀池集 %s（tag=%q，%d 条）→ heldout-pool/%s.json\n", name, tag, len(criteria), name)
		return nil
	case list:
		sets := taskpipeline.ListHeldoutSets(root, tag)
		if len(sets) == 0 {
			fmt.Println("held-out 池为空（--add 沉淀第一套业务坏天气集）")
			return nil
		}
		fmt.Printf("held-out 池（%d 套，tag=%q）：\n", len(sets), tag)
		for _, s := range sets {
			fmt.Printf("  %-24s tag=%-14s %d 条\n", s.Name, s.Tag, len(s.Criteria))
		}
		return nil
	default:
		sets, derr := taskpipeline.DrawHeldoutSets(root, tag, draw)
		if derr != nil {
			return derr
		}
		fmt.Printf("抽取 %d 套（名序确定性）：\n", len(sets))
		for _, s := range sets {
			fmt.Printf("  - %s（tag=%q，%d 条）\n", s.Name, s.Tag, len(s.Criteria))
		}
		if !apply {
			fmt.Println("→ 加 --apply 并入当前活跃任务的 held-out 侧车")
			return nil
		}
		// --apply 才需要任务；显式 --ref 优先，缺省活跃任务（审查 P2-5 的
		// 反面：apply 是任务级动作）。完成态检查用【 ApplyHeldoutToTask 前】
		// 的最新盘上快照（审查 P1-2：不用命令开头的陈旧快照；侧车无任务级
		// 锁基建，残余竞态如实记录在 v1 已知边界）。
		explicitRef, _ := cmd.Flags().GetString("ref")
		var state *taskpipeline.TaskState
		if explicitRef != "" {
			state, err = taskpipeline.LoadTaskState(root, explicitRef)
		} else {
			state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		}
		if err != nil {
			return err
		}
		if state == nil {
			return fmt.Errorf("--apply 需要活跃任务（侧车挂在任务上；纯抽取去掉 --apply 即可）")
		}
		if state.CompletedAt != nil {
			return fmt.Errorf("任务已完成——保留集在交付前定稿，拒绝事后注入")
		}
		total, aerr := taskpipeline.ApplyHeldoutToTask(root, state.TaskRef, sets)
		if aerr != nil {
			return aerr
		}
		fmt.Printf("✓ 已并入任务 %s 的 held-out 侧车（共 %d 条）——forge task verify-acceptance 实跑双套件记 gap\n", state.TaskRef, total)
		return nil
	}
}

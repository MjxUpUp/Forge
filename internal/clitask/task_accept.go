package clitask

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_accept.go — oracle-pipeline L1 的补登入口：任务开工后给考卷加题
// （`forge task accept "run :: expected"`）。层级制下这不是中性的「编辑」——
// 事后补登的考卷盖 manual 层（实现者看过代码之后才出的题），交付验收单如实
// 披露该层级；想出先于代码的考卷，用 task start --accept 或产物链 --extract。

var taskAcceptCmd = &cobra.Command{
	Use:   `accept <criteria...>`,
	Short: "补登记验收标准（考卷层级 manual——事后补登，验收单如实披露）",
	Long: `补登记验收标准到当前（或 --ref 指定的）任务，格式与 task start --accept 相同：
"run :: expected"（expected 空 = 只看退出码 0；可多条）。

考卷层级制（oracle-pipeline L1）：本命令登记的标准盖 manual 层——实现者看过
代码之后才出的题，是考卷梯子里最弱的一级（spec 提取 ＞ start 登记 ＞
conventions 兜底 ＞ manual）。层级随登记一次盖章、不可改写；forge task report
将如实披露 manual 层的存在。先于代码写考卷请用 task start --accept 或
forge task artifact --extract（从 spec 产物提取）。`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTaskAccept,
}

func init() {
	Root.AddCommand(taskAcceptCmd)
	taskAcceptCmd.Flags().String("ref", "", "指定任务 ref（缺省取活跃任务）")
}

// runTaskAccept 解析标准串、盖 manual 层、锁内合并进任务验收集，并打印实际新增
// 条目（按 Run 去重——已存在的命令是 no-op，其原层级保留）。
func runTaskAccept(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")

	var state *taskpipeline.TaskState
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
	if state.CompletedAt != nil {
		return fmt.Errorf("任务 %s 已完成——验收标准不可再补登（考卷必须在交付前定稿；发现问题的正确出口是开新修复任务并带上失败的验收命令）", state.TaskRef)
	}

	criteria := taskpipeline.ParseAcceptance(args)
	if adjusted := taskpipeline.EnsureGoTestVerbose(criteria); len(adjusted) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "ℹ️ 验收命令自动补 -v（go test 无 -v 时输出无 PASS 行，Expected 子串永不匹配）：%s\n", strings.Join(adjusted, ", "))
	}
	added, err := taskpipeline.RegisterAcceptance(root, state.TaskRef, criteria, taskpipeline.AcceptanceSourceManual)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		fmt.Println("0 条新增——登记的命令均已存在（按 Run 去重，原层级保留）。")
		return nil
	}
	fmt.Printf("已补登记 %d 条验收标准（考卷层级 manual——实现者看过代码后出的题，交付验收单将如实披露）：\n", len(added))
	for i, c := range added {
		exp := c.Expected
		if exp == "" {
			exp = "(退出码 0)"
		}
		fmt.Printf("  [%d] %s :: %s\n", i+1, c.Run, exp)
	}
	if dup := len(criteria) - len(added); dup > 0 {
		fmt.Printf("（另有 %d 条因命令已存在被去重跳过）\n", dup)
	}
	fmt.Println("→ 实跑：forge task verify-acceptance")
	return nil
}

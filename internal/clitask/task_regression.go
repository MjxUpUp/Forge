package clitask

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_regression.go — oracle-pipeline L4 的登记入口：`forge task regression`
// 把 finding 与回归测试绑定（修前红由 finding 作证、修后绿由它作证），resolve
// 消费该绑定作硬前置。刻意独立成命令而非 finding 的 flag：finding 的命令面
// （path+flags）是 compat 七面快照的冻结面，加 flag 判 changed（破坏性预告
// 流程）——新增命令只判 added（非破坏），与 chain-init 同路。

var taskRegressionCmd = &cobra.Command{
	Use:   "regression --finding <id> --test <file> | --none --note <理由>",
	Short: "登记 finding 的回归测试绑定（L4 修复自证——resolve 的硬前置）",
	Long: `登记 finding 的回归绑定（oracle-pipeline L4：修复必须自证）：
修前红由 finding 本身作证，修后绿由 --test 指向的【本任务改动的】_test.go
作证——没有回归测试的 fixed 会换顶帽子复发。

--test 三重校验：是 _test.go、在任务改动窗口内（forge task scope show）、
真含 Test/Fuzz 函数（拿无关旧测试冒充回归不算数）。
确无可测形态（文档措辞类等）：--none --note <理由> 落审计行（逃生不静默）。
之后 forge task finding --resolve <id> 才放行。`,
	RunE: runTaskRegression,
}

func init() {
	Root.AddCommand(taskRegressionCmd)
	taskRegressionCmd.Flags().String("ref", "", "指定任务（缺省取活跃任务）")
	taskRegressionCmd.Flags().String("finding", "", "要绑定的发现 ID（必填）")
	taskRegressionCmd.Flags().String("test", "", "回归测试文件：本任务改动的 _test.go")
	taskRegressionCmd.Flags().Bool("none", false, "无可测形态声明（须 --note <理由>，落审计行）")
	taskRegressionCmd.Flags().String("note", "", "--none 的理由（必填，可审计）")
}

func runTaskRegression(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	findingID, _ := cmd.Flags().GetString("finding")
	testFile, _ := cmd.Flags().GetString("test")
	none, _ := cmd.Flags().GetBool("none")
	note, _ := cmd.Flags().GetString("note")
	if none && testFile != "" {
		return fmt.Errorf("--test 与 --none 二选一")
	}
	if !none {
		// 契约卫生（审查 P2-1）：--note 只属于 --none——裸 --note 不得静默
		// 走成 none 逃生。
		note = ""
	} else {
		testFile = ""
	}
	if err := taskpipeline.RecordFindingRegression(root, state, findingID, testFile, note); err != nil {
		return err
	}
	if none {
		fmt.Printf("✓ 已登记 %s 的无可测形态声明（理由已落审计行）——resolve 将放行\n", findingID)
		return nil
	}
	fmt.Printf("✓ 已绑定 %s → %s（修后绿由它作证）——forge task finding --resolve %s 放行\n", findingID, testFile, findingID)
	return nil
}

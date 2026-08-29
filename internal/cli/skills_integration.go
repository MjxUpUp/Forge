package cli

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillintegrate"
	"github.com/spf13/cobra"
)

// skillsIntegrationCmd — `forge skills integration [skill]`：查看某 skill 的 forge
// 集成笔记（skills 零反向依赖契约 CONVENTIONS §13 的 forge 侧承接面——skill 目录
// 不含 forge 集成内容，forge 用户从这里拿命令语法/门禁机制/逃生口）。无参数列出
// 有笔记的 skill 清单。
var skillsIntegrationCmd = &cobra.Command{
	Use:   "integration [skill]",
	Short: "查看 skill 的 forge 集成笔记（零反向依赖契约的 forge 侧承接）",
	Long: `forge skills integration — 查看 skill 的 forge 集成笔记。

skills 零反向依赖契约（CONVENTIONS §13）：skill 目录是工具中立的方法论资产，
不含 forge 集成内容；各 skill 的 forge 专属机制（命令语法、门禁、逃生口、
与通用方法论的衔接点）由 forge 侧以本命令承载。

用法：
  forge skills integration              列出有集成笔记的 skill
  forge skills integration <skill>      打印该 skill 的集成笔记`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			names := skillintegrate.List()
			if len(names) == 0 {
				fmt.Println("（暂无集成笔记）")
				return nil
			}
			fmt.Printf("有 forge 集成笔记的 skill（%d 个）：\n", len(names))
			for _, n := range names {
				fmt.Printf("  %s\n", n)
			}
			fmt.Println("查看：forge skills integration <skill>")
			return nil
		}
		name := strings.TrimSpace(args[0])
		note, ok := skillintegrate.Lookup(name)
		if !ok {
			return fmt.Errorf("skill %q 无集成笔记（有笔记的 skill 见 forge skills integration）", name)
		}
		fmt.Print(note)
		if !strings.HasSuffix(note, "\n") {
			fmt.Println()
		}
		return nil
	},
}

func init() {
	skillsCmd.AddCommand(skillsIntegrationCmd)
}

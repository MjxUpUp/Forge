package cli

// skills_mutex_gen.go — mutex-gen 子命令：对全量 canonical 从 SKIP 让渡边派生跨 skill
// 互斥 case 集，落盘 <eval-dir>/mutex/cases.json。单 skill eval case（eval-gen）从不
// 说误路由的 prompt「本该去哪」；互斥 case 正是断言这件事（B 域必须路由到 B、绝不
// 路由到声明让渡的 A）。

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var skillsMutexGenCmd = &cobra.Command{
	Use:   "mutex-gen",
	Short: "生成跨 skill 互斥 case 集（从 SKIP 让渡边派生，B 域 prompt 必须路由到 B）",
	Long: `遍历 canonical 全部 skill 的 description，从 SKIP 段的（用 X）/（use X) 括号
模式解析让渡边 A→B，再对每条边取 B 的 trigger 片段渲染成对比 case（每边 ≤2 个），
落盘 <eval-dir>/mutex/cases.json。

让渡目标不在 canonical 里的边被丢弃（悬空引用是 description 的 bug，不是 eval 边）。

  forge skills mutex-gen              # 生成 + 落盘 + 打印边摘要
  forge skills mutex-gen --dir evals  # 落到仓库级 eval 目录`,
	RunE: runSkillsMutexGen,
}

func runSkillsMutexGen(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	dir, err := evalDataDir()
	if err != nil {
		return err
	}
	edges, err := skillseval.MutexEdges(canonical)
	if err != nil {
		return fmt.Errorf("解析互斥边: %w", err)
	}
	cases, err := skillseval.MutexCases(canonical)
	if err != nil {
		return fmt.Errorf("派生互斥 case: %w", err)
	}
	if err := skillseval.SaveMutexCases(dir, cases); err != nil {
		return fmt.Errorf("落盘 case 集: %w", err)
	}
	fmt.Printf("互斥集：%d 条让渡边 → %d 个 case（已落盘 %s/mutex/cases.json）\n", len(edges), len(cases), dir)
	for _, e := range edges {
		fmt.Printf("  %s → %s（%s）\n", e.From, e.To, e.Fragment)
	}
	return nil
}

func init() {
	skillsCmd.AddCommand(skillsMutexGenCmd)
}

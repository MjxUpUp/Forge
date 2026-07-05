package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/spf13/cobra"
)

var (
	skAdpApply bool
)

var skillsAdaptersCmd = &cobra.Command{
	Use:   "adapters",
	Short: "部署/查看 skill-routing adapter 单文件（claude hook/cursor rule/routes 表）",
	Long: `forge skills adapters — 分发 skill-routing adapter 单文件：
  claude/skill-router.sh → ~/.claude/hooks/skill-router-claude.sh
  cursor/skill-routing.mdc → ~/.cursor/rules/skill-routing.mdc
  routes.json            → skill-routing routes 表

不带 --apply 为 dry-run（只显示 deploy/ok/skip）。`,
	RunE: runSkillsAdapters,
}

func runSkillsAdapters(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	if skAdpApply {
		done, plan, derr := skillsdist.DeployAdapters(canonical, home)
		if derr != nil {
			return derr
		}
		fmt.Printf("部署 %d/%d adapter:\n", done, len(plan))
		for _, a := range plan {
			fmt.Printf("  ✓ %s → %s\n", a.Spec.SrcRel, a.Spec.Dst)
		}
		return nil
	}

	plan := skillsdist.PlanAdapters(canonical, home)
	for _, a := range plan {
		mark := map[string]string{"deploy": "→", "ok": "=", "skip": "⊘"}[a.Action]
		fmt.Printf("  %s %-10s %s  (%s)\n", mark, a.Action, a.Spec.Dst, a.Detail)
	}
	return nil
}

func init() {
	skillsAdaptersCmd.Flags().BoolVar(&skAdpApply, "apply", false, "执行部署（默认 dry-run）")
	skillsCmd.AddCommand(skillsAdaptersCmd)
}

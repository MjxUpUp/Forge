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
	Short: "部署/查看 skill-routing adapter 单文件（pi extension/claude hook/cursor rule/routes.json）",
	Long: `forge skills adapters — 分发 4 个 skill-routing adapter 单文件：
  pi/index.ts            → ~/.pi/agent/extensions/skill-router/index.ts
  claude/skill-router.sh → ~/.claude/hooks/skill-router-claude.sh
  cursor/skill-routing.mdc → ~/.cursor/rules/skill-routing.mdc
  routes.json            → ~/.pi/agent/skill-routes.json

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

package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/spf13/cobra"
)

var (
	skDriftJSON    bool
	skDriftGlobal  bool
	skDriftProject bool
	skDriftTarget  []string
)

var skillsDriftCheckCmd = &cobra.Command{
	Use:   "drift-check",
	Short: "检测分发分叉（dry-run，不写）",
	Long:  `forge skills drift-check — 检测 canonical 与各目标的分发态（linked/copy-in-sync/drift/missing/target-only）。`,
	RunE:  runSkillsDriftCheck,
}

func runSkillsDriftCheck(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	targets, err := parseSkillTargets(skDriftTarget)
	if err != nil {
		return err
	}
	global := skDriftGlobal && !skDriftProject
	projectDir := ""
	if !global {
		root, ferr := findProjectRoot()
		if ferr != nil {
			return fmt.Errorf("--project 需在 forge 项目内运行；用 --global（默认）查全局")
		}
		projectDir = filepath.Join(root, ".claude", "skills")
	}
	opts := skillsdist.InstallOpts{
		Targets:          targets,
		Global:           global,
		ProjectSkillsDir: projectDir,
	}
	rep, err := skillsdist.DriftCheck(canonical, opts)
	if err != nil {
		return err
	}

	if skDriftJSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("canonical: %s\n", rep.Canonical)
		fmt.Printf("  linked=%d copy-in-sync=%d drift=%d missing=%d target-only=%d\n",
			rep.Stats.Linked, rep.Stats.CopyInSync, rep.Stats.Drift, rep.Stats.Missing, rep.Stats.TargetOnly)
		for _, it := range rep.Items {
			if it.State == "linked" || it.State == "copy-in-sync" {
				continue
			}
			fmt.Printf("  %s  %-28s [%s]  %s\n", stateMark(it.State), it.Name, it.Target, it.State)
		}
	}
	return nil
}

func stateMark(state string) string {
	switch state {
	case "drift":
		return "≠"
	case "missing":
		return "?"
	case "target-only":
		return "+"
	case "linked", "copy-in-sync":
		return "="
	}
	return " "
}

func init() {
	skillsDriftCheckCmd.Flags().BoolVar(&skDriftJSON, "json", false, "JSON 输出")
	skillsDriftCheckCmd.Flags().BoolVar(&skDriftGlobal, "global", true, "查全局目标")
	skillsDriftCheckCmd.Flags().BoolVar(&skDriftProject, "project", false, "查当前 forge 项目目标（覆盖 --global）")
	skillsDriftCheckCmd.Flags().StringSliceVar(&skDriftTarget, "target", []string{"all"}, "目标工具 claude|pi|cursor|codex|copilot|all")
	skillsCmd.AddCommand(skillsDriftCheckCmd)
}

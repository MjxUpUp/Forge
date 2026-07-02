package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var (
	skEvalSkill     string
	skEvalAll       bool
	skEvalSave      bool
	skEvalCasesOnly bool
)

var skillsEvalGenCmd = &cobra.Command{
	Use:   "eval-gen",
	Short: "生成 eval 测试用例（should-trigger / should-not-trigger），可选落结构化 case 集",
	RunE:  runSkillsEvalGen,
}

func runSkillsEvalGen(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	dir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}

	// genOne 统一处理单个 skill 的生成与落盘：
	//   --cases-only → 只落结构化 case 集（eval-record/闭环用）
	//   --save       → 落 markdown 清单 + 额外落结构化 case 集
	//   默认（都无）→ 输出 markdown 清单到 stdout
	genOne := func(name string) error {
		// --cases-only 或 --save 都需要结构化 case 集，统一生成一次。
		var cases []skillseval.EvalCase
		if skEvalCasesOnly || skEvalSave {
			c, err := skillseval.EvalCases(canonical, name)
			if err != nil {
				return err
			}
			cases = c
		}
		if skEvalCasesOnly {
			if err := skillseval.SaveCases(dir, name, cases); err != nil {
				return err
			}
			fmt.Printf("✅ %d cases → %s/cases/%s.json\n", len(cases), dir, name)
			return nil
		}
		md, err := skillseval.EvalSkill(canonical, name)
		if err != nil {
			return err
		}
		if skEvalSave {
			if err := saveEval(name, md); err != nil {
				return err
			}
			// 额外落结构化 case 集（eval-record 回归闭环用）。SaveCases 对空集 no-op。
			// 失败要 return error——否则 agent 收到「✅ cases」但实际没落盘，后续
			// eval-record 报「no eval cases」，与 --cases-only 路径行为不一致。
			if err := skillseval.SaveCases(dir, name, cases); err != nil {
				return err
			}
			fmt.Printf("✅ eval-%s.md + %d cases → ~/.pi/research/\n", name, len(cases))
			return nil
		}
		fmt.Print(md)
		return nil
	}

	if skEvalAll {
		names, err := skillsdist.ListSkills(canonical)
		if err != nil {
			return err
		}
		for _, n := range names {
			if err := genOne(n); err != nil {
				fmt.Fprintf(os.Stderr, "⚠️ %s: %v\n", n, err)
			}
		}
		return nil
	}

	if skEvalSkill == "" {
		return fmt.Errorf("需要 --skill NAME 或 --all")
	}
	return genOne(skEvalSkill)
}

func saveEval(name, md string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".pi", "research")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "eval-"+name+".md"), []byte(md), 0644)
}

func init() {
	skillsEvalGenCmd.Flags().StringVar(&skEvalSkill, "skill", "", "为指定 skill 生成 eval 清单")
	skillsEvalGenCmd.Flags().BoolVar(&skEvalAll, "all", false, "为所有 skill 生成（批量）")
	skillsEvalGenCmd.Flags().BoolVar(&skEvalSave, "save", false, "保存清单到 ~/.pi/research/eval-<name>.md 并落结构化 case 集")
	skillsEvalGenCmd.Flags().BoolVar(&skEvalCasesOnly, "cases-only", false, "只生成并落结构化 case 集（eval-record 闭环用），不输出 markdown")
	skillsCmd.AddCommand(skillsEvalGenCmd)
}

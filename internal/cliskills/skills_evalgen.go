package cliskills

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
	canonical, _, err := ResolveCanonical()
	if err != nil {
		return err
	}
	dir, err := evalDataDir()
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
			if err := saveEval(dir, name, md); err != nil {
				return err
			}
			// 额外落结构化 case 集（eval-record 回归闭环用）。SaveCases 对空集 no-op。
			// 失败要 return error——否则 agent 收到"✅ cases"但实际没落盘，后续
			// eval-record 报"no eval cases"，与 --cases-only 路径行为不一致。
			if err := skillseval.SaveCases(dir, name, cases); err != nil {
				return err
			}
			fmt.Printf("✅ checklists/eval-%s.md + %d cases → %s\n", name, len(cases), dir)
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
		// 批量模式全部失败也不能 exit 0——累计失败数、打印统计、返回 error，
		// 让 CI/脚本看到失败。
		failed := 0
		for _, n := range names {
			if err := genOne(n); err != nil {
				failed++
				fmt.Fprintf(os.Stderr, "⚠️ %s: %v\n", n, err)
			}
		}
		fmt.Printf("eval-gen --all: 成功 %d / 失败 %d\n", len(names)-failed, failed)
		if failed > 0 {
			return fmt.Errorf("%d 个 skill 生成失败", failed)
		}
		return nil
	}

	if skEvalSkill == "" {
		return fmt.Errorf("需要 --skill NAME 或 --all")
	}
	if err := requireValidSkillName(skEvalSkill); err != nil {
		return err
	}
	return genOne(skEvalSkill)
}

// saveEval 把 markdown 清单落 <evals-root>/checklists/eval-<name>.md。
// 曾硬编码 ~/.pi/research/（第二处独立的遗留 join）——现在跟随解析出的 eval 根，
// 仓库级 --dir 下生成的清单与其配套的 case 集落在一起。首次默认解析会把旧清单
// 迁入（dir.go）。
func saveEval(root, name, md string) error {
	dir := filepath.Join(root, "checklists")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "eval-"+name+".md"), []byte(md), 0644)
}

func init() {
	skillsEvalGenCmd.Flags().StringVar(&skEvalSkill, "skill", "", "为指定 skill 生成 eval 清单")
	skillsEvalGenCmd.Flags().BoolVar(&skEvalAll, "all", false, "为所有 skill 生成（批量）")
	skillsEvalGenCmd.Flags().BoolVar(&skEvalSave, "save", false, "保存清单到 <eval-dir>/checklists/eval-<name>.md 并落结构化 case 集")
	skillsEvalGenCmd.Flags().BoolVar(&skEvalCasesOnly, "cases-only", false, "只生成并落结构化 case 集（eval-record 闭环用），不输出 markdown")
	Root.AddCommand(skillsEvalGenCmd)
}

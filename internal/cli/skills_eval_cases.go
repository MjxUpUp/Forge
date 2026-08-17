package cli

// skills_eval_cases.go — eval-cases subcommand: emits the case set for agent dispatch.
//
// When an agent dispatches a fresh subagent to run a prompt, this command is used to
// obtain case_id + prompt (instead of cat-ing cases/<skill>.json directly).
//
// skills_eval_cases.go — eval-cases 子命令：输出 case 集供 agent dispatch。
//
// agent dispatch fresh subagent 跑 prompt 时，用本命令拿 case_id + prompt（而非直接
// cat cases/<skill>.json）。

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var skCasesSkill string

var skillsEvalCasesCmd = &cobra.Command{
	Use:   "eval-cases",
	Short: "输出 case 集（agent dispatch 跑 prompt 的输入）",
	Long: `输出 case 集 JSON，供 agent dispatch subagent 跑 prompt 时拿 case_id + prompt。

trigger/not-trigger case：agent 跑 Prompt，回填 actual_triggered（是否触发本 skill）。

格式：
  [{"id","kind","prompt","target"(trigger)}]`,
	RunE: runSkillsEvalCases,
}

// caseView is the view emitted by eval-cases.
//
// caseView 是 eval-cases 对外输出的视图。
type caseView struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Prompt string `json:"prompt"`
	Target string `json:"target,omitempty"` // trigger 类 = skill 名
}

// caseViews converts the EvalCase set into the output view.
//
// caseViews 把 EvalCase 集转成输出视图。
func caseViews(cases []skillseval.EvalCase) []caseView {
	out := make([]caseView, len(cases))
	for i, c := range cases {
		out[i] = caseView{
			ID:     c.ID,
			Kind:   c.Kind,
			Prompt: c.Prompt,
			Target: c.Target,
		}
	}
	return out
}

func runSkillsEvalCases(cmd *cobra.Command, args []string) error {
	if skCasesSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	if err := requireValidSkillName(skCasesSkill); err != nil {
		return err
	}
	dir, err := evalDataDir()
	if err != nil {
		return err
	}
	cases, err := skillseval.LoadCases(dir, skCasesSkill)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		return fmt.Errorf("skill %q 无 case 集——先 eval-gen --skill %s --save", skCasesSkill, skCasesSkill)
	}
	out := caseViews(cases)
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func init() {
	skillsEvalCasesCmd.Flags().StringVar(&skCasesSkill, "skill", "", "输出哪个 skill 的 case 集")
	skillsCmd.AddCommand(skillsEvalCasesCmd)
}

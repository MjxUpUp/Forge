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

	// genOne handles generation and persistence for a single skill:
	//   --cases-only → persist only the structured case set (for eval-record/closed loop)
	//   --save       → persist the markdown checklist + additionally persist structured case set
	//   default (neither set) → print the markdown checklist to stdout
	//
	// genOne 统一处理单个 skill 的生成与落盘：
	//   --cases-only → 只落结构化 case 集（eval-record/闭环用）
	//   --save       → 落 markdown 清单 + 额外落结构化 case 集
	//   默认（都无）→ 输出 markdown 清单到 stdout
	genOne := func(name string) error {
		// Both --cases-only and --save need the structured case set; generate it once.
		//
		// --cases-only 或 --save 都需要结构化 case 集，统一生成一次。
		var cases []skillseval.EvalCase
		if skEvalCasesOnly || skEvalSave {
			c, err := skillseval.EvalCases(canonical, name)
			if err != nil {
				return err
			}
			cases = c
			// Merge behavior probes (<name>/probes.yaml, optional). behavior cases are derived
			// independently of the description, persisted in the same set as trigger/not-trigger,
			// and SubmitRun branches judgment by Kind. When LoadProbes fails, a stderr warning
			// is emitted (see the perr branch below) — a skill without probes.yaml normally
			// has only routing cases, and stays silent when len(probes)==0.
			//
			// merge behavior probes（<name>/probes.yaml，可选）。behavior case 独立于
			// description 派生，与 trigger/not-trigger 同集落盘，SubmitRun 按 Kind 分支判定。
			// LoadProbes 失败时 stderr 警告（见下方 perr 分支）——无 probes.yaml 的 skill
			// 正常只有路由 case，len(probes)==0 时静默。
			if probes, perr := skillseval.LoadProbes(canonical, name); perr == nil {
				if len(probes) > 0 {
					cases = append(cases, probes...)
				}
			} else {
				// Silently skipping a bad probes.yaml (YAML syntax/type error) would let the
				// agent think the probe is in effect — a stderr warning makes the missing behavior
				// cases visible (consistent with the per-skill error handling in the --all loop below).
				//
				// 坏 probes.yaml（YAML 语法/类型错）静默跳过会让 agent 以为 probe 生效——
				// stderr 警告让 behavior case 缺失可见（与下方 --all 循环的 per-skill 错误处理一致）。
				fmt.Fprintf(os.Stderr, "⚠️ %s: probes.yaml 解析失败，已跳过 behavior probe: %v\n", name, perr)
			}
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
			// Additionally persist the structured case set (for the eval-record regression
			// loop). SaveCases is a no-op on an empty set. A failure must return an error —
			// otherwise the agent sees `✅ cases` but nothing was actually persisted, and a
			// later eval-record reports `no eval cases`, diverging from the --cases-only path.
			//
			// 额外落结构化 case 集（eval-record 回归闭环用）。SaveCases 对空集 no-op。
			// 失败要 return error——否则 agent 收到"✅ cases"但实际没落盘，后续
			// eval-record 报"no eval cases"，与 --cases-only 路径行为不一致。
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

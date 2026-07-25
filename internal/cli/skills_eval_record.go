package cli

// skills_eval_record.go — eval-record 子命令：把 agent 整批回填的 CaseResult 写成
// 一条 EvalRun。agent 通过 MCP dispatch fresh subagent 跑完每个 prompt 后，把
// 「实际触发了哪个 skill」整批喂给 eval-record（--from file.json 或 stdin），forge
// 负责归一化 + DescHash 校验 + 判定 + 算 health + append。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var (
	skRecSkill string
	skRecFrom  string
	skRecModel string
	skRecVer   string
)

var skillsEvalRecordCmd = &cobra.Command{
	Use:   "eval-record",
	Short: "回填一次 eval run 结果（agent dispatch 跑完后整批提交，算 health 并落盘）",
	Long: `把 agent dispatch 子代理跑出的整批结果写成一个 EvalRun。

--from 指向一个 JSON 数组文件，或 "-" / 省略从 stdin 读。格式：
  [{"case_id":"...","actual_triggered":"<skill 名 | none>","note":"可选"}]

forge 归一化 actual_triggered（trim+lowercase+canonical 精确匹配）、校验 case 集的
DescHash 与当前 SKILL.md 一致、判定每个 case、算 health 分、append 到 runs/<skill>.jsonl。`,
	RunE: runSkillsEvalRecord,
}

func runSkillsEvalRecord(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	if skRecSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	dir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}

	data, err := readFromArg(skRecFrom)
	if err != nil {
		return fmt.Errorf("read results: %w", err)
	}
	var raw []skillseval.SubmitResult
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse results JSON（应为 [{case_id,actual_triggered,note}]）: %w", err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("results 为空")
	}

	ver := skRecVer
	if ver == "" {
		ver = rootCmd.Version
	}

	run, err := skillseval.SubmitRun(dir, canonical, skRecSkill, skRecModel, ver, raw)
	if err != nil {
		return err
	}

	// 纯 behavior run 的 HealthScore 恒 0（HealthScore 只计路由维度 trigger/not-trigger，
	// behavior 不计入）——打 health=0.00 会误导（✅ 配 0.00 像全挂）。全 behavior 时
	// 改打 behavior pass-rate。
	if allBehavior, pass, total := behaviorOnlyStats(run.Results); allBehavior {
		rate := 0.0
		if total > 0 {
			rate = float64(pass) / float64(total)
		}
		fmt.Printf("✅ run %s recorded: behavior pass-rate=%.0f%% (%d/%d)\n", run.RunID, rate*100, pass, total)
	} else {
		fmt.Printf("✅ run %s recorded: health=%.2f, %d results\n", run.RunID, run.HealthScore, len(run.Results))
	}
	if run.BaselineRunID != "" {
		// run 时刻锁定了 baseline，提示拿 report 看回归。
		fmt.Printf("   baseline=%s（变更已记录，跑 eval-report 看回归）\n", run.BaselineRunID)
	}
	return nil
}

// readFromArg 从 "-" 或空（stdin）或文件路径读全部字节。
func readFromArg(from string) ([]byte, error) {
	if from == "" || from == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(from)
}

// behaviorOnlyStats 判断 results 是否全为 behavior case，并统计 behavior 通过数。
// 全 behavior 时 HealthScore 恒 0（只计路由维度 trigger/not-trigger），调用方据此
// 改打 behavior pass-rate，避免 ✅ 配 health=0.00 的误导输出。
func behaviorOnlyStats(results []skillseval.CaseResult) (allBehavior bool, pass, total int) {
	if len(results) == 0 {
		return false, 0, 0
	}
	for _, r := range results {
		if r.Kind != skillseval.KindBehavior {
			return false, 0, 0
		}
		total++
		if r.Pass {
			pass++
		}
	}
	return true, pass, total
}

func init() {
	skillsEvalRecordCmd.Flags().StringVar(&skRecSkill, "skill", "", "回填哪个 skill 的 run")
	skillsEvalRecordCmd.Flags().StringVar(&skRecFrom, "from", "-", "结果 JSON 文件路径（- 或省略 = stdin）")
	skillsEvalRecordCmd.Flags().StringVar(&skRecModel, "agent-model", "", "跑此 run 的 agent 模型（防跨模型假回归）")
	skillsEvalRecordCmd.Flags().StringVar(&skRecVer, "forge-version", "", "forge 版本（默认取二进制版本）")
	skillsCmd.AddCommand(skillsEvalRecordCmd)
}

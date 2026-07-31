package cli

// skills_decide.go — decide subcommand: appends a decision to a skill's decisions.md.
// Persistent decision history: quadruple (diagnosis, revision, evidence, outcome) + rationale + linked
// commit/probe-run. Lets the next-round agent understand why the skill was changed this way, avoiding re-exploring failed
// directions. Audit/reproducible, not generalized learning.
//
// skills_decide.go — decide 子命令：把一条决策追加到 skill 的 decisions.md。
// persistent decision history：四元组 (诊断,修订,证据,结果) + rationale + 关联
// commit/probe-run。让下一轮 agent 理解 skill「为什么这么改」，避免重复探索已失败
// 方向。审计/可复现，非泛化学习。

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
	"github.com/spf13/cobra"
)

var (
	skDecSkill     string
	skDecDiagnosis string
	skDecRevision  string
	skDecEvidence  string
	skDecOutcome   string
	skDecRationale string
	skDecCommit    string
	skDecProbeRun  string
	skDecBy        string
)

var skillsDecideCmd = &cobra.Command{
	Use:   "decide",
	Short: "记录一条 skill 决策到 decisions.md（诊断/修订/证据/结果四元组）",
	Long: `把一条决策追加到 <skill>/decisions.md 的 persistent decision history：让下一轮
agent 理解 skill「为什么这么改」，避免重复探索已失败方向。

四元组对应 decision record h_t = (q_t, r_t, e_t, o_t)：
  --diagnosis  q_t：诊断（什么失败模式/问题）
  --revision   r_t：候选修订（改了什么——SKILL.md/scripts/references 哪里）
  --evidence   e_t：评估证据（pass-rate / 回归比对 / 诊断线索）
  --outcome    o_t：accept | reject | revise | defer

可选锚点：
  --commit     修订关联的 git commit（scoped revert 锚点）
  --probe-run  关联的 eval/probe run ID
  --rationale  为什么这个 outcome（结合背景）
  --by         来源（claude-code/codex/...）`,
	RunE: runSkillsDecide,
}

func runSkillsDecide(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	if skDecSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	if err := requireValidSkillName(skDecSkill); err != nil {
		return err
	}
	if skDecDiagnosis == "" || skDecRevision == "" || skDecEvidence == "" {
		return fmt.Errorf("--diagnosis / --revision / --evidence 均必填（四元组的 q_t/r_t/e_t）")
	}
	if !skillsdecisions.ValidOutcome(skDecOutcome) {
		return fmt.Errorf("--outcome 必须是 accept|reject|revise|defer，得到 %q", skDecOutcome)
	}

	d := skillsdecisions.SkillDecision{
		Skill:      skDecSkill,
		Diagnosis:  skDecDiagnosis,
		Revision:   skDecRevision,
		Evidence:   skDecEvidence,
		Outcome:    skDecOutcome,
		Rationale:  skDecRationale,
		CommitHash: skDecCommit,
		ProbeRunID: skDecProbeRun,
		By:         skDecBy,
	}
	if err := skillsdecisions.AppendDecision(canonical, skDecSkill, d); err != nil {
		return err
	}
	fmt.Printf("✅ 决策已记录到 %s\n", skillsdecisions.DecisionsFile(canonical, skDecSkill))
	return nil
}

func init() {
	skillsDecideCmd.Flags().StringVar(&skDecSkill, "skill", "", "记录哪个 skill 的决策")
	skillsDecideCmd.Flags().StringVar(&skDecDiagnosis, "diagnosis", "", "诊断（q_t：什么问题/失败模式）")
	skillsDecideCmd.Flags().StringVar(&skDecRevision, "revision", "", "候选修订（r_t：改了什么）")
	skillsDecideCmd.Flags().StringVar(&skDecEvidence, "evidence", "", "评估证据（e_t：pass-rate / 回归比对）")
	skillsDecideCmd.Flags().StringVar(&skDecOutcome, "outcome", "", "结果（o_t：accept|reject|revise|defer）")
	skillsDecideCmd.Flags().StringVar(&skDecRationale, "rationale", "", "为什么这个 outcome（结合背景）")
	skillsDecideCmd.Flags().StringVar(&skDecCommit, "commit", "", "修订关联的 git commit（scoped revert 锚点）")
	skillsDecideCmd.Flags().StringVar(&skDecProbeRun, "probe-run", "", "关联的 eval run ID")
	skillsDecideCmd.Flags().StringVar(&skDecBy, "by", "", "来源（claude-code/codex/...）")
	skillsCmd.AddCommand(skillsDecideCmd)
}

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
	skDecSkill      string
	skDecDiagnosis  string
	skDecRevision   string
	skDecEvidence   string
	skDecOutcome    string
	skDecRationale  string
	skDecCommit     string
	skDecProbeRun   string
	skDecBy         string
	skDecPrediction string
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
  --by         来源（claude-code/codex/...）

预测闭环（AHE 决策可观测，推荐）：
  --prediction 修改时刻声明的可检验预测——修订若有效，哪个可观测信号应改善
               （如「skill X 触发率应从 15% 升到 30%」）。在结果已知前声明，
               事后用 forge skills verify 回填对账，让修改成为可证伪契约`,
	RunE: runSkillsDecide,
}

func runSkillsDecide(cmd *cobra.Command, args []string) error {
	canonical, isExternal, err := resolveCanonical()
	if err != nil {
		return err
	}
	// decide MUTATES canonical (appends to <skill>/decisions.md). The embed cache is a
	// regenerated distribution snapshot, not a writable source: EnsureEmbeddedCache
	// RemoveAll-rebuilds it whenever the version marker mismatches the running binary —
	// with two forge versions alternating on one machine (e.g. the globally installed
	// release driving the hook chain + a locally built dev binary), the cache is
	// version-ping-pong wiped on every foreign-version invocation. A decide that resolved
	// to the cache reported ✅ success and the entry was silently destroyed by the next
	// hook call (2026-08-24 incident: three decisions vanished between the ✅ and the
	// follow-up grep). Fail loudly instead: the agent must point at a real source via
	// $FORGE_SKILLS_CANONICAL / --canonical (in this repo: the skills/ directory).
	//
	// decide 会变更 canonical（追加 <skill>/decisions.md）。embed 缓存是可再生成的分发
	// 快照、不是可写源：EnsureEmbeddedCache 在版本标记与运行二进制不一致时会
	// RemoveAll 整目录重建——同一台机器上两个 forge 版本交替运行时（如全局安装的正式
	// 版驱动 hook 链 + 本地构建的 dev 二进制），缓存每次异版调用都会被 ping-pong 抹掉。
	// 解析到缓存的 decide 报了 ✅ 成功，条目却被下一次 hook 调用静默销毁（2026-08-24
	// 事故：三条决策在 ✅ 与随后的 grep 之间消失）。改为响亮失败：agent 必须经
	// $FORGE_SKILLS_CANONICAL / --canonical 指向真实源（本仓库即 skills/ 目录）。
	if !isExternal {
		return fmt.Errorf("decide 不能写入内置 embed 缓存（%s）——它是随时被版本重建的分发快照，写入必丢（异版二进制交替运行时每次 hook 调用都会抹掉它）。用 $FORGE_SKILLS_CANONICAL 或 --canonical 指向真实 skill 源（本仓库为 skills/ 目录）后重试", canonical)
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
		Prediction: skDecPrediction,
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
	skillsDecideCmd.Flags().StringVar(&skDecPrediction, "prediction", "", "可检验预测（修改时刻声明：哪个可观测信号应改善；事后 forge skills verify 回填）")
	skillsCmd.AddCommand(skillsDecideCmd)
}

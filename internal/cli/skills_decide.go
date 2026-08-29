package cli

// skills_decide.go — decide 子命令：把一条决策追加到 skill 的 decisions.md。
// persistent decision history：四元组 (诊断,修订,证据,结果) + rationale + 关联
// commit/probe-run。让下一轮 agent 理解 skill「为什么这么改」，避免重复探索已失败
// 方向。审计/可复现，非泛化学习。

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	Use:   "decide [<skill>]",
	Short: "记录一条 skill 决策到 decisions.md（诊断/修订/证据/结果四元组；skill 名可用位置参数或 --skill）",
	Long: `把一条决策追加到 <skill>/decisions.md 的 persistent decision history：让下一轮
agent 理解 skill「为什么这么改」，避免重复探索已失败方向。

skill 名两种给法等价：forge skills decide my-skill ...（位置参数）或 --skill my-skill。
在含 skills/ 规范树的仓库内运行（如 Forge 本仓）时默认写入仓库 canonical（./skills/），
不写 embed 缓存；仓库外经 $FORGE_SKILLS_CANONICAL / --canonical 指向真实源。

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
	Args: cobra.MaximumNArgs(1),
	RunE: runSkillsDecide,
}

func runSkillsDecide(cmd *cobra.Command, args []string) error {
	// Positional shorthand for --skill (usage-log fix: `skills decide <name> --diagnosis ...`
	// errored "需要 --skill NAME" although the intent was unambiguous — flag-parity with the
	// rest of the CLI's noun-verb ergonomics).
	//
	// 位置参数作 --skill 简写（usage 日志修复：`skills decide <name> --diagnosis ...`
	// 报「需要 --skill NAME」，但意图本无歧义）。
	if len(args) > 0 {
		if skDecSkill != "" && skDecSkill != args[0] {
			return fmt.Errorf("位置参数 %q 与 --skill %q 冲突，二选一", args[0], skDecSkill)
		}
		skDecSkill = args[0]
	}
	canonical, isExternal, err := resolveCanonical()
	if err != nil {
		return err
	}
	// explicitSource = 用户经 --canonical / $FORGE_SKILLS_CANONICAL 显式指定了源；
	// 该意图永远优先于仓库自动探测。
	explicitSource := isExternal
	// 仓库内默认（usage 日志修复）：forge 在项目根带真实 skills/ 树（CONVENTIONS.md
	// 标记——如 Forge 本仓）的 checkout 里运行时，decide 必须写该 canonical 树而非
	// embed 缓存。缓存是可再生成的分发快照、不是可写源：EnsureEmbeddedCache 在版本标记
	// 与运行二进制不一致时 RemoveAll 整目录重建——同机两个 forge 版本交替运行时（如
	// 全局正式版驱动 hook 链 + 本地 dev 二进制），缓存每次异版调用都被 ping-pong 抹掉。
	// 解析到缓存的 decide 报了 ✅ 成功，条目却被下一次 hook 调用静默销毁（2026-08-24
	// 事故：三条决策在 ✅ 与随后的 grep 之间消失；之后又有 agent 因仓库未自动识别再次
	// 写进缓存）。在带 skills 树的仓库内默认写 ./skills；仓库外响亮失败：agent 必须经
	// $FORGE_SKILLS_CANONICAL / --canonical 指向真实源。
	if !isExternal {
		if repoSkills := detectRepoSkillsDir(); repoSkills != "" {
			canonical = repoSkills
			isExternal = true
			// 单测直接调 runSkillsDecide 时 cmd 为 nil。
			w := io.Writer(os.Stderr)
			if cmd != nil {
				w = cmd.ErrOrStderr()
			}
			fmt.Fprintf(w, "ℹ️ 检测到仓库内 canonical skill 树，decide 写入 %s（非 embed 缓存）\n", repoSkills)
		}
	}
	// forge 原生 skill（requires_forge——skill-evolution / skill-routing /
	// skill-authoring-standard）2026-08 零反向依赖迁移后住 <root>/skills-forge，
	// 不在中立 ./skills 树。解析来自 embed 路径（未显式 --canonical/env）时，对这些
	// 名字把仓库内写入重定向到 forge 原生源；用户显式指定的源永远优先。缺了这一步
	// decide 要么报错（./skills 里没有该 skill）、要么写进可再生成的缓存。
	if !explicitSource && skDecSkill != "" {
		if forgeDir := detectRepoForgeSkillsDir(); forgeDir != "" {
			if _, serr := os.Stat(filepath.Join(forgeDir, skDecSkill)); serr == nil {
				canonical = forgeDir
				isExternal = true
				w := io.Writer(os.Stderr)
				if cmd != nil {
					w = cmd.ErrOrStderr()
				}
				fmt.Fprintf(w, "ℹ️ 检测到 forge 原生 skill 源树，decide 写入 %s（非 embed 缓存）\n", forgeDir)
			}
		}
	}
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
	// 孤儿写入警告（review W3）：AppendDecision 不要求目标 skill 目录存在——名字不在
	// 解析树里时会写入不可见的孤儿目录（plugin pack 的孤儿目录跳过意味着它永不
	// 分发）。显式提示而非静默 ✅。
	if _, serr := os.Stat(filepath.Join(canonical, skDecSkill, "SKILL.md")); serr != nil {
		w := io.Writer(os.Stderr)
		if cmd != nil {
			w = cmd.ErrOrStderr()
		}
		fmt.Fprintf(w, "⚠️ skill %q 在 %s 下无 SKILL.md——decide 将创建不可分发的孤儿目录；请核对 skill 名与目标树\n", skDecSkill, canonical)
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

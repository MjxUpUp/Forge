package cli

// skills_revert.go — revert subcommand: scoped revert, by the CommitHash in decisions.md
// reverts the commit associated with a decision.
//
// skills_revert.go — revert 子命令：scoped revert，按 decisions.md 的 CommitHash
// 撤销某条决策关联的 commit。
//
// scoped revert: precisely undoes a single optimization of a skill (by the CommitHash recorded in the decision), rather than rolling back the
// skill to an older version as a whole. One failed optimization should not wipe out all prior accumulation — scoped revert touches only that one commit,
// other historical optimizations are preserved.
//
// scoped revert：精准撤销 skill 的某次优化（按决策记录的 CommitHash），而非整体回退
// skill 到旧版本。一次失败优化不该抹掉之前所有积累——scoped revert 只动那一个 commit，
// 其他历史优化保留。
//
// Data source: decisions.md (component A). Only decisions with a CommitHash can be scoped-reverted (decisions without a commit anchor,
// e.g. pure documentation optimizations, cannot be precisely reverted; the command skips them).
//
// 数据源：decisions.md（A 组件）。只有带 CommitHash 的决策可 scoped revert（无 commit
// 锚点的决策，如纯文档优化，无法精准 revert，命令会跳过它们）。

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
	"github.com/spf13/cobra"
)

var (
	skRevSkill    string
	skRevDecision string
	skRevEdit     bool // --edit 打开编辑器改 revert commit message（默认 --no-edit）
	skRevDryRun   bool
)

var skillsRevertCmd = &cobra.Command{
	Use:   "revert",
	Short: "Scoped revert：按 decisions.md 的 CommitHash 撤销某条决策关联的 commit",
	Long: `精准撤销 skill 的某次优化（scoped revert）。

读 <skill>/decisions.md，按 --decision <id> 定位决策的 CommitHash，git revert 该 commit。
未指定 --decision 时列出该 skill 所有带 commit 的决策供选择。

scoped vs whole-candidate：只 revert 指定 commit（那次优化），不动其他历史优化——
避免因一次失败撤销所有积累。

冲突：git revert 可能因后续 commit 改了同区域而冲突，git 会退出非 0。手动解决后
'git revert --continue'，不要重跑本命令（会重复 revert）。

成功 revert 后自动追加一条 reject 决策到 decisions.md（spec-as-executable，取代手动
forge skills decide）——下轮 agent 知悉此优化被否决，避免重复探索已失败方向。`,
	RunE: runSkillsRevert,
}

func runSkillsRevert(cmd *cobra.Command, args []string) error {
	canonical, isExternal, err := resolveCanonical()
	if err != nil {
		return err
	}
	if skRevSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	if err := requireValidSkillName(skRevSkill); err != nil {
		return err
	}
	decisions, err := skillsdecisions.LoadDecisions(canonical, skRevSkill)
	if err != nil {
		return err
	}
	if len(decisions) == 0 {
		return fmt.Errorf("skill %q 无 decisions.md——scoped revert 需决策记录的 CommitHash", skRevSkill)
	}

	withCommit := decisionsWithCommit(decisions)
	if len(withCommit) == 0 {
		return fmt.Errorf("skill %q 的 decisions 无 CommitHash——scoped revert 无锚点（记决策时加 --commit）", skRevSkill)
	}

	if skRevDecision == "" {
		fmt.Printf("skill %s 带 commit 的决策（用 --decision <id> 选择 revert 目标）：\n", skRevSkill)
		for _, d := range withCommit {
			fmt.Printf("  %s  [%s]  %s\n    %s\n", d.ID, d.Outcome, d.CommitHash, truncRunesCLI(d.Diagnosis, 60))
		}
		return nil
	}

	target := findDecisionByID(withCommit, skRevDecision)
	if target == nil {
		return fmt.Errorf(`决策 %q 不在带 commit 列表中（用 'revert --skill %s' 查看可用决策）`, skRevDecision, skRevSkill)
	}

	// git-dir derivation (preceding dry-run, dry-run also shows repo).
	// external source (--canonical / FORGE_SKILLS_CANONICAL): canonical is inside a git repo, toplevel is the repo root.
	// embed cache (~/.forge/skills-cache/embedded): user-level read-only snapshot, not in a git repo — scoped revert
	// is meaningless for the cache (revert cannot modify sources compiled into the binary). Optimizing/reverting embedded skills must happen in the forge source
	// repo (the skills/ source directory), pointing --canonical at the source repo. Provides actionable error rather than a generic not-a-git-repository.
	//
	// git-dir 推导（dry-run 前置，dry-run 也显 repo）。
	// external 源（--canonical / FORGE_SKILLS_CANONICAL）：canonical 在 git repo，toplevel 即 repo 根。
	// embed 缓存（~/.forge/skills-cache/embedded）：用户级只读快照，不在 git repo——scoped revert
	// 对缓存无意义（revert 改不动编译进二进制的源）。embedded skill 的优化/revert 须在 forge 源码
	// repo（skills/ 源目录）操作，用 --canonical 指向源 repo。给可行动错误而非通用 not-a-git-repository。
	var gitDir string
	if isExternal {
		// CombinedOutput (not Output): git's real diagnosis (e.g. "fatal: not a
		// git repository") goes to stderr; Output() would reduce the failure to a
		// bare "exit status 128" with the actionable message discarded.
		//
		// 用 CombinedOutput（非 Output）：git 的真实诊断（如 "fatal: not a git
		// repository"）在 stderr；Output() 会把失败压成裸 "exit status 128"，
		// 可操作的信息被丢弃。
		out, err := exec.Command(`git`, `-C`, canonical, `rev-parse`, `--show-toplevel`).CombinedOutput()
		if err != nil {
			return fmt.Errorf(`canonical %q 不在 git repo 内——scoped revert 需决策 CommitHash 所在仓库: %w (%s)`, canonical, err, strings.TrimSpace(string(out)))
		}
		gitDir = strings.TrimSpace(string(out))
		if gitDir == "" {
			return fmt.Errorf(`git rev-parse 返回空 root（canonical %q）`, canonical)
		}
	} else {
		return fmt.Errorf(`canonical 是 embed 缓存（%s），不在 git repo——embedded skill 的 scoped revert 须在 forge 源码 repo 操作：在原命令上加 --canonical <forge-repo>/skills（指向 forge 源码 skills/ 目录，决策 CommitHash 所在仓库），其他参数不变`, canonical)
	}

	if skRevDryRun {
		fmt.Printf(`[dry-run] (repo %s) git revert %s  ←  决策 %s [%s]
    %s
`, gitDir, target.CommitHash, target.ID, target.Outcome, truncRunesCLI(target.Diagnosis, 60))
		return nil
	}

	gitArgs := []string{`revert`}
	if !skRevEdit {
		gitArgs = append(gitArgs, `--no-edit`)
	}
	gitArgs = append(gitArgs, target.CommitHash)
	c := exec.Command(`git`, gitArgs...)
	c.Dir = gitDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf(`git revert %s 失败（可能冲突——手动解决后 'git revert --continue'，勿重跑本命令）: %w`, target.CommitHash, err)
	}

	// auto AppendDecision(outcome=reject): auto-trace after successful revert (spec-as-executable, replacing
	// advisory print relying on agent self-awareness — dogfood rule: pure self-awareness always leaks). Next-round agent reads decisions.md and knows
	// this optimization is rejected, avoiding repeated exploration of failed directions. revert commit hash (HEAD after revert) serves as the CommitHash anchor.
	// rev-parse failure (rare, e.g. git config suddenly broken) is not fatal — CommitHash stays empty (next round decisionsWithCommit
	// filters out this reject, cannot scoped-revert it, but the revert itself has succeeded), stderr hint is not silently swallowed.
	//
	// auto AppendDecision(outcome=reject)：成功 revert 后自动留痕（spec-as-executable，取代靠
	// agent 自觉的 advisory print——dogfood 铁律：纯靠自觉必漏）。下轮 agent 看 decisions.md 即知
	// 此优化被否决，避免重复探索已失败方向。revert commit hash（revert 后 HEAD）作 CommitHash 锚点。
	// rev-parse 失败（极少，如 git config 突坏）不致命——CommitHash 留空（下轮 decisionsWithCommit
	// 过滤掉此 reject，无法再 scoped revert 它，但 revert 本身已成功），stderr 提示不静默吞。
	revertCommit, revErr := exec.Command(`git`, `-C`, gitDir, `rev-parse`, `HEAD`).Output()
	if revErr != nil {
		fmt.Fprintf(os.Stderr, `⚠️ git rev-parse HEAD 失败，auto reject 决策 CommitHash 留空（不影响 revert 本身）: %v
`, revErr)
	}
	revertCommitHash := strings.TrimSpace(string(revertCommit))
	dec := skillsdecisions.SkillDecision{
		ID:         skillsdecisions.NewDecisionID(),
		Skill:      skRevSkill,
		Outcome:    skillsdecisions.OutcomeReject,
		Diagnosis:  fmt.Sprintf(`scoped revert 撤销决策 %s（commit %s）的优化：%s`, target.ID, target.CommitHash, target.Diagnosis),
		Revision:   fmt.Sprintf(`git revert %s`, target.CommitHash),
		Evidence:   fmt.Sprintf(`scoped revert 自动留痕（reject）；revert commit %s。请重跑 eval-report 确认回归消除`, revertCommitHash),
		CommitHash: revertCommitHash,
		Rationale:  `失败优化的 scoped revert，自动 reject 留痕供下轮 agent 知悉被否决方向（spec-as-executable，非自述）`,
	}
	// The revert-success summary prints first (revert itself has landed, independent of tracing outcome). Tracing failure does not mask revert success,
	// nor let revert's ✅ confuse "tracing failure" — the two signals are separated; user/agent judges by each signal.
	//
	// revert 成功总结先打（revert 本身已落地，独立于留痕结果）。留痕失败不掩盖 revert 成功，
	// 也不让 revert 的 ✅ 混淆「留痕失败」——两信号分离，用户/agent 据各自信号判断。
	fmt.Printf(`
✅ scoped revert %s（决策 %s，revert commit %s）
`, target.CommitHash, target.ID, revertCommitHash)
	if err := skillsdecisions.AppendDecision(canonical, skRevSkill, dec); err != nil {
		fmt.Fprintf(os.Stderr, `⚠️ 自动留痕失败（revert 已成功，手动补：forge skills decide --skill %s --outcome reject）: %v
`, skRevSkill, err)
		// stdout adds a ⚠️ line: tracing is best-effort (does not mask revert's exit 0 success), but the stdout-only view
		// (task gate / CI reads only stdout) must notice tracing failure, otherwise the next-round agent misses the reject decision and repeats the rejected direction.
		//
		// stdout 补 ⚠️ 行：留痕是 best-effort（不掩盖 revert 成功的 exit 0），但 stdout-only 视角
		// （task gate / CI 只读 stdout）须能察觉留痕失败，否则下轮 agent 漏看 reject 决策重复探索已否决方向。
		fmt.Printf(`   ⚠️ 留痕失败，见 stderr（revert 本身已成功）
`)
		return nil
	}
	fmt.Printf(`   ✅ 自动追加 reject 决策（%s）
`, dec.ID)
	return nil
}

// decisionsWithCommit returns decisions that have a CommitHash (pure function, easy to test). Decisions without a commit
// cannot be precisely scoped-reverted; callers filter accordingly.
//
// decisionsWithCommit 返回有 CommitHash 的决策（纯函数，便于测试）。无 commit 的决策
// 无法精准 scoped revert，调用方据此过滤。
func decisionsWithCommit(decisions []skillsdecisions.SkillDecision) []skillsdecisions.SkillDecision {
	var out []skillsdecisions.SkillDecision
	for _, d := range decisions {
		if d.CommitHash != "" {
			out = append(out, d)
		}
	}
	return out
}

// findDecisionByID finds a decision by ID in the list (pure function, easy to test). Returns nil if not found.
//
// findDecisionByID 按 ID 在列表里找（纯函数，便于测试）。未找到返回 nil。
func findDecisionByID(decisions []skillsdecisions.SkillDecision, id string) *skillsdecisions.SkillDecision {
	for i := range decisions {
		if decisions[i].ID == id {
			return &decisions[i]
		}
	}
	return nil
}

// truncRunesCLI truncates a string to n runes (appends ... if exceeded). Used for the revert list display.
//
// truncRunesCLI 把字符串截断到 n rune（超出加 ...）。给 revert 列表展示用。
func truncRunesCLI(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func init() {
	skillsRevertCmd.Flags().StringVar(&skRevSkill, "skill", "", "revert 哪个 skill 的优化")
	skillsRevertCmd.Flags().StringVar(&skRevDecision, "decision", "", "决策 ID（省略则列出可选决策）")
	skillsRevertCmd.Flags().BoolVar(&skRevEdit, "edit", false, "打开编辑器改 revert commit message（默认 --no-edit 跳过）")
	skillsRevertCmd.Flags().BoolVar(&skRevDryRun, "dry-run", false, "只打印将执行的 git revert，不实际跑")
	skillsCmd.AddCommand(skillsRevertCmd)
}

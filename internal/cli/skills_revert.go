package cli

// skills_revert.go — revert 子命令：scoped revert，按 decisions.md 的 CommitHash
// 撤销某条决策关联的 commit。
//
// scoped revert：精准撤销 skill 的某次优化（按决策记录的 CommitHash），而非整体回退
// skill 到旧版本。一次失败优化不该抹掉之前所有积累——scoped revert 只动那一个 commit，
// 其他历史优化保留。
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

建议：revert 后追加一条 reject 决策记录「为什么 revert」，让下一轮 agent 知道这次
优化被否决（forge skills decide --outcome reject）。`,
	RunE: runSkillsRevert,
}

func runSkillsRevert(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	if skRevSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
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
		return fmt.Errorf("决策 %q 不在带 commit 列表中（用 'revert --skill %s' 查看可用决策）", skRevDecision, skRevSkill)
	}

	if skRevDryRun {
		fmt.Printf("[dry-run] git revert %s  ←  决策 %s [%s]\n    %s\n", target.CommitHash, target.ID, target.Outcome, truncRunesCLI(target.Diagnosis, 60))
		return nil
	}

	gitArgs := []string{"revert"}
	if !skRevEdit {
		gitArgs = append(gitArgs, "--no-edit")
	}
	gitArgs = append(gitArgs, target.CommitHash)
	// git root 从 canonical 推导。canonical 必须在 git repo 内（scoped revert 按决策的
	// CommitHash 撤销 commit，无 repo 无意义）。设 c.Dir 确保 git revert 在正确 repo 跑，
	// 不依赖 forge 调用时的 CWD。rev-parse 失败（canonical 不在 repo）直接报错——不用
	// canonical 作 c.Dir fallback，否则 git 会向上找祖先里别的 .git 错配到无关 repo，
	// 静默 revert 比 not-a-git-repository 报错更糟。
	out, err := exec.Command("git", "-C", canonical, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("canonical %q 不在 git repo 内——scoped revert 需决策 CommitHash 所在仓库: %w", canonical, err)
	}
	gitDir := strings.TrimSpace(string(out))
	if gitDir == "" {
		return fmt.Errorf("git rev-parse 返回空 root（canonical %q）", canonical)
	}
	c := exec.Command("git", gitArgs...)
	c.Dir = gitDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git revert %s 失败（可能冲突——手动解决后 'git revert --continue'，勿重跑本命令）: %w", target.CommitHash, err)
	}
	fmt.Printf("\n✅ scoped revert %s（决策 %s）\n", target.CommitHash, target.ID)
	fmt.Printf("   建议追加 reject 决策：forge skills decide --skill %s --outcome reject --diagnosis <为何 revert> --revision <原 commit> --evidence <revert 后 probe/回归> --commit <revert 的 commit>\n", skRevSkill)
	return nil
}

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

// findDecisionByID 按 ID 在列表里找（纯函数，便于测试）。未找到返回 nil。
func findDecisionByID(decisions []skillsdecisions.SkillDecision, id string) *skillsdecisions.SkillDecision {
	for i := range decisions {
		if decisions[i].ID == id {
			return &decisions[i]
		}
	}
	return nil
}

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

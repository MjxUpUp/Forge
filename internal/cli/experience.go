package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/experience"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(experienceCmd)
	experienceCmd.AddCommand(experienceListCmd)
	experienceCmd.AddCommand(experienceShowCmd)
	experienceCmd.AddCommand(experienceAcceptCmd)
	experienceCmd.AddCommand(experienceRejectCmd)
	experienceCmd.AddCommand(experienceGenerateCmd)
	experienceCmd.AddCommand(experienceResolveCmd)

	experienceListCmd.Flags().Bool("json", false, "JSON 格式输出")
}

var experienceCmd = &cobra.Command{
	Use:   "experience",
	Short: "经验提取管理",
	Long: `forge experience 管理低分任务的经验审查和规则提案。

列出待审查任务和 AI 生成的经验规则提案，查看详情，接受或拒绝规则。`,
}

var experienceListCmd = &cobra.Command{
	Use:   "list [--json]",
	Short: "列出待审查任务和规则提案",
	RunE:  runExperienceList,
}

var experienceShowCmd = &cobra.Command{
	Use:   "show <task-ref>",
	Short: "显示审查详情和关联规则提案",
	Args:  cobra.ExactArgs(1),
	RunE:  runExperienceShow,
}

var experienceAcceptCmd = &cobra.Command{
	Use:   "accept <rule-id>",
	Short: "接受规则提案（写入经验库）",
	Args:  cobra.ExactArgs(1),
	RunE:  runExperienceAccept,
}

var experienceRejectCmd = &cobra.Command{
	Use:   "reject <rule-id>",
	Short: "拒绝规则提案",
	Args:  cobra.ExactArgs(1),
	RunE:  runExperienceReject,
}

var experienceGenerateCmd = &cobra.Command{
	Use:   "generate <task-ref>",
	Short: "为已有 review 回填经验规则提案",
	Long: `为已存在的 review 生成经验规则提案（每个低分维度一条）。

用于修复在自动生成机制上线之前创建的 review —— 这类 review 没有 proposal，
无法通过 accept 清除，mandatory review 卡在 pending。运行本命令后即可用
'forge experience accept <id>' 解除 pending。`,
	Args: cobra.ExactArgs(1),
	RunE: runExperienceGenerate,
}

var experienceResolveCmd = &cobra.Command{
	Use:   "resolve <task-ref>",
	Short: "直接解除 review（不依赖 proposal accept）",
	Long: `将 review 标记为 resolved，无需 accept 任何 proposal。

兜底路径：当 mandatory review 没有 proposal 可 accept（维度模板缺失、生成
失败、全部被 reject）时，task-verify 会因 pending mandatory review 阻塞会话。
本命令直接解除，作为 AcceptProposal 之外的独立 resolve 通路，彻底避免死锁。
正常情况下仍应优先用 'forge experience accept <id>' 把规则写入经验库。`,
	Args: cobra.ExactArgs(1),
	RunE: runExperienceResolve,
}

func runExperienceList(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	reviews, err := experience.ListReviews(root)
	if err != nil {
		return fmt.Errorf("failed to list reviews: %w", err)
	}

	proposals, err := experience.ListProposals(root, "")
	if err != nil {
		return fmt.Errorf("failed to list proposals: %w", err)
	}

	if asJSON {
		type listOutput struct {
			Reviews   []*experience.ReviewRequest      `json:"reviews"`
			Proposals []*experience.ExperienceProposal `json:"proposals"`
		}
		out := listOutput{
			Reviews:   reviews,
			Proposals: proposals,
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Text output — Reviews
	fmt.Println("Reviews:")
	if len(reviews) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, r := range reviews {
			mandatory := "optional"
			if r.Mandatory {
				mandatory = "mandatory"
			}
			fmt.Printf("  %-20s %s (%-3.0f)  %-9s %-9s %s\n",
				r.TaskRef, r.Grade, r.Score,
				mandatory, string(r.Status),
				r.CreatedAt.Format("2006-01-02"))
		}
	}

	fmt.Println()

	// Text output — Proposed Rules
	fmt.Println("Proposed Rules:")
	if len(proposals) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, p := range proposals {
			fmt.Printf("  %-20s %-30s %-10s %s\n",
				p.ID, truncateStr(p.Title, 30), p.Severity, string(p.Status))
		}
	}

	return nil
}

func runExperienceShow(cmd *cobra.Command, args []string) error {
	taskRef := args[0]

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	review, err := experience.LoadReview(root, taskRef)
	if err != nil {
		return err
	}

	fmt.Printf("Task: %s\n", review.TaskRef)
	fmt.Printf("Score: %.0f (%s)\n", review.Score, review.Grade)
	taskType := "optional"
	if review.Mandatory {
		taskType = "mandatory"
	}
	fmt.Printf("Type: %s\n", taskType)
	fmt.Printf("Status: %s\n", string(review.Status))
	fmt.Printf("Created: %s\n", review.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Println(strings.Repeat("─", 40))

	if len(review.LowDimensions) > 0 {
		fmt.Println("Low Dimensions:")
		for _, d := range review.LowDimensions {
			fmt.Printf("  %-20s %3d  %s\n", string(d.Dimension), d.Score, d.Detail)
		}
	}

	fmt.Println(strings.Repeat("─", 40))

	// Find proposals linked to this review
	linked, err := experience.ProposalsForReview(root, taskRef, "")
	if err != nil {
		linked = nil
	}

	fmt.Println("Proposed Rules:")
	if len(linked) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, p := range linked {
			fmt.Printf("  %s  %q  [%s]  %s\n", p.ID, p.Title, p.Severity, string(p.Status))
			for _, pat := range p.Patterns {
				fmt.Printf("    pattern: %s\n", pat)
			}
		}
	}

	return nil
}

func runExperienceAccept(cmd *cobra.Command, args []string) error {
	proposalID := args[0]

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	if err := experience.AcceptProposal(root, proposalID); err != nil {
		return err
	}

	fmt.Printf("✅ Rule %s accepted — added to knowledge store.\n", proposalID)
	return nil
}

func runExperienceReject(cmd *cobra.Command, args []string) error {
	proposalID := args[0]

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	if err := experience.RejectProposal(root, proposalID); err != nil {
		return err
	}

	fmt.Printf("❌ Rule %s rejected.\n", proposalID)
	return nil
}

func runExperienceGenerate(cmd *cobra.Command, args []string) error {
	taskRef := args[0]

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	n, err := experience.GenerateForExistingReview(root, taskRef)
	if err != nil {
		return err
	}

	fmt.Printf("✅ Generated %d proposal(s) for %s — run 'forge experience list' then 'forge experience accept <id>'.\n", n, taskRef)
	return nil
}

func runExperienceResolve(cmd *cobra.Command, args []string) error {
	taskRef := args[0]

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	if err := experience.ResolveReview(root, taskRef); err != nil {
		return err
	}

	fmt.Printf("✅ Review %s resolved.\n", taskRef)
	return nil
}

// truncate truncates s with "..." if it exceeds maxLen.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// ensure experience commands don't silently suppress errors —
// stderr is used only for non-fatal warnings; command errors
// propagate through cobra's RunE mechanism.
var _ = os.Stderr // keep import if needed by future warnings

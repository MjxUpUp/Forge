package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Harness/forge/internal/experience"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(experienceCmd)
	experienceCmd.AddCommand(experienceListCmd)
	experienceCmd.AddCommand(experienceShowCmd)
	experienceCmd.AddCommand(experienceAcceptCmd)
	experienceCmd.AddCommand(experienceRejectCmd)

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
			Reviews   []*experience.ReviewRequest    `json:"reviews"`
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
	proposals, err := experience.ListProposals(root, "")
	if err != nil {
		proposals = nil
	}

	var linked []*experience.ExperienceProposal
	for _, p := range proposals {
		if p.SourceReview == taskRef {
			linked = append(linked, p)
		}
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

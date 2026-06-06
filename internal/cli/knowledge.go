package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/Harness/forge/internal/knowledge"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(knowledgeCmd)
	knowledgeCmd.AddCommand(knowledgeListCmd)
	knowledgeCmd.AddCommand(knowledgeAddCmd)
	knowledgeCmd.AddCommand(knowledgeCheckCmd)

	knowledgeListCmd.Flags().String("category", "", "分类: gotchas, patterns, apis")
	knowledgeAddCmd.Flags().String("category", "", "分类: gotchas, patterns, apis (必填)")
	knowledgeAddCmd.Flags().String("file", "", "从文件读取内容")
	knowledgeAddCmd.Flags().String("title", "", "条目标题")
	knowledgeAddCmd.Flags().String("content", "", "条目内容")
	knowledgeAddCmd.Flags().StringSlice("patterns", nil, "检测模式（逗号分隔）")
	knowledgeAddCmd.Flags().String("severity", "error", "严重级别: error, warning, info")
}

var knowledgeCmd = &cobra.Command{
	Use:   "knowledge",
	Short: "跨项目经验库管理",
	Long:  "管理 ~/.forge/knowledge/ 中的跨项目开发经验。",
}

var knowledgeListCmd = &cobra.Command{
	Use:   "list [--category gotchas|patterns|apis]",
	Short: "列出经验条目",
	RunE:  runKnowledgeList,
}

var knowledgeAddCmd = &cobra.Command{
	Use:   "add --category <cat> [--title <t>] [--content <c>] [--file <f>] [--patterns <p,...>]",
	Short: "添加经验条目",
	RunE:  runKnowledgeAdd,
}

var knowledgeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "检查当前项目是否违反已知经验",
	RunE:  runKnowledgeCheck,
}

func runKnowledgeList(cmd *cobra.Command, args []string) error {
	idx, err := knowledge.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load knowledge index: %w", err)
	}

	category, _ := cmd.Flags().GetString("category")
	entries := idx.ListEntries(category)

	fmt.Printf("Knowledge Base (%d entries):\n", len(entries))
	fmt.Printf("%-20s %-12s %-30s %-10s\n", "ID", "Category", "Title", "Severity")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		id := e.ID
		if len(id) > 20 {
			id = id[:17] + "..."
		}
		fmt.Printf("%-20s %-12s %-30s %-10s\n", id, e.Category, truncateStr(e.Title, 30), e.Severity)
	}
	return nil
}

func runKnowledgeAdd(cmd *cobra.Command, args []string) error {
	category, _ := cmd.Flags().GetString("category")
	if category == "" {
		return fmt.Errorf("--category is required (gotchas, patterns, apis)")
	}
	if !knowledge.ValidCategories[category] {
		return fmt.Errorf("--category must be one of: gotchas, patterns, apis")
	}

	title, _ := cmd.Flags().GetString("title")
	content, _ := cmd.Flags().GetString("content")
	filePath, _ := cmd.Flags().GetString("file")
	patterns, _ := cmd.Flags().GetStringSlice("patterns")
	severity, _ := cmd.Flags().GetString("severity")

	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		if title == "" {
			title = filePath
		}
		content = string(data)
	}

	if title == "" || content == "" {
		return fmt.Errorf("--title and --content (or --file) are required")
	}

	idx, err := knowledge.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load knowledge index: %w", err)
	}

	dir, _ := os.Getwd()
	entry := knowledge.Entry{
		Category: category,
		Title:    title,
		Description: content,
		Source:   dir,
		Patterns: patterns,
		Severity: severity,
	}

	if err := idx.AddEntry(entry); err != nil {
		return fmt.Errorf("failed to add entry: %w", err)
	}

	fmt.Printf("Added '%s' to %s\n", title, category)
	return nil
}

func runKnowledgeCheck(cmd *cobra.Command, args []string) error {
	idx, err := knowledge.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load knowledge index: %w", err)
	}

	root, err := findProjectRoot()
	if err != nil {
		root, _ = os.Getwd()
	}

	fmt.Println("Scanning for known gotcha patterns...")
	violations := idx.CheckViolations(root)

	if len(violations) == 0 {
		fmt.Println("No known experience violations found.")
		return nil
	}

	fmt.Printf("Found %d violation(s):\n", len(violations))
	fmt.Print(knowledge.FormatViolations(violations))
	return fmt.Errorf("%d known experience violation(s) found", len(violations))
}

func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

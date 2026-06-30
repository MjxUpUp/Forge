package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/clone"
	"github.com/spf13/cobra"
)

func init() {
	cloneCmd.AddCommand(cloneCheckCmd)
	cloneCheckCmd.Flags().String("file", "", "File to check for clones")
	cloneCheckCmd.Flags().Float64("threshold", 0.7, "Similarity threshold (0.0-1.0)")
	rootCmd.AddCommand(cloneCmd)
}

var cloneCmd = &cobra.Command{
	Use:   "clone",
	Short: "Code clone detection",
	Long:  "Detect code duplication using token-level Jaccard similarity.",
}

var cloneCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check a file for code clones",
	RunE:  runCloneCheck,
}

func runCloneCheck(cmd *cobra.Command, args []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	threshold, _ := cmd.Flags().GetFloat64("threshold")

	if filePath == "" {
		return fmt.Errorf("--file is required")
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", filePath)
	}

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	results, err := clone.DetectClones(root, filePath, threshold)
	if err != nil {
		return fmt.Errorf("clone detection failed: %w", err)
	}

	if len(results) > 0 {
		for _, r := range results {
			fmt.Printf("  Similar: %s (%.0f%%)\n", r.FileB, r.Similarity*100)
		}
		os.Exit(1) // signal similar files found (shell scripts check exit code)
	}
	return nil
}

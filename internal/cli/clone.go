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
		// Return an error instead of os.Exit(1): os.Exit bypasses the deferred
		// chain (root Execute's panic recovery and any parent defers never run).
		// Root's Execute prints this error to stderr and exits 1, so shell
		// scripts checking the exit code are unaffected and stdout stays clean
		// (only the "Similar:" lines above).
		//
		// 返回 error 而非 os.Exit(1)：os.Exit 绕过 defer 链（root Execute 的
		// panic recovery 与上层 defer 都不会跑）。root 的 Execute 把该错误打到
		// stderr 并 exit 1，检查退出码的 shell 脚本不受影响，stdout 保持干净
		// （只有上面的 "Similar:" 行）。
		return fmt.Errorf("found %d similar file(s)", len(results))
	}
	return nil
}

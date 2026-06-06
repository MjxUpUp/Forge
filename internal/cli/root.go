package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Harness/forge/internal/pipeline"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "forge",
	Short: "Pipeline Engine — AI 开发质量门禁管道",
	Long: `forge 是开发阶段的质量门禁引擎。
	通过 .forge/pipeline.yml 定义门禁管道（v2 格式），
	配合 Claude Code Skill 驱动执行，hooks 硬拦截保障。`,
}

// SetVersion sets version info injected via -ldflags at build time.
func SetVersion(v, c, d string) {
	rootCmd.Version = v
	if v != "dev" {
		rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", v, c, d)
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		forgeDir := filepath.Join(dir, ".forge")
		if info, err := os.Stat(forgeDir); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a forge project (no .forge/ directory found)")
		}
		dir = parent
	}
}

func loadPipeline() (*pipeline.Pipeline, string, error) {
	root, err := findProjectRoot()
	if err != nil {
		return nil, "", err
	}
	p, err := pipeline.Load(root)
	if err != nil {
		return nil, root, err
	}
	return p, root, nil
}

func loadState() (*pipeline.State, string, error) {
	root, err := findProjectRoot()
	if err != nil {
		return nil, "", err
	}
	s, err := pipeline.LoadState(root)
	if err != nil {
		return nil, root, err
	}
	return s, root, nil
}

func jsonMarshal(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

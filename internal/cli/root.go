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
	Short: "AI 开发质量门禁管道",
	Long: `Forge — AI 开发质量门禁引擎

在 AI 生成的代码进入仓库前，通过结构化门禁管道进行质量锻造。
配合 Claude Code，从需求到发布全流程质量保障。

快速开始:
  forge init              在当前项目初始化管道
  forge status            查看管道执行状态
  forge gate <gate-id>    运行指定门禁

文档: https://github.com/MjxUpUp/forge`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Check for updates (24h cached, silent on failure)
		checkForUpdate(cmd.Root().Version, cmd)

		// Skip auto-sync for init command (project doesn't exist yet)
		if cmd.Name() == "init" {
			return nil
		}

		// Skip if not in a forge project (e.g. forge --version outside a project)
		dir, err := findProjectRoot()
		if err != nil {
			return nil
		}

		// Auto-sync .forge/ files to current binary version
		return autoSync(dir, cmd.Root().Version)
	},
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

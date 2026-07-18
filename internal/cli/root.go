package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/docsconsistency"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
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

文档: https://github.com/MjxUpUp/Forge`,
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
		return autoSync(dir, cmd.Root().Version, false)
	},
}

func init() {
	// 把 rootCmd 命令树注入 docsconsistency，让 task-complete advisory（taskpipeline 包）
	// 能反查 cobra 树检测文档里的 forge 命令 drift。回调打破 cli ↔ taskpipeline 循环：
	// docsconsistency 不 import cli，taskpipeline import docsconsistency 调 DriftedInProject。
	docsconsistency.RegisterCommandTree(func() *cobra.Command { return rootCmd })
}

// SetVersion sets version info injected via -ldflags at build time.
func SetVersion(v, c, d string) {
	rootCmd.Version = v
	if v != "dev" {
		rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", v, c, d)
	}
}

func Execute() {
	// graceful degradation (resilience §2.6 模式7 fail-open)：panic 时输出诊断到 stderr +
	// exit 2，保证 forge CLI 永不裸奔。dogfood 1.1：forge CLI panic 后偶发空 stdout 致
	// 解析端 EOF（DevWorkbench 159 次）。panic recovery 是 forge 侧收口——agent 看到
	// exit 2 + stderr 诊断而非静默崩溃。stdout 不输出（避免污染各命令输出语义）；hook
	// 命令的 stdout JSON 兜底由 runHook 负责（hook.go 永远输出合法 JSON）。
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "forge: internal panic: %v\n", r)
			os.Exit(2)
		}
	}()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func findProjectRoot() (string, error) {
	return projectroot.Find()
}

// findProject 解析 cwd → *forgedata.Project（三根：GitRoot/DataDir/ConfigDir）。
// runtime-state store（checklog/hazard/experience/act/...）的 caller 用它取 *Project，
// 走 DataDir；config reader（protocol/hooks）续用 findProjectRoot() 走 ConfigDir。
func findProject() (*forgedata.Project, error) {
	return projectroot.FindProject()
}

func jsonMarshal(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

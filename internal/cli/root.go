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
		// Check for updates (24h cache, silent on failure)
		//
		// 检查更新（24h 缓存，失败静默）
		checkForUpdate(cmd.Root().Version, cmd)

		// init command skips auto-sync (project does not exist yet)
		//
		// init 命令跳过 auto-sync（项目尚不存在）
		if cmd.Name() == "init" {
			return nil
		}

		// Skip for non-forge projects (e.g. forge --version run outside a project)
		//
		// 非 forge 项目跳过（如 forge --version 在项目外执行）
		dir, err := findProjectRoot()
		if err != nil {
			return nil
		}

		// Auto-sync .forge/ files to the current binary version
		//
		// 把 .forge/ 文件 auto-sync 到当前 binary version
		return autoSync(dir, cmd.Root().Version, false)
	},
}

func init() {
	// Inject the rootCmd command tree into docsconsistency so that the task-complete advisory
	// (taskpipeline package) can walk the cobra tree to detect forge command drift in docs.
	// The callback breaks the cli ↔ taskpipeline cycle: docsconsistency does not import cli,
	// taskpipeline imports docsconsistency and calls DriftedInProject.
	//
	// 把 rootCmd 命令树注入 docsconsistency，让 task-complete advisory（taskpipeline 包）
	// 能反查 cobra 树检测文档里的 forge 命令 drift。回调打破 cli ↔ taskpipeline 循环：
	// docsconsistency 不 import cli，taskpipeline import docsconsistency 调 DriftedInProject。
	docsconsistency.RegisterCommandTree(func() *cobra.Command { return rootCmd })
}

// SetVersion sets version info injected at build time via -ldflags.
//
// SetVersion 设置构建期经 -ldflags 注入的 version 信息。
func SetVersion(v, c, d string) {
	rootCmd.Version = v
	if v != "dev" {
		rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", v, c, d)
	}
}

func Execute() {
	// graceful degradation (resilience §2.6 pattern 7 fail-open): on panic, emit diagnostics to
	// stderr + exit 2 so the forge CLI never crashes raw. dogfood 1.1: forge CLI panics occasionally
	// produced empty stdout causing parser-side EOF (DevWorkbench 159 times). panic recovery is the
	// forge-side funnel — the agent sees exit 2 + stderr diagnostics instead of a silent crash.
	// Nothing is written to stdout (to avoid polluting per-command output semantics); the stdout
	// JSON fallback for hook commands is handled by runHook (hook.go always emits valid JSON).
	//
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

// findProject resolves cwd → *forgedata.Project (three roots: GitRoot/DataDir/ConfigDir).
// Callers of the runtime-state store (checklog/hazard/experience/act/...) use it to obtain
// *Project via DataDir; config readers (protocol/hooks) keep using findProjectRoot() via ConfigDir.
//
// findProject 解析 cwd → *forgedata.Project（三根：GitRoot/DataDir/ConfigDir）。
// runtime-state store（checklog/hazard/experience/act/...）的 caller 用它取 *Project，
// 走 DataDir；config reader（protocol/hooks）续用 findProjectRoot() 走 ConfigDir。
func findProject() (*forgedata.Project, error) {
	return projectroot.FindProject()
}

func jsonMarshal(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

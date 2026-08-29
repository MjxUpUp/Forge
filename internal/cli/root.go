package cli

import (
	"errors"
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
	// Silence cobra's own error/usage printing: Execute already prints the error
	// line to stderr itself (root.go Execute), so cobra's default would produce a
	// full usage dump plus a duplicated "Error: ..." line on every failure —
	// polluting the stderr of agent hosts that wrap forge.
	//
	// 静默 cobra 自己的错误/usage 打印：Execute 已自行向 stderr 打一行错误，
	// cobra 默认行为会让每次失败都 dump 完整 usage + 重复一行 "Error: ..."，
	// 污染包装 forge 的 agent 宿主 stderr。
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Check for updates (24h cache, silent on failure)
		//
		// 检查更新（24h 缓存，失败静默）
		checkForUpdate(cmd.Root().Version, cmd)

		// Self-heal npm's fragile Windows sh shim (coreutils-dependent) so
		// POSIX-shell hosts (kimi-code) resolve `forge` to the real binary. Quiet for
		// hook subcommands — their stderr may surface into the agent's context.
		//
		// 自愈 npm 的脆弱 Windows sh 垫片（依赖 coreutils），让 POSIX-shell
		// 宿主（kimi-code）能把 `forge` 解析到真实二进制。hook 子命令静默——
		// 其 stderr 可能进入 agent 上下文。
		healNpmShimIfNeeded(cmd.Name() == "hook")

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
	// Version source for the stale-binary hint on drift advisories (docsconsistency
	// cannot import cli; the callback keeps the version read lazy so SetVersion
	// ldflags injection order does not matter).
	//
	// drift advisory 版本提示的版本来源（docsconsistency 不能 import cli；回调惰性
	// 读取，SetVersion 的 ldflags 注入顺序不影响）。
	docsconsistency.RegisterVersion(func() string { return rootCmd.Version })
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

// hardExitError is the sentinel for commands whose VERDICT rides the process
// exit code rather than stderr prose (docs lint: hard failure ⇒ exit 2).
// Returning it from RunE (instead of calling os.Exit inside RunE) keeps cobra's
// deferred cleanup and Execute's panic-recovery funnel intact — os.Exit skips
// every defer, so the old in-RunE exits ran with no safety net. Execute maps it
// to os.Exit(2) without printing anything extra: the command has already
// printed its conclusion, mirroring the HookBlockError branch below.
//
// hardExitError 是「结论走进程退出码而非 stderr 散文」的命令哨兵（docs lint：
// 硬失败 ⇒ exit 2）。从 RunE 返回它（而非在 RunE 里 os.Exit）让 cobra 的 defer
// 清理与 Execute 的 panic 恢复盘保持生效——os.Exit 跳过所有 defer，旧的 RunE 内
// 退出等于裸奔。Execute 把它映射为 os.Exit(2) 且不额外打印：命令已输出自己的
// 结论，与下方 HookBlockError 分支同形。
type hardExitError struct{}

func (e *hardExitError) Error() string { return "hard failure (exit 2)" }

// errHardExit is the shared sentinel instance for the "hard failure ⇒ exit 2"
// exit-code contract (docs lint today; skills validate & friends may migrate
// later — they are outside this change's allowed file set).
//
// errHardExit 是「硬失败 ⇒ exit 2」退出码契约的共享哨兵实例（目前 docs lint；
// skills validate 等同款后续可迁移——不在本次允许改动文件清单内）。
var errHardExit error = &hardExitError{}

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
			// Exit code semantics: 2 is the hook BLOCK code (kimi: intentional deny;
			// Claude: non-2 errors are non-blocking). A forge-internal crash must
			// read as infrastructure failure, not as a gate verdict — exiting 2 here
			// turned every internal bug into a hard deny of every tool call, with no
			// escape except uninstalling hooks. Hook subcommands exit 1 (fail-open);
			// everything else keeps 2 (arbitrary non-zero for plain CLI usage).
			//
			// 退出码语义：2 是 hook 的阻断码（kimi：有意 deny；Claude：非 2 的错误
			// 不阻断）。forge 内部崩溃必须读作基础设施故障而非门禁裁决——此处
			// exit 2 会把每个内部 bug 变成对每次工具调用的硬拦，只能卸载 hooks
			// 逃生。hook 子命令 exit 1（fail-open）；其余保持 2（普通 CLI 用法下
			// 的任意非零码）。
			if len(os.Args) > 1 && os.Args[1] == "hook" {
				os.Exit(1)
			}
			os.Exit(2)
		}
	}()
	if err := rootCmd.Execute(); err != nil {
		// kimi hook protocol: an intentional block must exit 2 — any other non-zero
		// code would fail open (allow). The reason is already on stderr.
		//
		// kimi hook 协议：有意阻断必须 exit 2——其他非零退出码会 fail-open（放行）。
		// 原因已写在 stderr。
		var blockErr *HookBlockError
		if errors.As(err, &blockErr) {
			os.Exit(2)
		}
		// Verdict-by-exit-code commands (docs lint hard failure): same mapping as a
		// hook block, different semantics — the conclusion was already printed by
		// the command itself, so nothing is echoed here. Consumers reading the exit
		// code (docs lint: 2 = hard failure) see no change vs the old in-RunE
		// os.Exit(2).
		//
		// 以退出码传达结论的命令（docs lint 硬失败）：与 hook 阻断同映射、不同语义
		// ——结论已由命令自身打印，此处不再回显。读退出码的消费方（docs lint：
		// 2=硬失败）相对旧的 RunE 内 os.Exit(2) 零变化。
		var hex *hardExitError
		if errors.As(err, &hex) {
			os.Exit(2)
		}
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

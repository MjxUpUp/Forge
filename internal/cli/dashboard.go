package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/MjxUpUp/Forge/internal/dashboard"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(dashboardCmd)
	dashboardCmd.Flags().Int(`port`, 0, `监听端口（默认 0 = 系统分配临时端口）`)
	dashboardCmd.Flags().Bool(`no-open`, false, `不自动打开浏览器，仅打印 URL`)
}

// forge dashboard — local Pulse board. Starts an HTTP service that visualizes
// the quality data of ALL registered projects (global by default — no need to
// cd into each project directory).
//
// forge dashboard —— 本地 Pulse 看板。起 HTTP 服务可视化「全部已登记项目」的
// 质量数据（默认全局——无需切到各项目目录分别看）。
var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "本地全局质量看板（Pulse）——聚合全部登记项目，起 HTTP 服务可视化",
	Long: `forge dashboard 在本地起一个只读 web 看板（Pulse 面板），聚合 ~/.forge/projects.json
登记的所有项目：事件流（任务/gate/skill 触发/结论）、任务评分与证据链、skills 聚合。
服务绑定 localhost，自动打开浏览器，Ctrl+C 退出。

看板始终是全局维度——在任意目录运行都聚合全部登记项目，面板内可按项目侧栏过滤；
无需切换到项目目录。端口默认 0（系统分配临时端口），可用 --port 指定；
--no-open 仅打印 URL 不开浏览器。`,
	RunE: runDashboard,
}

func runDashboard(cmd *cobra.Command, args []string) error {
	port, _ := cmd.Flags().GetInt(`port`)
	noOpen, _ := cmd.Flags().GetBool(`no-open`)

	// Capture interrupt signals for graceful shutdown (dashboard.Serve blocks until
	// ctx is cancelled):
	// os.Interrupt = Ctrl+C (cross-platform); syscall.SIGTERM only takes effect on
	// POSIX platforms — Windows does not deliver SIGTERM (Task Manager end goes
	// through a different path) — registering it helps Linux/mac and is harmless on Windows.
	//
	// 捕获中断信号优雅关闭服务（dashboard.Serve 阻塞直到 ctx 取消）：
	// os.Interrupt = Ctrl+C（全平台）；syscall.SIGTERM 仅 POSIX 平台生效，Windows 不传
	// 递 SIGTERM（任务管理器结束走别的路径）——注册它对 Linux/mac 有用，Windows 无害。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The board is global-only now: aggregate all registered projects. Self-register
	// the current project first (compatibility for old projects that ran init but were
	// never registered).
	//
	// 看板现在只有全局维度：聚合所有已登记项目。先自登记当前项目（兼容已 init
	// 但未登记的老项目）。
	if cwd, err := os.Getwd(); err == nil {
		if _, statErr := os.Stat(filepath.Join(cwd, `.forge`)); statErr == nil {
			// Self-registration failure only warns — consistent with forge init; already
			// registered projects are still aggregated.
			//
			// 自登记失败仅警告——与 forge init 一致，已登记项目仍会聚合。
			if err := registry.Add(cwd); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to register project globally: %v\n", err)
			}
		}
	}
	roots := registry.List()
	if len(roots) == 0 {
		return fmt.Errorf(`无已登记项目——在项目目录跑 forge init 登记后再启动看板`)
	}

	opts := dashboard.Options{Port: port, OpenBrowser: !noOpen, Roots: roots}
	return dashboard.Serve(ctx, opts)
}

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
	dashboardCmd.Flags().Bool(`global`, false, `全局视图：聚合 ~/.forge/projects.json 登记的所有项目`)
}

// forge dashboard —— 本地质量看板。起 HTTP 服务把 .forge/ 质量数据可视化。
var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "本地质量看板——起 HTTP 服务可视化项目质量数据",
	Long: `forge dashboard 在本地起一个只读 web 看板，把 .forge/ 里的质量数据（任务分数走势、
证据盲区率、复发低分维度、最近任务结论）渲染成图形。服务绑定 localhost，自动打开浏览器，
Ctrl+C 退出。

数据源与 forge status / health / act 同根——dashboard 是这一组只读观测命令的可视化 home：
它们各自把 .forge/ 聚合成文本，dashboard 把同一份聚合渲染成图形，单一真相源。
端口默认 0（系统分配临时端口），可用 --port 指定；--no-open 仅打印 URL 不开浏览器。`,
	RunE: runDashboard,
}

func runDashboard(cmd *cobra.Command, args []string) error {
	port, _ := cmd.Flags().GetInt(`port`)
	noOpen, _ := cmd.Flags().GetBool(`no-open`)
	global, _ := cmd.Flags().GetBool(`global`)

	// 捕获中断信号优雅关闭服务（dashboard.Serve 阻塞直到 ctx 取消）：
	// os.Interrupt = Ctrl+C（全平台）；syscall.SIGTERM 仅 POSIX 平台生效，Windows 不传
	// 递 SIGTERM（任务管理器结束走别的路径）——注册它对 Linux/mac 有用，Windows 无害。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := dashboard.Options{Port: port, OpenBrowser: !noOpen}
	if global {
		// 全局视图：聚合所有已登记项目。自登记当前项目（兼容已 init 但未登记的老项目）。
		if cwd, err := os.Getwd(); err == nil {
			if _, statErr := os.Stat(filepath.Join(cwd, `.forge`)); statErr == nil {
				// 自登记失败仅警告——与 forge init 一致，全局视图是增强不阻塞（已登记项目仍聚合）。
				if err := registry.Add(cwd); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to register project globally: %v\n", err)
				}
			}
		}
		roots := registry.List()
		if len(roots) == 0 {
			return fmt.Errorf(`全局视图无已登记项目——在项目目录跑 forge init 登记后重试`)
		}
		opts.Roots = roots
	} else {
		root, err := findProjectRoot()
		if err != nil {
			return err
		}
		opts.Root = root
	}

	return dashboard.Serve(ctx, opts)
}

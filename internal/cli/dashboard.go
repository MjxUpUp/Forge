package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/MjxUpUp/Forge/internal/dashboard"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(dashboardCmd)
	dashboardCmd.Flags().Int(`port`, 0, `监听端口（默认 0 = 系统分配临时端口）`)
	dashboardCmd.Flags().Bool(`no-open`, false, `不自动打开浏览器，仅打印 URL`)
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
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	port, _ := cmd.Flags().GetInt(`port`)
	noOpen, _ := cmd.Flags().GetBool(`no-open`)

	// 捕获 Ctrl+C / SIGTERM 优雅关闭服务（dashboard.Serve 阻塞直到 ctx 取消）。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return dashboard.Serve(ctx, dashboard.Options{
		Root:        root,
		Port:        port,
		OpenBrowser: !noOpen,
	})
}

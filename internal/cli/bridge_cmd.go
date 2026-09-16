package cli

// bridge_cmd.go — H2a 静态级插件检查器（docs/design/forge-dsh-provider-roadmap.md
// §H2）：`forge bridge verify <dir>` 对一个 dsh 插件包跑 bridgeverify 的静态
// 检查面（#1884 四大事故模式）。判级口径对齐 compat report：error → exit 2，
// warn/info → exit 0，工具故障 → exit 1（errHardExit 哨兵，结论已先行打印）。

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/bridgeverify"
	"github.com/spf13/cobra"
)

func init() {
	bridgeVerifyCmd.Flags().Bool("json", false, "JSON 输出完整 finding 清单")
	rootCmd.AddCommand(bridgeCmd)
	bridgeCmd.AddCommand(bridgeVerifyCmd)
}

var bridgeCmd = &cobra.Command{
	Use:   "bridge",
	Short: "dsh 插件生态工具：插件包静态检查（bridge verify）",
}

var bridgeVerifyCmd = &cobra.Command{
	Use:   "verify <plugin-dir>",
	Short: "静态检查一个 dsh 插件包（inject 一致性 / 同名注册 / vendored 耦合 / 未知事件）",
	Long: `forge bridge verify 对一个 DeepSeek Harness (dsh) 插件包目录跑静态检查，
映射 dsh Discussions #1884 实证的四大插件事故模式：

  - manifest / 入口三件套（name / inject / apply）缺失或形迹可疑
  - inject 声明 vs ctx.<key> 取用一致性（缺声明 → 运行时 pending / 抛错）
  - 同包跨文件同名注册互相覆盖
  - import @deepseek-ai/* vendored 内部（宿主私有面，随 dsh 版本静默断裂）
  - ctx.on 名册之外的类型化事件（静默不触发）与外部依赖（pnpm 安装摩擦）

判级与退出码（对齐 compat report）：error → exit 2；warn/info → exit 0；
目录不可读等工具故障 → exit 1。这是静态半场（H2a）——运行时行为重放
（H2b）见 docs/design/forge-dsh-provider-roadmap.md。`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := bridgeverify.VerifyPluginDir(args[0])
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			body, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(body))
		} else {
			errCount, warnCount, infoCount := 0, 0, 0
			for _, f := range report.Findings {
				switch f.Severity {
				case bridgeverify.SevError:
					errCount++
				case bridgeverify.SevWarn:
					warnCount++
				case bridgeverify.SevInfo:
					infoCount++
				}
			}
			fmt.Printf("bridge verify：%s\n", report.Dir)
			if len(report.Findings) == 0 {
				fmt.Println("  （零发现——四模式检查面全过）")
			}
			for _, f := range report.Findings {
				mark := "  ·"
				switch f.Severity {
				case bridgeverify.SevError:
					mark = " ✗"
				case bridgeverify.SevWarn:
					mark = " ⚠"
				}
				fmt.Printf("%s %-14s %-15s %s\n", mark, f.Severity, f.Check, f.Message)
			}
			fmt.Printf("→ %d error / %d warn / %d info（error → exit 2）\n", errCount, warnCount, infoCount)
		}
		if report.Errors() > 0 {
			return errHardExit
		}
		return nil
	},
}

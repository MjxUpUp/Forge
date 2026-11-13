package cli

import (
	"errors"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/userconfig"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// config.go — forge config 子命令：用户级偏好。
// 当前仅 compat 一个键（兼容基线声明）；读取显示生效值，--raw 供脚本消费。

func init() {
	rootCmd.AddCommand(configCmd)
	configGetCmd.Flags().Bool(`raw`, false, `只输出值（供脚本消费）`)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
}

var configCmd = &cobra.Command{
	Use:   `config`,
	Short: `用户级偏好（compat 兼容基线声明）`,
	Long: `forge config 管理 forge 的用户级偏好（~/.forge/config.json）。

compat——本机行为按哪个 forge 版本的默认走（空 = 跟最新，不钉）。`,
}

var configGetCmd = &cobra.Command{
	Use:   `get <compat> [--raw]`,
	Short: `读取偏好（默认人类可读；--raw 只输出值供脚本消费）`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, _ := cmd.Flags().GetBool(`raw`)
		switch args[0] {
		case `compat`:
			v := userconfig.CompatPref()
			if raw {
				fmt.Println(v)
				return nil
			}
			if v == `` {
				fmt.Println(`compat = （未钉，跟最新；声明基线：forge config set compat <版本>——GODEBUG go 行声明的轻量版）`)
				return nil
			}
			behind := ``
			if d := compatLag(v); d > 0 {
				behind = fmt.Sprintf(`；落后当前版本 %d 个 minor——建议对齐或显式确认继续钉住（逃生舱使用回读见 forge eval dashboard）`, d)
			}
			fmt.Printf("compat = %s%s\n", v, behind)
			return nil
		default:
			return fmt.Errorf(`未知配置键 %q（当前仅 compat）`, args[0])
		}
	},
}

var configSetCmd = &cobra.Command{
	Use:   `set <compat> <值>`,
	Short: `设置偏好（校验合法值，原子写 ~/.forge/config.json）`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case `compat`:
			if err := userconfig.SetCompat(args[1]); err != nil {
				return err
			}
			if args[1] == `` {
				fmt.Println(`compat 已清除（跟最新）`)
				return nil
			}
			note := ``
			if compatLag(args[1]) >= 2 {
				note = `——注意：已落后当前 ≥2 个 minor，行为变更承诺窗（compat-commitments.md）可能不覆盖`
			}
			fmt.Printf("compat = %s（已钉基线%s）\n", args[1], note)
			return nil
		default:
			return fmt.Errorf(`未知配置键 %q（当前仅 compat）`, args[0])
		}
	},
}

// compatLag 计算声明基线落后当前版本多少个 minor（同 major 按 minor 差；跨
// major 返回大数；解析失败返回 0——提示性信号非判据）。
func compatLag(declared string) int {
	var amaj, amin, bmaj, bmin int
	if _, err := fmt.Sscanf(declared, "%d.%d", &amaj, &amin); err != nil {
		return 0
	}
	cur := util.GetCurrentVersion(rootCmd.Version)
	if _, err := fmt.Sscanf(cur, "%d.%d", &bmaj, &bmin); err != nil {
		return 0
	}
	if amaj != bmaj {
		return 99
	}
	return bmin - amin
}

// policy.go 的命令注册在同文件族——policy state 是人（与脚本）共用的接管
// 状态快查。
func init() {
	rootCmd.AddCommand(policyCmd)
	policyCmd.AddCommand(policyStateCmd)
	policyCmd.AddCommand(policyYieldCmd)
}

var policyCmd = &cobra.Command{
	Use:   `policy`,
	Short: `接管策略（Project Policy Layer）：本项目状态快查`,
	Long: `forge policy 管理/查询 per-project 接管策略。

state 输出当前目录的接管三态：
  managed   已接管（仓库契约 spec/forge.yaml 或全局注册表命中；阶段 D 起不装 hook，无 hook 执行面）
  declined  已退出（init/自动接管拒绝；恢复用 forge on）
  unknown   未登记

退出码恒 0（状态本身不是错误）——脚本可按输出值分流。`,
}

var policyStateCmd = &cobra.Command{
	Use:   `state`,
	Short: `打印当前目录接管状态（managed|declined|unknown；退出码恒 0）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _, state, err := projectroot.State(policyRoot())
		if err != nil {
			if errors.Is(err, forgedata.ErrNoForgeConfig) {
				fmt.Println(registry.StatusUnknown)
				return nil
			}
			return err
		}
		if state == registry.StatusManaged {
			// StatusManaged 是空串（零值兼容存量 JSON）——显示层映射为字面
			// "managed"，与帮助文案一致；bash 消费方只判 "declined"，不受影响。
			fmt.Println(`managed`)
			return nil
		}
		fmt.Println(state)
		return nil
	},
}

// compatStatusLine 渲染 forge status 的兼容基线行（P2-1 指纹分流 v1）：
// 未钉/已钉+落后 minor 数。遥测回读（逃生舱使用聚合）在 forge eval dashboard。
func compatStatusLine() string {
	v := userconfig.CompatPref()
	if v == `` {
		return `未钉（跟最新；forge config set compat <版本> 声明基线）`
	}
	if d := compatLag(v); d >= 2 {
		return fmt.Sprintf(`%s（落后 %d 个 minor——承诺窗可能不覆盖，建议对齐）`, v, d)
	} else if d > 0 {
		return fmt.Sprintf(`%s（落后 %d 个 minor）`, v, d)
	}
	return v + `（当前）`
}

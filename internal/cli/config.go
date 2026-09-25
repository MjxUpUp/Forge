package cli

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/userconfig"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// config.go — forge config 子命令：用户级偏好（Project Policy Layer P2）。
// 当前仅 takeover 一个键；读取显示生效值（含 env 覆盖），--raw 供脚本消费。

func init() {
	rootCmd.AddCommand(configCmd)
	configGetCmd.Flags().Bool(`raw`, false, `只输出值（供脚本消费，如 init-suggest bash）`)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
}

var configCmd = &cobra.Command{
	Use:   `config`,
	Short: `用户级偏好（takeover 接管模式：ask 出厂默认 / auto 静默接管 / off 保守）`,
	Long: `forge config 管理 forge 的用户级偏好（~/.forge/config.json）。

takeover——init-suggest 自动接管模式（Project Policy Layer P2）：
  ask   出厂默认。每项目首次接触询问一次（同意 → forge init；拒绝 → forge off）。
  auto  静默自动接管所有 git 项目（P2 之前的行为；declined 与外来 harness 让位仍然生效）。
  off   不接管新项目、不询问（已接管项目不受影响，forge off 单独退出）。

env 覆盖（优先于配置文件）：FORGE_TAKEOVER=ask|auto|off；FORGE_AUTO_INIT=1
（legacy）等价 auto。`,
}

var configGetCmd = &cobra.Command{
	Use:   `get <takeover|compat> [--raw]`,
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
		case `takeover`:
			persisted := userconfig.TakeoverPref()
			effective := userconfig.TakeoverMode()
			if raw {
				fmt.Println(effective)
				return nil
			}
			if persisted == `` {
				fmt.Printf(`takeover = %s（未设置，出厂默认；env 覆盖见 FORGE_TAKEOVER）`+"\n", effective)
				return nil
			}
			fmt.Printf(`takeover = %s（已设置；当前生效 %s）`+"\n", persisted, effective)
			return nil
		default:
			return fmt.Errorf(`未知配置键 %q（当前仅 takeover / compat）`, args[0])
		}
	},
}

var configSetCmd = &cobra.Command{
	Use:   `set <takeover|compat> <值>`,
	Short: `设置偏好（校验合法值，原子写 ~/.forge/config.json）`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case `takeover`:
			if err := userconfig.SetTakeover(args[1]); err != nil {
				return err
			}
			fmt.Printf(`takeover = %s（已保存。ask=每项目问一次；auto=静默接管；off=不接管不询问）`+"\n", args[1])
			return nil
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
			return fmt.Errorf(`未知配置键 %q（当前仅 takeover / compat）`, args[0])
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

// policy.go 的命令注册在同文件族——policy state 是 init-suggest bash 与人共用的
// 接管状态快查。
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
  managed   forge 接管中（项目级 hook 生效）
  declined  已退出（hook 静默；init/自动接管拒绝；恢复用 forge on）
  unknown   未登记

退出码恒 0（状态本身不是错误）——init-suggest bash 以输出值分流。`,
}

var policyStateCmd = &cobra.Command{
	Use:   `state`,
	Short: `打印当前目录接管状态（managed|declined|unknown；退出码恒 0）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, state := registry.State(policyRoot())
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

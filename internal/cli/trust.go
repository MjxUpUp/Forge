package cli

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/spf13/cobra"
)

// trust.go — `forge trust list|add|remove|require-signed`: the TOFU trust-store
// command surface (docs/design/node-identity.md §3). Trust establishment is an
// explicit human act: `add` prints the fingerprint for out-of-band confirmation.
//
// trust.go —— `forge trust list|add|remove|require-signed`：TOFU trust store 的
// 命令面（docs/design/node-identity.md §3）。信任建立是显式人肉动作：add 打印
// 指纹供带外核对。

func init() {
	rootCmd.AddCommand(trustCmd)
	trustCmd.AddCommand(trustListCmd, trustAddCmd, trustRemoveCmd, trustRequireSignedCmd)
	trustAddCmd.Flags().String(`label`, ``, `对端备注（如 "工作机"）`)
	trustAddCmd.Flags().String(`profile`, `team`, `信任 profile：personal | team`)
}

var trustCmd = &cobra.Command{
	Use:   `trust`,
	Short: `节点信任 store 管理（TOFU；团队档验签开关）`,
}

var trustListCmd = &cobra.Command{
	Use:   `list`,
	Short: `列出已登记节点`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ts, err := nodeid.LoadTrustStore()
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, `require_signed: %v\n`, ts.RequireSigned)
		if len(ts.Peers) == 0 {
			fmt.Fprintln(out, `（无已登记节点）`)
			return nil
		}
		for id, p := range ts.Peers {
			label := p.Label
			if label == `` {
				label = `—`
			}
			fmt.Fprintf(out, `%s  profile=%s  label=%s  added=%s\n`, id, p.Profile, label, p.AddedAt.Format(`2006-01-02`))
		}
		return nil
	},
}

var trustAddCmd = &cobra.Command{
	Use:   `add <node_id> <public_key_base64>`,
	Short: `登记对端节点（TOFU——先带外核对指纹再执行）`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		label, _ := cmd.Flags().GetString(`label`)
		profile, _ := cmd.Flags().GetString(`profile`)
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, `即将登记节点：\n  node_id:    %s\n  public_key: %s\n请确认该指纹来自对端 forge node show 的输出（带外核对）。\n`, args[0], args[1])
		ts, err := nodeid.LoadTrustStore()
		if err != nil {
			return err
		}
		if err := ts.Add(args[0], args[1], label, profile); err != nil {
			return err
		}
		if err := nodeid.SaveTrustStore(ts); err != nil {
			return err
		}
		fmt.Fprintf(out, `✅ 已登记 %s（profile=%s）\n`, args[0], profile)
		return nil
	},
}

var trustRemoveCmd = &cobra.Command{
	Use:   `remove <node_id>`,
	Short: `注销对端节点`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ts, err := nodeid.LoadTrustStore()
		if err != nil {
			return err
		}
		if err := ts.Remove(args[0]); err != nil {
			return err
		}
		if err := nodeid.SaveTrustStore(ts); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), `✅ 已注销 %s\n`, args[0])
		return nil
	},
}

var trustRequireSignedCmd = &cobra.Command{
	Use:   `require-signed <on|off>`,
	Short: `团队档开关：on 后 bundle 必须带有效签名且签名者已登记`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ts, err := nodeid.LoadTrustStore()
		if err != nil {
			return err
		}
		switch args[0] {
		case `on`:
			ts.RequireSigned = true
		case `off`:
			ts.RequireSigned = false
		default:
			return fmt.Errorf(`参数必须是 on|off`)
		}
		if err := nodeid.SaveTrustStore(ts); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), `✅ require_signed = %v\n`, ts.RequireSigned)
		return nil
	},
}

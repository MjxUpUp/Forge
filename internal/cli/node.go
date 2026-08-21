package cli

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(nodeCmd)
	nodeCmd.AddCommand(nodeShowCmd)
}

var nodeJSON bool

// nodeCmd groups machine-identity commands (docs/design/node-identity.md). v1 has only
// `show`; trust/rotate commands arrive with the team-profile phase.
//
// nodeCmd 归组机器身份命令（docs/design/node-identity.md）。v1 只有 `show`；
// trust/rotate 命令随团队 profile 阶段到来。
var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "机器节点身份（node_id = 公钥指纹）",
}

// nodeShowCmd prints the local machine identity. The private key is NEVER printed —
// the display surface carries only what a peer would need to trust this node
// (node_id + public key).
//
// nodeShowCmd 输出本机身份。私钥绝不打印——展示面只携带对端信任本节点所需的材料
// （node_id + 公钥）。
var nodeShowCmd = &cobra.Command{
	Use:   "show [--json]",
	Short: "显示本机节点身份（node_id + 公钥，不含私钥）",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := nodeid.LoadOrCreate()
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if nodeJSON {
			raw, err := json.MarshalIndent(map[string]any{
				`node_id`:        id.NodeID,
				`public_key`:     id.PublicKey,
				`created_at`:     id.CreatedAt,
				`rotation_chain`: id.RotationChain,
			}, ``, `  `)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, string(raw))
			return nil
		}
		fmt.Fprintf(out, "node_id:     %s\n", id.NodeID)
		fmt.Fprintf(out, "public_key:  %s\n", id.PublicKey)
		fmt.Fprintf(out, "created_at:  %s\n", id.CreatedAt.Format(`2006-01-02 15:04:05 UTC`))
		return nil
	},
}

func init() {
	nodeShowCmd.Flags().BoolVar(&nodeJSON, "json", false, "JSON 输出")
}

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/spf13/cobra"
)

func newChangeDriftCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "drift <id>",
		Short: "对照 change.md 声明的改动面与实际 git 改动（advisory，不阻断）",
		Long: `对照 change.md「## 改动」里点名的文件与自该 change 建立以来的实际改动。

只披露，不阻断：中途多改/漏改是正常的，问题在于偏差没人看见。退出码恒为 0
（除非仓库/change 读不出来）。建议在归档前跑一次，把偏差写进交付说明。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := changeRoot()
			if err != nil {
				return err
			}
			res, err := change.Drift(root, strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if asJSON {
				raw, merr := json.MarshalIndent(res, "", "  ")
				if merr != nil {
					return merr
				}
				fmt.Println(string(raw))
				return nil
			}
			fmt.Printf("change %s 的改动面漂移（advisory）\n", res.ChangeID)
			fmt.Printf("  声明 %d 个 · 实际 %d 个\n", len(res.Declared), len(res.Changed))
			if len(res.Undeclared) > 0 {
				fmt.Println("  改了但没声明：")
				for _, p := range res.Undeclared {
					fmt.Printf("    - %s\n", p)
				}
			}
			if len(res.Untouched) > 0 {
				fmt.Println("  声明了但没改：")
				for _, p := range res.Untouched {
					fmt.Printf("    - %s\n", p)
				}
			}
			if len(res.Undeclared) == 0 && len(res.Untouched) == 0 {
				fmt.Println("  ✅ 声明与实际一致")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出 JSON")
	return cmd
}

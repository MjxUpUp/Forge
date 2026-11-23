package agentproto

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// FromCobra 把一条 cobra 命令树拍平成投递面用的命令清单。
//
// 规则与 compat 快照面 1 保持一致（同一棵树、同一套剔除规则），否则「块里说的
// 命令在不在树里」会和 compat 的漂移检查对不上：cobra 惰性注入的 completion/help
// 不属于 forge 的 API 面；隐藏命令不对外承诺；--help 是 cobra 自带的，不是我们的契约。
func FromCobra(root *cobra.Command) []Command {
	if root == nil {
		return nil
	}
	var out []Command
	var walk func(cmd *cobra.Command, prefix string)
	walk = func(cmd *cobra.Command, prefix string) {
		if cmd.Name() == "completion" || cmd.Name() == "help" {
			return
		}
		path := prefix + cmd.Name()
		var flags []string
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" {
				return
			}
			flags = append(flags, f.Name)
		})
		sort.Strings(flags)
		out = append(out, Command{Path: path, Flags: flags})
		for _, sub := range cmd.Commands() {
			if sub.Name() == "help" || sub.Hidden {
				continue
			}
			walk(sub, path+" ")
		}
	}
	walk(root, "")
	// 投递面里的路径一律不带二进制名：散文写 `forge change new`，注解键是 `change new`。
	prefix := root.Name() + " "
	for i := range out {
		out[i].Path = strings.TrimPrefix(out[i].Path, prefix)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

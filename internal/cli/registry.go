package cli

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/spf13/cobra"
)

// forge registry 管理全局项目注册表 ~/.forge/projects.json——记录用户在哪些项目跑过 forge
//（forge init 自登记），供 forge dashboard 聚合全局 Pulse 面板。
//
// 注册表会累积死路径：测试历史残留 + 项目移走/删除后未淡出的条目。registry.List() 读时
// 惰性精简，但只在 forge dashboard 触发（该命令启 web server 阻塞，不适合纯清理）。
// 本命令组给用户一个不启动 web 的主动清理入口（dogfood registry 历史残留清理的治本缺口）。
//
// 字符串全用反引号 raw string：Windows 下 Edit/Write 写 Go 源码时 ASCII 双引号偶发被转成
// 中文弯引号致编译错（见 memory windows-input-quote-corruption），raw string 规避。

func init() {
	rootCmd.AddCommand(registryCmd)
	registryCmd.AddCommand(registryPruneCmd)
	registryCmd.AddCommand(registryRemoveCmd)
}

var registryCmd = &cobra.Command{
	Use:   `registry`,
	Short: `管理全局项目注册表（~/.forge/projects.json）`,
	Long: `forge registry 管理全局项目注册表 ~/.forge/projects.json。

注册表记录用户在哪些项目跑过 forge（forge init 自登记），供 forge dashboard
聚合全局 Pulse 面板。它会累积死路径（项目移走/删除/测试残留），子命令提供清理入口。

子命令：
  prune  精简注册表——移除项目路径已不存在的死路径与重复条目，原子写回
  remove 按路径移除单条条目——活路径也可删（prune 只能清死路径；误注册的
         保留根如用户 home 目录只能具名移除）
  rekey  把 from key 的项目数据目录并入 to key（修复大小写身份分裂的存量数据，--dry-run 可预览）`,
}

var registryRemoveCmd = &cobra.Command{
	Use:   `remove <path>`,
	Short: `按路径移除单条注册表条目（精确匹配，不做前缀）`,
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistryRemove,
}

func runRegistryRemove(cmd *cobra.Command, args []string) error {
	removed, err := registry.Remove(args[0])
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf(`注册表中没有命中 %s 的条目（按路径形态与大小写归一比较）——可查阅注册表文件（FORGE_DATA_HOME/projects.json，默认 ~/.forge/projects.json）核对现存条目的登记拼写`, args[0])
	}
	fmt.Fprintf(cmd.OutOrStdout(), `✅ 已移除注册表条目：%s
`, args[0])
	return nil
}

var registryPruneCmd = &cobra.Command{
	Use:   `prune`,
	Short: `精简全局注册表（移除死路径与重复条目）`,
	RunE:  runRegistryPrune,
}

func runRegistryPrune(cmd *cobra.Command, args []string) error {
	pruned, remain, err := registry.Prune()
	if err != nil {
		return err
	}
	if pruned == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), `注册表已是最精简，无需清理（%d 个活跃项目）。
`, remain)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), `✅ 已精简全局注册表：移除 %d 条死路径/重复，保留 %d 个活跃项目。
`, pruned, remain)
	return nil
}

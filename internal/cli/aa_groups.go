package cli

import "github.com/spf13/cobra"

// 命令分组：core-only 之后只剩核心命令。分组只影响 --help 的排列，
// 不改变任何命令的行为；GroupID 必须先在 root 上 AddGroup，否则 cobra 会 panic
// （"group id ... is not defined for subcommand"）——删掉旧分组文件后残留的
// GroupID 赋值正是这样炸出来的。
func init() {
	rootCmd.AddGroup(&cobra.Group{ID: "lifecycle", Title: "项目生命周期"})
	rootCmd.AddGroup(&cobra.Group{ID: "pipeline", Title: "项目管道"})
}

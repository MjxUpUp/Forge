package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(dataDirCmd)
}

// dataDirCmd 输出当前 cwd 的 runtime-state DataDir 绝对路径，供 hook bash
// 算 DataDir 用（refactor-data-home commit D）。hook bash 无法自己复现 Key 算法
// （FNV-64a + worktree .git file 解析 + EvalSymlinks），改调本子命令拿 DataDir，
// 再拼 checklog / throttle-stamp / quarantine 等路径。DataDirFor 语义：git 项目返
// ~/.forge/projects/<key>/，非 git 回退 <cwd>/.forge/。
//
// Hidden：内部命令，hook 用，不进用户 help 顶层列表。hook 已多次 fork forge
// （TaskVerifyHook 调 task gate / experience list / task status / act nudge），
// 多一次 data-dir 无感。
var dataDirCmd = &cobra.Command{
	Use:    "data-dir",
	Short:  "输出当前项目 runtime DataDir 路径（hook bash 用）",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		dd := forgedata.DataDirFor(cwd)
		// hook bash 用 `>> "$_DATA_DIR/..."` 追加——bash 重定向不创建父目录，DataDir 若
		// 不存在（首次 hook 触发 / 新项目）写入会静默失败（被 hook 的 `2>/dev/null || true`
		// 吞掉，checklog 凭空消失）。forge data-dir 顺带 mkdir -p，让 hook 拿到的路径必然
		// 可写。非 git fallback <cwd>/.forge 的 mkdir 无害（项目配置目录，通常已存在）。
		// mkdir 失败不致命：仍输出路径，hook 的 forge data-dir 调用失败时自身回退 .forge。
		_ = os.MkdirAll(dd, 0755)
		fmt.Fprintln(cmd.OutOrStdout(), dd)
		return nil
	},
}

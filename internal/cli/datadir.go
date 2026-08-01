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

// dataDirCmd prints the absolute runtime-state DataDir path of the current cwd, used by hook bash
// to compute DataDir (refactor-data-home commit D). Hook bash cannot reproduce the Key algorithm
// (FNV-64a + worktree .git file parsing + EvalSymlinks), so it calls this subcommand to get DataDir,
// then assembles checklog / throttle-stamp / quarantine paths. DataDirFor semantics (post
// user-level-assets): ALWAYS user-level — git project returns ~/.forge/projects/<key>/,
// non-git returns ~/.forge/projects/<path-key>/; nothing ever falls back into the project tree.
//
// dataDirCmd 输出当前 cwd 的 runtime-state DataDir 绝对路径，供 hook bash
// 算 DataDir 用（refactor-data-home commit D）。hook bash 无法自己复现 Key 算法
// （FNV-64a + worktree .git file 解析 + EvalSymlinks），改调本子命令拿 DataDir，
// 再拼 checklog / throttle-stamp / quarantine 等路径。DataDirFor 语义
// （user-level-assets 之后）：始终用户级——git 项目返 ~/.forge/projects/<key>/，
// 非 git 返 ~/.forge/projects/<path-key>/；永不回落到项目树内。
//
// Hidden: internal command, used by hooks, not in the user help top-level list. Hook already forks forge multiple times
// (TaskVerifyHook calls task gate / task status / act nudge),
// one more data-dir call is unnoticeable.
//
// Hidden：内部命令，hook 用，不进用户 help 顶层列表。hook 已多次 fork forge
// （TaskVerifyHook 调 task gate / task status / act nudge），
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
		// hook bash appends via redirection to $_DATA_DIR — bash redirection does not create parent dirs; if DataDir
		// does not exist (first hook fire / new project), writes silently fail (swallowed by hook error-suppression `2>/dev/null || true`,
		// checklog vanishes). forge data-dir also does mkdir -p so the path hook receives is guaranteed
		// writable. DataDir is always user-level now, so this mkdir never touches the project tree.
		// mkdir failure is not fatal: still prints the path.
		//
		// hook bash 用 `>> "$_DATA_DIR/..."` 追加——bash 重定向不创建父目录，DataDir 若
		// 不存在（首次 hook 触发 / 新项目）写入会静默失败（被 hook 的 `2>/dev/null || true`
		// 吞掉，checklog 凭空消失）。forge data-dir 顺带 mkdir -p，让 hook 拿到的路径必然
		// 可写。DataDir 现在始终用户级，此 mkdir 不碰项目树。
		// mkdir 失败不致命：仍输出路径。
		_ = os.MkdirAll(dd, 0755)
		fmt.Fprintln(cmd.OutOrStdout(), dd)
		return nil
	},
}

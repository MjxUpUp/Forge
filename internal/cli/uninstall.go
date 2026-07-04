package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

// uninstall.go — `forge uninstall` 一键反装：npm 全局包 + init-suggest markers。
//
// 设计：清 `npm uninstall -g @agent_forge/forge`（binary）+ `~/.forge/.init-suggested/`
// （per-project init 提示标记）。Plugin 卸载必须在 agent CLI 内交互跑（`/plugin
// uninstall forge@forge` 等不可脚本化）——打印指引。项目级 `.forge/` 不动（用户
// 决定是否留；若要清先跑 `forge init --reset` 或手动 rm -rf .forge/）。
//
// 测试钩子：FORGE_UNINSTALL_SKIP_NPM=1 跳过 npm 调用（测试或 npm 不可用场景）。
//
// 中文字符串 raw string（反引号）规避 Windows 输入引号腐蚀。

// uninstallClearMarkers 删 init-suggest marker 目录（<GlobalHome>/.init-suggested/）。
// 走 forgedata.GlobalHome()（FORGE_DATA_HOME 优先，否则 ~/.forge）——refactor-data-home
// commit E 统一真相源，与 suggest 命令 + init-suggest hook 读写同一 marker store（uninstall
// 是该 store 的清理路径，必须同根，否则 FORGE_DATA_HOME 用户清错地方留残留 marker）。
// exported for testability — RunE 调用此。返 (dir, removed bool)；GlobalHome 失败返 ("", false)。
func uninstallClearMarkers() (string, bool) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, false
	}
	dir := filepath.Join(home, `.init-suggested`)
	if err := os.RemoveAll(dir); err != nil {
		return dir, false
	}
	return dir, true
}

var uninstallCmd = &cobra.Command{
	Use:   `uninstall`,
	Short: `卸载 forge 二进制 + init-suggest 标记（plugin 卸载需在 agent CLI 内进行）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. npm uninstall -g @agent_forge/forge（测试 / 离线场景可 SKIP_NPM 跳过）
		if os.Getenv(`FORGE_UNINSTALL_SKIP_NPM`) != `1` {
			if _, err := exec.LookPath(`npm`); err == nil {
				npmCmd := exec.Command(`npm`, `uninstall`, `-g`, `@agent_forge/forge`)
				npmCmd.Stdout = os.Stdout
				npmCmd.Stderr = os.Stderr
				if err := npmCmd.Run(); err != nil {
					fmt.Fprintf(os.Stderr, `警告：npm uninstall 失败：%v（可能未通过 npm 装）`+"\n", err)
				}
			} else {
				fmt.Fprintf(os.Stderr, `警告：npm 不可用，跳过 binary 卸载`+"\n")
			}
		}

		// 2. 删除 init-suggest markers
		dir, ok := uninstallClearMarkers()
		if !ok {
			fmt.Fprintf(os.Stderr, `警告：删除 %s 失败`+"\n", dir)
		} else {
			fmt.Printf(`已清除 init-suggest 标记：%s`+"\n", dir)
		}

		// 3. plugin 卸载指引（agent CLI 内交互，不可脚本化）
		fmt.Println(`plugin 卸载须在 agent CLI 内交互运行：`)
		fmt.Println(`  Claude Code / Cursor:  /plugin uninstall forge@forge`)
		fmt.Println(`  Codex:                 codex plugin uninstall forge@forge`)
		fmt.Println(`  Copilot CLI:           copilot plugin uninstall forge@forge`)
		fmt.Println(`项目级 .forge/ 未动。如需清，在项目内跑 'forge init --reset' 或手动 rm -rf .forge/`)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

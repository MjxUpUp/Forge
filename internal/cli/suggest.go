package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

// suggest.go — forge suggest 子命令，管理 init-suggest SessionStart hook 的
// per-project 提示标记。装了 forge 后，用户在任意 git 项目开 Claude Code，
// init-suggest hook 首次输出"是否启用 forge"提示给 agent；用户拒绝时 agent 运行
// 'forge suggest decline' 写 declined 标记，hook 不再提示该项目（HITL：不自动 init
// 污染用户项目，靠 agent 询问 + 用户拒绝静默）。status 查看，reset 清除重新提示。
//
// 标记存储与 init-suggest hook 同一目录（~/.forge/.init-suggested/<tag>）。tag 用
// projectSuggestTag()=suggestTagFor(cwd)，按 git root 键控（不是 cwd）——与 hook 的
// FORGE_CWD_TAG（cli/hook.go 也调 suggestTagFor）一致，确保命令与 hook 读写同一标记，
// 无论 agent 从项目根还是子目录跑 decline（F1 修复：原按 cwd 键控，子目录 decline
// 写错 tag 致永久静默失效）。declined/suggested 两个值：declined=用户拒绝永久静默，
// suggested=hook 已提示过不重复（由 hook 写，命令只读写 declined + 清除）。
//
// 中文字符串用 raw string（反引号）包裹，规避 Windows 输入引号腐蚀（ASCII " 外侧
// 界定符被转中文弯引号会让 Go 编译失败，见 memory windows-input-quote-corruption）。

// suggestStateDir 是 init-suggest hook 的 per-project 提示标记目录，与 hook 脚本的
// ${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested 同路径。refactor-data-home commit E：
// 统一走 forgedata.GlobalHome()（FORGE_DATA_HOME 优先），与 hook bash 一致。
func suggestStateDir() string {
	home, err := forgedata.GlobalHome()
	if err != nil {
		home, _ = os.UserHomeDir()
		home = filepath.Join(home, ".forge")
	}
	return filepath.Join(home, ".init-suggested")
}

// projectSuggestTag 返回当前项目的提示标记 tag——与 init-suggest hook 的
// FORGE_CWD_TAG 一致（都走 suggestTagFor，按 git root 键控），确保命令与 hook
// 读写同一标记，无论从项目哪个子目录运行。
func projectSuggestTag() string {
	cwd, _ := os.Getwd()
	return suggestTagFor(cwd)
}

// suggestProjectName 返回当前项目的可读名称（git root 的 basename；非 git 目录回退
// 到 cwd 的 basename），供 suggest 命令输出让用户知道操作的是哪个项目。
// 盘根/空 basename 时（filepath.Base 对 "E:\" / "/" 返裸分隔符，见 memory
// windows-go-bash-pitfalls）回退全路径，避免显示 '\' 这类丑陋值。
func suggestProjectName() string {
	cwd, _ := os.Getwd()
	if root := findGitRoot(cwd); root != "" {
		if name := baseName(root); name != "" {
			return name
		}
	}
	if name := baseName(cwd); name != "" {
		return name
	}
	return cwd
}

// baseName 是 filepath.Base 的盘根安全包装：basename 退化为裸分隔符/点/空时返回 ""，
// 让调用方回退到全路径（suggestProjectName 用）。
func baseName(path string) string {
	b := filepath.Base(path)
	if b == string(filepath.Separator) || b == "." || b == "" {
		return ""
	}
	return b
}

var suggestCmd = &cobra.Command{
	Use:   `suggest`,
	Short: `管理项目 forge init 提示状态（init-suggest hook 的拒绝/查看/重置）`,
	Long: `forge suggest 管理 init-suggest SessionStart hook 的 per-project 提示标记。

init-suggest hook 在新 git 项目首次会话时提示 agent 询问是否启用 forge。用户拒绝时
运行 'forge suggest decline' 永久静默该项目（写 declined 标记，hook 不再提示）。
status 查看当前标记状态，reset 清除标记重新允许提示。`,
}

var suggestDeclineCmd = &cobra.Command{
	Use:   `decline`,
	Short: `永久静默当前项目的 forge init 提示`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := suggestStateDir()
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf(`create suggest state dir: %w`, err)
		}
		if err := os.WriteFile(filepath.Join(dir, projectSuggestTag()), []byte(`declined`), 0644); err != nil {
			return fmt.Errorf(`write decline marker: %w`, err)
		}
		fmt.Printf(`已标记项目 '%s'：不再提示 forge init。如需重新启用，运行 'forge suggest reset' 后 'forge init'。`+"\n", suggestProjectName())
		return nil
	},
}

var suggestStatusCmd = &cobra.Command{
	Use:   `status`,
	Short: `查看当前项目的 forge init 提示状态`,
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(filepath.Join(suggestStateDir(), projectSuggestTag()))
		if err != nil {
			// 区分"未标记"（正常）与其他读错误（如权限），避免把 permission denied
			// 误导成"未标记"（P3：原统一回未标记，silently wrong）。
			if !os.IsNotExist(err) {
				return fmt.Errorf(`read suggest marker: %w`, err)
			}
			fmt.Printf(`项目 '%s'：未标记（下次会话开始 init-suggest hook 会提示 forge init）`+"\n", suggestProjectName())
			return nil
		}
		// 人类可读映射（N5）：suggested/declined 裸值易误读，补语义。
		state := strings.TrimSpace(string(data))
		desc := state
		switch state {
		case `declined`:
			desc = `declined（已拒绝，永久静默）`
		case `suggested`:
			desc = `suggested（已提示过，下次不再提示）`
		}
		fmt.Printf(`项目 '%s' 提示状态：%s`+"\n", suggestProjectName(), desc)
		return nil
	},
}

var suggestResetCmd = &cobra.Command{
	Use:   `reset`,
	Short: `清除当前项目的提示标记（重新允许提示）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		marker := filepath.Join(suggestStateDir(), projectSuggestTag())
		// 报告真实错误（P3：原 Remove 失败也打印"已清除"，silently wrong）。
		// 文件不存在不算错误（reset 本就是要清除，幂等）。
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(`clear suggest marker: %w`, err)
		}
		fmt.Printf(`已清除项目 '%s' 的标记。下次会话开始会重新提示 forge init。`+"\n", suggestProjectName())
		return nil
	},
}

func init() {
	suggestCmd.AddCommand(suggestDeclineCmd)
	suggestCmd.AddCommand(suggestStatusCmd)
	suggestCmd.AddCommand(suggestResetCmd)
	rootCmd.AddCommand(suggestCmd)
}

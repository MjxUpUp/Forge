package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hookdispatch"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/spf13/cobra"
)

// off.go — Project Policy Layer P1 的对称命令面：forge off / forge on。
//
// 设计背景见 docs/design/project-policy-layer.md。退出语义红线：一条命令、立即
// 生效（下一条 hook 触发即不跑——IsMember/Find 已按 declined 收口）、升级不重置
// （状态在用户级 store）、无残留（零项目写入）、幂等。declined→managed 的唯一
// 通道是 forge on（SetStatus）；forge suggest decline/reset 是同一核心的兼容别名。
//
// 双写垫片：off 同时写 legacy `.init-suggested/<tag>` declined 标记——init-suggest
// bash 仍按标记拦截（含 FORGE_AUTO_INIT 前置检查）；P2 把 init-suggest 改为注册表
// 驱动后移除该垫片。

func init() {
	rootCmd.AddCommand(offCmd)
	offCmd.Flags().Bool(`all`, false, `退出全部已登记项目（一键全退）`)
	rootCmd.AddCommand(onCmd)
}

var offCmd = &cobra.Command{
	Use:   `off [--all]`,
	Short: `退出 forge 对本项目（或全部项目）的接管`,
	Long: `forge off 把当前项目（git 根，非 git 目录为 cwd）的接管状态置为 declined：
项目级 hook 全部静默放行，forge init / FORGE_AUTO_INIT / plugin 自动接管不再生效
（不会静默重置退出决定）。

--all 退出全部存活项目（一键全退）。

恢复：在项目内运行 'forge on'。对称退出语义：一条命令、立即生效、升级不重置。`,
	RunE: runOff,
}

var onCmd = &cobra.Command{
	Use:   `on`,
	Short: `恢复 forge 对本项目的接管`,
	Long: `forge on 把当前项目的接管状态从 declined 恢复为 managed（唯一恢复通道），
并清除 legacy 提示标记。从未登记的项目请先运行 'forge init'。`,
	RunE: runOn,
}

// policyRoot 解析策略操作的目标根：git 根（与 init-suggest 的 ROOT 同语义），
// 非 git 目录回退 cwd——forge off 在首次接管前就要可用（首次接触前退出）。
func policyRoot() string {
	cwd, _ := os.Getwd()
	if root := forgedata.FindGitRoot(cwd); root != `` {
		return root
	}
	return cwd
}

// declineProject 是 off/suggest-decline 共享核心：注册表置 declined + legacy 标记
// 双写（垫片，见文件头注）。
func declineProject(root, by string) error {
	if err := registry.SetStatus(root, registry.StatusDeclined, by); err != nil {
		return err
	}
	return writeSuggestMarker(hookdispatch.SuggestTagFor(root), `declined`)
}

// resumeProject 是 on/suggest-reset 共享核心：注册表置 managed + 清 legacy 标记。
func resumeProject(root, by string) error {
	if err := registry.SetStatus(root, registry.StatusManaged, by); err != nil {
		return err
	}
	return removeSuggestMarker(hookdispatch.SuggestTagFor(root))
}

// recordTakeoverAudit 把状态翻转落 checklog 审计行（观察类）。从未 init 的项目无
// DataDir——跳过（Entry 决策字段已是审计，不为此创建目录）。
func recordTakeoverAudit(root, action string) {
	if _, err := os.Stat(forgedata.DataDirFor(root)); err != nil {
		return
	}
	_ = checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckTakeoverPolicy,
		Passed:  true,
		Checked: true,
		Detail:  fmt.Sprintf(`takeover %s by user（project policy layer；注册表 Status 同步更新）`, action),
	})
}

func runOff(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool(`all`)
	if all {
		roots := registry.List() // 存活条目（含 declined；幂等重跑无害）
		n := 0
		for _, r := range roots {
			if err := declineProject(r, `forge off --all`); err != nil {
				fmt.Fprintf(os.Stderr, "warn: %s: %v\n", r, err)
				continue
			}
			recordTakeoverAudit(r, `off`)
			n++
		}
		fmt.Printf(`已退出 %d 个项目的接管；恢复单个项目：在项目内运行 'forge on'。`+"\n", n)
		return nil
	}

	root := policyRoot()
	if err := declineProject(root, `forge off`); err != nil {
		return fmt.Errorf(`forge off: %w`, err)
	}
	recordTakeoverAudit(root, `off`)
	fmt.Printf(`项目 '%s' 已退出 forge 接管（declined）：项目级 hook 全部静默，init/自动接管不再生效。`+"\n", baseName(root))
	fmt.Println(`恢复：在项目内运行 'forge on'。`)
	return nil
}

func runOn(cmd *cobra.Command, args []string) error {
	root := policyRoot()
	_, state := registry.State(root)
	switch state {
	case registry.StatusManaged:
		fmt.Println(`本项目已由 forge 接管（managed），无需动作。`)
		return nil
	case registry.StatusUnknown:
		return fmt.Errorf(`本项目未登记接管状态——首次启用请运行 'forge init'（forge on 只负责 declined → managed 的恢复）`)
	}

	if err := resumeProject(root, `forge on`); err != nil {
		return fmt.Errorf(`forge on: %w`, err)
	}
	recordTakeoverAudit(root, `on`)
	fmt.Printf(`项目 '%s' 已恢复 forge 接管（managed）。`+"\n", baseName(root))
	// 从未 init 的 declined 条目（off 先于 init 发生）：只翻状态，不擅自跑完整
	// init（它会写用户级 agent 配置——显式动作留给用户）。
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(root), `protocol.yml`)); os.IsNotExist(err) {
		fmt.Println(`提示：本项目尚未初始化完整接线，请运行 'forge init' 补全（现在不会被拒绝）。`)
	}
	return nil
}

// ensureNotDeclined is the Go-side hard gate for forge init: a declined project
// refuses (re)initialization — the only un-decline path is forge on. This closes
// the silent re-takeover paths (plugin auto-takeover / FORGE_AUTO_INIT both exec
// `forge init`) even when a caller bypasses the bash-side marker check.
//
// ensureNotDeclined 是 forge init 的 Go 侧硬门禁：declined 项目拒绝（重新）初始化
// ——去 declined 的唯一路径是 forge on。即便调用方绕过 bash 侧标记检查（plugin
// auto-takeover / FORGE_AUTO_INIT 都以 `forge init` 落地），此门禁兜底拒绝静默
// 复活。
func ensureNotDeclined(dir string) error {
	if _, state := registry.State(dir); state == registry.StatusDeclined {
		return registry.ErrDeclinedProject
	}
	return nil
}

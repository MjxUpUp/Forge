package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/harnessdetect"
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
// bash 的标记检查是廉价第一道（免子进程），注册表（forge policy state）是权威读侧；
// 两写侧并存一致（P2 设计决策，见 docs/design/project-policy-layer.md）。

func init() {
	rootCmd.AddCommand(offCmd)
	offCmd.Flags().Bool(`all`, false, `退出全部已登记项目（一键全退）`)
	offCmd.Flags().Bool(`commit`, false, `在仓库根写 .forge-decline 团队声明文件（提交后对所有协作者让位；deny-wins）`)
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
	commitDecl, _ := cmd.Flags().GetBool(`commit`)
	if commitDecl {
		// 声明按 git 根键控（registry.lookup 只在 git 分支认它）——非 git 目录写
		// 出的是永不生效的死文件，拒绝并说明。
		if forgedata.FindGitRoot(root) == `` {
			return fmt.Errorf(`--commit 需在 git 仓库内运行（.forge-decline 声明按 git 根生效）`)
		}
		// 团队声明：committed 文件，deny-wins 压过任何机器上的 managed 状态。
		// 内容为可选理由（人读）；存在性即语义。
		note := fmt.Sprintf("# forge-decline：本仓由自己的 harness 管理，forge 让位。\n# 由 forge off --commit 写于 %s；恢复：删除本文件后运行 forge on。\n", time.Now().Format(`2006-01-02`))
		if err := os.WriteFile(filepath.Join(root, registry.DeclineFileName), []byte(note), 0644); err != nil {
			return fmt.Errorf(`write %s: %w`, registry.DeclineFileName, err)
		}
		fmt.Printf(`已写入 %s（团队声明，建议 git commit 使其对所有协作者生效）。`+"\n", registry.DeclineFileName)
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

	// 团队声明文件优先处理：deny-wins 下不清除它，翻转注册表也无意义。
	declPath := filepath.Join(root, registry.DeclineFileName)
	if _, derr := os.Stat(declPath); derr == nil {
		if err := os.Remove(declPath); err != nil {
			return fmt.Errorf(`remove %s: %w`, registry.DeclineFileName, err)
		}
		fmt.Printf(`已移除 %s（团队声明；本地删除请随变更一并 git commit，否则他人 pull 后仍让位）。`+"\n", registry.DeclineFileName)
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

// policyYieldCmd — `forge policy yield`：外来 harness 让位检测（P4）。命中高置信
// 信号（internal/harnessdetect 单一真相源）→ 记 declined(by=foreign-harness) +
// legacy 标记（双写垫片）并打印让位说明（非空输出）；无信号静默（空输出）。
// init-suggest bash 以输出非空判定让位；显式 forge on 可覆盖（检测是默认让位，
// 不是禁止——探索共存是合法诉求）。
var policyYieldCmd = &cobra.Command{
	Use:   `yield`,
	Short: `外来 harness 检测：命中则让位（记 declined）并打印说明；无信号静默`,
	Long: `检测当前项目（git 根）的外来 harness 信号（spec-kit、项目级 .claude 接线、
.cursor/rules 等高置信形态）。命中 → forge 让位（declined, by=foreign-harness），
输出一行让位说明；无信号输出为空。显式 'forge on' 可覆盖让位。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := policyRoot()
		signal, hit := harnessdetect.Detect(root)
		if !hit {
			return nil
		}
		by := `foreign-harness:` + signal
		if err := declineProject(root, by); err != nil {
			return fmt.Errorf(`policy yield: %w`, err)
		}
		recordTakeoverAudit(root, `off`)
		fmt.Printf("[forge] 检测到本项目使用自有 harness（信号：%s），forge 已让位（declined）；如需接管运行 'forge on'。\n", signal)
		return nil
	},
}

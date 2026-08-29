package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/freeze"
	"github.com/spf13/cobra"
)

// forge freeze 是会话/项目级写入范围冻结的激活/解除/查询端（on-demand-guards
// /freeze 的 forge 侧落地）。执行端是 freeze-guard PreToolUse Write|Edit hook
// （hooks/embed.go），在 ForgeHookSpec 中接线在 task-guard 之前——freeze 阻断时
// 先给 freeze 原因而非 task 告警。
//
// 形态（契约，与 skill 侧已对齐——不可改）：
//   - forge freeze <路径>...  激活：硬阻断所有冻结路径外的 Write/Edit
//   - forge freeze --off      解除
//   - forge freeze --status   查看当前范围

var freezeOff bool
var freezeStatus bool
var freezeCheckPath string

func init() {
	rootCmd.AddCommand(freezeCmd)
	freezeCmd.AddCommand(freezeCheckCmd)
	freezeCmd.Flags().BoolVar(&freezeOff, "off", false, "解除 freeze（幂等）")
	freezeCmd.Flags().BoolVar(&freezeStatus, "status", false, "查看当前 freeze 状态")
	freezeCheckCmd.Flags().StringVar(&freezeCheckPath, "path", "", "要判定的目标路径（hook 内部用）")
}

var freezeCmd = &cobra.Command{
	Use:   "freeze <路径>...",
	Short: "冻结写入范围：激活后 Write|Edit 仅允许写入指定路径内",
	Long: `forge freeze 管理会话/项目级写入范围冻结（on-demand-guards /freeze 的 forge 侧落地）。

激活后 freeze-guard hook（PreToolUse Write|Edit）硬阻断所有冻结路径之外的
Write/Edit——「只改这里别动其他」的硬护栏，不依赖 agent 每回合自检。

  forge freeze <路径>...   激活（可多个路径；相对路径相对当前目录解析；
                           再次激活即替换范围）
  forge freeze --off       解除（幂等）
  forge freeze --status    查看当前 freeze 状态`,
	Args: cobra.ArbitraryArgs,
	RunE: runFreeze,
}

// freezeCheckCmd 由 freeze-guard hook 调用，判定单个 Write/Edit 目标。退出码
// 契约（bash hook 转发它）：0 = 放行（静默），1 = 阻断（原因在 stdout），其他
// 退出码 = check 自身失败（hook fail-open）。Hidden：非用户面向，但保留可手动
// 调用以便调试。
var freezeCheckCmd = &cobra.Command{
	Use:    "check --path <目标路径>",
	Short:  "判定目标路径是否在 freeze 允许范围内（hook 内部用）",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runFreezeCheck,
}

func runFreeze(cmd *cobra.Command, args []string) error {
	if freezeOff && freezeStatus {
		return fmt.Errorf("--off 与 --status 不能同时使用")
	}
	p, err := findProject()
	if err != nil {
		return err
	}
	switch {
	case freezeStatus:
		return printFreezeStatus(p)
	case freezeOff:
		if err := freeze.Deactivate(p); err != nil {
			return err
		}
		fmt.Println("✅ 已解除 freeze，Write|Edit 不再受冻结范围限制。")
		return nil
	default:
		if len(args) == 0 {
			return fmt.Errorf("需要至少一个路径参数（或用 --off 解除 / --status 查看）")
		}
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd: %w", err)
		}
		st, err := freeze.Activate(p, cwd, args)
		if err != nil {
			return err
		}
		fmt.Printf("🔒 已 freeze（%d 个路径）。此后 Write|Edit 仅允许写入：\n", len(st.Paths))
		for _, path := range st.Paths {
			fmt.Printf("  %s\n", path)
		}
		fmt.Println("解除：forge freeze --off；调整范围：forge freeze <新路径...>")
		return nil
	}
}

func printFreezeStatus(p *forgedata.Project) error {
	st, err := freeze.Load(p)
	if err != nil {
		return err
	}
	if st == nil || len(st.Paths) == 0 {
		fmt.Println("未激活 freeze。激活：forge freeze <路径>...")
		return nil
	}
	fmt.Printf("freeze 激活中（%d 个路径，更新于 %s）：\n", len(st.Paths), st.UpdatedAt.Format("2006-01-02 15:04:05"))
	for _, path := range st.Paths {
		fmt.Printf("  %s\n", path)
	}
	fmt.Println("Write|Edit 写入上述路径之外的文件将被 freeze-guard 阻断。解除：forge freeze --off")
	return nil
}

// runFreezeCheck 用退出码传达结论（hook 只读退出码）。os.Exit 绕过 cobra 的
// "Error:" stderr 噪声（与 runHazardConfirmed 同款模式）。任何基础设施问题都
// fail-open：无项目、缺 --path、状态文件损坏，都不得硬停每次编辑。
func runFreezeCheck(cmd *cobra.Command, args []string) error {
	if freezeCheckPath == "" {
		fmt.Fprintln(os.Stderr, "[freeze] check 缺 --path，fail-open 放行")
		os.Exit(0)
	}
	p, err := findProject()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[freeze] 无项目根（%v），fail-open 放行\n", err)
		os.Exit(0)
	}
	allowed, st, err := freeze.Check(p, freezeCheckPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[freeze] 状态读取失败（%v），fail-open 放行\n", err)
		os.Exit(0)
	}
	if allowed {
		os.Exit(0)
	}
	// 阻断：原因写 stdout——hook 把它包进 FAIL 行，成为 additionalContext
	// （agent 唯一能看到的通道）。
	fmt.Printf("目录已 freeze，仅允许写入: %s\n", strings.Join(st.Paths, "; "))
	fmt.Printf("当前目标 %s 不在允许范围内。解除: forge freeze --off；调整范围: forge freeze <新路径...>\n", freezeCheckPath)
	os.Exit(1)
	return nil // unreachable — 所有路径已 os.Exit
}

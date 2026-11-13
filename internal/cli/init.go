package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/harnessdetect"
	"github.com/MjxUpUp/Forge/internal/specroot"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("profile", "", "仓库契约档位: mini（默认）/ standard / full（旧接线档 lite 按 mini 处理）")
	initCmd.Flags().String("agents", "", "(deprecated, no-op) 宿主接线改用 forge integrations enable <host>")
	initCmd.Flags().Bool("project", false, "(deprecated, no-op) 团队模式已并入仓库契约 spec/forge.yaml")
	// Deprecated no-op flags. 项目级管道删除后 mode/fresh 已无意义，保留为隐藏 no-op
	// 维持向后兼容：旧脚本的 `forge init --mode medium` 不报错，只是无效。
	initCmd.Flags().String("mode", "", "(deprecated, no-op) 旧版项目规模标志，项目级管道删除后无意义")
	initCmd.Flags().Bool("fresh", false, "(deprecated, no-op) 旧版强制重新生成标志")
	_ = initCmd.Flags().MarkHidden("mode")
	_ = initCmd.Flags().MarkHidden("fresh")
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "在当前仓库声明 forge 契约（只写仓库内产物）",
	Long: `forge init 只写仓库内产物：
  spec/forge.yaml   — 成员资格锚点（仓库里有它即被认定为 forge 项目，clone 即用）
  spec/changes/     — change 产物目录（含 archive/）
  AGENTS.md         — forge 托管块（缺文件整份写、无块追加、有块就地替换）
  已存在的入口指针  — CLAUDE.md / .cursor/rules/forge.mdc 指到 AGENTS.md

它不写用户主目录。宿主接线（~/.claude/settings.json、~/.codex/hooks.json 等）
是显式 opt-in，用 'forge integrations enable <host>' 开启；退出用 'forge off'。

--project 曾是团队模式（项目级 .forge/ 资产）；契约随仓库走后该 flag 是 no-op。`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, _ []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Project Policy Layer P1 硬门禁：declined 项目拒绝（重新）初始化——恢复唯一
	// 通道是 forge on。门禁在写任何产物之前。
	if err := ensureNotDeclined(dir); err != nil {
		return err
	}

	// P4：外来 harness 信号 → 手动 init 是显式用户意图，继续但警告（覆盖默认
	// 让位是知情选择）。注意 .forge-decline 声明项目在上方门禁已被拒。
	warnForeignHarness(policyRoot())

	if agents, _ := cmd.Flags().GetString("agents"); strings.TrimSpace(agents) != "" && strings.TrimSpace(agents) != "auto" {
		fmt.Fprintln(os.Stderr, "Warning: --agents 已废弃——宿主接线改用 forge integrations enable <host>（本次忽略）")
	}
	if team, _ := cmd.Flags().GetBool("project"); team {
		fmt.Fprintln(os.Stderr, "Warning: --project 已废弃——契约随仓库走（spec/forge.yaml），不再写项目级 .forge/ 资产（本次忽略）")
	}

	profile, _ := cmd.Flags().GetString("profile")
	return initRepoContract(dir, profile)
}

// initRepoContract 是 init 的仓内实现（不读进程工作目录，便于测试）：声明仓库契约。
// 已声明时幂等同步 AGENTS.md 与指针，绝不覆盖 spec/forge.yaml。
func initRepoContract(dir, profile string) error {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile == "lite" {
		// lite 是旧的接线档位（hook 裁剪），不是仓库契约档位。仓库契约的最小档
		// 是 mini——语义都指向「最小集」，按 mini 落，并说明改动。
		fmt.Fprintln(os.Stderr, "提示: lite 是旧的接线档位；仓库契约最小档叫 mini，按 mini 处理")
		profile = specroot.ProfileMini
	}

	if specroot.Declared(dir) {
		if profile != "" {
			fmt.Fprintln(os.Stderr, "spec/forge.yaml 已存在：档位不覆盖（改档位请直接编辑该文件）")
		}
		if _, err := change.EnsureAgentsBlock(dir); err != nil {
			return err
		}
		reports, err := change.EnsureAgentPointers(dir, nil)
		if err != nil {
			return err
		}
		for _, r := range reports {
			if !r.Skipped {
				fmt.Printf("  %s 已同步（指针 → AGENTS.md）\n", r.File)
			}
		}
		fmt.Println("forge 契约已存在：已同步 AGENTS.md 与指针（spec/forge.yaml 未改）")
		return nil
	}

	created, err := change.Init(dir, profile)
	if len(created) > 0 {
		fmt.Println("本次改动（已就位）：")
		for _, p := range created {
			fmt.Printf("  + %s\n", p)
		}
		fmt.Println()
	}
	if err != nil {
		return err
	}

	fmt.Println("forge 仓库契约已就位（只写仓库内产物，不写用户主目录）")
	fmt.Println()
	fmt.Println("下一步：")
	fmt.Println("  forge integrations enable <host>     # 可选：把 hooks/指令接到宿主用户级配置（显式 opt-in）")
	fmt.Println("  forge change new <id> --title \"...\"  # 建一个 change")
	fmt.Println("  forge status                         # 看当前状态")
	return nil
}

// warnForeignHarness prints an advisory when the project shows foreign-harness
// signals — manual init overrides the default yield, and that override should
// be an informed choice (P4). Advisory only; never blocks.
//
// warnForeignHarness 在项目带外来 harness 信号时打印警告——手动 init 覆盖默认
// 让位应是知情选择（P4）。仅 advisory，永不阻断。
func warnForeignHarness(root string) {
	if signal, hit := harnessdetect.Detect(root); hit {
		fmt.Fprintf(os.Stderr, "Warning: 检测到自有 harness 信号（%s）——默认让位已被本次手动 init 覆盖；若非本意请运行 'forge off'。\n", signal)
	}
}

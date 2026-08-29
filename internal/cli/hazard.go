package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/spf13/cobra"
)

// forge hazard 让 on-demand-guards 的高危命令拦截成为自动挡，并落地 human-in-the-loop。
//
// 形态（Forge hook 模型只有 approve/block，调不起各 AI 工具私有的确认弹窗）：
//   - PreToolUse Bash hook hazard-guard 检测高危命令 → block + additionalContext 指引：
//     用户本回合已明确指令/确认过该操作时 agent 直接 confirm 登记，无需二次确认；
//     否则先用所在工具的提问确认机制向用户说明风险获明确确认。
//   - agent 获确认后 `forge hazard confirm "<命令>"` 登记限时（5min）标记 → 重试原命令 →
//     hook 见标记放行。
//
// 本命令组是 HITL 闭环的"登记/查询"端；高危模式检测在 hooks/embed.go HazardGuardHook。

func init() {
	rootCmd.AddCommand(hazardCmd)
	hazardCmd.AddCommand(hazardConfirmCmd)
	hazardCmd.AddCommand(hazardFingerprintCmd)
	hazardCmd.AddCommand(hazardConfirmedCmd)
	hazardCmd.AddCommand(hazardStatusCmd)
	hazardCmd.AddCommand(hazardLogCmd)

	// --fingerprint：hook 已用 forge hazard fingerprint 算好指纹，agent 直接回传 hex
	// 登记确认。指纹是 sha256 hex（仅 [0-9a-f]），复制无引号/转义失真风险——而回传
	// 命令串会被 agent shell 重新解析吃掉引号（如 SQL mysql -e 'DROP TABLE t' 的单引号），
	// 与 hook 原始命令指纹不一致、确认后仍被拦。见 hazard.ConfirmByFingerprint。
	hazardConfirmCmd.Flags().StringVar(&hazardConfirmFingerprint, "fingerprint", "",
		"直接按 hook 输出的 hex 指纹登记确认（避免命令串复制失真）")
	// --last 免复制路径：直接从事件审计日志确认最新被拦命令。64 字符 hex 指纹或命令串
	// 的转写都已被证实是失真源（手抄错字；裸命令 confirm 与 hook 全行指纹失配）。
	// block 事件由 hook 自己写入——其指纹根本无需复制。见 hazard.ConfirmLastBlock。
	hazardConfirmCmd.Flags().BoolVar(&hazardConfirmLast, "last", false,
		"确认最近一条被拦命令（从事件日志取指纹，免复制转写）")
}

var hazardCmd = &cobra.Command{
	Use:   "hazard",
	Short: "高危命令 human-in-the-loop 确认管理",
	Long: `forge hazard 管理 on-demand-guards 自动挡的"高危命令已确认"标记，支撑 human-in-the-loop。

hazard-guard hook 拦截高危命令（rm -rf / git push --force / DROP TABLE / kubectl delete /
DELETE 无 WHERE 等）后：若用户在本回合已明确指令/确认过该操作，可直接 confirm --last
登记放行，无需二次确认；否则先用你所在工具的提问确认机制向用户说明风险、获明确确认，
再 confirm 登记限时标记（5min 内同命令重试放行）。这是 Forge hook 模型下 HITL 的落地
形态——Forge 不直接弹各工具的确认框，靠 block + 指引 + 限时标记闭环。

子命令：
  confirm <命令> [--fingerprint <hex>] [--last]
                     登记一次确认（5min 内同命令重试放行）；--fingerprint 直接按
                     hook 输出的 hex 指纹登记（避免命令串复制失真）；--last 确认最近
                     一条被拦命令（从事件日志取指纹，免任何复制转写，推荐）。
                     --last 与 --fingerprint 同给时 --last 优先（后者被忽略）
  fingerprint <命令> 算命令指纹（hook 内部用）
  confirmed <指纹>   查指纹是否已确认（hook 内部用，exit 0=是/1=否）
  status             列出当前有效确认`,
}

var hazardConfirmCmd = &cobra.Command{
	Use:   "confirm <命令>",
	Short: "登记一次高危命令确认（5min 内同命令重试放行）",
	Args: func(cmd *cobra.Command, args []string) error {
		// --fingerprint 路径不需要命令参数（指纹已含信息）；--last 也不需要（最新
		// block 事件已含全部信息）；否则需命令参数算指纹。
		if cmd.Flags().Changed("fingerprint") || cmd.Flags().Changed("last") {
			return nil
		}
		if len(args) < 1 {
			return fmt.Errorf("需要命令参数，或用 --fingerprint 按指纹登记，或用 --last 确认最近被拦命令")
		}
		return nil
	},
	RunE: runHazardConfirm,
}

// hazardConfirmFingerprint 由 --fingerprint flag 注入。非空时走 ConfirmByFingerprint
// 路径（hook 已算好指纹，绕过命令串复制失真）。
var hazardConfirmFingerprint string

// hazardConfirmLast 由 --last flag 注入：确认审计日志中最新一条 block 事件
// （免复制 HITL 路径，见 hazard.ConfirmLastBlock）。
var hazardConfirmLast bool

var hazardFingerprintCmd = &cobra.Command{
	Use:    "fingerprint <命令>",
	Short:  "算命令指纹（hook 内部用）",
	Args:   cobra.MinimumNArgs(1),
	RunE:   runHazardFingerprint,
	Hidden: true,
}

var hazardConfirmedCmd = &cobra.Command{
	Use:    "confirmed <指纹>",
	Short:  "查指纹是否已确认（hook 内部用）",
	Args:   cobra.ExactArgs(1),
	RunE:   runHazardConfirmed,
	Hidden: true,
}

var hazardStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "列出当前有效确认",
	RunE:  runHazardStatus,
}

// hazardLogCmd 由 hazard-guard hook 内部调用，追加事件到 events.jsonl 审计日志。
// Hidden：非用户面向（hook 用），但保留可手动调用以便调试审计流。
var hazardLogCmd = &cobra.Command{
	Use:    "log <type> <命令>",
	Short:  "追加一条 hazard 事件到审计日志（hook 内部用）",
	Args:   cobra.MinimumNArgs(1),
	Hidden: true,
	RunE:   runHazardLog,
}

// runHazardConfirm 登记确认。MinimumNArgs(1) + Join：agent 可引号传整串，也可不引号
// （多 arg 被空格 join 还原）——空白归一在 hazard.Fingerprint 内做，两种传法同指纹。
func runHazardConfirm(cmd *cobra.Command, args []string) error {
	// --last 免复制路径：直接从事件流确认最新被拦命令（指纹是 hook 拦截时写入的，
	// 天然权威、零转写）。最先判定：--last 表达的意图就是"刚被拦的那条"，不需要
	// 其他输入。
	if hazardConfirmLast {
		p, err := findProject()
		if err != nil {
			return err
		}
		fp, command, err := hazard.ConfirmLastBlock(p)
		if err != nil {
			return fmt.Errorf("failed to confirm last block: %w", err)
		}
		ttlMin := int(hazard.ConfirmTTL / time.Minute)
		fmt.Printf("✅ 已确认最近被拦命令（指纹 %s，%d 分钟内同命令重试放行）：\n  %s\n重试原命令即可。\n",
			shortFingerprint(fp), ttlMin, command)
		return nil
	}
	// --fingerprint 格式校验前置（在 findProjectRoot 前）：格式校验是纯输入校验，不需要
	// 项目上下文。CI 等无 .forge/ 环境下避免 not-in-a-forge-project 掩盖指纹校验失败——
	// agent 抄错指纹应被明确拒绝。与 ConfirmByFingerprint 同源校验。
	if hazardConfirmFingerprint != "" {
		if err := hazard.ValidateFingerprint(hazardConfirmFingerprint); err != nil {
			return err
		}
	}
	p, err := findProject()
	if err != nil {
		return err
	}
	ttlMin := int(hazard.ConfirmTTL / time.Minute)
	// --fingerprint 路径：hook 已算好指纹，agent 回传 hex（复制无失真）。命令串仅审计用。
	if hazardConfirmFingerprint != "" {
		// May be empty (not enforced when --fingerprint is set)
		command := strings.Join(args, " ") // 可空（--fingerprint 时不强制）
		if err := hazard.ConfirmByFingerprint(p, hazardConfirmFingerprint, command); err != nil {
			return fmt.Errorf("failed to confirm hazard: %w", err)
		}
		fmt.Printf("✅ 已确认高危命令（指纹 %s，%d 分钟内同命令重试放行）。重试原命令即可。\n",
			hazardConfirmFingerprint[:12], ttlMin)
		return nil
	}
	command := strings.Join(args, " ")
	fp, err := hazard.Confirm(p, command)
	if err != nil {
		return fmt.Errorf("failed to confirm hazard: %w", err)
	}
	fmt.Printf("✅ 已确认高危命令（指纹 %s，%d 分钟内同命令重试放行）。重试原命令即可。\n",
		fp[:12], ttlMin)
	return nil
}

// runHazardFingerprint 只打印指纹（hook 脚本用 $(forge hazard fingerprint ...) 捕获，
// 输出必须干净——无额外文字）。
func runHazardFingerprint(cmd *cobra.Command, args []string) error {
	command := strings.Join(args, " ")
	fmt.Println(hazard.Fingerprint(command))
	return nil
}

// runHazardConfirmed 用 exit code 传达结果（hook 脚本只读退出码）。os.Exit 绕过 cobra
// 的 "Error:" stderr 噪声。
func runHazardConfirmed(cmd *cobra.Command, args []string) error {
	p, err := findProject()
	if err != nil {
		// No project root -> treat as unconfirmed (fail-safe: block and re-confirm)
		os.Exit(1) // 无项目根 → 视为未确认（fail-safe：拦了重新确认）
	}
	ok, err := hazard.IsConfirmed(p, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "[hazard] %v\n", err)
		os.Exit(1)
	}
	if ok {
		os.Exit(0)
	}
	os.Exit(1)
	// unreachable — all paths have already called os.Exit
	return nil // unreachable — 所有路径已 os.Exit
}

// runHazardLog 由 hazard-guard hook 调用，追加一条事件到 events.jsonl。hook 是 bash，
// 直接写 jsonl 不安全（命令串引号/特殊字符破坏 JSON），故由 Go 端安全序列化。
// args[0]=事件类型（block/release/data），args[1:]=命令串（join 还原，与 confirm 同款）。
// 无项目根时静默跳过——审计不该污染非 forge 项目；失败由 hook 调用处 `|| true` 兜底，
// 审计失败绝不影响 hook 主流程（block/放行决策）。
func runHazardLog(cmd *cobra.Command, args []string) error {
	p, err := findProject()
	if err != nil {
		return nil
	}
	eventType := args[0]
	command := strings.Join(args[1:], " ")
	return hazard.AppendEvent(p, hazard.Event{
		Type:        eventType,
		Fingerprint: hazard.Fingerprint(command),
		Command:     command,
	})
}

func runHazardStatus(cmd *cobra.Command, args []string) error {
	p, err := findProject()
	if err != nil {
		return err
	}
	// 近 24h 事件统计（来自 events.jsonl 审计日志）：让用户看到 hazard-guard 的工作量
	// 与潜在误伤规模，而非只有"当前有效确认"——补全 2026-06 误伤审计只能扒 checklog 的痛点。
	since := time.Now().Add(-24 * time.Hour)
	blocks, berr := hazard.CountSince(p, hazard.EventBlock, since)
	releases, rerr := hazard.CountSince(p, hazard.EventRelease, since)
	data, derr := hazard.CountSince(p, hazard.EventData, since)
	if berr != nil || rerr != nil || derr != nil {
		// 审计日志不可读绝不能渲染成"零活动"（H-1 死探针伪装健康模式）——
		// 撤掉统计行而不是给出误导性数字。
		fmt.Printf("⚠ 事件日志不可读（%v / %v / %v），24h 统计不可用\n", berr, rerr, derr)
	} else {
		fmt.Printf("近 24h 事件：拦截 %d、确认放行 %d、数据上下文放行 %d\n", blocks, releases, data)
		fmt.Println(`  详见 hazards 事件日志：` + p.HazardsEventsPath())
	}

	active, err := hazard.ActiveConfirmations(p)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		fmt.Println("\n无有效确认。高危命令将被 hazard-guard 拦截，需确认后 forge hazard confirm 登记。")
		return nil
	}
	fmt.Printf("\n当前有效确认（%d 条，按剩余时间升序）：\n", len(active))
	now := time.Now()
	for _, c := range active {
		remaining := c.ExpiresAt.Sub(now).Round(time.Second)
		cmd := c.Command
		if cmd == "" {
			cmd = "(未记录命令)"
		}
		fmt.Printf("  %s  剩余 %-5s  %s\n", shortFingerprint(c.Fingerprint), remaining, cmd)
	}
	return nil
}

// shortFingerprint 返回确认指纹的展示前缀。指纹从磁盘确认文件读回（不可信
// 输入）：过短或空值不能在固定 [:12] 切片上 panic。
func shortFingerprint(fp string) string {
	const maxLen = 12
	if len(fp) > maxLen {
		return fp[:maxLen]
	}
	return fp
}

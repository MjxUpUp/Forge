package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/health"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(healthCmd)
	healthCmd.Flags().Bool("json", false, "JSON 格式输出")
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "项目级质量趋势——聚合所有任务结论（task→project 粒度联动）",
	Long: `forge health 把 ~/.forge/projects/<项目key>/act/conclusions.jsonl 里所有任务结论上卷成项目级质量趋势：
分数走势、证据盲区率、复发低分维度。单个任务的盲区/低分是个例，跨任务聚合才暴露系统性
问题——某维度反复低分说明该方向有共性缺口，完成声明盲区率高说明 agent 系统性"声明完成
却没真验证"。这是 PDCA 在 project 粒度的 Act，喂给 session-retrospective 在项目层面决策
该把什么沉淀成 CLAUDE.md 铁律 / 守卫测试。`,
	RunE: runHealth,
}

func runHealth(cmd *cobra.Command, args []string) error {
	proj, err := findProject()
	if err != nil {
		// 未登记目录报 ErrNoForgeConfig（"not a forge project; run forge init
		// first"）——可行动的提示，不裸报底层错误（dogfood 5.2：裸报
		// "cwd is not in a git repository" 曾让用户困惑）。
		return err
	}
	cs, err := act.LoadAll(proj)
	if err != nil {
		return err
	}
	summary := health.Summarize(cs)
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		out, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	printHealth(summary)
	return nil
}

// printHealth 渲染项目级趋势。盲区率是头条——它是 project 级的 LLM-judge 盲区信号。
func printHealth(s health.Summary) {
	if s.TotalTasks == 0 {
		fmt.Println("尚无完成任务结论（完成若干任务后 forge task complete 会产出，届时这里出趋势）。")
		return
	}
	fmt.Printf("项目质量趋势 — %d 个完成任务", s.TotalTasks)
	if !s.Span.Earliest.IsZero() {
		fmt.Printf("（%s ~ %s）", s.Span.Earliest.Format("2006-01-02"), s.Span.Latest.Format("2006-01-02"))
	}
	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))

	fmt.Printf("分数:   均分 %.0f / 中位 %.0f", s.AvgScore, s.MedianScore)
	switch s.Trend {
	case `improving`:
		fmt.Print("  ↑ 改善")
	case `regressing`:
		fmt.Print("  ↓ 回退")
	case `stable`:
		fmt.Print("  → 稳定")
	default:
		fmt.Print("  (样本不足判趋势)")
	}
	fmt.Println()
	if len(s.GradeDist) > 0 {
		fmt.Printf("  分布: %s\n", distBar(s.GradeDist, []string{`A`, `B`, `C`, `D`, `F`}))
	}

	// 头条：盲区率（项目级 LLM-judge 盲区信号）
	fmt.Printf("\n证据盲区率: %.0f%%（%d/%d 任务完成声明缺 deterministic 证据——Unverified/无证据 Weak）\n",
		s.BlindSpotRate*100, s.BlindSpotCount, s.TotalTasks)
	if s.CappedWeakCount > 0 {
		fmt.Printf("  另有 %d 个任务为逃生舱封顶 Weak（有 deterministic 证据但被 override 降级——是逃生代价，不是盲区）\n", s.CappedWeakCount)
	}
	if s.BlindSpotRate >= 0.5 {
		fmt.Println("  ⚠ 系统性盲区：过半完成声明缺 deterministic 证据——project 级该查'验证为何没真跑'")
	}
	if len(s.StrengthDist) > 0 {
		fmt.Printf("  强度: %s\n", distBar(s.StrengthDist, []string{`Strong`, `Weak`, `Unverified`, `NoData`}))
	}

	if len(s.LowDims) > 0 {
		fmt.Println("\n复发低分维度（<70，跨任务频次）:")
		for _, d := range s.LowDims {
			pattern := "集中低档＝纪律缺口：优先沉淀对应守卫/铁律"
			if d.SpreadAcrossBuckets() {
				// 时高时低 = 阈值/粒度信号：先核分档定义与任务拆分粒度，别急着定纪律
				//（2026-09-22 实证：scope×57 里 46 个仍 A 级、31 任务 >500 行——
				// 本仓任务粒度的常态，见 docs/plans/low-dim-recurrence-2026-09.md）。
				pattern = "跨全档＝量纲/任务粒度信号：先核分档阈值与任务拆分粒度，别急着定纪律"
			}
			fmt.Printf("  %-16s ×%d%s——%s\n", d.Dimension, d.Count, scoreHisto(d.Scores), pattern)
		}
		fmt.Println("  → 集中低档才是纪律缺口；跨全档先校准量纲（假规则比没规则贵）。")
	}

	if s.NudgeCount > 0 {
		fmt.Printf("\n回顾触发: %d/%d 任务被标 RetrospectiveNudge（证据弱或低分）。\n", s.NudgeCount, s.TotalTasks)
	}
}

// distBar 按给定顺序把 map 渲染成 "k=v k=v" 串，保证可读顺序与可复现。
func distBar(dist map[string]int, order []string) string {
	var parts []string
	for _, k := range order {
		if n, ok := dist[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", k, n))
		}
	}
	return strings.Join(parts, " ")
}

// scoreHisto 把低分维度的分数直方图渲染成 "（40×31 60×26 …）"，按分数升序——
// 低档在左一眼可见集中度。空直方图（存量结论无 DimScores）渲染空串，退回纯 ×N。
func scoreHisto(scores map[int]int) string {
	if len(scores) == 0 {
		return ""
	}
	keys := make([]int, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d×%d", k, scores[k]))
	}
	return "（" + strings.Join(parts, " ") + "）"
}

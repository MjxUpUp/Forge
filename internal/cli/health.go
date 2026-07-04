package cli

import (
	"encoding/json"
	"fmt"
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
	fmt.Printf("\n证据盲区率: %.0f%%（%d/%d 任务完成声明主要靠 agent 自述——Unverified/Weak）\n",
		s.BlindSpotRate*100, s.BlindSpotCount, s.TotalTasks)
	if s.BlindSpotRate >= 0.5 {
		fmt.Println("  ⚠ 系统性盲区：过半完成声明缺 deterministic 证据——project 级该查'验证为何没真跑'")
	}
	if len(s.StrengthDist) > 0 {
		fmt.Printf("  强度: %s\n", distBar(s.StrengthDist, []string{`Strong`, `Weak`, `Unverified`, `NoData`}))
	}

	if len(s.LowDims) > 0 {
		fmt.Println("\n复发低分维度（<70，跨任务频次）:")
		for _, d := range s.LowDims {
			fmt.Printf("  %-16s ×%d\n", d.Dimension, d.Count)
		}
		fmt.Println("  → 反复低分的维度是 project 级系统性缺口，优先沉淀对应守卫/铁律。")
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

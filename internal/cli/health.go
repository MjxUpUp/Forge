package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	Short: "项目级完成事实——聚合所有任务结论（task→project 粒度联动）",
	Long: `forge health 把 ~/.forge/projects/<项目key>/act/conclusions.jsonl 里所有任务结论上卷成项目级完成事实：
判据通过数、先失败证明（红先绿）、评审轮次、逃生舱次数、证据盲区率。单个任务的盲区是
个例，跨任务聚合才暴露系统性问题——完成声明盲区率高说明 agent 系统性"声明完成却没真
验证"。这是 PDCA 在 project 粒度的 Act，喂给 session-retrospective 在项目层面决策该把
什么沉淀成 CLAUDE.md 铁律 / 守卫测试。刻意不输出任何合成分（均分/档/趋势）：要量化就
数事实。`,
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
	summary := health.SummarizeAt(cs, time.Now())
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		out, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	printHealth(summary)
	return nil
}

// printHealth 渲染项目级完成事实。盲区率是头条——它是 project 级的 LLM-judge 盲区信号。
// 2.3 起不打印均分/中位/档分布/低分维度直方图：那些是合成分，掩盖「看起来好但零实跑
// 证据」的盲区；这里只数事实。
func printHealth(s health.Summary) {
	if s.TotalTasks == 0 {
		fmt.Println("尚无完成任务结论（完成若干任务后 forge task complete 会产出，届时这里出事实）。")
		return
	}
	fmt.Printf("项目完成事实 — %d 个完成任务", s.TotalTasks)
	if !s.Span.Earliest.IsZero() {
		fmt.Printf("（%s ~ %s）", s.Span.Earliest.Format("2006-01-02"), s.Span.Latest.Format("2006-01-02"))
	}
	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))

	fmt.Printf("判据:   通过 %d/%d", s.CriteriaPassed, s.Criteria)
	fmt.Printf("  先失败证明: %d/%d 已转绿", s.RedFirstClosed, s.RedFirstTrials)
	fmt.Printf("  评审: %d 轮  逃生: %d 次", s.ReviewRounds, s.Escapes)
	fmt.Println()

	// 头条：盲区率（项目级 LLM-judge 盲区信号）
	fmt.Printf("证据盲区率: %.0f%%（%d/%d 任务完成声明缺 deterministic 证据——Unverified/无证据 Weak）", s.BlindSpotRate*100, s.BlindSpotCount, s.TotalTasks)
	fmt.Println()
	if s.CappedWeakCount > 0 {
		fmt.Printf("  另有 %d 个任务为逃生舱封顶 Weak（有 deterministic 证据但被 override 降级——是逃生代价，不是盲区）", s.CappedWeakCount)
		fmt.Println()
	}
	if s.BlindSpotRate >= 0.5 {
		fmt.Println("  ⚠ 系统性盲区：过半完成声明缺 deterministic 证据——project 级该查「验证为何没真跑」")
	}
	if len(s.StrengthDist) > 0 {
		fmt.Printf("  强度: %s", distBar(s.StrengthDist, []string{"Strong", "Weak", "Unverified", "NoData"}))
		fmt.Println()
	}

	if s.NudgeCount > 0 {
		fmt.Printf("回顾触发: %d/%d 任务被标 RetrospectiveNudge（证据弱）", s.NudgeCount, s.TotalTasks)
		fmt.Println()
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

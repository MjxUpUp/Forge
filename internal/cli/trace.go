package cli

import (
	"fmt"
	"slices"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(traceCmd)
}

// traceCmd implements `forge trace <task-ref>`: replays the full quality-event timeline of a task
// (tool calls + check results), reducing a single score back into a traceable story. An observability
// consumption layer on top of checklog/toolusage.
//
// traceCmd 实现 `forge trace <task-ref>`：重放任务的完整质量事件时间线
// （工具调用 + 检查结果），把单个评分还原成可回溯的故事。checklog/toolusage
// 之上的可观测性消费层。
var traceCmd = &cobra.Command{
	Use:   "trace <task-ref>",
	Short: "查看任务的完整质量事件时间线",
	Long: `forge trace 重放一个任务从开始到完成的所有质量事件：
工具调用、检查结果、门禁推进。把"一个评分"还原成"一条可回溯的时间线"。

数据源：DataDir/checklog*.jsonl（检查事件，含已归档）+ DataDir/toollog.jsonl（工具调用）。
	DataDir：git 项目 ~/.forge/projects/<key>/，非 git 项目 <root>/.forge/。`,
	Args: cobra.ExactArgs(1),
	RunE: runTrace,
}

// traceEvent is the unified timeline event merging the two sources checklog and toolusage,
// normalized onto a single sortable time axis.
//
// traceEvent 是合并 checklog 与 toolusage 两源的统一时间线事件，
// 归一化到单一可排序的时间轴。
type traceEvent struct {
	ts      time.Time
	source  string // "check" or "tool"
	summary string
	detail  string
}

func runTrace(cmd *cobra.Command, args []string) error {
	ref := args[0]

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	// ForTask reads disk once and aggregates the evidence chain: trace needs both per-event replay (via Entries) and
	// evidence-bucket summary (via Deterministic/AgentClaim). ForTask is the shared entry of both,
	// avoiding separate LoadForTask + BuildEvidenceChain calls that would re-read disk.
	//
	// ForTask 一次读盘 + 聚合证据链：trace 既要逐事件回放（用 Entries）又要
	// 证据分桶汇总（用 Deterministic/AgentClaim），ForTask 是两者的共同入口，
	// 避免分别调 LoadForTask + BuildEvidenceChain 重复读盘。
	ec, err := checklog.ForTask(root, ref)
	if err != nil {
		return fmt.Errorf("failed to load checklog: %w", err)
	}
	checks := ec.Entries
	calls, err := toolusage.LoadForTaskAll(root, ref)
	if err != nil {
		return fmt.Errorf("failed to load toollog: %w", err)
	}

	var events []traceEvent
	for i := range checks {
		c := checks[i]
		mark := "✗"
		if c.Passed {
			mark = "✓"
		}
		events = append(events, traceEvent{
			ts:      c.RecordedAt,
			source:  "check",
			summary: fmt.Sprintf("[%s] %s — %s", mark, c.Check, c.ToolName),
			detail:  c.Detail,
		})
	}
	for i := range calls {
		c := calls[i]
		events = append(events, traceEvent{
			ts:      c.Timestamp,
			source:  "tool",
			summary: fmt.Sprintf("→ %s [#%s]", c.ToolName, c.ID),
			detail:  truncate(c.ToolInput, 80),
		})
	}

	if len(events) == 0 {
		fmt.Printf("No events found for task %q (checklog/toollog 为空或无此 ref)。\n", ref)
		return nil
	}

	slices.SortFunc(events, func(a, b traceEvent) int {
		return a.ts.Compare(b.ts)
	})

	fmt.Printf("Trace for task %q — %d events (%d checks, %d tool calls)\n",
		ref, len(events), len(checks), len(calls))
	fmt.Println()
	for _, e := range events {
		fmt.Printf("  %s  %-6s  %s\n", e.ts.Format("15:04:05"), e.source, e.summary)
		if e.detail != "" {
			fmt.Printf("           %s\n", e.detail)
		}
	}

	// Evidence-chain bucketing: summarize this task's checks into deterministic (hook/gate actually run, unforgeable) vs
	// agent-claim (agent self-report). review/scoring uses this to counter the blind spot where an LLM-judge cannot see that an
	// agent skipped prerequisites then declared completion — the deterministic ratio is the hard signal of completion-claim credibility.
	//
	// 证据链分桶：把本任务检查按 deterministic（hook/gate 实跑，不可伪造）vs
	// agent-claim（agent 自述）汇总。review/评分据此对冲 LLM-judge 看不出「agent
	// 跳过前置就声明完成」的盲区——deterministic 占比是「完成声明可信度」的硬信号。
	if len(checks) > 0 {
		fmt.Printf("\n  证据链: %d 条 — deterministic=%d（hook/gate 实跑） agent-claim=%d（agent 自述）\n",
			len(ec.Entries), ec.Deterministic, ec.AgentClaim)
		switch ec.Strength() {
		case checklog.Unverified:
			fmt.Println(`  ⚠ 全部为 agent-claim：本任务「完成」声明无 deterministic 证据支撑，review 必须核验声称的验证是否真发生过`)
		case checklog.Weak:
			fmt.Println(`  ⚠ deterministic 占比低：review 重点核验声称的验证是否真跑过，对冲 agent 跳过前置就声明完成的盲区`)
		}
	}

	// Token-cost visibility: accumulates the estimated tokens of recorded tool calls (a proxy for loop cost).
	// Only covers hook-sampled input (auto-compile/tool-track), not the full LLM bill —
	// the magnitude signal is enough to tell whether the loop is burning tokens, complementing the gate breaker to prevent runaway.
	//
	// Token 成本可见性：累计被记录工具调用的估算 token（loop 成本代理）。
	// 仅含 hook 采样的 input（auto-compile/tool-track），非完整 LLM 账单——
	// 量级信号足够判断「loop 是否在烧 token」，配合 gate breaker 共同防跑飞。
	if total := toolusage.SumEstTokens(calls); total > 0 {
		fmt.Printf("\n  ≈ %d 估算 token（loop 成本代理，基于被记录的工具调用 input；不含 LLM 输出/thinking）\n", total)
	}
	return nil
}

// truncate truncates s to max length (rune-safe), appending ... when over. Originally defined in knowledge.go,
// moved here after the experience/knowledge loop was removed.
//
// truncate 截断 s 到 max 长度（rune 安全），超长加"..."。原 knowledge.go 定义，
// experience/knowledge 经验闭环移除后迁此。
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(traceCmd)
}

// traceCmd implements `forge trace <task-ref>`: replays a task's full quality
// event timeline (tool calls + check results), turning a single score back into
// a traceable story. The observability consumption layer over checklog/toolusage.
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

// traceEvent is a unified timeline event merged from the checklog and
// toolusage sources, normalized to a single sortable time axis.
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

	sort.Slice(events, func(i, j int) bool {
		return events[i].ts.Before(events[j].ts)
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

	// 证据链分桶：把本任务检查按 deterministic（hook/gate 实跑，不可伪造）vs
	// agent-claim（agent 自述）汇总。review/评分据此对冲 LLM-judge 看不出"agent
	// 跳过前置就声明完成"的盲区——deterministic 占比是"完成声明可信度"的硬信号。
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

	// Token 成本可见性：累计被记录工具调用的估算 token（loop 成本代理）。
	// 仅含 hook 采样的 input（auto-compile/tool-track），非完整 LLM 账单——
	// 量级信号足够判断"loop 是否在烧 token"，配合 gate breaker 共同防跑飞。
	if total := toolusage.SumEstTokens(calls); total > 0 {
		fmt.Printf("\n  ≈ %d 估算 token（loop 成本代理，基于被记录的工具调用 input；不含 LLM 输出/thinking）\n", total)
	}
	return nil
}

// truncate 截断 s 到 max 长度（rune 安全），超长加 "..."。原 knowledge.go 定义，
// experience/knowledge 经验闭环移除后迁此（trace.go 是唯一残留调用方）。
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

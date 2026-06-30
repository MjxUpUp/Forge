package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(actCmd)
	actCmd.AddCommand(actShowCmd)
	actCmd.AddCommand(actListCmd)
	actShowCmd.Flags().String("ref", "", "指定任务引用（默认最新结论）")
	actListCmd.Flags().Bool("json", false, "JSON 格式输出")
}

var actCmd = &cobra.Command{
	Use:   "act",
	Short: "Act 反馈臂——证据驱动的任务结论（喂给 session-retrospective）",
	Long: `forge act 读取每个完成任务的证据驱动结论（评分 + 证据强度 + 验收通过率 + 低分维度），
落盘在 .forge/act/conclusions.jsonl。是 PDCA Act 反馈臂的产出：会话回顾不再靠 agent 临结束
回忆，而是读结构化、deterministic 的结论。Weak/Unverified 证据（完成声明主要靠 agent 自述）
或低分任务的结论会标 RetrospectiveNudge——session-retrospective 据此优先回顾，对冲"高分但
没真验证"的 LLM-judge 盲区（分数看不出 agent 是否真跑过验证）。`,
}

var actShowCmd = &cobra.Command{
	Use:   "show [--ref <ref>]",
	Short: "查看最新（或指定）任务结论",
	RunE:  runActShow,
}

var actListCmd = &cobra.Command{
	Use:   "list [--json]",
	Short: "列出所有任务结论",
	RunE:  runActList,
}

func runActShow(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")
	if explicitRef != "" {
		cs, err := act.LoadAll(root)
		if err != nil {
			return err
		}
		var found *act.Conclusion
		for i := range cs {
			if cs[i].TaskRef == explicitRef {
				found = &cs[i] // 多次完成取最新（最后一个匹配）
			}
		}
		if found == nil {
			return fmt.Errorf("no act conclusion for task %q", explicitRef)
		}
		printConclusion(found)
		return nil
	}
	c, err := act.Latest(root)
	if err != nil {
		return err
	}
	if c == nil {
		fmt.Println("尚无任务结论（完成一个任务后 forge task complete 会产出）。")
		return nil
	}
	printConclusion(c)
	return nil
}

func runActList(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	cs, err := act.LoadAll(root)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		fmt.Println("尚无任务结论。")
		return nil
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		out, _ := json.MarshalIndent(cs, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Println("任务结论（按时间序）：")
	fmt.Println(strings.Repeat("─", 60))
	for _, c := range cs {
		nudge := ""
		if c.RetrospectiveNudge {
			nudge = " ⚠回顾"
		}
		fmt.Printf("  %-22s %3.0f %-8s evidence=%-10s ratio=%.2f%s\n",
			c.TaskRef, c.Score, c.Grade, c.Strength, c.Ratio, nudge)
	}
	return nil
}

// printConclusion 渲染单条结论。Directive 非空时附行动指令——forge act show 是
// session-retrospective 的入口，直接给可执行指令。
func printConclusion(c *act.Conclusion) {
	fmt.Printf("Task:        %s\n", c.TaskRef)
	if c.SessionID != "" {
		fmt.Printf("Session:     %s\n", c.SessionID)
	}
	fmt.Printf("Score:       %.0f (%s)\n", c.Score, c.Grade)
	fmt.Printf("Evidence:    %s (ratio=%.2f, deterministic=%d agent-claim=%d)\n",
		c.Strength, c.Ratio, c.Deterministic, c.AgentClaim)
	if c.AcceptanceTotal > 0 {
		fmt.Printf("Acceptance:  %d/%d 通过\n", c.AcceptancePass, c.AcceptanceTotal)
	}
	if len(c.LowDimensions) > 0 {
		fmt.Printf("Low dims:    %s\n", strings.Join(c.LowDimensions, ", "))
	}
	fmt.Printf("Completed:   %s\n", c.CompletedAt.Format("2006-01-02 15:04"))
	if d := c.Directive(); d != "" {
		fmt.Println(d)
	}
}

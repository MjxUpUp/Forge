package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/spf13/cobra"
)

func init() {
	actCmd.AddCommand(actRemigrateCmd)
	actRemigrateCmd.Flags().Bool("dry-run", false, "只报告将要迁移的条数，不写盘")
}

var actRemigrateCmd = &cobra.Command{
	Use:   "remigrate",
	Short: "就地迁移历史结论到当前逃生舱 cap 规则（2026-08 证据缩放校准）",
	Long: `forge act remigrate 把 conclusions.jsonl 里旧规则（平价 escape-cap）写入的结论
就地迁移到当前规则（证据缩放：ratio>=0.85 且 deterministic>=20 的重证据任务不再被
cap 到 Weak）。

指纹识别：Strength==Weak 且 ratio>=0.5 是旧 escape-cap 的唯一签名（其余 Weak 全是
ratio<0.5 的真弱证据，Strong/Unverified 不经过 cap）。命中指纹的条目用
act.RemigrateConclusion 重推导 Strength/RetrospectiveNudge，其余字段逐字保留。

与 act rebuild 的区别：rebuild 从 tasks/*.json 全量重建，retention（默认 30 天）已删
源的任务会永久丢失；remigrate 就地改写存量 JSONL，一条不丢。

写盘前自动备份到 conclusions.jsonl.migrate-bak（每次运行覆盖）。--dry-run 只统计
不写盘。幂等：重复运行第二次为 no-op。`,
	RunE: runActRemigrate,
}

func runActRemigrate(cmd *cobra.Command, args []string) error {
	proj, err := findProject()
	if err != nil {
		return err
	}
	cs, err := act.LoadAll(proj)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		fmt.Println("尚无任务结论，无需迁移。")
		return nil
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	changed := 0
	var out []act.Conclusion
	for _, c := range cs {
		m := act.RemigrateConclusion(c)
		if m.Strength != c.Strength || m.RetrospectiveNudge != c.RetrospectiveNudge {
			changed++
			fmt.Printf("  迁移 %-40s %s→%s nudge %v→%v\n", c.TaskRef, c.Strength, m.Strength, c.RetrospectiveNudge, m.RetrospectiveNudge)
		}
		out = append(out, m)
	}
	if changed == 0 {
		fmt.Printf("扫描 %d 条结论：全部符合当前规则，无需迁移。\n", len(cs))
		return nil
	}
	if dryRun {
		fmt.Printf("[dry-run] 将迁移 %d/%d 条结论（未写盘）。\n", changed, len(cs))
		return nil
	}

	// Backup then rewrite in place. LoadAll returns time-sorted order, and Append wrote
	// chronologically — rewrite preserves that order (a sorted rewrite is byte-equivalent
	// to the original apart from migrated fields).
	//
	// 先备份再就地重写。LoadAll 返回时间序，Append 本就按时间追加——重写保持该序
	//（排序后的重写除迁移字段外与原文件逐字节等价）。
	path := proj.ActConclusionsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", path, err)
	}
	if err := os.WriteFile(path+".migrate-bak", data, 0644); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}
	if err := act.RewriteAll(proj, out); err != nil {
		return fmt.Errorf("重写失败（原文件已备份到 .migrate-bak）: %w", err)
	}
	fmt.Printf("已迁移 %d/%d 条结论 → %s（备份: %s.migrate-bak）\n", changed, len(cs), path, path)
	return nil
}

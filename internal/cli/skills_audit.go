package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillsqa"
	"github.com/spf13/cobra"
)

var (
	skAudSkill []string
	skAudJSON  bool
	skAudGate  bool
)

// exit code 契约（--gate 时）：0=无 HIGH/CRITICAL，4=存在 HIGH/CRITICAL。
var skillsAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "19 条安全规则审查（prompt 注入/数据外发/危险代码）",
	RunE:  runSkillsAudit,
}

func runSkillsAudit(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	names, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return err
	}
	if len(skAudSkill) > 0 {
		names = filterSkillNames(names, skAudSkill)
	}

	type res struct {
		Skill          string             `json:"skill"`
		Findings       []skillsqa.Finding `json:"findings,omitempty"`
		Score          int                `json:"score"`
		Severity       string             `json:"severity"`
		Recommendation string             `json:"recommendation"`
	}
	results := make([]res, 0, len(names))
	hasBlock := false
	for _, n := range names {
		fs, serr := skillsqa.ScanSkill(filepath.Join(canonical, n))
		if serr != nil {
			// ScanSkill 失败（skill 不存在/无权限/读取错误）必须转成 CRITICAL finding。
			// 否则 ScoreFindings(nil)=0/INFO/SAFE 会把坏掉的 skill 报为"干净"——--gate 的
			// HIGH/CRITICAL 检测就此失守（无法验证 = 安全风险）。与 Install 的 AuditSkill
			// 错误处理对称：审查失败本身就是 block。
			fs = []skillsqa.Finding{{
				RuleID: "SCAN-ERROR", Severity: "CRITICAL", Confidence: 1.0,
				Category: "scan_error", File: "SKILL.md",
				Message:     "审查失败: " + serr.Error(),
				Remediation: "检查 skill 目录可读性与完整性",
			}}
			if skAudGate {
				hasBlock = true
			}
		}
		score, sev, rec := skillsqa.ScoreFindings(fs)
		results = append(results, res{n, fs, score, sev, rec})
		if skAudGate && (sev == "HIGH" || sev == "CRITICAL") {
			hasBlock = true
		}
	}

	if skAudJSON {
		out := struct {
			Canonical string `json:"canonical"`
			Total     int    `json:"total"`
			Blocked   bool   `json:"blocked"`
			Results   []res  `json:"results"`
		}{canonical, len(names), hasBlock, results}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("canonical: %s  (%d skill)\n", canonical, len(names))
		totalFindings := 0
		for _, r := range results {
			mark := "✓"
			if len(r.Findings) > 0 {
				mark = "✗"
			}
			fmt.Printf("  %s %-28s score=%-3d %s (%s, %d finding)\n",
				mark, r.Skill, r.Score, r.Severity, r.Recommendation, len(r.Findings))
			totalFindings += len(r.Findings)
			for _, f := range r.Findings {
				fmt.Printf("      [%s] %s: %s (%s:%d)\n", f.Severity, f.RuleID, f.Message, f.File, f.StartLine)
			}
		}
		fmt.Printf("共 %d 条 finding\n", totalFindings)
	}

	if skAudGate && hasBlock {
		os.Exit(4)
	}
	return nil
}

func init() {
	skillsAuditCmd.Flags().StringSliceVar(&skAudSkill, "skill", nil, "只审查指定 skill（可重复）")
	skillsAuditCmd.Flags().BoolVar(&skAudJSON, "json", false, "JSON 输出")
	skillsAuditCmd.Flags().BoolVar(&skAudGate, "gate", false, "门禁模式：HIGH/CRITICAL 时 exit 4")
	skillsCmd.AddCommand(skillsAuditCmd)
}

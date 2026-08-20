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

// exit code contract (with --gate): 0=clean, 4=HIGH/CRITICAL severity band OR any single
// CRITICAL finding (regardless of aggregate score — #4 fix).
//
// exit code 契约（--gate 时）：0=干净，4=HIGH/CRITICAL 严重度带或任一单条 CRITICAL finding
// （无视聚合分——#4 修复）。
var skillsAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "21 条安全规则审查（prompt 注入/数据外发/危险代码/供应链执行向量）",
	RunE:  runSkillsAudit,
}

// auditGateBlocked decides the --gate block: severity band HIGH/CRITICAL (aggregate) OR any single
// CRITICAL finding. Extracted from the loop so the decision is unit-testable without the
// os.Exit(4) process exit, and so the any-CRITICAL half (#4 score-math fix) reads as one named
// predicate next to skillsqa.HasCritical instead of an inline condition.
//
// auditGateBlocked 判定 --gate 阻断：严重度带 HIGH/CRITICAL（聚合）或任一单条 CRITICAL finding。
// 从循环抽出以便绕开 os.Exit(4) 进程退出做单测，也让 #4 分数数学修复的 any-CRITICAL 半边
// 成为与 skillsqa.HasCritical 并列的具名谓词，而非内联条件。
func auditGateBlocked(sev string, fs []skillsqa.Finding) bool {
	return sev == "HIGH" || sev == "CRITICAL" || skillsqa.HasCritical(fs)
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
		names, err = filterSkillNames(names, skAudSkill)
		if err != nil {
			return err
		}
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
			// A ScanSkill failure (skill missing/no permission/read error) must be turned into a CRITICAL finding.
			// Otherwise ScoreFindings(nil)=0/INFO/SAFE would report a broken skill as clean — the --gate
			// HIGH/CRITICAL detection would then fail (cannot verify = security risk). Symmetric with Install AuditSkill
			// error handling: an audit failure is itself a block.
			//
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
		// #4 fix: band-only gating let a single CRITICAL finding (aggregate ≤23.75 → MEDIUM band)
		// pass as exit 0 — the same score-math hole as the install gate. auditGateBlocked adds
		// any-CRITICAL on top of the band judgment.
		//
		// #4 修复：只看带的门禁会让单条 CRITICAL（聚合 ≤23.75 → MEDIUM 带）以 exit 0 放过——
		// 与 install 门禁同一分数数学洞。auditGateBlocked 在带判定之上加 any-CRITICAL。
		if skAudGate && auditGateBlocked(sev, fs) {
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
	skillsAuditCmd.Flags().BoolVar(&skAudGate, "gate", false, "门禁模式：HIGH/CRITICAL 带或任一 CRITICAL finding 时 exit 4")
	skillsCmd.AddCommand(skillsAuditCmd)
}

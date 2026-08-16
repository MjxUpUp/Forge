package cli

// skills_verify.go — verify subcommand: backfills a verification onto a decision's prediction.
// Prediction→verification closure (AHE decision observability, pillar 3): the prediction was
// declared at edit time (--prediction of forge skills decide); this command records what the
// next round's real outcomes showed (hit / miss / inconclusive). A refuted prediction is
// visibly refuted — the next-round agent sees which directions were falsified.
//
// skills_verify.go — verify 子命令：把验证回填到决策的 prediction 上。
// prediction→验证闭环（AHE 决策可观测，支柱 3）：预测在修改时刻声明（forge skills
// decide 的 --prediction）；本命令记录下一轮真实结果如何（命中/未命中/不可判）。
// 被证伪的预测可见地被证伪——下一轮 agent 看到哪些方向被证伪。

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/spf13/cobra"
)

var (
	skVerSkill       string
	skVerDecision    string
	skVerResult      string
	skVerAt          string
	skVerHistory     bool
	skVerHistoryJSON bool
)

var skillsVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "回填决策预测的验证结果（prediction→verification 闭环）",
	Long: `把验证结果回填到 <skill>/decisions.md 的一条既有决策上（原地 section 补丁，
其余字节逐字保留）：
  --skill     哪个 skill
  --decision  决策 ID（decisions.md 里的 ## [d-...]，或用 --history 查看全部）
  --result    验证结果：命中/未命中/不可判 + 数字（如「命中：触发率 15%→32%，
              超过预测的 30%」）

闭环用法（AHE 决策可观测）：
  1. 修改时：forge skills decide ... --prediction "触发率应 15%→30%"
  2. 下一轮：观测真实结果后 forge skills verify --skill X --decision d-... \
              --result "未命中：触发率 18%，预测失败"

每条决策只验证一次；二次意见记一条引用它的新决策。`,
	RunE: runSkillsVerify,
}

func runSkillsVerify(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}

	// History mode: list decisions with their prediction/verification state (which
	// predictions are still open — the falsifiability ledger). No --decision needed.
	//
	// 历史模式：列出各决策的预测/验证状态（哪些预测还悬着——可证伪性台账）。无需 --decision。
	if skVerHistory || skVerHistoryJSON {
		return runSkillsVerifyHistory(canonical)
	}

	if skVerSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	if err := requireValidSkillName(skVerSkill); err != nil {
		return err
	}
	if skVerDecision == "" {
		return fmt.Errorf("需要 --decision ID（用 --history 查看可用决策 ID）")
	}
	verifiedAt := time.Time{}
	if skVerAt != "" {
		t, terr := time.Parse(time.RFC3339, skVerAt)
		if terr != nil {
			return fmt.Errorf("--at 须为 RFC3339（如 2026-08-16T10:00:00Z）: %v", terr)
		}
		verifiedAt = t
	}
	if err := skillsdecisions.VerifyDecision(canonical, skVerSkill, skVerDecision, skVerResult, verifiedAt); err != nil {
		return err
	}
	fmt.Printf("✅ 验证已回填到 %s 的决策 %s\n", skVerSkill, skVerDecision)
	return nil
}

// verifyHistoryRow is one row of the falsifiability ledger: a decision with a prediction,
// its verification state, and the prediction/verification text (truncated for text mode).
//
// verifyHistoryRow 是可证伪性台账的一行：带预测的决策、其验证状态、预测/验证文本
// （文本模式截断展示）。
type verifyHistoryRow struct {
	ID           string `json:"id"`
	Outcome      string `json:"outcome"`
	DecidedAt    string `json:"decided_at"`
	Prediction   string `json:"prediction"`
	Verification string `json:"verification"`
	Verified     bool   `json:"verified"`
}

func runSkillsVerifyHistory(canonical string) error {
	decisions, err := collectAllDecisions(canonical)
	if err != nil {
		return err
	}
	rows := make([]verifyHistoryRow, 0, len(decisions))
	for _, d := range decisions {
		rows = append(rows, verifyHistoryRow{
			ID:           d.ID,
			Outcome:      d.Outcome,
			DecidedAt:    d.DecidedAt.UTC().Format(time.RFC3339),
			Prediction:   d.Prediction,
			Verification: d.Verification,
			Verified:     !d.VerifiedAt.IsZero(),
		})
	}
	if skVerHistoryJSON {
		out, merr := json.MarshalIndent(rows, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Println(string(out))
		return nil
	}
	if len(rows) == 0 {
		fmt.Println("（无决策记录——forge skills decide 记录第一条）")
		return nil
	}
	for _, r := range rows {
		status := "🔍 未验证"
		if r.Verified {
			status = "✅ 已验证"
		}
		fmt.Printf("%s  %s  [%s]  %s\n", status, r.ID, r.Outcome, r.DecidedAt)
		if r.Prediction != "" {
			fmt.Printf("    预测: %s\n", firstLine(r.Prediction, 100))
			if r.Verification != "" {
				fmt.Printf("    验证: %s\n", firstLine(r.Verification, 100))
			}
		} else if !r.Verified {
			fmt.Printf("    （无预测——修改时刻未声明可检验信号）\n")
		}
	}
	return nil
}

// firstLine returns the first line of s, truncated to max runes (for compact ledger display).
//
// firstLine 返回 s 的首行并按 max rune 截断（紧凑台账展示）。
func firstLine(s string, max int) string {
	for i, r := range s {
		if r == '\n' {
			s = s[:i]
			break
		}
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// collectAllDecisions walks canonical skills' decisions.md files. Read errors on individual
// skills are skipped (fail-open for listing — a corrupt file should not hide the rest of the
// ledger), a canonical-level error propagates.
//
// collectAllDecisions 遍历 canonical 下各 skill 的 decisions.md。单 skill 读取错误跳过
// （列表 fail-open——一个坏文件不该藏起其余台账），canonical 级错误上抛。
func collectAllDecisions(canonical string) ([]skillsdecisions.SkillDecision, error) {
	names, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return nil, err
	}
	var out []skillsdecisions.SkillDecision
	for _, name := range names {
		ds, derr := skillsdecisions.LoadDecisions(canonical, name)
		if derr != nil {
			continue // 单 skill 解析失败跳过（fail-open for listing）
		}
		out = append(out, ds...)
	}
	return out, nil
}

func init() {
	skillsVerifyCmd.Flags().StringVar(&skVerSkill, "skill", "", "哪个 skill")
	skillsVerifyCmd.Flags().StringVar(&skVerDecision, "decision", "", "决策 ID（## [d-...]）")
	skillsVerifyCmd.Flags().StringVar(&skVerResult, "result", "", "验证结果：命中/未命中/不可判 + 数字")
	skillsVerifyCmd.Flags().StringVar(&skVerAt, "at", "", "验证时刻 RFC3339（默认 now；测试用）")
	skillsVerifyCmd.Flags().BoolVar(&skVerHistory, "history", false, "列出全部决策的预测/验证状态（可证伪性台账）")
	skillsVerifyCmd.Flags().BoolVar(&skVerHistoryJSON, "history-json", false, "--history 的 JSON 输出")
	skillsCmd.AddCommand(skillsVerifyCmd)
}

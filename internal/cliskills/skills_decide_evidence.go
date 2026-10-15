package cliskills

// skills_decide_evidence.go — `skills decide` 的证据来源核验：Evidence / Rationale 引用
// 外部数字（百分比、倍数、arXiv 编号）却没给来源 URL 时给 WARN，并落一条 checklog
// decision-evidence 行。advisory：决策照常写入，退出码不变。
//
// 动因：2026-10 research-workflow 复盘——「审查阶段占 59.4% token」进 agent-delegation
// 决策记录时丢了来源与「仅 ChatDev」限定，事后无从核对。warrant 只核锚点存在，不读证据
// 正文；本检查补上「外部数字要能回到原文」这一面。

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/projectroot"
)

// externalFigureRe matches figures that usually come from an outside source: percentages, multipliers and arXiv ids.
//
// externalFigureRe 匹配通常来自外部来源的数字：百分比、倍数（N×/Nx/N 倍）、arXiv 编号。
var externalFigureRe = regexp.MustCompile(`(?i)\d+(\.\d+)?\s?(%|％|×|倍)|\b\d+(\.\d+)?x\b|arxiv[:\s]*\d{4}\.\d{4,5}`)

// evidenceRunRefRe matches a reference to a local eval run id (run-20261015… / run:20261015…, ids start with a digit), which counts as a checkable source for internal metrics. Words like run-time or dry-run-mode do not match.
//
// evidenceRunRefRe 匹配本地 eval run id 引用（run-20261015… / run:20261015…，id 以数字开头）——
// 内部指标以 run 为来源，可核验。run-time、dry-run-mode 这类普通词不算。
var evidenceRunRefRe = regexp.MustCompile(`\brun[-:]\d`)

// evidenceURLRe matches a source URL.
//
// evidenceURLRe 匹配来源 URL。
var evidenceURLRe = regexp.MustCompile(`https?://`)

// maxEvidenceFigures caps how many matched figures are echoed into the message and checklog meta.
//
// maxEvidenceFigures 限制回显到提示与 checklog meta 的命中片段数。
const maxEvidenceFigures = 5

// evidenceCheck is the outcome of scanning a decision's evidence and rationale.
//
// evidenceCheck 是扫描一条决策 Evidence / Rationale 的结果。Figures 为空表示没有外部
// 数字（不落行）；HasURL 表示有可核验来源：两段文本里有 URL、引用了 run-<id>，或 warrant
// 是 run 锚点（内部 eval 指标的来源是 run，不是网页）。
type evidenceCheck struct {
	Figures []string
	HasURL  bool
}

// checkDecisionEvidence scans evidence and rationale for external figures and source URLs.
//
// checkDecisionEvidence 扫描 evidence 与 rationale 中的外部数字与来源 URL（去重、保序、
// 最多 maxEvidenceFigures 个片段）。
func checkDecisionEvidence(evidence, rationale, warrant string) evidenceCheck {
	text := evidence + "\n" + rationale
	var c evidenceCheck
	seen := map[string]bool{}
	for _, m := range externalFigureRe.FindAllString(text, -1) {
		m = strings.TrimSpace(m)
		if seen[m] {
			continue
		}
		seen[m] = true
		if len(c.Figures) < maxEvidenceFigures {
			c.Figures = append(c.Figures, m)
		}
	}
	c.HasURL = evidenceURLRe.MatchString(text) || evidenceRunRefRe.MatchString(text) ||
		strings.HasPrefix(strings.TrimSpace(warrant), "run:")
	return c
}

// reportDecisionEvidence prints a WARN when a just-recorded decision cites external figures without a source URL and appends the decision-evidence checklog row. Decisions without external figures are silent and leave no row; outside a forge project nothing is recorded.
//
// reportDecisionEvidence 在刚记录的决策引用外部数字却无来源 URL 时打 WARN，并追加
// decision-evidence 行。没有外部数字的决策静默且不落行；不在 forge 项目内时不落行。
func reportDecisionEvidence(cmd *cobra.Command, skill, outcome, evidence, rationale, warrant string) {
	c := checkDecisionEvidence(evidence, rationale, warrant)
	if len(c.Figures) == 0 {
		return
	}
	w := io.Writer(os.Stderr)
	if cmd != nil {
		w = cmd.ErrOrStderr()
	}
	figures := strings.Join(c.Figures, "、")
	if !c.HasURL {
		fmt.Fprintf(w, "⚠️ [decision-evidence] 证据引用了外部数字（%s）但没有来源（URL、run-<id> 或 --warrant run:）——补上原文链接与适用限定（场景/样本/模型），否则事后无从核对\n", figures)
	}
	root, err := projectroot.Find()
	if err != nil || root == "" {
		return
	}
	level := checklog.LevelPass
	detail := "decision-evidence: 外部数字带来源 URL（" + figures + "）"
	if !c.HasURL {
		level = checklog.LevelWarn
		detail = "decision-evidence: 外部数字缺来源 URL（" + figures + "）"
	}
	_ = checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckDecisionEvidence,
		Passed:  c.HasURL,
		Checked: true,
		Level:   level,
		Source:  checklog.EvidenceDeterministic,
		Detail:  detail,
		Meta: map[string]string{
			"skill":   skill,
			"outcome": outcome,
			"figures": figures,
		},
	})
}

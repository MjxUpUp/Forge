package evalkit

// judgeaudit.go — 判分器受审（Track B · C2，docs/design/forge-evaluation-system.md
// §六 P2，ABC I.c.1 的本地化）：对 model-assisted 判分器（如 docgate rubric 75 分
// 阈值）做两件事——同输入重放方差（自洽性）与人工标注一致率（Cohen's κ）。
// κ<0.6 时该判分器的下游 BLOCKED 决策降级为 ADVISORY 并落 eval-judge-weak 审计行。
// 分数采集是外部环节（agent 驱动的 rubric 评审）；forge 只做数学与裁决——
// 这正是"评别人的先评自己"。
//
// judgeaudit.go — judge audit: for a model-assisted grader (e.g. the docgate
// rubric's 75-point threshold) measure replay variance (self-consistency) and
// agreement with human labels (Cohen's κ). κ<0.6 degrades the judge's
// downstream BLOCKED decisions to advisory and records eval-judge-weak. Score
// collection is external (agent-driven rubric reviews); forge does the math and
// the verdict — judge the judge before trusting its judgments.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// JudgeAuditKappaFloor is the reliability bar: below it the judge's decisions
// are treated as noise and must not support hard gates.
//
// JudgeAuditKappaFloor 是可靠性阈值：低于它，判分器的决策视为噪声，不得支撑
// 硬门禁。
const JudgeAuditKappaFloor = 0.6

// JudgeAuditEntry is one document's recorded scores: the judge's k replays and
// the human's label (binarized by the judge's own operating threshold).
//
// JudgeAuditEntry 是一份文档的记录分数：judge 的 k 次重放与人工标注（按判分器
// 自己的工作阈值二值化）。
type JudgeAuditEntry struct {
	DocID       string `yaml:"doc_id"       json:"doc_id"`
	JudgeScores []int  `yaml:"judge_scores" json:"judge_scores"`
	HumanScore  int    `yaml:"human_score"  json:"human_score"`
	Threshold   int    `yaml:"threshold"    json:"threshold"`
}

// JudgeAuditReport is the audit's honest output.
//
// JudgeAuditReport 是审计的诚实输出。
type JudgeAuditReport struct {
	GeneratedAt   time.Time        `json:"generated_at"`
	Entries       []JudgeEntryStat `json:"entries"`
	Kappa         float64          `json:"kappa"`
	KappaValid    bool             `json:"kappa_valid"`
	JudgeReliable bool             `json:"judge_reliable"`
	Findings      []string         `json:"findings,omitempty"`
}

// JudgeEntryStat is one document's replay variance summary.
//
// JudgeEntryStat 是一份文档的重放方差摘要。
type JudgeEntryStat struct {
	DocID        string  `json:"doc_id"`
	Mean         float64 `json:"mean"`
	Std          float64 `json:"std"`
	Range        int     `json:"range"`
	Binomial     string  `json:"binomial"` // 人类阈值下的 pass/fail 判定（judge 首次重放口径）
	MatchesHuman bool    `json:"matches_human"`
}

// LoadJudgeScores reads the scores JSON file (produced by the external rubric
// review passes).
//
// LoadJudgeScores 读取分数 JSON 文件（由外部 rubric 评审轮次产出）。
func LoadJudgeScores(path string) ([]JudgeAuditEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("evalkit: 读取 judge 分数失败: %w", err)
	}
	var entries []JudgeAuditEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("evalkit: 解析 judge 分数失败: %w", err)
	}
	for i := range entries {
		if entries[i].DocID == "" || len(entries[i].JudgeScores) == 0 || entries[i].Threshold <= 0 {
			return nil, fmt.Errorf("evalkit: judge 分数第 %d 条缺 doc_id/judge_scores/threshold", i+1)
		}
	}
	return entries, nil
}

// RunJudgeAudit computes replay variance per document and Cohen's κ against
// human labels.
//
// RunJudgeAudit 逐文档计算重放方差，并计算与人工标注的 Cohen's κ。
func RunJudgeAudit(entries []JudgeAuditEntry) (*JudgeAuditReport, error) {
	rep := &JudgeAuditReport{GeneratedAt: time.Now().UTC()}
	var judgeBins, humanBins []string
	for _, e := range entries {
		vals := make([]float64, len(e.JudgeScores))
		for i, s := range e.JudgeScores {
			vals[i] = float64(s)
		}
		mean, std := MeanAndStd(vals)
		lo, hi := vals[0], vals[0]
		for _, v := range vals {
			lo = math.Min(lo, v)
			hi = math.Max(hi, v)
		}
		bin := "fail"
		if e.JudgeScores[0] >= e.Threshold {
			bin = "pass"
		}
		humanBin := "fail"
		if e.HumanScore >= e.Threshold {
			humanBin = "pass"
		}
		rep.Entries = append(rep.Entries, JudgeEntryStat{
			DocID: e.DocID, Mean: mean, Std: std, Range: int(hi - lo),
			Binomial: bin, MatchesHuman: bin == humanBin,
		})
		judgeBins = append(judgeBins, bin)
		humanBins = append(humanBins, humanBin)
	}
	// κ 需要 ≥2 条且至少两个类别出现（全同类别时 κ 无定义——如实标注）。
	cats := map[string]bool{}
	for _, b := range judgeBins {
		cats[b] = true
	}
	for _, b := range humanBins {
		cats[b] = true
	}
	if len(entries) >= 2 && len(cats) >= 2 {
		k, err := CohenKappa(judgeBins, humanBins)
		if err != nil {
			return nil, err
		}
		rep.Kappa = k
		rep.KappaValid = true
		rep.JudgeReliable = k >= JudgeAuditKappaFloor
	} else {
		rep.KappaValid = false
		rep.Findings = append(rep.Findings, "κ 无定义（样本 <2 或全部同类别）——judge 可靠性未判定，下游维持现状并继续采集")
	}
	if rep.KappaValid && !rep.JudgeReliable {
		rep.Findings = append(rep.Findings, fmt.Sprintf("judge κ=%.2f 低于 %.2f 阈值——该判分器的 BLOCKED 决策降级为 ADVISORY，75 分阈值在当前 judge 下视为噪声", rep.Kappa, JudgeAuditKappaFloor))
	}
	// 重放方差 finding：仅当重放分数跨越工作阈值（pass/fail 判定翻转）才报——
	// 判定不稳定是决策风险；阈值同侧的 2-5 分抖动是 LLM 评审的正常噪声，报了
	// 就是狼来了（judge-audit 首轮实测 2026-09-04：κ=1.00 但全部文档被误标
	// "自洽性不足"，正是本规则修正的动机）。极差数值仍在 entries 里展示。
	for _, e := range entries {
		lo, hi := e.JudgeScores[0], e.JudgeScores[0]
		for _, s := range e.JudgeScores {
			if s < lo {
				lo = s
			}
			if s > hi {
				hi = s
			}
		}
		if lo < e.Threshold && hi >= e.Threshold {
			rep.Findings = append(rep.Findings, fmt.Sprintf("文档 %s 重放分数 [%d,%d] 跨越阈值 %d——pass/fail 判定不稳定，下游决策不可依赖", e.DocID, lo, hi, e.Threshold))
		}
	}
	return rep, nil
}

// PersistJudgeAudit writes the report; an unreliable judge lands the
// eval-judge-weak audit row (observation class).
//
// PersistJudgeAudit 写报告；不可靠判分器落 eval-judge-weak 审计行（观察类）。
func PersistJudgeAudit(evalDir string, repoRoot string, rep *JudgeAuditReport) (string, error) {
	dir := evalDataDir(evalDir)
	data, err := jsonMarshal(rep)
	if err != nil {
		return "", err
	}
	path := filepathJoin(dir, fmt.Sprintf("judge-audit-%s.json", rep.GeneratedAt.UTC().Format("20060102-150405")))
	if err := atomicWriteFile(path, data); err != nil {
		return "", err
	}
	if rep.KappaValid && !rep.JudgeReliable {
		_ = checklog.Record(repoRoot, &checklog.Entry{
			Check:   checklog.CheckEvalJudgeWeak,
			Passed:  false,
			Checked: true,
			Detail:  fmt.Sprintf(`judge κ=%.2f 低于 %.2f——BLOCKED 决策降级 ADVISORY`, rep.Kappa, JudgeAuditKappaFloor),
		})
	}
	return path, nil
}

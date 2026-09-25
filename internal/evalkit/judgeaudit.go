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
	// MVVP 可选轮次（focus-batches §2e，方向 E）：对齐 arXiv 2606.19544 提出的
	// Minimum Viable Validation Protocol——"reliability without validity" 的对策是
	// 四项协议：chance-corrected κ（已有）+ test-retest + position bias + cue 敏感度。
	// 三组轮次全部可选（旧 scores 文件零字段照跑）：
	//   RetestScores  —— 同输入第二轮（时间上分离的重放）：binarized 判定不一致率
	//   SwappedScores —— 位置交换呈现（A/B 顺序对调）：均值差 = position bias 幅度
	//   CueScores     —— 提示词微扰（措辞偏好注入）：binarized 翻转率 >10% → 冻结建议
	RetestScores  []int `yaml:"retest_scores,omitempty"  json:"retest_scores,omitempty"`
	SwappedScores []int `yaml:"swapped_scores,omitempty" json:"swapped_scores,omitempty"`
	CueScores     []int `yaml:"cue_scores,omitempty"     json:"cue_scores,omitempty"`
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
	// MVVP 三项（可选轮次存在时才计算；-1 = 该协议未运行——区分"没测"与"完美"）。
	RetestAgreement float64  `json:"retest_agreement"` // binarized 重放一致率
	PositionBias    float64  `json:"position_bias"`    // 均值差（swap − original）
	CueFlipRate     float64  `json:"cue_flip_rate"`    // binarized 翻转率
	Findings        []string `json:"findings,omitempty"`
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
	// MVVP 三项（可选轮次；arXiv 2606.19544：test-retest / position bias / cue 敏感度）。
	// 默认 -1 = 该协议未运行——诚实区分"没测"与"完美"（insufficient ≠ pass）。
	rep.RetestAgreement, rep.PositionBias, rep.CueFlipRate = -1, -1, -1
	var hasRetest, hasSwapped, hasCue bool
	retestAgree, retestTotal := 0, 0
	var origSum, swapSum float64
	swapCount := 0
	cueFlips, cueTotal := 0, 0
	for _, e := range entries {
		if len(e.RetestScores) > 0 && len(e.JudgeScores) > 0 {
			hasRetest = true
			a := e.JudgeScores[0] >= e.Threshold
			for _, r := range e.RetestScores {
				retestTotal++
				if (r >= e.Threshold) == a {
					retestAgree++
				}
			}
		}
		if len(e.SwappedScores) > 0 && len(e.JudgeScores) > 0 {
			hasSwapped = true
			origSum += float64(e.JudgeScores[0])
			m := float64(e.SwappedScores[0])
			for _, s := range e.SwappedScores[1:] {
				m += float64(s)
			}
			m /= float64(len(e.SwappedScores))
			swapSum += m
			swapCount++
		}
		if len(e.CueScores) > 0 && len(e.JudgeScores) > 0 {
			hasCue = true
			a := e.JudgeScores[0] >= e.Threshold
			cueTotal++
			if (e.CueScores[0] >= e.Threshold) != a {
				cueFlips++
			}
		}
	}
	if hasRetest && retestTotal > 0 {
		rep.RetestAgreement = float64(retestAgree) / float64(retestTotal)
		if rep.RetestAgreement < 0.9 {
			rep.Findings = append(rep.Findings, fmt.Sprintf("MVVP test-retest 一致率 %.2f（<0.90）——judge 对同一输入的判定随时间漂移，冻结 judge prompt 并查采样温度", rep.RetestAgreement))
		}
	}
	if hasCue && cueTotal > 0 {
		rep.CueFlipRate = float64(cueFlips) / float64(cueTotal)
		if rep.CueFlipRate > 0.10 {
			rep.Findings = append(rep.Findings, fmt.Sprintf("MVVP cue 敏感度：扰动翻转率 %.2f（>0.10）——judge 判定可被提示词措辞改变，冻结该 judge prompt 版本并回归校准", rep.CueFlipRate))
		}
	}
	// position bias：mean(swapped) − mean(original first scores)（呈现顺序的均值影响）。
	if hasSwapped && swapCount > 0 {
		rep.PositionBias = swapSum/float64(swapCount) - origSum/float64(swapCount)
		if math.Abs(rep.PositionBias) >= 5 {
			rep.Findings = append(rep.Findings, fmt.Sprintf("MVVP position bias：位置交换均值差 %.1f 分（|Δ|≥5）——判定受呈现顺序影响，A/B 对照呈现进评审流程", rep.PositionBias))
		}
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

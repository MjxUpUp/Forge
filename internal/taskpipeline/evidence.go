package taskpipeline

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// BuildTaskEvidence 汇总任务完成时可观测的事实。它不计算任何分数：
// 逃生舱与红先绿的代价由事实本身表达（次数、是否闭合），不由加权分表达。
func BuildTaskEvidence(root string, state *TaskState) *tasktypes.TaskEvidence {
	ev := &tasktypes.TaskEvidence{
		Criteria:     len(state.Acceptance),
		ReviewRounds: len(state.ReviewRounds),
	}
	for _, c := range state.Acceptance {
		if c.Passed {
			ev.CriteriaPassed++
		}
	}
	for _, r := range state.RedAttempts {
		ev.RedFirstTrials++
		want := NormalizeGoTestRun(r.Run)
		for _, c := range state.Acceptance {
			if c.Passed && NormalizeGoTestRun(c.Run) == want {
				ev.RedFirstClosed++
				break
			}
		}
	}
	if state.ReviewScore != nil {
		v := *state.ReviewScore
		ev.ReviewScore = &v
	}
	for _, v := range []string{
		state.Overrides.WorkActivity,
		state.Overrides.TestCoverage,
		state.Overrides.AcceptanceGate,
		state.Overrides.SkillDecisions,
		state.Overrides.DocGate,
		state.Overrides.ArtifactChain,
	} {
		if v != "" {
			ev.Escapes++
		}
	}
	if state.AllowGreen {
		ev.Escapes++
	}
	if state.AllowEmptyCriteria {
		ev.Escapes++
	}
	if recorded, err := taskEscapeHatchRecorded(root, state.TaskRef); err == nil && recorded {
		ev.Escapes++
	}
	// 未测面必须总是现算（不依赖历史 Score）：逃生激活时按无视逃生的配对口径披露。
	ev.UntestedAreas = CoveragePairingForDisclosure(root, state)
	var open []tasktypes.Finding
	for _, f := range state.Findings {
		if f.Status == "open" {
			open = append(open, f)
		}
	}
	for i, f := range open {
		if i >= 5 {
			ev.RemainingRisks = append(ev.RemainingRisks, fmt.Sprintf("（其余 %d 条 open 发现未逐条列出——用 forge task context 查全量）", len(open)-5))
			break
		}
		sev := f.Severity
		if sev == "" {
			sev = "unrated"
		}
		ev.RemainingRisks = append(ev.RemainingRisks, sev+": "+f.Content)
	}
	return ev
}

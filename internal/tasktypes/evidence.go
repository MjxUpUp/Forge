package tasktypes

// TaskEvidence 是任务完成时的事实清单：不合成、不加权、不给等级。
// 每个字段都能回到一次可审计的事件（判据实跑、红快照、评审轮次、逃生舱）。
type TaskEvidence struct {
	Criteria       int      `json:"criteria"`
	CriteriaPassed int      `json:"criteria_passed"`
	RedFirstTrials int      `json:"red_first_trials,omitempty"`
	RedFirstClosed int      `json:"red_first_closed,omitempty"`
	ReviewRounds   int      `json:"review_rounds,omitempty"`
	ReviewScore    *int     `json:"review_score,omitempty"`
	Escapes        int      `json:"escapes,omitempty"`
	UntestedAreas  []string `json:"untested_areas,omitempty"`
	RemainingRisks []string `json:"remaining_risks,omitempty"`
}

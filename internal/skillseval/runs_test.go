package skillseval

import (
	"testing"
)

func TestNormalizeTriggered(t *testing.T) {
	canonical := []string{"code-review-gate", "frontend-feature-development"}
	cases := []struct{ in, want string }{
		{"code-review-gate", "code-review-gate"},
		{"Code-Review-Gate", "code-review-gate"},     // 大小写归一
		{"  code-review-gate  ", "code-review-gate"}, // trim
		{"code-review-gate.", "code-review-gate"},    // 英文句号 strip
		{"「code-review-gate」", "code-review-gate"},   // 中文引号 strip
		{"lark-doc。", "lark-doc"},                    // 中文句号 strip（不匹配 canonical 保留 lowercased）
		{"（none）", ""},                               // 全角括号 + none
		{"none", ""},
		{"无", ""},
		{"-", ""},
		{"unknown-skill", "unknown-skill"}, // 不匹配 canonical 保留 lowercased
		{"", ""},
	}
	for _, c := range cases {
		got := NormalizeTriggered(c.in, canonical)
		if got != c.want {
			t.Fatalf("NormalizeTriggered(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestJudgeResult(t *testing.T) {
	skill := "my-skill"
	trigCase := EvalCase{Skill: skill, Kind: KindTrigger}
	notCase := EvalCase{Skill: skill, Kind: KindNotTrigger}
	if !judgeResult(trigCase, skill) {
		t.Error("trigger actual==skill should pass")
	}
	if judgeResult(trigCase, "other") {
		t.Error("trigger actual!=skill should fail")
	}
	if judgeResult(trigCase, "") {
		t.Error("trigger actual empty should fail")
	}
	if !judgeResult(notCase, "other") {
		t.Error("not-trigger actual=other should pass")
	}
	if !judgeResult(notCase, "") {
		t.Error("not-trigger actual empty should pass")
	}
	if judgeResult(notCase, skill) {
		t.Error("not-trigger actual==skill should fail")
	}
}

func TestHealthScore(t *testing.T) {
	allPass := []CaseResult{
		{Kind: KindTrigger, Pass: true},
		{Kind: KindTrigger, Pass: true},
		{Kind: KindNotTrigger, Pass: true},
	}
	if h := HealthScore(allPass, 0); h != 100 {
		t.Fatalf("all pass health=%v want 100", h)
	}
	// trigger 1/2, not 0/1 → base=100*(0.6*0.5+0.4*0)=30
	mixed := []CaseResult{
		{Kind: KindTrigger, Pass: true},
		{Kind: KindTrigger, Pass: false},
		{Kind: KindNotTrigger, Pass: false},
	}
	if h := HealthScore(mixed, 0); h != 30 {
		t.Fatalf("mixed health=%v want 30", h)
	}
	// 全 pass 但 1 regression → 100-8=92
	if h := HealthScore(allPass, 1); h != 92 {
		t.Fatalf("1 regression health=%v want 92", h)
	}
	// 只 trigger 类 → base=triggerAcc*100
	onlyTrig := []CaseResult{
		{Kind: KindTrigger, Pass: true},
		{Kind: KindTrigger, Pass: false},
	}
	if h := HealthScore(onlyTrig, 0); h != 50 {
		t.Fatalf("only-trigger health=%v want 50", h)
	}
}

func TestCompareRuns_ThreeStates(t *testing.T) {
	baseline := &EvalRun{
		RunID: "run-base", ForgeVersion: "v1", AgentModel: "m", DescHash: "h",
		Results: []CaseResult{
			{CaseID: "a", Pass: true},
			{CaseID: "b", Pass: true},
			{CaseID: "c", Pass: true},
		},
	}
	latest := &EvalRun{
		RunID: "run-late", ForgeVersion: "v1", AgentModel: "m", DescHash: "h",
		Results: []CaseResult{
			{CaseID: "a", Pass: false},
			{CaseID: "b", Pass: true},
			{CaseID: "d", Pass: true},
		},
	}
	rep := CompareRuns(latest, baseline)
	if !rep.HasBaseline {
		t.Fatal("want HasBaseline")
	}
	if !rep.Comparable {
		t.Fatal("want Comparable (same version/model/desc)")
	}
	if len(rep.Regressions) != 1 || rep.Regressions[0].CaseID != "a" {
		t.Fatalf("regressions=%v want [a]", rep.Regressions)
	}
	if len(rep.Improvements) != 0 {
		t.Fatalf("improvements=%v want []", rep.Improvements)
	}
	if len(rep.Stable) != 1 || rep.Stable[0].CaseID != "b" {
		t.Fatalf("stable=%v want [b]", rep.Stable)
	}
	if len(rep.New) != 1 || rep.New[0].CaseID != "d" {
		t.Fatalf("new=%v want [d]", rep.New)
	}
	if len(rep.Removed) != 1 || rep.Removed[0].CaseID != "c" {
		t.Fatalf("removed=%v want [c]", rep.Removed)
	}
	if rep.Matched != 2 {
		t.Fatalf("matched=%d want 2", rep.Matched)
	}
	if rep.NetRegressions != 1 {
		t.Fatalf("net=%d want 1", rep.NetRegressions)
	}
}

func TestCompareRuns_NotComparableOnModelChange(t *testing.T) {
	baseline := &EvalRun{RunID: "b", ForgeVersion: "v1", AgentModel: "sonnet", DescHash: "h",
		Results: []CaseResult{{CaseID: "a", Pass: true}}}
	latest := &EvalRun{RunID: "l", ForgeVersion: "v1", AgentModel: "opus", DescHash: "h",
		Results: []CaseResult{{CaseID: "a", Pass: false}}}
	rep := CompareRuns(latest, baseline)
	if rep.Comparable {
		t.Fatal("model change → not comparable")
	}
	if rep.IncomparableReason == "" {
		t.Fatal("want incomparable reason")
	}
	// 数字仍计算，但 report 标不可比（消费方据此降级为 advisory）
	if len(rep.Regressions) != 1 {
		t.Fatalf("regressions still computed=%v want [a]", rep.Regressions)
	}
}

func TestCompareRuns_NoBaseline(t *testing.T) {
	latest := &EvalRun{RunID: "l", Results: []CaseResult{{Kind: KindTrigger, Pass: true}}}
	rep := CompareRuns(latest, nil)
	if rep.HasBaseline {
		t.Fatal("want no baseline")
	}
	if rep.TriggerPassRateLatest != 1 {
		t.Fatalf("trigger rate=%v want 1", rep.TriggerPassRateLatest)
	}
}

func TestSubmitRun_DescHashStalenessRejected(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	mustWrite(t, SaveCases(dir, "my-skill", cases))

	// 改 description（DescHash 变），case 集过期。
	writeSkill(t, canonical, "my-skill", "Use when: 别的场景 or 另一个场景 SKIP: 其他")
	_, err := SubmitRun(dir, canonical, "my-skill", "m", "v1",
		[]SubmitResult{{CaseID: cases[0].ID, ActualTriggered: "my-skill"}})
	if err == nil {
		t.Fatal("stale DescHash should be rejected")
	}
}

func TestSubmitRun_HappyPath(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	mustWrite(t, SaveCases(dir, "my-skill", cases))

	raw := make([]SubmitResult, 0, len(cases))
	for _, c := range cases {
		act := ""
		if c.Kind == KindTrigger {
			act = "my-skill"
		}
		raw = append(raw, SubmitResult{CaseID: c.ID, ActualTriggered: act})
	}
	run, err := SubmitRun(dir, canonical, "my-skill", "sonnet", "v1", raw)
	if err != nil {
		t.Fatal(err)
	}
	if run.HealthScore != 100 {
		t.Fatalf("health=%v want 100", run.HealthScore)
	}
	if run.DescHash == "" || run.CaseSetHash == "" {
		t.Fatal("want DescHash and CaseSetHash set")
	}

	loaded, _ := LoadRuns(dir, "my-skill")
	if len(loaded) != 1 {
		t.Fatalf("runs=%d want 1", len(loaded))
	}
	latest, _ := LatestRun(dir, "my-skill")
	if latest == nil || latest.RunID != run.RunID {
		t.Fatal("latest run mismatch")
	}
}

func TestSubmitRun_RegressionVsBaseline(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	mustWrite(t, SaveCases(dir, "my-skill", cases))

	allRight := func() []SubmitResult {
		raw := make([]SubmitResult, 0, len(cases))
		for _, c := range cases {
			act := ""
			if c.Kind == KindTrigger {
				act = "my-skill"
			}
			raw = append(raw, SubmitResult{CaseID: c.ID, ActualTriggered: act})
		}
		return raw
	}

	// run1 全对 → 设为 baseline。
	r1, err := SubmitRun(dir, canonical, "my-skill", "sonnet", "v1", allRight())
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, SetBaseline(dir, "my-skill", r1.RunID, "test"))

	// run2：第一个 trigger case 故意 fail（regression）。
	raw2 := allRight()
	raw2[0].ActualTriggered = "wrong-skill"
	r2, err := SubmitRun(dir, canonical, "my-skill", "sonnet", "v1", raw2)
	if err != nil {
		t.Fatal(err)
	}
	if r2.BaselineRunID != r1.RunID {
		t.Fatalf("baseline locked=%s want %s", r2.BaselineRunID, r1.RunID)
	}
	// trigger 1/2 pass, not 1/1 → base=100*(0.6*0.5+0.4*1)=70；1 regression → 70-8=62
	if r2.HealthScore != 62 {
		t.Fatalf("health=%v want 62 (1 regression)", r2.HealthScore)
	}

	base, _ := LoadRunByID(dir, "my-skill", r1.RunID)
	rep := CompareRuns(r2, base)
	if len(rep.Regressions) != 1 {
		t.Fatalf("regressions=%d want 1", len(rep.Regressions))
	}
	if rep.NetRegressions != 1 {
		t.Fatalf("net=%d want 1", rep.NetRegressions)
	}
}

func TestBaselinePersistence(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, SetBaseline(dir, "s", "run-x", "test"))
	bl, err := GetBaseline(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	if bl.RunID != "run-x" {
		t.Fatalf("baseline=%v want run-x", bl)
	}
}

// TestSubmitRun_AllUnknownCaseIDsRejected：所有 case_id 都不在 case 集（集刚重建，
// agent 拿旧 id）→ results 空 → 明确报错，不静默落一条 health=0 的空 run 让 agent
// 误判「跑成功只是全挂」。
func TestSubmitRun_AllUnknownCaseIDsRejected(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	mustWrite(t, SaveCases(dir, "my-skill", cases))

	_, err := SubmitRun(dir, canonical, "my-skill", "m", "v1",
		[]SubmitResult{{CaseID: "totally-bogus-id", ActualTriggered: "my-skill"}})
	if err == nil {
		t.Fatal("全未知 case_id 应报错，不静默落空 run")
	}
}

// TestCountRegressions：nil baseline → 0；baseline pass→latest fail → 1。
func TestCountRegressions(t *testing.T) {
	if got := countRegressions(&EvalRun{}, nil); got != 0 {
		t.Fatalf("nil baseline want 0, got %d", got)
	}
	dims := func(results []CaseResult) *EvalRun {
		return &EvalRun{ForgeVersion: "v", AgentModel: "m", DescHash: "h", Results: results}
	}
	base := dims([]CaseResult{{CaseID: "a", Pass: true}})
	latest := dims([]CaseResult{{CaseID: "a", Pass: false}})
	if got := countRegressions(latest, base); got != 1 {
		t.Fatalf("regression want 1, got %d", got)
	}
}

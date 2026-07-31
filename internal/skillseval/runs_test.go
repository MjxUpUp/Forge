package skillseval

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	// All pass but 1 regression → 100-8=92
	//
	// 全 pass 但 1 regression → 100-8=92
	if h := HealthScore(allPass, 1); h != 92 {
		t.Fatalf("1 regression health=%v want 92", h)
	}
	// Only trigger kind → base=triggerAcc*100
	//
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
	// Numbers are still computed, but the report is marked incomparable (consumers degrade to advisory based on this)
	//
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

	// Change description (DescHash changes) — the case set goes stale.
	//
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
	if run.DescHash == "" {
		t.Fatal("want DescHash set")
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

	// run1 all correct → set as baseline.
	//
	// run1 全对 → 设为 baseline。
	r1, err := SubmitRun(dir, canonical, "my-skill", "sonnet", "v1", allRight())
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, SetBaseline(dir, "my-skill", r1.RunID, "test"))

	// run2: the first trigger case intentionally fails (regression).
	//
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

// TestSubmitRun_AllUnknownCaseIDsRejected: all case_id values are absent from the case set (the set was just rebuilt,
// the agent holds stale ids) → results empty → fail explicitly; do not silently persist an empty health=0 run that would lead the agent
// to misread it as 'run succeeded, just all-failed'.
//
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

// TestCountRegressions: nil baseline → 0; baseline pass → latest fail → 1.
//
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

// TestSubmitRun_CorruptBaselineWarnsAndSkips pins the "unreadable baseline is not no-baseline"
// contract: a corrupt baselines.json must NOT be silently treated as "nothing to compare"
// (which would make the regression penalty vanish without a trace). SubmitRun still succeeds
// and records the run, but skips the regression comparison with an explicit stderr warn: —
// the failure is visible, BaselineRunID is left unset.
//
// TestSubmitRun_CorruptBaselineWarnsAndSkips 钉死「baseline 不可读 ≠ 无 baseline」
// 契约：baselines.json 损坏绝不能被静默当作「无可比 baseline」（那会让回归惩罚无痕
// 消失）。SubmitRun 仍成功落 run，但跳过回归比对并在 stderr 打显式 warn:——失败
// 可见，BaselineRunID 不设置。
func TestSubmitRun_CorruptBaselineWarnsAndSkips(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	mustWrite(t, SaveCases(dir, "my-skill", cases))

	// Corrupt baselines.json.
	//
	// 写坏 baselines.json。
	if err := os.MkdirAll(filepath.Dir(baselinesFile(dir)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinesFile(dir), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture stderr to assert the explicit warning.
	//
	// 捕获 stderr 断言显式告警。
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	raw := make([]SubmitResult, 0, len(cases))
	for _, c := range cases {
		act := ""
		if c.Kind == KindTrigger {
			act = "my-skill"
		}
		raw = append(raw, SubmitResult{CaseID: c.ID, ActualTriggered: act})
	}
	run, serr := SubmitRun(dir, canonical, "my-skill", "sonnet", "v1", raw)

	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}

	if serr != nil {
		t.Fatalf("corrupt baselines.json must not fail SubmitRun: %v", serr)
	}
	if run.BaselineRunID != "" {
		t.Errorf("BaselineRunID = %q, want empty（baseline 不可读时不得锁定比对）", run.BaselineRunID)
	}
	if !strings.Contains(buf.String(), "warn:") {
		t.Errorf("stderr 应有 warn: 显式告警（baseline 不可读 ≠ 无 baseline），got %q", buf.String())
	}
	// All cases pass, no regression comparison possible → no penalty, health 100.
	//
	// 全部通过且无法回归比对 → 无惩罚，health 100。
	if run.HealthScore != 100 {
		t.Errorf("health=%v want 100（跳过比对但不对未证实的回归施加惩罚）", run.HealthScore)
	}
}

// TestSubmitRun_UnknownKindSkipped pins the demolition hardening: legacy cases
// with a kind that no longer exists (e.g. "behavior" from the removed
// behavior-probe dimension) must be skipped rather than silently judged as
// not-trigger — a vacuous pass would pollute the run.
//
// TestSubmitRun_UnknownKindSkipped 钉住拆除加固：kind 已不存在的遗留 case
// （如 behavior-probe 维度拆除后的 "behavior"）必须被跳过，而非按
// not-trigger 静默判定——vacuous pass 会污染 run。
func TestSubmitRun_UnknownKindSkipped(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	// Inject a legacy unknown-kind case alongside the valid ones.
	//
	// 在合法 case 中混入一个未知 kind 的遗留 case。
	legacy := cases[0]
	legacy.ID = "legacy-behavior-case"
	legacy.Kind = "behavior"
	cases = append(cases, legacy)
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
	for _, r := range run.Results {
		if r.CaseID == "legacy-behavior-case" {
			t.Error("unknown-kind case must be skipped, not judged")
		}
	}
	if run.HealthScore != 100 {
		t.Fatalf("health=%v want 100 (valid cases all pass, legacy skipped)", run.HealthScore)
	}
}

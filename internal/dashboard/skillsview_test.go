package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// writeCanonicalSkill creates canonical/<name>/SKILL.md (minimal frontmatter body is
// irrelevant — ListSkills only checks existence).
//
// writeCanonicalSkill 建 canonical/<name>/SKILL.md（内容无关紧要——ListSkills 只查存在性）。
func writeCanonicalSkill(t *testing.T, canonical, name string) {
	t.Helper()
	dir := filepath.Join(canonical, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeEvalRuns appends runs to evalDir/runs/<skill>.jsonl (the LoadRuns read path).
//
// writeEvalRuns 把 run 追加到 evalDir/runs/<skill>.jsonl（LoadRuns 的读取路径）。
func writeEvalRuns(t *testing.T, evalDir, skill string, runs []skillseval.EvalRun) {
	t.Helper()
	dir := filepath.Join(evalDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b []byte
	for _, r := range runs {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, data...)
		b = append(b, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, skill+".jsonl"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeToollogEntries appends tool calls to DataDir/toollog.jsonl (toolusage.LoadAllAll path).
//
// writeToollogEntries 把 tool 调用追加到 DataDir/toollog.jsonl（toolusage.LoadAllAll 路径）。
func writeToollogEntries(t *testing.T, dataDir string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b []byte
	for _, l := range lines {
		b = append(b, l...)
		b = append(b, '\n')
	}
	if err := os.WriteFile(filepath.Join(dataDir, "toollog.jsonl"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// skillsFixture builds: canonical {alpha, beta}; project with 2 passive skill-trigger hits
// (alpha) + 1 active Skill tool call (alpha, task feat/t1) + a scored conclusion for feat/t1
// (80/B); evalDir with 2 runs for alpha (baseline run-1 all-pass, latest run-2 one trigger
// regression) + baselines.json anchoring run-1.
//
// skillsFixture 构造：canonical {alpha, beta}；项目带 2 次被动 skill-trigger（alpha）+
// 1 次主动 Skill 调用（alpha，task feat/t1）+ feat/t1 的评分结论（80/B）；evalDir 含
// alpha 的 2 条 run（baseline run-1 全过，latest run-2 一条 trigger 退化）+ baselines.json 锚 run-1。
func skillsFixture(t *testing.T) (opts Options, canonical, evalDir string) {
	t.Helper()
	root, p := forgedatatest.RealProject(t)
	canonical = t.TempDir()
	evalDir = t.TempDir()
	writeCanonicalSkill(t, canonical, "alpha")
	writeCanonicalSkill(t, canonical, "beta")

	now := time.Now()
	writeChecklogEntries(t, p.DataDir, []checklog.Entry{
		{Check: checklog.CheckSkillTrigger, Passed: true, Checked: true,
			Detail: checklog.DetailForSkillTrigger("alpha", "PostToolUse", "edit .go"), RecordedAt: now},
		{Check: checklog.CheckSkillTrigger, Passed: true, Checked: true,
			Detail: checklog.DetailForSkillTrigger("alpha", "SessionStart", "keyword"), RecordedAt: now},
	})
	writeToollogEntries(t, p.DataDir, []string{
		`{"tool_name":"Skill","tool_input":"{\"skill\":\"alpha\"}","task_ref":"feat/t1","timestamp":"` + now.UTC().Format(time.RFC3339) + `"}`,
	})
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/t1", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	base := time.Unix(1700000000, 0).UTC()
	run1 := skillseval.EvalRun{
		RunID: "run-1", Skill: "alpha", Timestamp: base,
		ForgeVersion: "v1", AgentModel: "m1", DescHash: "dh1",
		Results: []skillseval.CaseResult{
			{CaseID: "t1", Kind: skillseval.KindTrigger, Pass: true},
			{CaseID: "t2", Kind: skillseval.KindTrigger, Pass: true},
			{CaseID: "n1", Kind: skillseval.KindNotTrigger, Pass: true},
		},
		HealthScore: 100,
	}
	run2 := skillseval.EvalRun{
		RunID: "run-2", Skill: "alpha", Timestamp: base.Add(time.Hour),
		ForgeVersion: "v1", AgentModel: "m1", DescHash: "dh1",
		BaselineRunID: "run-1",
		Results: []skillseval.CaseResult{
			{CaseID: "t1", Kind: skillseval.KindTrigger, Pass: true},
			{CaseID: "t2", Kind: skillseval.KindTrigger, Pass: false}, // 退化
			{CaseID: "n1", Kind: skillseval.KindNotTrigger, Pass: true},
		},
		HealthScore: 55,
	}
	writeEvalRuns(t, evalDir, "alpha", []skillseval.EvalRun{run1, run2})
	if err := skillseval.SetBaseline(evalDir, "alpha", "run-1", "test"); err != nil {
		t.Fatal(err)
	}
	return Options{Root: root}, canonical, evalDir
}

func findSkill(t *testing.T, skills []SkillSummary, name string) SkillSummary {
	t.Helper()
	for _, s := range skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("skills 总览缺 %q: %+v", name, skills)
	return SkillSummary{}
}

// TestAggregateSkills_HitsMergeTwoSources: passive checklog firings + active toollog Skill
// calls merge into one per-skill hit count (2 passive + 1 active = 3).
//
// TestAggregateSkills_HitsMergeTwoSources：被动 checklog 触发 + 主动 toollog Skill 调用
// 合并成单条 per-skill 命中数（2 被动 + 1 主动 = 3）。
func TestAggregateSkills_HitsMergeTwoSources(t *testing.T) {
	opts, canonical, evalDir := skillsFixture(t)
	ov, err := AggregateSkills(opts, canonical, evalDir)
	if err != nil {
		t.Fatal(err)
	}
	alpha := findSkill(t, ov.Skills, "alpha")
	if alpha.Hits != 3 {
		t.Errorf("alpha hits = %d, want 3（2 被动 + 1 主动合并）", alpha.Hits)
	}
	beta := findSkill(t, ov.Skills, "beta")
	if beta.Hits != 0 {
		t.Errorf("beta hits = %d, want 0", beta.Hits)
	}
	if ov.Coverage == "" {
		t.Error("coverage 说明字段不得为空（盲区须显式呈现）")
	}
}

// TestAggregateSkills_EffectivenessJoin: the active Skill call's TaskRef joins to the act
// conclusion, giving taskCount=1 / avgScore=80 for alpha.
//
// TestAggregateSkills_EffectivenessJoin：主动 Skill 调用的 TaskRef join 到 act 结论，
// alpha 得 taskCount=1 / avgScore=80。
func TestAggregateSkills_EffectivenessJoin(t *testing.T) {
	opts, canonical, evalDir := skillsFixture(t)
	ov, err := AggregateSkills(opts, canonical, evalDir)
	if err != nil {
		t.Fatal(err)
	}
	alpha := findSkill(t, ov.Skills, "alpha")
	if alpha.TaskCount != 1 {
		t.Errorf("alpha taskCount = %d, want 1", alpha.TaskCount)
	}
	if alpha.AvgScore == nil || *alpha.AvgScore != 80 {
		t.Errorf("alpha avgScore = %v, want 80", alpha.AvgScore)
	}
	beta := findSkill(t, ov.Skills, "beta")
	if beta.AvgScore != nil {
		t.Errorf("beta 无关联任务，avgScore 应为 null，got %v", *beta.AvgScore)
	}
}

// TestAggregateSkills_HealthFromLatestRun: health comes from the latest EvalRun (55);
// a skill without runs gets null health.
//
// TestAggregateSkills_HealthFromLatestRun：健康分取最新 EvalRun（55）；无 run 的 skill
// health 为 null。
func TestAggregateSkills_HealthFromLatestRun(t *testing.T) {
	opts, canonical, evalDir := skillsFixture(t)
	ov, err := AggregateSkills(opts, canonical, evalDir)
	if err != nil {
		t.Fatal(err)
	}
	alpha := findSkill(t, ov.Skills, "alpha")
	if alpha.Health == nil || *alpha.Health != 55 {
		t.Errorf("alpha health = %v, want 55（最新 run）", alpha.Health)
	}
	beta := findSkill(t, ov.Skills, "beta")
	if beta.Health != nil {
		t.Errorf("beta 无 eval run，health 应为 null，got %v", *beta.Health)
	}
}

// TestAggregateSkills_NeverTriggered: canonical skills with zero hits land in
// neverTriggered; hit skills do not.
//
// TestAggregateSkills_NeverTriggered：零命中的 canonical skill 进 neverTriggered；
// 有命中的不进。
func TestAggregateSkills_NeverTriggered(t *testing.T) {
	opts, canonical, evalDir := skillsFixture(t)
	ov, err := AggregateSkills(opts, canonical, evalDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.NeverTriggered) != 1 || ov.NeverTriggered[0] != "beta" {
		t.Errorf("neverTriggered = %v, want [beta]", ov.NeverTriggered)
	}
}

// TestAggregateSkills_NoEvalDirDegrades: a nonexistent eval dir (the ~/.pi legacy path
// never created) must degrade gracefully — empty health, no error.
//
// TestAggregateSkills_NoEvalDirDegrades：eval 目录不存在（~/.pi 遗留路径从未建过）必须
// 优雅降级——health 为空，不报错。
func TestAggregateSkills_NoEvalDirDegrades(t *testing.T) {
	opts, canonical, _ := skillsFixture(t)
	ov, err := AggregateSkills(opts, canonical, filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("eval 目录不存在不应报错: %v", err)
	}
	alpha := findSkill(t, ov.Skills, "alpha")
	if alpha.Health != nil {
		t.Errorf("无 eval 数据时 health 应为 null，got %v", *alpha.Health)
	}
	if alpha.Hits != 3 {
		t.Errorf("无 eval 数据时 hits 仍应正常聚合，got %d", alpha.Hits)
	}
}

// TestLoadSkillDetail_RunsSeriesAndCompare: the detail view carries the full run time
// series (with per-run trigger/not-trigger accuracy), the baseline anchor, and the
// CompareRuns regression counts.
//
// TestLoadSkillDetail_RunsSeriesAndCompare：详情视图带完整 run 时间序列（含每 run 的
// trigger/not-trigger 准确率）、baseline 锚点与 CompareRuns 回归计数。
func TestLoadSkillDetail_RunsSeriesAndCompare(t *testing.T) {
	_, canonical, evalDir := skillsFixture(t)
	d, err := LoadSkillDetail(canonical, evalDir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "alpha" {
		t.Errorf("Name = %q, want alpha", d.Name)
	}
	if len(d.Runs) != 2 {
		t.Fatalf("runs len = %d, want 2", len(d.Runs))
	}
	if d.Runs[0].RunID != "run-1" || d.Runs[0].Health != 100 || d.Runs[0].Cases != 3 {
		t.Errorf("runs[0] 异常: %+v", d.Runs[0])
	}
	latest := d.Runs[1]
	if latest.TriggerAcc == nil || *latest.TriggerAcc != 0.5 {
		t.Errorf("latest triggerAcc = %v, want 0.5", latest.TriggerAcc)
	}
	if latest.NotTriggerAcc == nil || *latest.NotTriggerAcc != 1.0 {
		t.Errorf("latest notTriggerAcc = %v, want 1.0", latest.NotTriggerAcc)
	}
	if d.BaselineRunID != "run-1" {
		t.Errorf("baselineRunId = %q, want run-1", d.BaselineRunID)
	}
	if d.Compare == nil {
		t.Fatal("有 baseline 时 compare 不得为 null")
	}
	if d.Compare.NetRegressions != 1 || d.Compare.Regressions != 1 || d.Compare.Improvements != 0 || !d.Compare.Comparable {
		t.Errorf("compare 异常: %+v", d.Compare)
	}
}

// TestLoadSkillDetail_Decisions: decisions.md history is parsed into the decision list.
//
// TestLoadSkillDetail_Decisions：decisions.md 决策史解析进决策列表。
func TestLoadSkillDetail_Decisions(t *testing.T) {
	_, canonical, evalDir := skillsFixture(t)
	err := skillsdecisions.AppendDecision(canonical, "alpha", skillsdecisions.SkillDecision{
		Diagnosis: "trigger 漏报", Revision: "加关键词", Evidence: "run-2 t2 fail",
		Outcome: skillsdecisions.OutcomeAccept, Rationale: "实测有效", CommitHash: "abc1234",
		By: "claude-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := LoadSkillDetail(canonical, evalDir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Decisions) != 1 {
		t.Fatalf("decisions len = %d, want 1", len(d.Decisions))
	}
	dec := d.Decisions[0]
	if dec.Outcome != "accept" || dec.Diagnosis != "trigger 漏报" || dec.Rationale != "实测有效" || dec.Commit != "abc1234" {
		t.Errorf("decision 字段映射异常: %+v", dec)
	}
	if dec.ID == "" || dec.Ts.IsZero() {
		t.Errorf("decision 应有 id 与时间: %+v", dec)
	}
}

// TestLoadSkillDetail_TriggerQualityAndBlindSpot: triggerQuality mirrors the latest run;
// liveFalsePositiveRate stays null (no data source — never fabricated).
//
// TestLoadSkillDetail_TriggerQualityAndBlindSpot：triggerQuality 镜像最新 run；
// liveFalsePositiveRate 保持 null（无数据源——不编造）。
func TestLoadSkillDetail_TriggerQualityAndBlindSpot(t *testing.T) {
	_, canonical, evalDir := skillsFixture(t)
	d, err := LoadSkillDetail(canonical, evalDir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if d.TriggerQuality == nil {
		t.Fatal("有 run 时 triggerQuality 不得为 null")
	}
	if d.TriggerQuality.FromRun != "run-2" || d.TriggerQuality.Cases != 3 {
		t.Errorf("triggerQuality 异常: %+v", d.TriggerQuality)
	}
	if d.TriggerQuality.TriggerAcc == nil || *d.TriggerQuality.TriggerAcc != 0.5 {
		t.Errorf("triggerQuality.triggerAcc = %v, want 0.5", d.TriggerQuality.TriggerAcc)
	}
	if d.LiveFalsePositiveRate != nil {
		t.Errorf("liveFalsePositiveRate 必须保持 null（线上误触发率无数据源），got %v", *d.LiveFalsePositiveRate)
	}
	// JSON 层同样序列化为 null。
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["liveFalsePositiveRate"]) != "null" {
		t.Errorf("JSON liveFalsePositiveRate 应为 null，got %s", m["liveFalsePositiveRate"])
	}
}

// TestLoadSkillDetail_NoRuns: an unknown-but-valid skill name yields an empty view
// (nil compare / nil triggerQuality), not an error.
//
// TestLoadSkillDetail_NoRuns：合法但无数据的 skill 名给空视图（compare/triggerQuality
// 为 null），不报错。
func TestLoadSkillDetail_NoRuns(t *testing.T) {
	_, canonical, evalDir := skillsFixture(t)
	d, err := LoadSkillDetail(canonical, evalDir, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Runs) != 0 || d.Compare != nil || d.TriggerQuality != nil {
		t.Errorf("无 run 的 skill 应为空视图: %+v", d)
	}
}

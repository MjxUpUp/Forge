package cliskills

// skills_closure_test.go — P0-P2 新增 CLI 面的配对测试：decide --prediction / verify
// （prediction→verification 闭环）、battery（回归电池报告 + --gate exit 4）、analyze
// （弱点挖掘报告，子进程 + 隔离项目）。照 skills_eval_loop_test.go 的 runXxx(nil,nil) +
// 捕获 stdout 模式；子进程用 runForge（TestMain 已隔离 home/registry）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// closureDecide 进程内跑一次 `skills decide`（带指定 prediction）；返回 AppendDecision
// 分配的决策 ID（新 ID = 与调用前的差集）。
func closureDecide(t *testing.T, skill, prediction string) string {
	t.Helper()
	before := map[string]bool{}
	if ds, err := skillsdecisions.LoadDecisions(canonicalEnv(t), skill); err == nil {
		for _, d := range ds {
			before[d.ID] = true
		}
	}
	skDecSkill = skill
	skDecDiagnosis = "diagnosis"
	skDecRevision = "revision"
	skDecEvidence = "evidence"
	skDecOutcome = "accept"
	skDecPrediction = prediction
	defer func() {
		skDecSkill = ""
		skDecDiagnosis = ""
		skDecRevision = ""
		skDecEvidence = ""
		skDecOutcome = ""
		skDecPrediction = ""
	}()
	if err := runSkillsDecide(nil, nil); err != nil {
		t.Fatalf("decide: %v", err)
	}
	ds, err := skillsdecisions.LoadDecisions(canonicalEnv(t), skill)
	if err != nil {
		t.Fatalf("LoadDecisions: %v", err)
	}
	for _, d := range ds {
		if !before[d.ID] {
			return d.ID
		}
	}
	t.Fatal("decide 后应出现一条新决策")
	return ""
}

// canonicalEnv 返回测试设置的 FORGE_SKILLS_CANONICAL 值（与 evalLoopSetup 的 t.Setenv
// 对称；让调用点明示 canonical 从哪来）。
func canonicalEnv(t *testing.T) string {
	t.Helper()
	v := os.Getenv("FORGE_SKILLS_CANONICAL")
	if v == "" {
		t.Fatal("FORGE_SKILLS_CANONICAL 未设置（先 evalLoopSetup）")
	}
	return v
}

// closureCanonical 隔离 home 并种入标准 closure-skill canonical 树，返回
// canonical 目录——verify 闭环测试共享的测试头。
func closureCanonical(t *testing.T) string {
	t.Helper()
	canonical := t.TempDir()
	evalLoopIsolateHome(t)
	evalLoopWriteSkill(t, canonical, "closure-skill", "Use when: 编写 React 组件 SKIP: 选型")
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)
	return canonical
}

// setSkVerVars 为一次进程内调用钉住 `skills verify` 的包级变量，测试结束时
// 复位（flag 绑定的全局状态）。
func setSkVerVars(t *testing.T, skill, decision, result, at string) {
	t.Helper()
	skVerSkill = skill
	skVerDecision = decision
	skVerResult = result
	skVerAt = at
	t.Cleanup(func() {
		skVerSkill = ""
		skVerDecision = ""
		skVerResult = ""
		skVerAt = ""
	})
}

func TestSkillsDecideVerify_PredictionClosure(t *testing.T) {
	canonical := closureCanonical(t)

	// 第一步：decide 在修改时刻声明可检验预测。
	prediction := `触发率应从 15% 升到 30%`
	id := closureDecide(t, "closure-skill", prediction)
	ds, err := skillsdecisions.LoadDecisions(canonical, "closure-skill")
	if err != nil || len(ds) != 1 {
		t.Fatalf("LoadDecisions: %v", err)
	}
	if ds[0].Prediction != prediction {
		t.Fatalf("Prediction=%q want %q（decide --prediction 未落盘）", ds[0].Prediction, prediction)
	}

	// 第二步：verify 把真实结果回填到该决策。
	setSkVerVars(t, "closure-skill", id, `命中：触发率 32%`, "2026-08-16T10:00:00Z")
	if err := runSkillsVerify(nil, nil); err != nil {
		t.Fatalf("verify: %v", err)
	}
	ds, err = skillsdecisions.LoadDecisions(canonical, "closure-skill")
	if err != nil || len(ds) != 1 {
		t.Fatalf("LoadDecisions after verify: %v", err)
	}
	if ds[0].Verification != `命中：触发率 32%` || ds[0].VerifiedAt.IsZero() {
		t.Fatalf("验证未回填: %+v", ds[0])
	}
	wantAt, _ := time.Parse(time.RFC3339, "2026-08-16T10:00:00Z")
	if !ds[0].VerifiedAt.Equal(wantAt) {
		t.Fatalf("VerifiedAt=%v want %v（--at 未生效）", ds[0].VerifiedAt, wantAt)
	}
}

func TestSkillsVerify_InvalidAt(t *testing.T) {
	closureCanonical(t)
	id := closureDecide(t, "closure-skill", "p")

	setSkVerVars(t, "closure-skill", id, "r", "not-a-time")
	err := runSkillsVerify(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("--at 非法应报 RFC3339 错误, got %v", err)
	}
}

func TestSkillsVerify_HistoryLedger(t *testing.T) {
	closureCanonical(t)

	// d1：带预测未验证；d2：带预测已验证；d3：无预测。
	d1 := closureDecide(t, "closure-skill", "pred-1")
	d2 := closureDecide(t, "closure-skill", "pred-2")
	setSkVerVars(t, "closure-skill", d2, "已验证", "2026-08-16T10:00:00Z")
	if err := runSkillsVerify(nil, nil); err != nil {
		t.Fatalf("verify d2: %v", err)
	}
	// 测试中段手动复位（history 模式在变量清零下运行；helper 的 cleanup 届时
	// 再复位一次，等于 no-op）。
	skVerSkill = ""
	skVerDecision = ""
	skVerResult = ""
	skVerAt = ""
	closureDecide(t, "closure-skill", "") // d3 无预测

	// 文本台账：逐决策渲染状态。
	skVerHistory = true
	skVerHistoryJSON = false
	defer func() { skVerHistory = false; skVerHistoryJSON = false }()
	var hErr error
	out := captureStdout(t, func() { hErr = runSkillsVerify(nil, nil) })
	if hErr != nil {
		t.Fatalf("history: %v", hErr)
	}
	if !strings.Contains(out, "未验证") || !strings.Contains(out, "已验证") {
		t.Fatalf("台账应含未验证/已验证两种状态:\n%s", out)
	}
	if !strings.Contains(out, "pred-1") || !strings.Contains(out, "无预测") {
		t.Fatalf("台账应展示预测文本与无预测标注:\n%s", out)
	}

	// JSON 台账：机器可读行 + verified 标志。
	skVerHistory = false
	skVerHistoryJSON = true
	out = captureStdout(t, func() { hErr = runSkillsVerify(nil, nil) })
	if hErr != nil {
		t.Fatalf("history-json: %v", hErr)
	}
	var rows []VerifyHistoryRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rows); err != nil {
		t.Fatalf("history-json 不是合法 JSON: %v\n%s", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d want 3", len(rows))
	}
	verified := map[string]bool{}
	preds := map[string]string{}
	for _, r := range rows {
		verified[r.ID] = r.Verified
		preds[r.ID] = r.Prediction
	}
	if verified[d1] || !verified[d2] {
		t.Fatalf("verified 状态错: d1=%v want false, d2=%v want true", verified[d1], verified[d2])
	}
	if preds[d1] != "pred-1" || preds[d2] != "pred-2" || preds[closureThirdID(t, rows)] != "" {
		t.Fatalf("预测列错: %v", preds)
	}
}

// closureThirdID 找到预测为空的那行（无预测决策）。
func closureThirdID(t *testing.T, rows []VerifyHistoryRow) string {
	t.Helper()
	for _, r := range rows {
		if r.Prediction == "" {
			return r.ID
		}
	}
	t.Fatal("应存在一条无预测行")
	return ""
}

// TestSkillsBattery_ReportAndGate: full loop — gen → all-correct record → baseline → gate passes (exit 0) → regressed record → report shows reject → --gate exits 4.
//
// TestSkillsBattery_ReportAndGate：完整闭环——gen→全对 record→baseline→gate 过（exit 0）→
// 含回归 record→报告 reject→--gate exit 4。
func TestSkillsBattery_ReportAndGate(t *testing.T) {
	canonical := t.TempDir()
	skill := "batt-skill" // 唯一 skill 名，隔离共享 EvalDir 下的 baselines.json
	evalLoopSetup(t, canonical, skill)

	// 全对 run → 锚定 baseline。
	evalLoopRecord(t, skill, evalLoopResultsAllRight(t, canonical, skill), "sonnet", "v1")
	skBaseSkill = skill
	skBaseRun = ""
	if err := runSkillsEvalBaseline(nil, nil); err != nil {
		t.Fatal(err)
	}
	skBaseSkill = ""
	skBaseRun = ""

	// 尚无回归：gate 模式须保持 exit 0（纯报告契约）。
	if _, _, code := runForge(t, t.TempDir(), "skills", "battery", "--gate"); code != 0 {
		t.Fatalf("无回归时 battery --gate 应 exit 0, got %d", code)
	}

	// 第二个 run：首个 trigger case 误路由 → 相对 baseline 回归。
	cases, _ := skillseval.EvalCases(canonical, skill)
	results := make([]skillseval.SubmitResult, 0, len(cases))
	first := true
	for _, c := range cases {
		act := ""
		if c.Kind == skillseval.KindTrigger {
			if first {
				act = "wrong-skill"
				first = false
			} else {
				act = skill
			}
		}
		results = append(results, skillseval.SubmitResult{CaseID: c.ID, ActualTriggered: act})
	}
	evalLoopRecord(t, skill, evalLoopWriteResults(t, results), "sonnet", "v1")

	// 人读报告：reject 行先行。
	var battErr error
	out := captureStdout(t, func() { battErr = runSkillsBattery(nil, nil) })
	if battErr != nil {
		t.Fatalf("battery: %v", battErr)
	}
	if !strings.Contains(out, "reject") || !strings.Contains(out, skill) {
		t.Fatalf("报告应含 reject 行:\n%s", out)
	}

	// JSON 报告：GateBlocked=true（机器门禁信号）。
	skBattJSON = true
	out = captureStdout(t, func() { battErr = runSkillsBattery(nil, nil) })
	skBattJSON = false
	if battErr != nil {
		t.Fatalf("battery --json: %v", battErr)
	}
	var rep skillseval.BatteryReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rep); err != nil {
		t.Fatalf("battery --json 不是合法 JSON: %v\n%s", err, out)
	}
	if rep.Rejected != 1 || !rep.GateBlocked {
		t.Fatalf("rejected=%d gateBlocked=%v want 1/true", rep.Rejected, rep.GateBlocked)
	}

	// 门禁契约：exit 4 + BLOCKED 走 STDERR（对齐 skills audit --gate；stdout 是数据通道，
	// `--json --gate | jq .` 不得吃到非 JSON 字节）。
	gateOut, gateErr, code := runForgeStreams(t, t.TempDir(), "skills", "battery", "--gate")
	if code != 4 {
		t.Fatalf("battery --gate 应 exit 4, got %d, out: %s", code, gateOut)
	}
	if !strings.Contains(gateErr, "BLOCKED") {
		t.Fatalf("BLOCKED 应在 stderr:\nstderr: %s", gateErr)
	}

	// --json --gate：即便 reject，stdout 仍须是可解析 JSON；门禁信号由退出码 + stderr
	// 承载（审查 F2）。
	jsonOut, jsonErr, code := runForgeStreams(t, t.TempDir(), "skills", "battery", "--json", "--gate")
	if code != 4 {
		t.Fatalf("battery --json --gate 应 exit 4, got %d", code)
	}
	if !strings.Contains(jsonErr, "BLOCKED") {
		t.Fatalf("BLOCKED 应在 stderr:\nstderr: %s", jsonErr)
	}
	var gateRep skillseval.BatteryReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonOut)), &gateRep); err != nil {
		t.Fatalf("--json --gate 的 stdout 应为纯 JSON:\n%s", jsonOut)
	}
	if !gateRep.GateBlocked {
		t.Fatal("gate 报告应 GateBlocked=true")
	}
}

// TestSkillsAnalyze_WeaknessReport: subprocess against an isolated legacy-.forge project seeded with recurring low dims; canonical has 2 never-triggered skills.
//
// TestSkillsAnalyze_WeaknessReport：子进程跑隔离的 legacy-.forge 项目（预置复现低分维度）；
// canonical 有 2 个从未触发 skill。断言报告浮出维度弱点/从未触发/caveat。
func TestSkillsAnalyze_WeaknessReport(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0755); err != nil {
		t.Fatal(err)
	}
	proj, err := forgedata.ProjectFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	// testing 低分复现 2 次（达到 weakDimMinCount）；scope 1 次（噪声过滤）。
	for _, ref := range []string{"t1", "t2"} {
		if err := act.Append(proj, &act.Conclusion{
			TaskRef:       ref,
			Score:         60,
			Grade:         "C",
			Strength:      "Strong",
			Ratio:         0.9,
			LowDimensions: []string{"testing"},
			CompletedAt:   time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
	}

	canonical := t.TempDir()
	evalLoopIsolateHome(t)
	evalLoopWriteSkill(t, canonical, "alpha", "Use when: 编写 React 组件 SKIP: 选型")
	evalLoopWriteSkill(t, canonical, "beta", "Use when: 数据分析 SKIP: 选型")
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)

	out, _, code := runForge(t, dir, "skills", "analyze")
	if code != 0 {
		t.Fatalf("skills analyze exit %d:\n%s", code, out)
	}
	for _, want := range []string{"弱点挖掘报告", "testing", "从未触发", "⚠"} {
		if !strings.Contains(out, want) {
			t.Errorf("报告缺 %q:\n%s", want, out)
		}
	}
}

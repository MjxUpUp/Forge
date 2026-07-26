package cli

// skills_eval_loop_test.go — eval-gen(--cases-only) / eval-record / eval-baseline /
// eval-report 四命令的端到端串测。隔离 home（避免污染真实 ~/.pi/research）+ 隔离
// canonical，照 skills_audit_test.go 的 runXxx(nil,nil) + 捕获 stdout 模式。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// evalLoopIsolateHome 把 home 隔离到临时目录，防测试落到真实 ~/.pi/research。
// os.UserHomeDir 在 Windows 认 USERPROFILE、unix 认 HOME，两个都设。
func evalLoopIsolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
}

func evalLoopWriteSkill(t *testing.T, canonical, name, desc string) {
	t.Helper()
	sd := filepath.Join(canonical, name)
	mustMkdir(t, os.MkdirAll(sd, 0755))
	mustMkdir(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n\nbody\n"), 0644))
}

// evalLoopSetup 造 skill + 隔离 home/canonical + 生成结构化 case 集。复用前缀。
func evalLoopSetup(t *testing.T, canonical, skill string) {
	t.Helper()
	evalLoopIsolateHome(t)
	evalLoopWriteSkill(t, canonical, skill, "Use when: 编写 React 组件 or 实现前端布局 SKIP: 选择技术栈")
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)

	skEvalSkill = skill
	skEvalCasesOnly = true
	if err := runSkillsEvalGen(nil, nil); err != nil {
		t.Fatalf("eval-gen --cases-only: %v", err)
	}
	skEvalSkill = ""
	skEvalCasesOnly = false
}

// evalLoopWriteResults 把一组 SubmitResult 落临时文件，返回路径供 eval-record --from。
func evalLoopWriteResults(t *testing.T, results []skillseval.SubmitResult) string {
	t.Helper()
	b, _ := json.Marshal(results)
	p := filepath.Join(t.TempDir(), "results.json")
	mustMkdir(t, os.WriteFile(p, b, 0644))
	return p
}

// evalLoopResultsAllRight 造一组全对结果（trigger→skill，not→空）。
func evalLoopResultsAllRight(t *testing.T, canonical, skill string) string {
	t.Helper()
	cases, _ := skillseval.EvalCases(canonical, skill)
	results := make([]skillseval.SubmitResult, 0, len(cases))
	for _, c := range cases {
		act := ""
		if c.Kind == skillseval.KindTrigger {
			act = skill
		}
		results = append(results, skillseval.SubmitResult{CaseID: c.ID, ActualTriggered: act})
	}
	return evalLoopWriteResults(t, results)
}

// evalLoopRecord 跑一次 eval-record，失败 t.Fatal。
func evalLoopRecord(t *testing.T, skill, from, model, ver string) {
	t.Helper()
	skRecSkill = skill
	skRecFrom = from
	skRecModel = model
	skRecVer = ver
	defer func() {
		skRecSkill = ""
		skRecFrom = "-"
		skRecModel = ""
		skRecVer = ""
	}()
	if err := runSkillsEvalRecord(nil, nil); err != nil {
		t.Fatalf("eval-record: %v", err)
	}
}

func TestRunSkillsEvalGen_CasesOnly_SavesCaseSet(t *testing.T) {
	canonical := t.TempDir()
	evalLoopSetup(t, canonical, "loop-skill")

	dir, _ := skillseval.EvalDir()
	data, err := os.ReadFile(filepath.Join(dir, "cases", "loop-skill.json"))
	if err != nil {
		t.Fatalf("cases json not written: %v", err)
	}
	var set skillseval.CaseSet
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 3 {
		t.Fatalf("cases=%d want 3 (2 trigger + 1 skip)", len(set.Cases))
	}
	if set.DescHash == "" {
		t.Fatal("CaseSet 缺 DescHash")
	}
}

func TestRunSkillsEvalRecord_HappyPath(t *testing.T) {
	canonical := t.TempDir()
	evalLoopSetup(t, canonical, "loop-skill")

	from := evalLoopResultsAllRight(t, canonical, "loop-skill")

	skRecSkill = "loop-skill"
	skRecFrom = from
	skRecModel = "sonnet"
	skRecVer = "v1"
	defer func() {
		skRecSkill = ""
		skRecFrom = "-"
		skRecModel = ""
		skRecVer = ""
	}()

	var recErr error
	out := captureStdout(t, func() { recErr = runSkillsEvalRecord(nil, nil) })
	if recErr != nil {
		t.Fatalf("record: %v", recErr)
	}
	if !strings.Contains(out, "health=100") {
		t.Fatalf("stdout=%q want health=100", out)
	}
	dir, _ := skillseval.EvalDir()
	latest, _ := skillseval.LatestRun(dir, "loop-skill")
	if latest == nil {
		t.Fatal("run not appended")
	}
	if latest.ForgeVersion != "v1" || latest.AgentModel != "sonnet" {
		t.Fatalf("version/model not stamped: %+v", latest)
	}
}

func TestRunSkillsEvalBaseline_DefaultsToLatest(t *testing.T) {
	canonical := t.TempDir()
	evalLoopSetup(t, canonical, "loop-skill")
	evalLoopRecord(t, "loop-skill", evalLoopResultsAllRight(t, canonical, "loop-skill"), "sonnet", "v1")

	skBaseSkill = "loop-skill"
	skBaseRun = ""
	defer func() { skBaseSkill = ""; skBaseRun = "" }()

	if err := runSkillsEvalBaseline(nil, nil); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	dir, _ := skillseval.EvalDir()
	bl, _ := skillseval.GetBaseline(dir, "loop-skill")
	if bl.RunID == "" {
		t.Fatal("baseline not set")
	}
	if bl.SetBy != "cli" {
		t.Fatalf("set_by=%q want cli", bl.SetBy)
	}
}

// TestRunSkillsEvalReport_JSON_ShowRegression：完整闭环——gen→全对 record→baseline→
// 含 1 个 regression 的 record→report，断言 NetRegressions=1。
func TestRunSkillsEvalReport_JSON_ShowRegression(t *testing.T) {
	canonical := t.TempDir()
	skill := "loop-skill"
	evalLoopSetup(t, canonical, skill)

	// run1 全对 → 设 baseline。
	evalLoopRecord(t, skill, evalLoopResultsAllRight(t, canonical, skill), "sonnet", "v1")
	skBaseSkill = skill
	skBaseRun = ""
	if err := runSkillsEvalBaseline(nil, nil); err != nil {
		t.Fatal(err)
	}
	skBaseSkill = ""

	// run2：第一个 trigger case 故意 fail（actual=别的 skill → regression）。
	cases, _ := skillseval.EvalCases(canonical, skill)
	results := make([]skillseval.SubmitResult, 0, len(cases))
	first := true
	for _, c := range cases {
		act := ""
		if c.Kind == skillseval.KindTrigger {
			if first {
				act = "wrong-skill" // 故意误路由 → regression
				first = false
			} else {
				act = skill
			}
		}
		results = append(results, skillseval.SubmitResult{CaseID: c.ID, ActualTriggered: act})
	}
	evalLoopRecord(t, skill, evalLoopWriteResults(t, results), "sonnet", "v1")

	// report --json。
	skRepSkill = skill
	skRepJSON = true
	skRepVerbose = false
	skRepBaseline = ""
	defer func() {
		skRepSkill = ""
		skRepJSON = false
	}()

	var repErr error
	out := captureStdout(t, func() { repErr = runSkillsEvalReport(nil, nil) })
	if repErr != nil {
		t.Fatalf("report: %v", repErr)
	}
	var rep skillseval.RegressionReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rep); err != nil {
		t.Fatalf("report not JSON: %v\n%s", err, out)
	}
	if !rep.HasBaseline {
		t.Fatal("report 应锚定 baseline")
	}
	if !rep.Comparable {
		t.Fatalf("应可比，原因=%s", rep.IncomparableReason)
	}
	if rep.NetRegressions != 1 {
		t.Fatalf("net_regressions=%d want 1", rep.NetRegressions)
	}
}

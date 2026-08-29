package cli

// skills_mutex_test.go — 互斥命令族的 CLI 面测试：mutex-gen（边 + case 落盘 + 摘要）、
// mutex-record（pass/fail 判定、未知跳过、全未知报错）、mutex-report（混淆矩阵 JSON +
// 子进程验证 --gate exit 4 且 BLOCKED 在 stderr）。照 skills_eval_loop_test.go 的
// runXxx(nil,nil) + captureStdout 模式；门禁契约照 TestSkillsBattery_ReportAndGate
// 用 runForgeStreams 断言。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// mutexCLISetup 造双 skill canonical（skill-a 声明让渡给 skill-b），隔离 canonical
// 与 eval 目录，cleanup 时复位 CLI flag。返回 eval 目录供测试直接读落盘产物。
func mutexCLISetup(t *testing.T) string {
	t.Helper()
	canonical := t.TempDir()
	evalLoopWriteSkill(t, canonical, "skill-a",
		"Use when: 处理 A 域任务时 SKIP: B 域任务（用 skill-b）")
	evalLoopWriteSkill(t, canonical, "skill-b",
		"Use when: 处理 B 域任务时 SKIP: 无让渡")
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)

	dir := t.TempDir()
	skEvalDirFlag = dir
	t.Cleanup(func() { skEvalDirFlag = "" })
	return dir
}

// mutexCLIGen 进程内跑一次 mutex-gen，返回派生的 case 集。
func mutexCLIGen(t *testing.T, dir string) []skillseval.MutexCase {
	t.Helper()
	var genErr error
	out := captureStdout(t, func() { genErr = runSkillsMutexGen(nil, nil) })
	if genErr != nil {
		t.Fatalf("mutex-gen: %v", genErr)
	}
	if !strings.Contains(out, "skill-a → skill-b") {
		t.Fatalf("边摘要应含 skill-a → skill-b:\n%s", out)
	}
	cases, err := skillseval.LoadMutexCases(dir)
	if err != nil || len(cases) == 0 {
		t.Fatalf("mutex-gen 后应有落盘 case: %v, %v", cases, err)
	}
	return cases
}

func TestRunSkillsMutexGen_SavesCases(t *testing.T) {
	dir := mutexCLISetup(t)
	cases := mutexCLIGen(t, dir)
	for _, c := range cases {
		if c.Positive != "skill-b" || c.Negative != "skill-a" {
			t.Fatalf("case 应锚定 Positive=skill-b/Negative=skill-a: %+v", c)
		}
	}
}

func TestRunSkillsMutexRecord_Judgment(t *testing.T) {
	dir := mutexCLISetup(t)
	cases := mutexCLIGen(t, dir)

	// 第一条路由正确，第二条路由回让渡声明方（混淆）。
	raw := []skillseval.SubmitResult{
		{CaseID: cases[0].ID, ActualTriggered: "skill-b"},
		{CaseID: cases[0].ID, ActualTriggered: "skill-a"},
		{CaseID: "unknown-case", ActualTriggered: "skill-a"},
	}
	b, _ := json.Marshal(raw)
	from := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(from, b, 0644); err != nil {
		t.Fatal(err)
	}

	skMRecFrom = from
	skMRecModel = "sonnet"
	skMRecVer = "v1"
	defer func() { skMRecFrom = "-"; skMRecModel = ""; skMRecVer = "" }()

	var recErr error
	out := captureStdout(t, func() { recErr = runSkillsMutexRecord(nil, nil) })
	if recErr != nil {
		t.Fatalf("mutex-record: %v", recErr)
	}
	if !strings.Contains(out, "1/2 passed") {
		t.Fatalf("应 1/2 passed（actual==Positive 才 pass，未知 case_id 跳过）:\n%s", out)
	}
	latest, err := skillseval.LatestMutexRun(dir)
	if err != nil || latest == nil {
		t.Fatalf("run 未落盘: %v, %v", latest, err)
	}

	// 全未知 → 显式报错（SubmitRun 语义）。
	bad, _ := json.Marshal([]skillseval.SubmitResult{{CaseID: "nope", ActualTriggered: "x"}})
	badFrom := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badFrom, bad, 0644); err != nil {
		t.Fatal(err)
	}
	skMRecFrom = badFrom
	if err := runSkillsMutexRecord(nil, nil); err == nil {
		t.Fatal("全未知 case_id 应显式报错")
	}
}

// TestRunSkillsMutexReport_GateContract：含混淆行的 run → JSON 报告 GateBlocked=true；
// 子进程 --gate exit 4 且 BLOCKED 在 STDERR、stdout 保持纯 JSON；干净 run 的 --gate
// 保持 exit 0。
func TestRunSkillsMutexReport_GateContract(t *testing.T) {
	dir := mutexCLISetup(t)
	cases := mutexCLIGen(t, dir)

	// 回填一个全部路由回让渡声明方的 run（全是混淆行）。
	raw := make([]skillseval.SubmitResult, 0, len(cases))
	for _, c := range cases {
		raw = append(raw, skillseval.SubmitResult{CaseID: c.ID, ActualTriggered: c.Negative})
	}
	b, _ := json.Marshal(raw)
	from := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(from, b, 0644); err != nil {
		t.Fatal(err)
	}
	skMRecFrom = from
	skMRecVer = "v1"
	if err := runSkillsMutexRecord(nil, nil); err != nil {
		t.Fatalf("mutex-record: %v", err)
	}
	skMRecFrom = "-"
	skMRecVer = ""

	// 进程内 JSON 报告：GateBlocked=true。
	skMRepJSON = true
	var repErr error
	out := captureStdout(t, func() { repErr = runSkillsMutexReport(nil, nil) })
	skMRepJSON = false
	if repErr != nil {
		t.Fatalf("mutex-report --json: %v", repErr)
	}
	var m skillseval.MutexMatrix
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("report 非合法 JSON: %v\n%s", err, out)
	}
	if !m.GateBlocked || len(m.Confusions) == 0 {
		t.Fatalf("应 GateBlocked=true 且有混淆行: %+v", m)
	}

	// 子进程门禁契约：exit 4 + BLOCKED 在 stderr + stdout 仍可解析。显式传 --dir
	// （子进程看不到进程内 flag 变量）。
	gateOut, gateErr, code := runForgeStreams(t, t.TempDir(),
		"skills", "mutex-report", "--gate", "--json", "--dir", dir)
	if code != 4 {
		t.Fatalf("mutex-report --gate 应 exit 4, got %d, out: %s", code, gateOut)
	}
	if !strings.Contains(gateErr, "BLOCKED") {
		t.Fatalf("BLOCKED 应在 stderr:\nstderr: %s", gateErr)
	}
	var gm skillseval.MutexMatrix
	if err := json.Unmarshal([]byte(strings.TrimSpace(gateOut)), &gm); err != nil {
		t.Fatalf("--json --gate 的 stdout 应为纯 JSON:\n%s", gateOut)
	}
	if !gm.GateBlocked {
		t.Fatal("gate 报告应 GateBlocked=true")
	}

	// 干净 run（全路由到 Positive）→ --gate 保持 exit 0。
	clean := make([]skillseval.SubmitResult, 0, len(cases))
	for _, c := range cases {
		clean = append(clean, skillseval.SubmitResult{CaseID: c.ID, ActualTriggered: c.Positive})
	}
	cb, _ := json.Marshal(clean)
	cleanFrom := filepath.Join(t.TempDir(), "clean.json")
	if err := os.WriteFile(cleanFrom, cb, 0644); err != nil {
		t.Fatal(err)
	}
	skMRecFrom = cleanFrom
	if err := runSkillsMutexRecord(nil, nil); err != nil {
		t.Fatalf("mutex-record(clean): %v", err)
	}
	skMRecFrom = "-"
	if _, _, code := runForge(t, t.TempDir(), "skills", "mutex-report", "--gate", "--dir", dir); code != 0 {
		t.Fatalf("无混淆时 mutex-report --gate 应 exit 0, got %d", code)
	}
}

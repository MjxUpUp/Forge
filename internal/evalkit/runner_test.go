package evalkit

// runner/decompose/judge/rotate/report 的行为测试（Track A 数学与持久化）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func smokeManifest(t *testing.T) *BenchmarkManifest {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	content := `id: bench-test
version: 1.0.0
split: frozen
tasks:
  - id: t-pass
    command: "true"
  - id: t-fail
    command: "false"
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRunBenchmarkExecRunner(t *testing.T) {
	spec := RunSpec{Profile: ProfileFull, Model: "m1", Benchmark: "bench-test", Split: "frozen",
		Repeats: 2, ForgeRef: "test", Budget: Budget{WallclockEach: 5 * time.Second}}
	sc, err := RunBenchmark(context.Background(), spec, smokeManifest(t), ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	// t-pass 恒过、t-fail 恒败：pass@1 = 1/2；pass^2 = 1/2（t-pass 两次全对，
	// t-fail 两次全败——pass^k 是"任务级 k 次全对"的比例，不是全体任务通过）。
	if sc.Pass1.Numerator != 1 || sc.Pass1.Denominator != 2 {
		t.Fatalf("pass@1 应为 1/2: %+v", sc.Pass1)
	}
	if len(sc.PassKCurve) != 2 || sc.PassKCurve[1].Value != 0.5 {
		t.Fatalf("pass^2 应为 1/2: %+v", sc.PassKCurve)
	}
	if !strings.Contains(sc.Header, "profile=full") || !strings.Contains(sc.Header, "组合评测") {
		t.Fatalf("scorecard 头部缺四元组/评测对象声明: %s", sc.Header)
	}
}

func TestRunSpecValidation(t *testing.T) {
	spec := RunSpec{Profile: "weird", Model: "m", Benchmark: "b", Split: "s", Repeats: 1, ForgeRef: "r", Budget: Budget{WallclockEach: time.Second}}
	if err := spec.Validate(); err == nil {
		t.Fatal("非法 profile 应报错")
	}
	noBudget := RunSpec{Profile: ProfileFull, Model: "m", Benchmark: "b", Split: "s", Repeats: 1, ForgeRef: "r"}
	if err := noBudget.Validate(); err == nil {
		t.Fatal("预算全空应报错")
	}
}

func TestRunBenchmarkBudgetCut(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	content := "id: b\nversion: 1\nsplit: frozen\ntasks:\n  - id: slow\n    command: \"sleep 2\"\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatal(err)
	}
	spec := RunSpec{Profile: ProfileOff, Model: "m", Benchmark: "b", Split: "frozen",
		Repeats: 1, ForgeRef: "t", Budget: Budget{WallclockEach: 50 * time.Millisecond}}
	sc, err := RunBenchmark(context.Background(), spec, m, ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.BudgetCuts != 1 {
		t.Fatalf("超墙钟应记 budget-cut: %+v", sc)
	}
	if !strings.Contains(sc.Note, "预算截断") {
		t.Fatalf("scorecard 应披露截断: %s", sc.Note)
	}
}

func TestRunDecompose(t *testing.T) {
	grid := DecomposeGrid{Profiles: []Profile{ProfileOff, ProfileFull}, Models: []string{"m1", "m2"}}
	spec := RunSpec{Profile: ProfileOff, Model: "grid", Benchmark: "bench-test", Split: "frozen",
		Repeats: 1, ForgeRef: "t", Budget: Budget{WallclockEach: time.Second}}
	rep, err := RunDecompose(context.Background(), grid, spec, smokeManifest(t), ScriptedRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Decomposition == nil || len(rep.CellMeans) != 4 {
		t.Fatalf("应有 2×2 网格与分解: %+v", rep)
	}
	if len(rep.DeltaFullOff) != 2 {
		t.Fatalf("应有逐模型 full-off 差值")
	}
	md := rep.RenderDecomposeMarkdown()
	for _, want := range []string{"HV̄/MV̄", "排名翻转", "η²_p", "整体贡献（full − off）"} {
		if !strings.Contains(md, want) {
			t.Fatalf("分解报告缺 %q", want)
		}
	}
	if err := grid.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := DecomposeGrid{Profiles: []Profile{ProfileOff}, Models: []string{"m1"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("1×1 网格应报错")
	}
}

func TestJudgeAudit(t *testing.T) {
	good := []JudgeAuditEntry{
		{DocID: "d1", JudgeScores: []int{80, 80, 82}, HumanScore: 80, Threshold: 75},
		{DocID: "d2", JudgeScores: []int{60, 61, 59}, HumanScore: 60, Threshold: 75},
	}
	rep, err := RunJudgeAudit(good)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.KappaValid || !rep.JudgeReliable {
		t.Fatalf("一致判分应可靠: %+v", rep)
	}
	bad := []JudgeAuditEntry{
		{DocID: "d1", JudgeScores: []int{80, 80}, HumanScore: 60, Threshold: 75},
		{DocID: "d2", JudgeScores: []int{60, 61}, HumanScore: 80, Threshold: 75},
	}
	// 阈值同侧抖动不是 finding（首轮实测修正：2-5 分正常噪声不得报"自洽性
	// 不足"）；跨阈值抖动才是。
	sameSide := []JudgeAuditEntry{
		{DocID: "s1", JudgeScores: []int{88, 90, 87}, HumanScore: 80, Threshold: 75},
		{DocID: "s2", JudgeScores: []int{78, 80, 77}, HumanScore: 80, Threshold: 75},
	}
	repSS, err := RunJudgeAudit(sameSide)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range repSS.Findings {
		if strings.Contains(f, "自洽性") || strings.Contains(f, "不稳定") {
			t.Fatalf("阈值同侧抖动不得报 finding: %s", f)
		}
	}
	crossing := []JudgeAuditEntry{
		{DocID: "c1", JudgeScores: []int{78, 80, 74}, HumanScore: 80, Threshold: 75},
	}
	repC, err := RunJudgeAudit(crossing)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range repC.Findings {
		if strings.Contains(f, "c1") && strings.Contains(f, "跨越阈值") {
			found = true
		}
	}
	if !found {
		t.Fatalf("跨阈值抖动必须报 finding: %v", repC.Findings)
	}
	rep2, err := RunJudgeAudit(bad)
	if err != nil {
		t.Fatal(err)
	}
	if !rep2.KappaValid || rep2.JudgeReliable {
		t.Fatalf("完全错位应不可靠: %+v", rep2)
	}
	if len(rep2.Findings) == 0 {
		t.Fatal("κ 低于阈值应产生 finding（降级 ADVISORY）")
	}
}

func TestPersistScorecardAndGoldenReport(t *testing.T) {
	evalDir := t.TempDir()
	root := t.TempDir()
	spec := RunSpec{Profile: ProfileFull, Model: "m", Benchmark: "b", Split: "s", Repeats: 1, ForgeRef: "r", Budget: Budget{WallclockEach: time.Second}}
	manifest := &BenchmarkManifest{ID: "b", Version: "1", Split: "s", fingerprint: "fp",
		Tasks: []BenchTask{{ID: "t1", Command: "true"}}}
	sc, err := RunBenchmark(context.Background(), spec, manifest, ScriptedRunner{})
	if err != nil {
		t.Fatal(err)
	}
	path, err := PersistScorecard(evalDir, root, sc)
	if err != nil || !strings.Contains(path, "scorecard") {
		t.Fatalf("scorecard 持久化失败: %v %s", err, path)
	}
	grep, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(grep), "组合评测") {
		t.Fatalf("scorecard JSON 应含评测对象声明: %v", err)
	}
}

func TestRotatePrivateGoldenPermissions(t *testing.T) {
	evalDir := t.TempDir()
	dir, err := InitPrivateGolden(evalDir)
	if err != nil {
		t.Fatal(err)
	}
	// 目录未建用例：加载报错（轮换要求可解析集合）。
	if _, _, err := RotatePrivateGolden(evalDir, 10, t.TempDir()); err == nil {
		t.Fatal("空私有集应报错")
	}
	// 写一个 0644 的用例 → 权限检查拒绝。
	caseYaml := "id: p1\ngate: g\nkind: clean\nexpect: clean\ndescription: d\nprobe_argv: [x]\ndetect_any: [exit_nonzero]\n"
	if err := os.WriteFile(filepath.Join(dir, "p1.yaml"), []byte(caseYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RotatePrivateGolden(evalDir, 10, t.TempDir()); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("0644 私有用例应被拒: %v", err)
	}
	// 修正为 0600 → 轮换保留。
	if err := os.Chmod(filepath.Join(dir, "p1.yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, _, err := RotatePrivateGolden(evalDir, 10, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Kept != 1 || len(rec.Retired) != 0 {
		t.Fatalf("合法用例应保留: %+v", rec)
	}
}

func TestBuildQuarterlyReport(t *testing.T) {
	evalDir := t.TempDir()
	dir := filepath.Join(evalDir, "forge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 一份 golden 报告在场，其余证据缺失。
	golden := `{"precision":{"numerator":1,"denominator":2,"value":0.5},"false_positive":{"numerator":0,"denominator":1,"value":0}}`
	if err := os.WriteFile(filepath.Join(dir, "golden-report-1.json"), []byte(golden), 0o644); err != nil {
		t.Fatal(err)
	}
	md, missing, err := BuildQuarterlyReport(evalDir, "2026-Q3", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "2026-Q3") || !strings.Contains(md, "golden") {
		t.Fatalf("报告应含季度与 golden 节: %s", md)
	}
	found := false
	for _, m := range missing {
		if strings.Contains(m, "dashboard") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺失证据应如实列出: %v", missing)
	}
}

func TestLoadResumeDrillsAndRun(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeForge(t, dir, `echo "start EVAL-DRILL-9 $*"; exit 0`)
	drillYaml := `id: d1
description: fake
steps:
  - argv: ["{forge}", "task", "start"]
    expect_contains: ["EVAL-DRILL-9"]
`
	drillDir := filepath.Join(dir, "drills")
	if err := os.MkdirAll(drillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(drillDir, "d.yaml"), []byte(drillYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	drills, err := LoadResumeDrills(drillDir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := RunResumeDrills(drills, bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Passed {
		t.Fatalf("演练应通过: %+v", results)
	}
	fid, err := ResumeFidelity(results)
	if err != nil || fid.Value != 1 {
		t.Fatalf("保真度应 1: %+v %v", fid, err)
	}
}

func TestSelectRunnerLadder(t *testing.T) {
	dockerManifest := func() *BenchmarkManifest {
		return &BenchmarkManifest{ID: "tb", Version: "1", Split: "frozen", Tasks: []BenchTask{
			{ID: "t1", Image: "alpine:3.20", RunCmd: "echo", TestCmd: "true", Command: "true"},
		}}
	}
	plainManifest := &BenchmarkManifest{ID: "cs", Version: "1", Split: "frozen", Tasks: []BenchTask{{ID: "t1", Command: "true"}}}

	// 未武装 smoke：一律确定性替身（容器任务也不跑）。
	r, label, degraded := SelectRunner(dockerManifest(), false)
	if _, ok := r.(ScriptedRunner); !ok || label != SandboxScripted || degraded {
		t.Fatalf("无 smoke 应 scripted 不降级: %T %s %v", r, label, degraded)
	}
	// 纯命令 manifest + smoke → exec（真实命令执行）。
	r, label, degraded = SelectRunner(plainManifest, true)
	if _, ok := r.(ExecRunner); !ok || label != SandboxExec || degraded {
		t.Fatalf("纯命令 + smoke 应 exec: %T %s %v", r, label, degraded)
	}
	// 容器 manifest + smoke + docker 打桩为不可用 → 回退 exec 并标注降级。
	orig := dockerAvailableFunc
	dockerAvailableFunc = func() bool { return false }
	dockerChecked = false // 清缓存：可用性每进程只探测一次，桩切换必须重置
	defer func() { dockerAvailableFunc = orig; dockerChecked = false }()
	r, label, degraded = SelectRunner(dockerManifest(), true)
	if label != SandboxFallbackExec || !degraded {
		t.Fatalf("docker 不可用应回退并标注 fallback-exec: %T %s %v", r, label, degraded)
	}
	if sl, ok := r.(sandboxLabeled); !ok || sl.SandboxLabel() != SandboxFallbackExec {
		t.Fatalf("降级标签必须经 sandboxLabeled 透传到 scorecard: %v", ok)
	}
	// docker 打桩为可用 → DockerRunner。
	dockerAvailableFunc = func() bool { return true }
	dockerChecked = false
	r, label, degraded = SelectRunner(dockerManifest(), true)
	if _, ok := r.(DockerRunner); !ok || label != SandboxDocker || degraded {
		t.Fatalf("docker 可用应容器执行: %T %s %v", r, label, degraded)
	}
}

func TestFallbackExecAnnotatedInScorecard(t *testing.T) {
	spec := RunSpec{Profile: ProfileFull, Model: "m", Benchmark: "b", Split: "frozen",
		Repeats: 1, ForgeRef: "r", Budget: Budget{WallclockEach: time.Second}}
	manifest := &BenchmarkManifest{ID: "b", Version: "1", Split: "frozen", fingerprint: "fp",
		Tasks: []BenchTask{{ID: "t1", Command: "true", Image: "alpine:3.20", TestCmd: "true"}}}
	sc, err := RunBenchmark(context.Background(), spec, manifest, ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Sandbox != SandboxFallbackExec && sc.Sandbox != SandboxExec {
		t.Fatalf("sandbox 应为 exec/fallback-exec 之一: %s", sc.Sandbox)
	}
	if !strings.Contains(sc.Header, "sandbox=") {
		t.Fatalf("头部应带 sandbox 标注: %s", sc.Header)
	}
}

// TestSummarizeSandboxes 钉住沙箱标签粒度规则（环境无关纯函数）：
// 单一后端 → 该标签；混合 → mixed（宿主 exec 不得躲在 docker 标签后）；
// 降级声明优先（fallback-exec 顶层保留，mix 仍展示真实分布）。
func TestSummarizeSandboxes(t *testing.T) {
	label, mix := summarizeSandboxes(SandboxDocker, map[string]string{
		"t1": SandboxDocker, "t2": SandboxDocker,
	})
	if label != SandboxDocker || mix[SandboxDocker] != 2 {
		t.Fatalf("纯容器跑应标 docker: %s %v", label, mix)
	}
	label, mix = summarizeSandboxes(SandboxDocker, map[string]string{
		"t1": SandboxDocker, "t2": SandboxExec,
	})
	if label != "mixed" || mix[SandboxDocker] != 1 || mix[SandboxExec] != 1 {
		t.Fatalf("混合跑应标 mixed 且分布如实: %s %v", label, mix)
	}
	label, mix = summarizeSandboxes(SandboxFallbackExec, map[string]string{
		"t1": SandboxDocker, "t2": SandboxExec,
	})
	if label != SandboxFallbackExec || mix[SandboxDocker] != 1 {
		t.Fatalf("降级声明优先，mix 展示真实分布: %s %v", label, mix)
	}
	label, _ = summarizeSandboxes(SandboxScripted, nil)
	if label != SandboxScripted {
		t.Fatalf("无任务数据时用 runner 声明: %s", label)
	}
}

// TestDockerRunnerMixedDispatchTaskSandbox 钉住混合分派下的任务级标签：
// 纯命令任务在 DockerRunner 下回退 ExecRunner 执行并如实标注 exec
// （不依赖 docker 可用性——命令路径不触碰 docker）。
func TestDockerRunnerMixedDispatchTaskSandbox(t *testing.T) {
	spec := RunSpec{Profile: ProfileFull, Model: "m", Benchmark: "b", Split: "s",
		Repeats: 1, ForgeRef: "t", Budget: Budget{WallclockEach: 5 * time.Second}}
	res, err := DockerRunner{}.RunTask(context.Background(), spec,
		BenchTask{ID: "cmd-only", Command: "true"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pass || res.Sandbox != SandboxExec {
		t.Fatalf("命令任务应回退 exec 并标注: %+v", res)
	}
}

// TestScorecardMixedSandboxSurfaces 钉住 scorecard/header 消费面：
// 混合分布时顶层 mixed 且 header 可见（防标签只在 JSON 里、头部照旧骗人）。
func TestScorecardMixedSandboxSurfaces(t *testing.T) {
	spec := RunSpec{Profile: ProfileFull, Model: "m", Benchmark: "b", Split: "s",
		Repeats: 1, ForgeRef: "t", Budget: Budget{WallclockEach: time.Second}}
	manifest := &BenchmarkManifest{ID: "b", Version: "1", Split: "s", fingerprint: "fp",
		Tasks: []BenchTask{{ID: "t1", Image: "alpine:3.20", TestCmd: "true"}, {ID: "t2", Command: "true"}}}
	attempts := []Attempt{{TaskID: "t1", Repeat: 1, Pass: true}, {TaskID: "t2", Repeat: 1, Pass: true}}
	// 真正的混合：docker 形态任务跑容器 + 命令任务跑宿主 exec。
	sc := buildScorecard(spec, manifest, attempts, 0, 0, 0, SandboxDocker,
		map[string]string{"t1": SandboxDocker, "t2": SandboxExec})
	if sc.Sandbox != "mixed" || sc.SandboxMix[SandboxExec] != 1 {
		t.Fatalf("混合应顶层 mixed + 分布: %s %v", sc.Sandbox, sc.SandboxMix)
	}
	if !strings.Contains(sc.Header, "sandbox=mixed") {
		t.Fatalf("header 必须可见 mixed: %s", sc.Header)
	}
}

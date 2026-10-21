package evalkit

// validity_test.go — 周期总纲 R3/R4/R5 的行为测试：Track A preflight（oracle /
// do-nothing / 逃生舱 / n/a）、leakage 与复核清单、扰动臂与分层 bootstrap、null
// 结论 MDE 文本、golden test 层计数、judge 成对位置交换。全部离线：preflight 用
// 桩 runner（按任务预设各臂结果并计调用次数），golden 用假 forge 二进制。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// stubBaselineRunner is a BaselineRunner double: per-task outcomes for the
// main run and each baseline arm, plus call counters.
//
// stubBaselineRunner 是 BaselineRunner 替身：按任务预设主评测与各基线臂的结果，
// 并计调用次数。
type stubBaselineRunner struct {
	main      map[string]bool
	tokens    map[string]int
	oracle    map[string]bool
	doNothing map[string]bool
	leakage   map[string]bool
	budgetCut map[BaselineArm]bool
	armErr    error
	mainCalls int
	armCalls  map[BaselineArm]int
	perTask   map[string]int
	variants  []string
}

func newStub() *stubBaselineRunner {
	return &stubBaselineRunner{
		main: map[string]bool{}, tokens: map[string]int{}, oracle: map[string]bool{},
		doNothing: map[string]bool{}, leakage: map[string]bool{}, budgetCut: map[BaselineArm]bool{},
		armCalls: map[BaselineArm]int{}, perTask: map[string]int{},
	}
}

func (s *stubBaselineRunner) RunTask(_ context.Context, spec RunSpec, task BenchTask) (TaskResult, error) {
	s.mainCalls++
	s.variants = append(s.variants, spec.Variant)
	return TaskResult{TaskID: task.ID, Pass: s.main[task.ID], Tokens: s.tokens[task.ID], Sandbox: SandboxScripted}, nil
}

func (s *stubBaselineRunner) RunBaseline(_ context.Context, _ RunSpec, task BenchTask, arm BaselineArm) (TaskResult, bool, error) {
	if task.TestCmd == "" {
		return TaskResult{TaskID: task.ID}, false, nil
	}
	var pass bool
	switch arm {
	case ArmOracle:
		if task.OracleCmd == "" {
			return TaskResult{TaskID: task.ID}, false, nil
		}
		pass = s.oracle[task.ID]
	case ArmDoNothing:
		pass = s.doNothing[task.ID]
	case ArmLeakage:
		if task.LeakageCmd == "" {
			return TaskResult{TaskID: task.ID}, false, nil
		}
		pass = s.leakage[task.ID]
	}
	s.armCalls[arm]++
	s.perTask[task.ID]++
	if s.armErr != nil {
		return TaskResult{}, true, s.armErr
	}
	return TaskResult{TaskID: task.ID, Pass: pass && !s.budgetCut[arm], BudgetCut: s.budgetCut[arm]}, true, nil
}

// firstFailRunner fails the first attempt of each (profile, model, task) cell
// on the full profile and passes the rest; every other profile always fails.
//
// firstFailRunner 在 full 档每个（profile, model, task）格的首次 attempt 失败、
// 之后通过；其余 profile 一律失败。
type firstFailRunner struct{ seen map[string]int }

func (r *firstFailRunner) RunTask(_ context.Context, spec RunSpec, task BenchTask) (TaskResult, error) {
	key := string(spec.Profile) + "|" + spec.Model + "|" + task.ID
	r.seen[key]++
	return TaskResult{TaskID: task.ID, Pass: spec.Profile == ProfileFull && r.seen[key] > 1}, nil
}

func gradedTask(id string) BenchTask {
	return BenchTask{ID: id, Command: "true", Image: "img", RunCmd: "solve", TestCmd: "grade", OracleCmd: "ref"}
}

func validitySpec(repeats int) RunSpec {
	return RunSpec{Profile: ProfileFull, Model: "m", Benchmark: "b", Split: "s", Repeats: repeats, ForgeRef: "r", Budget: Budget{WallclockEach: time.Second}}
}

func manifestOf(tasks ...BenchTask) *BenchmarkManifest {
	return &BenchmarkManifest{ID: "b", Version: "1", Split: "s", Tasks: tasks, fingerprint: "fp"}
}

// TestRunBenchmarkPreflight covers REQ-4 / INV-1 / NFR-1: violations refuse
// the run before any main attempt, the escape hatch skips with a Weak note,
// command-only manifests and non-baseline runners stay n/a and unchanged, and
// each graded task costs at most 3 baseline executions regardless of repeats.
func TestRunBenchmarkPreflight(t *testing.T) {
	cases := []struct {
		name        string
		tasks       []BenchTask
		setup       func(s *stubBaselineRunner)
		wantErr     bool
		wantFailed  []string
		wantMissing []string
		wantDN      []string
		wantStatus  string
	}{
		{name: "合法基线放行", tasks: []BenchTask{gradedTask("t1"), gradedTask("t2")},
			setup:      func(s *stubBaselineRunner) { s.oracle["t1"], s.oracle["t2"] = true, true },
			wantStatus: PreflightPassed},
		{name: "oracle 未通过", tasks: []BenchTask{gradedTask("t1"), gradedTask("t2")},
			setup:   func(s *stubBaselineRunner) { s.oracle["t2"] = true },
			wantErr: true, wantFailed: []string{"t1"}},
		{name: "oracle 未声明", tasks: []BenchTask{gradedTask("t1"), func() BenchTask { b := gradedTask("t2"); b.OracleCmd = ""; return b }()},
			setup:   func(s *stubBaselineRunner) { s.oracle["t1"] = true },
			wantErr: true, wantMissing: []string{"t2"}},
		{name: "do-nothing 击穿判分", tasks: []BenchTask{gradedTask("t1")},
			setup:   func(s *stubBaselineRunner) { s.oracle["t1"], s.doNothing["t1"] = true, true },
			wantErr: true, wantDN: []string{"t1"}},
		{name: "无判分任务为 n/a", tasks: []BenchTask{{ID: "c1", Command: "true"}},
			setup: func(s *stubBaselineRunner) {}, wantStatus: PreflightNA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newStub()
			tc.setup(stub)
			sc, err := RunBenchmark(context.Background(), validitySpec(3), manifestOf(tc.tasks...), stub)
			if tc.wantErr {
				var pe *PreflightError
				if !errors.As(err, &pe) {
					t.Fatalf("应返回 PreflightError，得到 %v", err)
				}
				if stub.mainCalls != 0 {
					t.Fatalf("违例后不得跑主评测，实际 %d 次", stub.mainCalls)
				}
				if strings.Join(pe.Report.OracleFailed, ",") != strings.Join(tc.wantFailed, ",") ||
					strings.Join(pe.Report.OracleMissing, ",") != strings.Join(tc.wantMissing, ",") ||
					strings.Join(pe.Report.DoNothingPassed, ",") != strings.Join(tc.wantDN, ",") {
					t.Fatalf("违例清单不符: %+v", pe.Report)
				}
				if !strings.Contains(pe.Error(), "首发即 blocked") || !strings.Contains(pe.Error(), PreflightDisableEnv+"=skip") {
					t.Fatalf("拒绝文案缺首发声明或逃生指引: %s", pe.Error())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if sc.Preflight == nil || sc.Preflight.Status != tc.wantStatus {
				t.Fatalf("preflight 状态应为 %s: %+v", tc.wantStatus, sc.Preflight)
			}
			if !containsLine(sc.HarnessDisclosure, "preflight: "+tc.wantStatus) {
				t.Fatalf("披露清单缺 preflight 行: %v", sc.HarnessDisclosure)
			}
			// NFR-1：每个判分任务至多 3 次基线（本例未声明 leakage → 2 次），与 repeats=3 无关。
			for id, n := range stub.perTask {
				if n > 2 {
					t.Fatalf("任务 %s 基线执行 %d 次，超出 2（无 leakage）", id, n)
				}
			}
		})
	}

	t.Run("声明 leakage 时每任务至多 3 次基线", func(t *testing.T) {
		stub := newStub()
		task := gradedTask("t1")
		task.LeakageCmd = "guess"
		stub.oracle["t1"] = true
		if _, err := RunBenchmark(context.Background(), validitySpec(5), manifestOf(task), stub); err != nil {
			t.Fatal(err)
		}
		if stub.perTask["t1"] != 3 {
			t.Fatalf("基线次数应为 3（oracle+do-nothing+leakage），得到 %d", stub.perTask["t1"])
		}
	})

	t.Run("逃生舱跳过并封顶 Weak", func(t *testing.T) {
		t.Setenv(PreflightDisableEnv, "skip")
		stub := newStub() // oracle 全败——若未跳过必然违例
		sc, err := RunBenchmark(context.Background(), validitySpec(1), manifestOf(gradedTask("t1")), stub)
		if err != nil {
			t.Fatalf("逃生舱下不应拒绝: %v", err)
		}
		if sc.Preflight.Status != PreflightSkipped || len(stub.armCalls) != 0 {
			t.Fatalf("应跳过且零基线调用: %+v %v", sc.Preflight, stub.armCalls)
		}
		if !strings.Contains(sc.Note, "Weak") {
			t.Fatalf("Note 应写明证据封顶 Weak: %q", sc.Note)
		}
		// 不支持基线的后端没有可跳过的东西：仍是 n/a，不得记成 skipped。
		sc2, err := RunBenchmark(context.Background(), validitySpec(1), smokeManifest(t), ScriptedRunner{})
		if err != nil || sc2.Preflight.Status != PreflightNA {
			t.Fatalf("逃生舱下 scripted 后端应仍为 n/a: %v %+v", err, sc2.Preflight)
		}
		root := t.TempDir()
		RecordPreflightEscape(root)
		rows, err := checklog.LoadAllAll(root)
		if err != nil || len(rows) != 1 || rows[0].Check != checklog.CheckEscapeHatch || checklog.EscapeGateOf(&rows[0]) != string(checklog.CheckEvalPreflight) {
			t.Fatalf("应落一条 eval-preflight 逃生舱行: %v %+v", err, rows)
		}
	})

	t.Run("违例落 eval-preflight 行", func(t *testing.T) {
		root := t.TempDir()
		_, err := RunBenchmark(context.Background(), validitySpec(1), manifestOf(gradedTask("t1")), newStub())
		var pe *PreflightError
		if !errors.As(err, &pe) {
			t.Fatalf("应违例: %v", err)
		}
		RecordPreflightBlock(root, validitySpec(1), pe)
		rows, _ := checklog.LoadAllAll(root)
		if len(rows) != 1 || rows[0].Check != checklog.CheckEvalPreflight || rows[0].Passed || rows[0].Level != checklog.LevelBlocked {
			t.Fatalf("应落一条 Passed=false 的 eval-preflight 行: %+v", rows)
		}
	})

	// 超时与执行错误不是裁决：不得返回 PreflightError（否则 exit 2 误报"环境坏了"）。
	for _, arm := range []BaselineArm{ArmOracle, ArmDoNothing} {
		t.Run("基线超时不成裁决/"+string(arm), func(t *testing.T) {
			stub := newStub()
			stub.oracle["t1"] = true
			stub.budgetCut[arm] = true
			_, err := RunBenchmark(context.Background(), validitySpec(1), manifestOf(gradedTask("t1")), stub)
			var pe *PreflightError
			if err == nil || errors.As(err, &pe) || !strings.Contains(err.Error(), "--wallclock") {
				t.Fatalf("超时应为执行错误（提示调大 --wallclock），不是违例: %v", err)
			}
		})
	}
	t.Run("基线执行错误原样上抛", func(t *testing.T) {
		stub := newStub()
		stub.armErr = errors.New("docker 起不来")
		_, err := RunBenchmark(context.Background(), validitySpec(1), manifestOf(gradedTask("t1")), stub)
		var pe *PreflightError
		if err == nil || errors.As(err, &pe) || !strings.Contains(err.Error(), "docker 起不来") || stub.mainCalls != 0 {
			t.Fatalf("执行错误应原样上抛且不跑主评测: %v", err)
		}
	})

	// 生产 BaselineRunner（DockerRunner）的臂语义：纯函数 baselineCommand 表驱动。
	t.Run("容器基线命令组装", func(t *testing.T) {
		full := BenchTask{ID: "x", Image: "img", RunCmd: "solve", TestCmd: "grade", OracleCmd: "ref", LeakageCmd: "guess"}
		noOracle := full
		noOracle.OracleCmd, noOracle.LeakageCmd = "", ""
		cmdOnly := BenchTask{ID: "c", Command: "true", TestCmd: "grade", OracleCmd: "ref"}
		noGrader := full
		noGrader.TestCmd = ""
		cases := []struct {
			task       BenchTask
			arm        BaselineArm
			want       string
			applicable bool
		}{
			{full, ArmOracle, "ref && grade", true},
			{full, ArmDoNothing, "grade", true},
			{full, ArmLeakage, "guess && grade", true},
			{noOracle, ArmOracle, "", false},
			{noOracle, ArmLeakage, "", false},
			{noOracle, ArmDoNothing, "grade", true},
			{cmdOnly, ArmDoNothing, "", false},
			{noGrader, ArmOracle, "", false},
		}
		for _, c := range cases {
			got, ok, err := baselineCommand(c.task, c.arm)
			if err != nil || got != c.want || ok != c.applicable {
				t.Fatalf("%s/%s: 得到 (%q,%v,%v)，期望 (%q,%v)", c.task.ID, c.arm, got, ok, err, c.want, c.applicable)
			}
		}
		if _, _, err := baselineCommand(full, "bogus"); err == nil {
			t.Fatal("未知基线应报错")
		}
		// 不适用的分支不碰 docker：DockerRunner 直接返回 applicable=false。
		if _, ok, err := (DockerRunner{}).RunBaseline(context.Background(), validitySpec(1), cmdOnly, ArmDoNothing); ok || err != nil {
			t.Fatalf("无容器判分的任务应不适用: %v %v", ok, err)
		}
	})

	// NFR-1 在 decompose 上的口径：整张网格只跑一次 preflight。
	t.Run("decompose 只跑一次 preflight", func(t *testing.T) {
		stub := newStub()
		stub.oracle["t1"] = true
		grid := DecomposeGrid{Profiles: Profiles, Models: []string{"m1", "m2"}}
		rep, err := RunDecompose(context.Background(), grid, validitySpec(2), manifestOf(gradedTask("t1")), stub)
		if err != nil {
			t.Fatal(err)
		}
		if stub.perTask["t1"] != 2 || rep.Preflight == nil || rep.Preflight.Status != PreflightPassed {
			t.Fatalf("3×2 网格应只跑 2 次基线（oracle+do-nothing）: %d %+v", stub.perTask["t1"], rep.Preflight)
		}
		if !strings.Contains(rep.RenderDecomposeMarkdown(), "preflight=passed") {
			t.Fatal("decompose 报告应渲染 preflight 状态")
		}
	})

	// INV-1：只有 command 的 manifest + 不支持基线的 runner，行为与基线一致。
	t.Run("command-suite 行为不变", func(t *testing.T) {
		sc, err := RunBenchmark(context.Background(), validitySpec(2), smokeManifest(t), ExecRunner{})
		if err != nil {
			t.Fatal(err)
		}
		if sc.Preflight.Status != PreflightNA || sc.Pass1.Numerator != 1 || sc.Pass1.Denominator != 2 {
			t.Fatalf("应 n/a 且 pass@1=1/2: %+v %+v", sc.Preflight, sc.Pass1)
		}
	})
}

// TestScorecardLeakage covers REQ-5: the leakage arm's pass rate sits next to
// pass@1, leakage-pass tasks head the review queue, and the queue fills with
// top pass-rate tasks (ties by fewer tokens), never with never-passed tasks.
func TestScorecardLeakage(t *testing.T) {
	stub := newStub()
	t1, t2, t3, t4 := gradedTask("t1"), gradedTask("t2"), gradedTask("t3"), gradedTask("t4")
	t1.LeakageCmd, t2.LeakageCmd = "guess", "guess"
	for _, id := range []string{"t1", "t2", "t3", "t4"} {
		stub.oracle[id] = true
	}
	stub.leakage["t1"] = true
	stub.main["t1"], stub.main["t2"], stub.main["t4"] = true, true, true
	stub.tokens["t2"], stub.tokens["t4"] = 500, 100
	sc, err := RunBenchmark(context.Background(), validitySpec(2), manifestOf(t1, t2, t3, t4), stub)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Leakage.Status != LeakageMeasured || sc.Leakage.Rate.Numerator != 1 || sc.Leakage.Rate.Denominator != 2 {
		t.Fatalf("leakage 应为 measured 1/2: %+v", sc.Leakage)
	}
	if strings.Join(sc.Leakage.PassedTasks, ",") != "t1" {
		t.Fatalf("leakage 通过任务应为 t1: %v", sc.Leakage.PassedTasks)
	}
	var got []string
	for _, r := range sc.ReviewQueue {
		got = append(got, r.TaskID+":"+r.Reason)
	}
	want := "t1:leakage-pass,t4:top-score,t2:top-score"
	if strings.Join(got, ",") != want {
		t.Fatalf("复核清单应为 %s，得到 %s", want, strings.Join(got, ","))
	}
	if !containsLine(sc.HarnessDisclosure, "leakage: measured") {
		t.Fatalf("披露清单缺 leakage 行: %v", sc.HarnessDisclosure)
	}
	data, _ := json.Marshal(sc)
	for _, key := range []string{`"leakage"`, `"review_queue"`, `"preflight"`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("scorecard JSON 缺 %s", key)
		}
	}

	// leakage 臂超时：不进分母，但计数披露（不能被读成"未声明"）。
	stubT := newStub()
	lt := gradedTask("t1")
	lt.LeakageCmd = "guess"
	stubT.oracle["t1"] = true
	stubT.budgetCut[ArmLeakage] = true
	scT, err := RunBenchmark(context.Background(), validitySpec(1), manifestOf(lt), stubT)
	if err != nil {
		t.Fatal(err)
	}
	if scT.Leakage.Status != LeakageUnmeasured || scT.Leakage.TimedOut != 1 {
		t.Fatalf("超时的 leakage 臂应 unmeasured 且 TimedOut=1: %+v", scT.Leakage)
	}

	// 未声明 leakage_cmd → unmeasured，清单只按高分补足且不超过上限。
	stub2 := newStub()
	var tasks []BenchTask
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		tasks = append(tasks, gradedTask(id))
		stub2.oracle[id], stub2.main[id] = true, true
	}
	sc2, err := RunBenchmark(context.Background(), validitySpec(1), manifestOf(tasks...), stub2)
	if err != nil {
		t.Fatal(err)
	}
	if sc2.Leakage.Status != LeakageUnmeasured || len(sc2.ReviewQueue) != reviewQueueSize {
		t.Fatalf("应 unmeasured 且清单 %d 条: %+v %d", reviewQueueSize, sc2.Leakage, len(sc2.ReviewQueue))
	}
}

// TestPerturbationVariance covers REQ-6: ≥3 variants run variant × repeat and
// widen the paired CI with a third variance source; fewer stays unmeasured and
// the dictionary warns the CI may be too narrow.
func TestPerturbationVariance(t *testing.T) {
	stub := newStub()
	spec := validitySpec(2)
	spec.Variants = []string{"v-a", "v-b", "v-c"}
	sc, err := RunBenchmark(context.Background(), spec, manifestOf(BenchTask{ID: "c1", Command: "true"}), stub)
	if err != nil {
		t.Fatal(err)
	}
	if stub.mainCalls != 6 || len(sc.Attempts) != 6 || sc.Perturbation != PerturbationMeasured {
		t.Fatalf("3 变体 × 2 重复应执行 6 次且 measured: calls=%d attempts=%d %s", stub.mainCalls, len(sc.Attempts), sc.Perturbation)
	}
	if strings.Join(stub.variants, ",") != "v-a,v-a,v-b,v-b,v-c,v-c" {
		t.Fatalf("runner 应收到当前变体: %v", stub.variants)
	}
	spec.Variants = spec.Variants[:2]
	if spec.PerturbationStatus() != PerturbationUnmeasured {
		t.Fatal("2 个变体应为 unmeasured")
	}

	// 构造：变体 0 恒过、变体 1/2 恒败（变体效应大、重复内无方差）；b 臂全败。
	var a, aPooled, b []Attempt
	for ti := 0; ti < 10; ti++ {
		id := string(rune('a' + ti))
		for v := 0; v < 3; v++ {
			for r := 1; r <= 4; r++ {
				a = append(a, Attempt{TaskID: id, Repeat: r, Variant: v, Pass: v == 0})
				aPooled = append(aPooled, Attempt{TaskID: id, Repeat: r, Pass: v == 0})
				b = append(b, Attempt{TaskID: id, Repeat: r, Variant: v})
			}
		}
	}
	three, err := PairedDeltaCI(a, b, 4000, 7)
	if err != nil {
		t.Fatal(err)
	}
	two, err := PairedDeltaCI(aPooled, b, 4000, 7)
	if err != nil {
		t.Fatal(err)
	}
	if three.Perturbation != PerturbationMeasured || two.Perturbation != PerturbationUnmeasured {
		t.Fatalf("扰动标注不符: %s / %s", three.Perturbation, two.Perturbation)
	}
	if w3, w2 := three.Hi-three.Lo, two.Hi-two.Lo; w3 < 1.5*w2 {
		t.Fatalf("三源 CI（%.3f）应明显宽于两源（%.3f）", w3, w2)
	}
	if d := three.Delta - 1.0/3; d > 1e-9 || d < -1e-9 {
		t.Fatalf("点估计应为 1/3: %v", three.Delta)
	}
	again, _ := PairedDeltaCI(a, b, 4000, 7)
	if again.Lo != three.Lo || again.Hi != three.Hi {
		t.Fatal("同 seed 结果应确定")
	}
	// 变体层配对：两臂结果完全相同（变体效应共享）时差值恒为 0，区间不得被变体噪声撑宽。
	same, err := PairedDeltaCI(a, a, 2000, 3)
	if err != nil {
		t.Fatal(err)
	}
	if same.SE != 0 || same.Lo != 0 || same.Hi != 0 {
		t.Fatalf("同结果两臂的配对 CI 应退化为 0: %+v", same)
	}
	// 两臂各 3 个变体但只共享 2 个：变体层无法完整配对，退回 unmeasured。
	var pa, pb []Attempt
	for _, v := range []int{0, 1, 2} {
		pa = append(pa, Attempt{TaskID: "t", Variant: v, Pass: true})
		pb = append(pb, Attempt{TaskID: "t", Variant: v + 1})
	}
	part, err := PairedDeltaCI(pa, pb, 200, 1)
	if err != nil || part.Perturbation != PerturbationUnmeasured {
		t.Fatalf("共同变体不足 3 个应为 unmeasured: %+v %v", part, err)
	}
	// NRuns 只数共同任务上的 attempt。
	extra := append(append([]Attempt(nil), a...), Attempt{TaskID: "only-in-a", Pass: true})
	ci, err := PairedDeltaCI(extra, b, 200, 1)
	if err != nil || ci.NRuns != len(b) || ci.NTasks != 10 {
		t.Fatalf("NRuns 应只数共同任务: %+v %v", ci, err)
	}

	dict, err := LoadDictionary(filepath.Join("..", "..", "evals", "forge", "metrics.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := dict.Find("profile_delta")
	if !ok || !strings.Contains(m.MisuseNote, "CI 可能偏窄") || !strings.Contains(m.MisuseNote, "perturbation=unmeasured") {
		t.Fatalf("profile_delta misuse_note 应写明 unmeasured 时 CI 可能偏窄: %+v", m)
	}
}

// TestNullResultMDE covers REQ-7: a CI crossing zero renders the fixed null
// statement with the MDE in percentage points; a clear difference renders the
// interval; decompose uses the statement for every delta.
func TestNullResultMDE(t *testing.T) {
	null := NullResultStatement(&DeltaCI{CrossesZero: true, NRuns: 40, SE: 0.123 / mdeZ, MDE: 0.123})
	if null != "在 n=40 次运行下未检测到显著差异；最小可检测效应 12.3 pp" {
		t.Fatalf("null 结论文本不符: %q", null)
	}
	sig := NullResultStatement(&DeltaCI{Delta: 0.2, Lo: 0.1, Hi: 0.3, SE: 0.05, NRuns: 40})
	if !strings.HasPrefix(sig, "差值 +20.0 pp，95% CI [+10.0, +30.0] pp") {
		t.Fatalf("显著结论文本不符: %q", sig)
	}
	if NullResultStatement(nil) != "区间不可得" {
		t.Fatal("nil CI 应渲染区间不可得")
	}
	// 零方差：不得出"MDE 0.0 pp"式的误导结论（设计 §五：0 误差 = 计算异常告警）。
	zero := NullResultStatement(&DeltaCI{CrossesZero: true, NRuns: 6})
	if strings.Contains(zero, "最小可检测效应") || !strings.Contains(zero, "bootstrap 方差为 0") {
		t.Fatalf("零方差应给专门文案: %q", zero)
	}
	// 两臂同分布（各任务 50% 通过）→ 跨零，MDE = (1.96+0.8416)·SE。
	var a, b []Attempt
	for ti := 0; ti < 8; ti++ {
		id := string(rune('a' + ti))
		for r := 1; r <= 4; r++ {
			a = append(a, Attempt{TaskID: id, Repeat: r, Pass: r%2 == 0})
			b = append(b, Attempt{TaskID: id, Repeat: r, Pass: r%2 == 1})
		}
	}
	ci, err := PairedDeltaCI(a, b, 2000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !ci.CrossesZero || ci.NRuns != 32 || ci.SE <= 0 {
		t.Fatalf("应跨零、n=32、SE>0: %+v", ci)
	}
	if d := ci.MDE - mdeZ*ci.SE; d > 1e-12 || d < -1e-12 {
		t.Fatalf("MDE 应为 2.8×SE: %+v", ci)
	}
	if !strings.Contains(NullResultStatement(ci), "在 n=32 次运行下未检测到显著差异") {
		t.Fatalf("跨零应出 null 结论: %s", NullResultStatement(ci))
	}
	if _, err := PairedDeltaCI(a, []Attempt{{TaskID: "zzz"}}, 10, 1); err == nil {
		t.Fatal("无共同任务应报错")
	}

	grid := DecomposeGrid{Profiles: []Profile{ProfileOff, ProfileFull}, Models: []string{"m1", "m2"}}
	rep, err := RunDecompose(context.Background(), grid, validitySpec(3), manifestOf(BenchTask{ID: "x", Command: "true"}, BenchTask{ID: "y", Command: "true"}), ScriptedRunner{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range rep.DeltaFullOff {
		if d.CI == nil {
			t.Fatalf("decompose 差值应带 CI: %+v", d)
		}
	}
	// 格均值与差值同一估计量（全部 attempt）：首次失败、之后通过的 full 档
	// 格均值是 2/3 而不是 pass@1 的 0，分解与差值在同一份报告里不打架。
	ff, err := RunDecompose(context.Background(), grid, validitySpec(3), manifestOf(BenchTask{ID: "x", Command: "true"}), &firstFailRunner{seen: map[string]int{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ff.CellMeans["full|m1"]; got < 0.666 || got > 0.667 {
		t.Fatalf("full 格均值应为全部 attempt 通过率 2/3，得到 %v", got)
	}
	for _, d := range ff.DeltaFullOff {
		if d.Delta != ff.CellMeans["full|"+d.Model]-ff.CellMeans["off|"+d.Model] {
			t.Fatalf("差值应与格均值同口径: %+v vs %v", d, ff.CellMeans)
		}
	}

	// 网格未含 gates-only：涉及它的两档差值不出区间、也不出点差结论，原因写明。
	for _, d := range append(rep.DeltaFullGates, rep.DeltaGatesOff...) {
		if d.CI != nil || !strings.Contains(d.CIError, "网格未含") {
			t.Fatalf("缺档 profile 的差值应标区间不可得: %+v", d)
		}
	}
	md := rep.RenderDecomposeMarkdown()
	if !strings.Contains(md, "perturbation=unmeasured") || strings.Contains(md, "是否显著以置信区间为准") {
		t.Fatalf("decompose 渲染应标 perturbation 且不再出无区间的点差: %s", md)
	}
	for _, d := range rep.DeltaFullOff {
		if line := "- " + d.Model + ": " + NullResultStatement(d.CI); !strings.Contains(md, line) {
			t.Fatalf("decompose 渲染应逐模型使用 NullResultStatement（缺 %q）: %s", line, md)
		}
		// 点差与区间同一估计量：JSON 的 delta 就是 CI 的点估计。
		if d.Delta != d.CI.Delta {
			t.Fatalf("delta 应等于 CI 点估计: %v vs %v", d.Delta, d.CI.Delta)
		}
	}
}

// TestGoldenTestLayerQueryCount covers REQ-8 / INV-2: the test layer is
// excluded by default, runs only with IncludeTestLayer, and every persisted
// report that includes it bumps the counter and lands one audit row; cases
// without a layer keep their fingerprint byte-identical.
func TestGoldenTestLayerQueryCount(t *testing.T) {
	dir := t.TempDir()
	okBin := writeFakeForgeNamed(t, dir, "fakeforge-layer", "exit 0")
	holdout := goldenCleanCase()
	test := goldenCleanCase()
	test.ID = "c-test"
	test.Layer = GoldenLayerTest
	cases := []GoldenCase{holdout, test}

	rep, err := RunGolden(dir, cases, GoldenOptions{ForgeBin: okBin, Repetitions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Cases) != 1 || rep.TestLayerExcluded != 1 || rep.TestLayerCases != 0 {
		t.Fatalf("默认应排除 test 层: cases=%d excluded=%d", len(rep.Cases), rep.TestLayerExcluded)
	}
	evalDir, root := t.TempDir(), t.TempDir()
	if _, err := PersistGoldenReport(evalDir, root, rep); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(evalDir, "forge", goldenTestQueryFile)
	if _, err := os.Stat(counter); !os.IsNotExist(err) {
		t.Fatalf("不含 test 层的运行不得计数: %v", err)
	}

	for i := 1; i <= 2; i++ {
		rep, err := RunGolden(dir, cases, GoldenOptions{ForgeBin: okBin, Repetitions: 1, IncludeTestLayer: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Cases) != 2 || rep.TestLayerCases != 1 {
			t.Fatalf("--release 应跑 test 层: %+v", rep)
		}
		path, err := PersistGoldenReport(evalDir, root, rep)
		if err != nil {
			t.Fatal(err)
		}
		if rep.TestLayerQueryCount != i {
			t.Fatalf("第 %d 次运行计数应为 %d，得到 %d", i, i, rep.TestLayerQueryCount)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), `"test_layer_query_count"`) {
			t.Fatal("报告文件应写回计数")
		}
	}
	var q goldenTestQueries
	data, err := os.ReadFile(counter)
	if err != nil || json.Unmarshal(data, &q) != nil || q.Count != 2 {
		t.Fatalf("计数文件应为 2: %v %s", err, data)
	}
	rows, _ := checklog.LoadAllAll(root)
	n := 0
	for _, r := range rows {
		if r.Check == checklog.CheckEvalGoldenTestQuery {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("应落 2 条 eval-golden-test-query 行，得到 %d", n)
	}

	// 平台跳过的 test 层用例没有被查询：不计数、不落行。
	skipped := test
	skipped.ID = "c-test-skip"
	skipped.SkipPlatforms = []string{runtime.GOOS}
	srep, err := RunGolden(dir, []GoldenCase{holdout, skipped}, GoldenOptions{ForgeBin: okBin, Repetitions: 1, IncludeTestLayer: true})
	if err != nil {
		t.Fatal(err)
	}
	if srep.TestLayerCases != 0 {
		t.Fatalf("平台跳过的 test 层用例不应计为查询: %d", srep.TestLayerCases)
	}
	if _, err := PersistGoldenReport(evalDir, root, srep); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(counter)
	if json.Unmarshal(data, &q) != nil || q.Count != 2 {
		t.Fatalf("平台跳过的运行不得给计数 +1: %s", data)
	}
	// 计数文件不可读（非 NotExist）：从 0 重新起算，并在 note 里写明。
	badDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(badDir, "forge", goldenTestQueryFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if count, note := recordTestLayerQuery(badDir, &GoldenReport{}); count != 1 || !strings.Contains(note, "已重置") {
		t.Fatalf("不可读计数应重置并说明: %d %q", count, note)
	}

	// INV-2：仓库内已钉的 golden 集（无 layer 字段）指纹不变；换层改变指纹；非法层拒载。
	repoGolden := filepath.Join("..", "..", "evals", "forge", "golden")
	loaded, err := LoadGoldenDir(repoGolden)
	if err != nil {
		t.Fatal(err)
	}
	if GoldenFingerprint(loaded) != LoadGoldenManifest(repoGolden) {
		t.Fatal("无 layer 的存量用例集指纹应与已钉 MANIFEST.sha256 一致")
	}
	moved := holdout
	moved.Layer = GoldenLayerTest
	if GoldenFingerprint([]GoldenCase{moved}) == GoldenFingerprint([]GoldenCase{holdout}) {
		t.Fatal("换层应改变指纹")
	}
	bad := holdout
	bad.Layer = "secret"
	if err := validateGoldenCase(&bad); err == nil {
		t.Fatal("非法 layer 应被拒")
	}
}

// TestJudgePositionSwap covers REQ-9: pairwise verdicts compared by candidate
// identity; a flip rate above 10% is high systematic bias with a finding,
// exactly 10% is not; absent pairs stay -1; malformed pairs fail loading.
func TestJudgePositionSwap(t *testing.T) {
	build := func(flips int) []JudgeAuditEntry {
		var es []JudgeAuditEntry
		for i := 0; i < 10; i++ {
			swapped := "a"
			if i < flips {
				swapped = "b"
			}
			es = append(es, JudgeAuditEntry{DocID: string(rune('a' + i)), JudgeScores: []int{80}, HumanScore: 80, Threshold: 75,
				PairOriginal: "a", PairSwapped: swapped})
		}
		return es
	}
	high, err := RunJudgeAudit(build(2))
	if err != nil {
		t.Fatal(err)
	}
	if high.PositionSwapFlipRate != 0.2 || high.PositionSwapPairs != 10 || high.PositionSwapBias != PositionSwapBiasHigh {
		t.Fatalf("2/10 翻转应判 high: %+v", high)
	}
	if !strings.Contains(strings.Join(high.Findings, "\n"), "high systematic bias") {
		t.Fatalf("应出 high systematic bias finding: %v", high.Findings)
	}
	edge, err := RunJudgeAudit(build(1))
	if err != nil {
		t.Fatal(err)
	}
	if edge.PositionSwapBias != PositionSwapBiasOK || strings.Contains(strings.Join(edge.Findings, "\n"), "high systematic bias") {
		t.Fatalf("1/10（=10%%）不应触发: %+v", edge)
	}
	none, err := RunJudgeAudit([]JudgeAuditEntry{{DocID: "x", JudgeScores: []int{80}, HumanScore: 80, Threshold: 75}})
	if err != nil {
		t.Fatal(err)
	}
	if none.PositionSwapFlipRate != -1 || none.PositionSwapBias != "" {
		t.Fatalf("未提供成对判定应为 -1 未运行: %+v", none)
	}
	good := filepath.Join(t.TempDir(), "good.json")
	if err := os.WriteFile(good, []byte(`[{"doc_id":"x","judge_scores":[80],"human_score":80,"threshold":75,"pair_original":"a","pair_swapped":"tie"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadJudgeScores(good)
	if err != nil || len(loaded) != 1 || loaded[0].PairOriginal != "a" || loaded[0].PairSwapped != "tie" {
		t.Fatalf("合法成对判定应能加载: %v %+v", err, loaded)
	}
	for name, body := range map[string]string{
		"缺一半": `[{"doc_id":"x","judge_scores":[80],"human_score":80,"threshold":75,"pair_original":"a"}]`,
		"非法值": `[{"doc_id":"x","judge_scores":[80],"human_score":80,"threshold":75,"pair_original":"a","pair_swapped":"left"}]`,
	} {
		p := filepath.Join(t.TempDir(), "scores.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadJudgeScores(p); err == nil {
			t.Fatalf("%s 应加载失败", name)
		}
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

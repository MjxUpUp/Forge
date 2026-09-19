package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// mutation_test.go — oracle-pipeline L2 引擎行为钉：扫描/抽样/变异还原的纯
// 函数面 + 小型真实 Go 模块上的端到端（杀灭/存活/证据行/fuzz 预算实跑/edge
// 清单生成/回归测试校验）。

// questioningFixture 建一个带强/弱测试混合的迷你 Go 模块（git 仓库，任务窗口
// 内有源码改动）：Max 有强断言测试（变异必被杀）；Clamp 无断言测试（变异存活
// ——正是假绿形态）。返回 dir 与已保存的 state。
func questioningFixture(t *testing.T, weakOrFuzz bool) (string, *TaskState) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "t@t.com")
	run("git", "config", "user.name", "T")
	files := map[string]string{
		"go.mod": "module q\n\ngo 1.21\n",
		"calc.go": `package q

func Max(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
`,
		"calc_test.go": `package q

import "testing"

func TestMax(t *testing.T) {
	if got := Max(3, 2); got != 3 {
		t.Fatalf("Max(3,2)=%d want 3", got)
	}
	if got := Max(2, 3); got != 3 {
		t.Fatalf("Max(2,3)=%d want 3", got)
	}
}
`,
	}
	if weakOrFuzz {
		// 弱测试（无 Clamp 断言）+ fuzz 不变式（真 Max 恒满足——预算内零发现）。
		files["weak_test.go"] = `package q

import "testing"

func TestWeak(t *testing.T) { _ = Clamp(1, 0, 9) }

func FuzzMax(f *testing.F) {
	f.Add(1, 2)
	f.Fuzz(func(t *testing.T, a, b int) {
		got := Max(a, b)
		if got < a || got < b {
			t.Fatalf("Max(%d,%d)=%d breaks invariant", a, b, got)
		}
	})
}
`
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	head := strings.TrimSpace(func() string {
		out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}())
	// 任务窗口内的工作区改动：calc.go 加一行（diff 命中）。
	if err := os.WriteFile(filepath.Join(dir, "calc.go"), []byte(files["calc.go"]+"\n// task touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &TaskState{TaskRef: "feat/question", Branch: "feat/question", HeadCommit: head}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	return dir, state
}

// TestScanMutationSites: comparison/logic operators are collected with offsets;
// non-flippable operators are not.
//
// TestScanMutationSites：比较/逻辑算子按偏移收集；不可翻转算子不收。
func TestScanMutationSites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "s.go")
	body := "package p\n\nfunc f(a, b int) bool {\n\treturn a >= b && a != 0\n}\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sites, err := ScanMutationSites(src, "s.go")
	if err != nil {
		t.Fatalf(`ScanMutationSites: %v`, err)
	}
	ops := map[string]bool{}
	for _, s := range sites {
		ops[s.Op] = true
		if s.PkgDir != "." {
			t.Errorf(`根包 PkgDir 应为 "."，got %q`, s.PkgDir)
		}
	}
	for _, want := range []string{">=", "&&", "!="} {
		if !ops[want] {
			t.Errorf(`应收集算子 %q（got %v）`, want, ops)
		}
	}
}

// TestSampleMutationSites_Deterministic: stride sampling is order-stable and
// total when n >= len.
//
// TestSampleMutationSites_Deterministic：步进抽样顺序稳定；n≥len 全取。
func TestSampleMutationSites_Deterministic(t *testing.T) {
	sites := []MutationSite{
		{File: "b.go", Offset: 10}, {File: "a.go", Offset: 30}, {File: "a.go", Offset: 5}, {File: "a.go", Offset: 20},
	}
	p1 := SampleMutationSites(append([]MutationSite(nil), sites...), 2)
	p2 := SampleMutationSites(append([]MutationSite(nil), sites...), 2)
	if len(p1) != 2 || len(p2) != 2 {
		t.Fatalf(`抽样应取 2，got %d/%d`, len(p1), len(p2))
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			t.Errorf(`确定性破坏：第 %d 个样本不一致`, i)
		}
	}
	if p1[0].File != "a.go" || p1[0].Offset != 5 {
		t.Errorf(`排序后首样本应为 a.go@5，got %+v`, p1[0])
	}
	all := SampleMutationSites(append([]MutationSite(nil), sites...), 99)
	if len(all) != 4 {
		t.Errorf(`n≥len 应全取，got %d`, len(all))
	}
}

// TestRunMutationSampling_KillsAndSurvives: on the real mini-module — Max's
// flip is killed by the strong test, Clamp's flips survive (fake green made
// visible); the working file is restored byte-identical; the deterministic
// evidence row lands with the verdict meta.
//
// TestRunMutationSampling_KillsAndSurvives：真实迷你模块上——Max 的变异被强
// 测试杀灭、Clamp 的变异存活（假绿现形）；工作文件字节级还原；确定性证据行
// 带 verdict meta 落盘。
func TestRunMutationSampling_KillsAndSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip(`端到端变异实跑需要 go 工具链`)
	}
	dir, state := questioningFixture(t, true)
	before, _ := os.ReadFile(filepath.Join(dir, "calc.go"))

	res, checked, err := RunMutationSampling(dir, state, 3)
	if err != nil {
		t.Fatalf(`RunMutationSampling: %v`, err)
	}
	if !checked {
		t.Fatal(`有改动源文件与位点，Checked 应为 true`)
	}
	if res.Killed == 0 {
		t.Errorf(`强测试应杀灭 Max 的变异（killed=%d）`, res.Killed)
	}
	if res.Survived == 0 {
		t.Errorf(`Clamp 无断言测试，其变异应存活（survived=%d——假绿应现形）`, res.Survived)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "calc.go"))
	if string(before) != string(after) {
		t.Errorf("变异后文件必须字节级还原\nbefore:\n%s\nafter:\n%s", string(before), string(after))
	}
	entries, _ := checklog.LoadForTask(dir, state.TaskRef)
	found := false
	for _, e := range entries {
		if e.Check == CheckNameMutationSampling {
			found = true
			if e.Source != checklog.EvidenceDeterministic {
				t.Errorf(`证据行应为 deterministic，got %q`, e.Source)
			}
			if e.Meta["verdict"] != "has-survivors" {
				t.Errorf(`verdict meta 应为 has-survivors，got %q`, e.Meta["verdict"])
			}
		}
	}
	if !found {
		t.Error(`应落 mutation-sampling 证据行`)
	}
}

// TestMutationZeroSignal_NoFakePass: all-invalid samples (toolchain missing /
// broken window) must yield no-signal — never a deterministic PASS row with
// zero actual verification (review P1-3).
//
// TestMutationZeroSignal_NoFakePass：全无效样本（工具链缺失/窗口构建坏）必须
// 判 no-signal——绝不产出零验证却 deterministic PASS 的假绿行（审查 P1-3）。
func TestMutationZeroSignal_NoFakePass(t *testing.T) {
	res := MutationResult{Sampled: 3, Invalid: 3}
	if got := mutationVerdict(res); got != "no-signal" {
		t.Errorf(`全无效样本 verdict 应为 no-signal，got %q`, got)
	}
	dir := t.TempDir()
	recordMutationRow(dir, "zs", res)
	entries, err := checklog.LoadForTask(dir, "zs")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Check != CheckNameMutationSampling {
			continue
		}
		if e.Passed {
			t.Error(`零有效样本不得落 Passed=true（假绿行）`)
		}
		if e.Meta["verdict"] != "no-signal" {
			t.Errorf(`行 verdict 应为 no-signal，got %q`, e.Meta["verdict"])
		}
		if !strings.Contains(e.Detail, "无验证信号") {
			t.Errorf(`detail 应明示无信号: %q`, e.Detail)
		}
	}
}

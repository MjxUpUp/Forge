package taskpipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// fuzz_test.go — L2b 引擎行为钉（配对 fuzz.go）。

// TestDiscoverFuzzTargets_AndRunFuzz: fuzz targets in changed dirs are
// discovered; a 1s budget run on the invariant-holding target finds nothing and
// lands the deterministic row.
//
// TestDiscoverFuzzTargets_AndRunFuzz：改动目录的 fuzz 目标可发现；1s 预算
// 实跑不变式成立的目标零发现并落确定性行。
func TestDiscoverFuzzTargets_AndRunFuzz(t *testing.T) {
	if testing.Short() {
		t.Skip(`fuzz 实跑需要 go 工具链`)
	}
	dir, state := questioningFixture(t, true)
	targets := DiscoverFuzzTargets(dir, state)
	if len(targets) != 1 || targets[0].Func != "FuzzMax" {
		t.Fatalf(`应发现 FuzzMax，got %+v`, targets)
	}
	res, err := RunFuzz(dir, state, targets, time.Second, 1) // 1s 预算——套件时长纪律；不变式成立即零发现
	if err != nil {
		t.Fatalf(`RunFuzz: %v`, err)
	}
	if len(res.Failed) != 0 {
		// Findings 带退出码分型与子进程输出尾部——macos 2026-09-22 实录：夹具
		// 不变式不可打破仍报 Failed，而输出被吞导致根因不可考。失败必须自证。
		t.Errorf(`不变式成立的目标预算内应零失败: %+v`+"\n"+`findings: %v`, res.Failed, res.Findings)
	}
	entries, _ := checklog.LoadForTask(dir, state.TaskRef)
	found := false
	for _, e := range entries {
		if e.Check == CheckNameFuzzRun && e.Passed {
			found = true
		}
	}
	if !found {
		t.Errorf(`应落 fuzz-run 通过证据行`+"\n"+`findings: %v`, res.Findings)
	}
}

// TestClassifyFuzzCrasher：非零退出的分型唯一判据是 go fuzz 的输出标记
// "Failing input written to"。构建失败/worker 瞬断同样退出码 1 但无标记，
// 不得谎称 crasher（2026-09-22 macos 实录的混报根源）；刻意不做 testdata
// 语料目录兜底——种子语料/历史残留会让目录非空推断误判（审查 P2）。
func TestClassifyFuzzCrasher(t *testing.T) {
	if !classifyFuzzCrasher("Failing input written to testdata/fuzz/FuzzMax/abc123") {
		t.Error("输出标记应判 crasher")
	}
	if classifyFuzzCrasher("go test exits 1: build failed") {
		t.Error("构建失败输出不得判 crasher")
	}
	if classifyFuzzCrasher("fatal error: worker died unexpectedly") {
		t.Error("worker 瞬断输出不得判 crasher")
	}
	if classifyFuzzCrasher("") {
		t.Error("空输出不得判 crasher")
	}
}

// TestRunFuzz_NonCrasherRetry：非 crasher 非零（构建失败/worker 瞬断——macos
// 2026-09-22 实录）重试一次；重试通过计通过并留 findings，重试仍败按非 crasher
// 措辞报；真 crasher（输出带语料标记）不重试——证据即语料，重试只会白烧预算。
func TestRunFuzz_NonCrasherRetry(t *testing.T) {
	// fuzz 子进程被替身注入（runOneFuzzFn），无需 go 工具链——-short 也跑。
	dir, state := questioningFixture(t, true)
	targets := []FuzzTarget{{Pkg: ".", Func: "FuzzMax", File: "./weak_test.go"}}
	restore := runOneFuzzFn
	defer func() { runOneFuzzFn = restore }()

	// 瞬断恢复：首次退出码 1 无语料，重试 0 → 零失败 + findings 提示。
	calls := 0
	runOneFuzzFn = func(root string, t FuzzTarget, d time.Duration) (int, string) {
		calls++
		if calls == 1 {
			return 1, "fatal error: worker died unexpectedly"
		}
		return 0, "pass"
	}
	res, err := RunFuzz(dir, state, targets, time.Second, 1)
	if err != nil {
		t.Fatalf(`RunFuzz: %v`, err)
	}
	if len(res.Failed) != 0 || res.Run != 1 || calls != 2 {
		t.Errorf(`瞬断重试应计通过: failed=%v run=%d calls=%d`, res.Failed, res.Run, calls)
	}
	if len(res.Findings) != 1 || !strings.Contains(res.Findings[0], `重试通过`) {
		t.Errorf(`重试通过应留 findings 提示: %v`, res.Findings)
	}

	// 持续失败：两次都非 crasher → Failed 但措辞不得谎称 crasher。
	calls = 0
	runOneFuzzFn = func(root string, t FuzzTarget, d time.Duration) (int, string) {
		calls++
		return 1, "build failed: syntax error in weak_test.go"
	}
	res, err = RunFuzz(dir, state, targets, time.Second, 1)
	if err != nil {
		t.Fatalf(`RunFuzz: %v`, err)
	}
	if len(res.Failed) != 1 || calls != 2 {
		t.Fatalf(`持续非 crasher 失败: failed=%v calls=%d`, res.Failed, calls)
	}
	if len(res.Findings) != 1 || !strings.Contains(res.Findings[0], `非 crasher`) {
		t.Errorf(`无崩溃语料不得谎称 crasher: %v`, res.Findings)
	}

	// 真 crasher：输出带语料标记 → 立即 Failed 不重试（calls 停在 1）。
	calls = 0
	runOneFuzzFn = func(root string, t FuzzTarget, d time.Duration) (int, string) {
		calls++
		return 1, "Failing input written to testdata/fuzz/FuzzMax/deadbeef"
	}
	res, err = RunFuzz(dir, state, targets, time.Second, 1)
	if err != nil {
		t.Fatalf(`RunFuzz: %v`, err)
	}
	if len(res.Failed) != 1 || calls != 1 {
		t.Errorf(`真 crasher 不重试: failed=%v calls=%d`, res.Failed, calls)
	}
	if len(res.Findings) != 1 || !strings.Contains(res.Findings[0], `发现 crasher`) {
		t.Errorf(`真 crasher 措辞: %v`, res.Findings)
	}
}

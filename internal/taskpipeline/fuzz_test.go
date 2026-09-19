package taskpipeline

import (
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
		t.Errorf(`不变式成立的目标预算内应零失败: %+v`, res.Failed)
	}
	entries, _ := checklog.LoadForTask(dir, state.TaskRef)
	found := false
	for _, e := range entries {
		if e.Check == CheckNameFuzzRun && e.Passed {
			found = true
		}
	}
	if !found {
		t.Error(`应落 fuzz-run 通过证据行`)
	}
}

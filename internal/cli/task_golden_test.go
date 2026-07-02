package cli

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestCollectGoldenFromTask_MetaSource 守卫采集器的 Meta 标注：已评分任务经
// CollectGoldenFromTask 产出的 golden case 须带 source=auto-collected。temp dir 无
// git → GetHeadCommit=="" == state.HeadCommit("") → 无 drift_known（"有 drift" 路径
// 由 chore-fix-ci-cmd-forge fixture 的 collect-golden 端到端覆盖，见 testdata/golden_real）。
func TestCollectGoldenFromTask_MetaSource(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []checklog.CheckName{checklog.CheckAutoCompile, checklog.CheckAssertion} {
		if err := checklog.Record(dir, &checklog.Entry{Check: c, Passed: true, TaskRef: "t-golden"}); err != nil {
			t.Fatalf(`Record %s: %v`, c, err)
		}
	}
	state := &taskpipeline.TaskState{TaskRef: "t-golden", Branch: "feat/x"}
	if err := scoreTask(dir, state); err != nil {
		t.Fatalf(`scoreTask: %v`, err)
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatalf(`SaveTaskState: %v`, err)
	}

	gc, err := CollectGoldenFromTask(dir, "t-golden")
	if err != nil {
		t.Fatalf(`CollectGoldenFromTask: %v`, err)
	}
	if gc.Meta.Source != `auto-collected` {
		t.Errorf(`Meta.Source: got %q, want auto-collected`, gc.Meta.Source)
	}
	if len(gc.Meta.DriftKnown) != 0 {
		t.Errorf(`DriftKnown: got %v, want empty (temp dir has no git, HeadCommit matches)`, gc.Meta.DriftKnown)
	}
	if gc.Expected.Overall <= 0 {
		t.Errorf(`Expected.Overall: got %.2f, want >0`, gc.Expected.Overall)
	}
	if gc.Name != `t-golden` {
		t.Errorf(`Name: got %q, want t-golden`, gc.Name)
	}
}

// TestCollectGoldenFromTask_NotScored 守卫前置条件：未评分任务采集应报错（避免产
// Expected 为零值的废 fixture）。
func TestCollectGoldenFromTask_NotScored(t *testing.T) {
	dir := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: "t-unscored", Branch: "feat/x"}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatalf(`SaveTaskState: %v`, err)
	}
	if _, err := CollectGoldenFromTask(dir, "t-unscored"); err == nil {
		t.Fatal(`CollectGoldenFromTask on unscored task: want error, got nil`)
	}
}

func TestGoldenCaseName(t *testing.T) {
	cases := []struct{ in, want string }{
		{`feat/review-snapshot`, `feat-review-snapshot`},
		{`chore-fix-ci-cmd-forge`, `chore-fix-ci-cmd-forge`},
		{`fix/a/b`, `fix-a-b`},
	}
	for _, c := range cases {
		if got := goldenCaseName(c.in); got != c.want {
			t.Errorf(`goldenCaseName(%q): got %q, want %q`, c.in, got, c.want)
		}
	}
}

package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// task_questioning_test.go — L2 三命令的 CLI 面 + L4 finding resolve 回归前置
// 的行为钉。

// TestTaskQuestioningCmd_NoActiveTask: mutation/fuzz/edgecheck 都要求活跃任务
// （证据要落任务链），cobra 面可解析。
func TestTaskQuestioningCmd_NoActiveTask(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	for _, sub := range []string{"mutation", "fuzz", "edgecheck"} {
		resetQuestioningFlags()
		Root.SetArgs([]string{sub})
		Root.SetOut(nil)
		Root.SetErr(nil)
		err := Root.Execute()
		if err == nil || !strings.Contains(err.Error(), "no active task") {
			t.Errorf(`%s 无活跃任务应报 no active task，got %v`, sub, err)
		}
	}
}

// TestMutationNextStep_BaselineRedNeverAllKilled: a red baseline must not end
// with the all-killed line, even when the green packages were fully killed;
// survivors and the clean case keep their existing guidance.
//
// TestMutationNextStep_BaselineRedNeverAllKilled：基线红时即使绿包全部杀灭，
// 收尾也不得是"全部杀灭"；存活与干净两种情形保持原指引。
func TestMutationNextStep_BaselineRedNeverAllKilled(t *testing.T) {
	cases := []struct {
		name    string
		res     taskpipeline.MutationResult
		want    string
		notWant string
	}{
		{"基线红且绿包全杀", taskpipeline.MutationResult{Sampled: 2, Killed: 2, BaselineRed: []string{"./internal/x"}}, "基线红", "全部杀灭"},
		{"有存活", taskpipeline.MutationResult{Sampled: 3, Killed: 2, Survived: 1}, "补断言", "全部杀灭"},
		{"全部杀灭", taskpipeline.MutationResult{Sampled: 3, Killed: 3}, "全部杀灭", "基线红"},
	}
	for _, c := range cases {
		got := mutationNextStep(c.res)
		if !strings.Contains(got, c.want) {
			t.Errorf(`%s：应含 %q，got %q`, c.name, c.want, got)
		}
		if strings.Contains(got, c.notWant) {
			t.Errorf(`%s：不得含 %q，got %q`, c.name, c.notWant, got)
		}
	}
}

// resetQuestioningFlags 重置 L2/L4 命令族的 flag 残留（同进程多次 Execute）。
func resetQuestioningFlags() {
	for _, cmd := range []*cobra.Command{taskMutationCmd, taskFuzzCmd, taskEdgecheckCmd, taskFindingCmd, taskRegressionCmd} {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			f.Changed = false
			_ = f.Value.Set(f.DefValue)
		})
	}
}

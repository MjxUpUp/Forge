package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

// resetQuestioningFlags 重置 L2/L4 命令族的 flag 残留（同进程多次 Execute）。
func resetQuestioningFlags() {
	for _, cmd := range []*cobra.Command{taskMutationCmd, taskFuzzCmd, taskEdgecheckCmd, taskFindingCmd, taskRegressionCmd} {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			f.Changed = false
			_ = f.Value.Set(f.DefValue)
		})
	}
}

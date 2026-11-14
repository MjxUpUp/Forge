package clitask

import (
	"strings"
	"testing"
)

// setGoalFlags 设置三条互斥 flag，并在用例结束时清空——taskStartCmd 是包级变量，
// 不清会把 flag 状态漏给同包其它用例。
func setGoalFlags(t *testing.T, change, goalFile, goal string) {
	t.Helper()
	for name, val := range map[string]string{"change": change, "goal-file": goalFile, "goal": goal} {
		if err := taskStartCmd.Flags().Set(name, val); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, name := range []string{"change", "goal-file", "goal"} {
			_ = taskStartCmd.Flags().Set(name, "")
		}
	})
}

// TestTaskStart_RegistersGoalContractFlags 钉住两个入口 flag 存在：删任意一个，
// --change/--goal-file 就会静默变成 unknown flag，契约无法钉。
func TestTaskStart_RegistersGoalContractFlags(t *testing.T) {
	for _, name := range []string{"change", "goal-file"} {
		if taskStartCmd.Flags().Lookup(name) == nil {
			t.Fatalf("task start 缺 --%s flag", name)
		}
	}
}

func TestResolveGoalContract_RejectsTwoSources(t *testing.T) {
	setGoalFlags(t, "task-goal-contract", "goal.md", "")
	if _, _, err := resolveGoalContract(taskStartCmd, t.TempDir()); err == nil || !strings.Contains(err.Error(), "只能选一个") {
		t.Fatalf("--change 与 --goal-file 同时给应报互斥，got %v", err)
	}
}

func TestResolveGoalContract_RejectsFreeTextWithContract(t *testing.T) {
	setGoalFlags(t, "", "goal.md", "随手写个目标")
	if _, _, err := resolveGoalContract(taskStartCmd, t.TempDir()); err == nil || !strings.Contains(err.Error(), "互斥") {
		t.Fatalf("--goal 与 --goal-file 应互斥，got %v", err)
	}
}

func TestResolveGoalContract_RejectsPathOutsideRepo(t *testing.T) {
	setGoalFlags(t, "", "../outside.md", "")
	if _, _, err := resolveGoalContract(taskStartCmd, t.TempDir()); err == nil || !strings.Contains(err.Error(), "必须位于仓库内") {
		t.Fatalf("仓外 goal 文件应被拒绝，got %v", err)
	}
}

func TestResolveGoalContract_MissingChangeGoalFails(t *testing.T) {
	setGoalFlags(t, "no-such-change", "", "")
	if _, _, err := resolveGoalContract(taskStartCmd, t.TempDir()); err == nil {
		t.Fatal("change 不存在时读 goal.md 应报错，而不是静默放行")
	}
}
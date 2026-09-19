package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// task_regression_test.go — L4 登记命令与 resolve 硬前置的行为钉（配对 task_regression.go）。

// TestTaskFindingResolve_RequiresRegressionTest: the L4 pre-condition —
// resolve without --with-test/--no-test is refused with guidance; --no-test
// without --note is refused; a valid in-window test file resolves; an
// out-of-window or non-test file is rejected.
//
// TestTaskFindingResolve_RequiresRegressionTest：L4 前置——无 --with-test/
// --no-test 拒绝并指路；--no-test 无 --note 拒绝；窗口内合法测试文件放行；
// 窗口外/非测试文件拒绝。
func TestTaskFindingResolve_RequiresRegressionTest(t *testing.T) {
	dir := setupDeadlockTask(t, nil)
	// 项目锚（setupChainTask 同款）+ chdir：cobra 全链路经 projectroot.Find 从
	// cwd 解析 root，不 chdir 会落到真实仓库上。
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	const taskRef = `feat/deadlock`

	// 登记 finding（显式 ID 便于 resolve）+ 任务窗口内的回归测试文件（untracked）。
	if err := taskpipeline.MutateTaskState(dir, taskRef, func(s *taskpipeline.TaskState) error {
		s.AddFinding(taskpipeline.Finding{ID: "f-r1", Content: "退款金额越界"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "refund_test.go"),
		[]byte("package main\n\nimport \"testing\"\n\nfunc TestRefundBound(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) error {
		resetQuestioningFlags()
		verb := "finding"
		if len(args) > 0 && args[0] == "regression" {
			verb = "regression"
			args = args[1:]
		}
		full := append([]string{verb}, args...)
		Root.SetArgs(full)
		Root.SetOut(nil)
		Root.SetErr(nil)
		return Root.Execute()
	}

	reg := func(args ...string) error { return run(append([]string{"regression"}, args...)...) }

	// 1) 裸 resolve：拒绝并指路 regression 登记。
	if err := run("--resolve", "f-r1", "--ref", taskRef); err == nil || !strings.Contains(err.Error(), "forge task regression") {
		t.Fatalf(`裸 resolve 应被拒并指路 forge task regression: %v`, err)
	}
	// 2) --none 无 --note：拒绝（逃生必须可审计）。
	if err := reg("--finding", "f-r1", "--ref", taskRef, "--none"); err == nil || !strings.Contains(err.Error(), "--note") {
		t.Fatalf(`--none 无理由应被拒: %v`, err)
	}
	// 3) 非测试文件 / 不存在的 finding：拒绝。
	if err := reg("--finding", "f-r1", "--ref", taskRef, "--test", "plain.go"); err == nil || !strings.Contains(err.Error(), "校验") {
		t.Fatalf(`非 _test.go 应被拒: %v`, err)
	}
	if err := reg("--finding", "f-nope", "--ref", taskRef, "--test", "refund_test.go"); err == nil {
		t.Fatal(`不存在的 finding 应被拒`)
	}
	// 4) 合法路径：登记绑定 → resolve 成功。
	_ = captureStdout(t, func() {
		if err := reg("--finding", "f-r1", "--ref", taskRef, "--test", "refund_test.go"); err != nil {
			t.Fatalf(`合法 --test 登记应成功: %v`, err)
		}
	})
	out := captureStdout(t, func() {
		if err := run("--resolve", "f-r1", "--ref", taskRef); err != nil {
			t.Fatalf(`有绑定的 resolve 应放行: %v`, err)
		}
	})
	if !strings.Contains(out, "已标 fixed") {
		t.Errorf(`成功输出应确认 fixed: %q`, out)
	}
	st, _ := taskpipeline.LoadTaskState(dir, taskRef)
	if len(st.Findings) != 1 || st.Findings[0].Status != "fixed" {
		t.Errorf(`finding 应为 fixed: %+v`, st.Findings)
	}
	if st.RegressionTests["f-r1"] != "refund_test.go" {
		t.Errorf(`绑定应落盘: %+v`, st.RegressionTests)
	}
	// 5) --none 带 --note：登记 + resolve 放行 + 落审计行。
	if err := taskpipeline.MutateTaskState(dir, taskRef, func(s *taskpipeline.TaskState) error {
		s.AddFinding(taskpipeline.Finding{ID: "f-r2", Content: "第二个发现"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() {
		if err := reg("--finding", "f-r2", "--ref", taskRef, "--none", "--note", "文档措辞类发现，无可执行回归形态"); err != nil {
			t.Fatalf(`--none --note 登记应成功: %v`, err)
		}
	})
	_ = captureStdout(t, func() {
		if err := run("--resolve", "f-r2", "--ref", taskRef); err != nil {
			t.Fatalf(`none 声明后的 resolve 应放行: %v`, err)
		}
	})
	entries, _ := checklog.LoadForTask(dir, taskRef)
	foundEscape := false
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && checklog.EscapeGateOf(&e) == "finding-regression" {
			foundEscape = true
		}
	}
	if !foundEscape {
		t.Error(`--none 应落 finding-regression 逃生审计行`)
	}
}

// TestTaskRegression_BareNoteRefused（复审 P2-1）：裸 --note（无 --none 无
// --test）不得静默走成 none 逃生——按命令契约报二选一。
func TestTaskRegression_BareNoteRefused(t *testing.T) {
	dir := setupDeadlockTask(t, nil)
	const taskRef = `feat/deadlock`
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.MutateTaskState(dir, taskRef, func(s *taskpipeline.TaskState) error {
		s.AddFinding(taskpipeline.Finding{ID: "f-bn", Content: "x"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	resetQuestioningFlags()
	Root.SetArgs([]string{"regression", "--finding", "f-bn", "--ref", taskRef, "--note", "裸注"})
	Root.SetOut(nil)
	Root.SetErr(nil)
	err := Root.Execute()
	if err == nil || !strings.Contains(err.Error(), "二选一") {
		t.Fatalf(`裸 --note 应报 --test/--none 二选一: %v`, err)
	}
}

package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/pflag"
)

// task_heldoutpool_test.go — L3 池命令的 CLI 面行为钉（配对 task_heldoutpool.go）。

// TestTaskHeldoutPool_CLI 全链：动作互斥 → 沉淀 → 列池（tag 过滤）→ 抽取并入
// 活跃任务（输出断言条数；侧车层级的 taskpipeline 侧钉在 heldoutpool_test.go）。
func TestTaskHeldoutPool_CLI(t *testing.T) {
	dir := setupDeadlockTask(t, nil)
	// 项目锚（cobra 全链路 forgedata 需要项目形态；questioning 测试同款）。
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	setFile := filepath.Join(dir, "money.txt")
	if err := os.WriteFile(setFile, []byte("go test -run TestMoney ./pay/... :: ok\ngo version :: "), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	run := func(args ...string) (string, error) {
		taskHeldoutPoolCmd.Flags().VisitAll(func(f *pflag.Flag) {
			f.Changed = false
			_ = f.Value.Set(f.DefValue)
		})
		full := append([]string{"heldout-pool"}, args...)
		Root.SetArgs(full)
		Root.SetOut(nil)
		Root.SetErr(nil)
		var err error
		out := captureStdout(t, func() { err = Root.Execute() })
		return out, err
	}

	if _, err := run(); err == nil || !strings.Contains(err.Error(), "恰好一个动作") {
		t.Fatalf(`零动作应报互斥: %v`, err)
	}
	if _, err := run("--add", setFile, "--name", "money-bounds", "--tag", "money", "--ref", "feat/deadlock"); err != nil {
		t.Fatalf(`沉淀应成功: %v`, err)
	}
	if _, err := run("--add", setFile, "--name", "money-bounds", "--ref", "feat/deadlock"); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf(`同名沉淀应拒绝（拒绝静默换锚）: %v`, err)
	}
	out, err := run("--list", "--tag", "money", "--ref", "feat/deadlock")
	if err != nil || !strings.Contains(out, "money-bounds") {
		t.Fatalf(`列池应含 money-bounds: %v %q`, err, out)
	}
	// draw+apply：deadlock 任务的 session 环境由 setupDeadlockTask 设置（活跃）。
	out, err = run("--draw", "1", "--apply")
	if err != nil || !strings.Contains(out, "已并入") {
		t.Fatalf(`draw+apply 应并入当前任务: %v %q`, err, out)
	}
	if !strings.Contains(out, "共 2 条") {
		t.Errorf(`并入后侧车应 2 条（输出含条数）: %q`, out)
	}
	// 不带 --apply 的 draw 不写侧车（幂等提示）。
	out, err = run("--draw", "1")
	if err != nil || !strings.Contains(out, "--apply") {
		t.Fatalf(`纯 draw 应提示 --apply: %v %q`, err, out)
	}
	// 侧车确实在册（探针）。
	if held, herr := taskpipeline.HeldoutRegistered(dir, "feat/deadlock"); !held || herr != nil {
		t.Errorf(`--apply 后侧车应在册: (%v, %v)`, held, herr)
	}
}

// TestTaskHeldoutPool_CompletedRejectAndNoTaskList（阶段三复审必须项③）：
// 完成态任务的 --apply 拒绝（唯一防线必须有钉）；无任务状态下 --list 可跑
// （项目级动作解耦，P2-5）。
func TestTaskHeldoutPool_CompletedRejectAndNoTaskList(t *testing.T) {
	dir := setupDeadlockTask(t, nil)
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	setFile := filepath.Join(dir, "money.txt")
	if err := os.WriteFile(setFile, []byte("go version :: "), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	run := func(args ...string) (string, error) {
		taskHeldoutPoolCmd.Flags().VisitAll(func(f *pflag.Flag) {
			f.Changed = false
			_ = f.Value.Set(f.DefValue)
		})
		full := append([]string{"heldout-pool"}, args...)
		Root.SetArgs(full)
		Root.SetOut(nil)
		Root.SetErr(nil)
		var err error
		out := captureStdout(t, func() { err = Root.Execute() })
		return out, err
	}
	// 沉淀一套（带 --ref，项目级动作）。
	if _, err := run("--add", setFile, "--name", "m", "--ref", "feat/deadlock"); err != nil {
		t.Fatalf(`沉淀: %v`, err)
	}
	// 完成态任务的 --apply：拒绝（保留集在交付前定稿）。
	now := time.Now()
	if err := taskpipeline.MutateTaskState(dir, "feat/deadlock", func(s *taskpipeline.TaskState) error {
		s.CompletedAt = &now
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := run("--draw", "1", "--apply", "--ref", "feat/deadlock"); err == nil || !strings.Contains(err.Error(), "已完成") {
		t.Fatalf(`完成态 --apply 应拒绝: %v`, err)
	}
	// 无任务状态下 --list 可跑（P2-5 解耦：清掉活跃引用后仍应成功）。
	if err := taskpipeline.SetActiveTaskRef(dir, `test-session-deadlock`, ""); err != nil {
		t.Fatal(err)
	}
	out, err := run("--list")
	if err != nil || !strings.Contains(out, "m") {
		t.Fatalf(`无任务 --list 应可跑并列出池集: %v %q`, err, out)
	}
}

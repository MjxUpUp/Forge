package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// next_wild_test.go 钉住 vNext P1 两个新命令：forge next（单命令引导——决策表）
// 与 forge task wild（野外申报——落盘契约）。

// TestNextDecision 覆盖决策表全行：无任务（脏/净）、门禁链四步、完成收尾两态。
// nextDecision 是纯函数——git/任务状态的接入由 runNext 负责，此处钉语义。
func TestNextDecision(t *testing.T) {
	withGates := func(gates ...string) *taskpipeline.TaskState {
		st := &taskpipeline.TaskState{TaskRef: "feat/x"}
		for _, g := range gates {
			st.History = append(st.History, taskpipeline.TaskGateResult{Gate: g, Passed: true})
		}
		return st
	}
	completed := func(gates ...string) *taskpipeline.TaskState {
		st := withGates(gates...)
		now := time.Now()
		st.CompletedAt = &now
		return st
	}
	cases := []struct {
		name   string
		branch string
		dirty  bool
		st     *taskpipeline.TaskState
		want   string // Next 的关键子串
	}{
		{"no task + dirty tree", "main", true, nil, "forge task start"},
		{"no task + clean tree", "main", false, nil, "forge status"},
		{"task just started", "feat/x", false, withGates(), "forge task gate task-implement"},
		{"implemented not verified", "feat/x", false, withGates("task-implement"), "forge task verify-acceptance"},
		{"verified not reviewed", "feat/x", false, withGates("task-implement", "task-verify"), "forge review pass"},
		{"reviewed not complete", "feat/x", false, func() *taskpipeline.TaskState {
			st := withGates("task-implement", "task-verify")
			st.ReviewPassed = true
			return st
		}(), "forge task complete"},
		{"all gates on feature branch", "feat/x", false, func() *taskpipeline.TaskState {
			st := withGates("task-implement", "task-verify", "task-complete")
			st.ReviewPassed = true
			return st
		}(), "forge task finish"},
		{"all gates on main", "main", false, func() *taskpipeline.TaskState {
			st := withGates("task-implement", "task-verify", "task-complete")
			st.ReviewPassed = true
			return st
		}(), "forge status"},
		{"completed + dirty → collect first", "main", true, completed(), "forge task start"},
		{"completed + clean", "main", false, completed(), "forge status"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextDecision(c.branch, c.dirty, c.st)
			if !strings.Contains(got.Next, c.want) {
				t.Errorf("nextDecision(%q, dirty=%v) Next = %q, want substring %q (reason: %q)", c.branch, c.dirty, got.Next, c.want, got.Reason)
			}
			if got.Next == "" || got.Reason == "" {
				t.Errorf("Next 与 Reason 恒非空（单命令契约），got %+v", got)
			}
		})
	}
	// 无任务 + 脏树：理由必须提到 wild 出口（INV-1 的合法出口要在引导里可见）。
	got := nextDecision("main", true, nil)
	if !strings.Contains(got.Reason, "forge task wild") {
		t.Errorf("dirty-no-task 引导应指向 wild 申报出口, reason = %q", got.Reason)
	}
}

// TestNextJSONOutput 钉住 --json 契约：{"next","reason","state"} 三字段、next 恰一条
// 命令。在临时 git 仓库（干净树、无任务）上走 runNext 全链路。
func TestNextJSONOutput(t *testing.T) {
	root := newWildGitRepo(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.Flags().Bool("json", true, "")
	if err := runNext(cmd, nil); err != nil {
		t.Fatalf("runNext: %v", err)
	}
	var got nextResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output must be a nextResult JSON, got %q: %v", buf.String(), err)
	}
	if got.Next == "" || got.Reason == "" || got.State == nil {
		t.Errorf("json 契约三字段须齐备, got %+v", got)
	}
	if _, ok := got.State["dirty"]; !ok {
		t.Errorf("state 应含 dirty 键（决策可追溯）, got %v", got.State)
	}
}

// TestTaskWildDeclaresAndCounts E2E：申报两次，断言落盘字段（note/branch/head/
// task_active）与本会话计数输出递增。
func TestTaskWildDeclaresAndCounts(t *testing.T) {
	root := newWildGitRepo(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	run := func(note string) string {
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		if err := runTaskWild(cmd, []string{note}); err != nil {
			t.Fatalf("runTaskWild(%q): %v", note, err)
		}
		return buf.String()
	}
	out1 := run("一次性修复 README 错字")
	out2 := run("紧急热修 CI 脚本")
	if !strings.Contains(out1, "第 1 条") || !strings.Contains(out2, "第 2 条") {
		t.Errorf("会话计数应递增（1→2），got %q / %q", out1, out2)
	}

	data, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(root), "wild", "declarations.jsonl"))
	if err != nil {
		t.Fatalf("wild 申报必须落盘: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("应恰有 2 条申报, got %d lines", len(lines))
	}
	var e wildDeclaration
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("申报行须为合法 JSON: %v", err)
	}
	if e.Note != "一次性修复 README 错字" || e.Branch == "" || e.Branch == "?" || e.TaskActive {
		t.Errorf("申报字段不符: %+v", e)
	}
	if e.Head == "" || e.Head == "?" {
		t.Errorf("Head 应记录提交哈希, got %q", e.Head)
	}
}

// TestTaskWildRequiresNote 空说明必须拒绝（无说明的申报没有审计价值）。
func TestTaskWildRequiresNote(t *testing.T) {
	root := newWildGitRepo(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if err := runTaskWild(&cobra.Command{}, []string{"   "}); err == nil {
		t.Error("空白说明必须被拒绝")
	}
}

// newWildGitRepo 建一个带一次提交的临时 git 仓库 + 隔离数据根 + forge 项目标记
// （.forge/state.json——findProjectRoot 的项目判据，同 newTaskGuardProject）。
func newWildGitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge", "state.json"),
		[]byte(`{"pipeline_version":"2.0","mode":"small"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, cmd...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", cmd, err, out)
		}
	}
	return root
}

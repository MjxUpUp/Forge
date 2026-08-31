package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// next_wild_test.go 钉住 vNext P1 两个新命令：forge next（单命令引导——决策表）
// 与 forge task wild（野外申报——落盘契约）。

// TestNextDecision 覆盖决策表全行：无任务（脏/净）、门禁链逐步（含验收实跑与过门
// 两分、task-complete gate 在 complete 之前——P1 审查 FAIL 修正的核心行）、乱序
// 状态、活跃任务忽略脏树。nextDecision 是纯函数——git/任务状态接入由 runNext
// 负责，此处钉语义。
func TestNextDecision(t *testing.T) {
	withGates := func(gates ...string) *taskpipeline.TaskState {
		st := &taskpipeline.TaskState{TaskRef: "feat/x"}
		for _, g := range gates {
			st.History = append(st.History, taskpipeline.TaskGateResult{Gate: g, Passed: true})
		}
		return st
	}
	reviewed := func(st *taskpipeline.TaskState) *taskpipeline.TaskState {
		st.ReviewPassed = true
		return st
	}
	withAcceptance := func(head string) []taskpipeline.AcceptanceCriterion {
		return []taskpipeline.AcceptanceCriterion{{Run: "go test ./...", AcceptedHeadCommit: head}}
	}
	cases := []struct {
		name   string
		branch string
		dirty  bool
		st     *taskpipeline.TaskState
		want   string // Next 全等或唯一关键子串
		exact  bool   // true=Next 必须全等 want
	}{
		{"no task + dirty tree", "main", true, nil, `forge task start --ref <ref> --branch --title <title>`, true},
		{"no task + clean tree", "main", false, nil, "forge status", true},
		{"task just started", "feat/x", false, withGates(), "forge task gate task-implement", true},
		{"active task ignores dirty tree", "feat/x", true, withGates("task-implement"), "forge task gate task-verify", true},
		{"acceptance pending → run it first", "feat/x", false, func() *taskpipeline.TaskState {
			st := withGates("task-implement")
			st.Acceptance = withAcceptance("")
			return st
		}(), "forge task verify-acceptance", true},
		{"acceptance run → gate verify", "feat/x", false, func() *taskpipeline.TaskState {
			st := withGates("task-implement")
			st.Acceptance = withAcceptance("abc123")
			return st
		}(), "forge task gate task-verify", true},
		{"no acceptance criteria → straight to verify gate", "feat/x", false, withGates("task-implement"), "forge task gate task-verify", true},
		{"verified not reviewed", "feat/x", false, withGates("task-implement", "task-verify"), "forge review pass", true},
		{"reviewed but complete-gate missing → gate first", "feat/x", false, reviewed(withGates("task-implement", "task-verify")), "forge task gate task-complete", true},
		{"three gates + review → complete", "feat/x", false, reviewed(withGates("task-implement", "task-verify", "task-complete")), "forge task complete", true},
		{"out-of-order: complete-gate passed, review missing", "feat/x", false, withGates("task-implement", "task-verify", "task-complete"), "forge review pass", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextDecision(c.branch, c.dirty, c.st)
			if c.exact && got.Next != c.want || !c.exact && !strings.Contains(got.Next, c.want) {
				t.Errorf("nextDecision(%q, dirty=%v) Next = %q, want %q (reason: %q)", c.branch, c.dirty, got.Next, c.want, got.Reason)
			}
			// 单命令契约：Next 不得是 &&/; 复合（P1 审查 MEDIUM）。
			if strings.Contains(got.Next, "&&") || strings.Contains(got.Next, ";") {
				t.Errorf("Next 必须恰一条命令（无复合），got %q", got.Next)
			}
			if got.Reason == "" {
				t.Errorf("Reason 恒非空（单命令契约），got %+v", got)
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
	// 本测试环境无宿主注入的 session id——空会话必须如实标注"本机累计"而非冒称
	// 本会话（P2 收尾 F7：所有匿名申报共享计数，文案不得误导）。
	if !strings.Contains(out1, "本机累计") {
		t.Errorf("空会话计数的 scope 文案应为「本机累计（无会话身份…）」, got %q", out1)
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

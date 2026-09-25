package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/clitask"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_artifacts_test.go 钉住 vNext P3 三段工件与缓冲窗口：intent 追加式、
// checklist 勾选门禁、invariant 声明期校验、窗口超时落违规。

func TestValidateInvariant(t *testing.T) {
	if err := clitask.ValidateInvariant("go build ./..."); err != nil {
		t.Errorf("可执行命令应通过: %v", err)
	}
	if err := clitask.ValidateInvariant("go test ./internal/cli/ -count=1 :: ok"); err != nil {
		t.Errorf("run :: expected 形态应通过: %v", err)
	}
	if err := clitask.ValidateInvariant("代码必须优雅，不允许过度设计"); err == nil {
		t.Error("叙述性约束必须被拒绝（析出段必须映射到可执行 validator）")
	} else if !strings.Contains(err.Error(), "checklist") || !strings.Contains(err.Error(), "intent") {
		t.Errorf("拒绝文案须指引降级到 checklist/intent, got %v", err)
	}
	if err := clitask.ValidateInvariant("   "); err == nil {
		t.Error("空 invariant 必须被拒绝")
	}
}

// seedActiveTask 建一个带活跃任务引用的 fixture（工件命令的作用对象），返回根与任务 ref。
func seedActiveTask(t *testing.T) (string, string) {
	t.Helper()
	root := newWildGitRepo(t)
	ref := "feat/artifact-e2e"
	sid := taskpipeline.CurrentSessionID()
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: ref, Branch: "feat/x", Source: "explicit", Summary: "s", StartedAt: time.Now(), SessionID: sid,
	}); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SetActiveTaskRef(root, sid, ref); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	return root, ref
}

func TestTaskArtifactsIntentAppendOnly(t *testing.T) {
	root, ref := seedActiveTask(t)
	run := func(note string) string {
		var buf strings.Builder
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		if err := clitask.RunIntentCmd(cmd, []string{note}); err != nil {
			t.Fatalf("intent %q: %v", note, err)
		}
		return buf.String()
	}
	out1 := run("为什么做：收口 8-30 事故的执法缺口")
	out2 := run("追加约束：不得引入新依赖")
	if !strings.Contains(out1, "第 1 条") || !strings.Contains(out2, "第 2 条") {
		t.Errorf("intent 应追加计数（1→2）, got %q / %q", out1, out2)
	}
	st, err := taskpipeline.LoadTaskState(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.IntentLog) != 2 || st.IntentLog[1].Text != "追加约束：不得引入新依赖" {
		t.Errorf("落盘 intent 应为 2 条且保序, got %+v", st.IntentLog)
	}
}

func TestTaskArtifactsChecklistLifecycle(t *testing.T) {
	root, ref := seedActiveTask(t)
	add := func(desc string) {
		cmd := &cobra.Command{}
		cmd.SetOut(&strings.Builder{})
		if err := clitask.RunChecklistAdd(cmd, []string{desc}); err != nil {
			t.Fatalf("checklist add %q: %v", desc, err)
		}
	}
	add("补 README 文档行")
	add("补 enforcement 报告字段")

	st, _ := taskpipeline.LoadTaskState(root, ref)
	if n := len(st.UntickedChecklist()); n != 2 {
		t.Fatalf("应 2 项未勾, got %d", n)
	}
	cmd := &cobra.Command{}
	cmd.SetOut(&strings.Builder{})
	if err := clitask.RunChecklistTick(cmd, []string{"1"}); err != nil {
		t.Fatalf("tick #1: %v", err)
	}
	st, _ = taskpipeline.LoadTaskState(root, ref)
	if n := len(st.UntickedChecklist()); n != 1 {
		t.Fatalf("tick 后应剩 1 项未勾, got %d", n)
	}
	cmd2 := &cobra.Command{}
	cmd2.SetOut(&strings.Builder{})
	if err := clitask.RunChecklistDrop(cmd2, []string{"2"}); err != nil {
		t.Fatalf("drop #2: %v", err)
	}
	st, _ = taskpipeline.LoadTaskState(root, ref)
	if n := len(st.UntickedChecklist()); n != 0 {
		t.Fatalf("drop 后应 0 项未勾, got %d", n)
	}
}

// TestChecklistHardGate 钉住 complete 硬门禁：三门禁+审查全绿，仅 checklist 未勾
// → task-complete 必须 BLOCK 且文案指向 tick。
func TestChecklistHardGate(t *testing.T) {
	root, ref := seedActiveTask(t)
	st, err := taskpipeline.LoadTaskState(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"task-implement", "task-verify", "task-complete"} {
		st.History = append(st.History, taskpipeline.TaskGateResult{Gate: g, Passed: true, CompletedAt: time.Now()})
	}
	st.ReviewPassed = true
	st.Checklist = []taskpipeline.ChecklistItem{{ID: 1, Desc: "补文档"}, {ID: 2, Desc: "补测试"}}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}

	_, gerr := taskpipeline.ExecuteTaskGate(root, "task-complete", st)
	if gerr == nil {
		t.Fatal("checklist 未勾全时 task-complete 必须被拒")
	}
	msg := gerr.Error()
	if !strings.Contains(msg, "checklist") || !strings.Contains(msg, "#1") {
		t.Errorf("拒绝文案须指向未勾项与 tick 命令, got %q", msg)
	}

	// 勾完再过：checklist 维度放行（其余门禁语义不在本测试范围——只要错误不再
	// 是 checklist 即证明该门禁闭合）。
	st.Checklist[0].Done = true
	st.Checklist[1].Done = true
	now := time.Now()
	st.Checklist[0].DoneAt = &now
	st.Checklist[1].DoneAt = &now
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	_, gerr2 := taskpipeline.ExecuteTaskGate(root, "task-complete", st)
	if gerr2 != nil && strings.Contains(gerr2.Error(), "checklist") {
		t.Errorf("全勾后 checklist 门禁应放行, got %q", gerr2.Error())
	}
}

// TestTaskGuardGraceWindowExpired 缓冲窗口 E2E：第 2 次编辑开窗，窗口内第 3 次编辑
// （总第 5 次）未补救 → 落 violation 标记 + 一次性超时文案；此前各次保持原谱系。
func TestTaskGuardGraceWindowExpired(t *testing.T) {
	root := newTaskGuardProject(t)
	sess := fmt.Sprintf("claude-window-%d", time.Now().UnixNano())

	var last string
	for i := 1; i <= 5; i++ {
		stdout, _, err := runTaskGuardHookOnce(t, ``, sess)
		if err != nil {
			t.Fatalf("edit #%d must stay allowed (window records, never blocks on non-promoted hosts), got %v", i, err)
		}
		last = stdout
	}
	if !strings.Contains(last, "Grace window expired") || !strings.Contains(last, "违规已记录") {
		t.Errorf("第 5 次编辑应发窗口超时文案, got %q", last)
	}
	markers := filepath.Join(forgedata.DataDirFor(root), "markers")
	found := false
	entries, _ := os.ReadDir(markers)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "forge-taskguard-violation-") {
			found = true
		}
	}
	if !found {
		t.Errorf("violation 标记必须落盘（forge enforcement 消费）, markers=%s", markers)
	}

	// 超时文案一次性：第 6 次编辑静默（违规已记录，不再重复）。
	stdout, _, err := runTaskGuardHookOnce(t, ``, sess)
	if err != nil {
		t.Fatalf("edit #6 must stay allowed, got %v", err)
	}
	if stdout != "" {
		t.Errorf("违规记录后应回到静默计数, got %q", stdout)
	}
}

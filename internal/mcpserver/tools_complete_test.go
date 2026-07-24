package mcpserver

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// taskComplete 单元测试在非 git 的 t.TempDir 下覆盖核心分支：强制 ref、code 未过门禁 BLOCKED、
// generic 跳评分、code 评分完成落盘、任务不存在。code_Completed_WithScore 构造三道门禁全过 + MarkComplete
// 的 state（IsComplete=true），验证评分落盘 + completed。act.Append 在非 forge project 失败 → 进 Warnings
// （不阻断 complete），测试不断言 Warnings 空（环境依赖），只断言完成 + 评分核心契约。

// TestTaskComplete_MissingRef_Blocked：ref 空 → error。complete 是不可逆收口（清 active + 落 Score +
// 写 Act），强制显式 ref 防止 spawn 式编排器误清非预期 active task。与 start/proof 可空 ref 不同。
func TestTaskComplete_MissingRef_Blocked(t *testing.T) {
	root := t.TempDir()
	if _, err := taskCompleteCore(root, taskCompleteInput{Ref: ``}); err == nil {
		t.Fatal("ref 空 complete 应 BLOCKED（强制显式 ref）")
	}
}

// TestTaskComplete_NotComplete_Blocked：code task 无门禁 history（IsComplete=false）→ error BLOCKED，
// 带 missingGates 人话。complete 不在里头硬塞门禁——agent 应先 forge_task_proof 预判，被 BLOCKED 即返工。
func TestTaskComplete_NotComplete_Blocked(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: `feat/nc`, Branch: `feat/nc`}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskCompleteCore(root, taskCompleteInput{Ref: `feat/nc`})
	if err == nil {
		t.Fatal("未过门禁 complete 应 BLOCKED")
	}
	if out.IsComplete {
		t.Error("空 state IsComplete 应 false")
	}
	if !strings.Contains(err.Error(), `Missing gates`) {
		t.Errorf("error 应含 Missing gates 人话，got %q", err.Error())
	}
	if out.Completed {
		t.Error("BLOCKED 时 Completed 应 false（不应清 active/落 Score）")
	}
}

// TestTaskComplete_Generic_Completed：generic（调研/设计/接续）跳过门禁与评分，标 3 门禁 passed +
// MarkComplete + 清 active。IsComplete=true（门禁全标 passed），HasScore=false（不评分）。
func TestTaskComplete_Generic_Completed(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: `feat/g`, Branch: `feat/g`, Kind: taskpipeline.TaskKindGeneric}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskCompleteCore(root, taskCompleteInput{Ref: `feat/g`})
	if err != nil {
		t.Fatalf("generic complete 应无 error，got %v", err)
	}
	if !out.Completed {
		t.Error("generic 应 Completed=true")
	}
	if !out.IsComplete {
		t.Error("generic 标 3 门禁 passed 后 IsComplete 应 true")
	}
	if out.HasScore {
		t.Error("generic 不评分，HasScore 应 false")
	}
	// reload 验证：generic 标门禁 + MarkComplete 落盘（History 含 3 道 + CompletedAt 设）。
	reloaded, rerr := taskpipeline.LoadTaskState(root, `feat/g`)
	if rerr != nil {
		t.Fatalf("reload: %v", rerr)
	}
	if !reloaded.IsComplete() {
		t.Error("reload 后 IsComplete 应 true（门禁已落盘）")
	}
	if reloaded.CompletedAt == nil {
		t.Error("generic MarkComplete 应设 CompletedAt")
	}
	if reloaded.Score != nil {
		t.Error("generic 不应评分（Score 应 nil）")
	}
}

// TestTaskComplete_Code_Completed_WithScore：code task 三道门禁全过 + MarkComplete（IsComplete=true）
// → 评分落盘 + Act + 清 active + completed=true。验证 proof-of-work 闭环终点：评分走下沉的 ScoreTask
// （与 CLI 共用真相源），Score 持久化供 act_query/health/dashboard。
func TestTaskComplete_Code_Completed_WithScore(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: `feat/cc`, Branch: `feat/cc`}
	for _, g := range taskpipeline.DefaultGates() {
		state.RecordGateResult(g.ID, true, ``)
	}
	state.MarkComplete()
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskCompleteCore(root, taskCompleteInput{Ref: `feat/cc`})
	if err != nil {
		t.Fatalf("code complete 应无 error，got %v", err)
	}
	if !out.Completed {
		t.Error("code 过门禁应 Completed=true")
	}
	if !out.IsComplete {
		t.Error("三道门禁全过 IsComplete 应 true")
	}
	if !out.HasScore {
		t.Error("code complete 应评分成功 HasScore=true")
	}
	// reload 验证 Score 落盘（act_query/health 读此）。
	reloaded, rerr := taskpipeline.LoadTaskState(root, `feat/cc`)
	if rerr != nil {
		t.Fatalf("reload: %v", rerr)
	}
	if reloaded.Score == nil {
		t.Fatal("reload 后 Score 应非 nil（评分已落盘）")
	}
	if reloaded.Score.TaskRef != `feat/cc` {
		t.Errorf("Score.TaskRef = %q，want feat/cc", reloaded.Score.TaskRef)
	}
}

// TestTaskComplete_NoTask：ref 不存在 → error（complete 不能凭空完成不存在的 task）。
func TestTaskComplete_NoTask(t *testing.T) {
	root := t.TempDir()
	if _, err := taskCompleteCore(root, taskCompleteInput{Ref: `nonexistent`}); err == nil {
		t.Fatal("不存在的 task ref complete 应报错")
	}
}

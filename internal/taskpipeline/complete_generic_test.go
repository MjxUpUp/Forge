package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestCompleteGeneric pins the sunk-in completion orchestration (2026-09 census
// A1, moved from cli/task_gate.go): a generic-kind task completes with all three
// gates auto-passed (History complete for list/dashboard, no checks executed),
// MarkComplete stamped, and the active-task ref cleared.
//
// TestCompleteGeneric 钉住下沉后的完成编排（2026-09 普查 A1，自
// cli/task_gate.go 迁入）：generic 任务 complete 后三道门禁全部自动通过
// （History 完整供 list/dashboard 显示、不跑任何检查）、MarkComplete 落章、
// active-task ref 清空。harness 提交钩子留在 cli 包装层（会话语义）。
func TestCompleteGeneric(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)

	seed := &TaskState{TaskRef: "feat/generic-demo", Branch: "feat/generic-demo", Kind: TaskKindGeneric, Summary: "调研"}
	if err := SaveTaskState(root, seed); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	if err := CompleteGeneric(root, seed); err != nil {
		t.Fatalf("CompleteGeneric: %v", err)
	}

	got, err := LoadTaskState(root, seed.TaskRef)
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}
	if got == nil || !got.IsComplete() {
		t.Fatalf("generic 任务应被标完成: %+v", got)
	}
	for _, id := range GateIDs() {
		if !got.GatePassed(id) {
			t.Errorf("门禁 %s 应自动通过（generic 秒过语义）", id)
		}
	}
	// active-task ref 清空：ActiveTaskState 不再解析到已完结任务。
	if active, err := ActiveTaskState(root, CurrentSessionID()); err == nil && active != nil && active.TaskRef == seed.TaskRef {
		t.Errorf("complete 后 active-task ref 应清空，仍解析到 %s", active.TaskRef)
	}
	if !strings.HasSuffix(seed.TaskRef, "generic-demo") {
		t.Fatalf("fixture sanity: unexpected ref %q", seed.TaskRef)
	}
}

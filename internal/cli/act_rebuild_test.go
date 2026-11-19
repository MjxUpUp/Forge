package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestActRebuild_RebuildsFromTasks is the regression guard for the DevWorkbench empty-dashboard bug: old projects (all tasks completed before act shipped) have no DataDir/act/conclusions.jsonl — the dashboard only reads conclusions.jsonl and never tasks/*.json, so it shows empty. rebuild must reconstruct conclusions from tasks.
//
// TestActRebuild_RebuildsFromTasks 是 DevWorkbench 空 dashboard 数据 bug 的回归守卫：
// 旧项目（act 上线前完成所有任务）没有 DataDir/act/conclusions.jsonl——dashboard 只读
// conclusions.jsonl 不读 tasks/*.json，故显示空。rebuild 必须从 tasks 重建结论。
func TestActRebuild_RebuildsFromTasks(t *testing.T) {
	tmpDir, p := forgedatatest.RealProject(t)
	if out, _, code := runForge(t, tmpDir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`init: %s`, out)
	}
	// 旧项目前置：init 不应创建 act 目录（结论是 task complete 时才落的）
	if cs, _ := act.LoadAll(p); len(cs) != 0 {
		t.Fatalf(`init 不应产生 conclusions，got %d`, len(cs))
	}
	completed := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	state := &taskpipeline.TaskState{
		TaskRef:     `feat/old-task`,
		SessionID:   `sess-old`,
		Evidence:    &tasktypes.TaskEvidence{Criteria: 3, CriteriaPassed: 3, ReviewRounds: 1},
		CompletedAt: &completed,
	}
	if err := taskpipeline.SaveTaskState(tmpDir, state); err != nil {
		t.Fatalf(`seed task: %v`, err)
	}
	out, _, code := runForge(t, tmpDir, `act`, `rebuild`)
	if code != 0 {
		t.Fatalf(`forge act rebuild exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `重建 1 条结论`) {
		t.Errorf(`输出应含"重建 1 条结论"，got: %s`, out)
	}
	cs, err := act.LoadAll(p)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	if len(cs) != 1 {
		t.Fatalf(`rebuild 后应有 1 条结论，got %d`, len(cs))
	}
	if cs[0].TaskRef != `feat/old-task` {
		t.Errorf(`TaskRef=%q want feat/old-task`, cs[0].TaskRef)
	}
	// 事实清单必须从 TaskState 复原——重建结论的量化面只有事实。
	if cs[0].AcceptanceTotal != 3 || cs[0].AcceptancePass != 3 || cs[0].ReviewRounds != 1 {
		t.Errorf(`结论事实=%d/%d rounds=%d want 3/3/1`, cs[0].AcceptancePass, cs[0].AcceptanceTotal, cs[0].ReviewRounds)
	}
}

// TestActRebuild_SkipsIncomplete verifies filtering: tasks without CompletedAt are skipped (not counted in the rebuild).
//
// TestActRebuild_SkipsIncomplete 验证过滤：没有 CompletedAt 的任务跳过（不计入重建）。
func TestActRebuild_SkipsIncomplete(t *testing.T) {
	tmpDir, p := forgedatatest.RealProject(t)
	runForge(t, tmpDir, `init`, `--mode`, `medium`)
	completed := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := taskpipeline.SaveTaskState(tmpDir, &taskpipeline.TaskState{
		TaskRef:     `feat/completed`,
		Evidence:    &tasktypes.TaskEvidence{Criteria: 2, CriteriaPassed: 2},
		CompletedAt: &completed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SaveTaskState(tmpDir, &taskpipeline.TaskState{
		TaskRef: `feat/incomplete`, // 无 CompletedAt
	}); err != nil {
		t.Fatal(err)
	}
	out, _, code := runForge(t, tmpDir, `act`, `rebuild`)
	if code != 0 {
		t.Fatalf(`exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `跳过 1 个未完成`) {
		t.Errorf(`应跳过 1 个未完成，got: %s`, out)
	}
	cs, _ := act.LoadAll(p)
	if len(cs) != 1 || cs[0].TaskRef != `feat/completed` {
		t.Errorf(`只应重建 completed，got %d 条: %+v`, len(cs), cs)
	}
}

// TestActRebuild_BacksUpExisting verifies that existing conclusions are backed up rather than discarded when present: rebuild is a full reconstruction, old data is moved to .bak.
//
// TestActRebuild_BacksUpExisting 验证有旧结论时备份不丢弃：rebuild 是全量重建，旧数据移到 .bak。
func TestActRebuild_BacksUpExisting(t *testing.T) {
	tmpDir, p := forgedatatest.RealProject(t)
	runForge(t, tmpDir, `init`, `--mode`, `medium`)
	old := act.Conclusion{TaskRef: `feat/legacy`, Strength: `Strong`, CompletedAt: time.Now()}
	if err := act.Append(p, &old); err != nil {
		t.Fatal(err)
	}
	completed := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := taskpipeline.SaveTaskState(tmpDir, &taskpipeline.TaskState{
		TaskRef:     `feat/rebuilt`,
		Evidence:    &tasktypes.TaskEvidence{Criteria: 1, CriteriaPassed: 1},
		CompletedAt: &completed,
	}); err != nil {
		t.Fatal(err)
	}
	out, _, code := runForge(t, tmpDir, `act`, `rebuild`)
	if code != 0 {
		t.Fatalf(`exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `已备份原 conclusions.jsonl`) {
		t.Errorf(`有旧结论应备份，got: %s`, out)
	}
	// rebuild 后 conclusions.jsonl 只含从 task 重建的（旧的已 Rename 移到 .bak）
	cs, _ := act.LoadAll(p)
	if len(cs) != 1 || cs[0].TaskRef != `feat/rebuilt` {
		t.Errorf(`rebuild 后应只剩 task 重建的，got %d: %+v`, len(cs), cs)
	}
}

// TestActRebuild_HalfCompleteSkipped covers the real-world shape of "acceptance recorded but complete failed midway": Evidence filled but CompletedAt == nil — such a task never truly finished, its conclusion is meaningless, and it must be skipped.
//
// TestActRebuild_HalfCompleteSkipped 覆盖"验收事实已记但 complete 中途失败"的真实形态：
// Evidence 有值但 CompletedAt == nil —— 这种任务没真正完成，结论无意义，必须跳过。
// 与 SkipsIncomplete（完全无字段）互补，钉死过滤逻辑只看 CompletedAt。
func TestActRebuild_HalfCompleteSkipped(t *testing.T) {
	tmpDir, p := forgedatatest.RealProject(t)
	runForge(t, tmpDir, `init`, `--mode`, `medium`)
	completed := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	// 真正完成的任务（应重建）
	if err := taskpipeline.SaveTaskState(tmpDir, &taskpipeline.TaskState{
		TaskRef:     `feat/done`,
		Evidence:    &tasktypes.TaskEvidence{Criteria: 2, CriteriaPassed: 2},
		CompletedAt: &completed,
	}); err != nil {
		t.Fatal(err)
	}
	// 验收事实已记但未 complete（应跳过——task-complete 门禁没过）
	if err := taskpipeline.SaveTaskState(tmpDir, &taskpipeline.TaskState{
		TaskRef:  `feat/verified-not-completed`,
		Evidence: &tasktypes.TaskEvidence{Criteria: 2, CriteriaPassed: 1},
		// 无 CompletedAt
	}); err != nil {
		t.Fatal(err)
	}
	out, _, code := runForge(t, tmpDir, `act`, `rebuild`)
	if code != 0 {
		t.Fatalf(`exit %d: %s`, code, out)
	}
	cs, _ := act.LoadAll(p)
	if len(cs) != 1 || cs[0].TaskRef != `feat/done` {
		t.Errorf(`应只重建 done，半完成（有 Evidence 无 CompletedAt）必须跳过，got %d: %+v`, len(cs), cs)
	}
}

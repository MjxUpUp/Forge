package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestTaskScope_E2E_FlagToStatusToAddToShow pins the PlanScope user path end-to-end:
// task start --scope (multiple entries, verifying StringArray is not split) → task status display → task scope add
// mid-flight append (with dedup) → task scope show prints the declaration + no drift (no source changes in the temp dir).
// Covers the full chain of cobra flag binding + persistence + subcommand registration + rendering; the drift calculation itself is
// pinned by the pure-function tests in scope_test.go (here we only verify the "no changes → no drift" path does not crash).
//
// TestTaskScope_E2E_FlagToStatusToAddToShow 端到端钉住 PlanScope 用户路径：
// task start --scope（多条，验证 StringArray 不被切）→ task status 展示 → task scope add
// 中途追加（去重）→ task scope show 打印声明 + 无 drift（临时目录无源码改动）。
// 覆盖 cobra flag 绑定 + 持久化 + 子命令注册 + 渲染的完整链路；drift 计算本身由
// scope_test.go 的纯函数测试钉住（这里只验"无改动→无 drift"路径不崩）。
func TestTaskScope_E2E_FlagToStatusToAddToShow(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-scope`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}

	// Two --scope entries (StringArray: kept whole, not split).
	//
	// 两条 --scope（StringArray：整条不切）。
	startOut, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/scope-e2e`,
		`--scope`, `internal/cli/task.go`,
		`--scope`, `internal/cli/scope.go`)
	if code != 0 {
		t.Fatalf(`task start --scope failed: %s`, startOut)
	}
	if !strings.Contains(startOut, `计划改动白名单`) {
		t.Errorf(`task start 输出缺 PlanScope 块: %s`, startOut)
	}
	if !strings.Contains(startOut, `internal/cli/task.go`) {
		t.Errorf(`task start 输出缺 scope 条目: %s`, startOut)
	}

	// status: displays the declared whitelist.
	//
	// status：展示声明的白名单。
	statusOut, _, code := runForge(t, dir, `task`, `status`)
	if code != 0 {
		t.Fatalf(`task status failed: %s`, statusOut)
	}
	if !strings.Contains(statusOut, `计划改动白名单`) {
		t.Errorf(`status 缺 PlanScope 块: %s`, statusOut)
	}

	// scope add: append one entry mid-flight (verifies layered positioning — the plan is iterable), and dedupe one already-present entry.
	//
	// scope add：中途追加一条（验证分层定位——规划可迭代），去重一条已存在的。
	addOut, _, code := runForge(t, dir, `task`, `scope`, `add`, `internal/cli/hook.go`, `internal/cli/task.go`)
	if code != 0 {
		t.Fatalf(`task scope add failed: %s`, addOut)
	}
	// Now 3 entries in total (task/scope/hook); this run added 1 (task.go is deduped, not counted).
	//
	// 现共 3 条（task/scope/hook），本次新增 1（task.go 去重不计）。
	if !strings.Contains(addOut, `现共 3 条`) {
		t.Errorf(`scope add 去重计数错: %s`, addOut)
	}
	if !strings.Contains(addOut, `本次新增 1`) {
		t.Errorf(`scope add 新增计数错（应去重 task.go）: %s`, addOut)
	}

	// scope show: prints 3 declared entries + no drift (no source changes in the temp dir).
	//
	// scope show：打印声明 3 条 + 无 drift（临时目录无源码改动）。
	showOut, _, code := runForge(t, dir, `task`, `scope`, `show`)
	if code != 0 {
		t.Fatalf(`task scope show failed: %s`, showOut)
	}
	if !strings.Contains(showOut, `声明态`) {
		t.Errorf(`scope show 缺声明态标签: %s`, showOut)
	}
	if !strings.Contains(showOut, `scope-drift: 无`) {
		t.Errorf(`无源码改动时应显示"无 drift": %s`, showOut)
	}

	// Persistence: reload confirms PlanScope is persisted with 3 entries.
	//
	// 持久化：reload 确认 PlanScope 落盘 3 条。
	loaded, err := taskpipeline.LoadTaskState(dir, `feat/scope-e2e`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if len(loaded.PlanScope) != 3 {
		t.Errorf(`PlanScope 落盘 %d 条，want 3: %v`, len(loaded.PlanScope), loaded.PlanScope)
	}
}

// TestTaskScope_ShowNoActiveTask verifies that scope show errors out (non-nil exit) when there is no active task, without crashing.
//
// TestTaskScope_ShowNoActiveTask 无活动任务时 scope show 应报错退出（非 nil），不崩。
func TestTaskScope_ShowNoActiveTask(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-scope-none`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}
	_, _, code := runForge(t, dir, `task`, `scope`, `show`)
	if code == 0 {
		t.Errorf(`无活动任务时 scope show 应非 0 退出`)
	}
}

// TestTaskScope_ShowEmptyScope verifies that when a task is declared but no scope is declared, show reports it as empty (without computing drift).
//
// TestTaskScope_ShowEmptyScope 声明了任务但未声明 scope 时，show 提示空（不检测 drift）。
func TestTaskScope_ShowEmptyScope(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-scope-empty`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}
	if _, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/no-scope`); code != 0 {
		t.Fatal(`task start failed`)
	}
	showOut, _, code := runForge(t, dir, `task`, `scope`, `show`)
	if code != 0 {
		t.Fatalf(`scope show 应 exit 0（空 scope 是合法态）: %s`, showOut)
	}
	if !strings.Contains(showOut, `PlanScope: 空`) {
		t.Errorf(`空 scope 应提示"PlanScope: 空": %s`, showOut)
	}
}

package clitask

// task_mine_test.go —— 工作方视角的 `task mine` 视图（空 JSON 形态、--blocked
// 待交付依赖、--all-projects 分组、僵尸标注、已完成但悬置 offered 的渲染
// reconcile）及其进程内 adviser（未认领分派 advisory、跨仓依赖标注）。
// 自 task_assignment_test.go 按域拆分时迁入。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestTaskMine_EmptyReturnsArray: mine with no matching delegations returns a JSON array (never nil/null) so downstream JSON consumers do not need a null special-case.
//
// TestTaskMine_EmptyReturnsArray：mine 无匹配分派时返回 JSON 数组（绝非 nil/null），
// 下游 JSON 消费者无需 null 特例处理。
func TestTaskMine_EmptyReturnsArray(t *testing.T) {
	dir := setupDelegateProject(t)
	stdout, _, code := runForge(t, dir, "task", "mine", "--agent", "reasonix", "--json")
	if code != 0 {
		t.Fatalf("mine on no delegations should succeed, got exit %d: %s", code, stdout)
	}
	trimmed := strings.TrimSpace(stdout)
	if trimmed != `[]` {
		t.Errorf("mine with no matches should print exactly [], got: %q", trimmed)
	}
}

// TestTaskMine_BlockedShowsPendingDeps: mine --blocked lists only tasks whose DependsOn is not fully delivered, with the pending upstream refs in pending_deps.
//
// TestTaskMine_BlockedShowsPendingDeps：mine --blocked 只列 DependsOn 未全交付的 task，pending_deps 带未
// 交付上游 ref。上游交付后该 task 退出 --blocked。这是工作方视角看与门禁相同的 PendingDependencies——
// mine 与门禁对「阻塞」不可能不一致。
func TestTaskMine_BlockedShowsPendingDeps(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--title`, `上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/up`, `--assignee`, `kimi`, `--role`, `frontend`, `--title`, `下游`)

	out, _, code := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--blocked`, `--json`)
	if code != 0 {
		t.Fatalf(`mine --blocked exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `feat/down`) {
		t.Errorf(`mine --blocked 应含被阻塞的 feat/down, got: %s`, out)
	}
	if !strings.Contains(out, `pending_deps`) || !strings.Contains(out, `feat/up`) {
		t.Errorf(`mine --blocked JSON 应含 pending_deps=[feat/up], got: %s`, out)
	}

	// 上游交付后 feat/down 不再 blocked，应从 --blocked 结果消失
	runForge(t, dir, `task`, `assign`, `--ref`, `feat/up`, `--to`, `kimi`, `--by`, `claude-code`)
	runForge(t, dir, `task`, `claim`, `--ref`, `feat/up`, `--as`, `kimi`)
	runForge(t, dir, `task`, `deliver`, `--ref`, `feat/up`)
	out, _, code = runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--blocked`, `--json`)
	if code != 0 {
		t.Fatalf(`mine --blocked after deliver exit %d: %s`, code, out)
	}
	if strings.Contains(out, `feat/down`) {
		t.Errorf(`上游交付后 feat/down 不再 blocked，应退出 --blocked 结果, got: %s`, out)
	}
}

// TestTaskMine_BlockedAnnotatesDepStatus: mine --blocked's pending_dep_detail annotates each pending
// upstream with its collaboration status + gate progress (design §4: "卡在 feat/backend[claimed, 进度 60%]").
// The upstream is assigned+claimed (no gate passed) so it is still pending (delivered is the unblock
// signal) and its detail reads [claimed, 0/3].
//
// TestTaskMine_BlockedAnnotatesDepStatus：mine --blocked 的 pending_dep_detail 为每条待交上游标注协作
// 状态 + 门禁进度（设计§4：「卡在 feat/backend[claimed, 进度 60%]」）。上游被分派+认领（无门禁通过）故
// 仍 pending（delivered 才放行），其 detail 读作 [claimed, 0/3]。
func TestTaskMine_BlockedAnnotatesDepStatus(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--assignee`, `kimi`, `--title`, `上游`)
	runForge(t, dir, `task`, `claim`, `--ref`, `feat/up`, `--as`, `kimi`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/up`, `--assignee`, `kimi`, `--title`, `下游`)

	out, _, code := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--blocked`, `--json`)
	if code != 0 {
		t.Fatalf(`mine --blocked exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `pending_dep_detail`) {
		t.Errorf(`应含 pending_dep_detail（设计§4 状态/进度标注）, got: %s`, out)
	}
	if !strings.Contains(out, `"status": "claimed"`) {
		t.Errorf(`上游 claimed，pending_dep_detail.status 应为 claimed, got: %s`, out)
	}
	if !strings.Contains(out, `"gate_passed": 0`) || !strings.Contains(out, `"gate_total": 3`) {
		t.Errorf(`上游无门禁通过，应 gate_passed=0/gate_total=3, got: %s`, out)
	}
}

// TestTaskMine_AllProjects: --all-projects scans every registered project (design §8) and groups by project.
//
// TestTaskMine_AllProjects：--all-projects 扫描每个已登记 project（设计§8）并按 project 分组。两个
// project 各分派给 kimi；全局视图列两组带 project 标签且绝不自动 resume。共享 FORGE_DATA_HOME 使两次
// forge init 的注册落进同一 registry。
func TestTaskMine_AllProjects(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "allproj-sid")
	makeProject := func(ref string) string {
		d := t.TempDir()
		initGitProject(t, d)
		runForge(t, d, "task", "start", "--ref", ref, "--assignee", "kimi", "--title", ref)
		return d
	}
	dirA := makeProject("feat/a")
	makeProject("feat/b")

	out, _, code := runForge(t, dirA, "task", "mine", "--agent", "kimi", "--all-projects", "--json")
	if code != 0 {
		t.Fatalf(`mine --all-projects exit %d: %s`, code, out)
	}
	if !strings.Contains(out, "projects") {
		t.Errorf(`--all-projects JSON 应含 projects 分组, got: %s`, out)
	}
	if !strings.Contains(out, "feat/a") || !strings.Contains(out, "feat/b") {
		t.Errorf(`--all-projects 应跨 project 列出 feat/a + feat/b, got: %s`, out)
	}
}

// TestTaskMine_MultipleDepsPendingList: with two upstream deps where one is delivered and the other is not, mine --blocked must list only the still-pending one in pending_deps — the delivered upstream must not appear.
//
// TestTaskMine_MultipleDepsPendingList：两个上游依赖中一个已交付一个未交付时，mine --blocked 的
// pending_deps 必须只列仍未交付的那个——已交付的上游不应出现。守卫 PendingDependencies 不误报已交付 ref。
func TestTaskMine_MultipleDepsPendingList(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/a`, `--title`, `A`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/b`, `--title`, `B`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/a`, `--depends-on`, `feat/b`, `--assignee`, `kimi`, `--title`, `下游`)
	// 交付 A，B 仍 pending
	runForge(t, dir, `task`, `assign`, `--ref`, `feat/a`, `--to`, `kimi`, `--by`, `claude-code`)
	runForge(t, dir, `task`, `claim`, `--ref`, `feat/a`, `--as`, `kimi`)
	runForge(t, dir, `task`, `deliver`, `--ref`, `feat/a`)
	out, _, code := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--blocked`, `--json`)
	if code != 0 {
		t.Fatalf(`mine --blocked exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `feat/down`) {
		t.Errorf(`应含被阻塞的 feat/down, got: %s`, out)
	}
	if !strings.Contains(out, `feat/b`) {
		t.Errorf(`pending_deps 应含未交付的 feat/b, got: %s`, out)
	}
	if strings.Contains(out, `feat/a`) {
		t.Errorf(`已交付的 feat/a 不应出现在 mine 输出/pending_deps, got: %s`, out)
	}
}

// TestTaskMine_AnnotatesZombie: a delegation that has stalled (offered>7d here) is flagged on the
// mine row — the worker-facing surface of the same zombie signal `task health` and the dashboard
// render (design §12 标黄). Asserts both the JSON is_zombie field and the human ⚠ marker, and that a
// fresh task is NOT flagged. The stale offer is seeded via the package-shared saveOfferedAgo helper
// (the CLI stamps now, so an 8-days-ago offer can only be set in-process).
//
// TestTaskMine_AnnotatesZombie：停滞的分派（此处 offered>7d）在 mine 行被标记——工作方视角看与
// task health / 看板同一僵尸信号（设计 §12 标黄）。断言 JSON is_zombie 字段 + 人类 ⚠ 标记两者，
// 且刚 offered 的任务不被标记。陈旧 offered 经包内共享 saveOfferedAgo 助手种入（CLI 盖当前时间，
// 8 天前的 offered 只能进程内设置）。
func TestTaskMine_AnnotatesZombie(t *testing.T) {
	dir := setupDelegateProject(t)
	// Stalled: offered 8 days ago → offered>7d zombie.
	saveOfferedAgo(t, dir, `feat/stalled`, `kimi`, time.Now().Add(-8*24*time.Hour))
	// Fresh: offered just now → not a zombie (negative control).
	saveOfferedAgo(t, dir, `feat/fresh`, `kimi`, time.Now())

	t.Run(`json carries is_zombie for stalled only`, func(t *testing.T) {
		out, _, code := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--json`)
		if code != 0 {
			t.Fatalf(`mine --json exit %d: %s`, code, out)
		}
		var rows []delegatedEntry
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf(`解析 mine JSON 失败: %v`+"\n"+`输出: %s`, err, out)
		}
		byRef := map[string]delegatedEntry{}
		for _, r := range rows {
			byRef[r.Ref] = r
		}
		stalled, ok := byRef[`feat/stalled`]
		if !ok {
			t.Fatalf(`feat/stalled 应在 mine 结果中, got %+v`, byRef)
		}
		if !stalled.IsZombie || len(stalled.ZombieReasons) == 0 || stalled.ZombieReasons[0] != `offered>7d` {
			t.Errorf(`feat/stalled 应 is_zombie 且 reason=offered>7d, got %+v`, stalled)
		}
		if fresh, ok := byRef[`feat/fresh`]; ok && fresh.IsZombie {
			t.Errorf(`feat/fresh 刚 offered 不应标僵尸, got %+v`, fresh)
		}
	})

	t.Run(`text shows marker for stalled`, func(t *testing.T) {
		out, _, code := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`)
		if code != 0 {
			t.Fatalf(`mine exit %d: %s`, code, out)
		}
		// Both rows appear; the stalled one carries the ⚠僵尸(offered>7d) marker.
		idxStalled := strings.Index(out, `feat/stalled`)
		if idxStalled < 0 {
			t.Fatalf(`应含 feat/stalled 行, got:`+"\n"+`%s`, out)
		}
		// The marker must be on the stalled row — find the next newline after feat/stalled and check
		// the marker is within that line (not on the fresh row).
		line := out[idxStalled:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, `⚠僵尸`) || !strings.Contains(line, `offered>7d`) {
			t.Errorf(`feat/stalled 行应含 ⚠僵尸(offered>7d) 标记, got 行: %q`, line)
		}
	})
}

// saveCompletedOffered writes a task whose gates ALL passed (IsComplete) but whose assignment
// is still suspended at offered — the exact 脱节 shape dogfooded on 2026-08-17~18 (the worker
// finished the pipeline without ever running claim/deliver, and pre-fix MarkComplete did not
// reconcile the assignment). Used to pin mine's render-reconcile.
//
// saveCompletedOffered 写一个门禁全过（IsComplete）但分派仍悬置在 offered 的任务——正是
// 2026-08-17~18 dogfood 的脱节形态（执行方走完管线却从未 claim/deliver，修复前的
// MarkComplete 也不回收分派）。用于钉住 mine 的渲染 reconcile。
func saveCompletedOffered(t *testing.T, dir, ref, agent string) {
	t.Helper()
	seedTaskState(t, dir, ref, func(s *taskpipeline.TaskState) {
		s.Summary = ref + ` 任务`
		if err := s.AssignTo(agent, `backend`, `claude-code`); err != nil {
			t.Fatalf(`AssignTo %s: %v`, ref, err)
		}
		for _, g := range taskpipeline.DefaultGates() {
			s.RecordGateResult(g.ID, true, ``)
		}
	})
}

// TestMineRendersCompletedNotOffered pins the P1 render-reconcile: a completed task whose
// assignment is suspended at offered must render as `complete` in mine (both JSON and text),
// NOT as 待认领 — a finished task shown as pending forever is the exact 2026-08-18 dogfood
// symptom. The row is kept (not filtered) to preserve visibility. A genuinely in-flight
// offered task still renders `offered` (control).
//
// TestMineRendersCompletedNotOffered 钉住 P1 渲染 reconcile：已完成但分派悬置 offered 的
// 任务在 mine 中（JSON 与 text 两形态）必须渲染为 `complete` 而非待认领——已完成任务永久
// 显示成待办正是 2026-08-18 dogfood 的症状。行保留（不过滤）以保留可见性。真正在途的
// offered 任务仍渲染 `offered`（对照）。
func TestMineRendersCompletedNotOffered(t *testing.T) {
	dir := setupDelegateProject(t)
	saveCompletedOffered(t, dir, `feat/done-suspended`, `kimi`)
	saveOfferedAgo(t, dir, `feat/still-pending`, `kimi`, time.Now())
	// Reopen 对照（review M1）：门禁全过 + delivered + Reopen → 带返工理由的 claimed。
	// IsComplete() 跨 reopen 按设计仍为 true，但 mine 必须渲染真实协作状态（claimed）而非
	// `complete`——卡住的返工不得伪装成已完成。
	{
		s := &taskpipeline.TaskState{TaskRef: `feat/reopened`, Summary: `reopened 任务`}
		if err := s.AssignTo(`kimi`, `backend`, `claude-code`); err != nil {
			t.Fatalf(`AssignTo feat/reopened: %v`, err)
		}
		if err := s.Claim(`kimi`); err != nil {
			t.Fatalf(`Claim feat/reopened: %v`, err)
		}
		if err := s.Deliver(); err != nil {
			t.Fatalf(`Deliver feat/reopened: %v`, err)
		}
		for _, g := range taskpipeline.DefaultGates() {
			s.RecordGateResult(g.ID, true, ``)
		}
		if err := s.Reopen(`交付后发现 bug`); err != nil {
			t.Fatalf(`Reopen feat/reopened: %v`, err)
		}
		if err := taskpipeline.SaveTaskState(dir, s); err != nil {
			t.Fatalf(`SaveTaskState feat/reopened: %v`, err)
		}
	}

	t.Run(`json status is complete`, func(t *testing.T) {
		out, _, code := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--json`)
		if code != 0 {
			t.Fatalf(`mine --json exit %d: %s`, code, out)
		}
		var rows []delegatedEntry
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf(`解析 mine JSON 失败: %v`+"\n"+`输出: %s`, err, out)
		}
		byRef := map[string]delegatedEntry{}
		for _, r := range rows {
			byRef[r.Ref] = r
		}
		done, ok := byRef[`feat/done-suspended`]
		if !ok {
			t.Fatalf(`已完成任务应保留在 mine 结果中（不静默过滤）, got %+v`, byRef)
		}
		if done.Status != `complete` {
			t.Errorf(`已完成且分派悬置的任务 status 应渲染 complete, got %q`, done.Status)
		}
		if done.IsZombie {
			t.Errorf(`已完成任务不应带僵尸标注, got %+v`, done)
		}
		if pending, ok := byRef[`feat/still-pending`]; !ok || pending.Status != `offered` {
			t.Errorf(`在途对照任务应仍渲染 offered, got %+v`, pending)
		}
		if reopened, ok := byRef[`feat/reopened`]; !ok || reopened.Status != `claimed` {
			t.Errorf(`reopen 返工中的任务应渲染真实状态 claimed 而非 complete, got %+v`, reopened)
		}
	})

	t.Run(`text renders complete not offered`, func(t *testing.T) {
		out, _, code := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`)
		if code != 0 {
			t.Fatalf(`mine exit %d: %s`, code, out)
		}
		idx := strings.Index(out, `[feat/done-suspended]`)
		if idx < 0 {
			t.Fatalf(`应含 feat/done-suspended 行, got:`+"\n"+`%s`, out)
		}
		// 向前扩到行首，使状态前缀（`complete  [`）进入视野。
		start := strings.LastIndexByte(out[:idx], '\n') + 1
		line := out[start:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, `complete  [feat/done-suspended]`) {
			t.Errorf(`已完成任务行应渲染 complete, got 行: %q`, line)
		}
		if strings.Contains(line, `offered`) {
			t.Errorf(`已完成任务行不得再出现 offered（待认领）, got 行: %q`, line)
		}
	})
}

// TestAdviseUnclaimedAssignment pins the P2 task-implement advisory: gating a task offered to ANOTHER agent that was never claimed emits an ADVISORY checklog trail (never blocks); the assignee gating their own task, a claimed task, an undetectable current agent, and a failed gate all stay silent.
//
// TestAdviseUnclaimedAssignment 钉住 P2 task-implement advisory：给「分派给另一个 agent 且
// 从未认领」的任务过门禁会留下 ADVISORY checklog 痕迹（绝不阻断）；受派方本人过门禁、已
// claimed、探测不到当前 agent、门禁未过均静默。
func TestAdviseUnclaimedAssignment(t *testing.T) {
	setup := func(t *testing.T) (string, *taskpipeline.TaskState) {
		t.Helper()
		t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
		root := t.TempDir()
		s := &taskpipeline.TaskState{TaskRef: `feat/delegated`, Summary: `分派任务`}
		if err := s.AssignTo(`kimi`, `backend`, `claude-code`); err != nil {
			t.Fatalf(`AssignTo: %v`, err)
		}
		return root, s
	}
	count := func(t *testing.T, root, ref string) int {
		t.Helper()
		entries, err := checklog.LoadForTask(root, ref)
		if err != nil {
			t.Fatalf(`LoadForTask: %v`, err)
		}
		return len(entries)
	}

	t.Run(`other agent gating unclaimed offered emits advisory`, func(t *testing.T) {
		root, s := setup(t)
		t.Setenv(`FORGE_AGENT`, `claude-code`)
		adviseUnclaimedAssignment(root, `task-implement`, true, s)
		if n := count(t, root, s.TaskRef); n != 1 {
			t.Fatalf(`应落 1 条 advisory 痕迹, got %d`, n)
		}
	})
	t.Run(`assignee gating own task stays silent`, func(t *testing.T) {
		root, s := setup(t)
		t.Setenv(`FORGE_AGENT`, `kimi`)
		adviseUnclaimedAssignment(root, `task-implement`, true, s)
		if n := count(t, root, s.TaskRef); n != 0 {
			t.Fatalf(`受派方本人过门禁不应 advisory, got %d 条`, n)
		}
	})
	t.Run(`claimed task stays silent`, func(t *testing.T) {
		root, s := setup(t)
		t.Setenv(`FORGE_AGENT`, `claude-code`)
		if err := s.Claim(`kimi`); err != nil {
			t.Fatalf(`Claim: %v`, err)
		}
		adviseUnclaimedAssignment(root, `task-implement`, true, s)
		if n := count(t, root, s.TaskRef); n != 0 {
			t.Fatalf(`已 claimed 不应 advisory, got %d 条`, n)
		}
	})
	t.Run(`other gates and failed gates stay silent`, func(t *testing.T) {
		root, s := setup(t)
		t.Setenv(`FORGE_AGENT`, `claude-code`)
		adviseUnclaimedAssignment(root, `task-verify`, true, s)
		adviseUnclaimedAssignment(root, `task-implement`, false, s)
		if n := count(t, root, s.TaskRef); n != 0 {
			t.Fatalf(`非 task-implement / 未过门禁不应 advisory, got %d 条`, n)
		}
	})
	t.Run(`undetectable current agent stays silent`, func(t *testing.T) {
		root, s := setup(t)
		t.Setenv(`FORGE_AGENT`, ``)
		t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
		adviseUnclaimedAssignment(root, `task-implement`, true, s)
		if n := count(t, root, s.TaskRef); n != 0 {
			t.Fatalf(`探测不到当前 agent 应静默（误报比漏报更糟）, got %d 条`, n)
		}
	})
}

// writeCrossRepoDepState 往指定 workspace 成员 key 的 DataDir（相对
// FORGE_DATA_HOME）写一个 task state 文件——即 taskpipeline.LoadDepState 解析
// key:ref 依赖到的磁盘形态（与 taskpipeline/depref_test.go 同款夹具纪律）。
func writeCrossRepoDepState(t *testing.T, home, key string, s *taskpipeline.TaskState) {
	t.Helper()
	dir := filepath.Join(home, `projects`, key, `tasks`)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskcontext.SanitizeRef(s.TaskRef)+`.json`), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestAnnotateDep_CrossRepoResolves pins the cross-repo fix (fix/cleanup-batch, 2026-08-29): a key:ref dependency resolves via taskpipeline.LoadDepState (mirroring task_health.go's lookupState) instead of reading as forever-missing — the foreign task can never appear in this repo's byRef index, so before the fix a live cross-repo dep rendered "missing" and hid where the worker is actually blocked.
//
// TestAnnotateDep_CrossRepoResolves 钉住跨仓修复（fix/cleanup-batch，
// 2026-08-29）：key:ref 依赖经 taskpipeline.LoadDepState 解析（镜像
// task_health.go 的 lookupState），不再恒读作 missing——他仓任务本就不可能
// 出现在本仓 byRef 索引里，修复前在途的跨仓依赖渲染成 "missing"、掩盖工作方
// 真正卡在哪一环。失败形态（key 未知/目标消失）保守保持 "missing"，与门禁
// PendingDependencies 一致。
func TestAnnotateDep_CrossRepoResolves(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	root := t.TempDir()

	// 跨仓成员 bb0000000002：一个已交付的分派依赖 + 一个普通在途依赖。
	writeCrossRepoDepState(t, home, `bb0000000002`,
		&taskpipeline.TaskState{TaskRef: `b-done`, Assignment: &taskpipeline.Assignment{Status: taskpipeline.AssignDelivered}})
	writeCrossRepoDepState(t, home, `bb0000000002`, &taskpipeline.TaskState{TaskRef: `b-wip`})

	byRef := map[string]*taskpipeline.TaskState{} // 本仓索引为空：跨仓 ref 永不命中
	total := len(taskpipeline.DefaultGates())

	// 已交付的跨仓依赖 → 其 Assignment.Status，而非 "missing"。
	if st, _, tot := annotateDep(root, `bb0000000002:b-done`, byRef); st != taskpipeline.AssignDelivered {
		t.Errorf(`cross-repo delivered: status = %q, want %q（跨仓 ref 不得恒 missing）`, st, taskpipeline.AssignDelivered)
	} else if tot != total {
		t.Errorf(`gate total = %d, want %d`, tot, total)
	}

	// 在途跨仓依赖 → incomplete 带门禁进度，而非 "missing"。
	if st, passed, _ := annotateDep(root, `bb0000000002:b-wip`, byRef); st != `incomplete` {
		t.Errorf(`cross-repo wip: status = %q, want "incomplete"`, st)
	} else if passed != 0 {
		t.Errorf(`cross-repo wip: gate passed = %d, want 0`, passed)
	}

	// 保守失败形态保持 "missing"：key 未知、目标消失、以及不在索引中的本仓 ref
	// （回归守卫）。
	for _, ref := range []string{`cc0000000003:anything`, `bb0000000002:b-ghost`, `local-ghost`} {
		if st, _, _ := annotateDep(root, ref, byRef); st != `missing` {
			t.Errorf(`%s: status = %q, want "missing"`, ref, st)
		}
	}

	// 本仓索引路径不变：索引中的前序依赖照旧标注。
	byRef[`local-wip`] = &taskpipeline.TaskState{TaskRef: `local-wip`}
	if st, _, _ := annotateDep(root, `local-wip`, byRef); st != `incomplete` {
		t.Errorf(`local wip: status = %q, want "incomplete"`, st)
	}
}

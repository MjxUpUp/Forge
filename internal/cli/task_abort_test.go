package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// taskStatePath returns the DataDir/tasks/<sanitized-ref>.json path for dir,
// mirroring how SaveTaskState names state files after the refactor-data-home
// migration (task state lives in the user-level DataDir, NOT <dir>/.forge/).
func taskStatePath(dir, taskRef string) string {
	return filepath.Join(forgedata.DataDirFor(dir), "tasks", taskcontext.SanitizeRef(taskRef)+".json")
}

// TestTaskAbort_ExplicitRef deletes the task state file and clears the
// active-task-ref. This is the escape hatch for ghost/stuck tasks — the gap
// that left the 2026-06-16 code-knowledge-base session unable to clean up a
// task that could never pass its gates.
func TestTaskAbort_ExplicitRef(t *testing.T) {
	// Pin the session id so forge writes the deterministic legacy global
	// active-task-ref file rather than a CLAUDE_CODE_SESSION_ID-scoped one
	// (which the ambient Claude Code env would otherwise inject).
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate DataDir from real ~/.forge (refactor-data-home)
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")

	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")
	runGit(t, tmpDir, "checkout", "-b", "feature/test-abort")

	// Start a task with an explicit ref. It must create the state file AND set
	// the active-task-ref (the legacy global file, since no CLAUDE_CODE_SESSION_ID).
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "feature/test-abort", "--title", "abort probe"); code != 0 {
		t.Fatalf("forge task start failed: %s", stdout)
	}
	statePath := taskStatePath(tmpDir, "feature/test-abort")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("task state file not created at %s: %v", statePath, err)
	}
	activeRefPath := filepath.Join(forgedata.DataDirFor(tmpDir), "active-task-ref")
	if _, err := os.Stat(activeRefPath); err != nil {
		t.Fatalf("active-task-ref not created: %v", err)
	}

	// Abort by explicit ref.
	stdout, _, code := runForge(t, tmpDir, "task", "abort", "--ref", "feature/test-abort")
	if code != 0 {
		t.Fatalf("forge task abort exit %d, output: %s", code, stdout)
	}
	if !strings.Contains(stdout, "Task aborted") {
		t.Fatalf("abort output missing confirmation, got: %s", stdout)
	}

	// Core guarantee: the state file is gone.
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("task state file still exists after abort (err=%v)", err)
	}
	// The active-task-ref pointing at the aborted task is cleared.
	if _, err := os.Stat(activeRefPath); !os.IsNotExist(err) {
		t.Fatalf("active-task-ref still exists after abort (err=%v)", err)
	}

	// Idempotent: re-aborting an already-gone task must not error (it may be a
	// stale dangling ref). This is what makes abort safe to call repeatedly.
	stdout, _, code = runForge(t, tmpDir, "task", "abort", "--ref", "feature/test-abort")
	if code != 0 {
		t.Fatalf("re-abort of missing task should be idempotent, got exit %d: %s", code, stdout)
	}
}

// TestTaskAbort_NoRefResolvesActiveTask verifies abort without --ref resolves to
// the session's active task — the common case for cleaning up a half-started task.
func TestTaskAbort_NoRefResolvesActiveTask(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate DataDir from real ~/.forge (refactor-data-home)
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")
	runGit(t, tmpDir, "checkout", "-b", "feature/active-probe")

	// Start with branch-derived ref (no explicit --ref).
	if stdout, _, code := runForge(t, tmpDir, "task", "start"); code != 0 {
		t.Fatalf("forge task start failed: %s", stdout)
	}
	statePath := taskStatePath(tmpDir, "feature/active-probe")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("task state file not created: %v", err)
	}

	// Abort with no --ref — must resolve the active task.
	stdout, _, code := runForge(t, tmpDir, "task", "abort")
	if code != 0 {
		t.Fatalf("forge task abort (no ref) exit %d, output: %s", code, stdout)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("active task state file still exists after abort (err=%v)", err)
	}
}

// TestTaskAbort_NoTaskErrors verifies abort errors clearly when there is no task
// to identify, rather than silently no-op'ing or deleting something unexpected.
func TestTaskAbort_NoTaskErrors(t *testing.T) {
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	// On master with no active task and no --ref → must error.
	stdout, _, code := runForge(t, tmpDir, "task", "abort")
	if code == 0 {
		t.Fatalf("expected non-zero exit when no task to abort, got exit 0: %s", stdout)
	}
	if !strings.Contains(stdout, "no task to abort") {
		t.Fatalf("error output should guide the user, got: %s", stdout)
	}
}

// ---- CLI 依赖方 E2E（自 task_assignment_test.go 迁入）----
//
// TestTaskAbort_WarnsReverseDeps: aborting a task that others DependsOn surfaces the dangling
// edge.
//
// TestTaskAbort_WarnsReverseDeps：abort 一个被其他 task DependsOn 的 task 会暴露悬空边——依赖方门禁将
// 因上游缺失永远阻塞。我们不级联 abort，但 JSON 带 dependents_blocked 让编排器可重指或 abort 它们。
func TestTaskAbort_WarnsReverseDeps(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--title`, `上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/up`, `--title`, `下游`)

	out, _, code := runForge(t, dir, `task`, `abort`, `--ref`, `feat/up`, `--json`)
	if code != 0 {
		t.Fatalf(`abort exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `dependents_blocked`) || !strings.Contains(out, `feat/down`) {
		t.Errorf(`abort JSON 应含 dependents_blocked=[feat/down], got: %s`, out)
	}
}

// TestTaskAbort_CascadeAbortsDependents: --cascade aborts the transitive closure of dependents.
//
// TestTaskAbort_CascadeAbortsDependents：--cascade abort 依赖方传递闭包。链 feat/up <- feat/mid <-
// feat/down：abort feat/up --cascade 删三者；cascaded 列 mid+down，且随后的 mine 不再见已分派的 feat/down。
//
// 已知未覆盖（justified gap，决策 ddkltp44tto8o-1-31332e0a）：单条级联删除的【失败分支】（DeleteTaskState 返
// 非 ENOENT 错）未单测。根因是依赖无注入手段——ListTaskStates（state.go）对目录条目 IsDir 跳过，使非空目录
// 不可作 dependent；chmod 0444 / icacls deny :(D) 经实证 os.Remove 仍成功；x/sys op-lock 或 ACL 需引新依赖
// （YAGNI），为单一错误分支抽 deleteFn seam 属 over-engineering。行为正确性已由代码审读 + 本测试 happy-path
// 精确集（cascaded==cascadedDone）钉死：失败删除走 continue 被排除出 cascadedDone，且正确跳过盘上仍在的
// 任务的 ClearActiveTaskRef。
func TestTaskAbort_CascadeAbortsDependents(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--title`, `上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/mid`, `--depends-on`, `feat/up`, `--title`, `中游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/mid`, `--assignee`, `kimi`, `--title`, `下游`)

	out, _, code := runForge(t, dir, `task`, `abort`, `--ref`, `feat/up`, `--cascade`, `--json`)
	if code != 0 {
		t.Fatalf(`abort --cascade exit %d: %s`, code, out)
	}
	// JSON `cascaded` 必须精确报实际删除成功的依赖方（cascadedDone）并排序——不是 BFS 试图闭包。
	// 松散子串检查即便错把失败删除列进去也会过。解析钉死修复：cascaded == [feat/down, feat/mid]（皆删、排序）。
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf(`abort --json 输出应可解析为 JSON, got %q: %v`, out, err)
	}
	cascaded, _ := payload[`cascaded`].([]any)
	got := map[string]bool{}
	for _, c := range cascaded {
		if s, ok := c.(string); ok {
			got[s] = true
		}
	}
	if len(got) != 2 || !got[`feat/mid`] || !got[`feat/down`] {
		t.Errorf(`cascaded 应恰为 [feat/down, feat/mid]（皆成功删除），got %v (raw %s)`, cascaded, out)
	}
	mineOut, _, _ := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--json`)
	if strings.Contains(mineOut, `feat/down`) {
		t.Errorf(`cascade 后 feat/down 应已删，mine 不应再含, got: %s`, mineOut)
	}
}

// TestTaskAbort_DetachDepsRemovesEdge: --detach-deps removes the edge from each
// direct dependent, keeping the dependent task alive. feat/up <- feat/down:
// abort feat/up --detach-deps leaves feat/down still visible to mine but its
// DependsOn emptied of feat/up (detached lists feat/down).
//
// TestTaskAbort_DetachDepsRemovesEdge：--detach-deps 摘掉每个直接依赖方的边，保留依赖方任务。
// feat/up <- feat/down：abort feat/up --detach-deps 留下 feat/down 仍被 mine 可见，但 DependsOn 不再含
// feat/up（detached 列 feat/down）。
func TestTaskAbort_DetachDepsRemovesEdge(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--title`, `上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/up`, `--assignee`, `kimi`, `--title`, `下游`)

	out, _, code := runForge(t, dir, `task`, `abort`, `--ref`, `feat/up`, `--detach-deps`, `--json`)
	if code != 0 {
		t.Fatalf(`abort --detach-deps exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `detached`) || !strings.Contains(out, `feat/down`) {
		t.Errorf(`--detach-deps 应在 JSON 含 detached=[feat/down], got: %s`, out)
	}
	mineOut, _, _ := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--json`)
	if !strings.Contains(mineOut, `feat/down`) {
		t.Errorf(`detach 后 feat/down 应仍存在（mine 可见）, got: %s`, mineOut)
	}
	if strings.Contains(mineOut, `feat/up`) {
		t.Errorf(`detach 后 feat/down 的 DependsOn 不应再含 feat/up, got: %s`, mineOut)
	}
}

// TestTaskAbort_DetachDepsKeepsOtherEdges (L5): --detach-deps rebuilds DependsOn
// by excluding only the aborted ref, so other edges survive. feat/down depends
// on feat/up AND feat/other.
//
// TestTaskAbort_DetachDepsKeepsOtherEdges（L5）：--detach-deps 重建 DependsOn 时只排除被 abort 的 ref，
// 其他边存活。feat/down 依赖 feat/up 且 feat/other；abort feat/up --detach-deps 必须保留 feat/other 在
// feat/down 的 DependsOn（mine 仍显示 feat/down 阻塞于 feat/other），而 feat/up 消失。钉住闭包保边正确性。
func TestTaskAbort_DetachDepsKeepsOtherEdges(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--title`, `上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/other`, `--title`, `其他上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/up`, `--depends-on`, `feat/other`, `--assignee`, `kimi`, `--title`, `下游`)

	out, _, code := runForge(t, dir, `task`, `abort`, `--ref`, `feat/up`, `--detach-deps`, `--json`)
	if code != 0 {
		t.Fatalf(`abort --detach-deps exit %d: %s`, code, out)
	}
	mineOut, _, _ := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--json`)
	if strings.Contains(mineOut, `feat/up`) {
		t.Errorf(`detach 后 feat/down 不应再依赖 feat/up, got: %s`, mineOut)
	}
	if !strings.Contains(mineOut, `feat/other`) {
		t.Errorf(`detach 只摘 feat/up 边，feat/other 应保留在 feat/down 的 DependsOn, got: %s`, mineOut)
	}
}

// TestTaskAbort_CascadeDiamondTopology (L1): --cascade over a diamond (A<-B,
// A<-C, B<-D, C<-D) must collect B/C/D with the visited guard preventing D from
// being entered twice via its two paths.
//
// TestTaskAbort_CascadeDiamondTopology（L1）：--cascade 跨钻石拓扑（A<-B, A<-C, B<-D, C<-D）必须收
// B/C/D，visited 守卫防 D 经两条路径入队两次。随后的 mine 不再见已分派的 feat/d。覆盖 CascadeAbortsDependents
// 的线性链之外的非线性拓扑。
func TestTaskAbort_CascadeDiamondTopology(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/a`, `--title`, `A`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/b`, `--depends-on`, `feat/a`, `--title`, `B`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/c`, `--depends-on`, `feat/a`, `--title`, `C`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/d`, `--depends-on`, `feat/b`, `--depends-on`, `feat/c`, `--assignee`, `kimi`, `--title`, `D`)

	out, _, code := runForge(t, dir, `task`, `abort`, `--ref`, `feat/a`, `--cascade`, `--json`)
	if code != 0 {
		t.Fatalf(`abort --cascade exit %d: %s`, code, out)
	}
	// 双重钉法：原始切片长度（len(cascaded)）+ 集合（got）双检。集合会去重——若 visited 守卫失效，D 经两
	// 路径被 BFS 入队两次，cascaded=[b,c,d,d]（第二次删走 ENOENT 容忍仍计成功），集合塌缩到 len 3 漏掉该回归。
	// 故必须先检原始长度==3（捕获重复入队），再检集合恰为 {feat/b, feat/c, feat/d}（捕获误列/漏列）。
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf(`abort --json 输出应可解析为 JSON, got %q: %v`, out, err)
	}
	cascaded, _ := payload[`cascaded`].([]any)
	got := map[string]bool{}
	for _, c := range cascaded {
		if s, ok := c.(string); ok {
			got[s] = true
		}
	}
	if len(cascaded) != 3 || len(got) != 3 || !got[`feat/b`] || !got[`feat/c`] || !got[`feat/d`] {
		t.Errorf(`钻石 cascaded 应恰为 {feat/b, feat/c, feat/d}（皆删、visited 守卫防 D 重复入队），got rawLen=%d set=%v (raw %s)`, len(cascaded), got, out)
	}
	mineOut, _, _ := runForge(t, dir, `task`, `mine`, `--agent`, `kimi`, `--json`)
	if strings.Contains(mineOut, `feat/d`) {
		t.Errorf(`钻石 cascade 后 feat/d 应已删，mine 不应再含, got: %s`, mineOut)
	}
}

// TestTaskAbort_CascadeAndDetachMutuallyExclusive: the two non-default branches
// of the design §4 three-way abort cannot combine.
//
// TestTaskAbort_CascadeAndDetachMutuallyExclusive：设计§4 三选一的两个非默认分支不可组合——
// --cascade 删依赖方，--detach-deps 留依赖方。
func TestTaskAbort_CascadeAndDetachMutuallyExclusive(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/x`, `--title`, `X`)
	out, _, code := runForge(t, dir, `task`, `abort`, `--ref`, `feat/x`, `--cascade`, `--detach-deps`)
	if code == 0 {
		t.Fatalf(`--cascade 与 --detach-deps 互斥应失败, got exit 0: %s`, out)
	}
	if !strings.Contains(out, `互斥`) {
		t.Errorf(`应报互斥错误, got: %s`, out)
	}
}

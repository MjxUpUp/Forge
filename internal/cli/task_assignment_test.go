package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupDelegateProject prepares an isolated forge project (git + forge init + initial commit
// + feat/delegate branch) for delegation E2E tests. CLAUDE_CODE_SESSION_ID is pinned non-empty
// so claim's SetActiveTaskRef writes a session-scoped active-task-ref that resume can read back
// (the anchor-side-effect we assert in the lifecycle test). FORGE_DATA_HOME is isolated per
// test so delegated task states never leak into the real ~/.forge.
//
// setupDelegateProject 为分派 E2E 测试准备一个隔离的 forge 项目（git + forge init + 初始提交
// + feat/delegate 分支）。CLAUDE_CODE_SESSION_ID 钉为非空，使 claim 的 SetActiveTaskRef 写
// session-scoped active-task-ref 供 resume 读回（生命周期测试断言的锚定副作用）。
// FORGE_DATA_HOME 每测试隔离，分派 task state 不泄漏进真实 ~/.forge。
func setupDelegateProject(t *testing.T) string {
	t.Helper()
	t.Setenv("CLAUDE_CODE_SESSION_ID", "delegate-test-sid")
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	if stdout, _, code := runForge(t, dir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	// main.go body via a raw string so real newlines are preserved (backtick strings do not
	// process \n escapes) AND no ASCII double-quote is written into the test source (Windows
	// quote-corruption mitigation, see memory windows-input-quote-corruption).
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func main() {}
`), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "checkout", "-b", "feat/delegate")
	return dir
}

// TestTaskAssignClaimDeliver_Lifecycle walks the full A2A delegation lifecycle end-to-end:
// start → assign (offered) → claim (claimed, session anchored) → deliver (delivered) → mine.
// It pins the central contract of the multi-agent delegation design — the state machine in
// types.go driven through the CLI, plus claim's implicit session anchoring (resume with no
// --ref pulls back the claimed task).
//
// TestTaskAssignClaimDeliver_Lifecycle 端到端走完整 A2A 分派生命周期：
// start → assign（offered）→ claim（claimed，session 锚定）→ deliver（delivered）→ mine。
// 钉住多 agent 分派设计的核心契约——types.go 状态机经 CLI 驱动，外加 claim 的隐式 session
// 锚定（resume 无 --ref 拉回所认领的任务）。
func TestTaskAssignClaimDeliver_Lifecycle(t *testing.T) {
	dir := setupDelegateProject(t)

	// Start a task to delegate.
	if stdout, _, code := runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "delegated work"); code != 0 {
		t.Fatalf("task start failed: %s", stdout)
	}

	// Orchestrator assigns to kimi (frontend role). Task becomes offered.
	stdout, _, code := runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "kimi", "--role", "frontend", "--by", "claude-code")
	if code != 0 {
		t.Fatalf("task assign exit %d: %s", code, stdout)
	}
	for _, want := range []string{`已分派给 kimi`, `offered`, `frontend`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("assign 输出应含 %q, got: %s", want, stdout)
		}
	}

	// Worker claims. State → claimed, and the session is anchored to this task.
	stdout, _, code = runForge(t, dir, "task", "claim", "--ref", "feat/delegate", "--as", "kimi")
	if code != 0 {
		t.Fatalf("task claim exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `已认领`) {
		t.Errorf("claim 输出应含「已认领」, got: %s", stdout)
	}

	// Anchor side-effect: resume with NO --ref must pull back the claimed task (claim set the
	// session-scoped active-task-ref). This is the claim = "successor attaches" guarantee.
	resumeOut, _, rcode := runForge(t, dir, "task", "resume", "--no-attach", "--json")
	if rcode != 0 {
		t.Fatalf("resume after claim exit %d: %s", rcode, resumeOut)
	}
	if !strings.Contains(resumeOut, "feat/delegate") {
		t.Errorf("claim 应锚定 session，resume 无 --ref 应拉回 feat/delegate, got: %s", resumeOut)
	}

	// Worker delivers. State → delivered.
	stdout, _, code = runForge(t, dir, "task", "deliver", "--ref", "feat/delegate")
	if code != 0 {
		t.Fatalf("task deliver exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `已交付`) || !strings.Contains(stdout, `delivered`) {
		t.Errorf("deliver 输出应含「已交付/delivered」, got: %s", stdout)
	}

	// mine lists the delegated task back to kimi (status now delivered).
	mineOut, _, mcode := runForge(t, dir, "task", "mine", "--agent", "kimi", "--json")
	if mcode != 0 {
		t.Fatalf("task mine exit %d: %s", mcode, mineOut)
	}
	for _, want := range []string{`feat/delegate`, `delivered`, `frontend`} {
		if !strings.Contains(mineOut, want) {
			t.Errorf("mine JSON 应含 %q, got: %s", want, mineOut)
		}
	}
}

// TestTaskAssign_UnknownAgentWarnAccepted: an agent absent from the known set (codebuddy,
// which has no project marker) is warned about but still accepted — the soft-validation
// contract. The task is created offered so a worker using --as can still claim it explicitly.
//
// TestTaskAssign_UnknownAgentWarnAccepted：不在已知集的 agent（codebuddy，无项目标记）被
// 警告但仍接受——软校验契约。任务创建为 offered，工作方用 --as 仍能显式认领。
func TestTaskAssign_UnknownAgentWarnAccepted(t *testing.T) {
	dir := setupDelegateProject(t)
	if stdout, _, code := runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x"); code != 0 {
		t.Fatalf("task start failed: %s", stdout)
	}
	// codebuddy is NOT in KnownAgents (no project marker) → warn, but exit 0 and offered.
	stdout, _, code := runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "codebuddy", "--by", "claude-code")
	if code != 0 {
		t.Fatalf("assign to unknown agent should be accepted (warn only), got exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `不在已知集`) {
		t.Errorf("assign to unknown agent should warn「不在已知集」, got: %s", stdout)
	}
	if !strings.Contains(stdout, `已分派给 codebuddy`) {
		t.Errorf("assign should still succeed and report offered to codebuddy, got: %s", stdout)
	}
}

// TestTaskAssign_RequiresTo: --to is mandatory; omitting it errors with guidance listing the
// known agents (so the user discovers the valid set from the error itself).
//
// TestTaskAssign_RequiresTo：--to 必填；缺省时报错并列出已知 agent（用户从错误本身即可发现合法集）。
func TestTaskAssign_RequiresTo(t *testing.T) {
	dir := setupDelegateProject(t)
	stdout, _, code := runForge(t, dir, "task", "assign", "--ref", "feat/delegate")
	if code == 0 {
		t.Fatalf("assign without --to should fail, got exit 0: %s", stdout)
	}
	if !strings.Contains(stdout, `--to 必填`) {
		t.Errorf("error should guide with「--to 必填」, got: %s", stdout)
	}
	// The error surfaces the known set so the user learns valid agents inline.
	if !strings.Contains(stdout, `kimi`) {
		t.Errorf("error should list known agents (e.g. kimi), got: %s", stdout)
	}
}

// TestTaskMine_EmptyReturnsArray: mine with no matching delegations returns a JSON array
// (never nil/null) so downstream JSON consumers do not need a null special-case.
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

// TestTaskStart_AssigneeWarnAccepted: `task start --assignee` shares the warnIfUnknownAgent
// guard with `task assign` (Bug 2's whole point — without this test the guard on the start path
// could be deleted and every test would still stay green). codebuddy is unknown → warn, but the
// task is still created and offered (exit 0), so a worker using --as can claim it. mine must find
// it back, proving the offered assignment was actually built.
//
// TestTaskStart_AssigneeWarnAccepted：task start --assignee 与 task assign 共享 warnIfUnknownAgent
// 守卫（Bug 2 的全部意义——无此测试，start 路径的守卫可被删而所有测试仍绿）。codebuddy 未知 → 警告，
// 但任务仍创建为 offered（exit 0），工作方用 --as 可认领。mine 必须拉回，证明 offered 分派确已建成。
func TestTaskStart_AssigneeWarnAccepted(t *testing.T) {
	dir := setupDelegateProject(t)
	// start --assignee codebuddy: unknown agent → warn but accept (the soft-validation contract
	// is identical to `task assign --to`).
	stdout, _, code := runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--assignee", "codebuddy", "--role", "frontend", "--title", "x")
	if code != 0 {
		t.Fatalf("start --assignee to unknown agent should be accepted (warn only), got exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `不在已知集`) {
		t.Errorf("start --assignee should warn「不在已知集」, got: %s", stdout)
	}
	// The guard has teeth only if the offered assignment was actually built — mine must return it.
	mineOut, _, mcode := runForge(t, dir, "task", "mine", "--agent", "codebuddy", "--json")
	if mcode != 0 {
		t.Fatalf("mine after start --assignee exit %d: %s", mcode, mineOut)
	}
	for _, want := range []string{`feat/delegate`, `offered`, `frontend`} {
		if !strings.Contains(mineOut, want) {
			t.Errorf("mine JSON 应含 %q（任务应建成 offered 给 codebuddy）, got: %s", want, mineOut)
		}
	}
}

// TestTaskQuestionAnswer_Lifecycle walks the回抛/答复 path: claim→question(input-required)→
// answer(回 claimed)→deliver。钉住「答复后任务恢复到可交付态」——这是 input-required 不死锁的保证。
//
// TestTaskQuestionAnswer_Lifecycle 走回抛/答复路径：claim→question(input-required)→answer(回
// claimed)→deliver。钉住「答复后任务恢复到可交付态」——input-required 不死锁的保证。
func TestTaskQuestionAnswer_Lifecycle(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x")
	runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "kimi", "--by", "claude-code")
	runForge(t, dir, "task", "claim", "--ref", "feat/delegate", "--as", "kimi")

	// question: claimed → input-required
	stdout, _, code := runForge(t, dir, "task", "question", "--ref", "feat/delegate", "--content", "API 契约不明")
	if code != 0 {
		t.Fatalf("question exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `input-required`) {
		t.Errorf("question 输出应含 input-required, got: %s", stdout)
	}

	// answer: input-required → claimed
	stdout, _, code = runForge(t, dir, "task", "answer", "--ref", "feat/delegate", "--content", "用 REST")
	if code != 0 {
		t.Fatalf("answer exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `claimed`) {
		t.Errorf("answer 输出应含 claimed, got: %s", stdout)
	}

	// 回 claimed 后必须能 deliver（input-required 不卡死后续交付）
	stdout, _, code = runForge(t, dir, "task", "deliver", "--ref", "feat/delegate")
	if code != 0 {
		t.Fatalf("deliver after answer exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `delivered`) {
		t.Errorf("deliver 输出应含 delivered, got: %s", stdout)
	}
}

// TestTaskFailCancelReopen covers the three terminal/diversion transitions via CLI, each from its
// legal前置: fail(claimed), cancel(offered), reopen(delivered). 状态机合法性已在 assignment_test
// 枚举覆盖；此处只验 CLI 接线（命令解析 + 状态 + 原因回显）。
//
// TestTaskFailCancelReopen 经 CLI 覆盖三个终态/ diversion 转换，各从合法前置触发：fail(claimed)、
// cancel(offered)、reopen(delivered)。状态机合法性已在 assignment_test 枚举覆盖；此处只验 CLI 接线
// （命令解析 + 状态 + 原因回显）。
func TestTaskFailCancelReopen(t *testing.T) {
	t.Run("fail: claimed->failed", func(t *testing.T) {
		dir := setupDelegateProject(t)
		runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x")
		runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "kimi", "--by", "claude-code")
		runForge(t, dir, "task", "claim", "--ref", "feat/delegate", "--as", "kimi")
		stdout, _, code := runForge(t, dir, "task", "fail", "--ref", "feat/delegate", "--reason", "编译不过")
		if code != 0 {
			t.Fatalf("fail exit %d: %s", code, stdout)
		}
		if !strings.Contains(stdout, `failed`) || !strings.Contains(stdout, `编译不过`) {
			t.Errorf("fail 输出应含 failed+原因, got: %s", stdout)
		}
	})
	t.Run("cancel: offered->canceled", func(t *testing.T) {
		dir := setupDelegateProject(t)
		runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x")
		runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "kimi", "--by", "claude-code")
		stdout, _, code := runForge(t, dir, "task", "cancel", "--ref", "feat/delegate", "--reason", "需求变了")
		if code != 0 {
			t.Fatalf("cancel exit %d: %s", code, stdout)
		}
		if !strings.Contains(stdout, `canceled`) || !strings.Contains(stdout, `需求变了`) {
			t.Errorf("cancel 输出应含 canceled+原因, got: %s", stdout)
		}
	})
	t.Run("reopen: delivered->claimed", func(t *testing.T) {
		dir := setupDelegateProject(t)
		runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x")
		runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "kimi", "--by", "claude-code")
		runForge(t, dir, "task", "claim", "--ref", "feat/delegate", "--as", "kimi")
		runForge(t, dir, "task", "deliver", "--ref", "feat/delegate")
		stdout, _, code := runForge(t, dir, "task", "reopen", "--ref", "feat/delegate", "--reason", "联调发现 bug")
		if code != 0 {
			t.Fatalf("reopen exit %d: %s", code, stdout)
		}
		if !strings.Contains(stdout, `claimed`) || !strings.Contains(stdout, `联调发现 bug`) {
			t.Errorf("reopen 输出应含 claimed+原因, got: %s", stdout)
		}
	})
}

// TestTaskQuestion_RequiresContent: --content is mandatory for question (回抛需有内容，无内容无意义)。
//
// TestTaskQuestion_RequiresContent：question 的 --content 必填（回抛需有内容）。
func TestTaskQuestion_RequiresContent(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x")
	runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "kimi", "--by", "claude-code")
	runForge(t, dir, "task", "claim", "--ref", "feat/delegate", "--as", "kimi")
	stdout, _, code := runForge(t, dir, "task", "question", "--ref", "feat/delegate")
	if code == 0 {
		t.Fatalf("question without --content should fail, got exit 0: %s", stdout)
	}
	if !strings.Contains(stdout, `--content 必填`) {
		t.Errorf("error should guide with「--content 必填」, got: %s", stdout)
	}
}

// TestTaskAnswer_EmptyAllowed: answer with no --content is accepted (resume-only, no Decision
// recorded) — the design contract that an empty reply still unblocks input-required without forcing
// the orchestrator to fabricate a rationale.
//
// TestTaskAnswer_EmptyAllowed：answer 无 --content 被接受（仅恢复 claimed 不记 Decision）——设计契约：
// 空答复仍能解除 input-required 死锁，不强制编排器编造 rationale。
func TestTaskAnswer_EmptyAllowed(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x")
	runForge(t, dir, "task", "assign", "--ref", "feat/delegate", "--to", "kimi", "--by", "claude-code")
	runForge(t, dir, "task", "claim", "--ref", "feat/delegate", "--as", "kimi")
	runForge(t, dir, "task", "question", "--ref", "feat/delegate", "--content", "q")
	stdout, _, code := runForge(t, dir, "task", "answer", "--ref", "feat/delegate")
	if code != 0 {
		t.Fatalf("empty answer should be accepted (resume only), got exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `空答复`) {
		t.Errorf("empty answer should note「空答复」, got: %s", stdout)
	}
}

// TestTaskDependsOn_GateBlocksUntilUpstreamDelivered pins the DependsOn gate (design phase 3): a
// task cannot pass task-verify while an upstream it DependsOn is not delivered, and unblocks the
// moment that upstream is delivered. Deliver is via the assignment path (assign→claim→deliver),
// which sets Assignment.Status=delivered → IsDelivered true without forcing the upstream through
// its own gates — proving the gate honors all three delivery shapes, not just "complete".
//
// TestTaskDependsOn_GateBlocksUntilUpstreamDelivered 钉住 DependsOn 门禁（设计阶段3）：task 在上游未交付时
// 不能过 task-verify，上游一交付立即放行。交付走分派路径（assign→claim→deliver），置 Assignment.Status=
// delivered → IsDelivered true，不强制上游过自身门禁——证明门禁认全部三种交付形态，而非仅 complete。
func TestTaskDependsOn_GateBlocksUntilUpstreamDelivered(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--title`, `上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/up`, `--title`, `下游`)

	// B 过 task-verify：上游 feat/up 未交付 → HARD BLOCK（注入点在 prerequisite 之前，故最先触发）
	stdout, _, code := runForge(t, dir, `task`, `gate`, `task-verify`, `--ref`, `feat/down`)
	if code == 0 {
		t.Fatalf(`task-verify 应因上游未交付 BLOCKED, got exit 0: %s`, stdout)
	}
	if !strings.Contains(stdout, `上游 task 未交付`) || !strings.Contains(stdout, `feat/up`) {
		t.Errorf(`BLOCKED 信息应含「上游 task 未交付」+ feat/up, got: %s`, stdout)
	}

	// 让上游交付（assignment-delivered，无需过上游门禁）
	runForge(t, dir, `task`, `assign`, `--ref`, `feat/up`, `--to`, `kimi`, `--by`, `claude-code`)
	runForge(t, dir, `task`, `claim`, `--ref`, `feat/up`, `--as`, `kimi`)
	runForge(t, dir, `task`, `deliver`, `--ref`, `feat/up`)

	// 再过 B 的 task-verify：DependsOn 已满足，不再因依赖 BLOCKED（可能因 prerequisite 等其他原因 BLOCK，
	// 但绝不应再是「上游未交付」——这正是门禁放行依赖的信号）
	stdout, _, _ = runForge(t, dir, `task`, `gate`, `task-verify`, `--ref`, `feat/down`)
	if strings.Contains(stdout, `上游 task 未交付`) {
		t.Errorf(`上游已交付后不应再因依赖 BLOCKED, got: %s`, stdout)
	}
}

// TestTaskMine_BlockedShowsPendingDeps: mine --blocked lists only tasks whose DependsOn is not
// fully delivered, with the pending upstream refs in pending_deps. After the upstream is delivered
// the task drops out of --blocked. This is the worker-facing view of the same PendingDependencies
// the gate uses — mine and the gate cannot disagree on "blocked".
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

// TestTaskAbort_WarnsReverseDeps: aborting a task that others DependsOn surfaces the dangling
// edge — the dependent's gate would now block forever on a missing upstream. We do NOT cascade-
// abort, but the JSON carries dependents_blocked so an orchestrator can re-point or abort them.
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

// TestTaskStart_DependsOnCycleRejected: AddDependency's cycle check has teeth at the CLI. A first
// task may forward-reference a not-yet-created upstream (the edge is recorded; the gate later
// treats missing as not-delivered), but closing a ring is rejected: A depends-on B, then B
// depends-on A would deadlock the ring, so the second start fails with a cycle error.
//
// TestTaskStart_DependsOnCycleRejected：AddDependency 的环检测在 CLI 有牙。首个 task 可前向引用尚未创建
// 的上游（边已记；门禁后把缺失当未交付），但闭合环被拒：A 依赖 B，再 B 依赖 A 会死锁环，故第二次 start
// 因环错误失败。
func TestTaskStart_DependsOnCycleRejected(t *testing.T) {
	dir := setupDelegateProject(t)
	// A 依赖 B（B 尚不存在 → 前向引用允许，lookup 返回 nil → 无环 → 接受）
	runForge(t, dir, `task`, `start`, `--ref`, `feat/a`, `--depends-on`, `feat/b`, `--title`, `A`)
	// B 依赖 A：B→A→B（A 已依赖 B）闭合环 → 拒绝
	stdout, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/b`, `--depends-on`, `feat/a`, `--title`, `B`)
	if code == 0 {
		t.Fatalf(`start feat/b --depends-on feat/a 应因环拒绝（B→A→B）, got exit 0: %s`, stdout)
	}
	if !strings.Contains(stdout, `环`) {
		t.Errorf(`环拒绝信息应含「环」, got: %s`, stdout)
	}
}

// TestTaskDependsOn_GateCompleteAlsoBlocks is a regression guard: the DependsOn gate condition is
// `gateID == task-verify || gateID == task-complete`. The other E2E only exercises task-verify, so
// if task-complete were dropped from the condition this test would catch it — an upstream-locked
// task must not slip through completion either.
//
// TestTaskDependsOn_GateCompleteAlsoBlocks 是回归守卫：DependsOn 门禁条件是
// `gateID == task-verify || gateID == task-complete`。另一 E2E 只走 task-verify，若 task-complete 被从
// 条件里回归掉，本测试会抓到——上游未交付的 task 也不该溜过 completion。
func TestTaskDependsOn_GateCompleteAlsoBlocks(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/up`, `--title`, `上游`)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/down`, `--depends-on`, `feat/up`, `--title`, `下游`)
	stdout, _, code := runForge(t, dir, `task`, `gate`, `task-complete`, `--ref`, `feat/down`)
	if code == 0 {
		t.Fatalf(`task-complete 应因上游未交付 BLOCKED, got exit 0: %s`, stdout)
	}
	if !strings.Contains(stdout, `未交付`) {
		t.Errorf(`task-complete BLOCKED 应含依赖未交付信息, got: %s`, stdout)
	}
}

// TestTaskMine_MultipleDepsPendingList: with two upstream deps where one is delivered and the other
// is not, mine --blocked must list only the still-pending one in pending_deps — the delivered
// upstream must not appear. This guards PendingDependencies against reporting already-delivered refs.
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
			t.Fatalf(`解析 mine JSON 失败: %v\n输出: %s`, err, out)
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
			t.Fatalf(`应含 feat/stalled 行, got:\n%s`, out)
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

// saveClaimedAgo writes a task claimed by agent with ClaimedAt forced to `ago` and NO checklog
// activity — the exact shape of a claimed zombie (claimer gone) IsClaimedStale detects. The CLI
// stamps time.Now(), so a deterministic >7d-old claim can only be set in-process.
//
// saveClaimedAgo 写一个被 agent 认领的任务，ClaimedAt 强制为 ago 且无 checklog 活动——正是
// IsClaimedStale 检测的 claimed 僵尸（认领方失联）形态。CLI 盖当前时间，确定性的 >7d 认领只能在进程内设。
func saveClaimedAgo(t *testing.T, dir, ref, agent string, ago time.Time) {
	t.Helper()
	s := &taskpipeline.TaskState{TaskRef: ref, Summary: ref + ` 任务`}
	if err := s.AssignTo(agent, `backend`, `claude-code`); err != nil {
		t.Fatalf(`AssignTo %s: %v`, ref, err)
	}
	if err := s.Claim(agent); err != nil {
		t.Fatalf(`Claim %s: %v`, ref, err)
	}
	s.Assignment.ClaimedAt = &ago
	if err := taskpipeline.SaveTaskState(dir, s); err != nil {
		t.Fatalf(`SaveTaskState %s: %v`, ref, err)
	}
}

// TestTaskReclaim end-to-end exercises forge task reclaim — the §3 TTL recovery trigger. Runs as a
// single linear flow because reclaim mutates state (subtests would couple on order): dry-run lists
// the stale claim without mutating → reclaim flips it to offered (AbandonedCount++) and leaves a
// fresh claim + an offered task untouched → a second reclaim finds nothing.
//
// Coverage note: this exercises the HAPPY path — state does not drift between the IsClaimedStale
// scan and the per-task lock, so the in-lock IsClaimedStale re-check (the M2 TOCTOU guard) is a
// no-op here (it returns the same verdict as the outer scan). Pinning that re-check would require
// mutating checklog activity in the sub-millisecond window between ListTaskStates and LockTask,
// which is not deterministic from the CLI; do NOT remove the in-lock check assuming the outer scan
// suffices — it does not.
//
// TestTaskReclaim 端到端跑 forge task reclaim——§3 的 TTL 回收触发。以单一线性流程跑（reclaim
// 改状态，子测试会顺序耦合）：dry-run 列出 stale 认领但不动 → 回收把它翻为 offered
// （AbandonedCount++）且不动刚认领 + offered 任务 → 第二次回收无候选。
//
// 覆盖说明：本测试只覆盖「快乐路径」——状态在 IsClaimedStale 扫描与按 task 加锁之间不漂移，故
// 锁内的 IsClaimedStale 复检（M2 TOCTOU 守卫）在此是 no-op（与外层扫描返回同样判定）。钉住该复检
// 需在 ListTaskStates 与 LockTask 之间的亚毫秒窗口内写入 checklog 活动，CLI 无法确定性触发；
// 切勿以外层扫描已过滤为由删除锁内复检——并不冗余。
func TestTaskReclaim(t *testing.T) {
	dir := setupDelegateProject(t)

	// Stale claimed zombie (8d ago, no checklog) → reclaim candidate.
	//
	// claimed 僵尸（8d 前，无 checklog）→ 回收候选。
	saveClaimedAgo(t, dir, `feat/stale`, `kimi`, time.Now().Add(-8*24*time.Hour))
	// Fresh claim (now) → NOT stale, must be left alone.
	//
	// 刚认领（当前）→ 非僵尸，须不动。
	saveClaimedAgo(t, dir, `feat/fresh`, `cursor`, time.Now())
	// Offered task → not claimed, must be left alone (Abandon requires claimed).
	//
	// offered 任务 → 非 claimed，须不动（Abandon 要求 claimed）。
	saveOfferedAgo(t, dir, `feat/offered`, `reasonix`, time.Now())

	// 1) --dry-run lists only the stale claim and does NOT mutate.
	//
	// 1) --dry-run 只列 stale 认领且不改状态。
	dryOut, _, dcode := runForge(t, dir, `task`, `reclaim`, `--dry-run`, `--json`)
	if dcode != 0 {
		t.Fatalf(`reclaim --dry-run --json exit %d: %s`, dcode, dryOut)
	}
	var dry reclaimResult
	if err := json.Unmarshal([]byte(dryOut), &dry); err != nil {
		t.Fatalf(`解析 reclaim dry-run JSON 失败: %v\n输出: %s`, err, dryOut)
	}
	if !dry.DryRun || dry.Count != 1 || len(dry.Reclaimed) != 1 || dry.Reclaimed[0] != `feat/stale` {
		t.Fatalf(`dry-run 应 dry_run=true/count=1/仅 feat/stale, got %+v`, dry)
	}
	if st, err := taskpipeline.LoadTaskState(dir, `feat/stale`); err != nil {
		t.Fatalf(`LoadTaskState feat/stale: %v`, err)
	} else if st.Assignment.Status != taskpipeline.AssignClaimed || st.Assignment.AbandonedCount != 0 {
		t.Fatalf(`dry-run 不应改状态, got status=%s count=%d`, st.Assignment.Status, st.Assignment.AbandonedCount)
	}

	// 2) Real reclaim flips feat/stale → offered, bumps AbandonedCount, clears ClaimedAt, sets
	//    AbandonedAt; feat/fresh (claimed now) and feat/offered are untouched.
	//
	// 2) 真回收把 feat/stale → offered、AbandonedCount++、清 ClaimedAt、置 AbandonedAt；
	//    feat/fresh（刚认领）与 feat/offered 不受影响。
	out, _, code := runForge(t, dir, `task`, `reclaim`)
	if code != 0 {
		t.Fatalf(`reclaim exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `已回收`) || !strings.Contains(out, `feat/stale`) {
		t.Errorf(`reclaim 输出应含「已回收」+ feat/stale, got:\n%s`, out)
	}
	st, err := taskpipeline.LoadTaskState(dir, `feat/stale`)
	if err != nil {
		t.Fatalf(`LoadTaskState feat/stale: %v`, err)
	}
	if st.Assignment.Status != taskpipeline.AssignOffered {
		t.Fatalf(`feat/stale 应回 offered, got %s`, st.Assignment.Status)
	}
	if st.Assignment.AbandonedCount != 1 {
		t.Errorf(`feat/stale AbandonedCount 应为 1, got %d`, st.Assignment.AbandonedCount)
	}
	if st.Assignment.AbandonedAt == nil {
		t.Error(`feat/stale AbandonedAt 应已设置`)
	}
	if st.Assignment.ClaimedAt != nil {
		t.Error(`feat/stale ClaimedAt 应被清空`)
	}
	if fresh, err := taskpipeline.LoadTaskState(dir, `feat/fresh`); err != nil {
		t.Fatalf(`LoadTaskState feat/fresh: %v`, err)
	} else if fresh.Assignment.Status != taskpipeline.AssignClaimed {
		t.Errorf(`feat/fresh 应仍 claimed 不被动, got %s`, fresh.Assignment.Status)
	}
	if off, err := taskpipeline.LoadTaskState(dir, `feat/offered`); err != nil {
		t.Fatalf(`LoadTaskState feat/offered: %v`, err)
	} else if off.Assignment.Status != taskpipeline.AssignOffered || off.Assignment.AbandonedCount != 0 {
		t.Errorf(`feat/offered 应不动, got status=%s count=%d`, off.Assignment.Status, off.Assignment.AbandonedCount)
	}

	// 3) Second reclaim finds no candidates — feat/stale is now offered (not claimed).
	//
	// 3) 第二次回收无候选——feat/stale 已 offered（非 claimed）。
	out2, _, code2 := runForge(t, dir, `task`, `reclaim`)
	if code2 != 0 {
		t.Fatalf(`second reclaim exit %d: %s`, code2, out2)
	}
	if !strings.Contains(out2, `无 claimed 僵尸`) {
		t.Errorf(`第二次 reclaim 应报告无候选, got:\n%s`, out2)
	}
}

// TestTaskReclaim_EmptyJSON pins the M1 fix: reclaim --json with NO stale tasks must emit
// "reclaimed": [] (a stable empty array), not the Go-default null. Sibling commands (mine/health
// JSON) share the same convention so consumers can range over the field unconditionally.
//
// TestTaskReclaim_EmptyJSON 钉住 M1 修复：无 stale 任务时 reclaim --json 必须输出
// "reclaimed": []（稳定空数组），而非 Go 默认的 null。兄弟命令（mine/health JSON）同约定，
// 使消费者可无条件遍历该字段。
func TestTaskReclaim_EmptyJSON(t *testing.T) {
	dir := setupDelegateProject(t)
	// A fresh (non-stale) claim → reclaim finds nothing.
	//
	// 刚认领（非僵尸）的任务 → reclaim 无候选。
	saveClaimedAgo(t, dir, `feat/fresh`, `kimi`, time.Now())

	out, _, code := runForge(t, dir, `task`, `reclaim`, `--json`)
	if code != 0 {
		t.Fatalf(`reclaim --json exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `"reclaimed": []`) {
		t.Errorf(`空结果应序列化为 "reclaimed": [] 而非 null, got:\n%s`, out)
	}
	if strings.Contains(out, `null`) {
		t.Errorf(`空结果不应含 null, got:\n%s`, out)
	}
	var res reclaimResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf(`解析失败: %v\n输出: %s`, err, out)
	}
	if res.Count != 0 || len(res.Reclaimed) != 0 {
		t.Errorf(`应 count=0/reclaimed 空, got %+v`, res)
	}
}

// startChild starts a task that is a subtask of parent (ParentTaskRef=parent) via the CLI.
//
// startChild 经 CLI 启动一个 parent 的子任务（ParentTaskRef=parent）。
func startChild(t *testing.T, dir, ref, parent string) {
	t.Helper()
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, ref, `--parent`, parent, `--title`, ref); code != 0 {
		t.Fatalf(`start child %s exit %d: %s`, ref, code, out)
	}
}

// deliverChild drives a subtask through assign→claim→deliver so it reaches the delivered terminal.
//
// deliverChild 把子任务经 assign→claim→deliver 推到 delivered 终态。
func deliverChild(t *testing.T, dir, ref, agent string) {
	t.Helper()
	steps := [][]string{
		{`task`, `assign`, `--ref`, ref, `--to`, agent, `--role`, `backend`, `--by`, `claude-code`},
		{`task`, `claim`, `--ref`, ref, `--as`, agent},
		{`task`, `deliver`, `--ref`, ref},
	}
	for _, args := range steps {
		if out, _, code := runForge(t, dir, args...); code != 0 {
			t.Fatalf(`deliverChild %s step %v exit %d: %s`, ref, args, code, out)
		}
	}
}

// TestCompleteGenericTask_OrchestrationWarn covers design §5: completing a generic orchestration
// task whose children are NOT all delivered/terminal emits an ADVISORY warn on stderr naming the
// pending children, but STILL completes (设计: 不强制 complete); when all children are delivered it
// completes silently. Uses runForgeStreams so the WARN is asserted on stderr specifically (proving
// it is a non-fatal advisory, not mixed into the stdout result).
//
// TestCompleteGenericTask_OrchestrationWarn 覆盖设计 §5：完成子任务未全交付/终态的 generic 编排任务
// 会在 stderr 发 advisory 告警列出未交付子任务，但「仍完成」（设计：不强制 complete）；全子任务
// delivered 时静默完成。用 runForgeStreams 专断 stderr 含告警（证其为非致命 advisory，未混进 stdout 结果）。
func TestCompleteGenericTask_OrchestrationWarn(t *testing.T) {
	dir := setupDelegateProject(t)

	// Generic parent (the orchestrator) + child-a (delivered, terminal) + child-b (started, pending).
	//
	// generic 父任务（编排器）+ child-a（delivered 终态）+ child-b（已 start，pending）。
	if out, _, code := runForge(t, dir, `task`, `start`, `--kind`, `generic`, `--ref`, `feat/orch`, `--title`, `orch`); code != 0 {
		t.Fatalf(`start parent exit %d: %s`, code, out)
	}
	startChild(t, dir, `feat/child-a`, `feat/orch`)
	deliverChild(t, dir, `feat/child-a`, `kimi`)
	startChild(t, dir, `feat/child-b`, `feat/orch`)

	// 1) Complete with child-b pending → advisory WARN on stderr (not stdout), exit 0, parent done.
	//
	// 1) child-b pending 时 complete → stderr（非 stdout）advisory 告警，exit 0，父任务完成。
	stdout, stderr, code := runForgeStreams(t, dir, `task`, `complete`, `--ref`, `feat/orch`)
	if code != 0 {
		t.Fatalf(`complete exit %d stdout=%q stderr=%q`, code, stdout, stderr)
	}
	if !strings.Contains(stderr, `尚有 1 个子任务未交付`) || !strings.Contains(stderr, `feat/child-b`) {
		t.Errorf(`stderr 应告警 1 个未交付子任务 feat/child-b, got stderr=%q`, stderr)
	}
	if strings.Contains(stderr, `feat/child-a`) {
		t.Errorf(`已交付的 child-a 不应出现在 pending 告警, stderr=%q`, stderr)
	}
	if strings.Contains(stdout, `子任务未交付`) {
		t.Errorf(`告警应在 stderr 不在 stdout, stdout=%q`, stdout)
	}
	orch, err := taskpipeline.LoadTaskState(dir, `feat/orch`)
	if err != nil {
		t.Fatalf(`LoadTaskState feat/orch: %v`, err)
	}
	if !orch.IsComplete() {
		t.Error(`父任务应已完成（advisory 不阻断）`)
	}

	// 2) A fresh generic parent whose ONLY child is delivered → completes with NO warn.
	//
	// 2) 唯一子任务已 delivered 的全新 generic 父任务 → 无告警完成。
	if out, _, code := runForge(t, dir, `task`, `start`, `--kind`, `generic`, `--ref`, `feat/orch2`, `--title`, `orch2`); code != 0 {
		t.Fatalf(`start orch2 exit %d: %s`, code, out)
	}
	startChild(t, dir, `feat/child-c`, `feat/orch2`)
	deliverChild(t, dir, `feat/child-c`, `cursor`)
	stdout2, stderr2, code2 := runForgeStreams(t, dir, `task`, `complete`, `--ref`, `feat/orch2`)
	if code2 != 0 {
		t.Fatalf(`complete orch2 exit %d stdout=%q stderr=%q`, code2, stdout2, stderr2)
	}
	if strings.Contains(stderr2, `子任务未交付`) {
		t.Errorf(`全交付时不应告警, stderr=%q`, stderr2)
	}
}

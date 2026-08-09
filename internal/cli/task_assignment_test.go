package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

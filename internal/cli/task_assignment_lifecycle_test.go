package cli

// task_assignment_lifecycle_test.go —— A2A 分派生命周期端到端：经 CLI 驱动
// start/assign/claim/question/answer/fail/cancel/reopen/deliver
// （setupDelegateProject 夹具），外加 DependsOn 门禁 E2E 与 phase-1 的
// offered-block / task-resume 自动认领推送（自 task_continuity_test.go 按域
// 拆分时迁入）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupDelegateProject 为分派 E2E 测试准备一个隔离的 forge 项目（git + forge init + 初始提交
// + feat/delegate 分支）。CLAUDE_CODE_SESSION_ID 钉为非空，使 claim 的 SetActiveTaskRef 写
// session-scoped active-task-ref 供 resume 读回（生命周期测试断言的锚定副作用）。
// FORGE_DATA_HOME 每测试隔离，分派 task state 不泄漏进真实 ~/.forge。
//
// initGitProject 建一个带初始提交 main.go 的 git 仓并跑 forge init——
// setupDelegateProject 与 all-projects mine 夹具的共享基底（git 身份文本仅
// 夹具用，取值与任何断言无关）。
func initGitProject(t *testing.T, dir string) {
	t.Helper()
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
}

func setupDelegateProject(t *testing.T) string {
	t.Helper()
	t.Setenv("CLAUDE_CODE_SESSION_ID", "delegate-test-sid")
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initGitProject(t, dir)
	runGit(t, dir, "checkout", "-b", "feat/delegate")
	return dir
}

// startChild 经 CLI 启动一个 parent 的子任务（ParentTaskRef=parent）。
func startChild(t *testing.T, dir, ref, parent string) {
	t.Helper()
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, ref, `--parent`, parent, `--title`, ref); code != 0 {
		t.Fatalf(`start child %s exit %d: %s`, ref, code, out)
	}
}

// assignClaim 把任务经 assign→claim 推进，在 deliver 之前停——question/answer
// 与 fail 路径测试的共享前置。
func assignClaim(t *testing.T, dir, ref, agent string) {
	t.Helper()
	steps := [][]string{
		{`task`, `assign`, `--ref`, ref, `--to`, agent, `--by`, `claude-code`},
		{`task`, `claim`, `--ref`, ref, `--as`, agent},
	}
	for _, args := range steps {
		if out, _, code := runForge(t, dir, args...); code != 0 {
			t.Fatalf(`assignClaim %s step %v exit %d: %s`, ref, args, code, out)
		}
	}
}

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

	// generic 父任务（编排器）+ child-a（delivered 终态）+ child-b（已 start，pending）。
	if out, _, code := runForge(t, dir, `task`, `start`, `--kind`, `generic`, `--ref`, `feat/orch`, `--title`, `orch`); code != 0 {
		t.Fatalf(`start parent exit %d: %s`, code, out)
	}
	startChild(t, dir, `feat/child-a`, `feat/orch`)
	deliverChild(t, dir, `feat/child-a`, `kimi`)
	startChild(t, dir, `feat/child-b`, `feat/orch`)

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

// TestTaskAssignClaimDeliver_Lifecycle walks the full A2A delegation lifecycle end-to-end: start → assign (offered) → claim (claimed, session anchored) → deliver (delivered) → mine.
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

// TestTaskDeliver_AsFlag pins the claim/deliver flag parity on hosts where agent identity is undetectable.
//
// TestTaskDeliver_AsFlag 钉住 claim/deliver 的 flag 一致性（usage 日志修复：探测不到
// agent 身份的宿主上 `claim --as kimi` 成功而 `deliver --as kimi` 报 unknown flag，
// 只能裸 deliver 重试）。deliver 的 --as 只是接口一致：身份相符静默交付，不符仍交付
// 但打 stderr advisory——绝不阻断。
func TestTaskDeliver_AsFlag(t *testing.T) {
	dir := setupDelegateProject(t)

	// 相符的 --as：静默交付，无不符 advisory。
	if stdout, _, code := runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "as-flag"); code != 0 {
		t.Fatalf("task start failed: %s", stdout)
	}
	assignClaim(t, dir, "feat/delegate", "kimi")
	stdout, _, code := runForge(t, dir, "task", "deliver", "--ref", "feat/delegate", "--as", "kimi")
	if code != 0 {
		t.Fatalf("deliver --as kimi exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `已交付`) {
		t.Errorf("deliver --as 输出应含「已交付」, got: %s", stdout)
	}
	if strings.Contains(stdout, `不符`) {
		t.Errorf("身份相符不应打不符 advisory, got: %s", stdout)
	}
	state, err := taskpipeline.LoadTaskState(dir, "feat/delegate")
	if err != nil {
		t.Fatal(err)
	}
	if state.Assignment == nil || state.Assignment.Status != taskpipeline.AssignDelivered {
		t.Errorf("deliver --as 后状态应为 delivered, got %+v", state.Assignment)
	}

	// 不符的 --as：仍交付（仅 advisory），警告须点明两个身份。
	if stdout, _, code := runForge(t, dir, "task", "start", "--ref", "feat/delegate-2", "--title", "as-flag-2"); code != 0 {
		t.Fatalf("task start 2 failed: %s", stdout)
	}
	assignClaim(t, dir, "feat/delegate-2", "kimi")
	stdout, _, code = runForge(t, dir, "task", "deliver", "--ref", "feat/delegate-2", "--as", "cursor")
	if code != 0 {
		t.Fatalf("deliver --as 不符也应交付成功（advisory 不阻断）, exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `不符`) || !strings.Contains(stdout, `cursor`) || !strings.Contains(stdout, `kimi`) {
		t.Errorf("不符 advisory 应点明两个身份, got: %s", stdout)
	}
	state2, err := taskpipeline.LoadTaskState(dir, "feat/delegate-2")
	if err != nil {
		t.Fatal(err)
	}
	if state2.Assignment == nil || state2.Assignment.Status != taskpipeline.AssignDelivered {
		t.Errorf("不符 --as 交付后状态仍应为 delivered, got %+v", state2.Assignment)
	}
}

// TestTaskAssign_UnknownAgentWarnAccepted: an agent absent from the known set (codebuddy, which has no project marker) is warned about but still accepted — the soft-validation contract.
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

// TestTaskAssign_RequiresTo: --to is mandatory; omitting it errors with guidance listing the known agents (so the user discovers the valid set from the error itself).
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

// TestTaskAssign_WarnsFindingsVisibleToAssignee (#7, 设计§14): task findings/decisions are cross-agent
// shared — the assignee sees them on claim/resume. assign must warn the orchestrator that recorded
// findings/decisions become visible to the assignee, so sensitive content can be reviewed first.
//
// TestTaskAssign_WarnsFindingsVisibleToAssignee（#7，设计§14）：task 的 findings/decisions 跨 agent
// 共享——被分派方 claim/resume 即见。assign 必须提醒编排器：已记的 findings/decisions 会对被分派方
// 可见，敏感内容可先审视。
func TestTaskAssign_WarnsFindingsVisibleToAssignee(t *testing.T) {
	dir := setupDelegateProject(t)
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/x`, `--title`, `X`); code != 0 {
		t.Fatalf(`start: %s`, out)
	}
	// runForge 用 CombinedOutput，stderr 合入第一返回值，故 output 含 warn。
	output, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/x`, `--to`, `kimi`)
	if code != 0 {
		t.Fatalf(`assign exit %d`, code)
	}
	if !strings.Contains(output, `findings/decisions`) || !strings.Contains(output, `可见`) {
		t.Errorf(`assign 应 warn findings/decisions 对被分派方可见, output=%q`, output)
	}
}

// TestTaskStart_AssigneeWarnAccepted pins that `task start --assignee` shares the warnIfUnknownAgent guard.
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
	assignClaim(t, dir, "feat/delegate", "kimi")

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
		deliverChild(t, dir, "feat/delegate", "kimi")
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
	assignClaim(t, dir, "feat/delegate", "kimi")
	stdout, _, code := runForge(t, dir, "task", "question", "--ref", "feat/delegate")
	if code == 0 {
		t.Fatalf("question without --content should fail, got exit 0: %s", stdout)
	}
	if !strings.Contains(stdout, `--content 必填`) {
		t.Errorf("error should guide with「--content 必填」, got: %s", stdout)
	}
}

// TestTaskAnswer_EmptyAllowed: answer with no --content is accepted (resume-only, no Decision recorded).
//
// TestTaskAnswer_EmptyAllowed：answer 无 --content 被接受（仅恢复 claimed 不记 Decision）——设计契约：
// 空答复仍能解除 input-required 死锁，不强制编排器编造 rationale。
func TestTaskAnswer_EmptyAllowed(t *testing.T) {
	dir := setupDelegateProject(t)
	runForge(t, dir, "task", "start", "--ref", "feat/delegate", "--title", "x")
	assignClaim(t, dir, "feat/delegate", "kimi")
	runForge(t, dir, "task", "question", "--ref", "feat/delegate", "--content", "q")
	stdout, _, code := runForge(t, dir, "task", "answer", "--ref", "feat/delegate")
	if code != 0 {
		t.Fatalf("empty answer should be accepted (resume only), got exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `空答复`) {
		t.Errorf("empty answer should note「空答复」, got: %s", stdout)
	}
}

// TestTaskDependsOn_GateBlocksUntilUpstreamDelivered pins the DependsOn gate (design phase 3).
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
	// BLOCK 还必须在 checklog 记一条 dependency-gate 审计条目（Passed=false）使原因在 forge trace
	// 可见——不留证据的被阻门禁对可观测性是不可见的。断言 executor.go 的 recordAudit(CheckNameDependencyGate) 调用。
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`load checklog: %v`, err)
	}
	foundDepGateBlock := false
	for _, e := range entries {
		if e.TaskRef == `feat/down` && e.Check == taskpipeline.CheckNameDependencyGate && !e.Passed {
			foundDepGateBlock = true
			// Detail（"%s 拒绝：…"）不以 "BLOCKED: " 起头，DeriveLevel 不会判为 blocked——故 executor.go 必须显式
			// 标 Level=LevelBlocked，否则这条 HARD 阻断在 score/dashboard/forge trace 被分桶成普通告警。钉死该显式标注。
			if e.Level != checklog.LevelBlocked {
				t.Errorf(`dependency-gate BLOCKED 条目 Level 应为 %q（HARD 阻断须显式标 blocked，非依赖 DeriveLevel）, got %q`, checklog.LevelBlocked, e.Level)
			}
			break
		}
	}
	if !foundDepGateBlock {
		t.Errorf(`task-verify 因依赖未交付 BLOCKED 应记一条 dependency-gate(Passed=false) checklog, got %d 条: %+v`, len(entries), entries)
	}

	// 让上游交付（assignment-delivered，无需过上游门禁）
	deliverChild(t, dir, `feat/up`, `kimi`)

	// 再过 B 的 task-verify：DependsOn 已满足，不再因依赖 BLOCKED（可能因 prerequisite 等其他原因 BLOCK，
	// 但绝不应再是「上游未交付」——这正是门禁放行依赖的信号）
	stdout, _, _ = runForge(t, dir, `task`, `gate`, `task-verify`, `--ref`, `feat/down`)
	if strings.Contains(stdout, `上游 task 未交付`) {
		t.Errorf(`上游已交付后不应再因依赖 BLOCKED, got: %s`, stdout)
	}
}

// TestTaskStart_DependsOnCycleRejected: AddDependency's cycle check has teeth at the CLI.
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

// TestTaskDependsOn_GateCompleteAlsoBlocks is a regression guard: the DependsOn gate condition is `gateID == task-verify || gateID == task-complete`.
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

// ptrHoursAgo returns a pointer to a time n hours in the past — for explicit NotifiedAt/OfferedAt
// in tests that must not depend on Windows' ~15ms clock resolution (near-simultaneous time.Now()
// calls are flaky there).
func ptrHoursAgo(n int) *time.Time {
	t := time.Now().Add(-time.Duration(n) * time.Hour)
	return &t
}

// offeredKimiTask builds an incomplete task offered to kimi (Status=offered, OfferedAt=now). parent
// is the optional ParentTaskRef (empty = not in a chain). Stands up the offered-to-me population
// that appendOfferedBlock filters + renders. Mirrors what TaskState.AssignTo would produce.
func offeredKimiTask(ref, parent, summary string) *taskpipeline.TaskState {
	now := time.Now()
	return &taskpipeline.TaskState{
		TaskRef:       ref,
		Branch:        ref,
		ParentTaskRef: parent,
		Summary:       summary,
		Assignment: &taskpipeline.Assignment{
			Agent:     `kimi`,
			Role:      `frontend`,
			Status:    taskpipeline.AssignOffered,
			OfferedBy: `claude-code`,
			OfferedAt: &now,
		},
	}
}

func saveAll(t *testing.T, root string, states ...*taskpipeline.TaskState) {
	t.Helper()
	for _, s := range states {
		if e := taskpipeline.SaveTaskState(root, s); e != nil {
			t.Fatalf(`SaveTaskState %s: %v`, s.TaskRef, e)
		}
	}
}

// noOfferedEnv sets the env that makes ActiveTaskState fall to the inventory branch (no session
// ref, branch won't match) while detectOriginTool resolves to kimi — the no-active offered-block
// case. CLAUDE_CODE_SESSION_ID is cleared so FORGE_AGENT=kimi wins detection unambiguously.
func noOfferedEnv(t *testing.T) {
	t.Helper()
	t.Setenv(`FORGE_AGENT`, `kimi`)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	t.Setenv(`FORGE_SESSION_ID`, ``)
}

// kimiSessionEnv 设置 hook 派生 kimi session 的 env（FORGE_AGENT=kimi、sid 走
// FORGE_SESSION_ID、清 claude 兜底）并锚定该 session 的 active-task-ref——
// active-in-chain offered-block 测试共享的前置（env 顺序对 saveAll 无关，
// 故可在种入之后执行）。
func kimiSessionEnv(t *testing.T, root, sid, activeRef string) {
	t.Helper()
	t.Setenv(`FORGE_AGENT`, `kimi`)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	t.Setenv(`FORGE_SESSION_ID`, sid)
	if e := taskpipeline.SetActiveTaskRef(root, sid, activeRef); e != nil {
		t.Fatalf(`SetActiveTaskRef: %v`, e)
	}
}

// TestOfferedChainSiblings pins the v1 chain definition: same ParentTaskRef (exact match), active
// excluded from its own sibling set, and nil when active is nil or has no parent.
func TestOfferedChainSiblings(t *testing.T) {
	active := &taskpipeline.TaskState{TaskRef: `feat/me`, ParentTaskRef: `feat/orch`}
	sib := func(ref string) *taskpipeline.TaskState {
		return &taskpipeline.TaskState{TaskRef: ref, ParentTaskRef: `feat/orch`}
	}
	if got := offeredChainSiblings(nil, []*taskpipeline.TaskState{sib(`feat/a`)}); got != nil {
		t.Errorf(`nil active 须返 nil，实得 %v`, got)
	}
	noParent := &taskpipeline.TaskState{TaskRef: `feat/me`}
	if got := offeredChainSiblings(noParent, []*taskpipeline.TaskState{sib(`feat/a`)}); got != nil {
		t.Errorf(`active 无 ParentTaskRef 须返 nil，实得 %v`, got)
	}
	offered := []*taskpipeline.TaskState{
		sib(`feat/a`),
		{TaskRef: `feat/me`, ParentTaskRef: `feat/orch`},
		{TaskRef: `feat/b`, ParentTaskRef: `feat/other`},
		sib(`feat/c`),
	}
	got := offeredChainSiblings(active, offered)
	if len(got) != 2 {
		t.Fatalf(`应得 2 个同链兄弟（feat/a, feat/c），实得 %d: %v`, len(got), got)
	}
	refs := map[string]bool{}
	for _, s := range got {
		refs[s.TaskRef] = true
	}
	if !refs[`feat/a`] || !refs[`feat/c`] || refs[`feat/me`] {
		t.Errorf(`同链兄弟集错误，应只含 feat/a 与 feat/c（排除自身），实得 %v`, refs)
	}
}

// TestOfferedBlock_AppendAdditive: with no active task, the inventory AND the offered one-liner
// both appear (additive — inventory is not replaced), and NotifiedAt is stamped on emit.
func TestOfferedBlock_AppendAdditive(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	noOfferedEnv(t)
	saveAll(t, root,
		offeredKimiTask(`feat/a`, ``, `任务甲`),
		offeredKimiTask(`feat/b`, ``, `任务乙`),
	)
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`renderHookResume: %v`, err)
	}
	if !strings.Contains(out, `feat/a`) || !strings.Contains(out, `feat/b`) {
		t.Errorf(`additive: 盘点应仍在（feat/a/feat/b），实得 %q`, out)
	}
	if !strings.Contains(out, `待认领`) || !strings.Contains(out, `本 project 有 2 个待认领`) {
		t.Errorf(`应附加 one-liner「本 project 有 2 个待认领」，实得 %q`, out)
	}
	for _, ref := range []string{`feat/a`, `feat/b`} {
		s, _ := taskpipeline.LoadTaskState(root, ref)
		if s == nil || s.Assignment == nil || s.Assignment.NotifiedAt == nil {
			t.Errorf(`%s 推送后应落 NotifiedAt，state=%v`, ref, s)
		}
	}
}

// TestOfferedBlock_DedupAndReNotify pins the NotifiedAt wiring end-to-end: first call emits +
// stamps; second call is suppressed (NotifiedAt >= OfferedAt); a genuine re-offer (OfferedAt
// bumped past NotifiedAt) re-notifies only the re-offered task.
func TestOfferedBlock_DedupAndReNotify(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	noOfferedEnv(t)
	saveAll(t, root,
		offeredKimiTask(`feat/a`, ``, `任务甲`),
		offeredKimiTask(`feat/b`, ``, `任务乙`),
	)
	out1, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`第 1 次 renderHookResume: %v`, err)
	}
	if !strings.Contains(out1, `待认领`) {
		t.Fatalf(`首次应推送待认领，实得 %q`, out1)
	}
	out2, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`第 2 次 renderHookResume: %v`, err)
	}
	if strings.Contains(out2, `待认领`) {
		t.Errorf(`第 2 次应去重不推送，实得 %q`, out2)
	}
	// re-offer B: bump OfferedAt past its NotifiedAt → fresh again → re-notify (only B).
	b, _ := taskpipeline.LoadTaskState(root, `feat/b`)
	if b == nil || b.Assignment == nil || b.Assignment.NotifiedAt == nil {
		t.Fatalf(`feat/b 应已被首次推送设 NotifiedAt，state=%v`, b)
	}
	now := time.Now()
	b.Assignment.OfferedAt = &now
	if e := taskpipeline.SaveTaskState(root, b); e != nil {
		t.Fatalf(`SaveTaskState feat/b: %v`, e)
	}
	out3, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`re-offer 后 renderHookResume: %v`, err)
	}
	if !strings.Contains(out3, `待认领`) {
		t.Errorf(`re-offer 后应重新推送待认领，实得 %q`, out3)
	}
	if !strings.Contains(out3, `本 project 有 1 个待认领`) {
		t.Errorf(`re-offer 后应只剩 feat/b（1 个）待认领，实得 %q`, out3)
	}
}

// TestOfferedBlock_WithActiveNotInChain: an active task with no ParentTaskRef collapses the offered
// set to a one-liner (the active-is-orchestrator case — orchestrators use task mine; the push is
// worker-facing). Handoff stays intact.
func TestOfferedBlock_WithActiveNotInChain(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	active := &taskpipeline.TaskState{TaskRef: `feat/me`, Branch: `feat/me`, Goal: `编排主任务`}
	saveAll(t, root, active,
		offeredKimiTask(`feat/a`, ``, `任务甲`),
		offeredKimiTask(`feat/b`, ``, `任务乙`),
	)
	kimiSessionEnv(t, root, `sid-x`, `feat/me`)
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`renderHookResume: %v`, err)
	}
	if !strings.Contains(out, `待认领`) || !strings.Contains(out, `本 project 有 2 个待认领`) {
		t.Errorf(`应 one-liner「2 个待认领」，实得 %q`, out)
	}
	if strings.Contains(out, `同链待认领`) {
		t.Errorf(`active 无 ParentTaskRef 不应进同链分档，实得 %q`, out)
	}
	if !strings.Contains(out, `编排主任务`) {
		t.Errorf(`handoff 应仍在（编排主任务），实得 %q`, out)
	}
}

// TestOfferedBlock_WithActiveInChain: an active task inside an orchestration chain lists same-chain
// offered siblings (ready-ordered) and folds non-siblings into a count — never the one-liner.
func TestOfferedBlock_WithActiveInChain(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	active := &taskpipeline.TaskState{TaskRef: `feat/me`, Branch: `feat/me`, ParentTaskRef: `feat/orch`, Goal: `编排链主任务`}
	saveAll(t, root, active,
		offeredKimiTask(`feat/sib-a`, `feat/orch`, `兄弟甲`),
		offeredKimiTask(`feat/sib-b`, `feat/orch`, `兄弟乙`),
		offeredKimiTask(`feat/other`, `feat/x`, `非同链`),
	)
	kimiSessionEnv(t, root, `sid-x`, `feat/me`)
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`renderHookResume: %v`, err)
	}
	if !strings.Contains(out, `同链待认领`) {
		t.Errorf(`应进同链分档，实得 %q`, out)
	}
	if !strings.Contains(out, `feat/sib-a`) || !strings.Contains(out, `feat/sib-b`) {
		t.Errorf(`应列出同链兄弟 feat/sib-a/feat/sib-b，实得 %q`, out)
	}
	if !strings.Contains(out, `另有 1 个非同链待认领`) {
		t.Errorf(`应附「另有 1 个非同链」，实得 %q`, out)
	}
	if strings.Contains(out, `本 project 有`) {
		t.Errorf(`同链分档时不应出 one-liner，实得 %q`, out)
	}
}

// TestOfferedBlock_ReadinessMarker: a sibling with an undelivered DependsOn is marked ⏳阻塞中;
// one with no pending deps is marked ✅可开干. PendingDependencies is the same primitive the
// DependsOn gate + task mine --blocked use, so push and gate cannot disagree.
func TestOfferedBlock_ReadinessMarker(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	active := &taskpipeline.TaskState{TaskRef: `feat/me`, Branch: `feat/me`, ParentTaskRef: `feat/orch`, Goal: `编排链主任务`}
	dep := &taskpipeline.TaskState{TaskRef: `feat/dep`, Branch: `feat/dep`, Goal: `前置依赖`} // incomplete → blocks
	sibReady := offeredKimiTask(`feat/sib-ready`, `feat/orch`, `就绪兄弟`)
	sibBlocked := offeredKimiTask(`feat/sib-blocked`, `feat/orch`, `阻塞兄弟`)
	sibBlocked.DependsOn = []string{`feat/dep`}
	saveAll(t, root, active, dep, sibReady, sibBlocked)
	kimiSessionEnv(t, root, `sid-x`, `feat/me`)
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`renderHookResume: %v`, err)
	}
	if !strings.Contains(out, `✅可开干`) {
		t.Errorf(`就绪兄弟应标 ✅可开干，实得 %q`, out)
	}
	if !strings.Contains(out, `⏳阻塞中`) {
		t.Errorf(`依赖未交付的兄弟应标 ⏳阻塞中，实得 %q`, out)
	}
}

// TestOfferedBlock_AgentEmptySkip: when the agent can't be attributed (codex/cursor/opencode/
// codebuddy gap), appendOfferedBlock is a clean no-op — output unchanged, zero NotifiedAt mutation.
func TestOfferedBlock_AgentEmptySkip(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	t.Setenv(`FORGE_AGENT`, ``)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	t.Setenv(`FORGE_SESSION_ID`, ``)
	saveAll(t, root,
		offeredKimiTask(`feat/a`, ``, `任务甲`),
		offeredKimiTask(`feat/b`, ``, `任务乙`),
	)
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`renderHookResume: %v`, err)
	}
	if strings.Contains(out, `待认领`) {
		t.Errorf(`agent 未知时应 no-op（不附加块），实得 %q`, out)
	}
	for _, ref := range []string{`feat/a`, `feat/b`} {
		s, _ := taskpipeline.LoadTaskState(root, ref)
		if s != nil && s.Assignment != nil && s.Assignment.NotifiedAt != nil {
			t.Errorf(`%s no-op 时不应落 NotifiedAt，state=%v`, ref, s)
		}
	}
}

// TestOfferedBlock_ZombieExcluded: a >7d-stale offered task is an offered-zombie — excluded from
// the push (zombies surface via task mine/dashboard, not the per-session push) and gets no
// NotifiedAt.
func TestOfferedBlock_ZombieExcluded(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	noOfferedEnv(t)
	zombie := offeredKimiTask(`feat/zombie`, ``, `僵尸 offer`)
	zombie.Assignment.OfferedAt = ptrHoursAgo(10 * 24) // >7d stale → offered-zombie
	saveAll(t, root, zombie,
		&taskpipeline.TaskState{TaskRef: `feat/plain`, Branch: `feat/plain`, Goal: `普通任务`},
	)
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`renderHookResume: %v`, err)
	}
	if strings.Contains(out, `待认领`) {
		t.Errorf(`offered 僵尸应排除不推送，实得 %q`, out)
	}
	z, _ := taskpipeline.LoadTaskState(root, `feat/zombie`)
	if z != nil && z.Assignment != nil && z.Assignment.NotifiedAt != nil {
		t.Errorf(`僵尸排除时不应落 NotifiedAt，state=%v`, z)
	}
}

// TestOfferedBlock_DoesNotAlterHandoff: when the active task is itself offered-to-me (resolved via
// SetActiveTaskRef), it is handed off AND excluded from the offered block — listing it as 待认领
// while handing it off would be contradictory. Block shows only the other offered task (count 1).
func TestOfferedBlock_DoesNotAlterHandoff(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	active := offeredKimiTask(`feat/me`, ``, `被接手的 offered 任务`)
	saveAll(t, root, active, offeredKimiTask(`feat/other`, ``, `另一个 offered`))
	kimiSessionEnv(t, root, `sid-x`, `feat/me`)
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf(`renderHookResume: %v`, err)
	}
	if !strings.Contains(out, `feat/me`) {
		t.Errorf(`handoff 应含 feat/me，实得 %q`, out)
	}
	if !strings.Contains(out, `本 project 有 1 个待认领`) {
		t.Errorf(`offered 块应只 1 个（feat/other，排除 active），实得 %q`, out)
	}
	if strings.Contains(out, `本 project 有 2 个待认领`) {
		t.Errorf(`active 不应被重复列为待认领，实得 %q`, out)
	}
}

// TestTaskResume_AutoClaim merges the five resume auto-claim scenarios (design §3: resume of an offered-to-me task auto-claims it, killing the manual `task claim` step).
//
// TestTaskResume_AutoClaim 合并五个 resume 自动认领场景（设计 §3：resume 一个
// 分派给我的 offered 任务即自动认领，砍掉手动 task claim 步骤）。每行的
// check 闭包逐一保留被吸收测试的断言：快乐路径（offered→claimed + stderr
// 提示 + session 锚定）、普通任务不认领、派给他 agent 的不认领、agent 探测
// 不到时跳过、--json（认领照旧生效、提示被抑制、输出保持合法 JSON）。
func TestTaskResume_AutoClaim(t *testing.T) {
	cases := []struct {
		name       string
		title      string
		assignTo   string // "" = skip the assign step (never offered)
		assignRole string
		agent      string // FORGE_AGENT during resume
		clearSid   bool   // also clear CLAUDE_CODE_SESSION_ID (undetectable identity)
		resumeArgs []string
		check      func(t *testing.T, dir, out string, st *taskpipeline.TaskState)
	}{
		{
			// 快乐路径：offered→claimed、stderr 提示、session 锚定。
			name: "happy path", title: `被分派`, assignTo: `kimi`, assignRole: `frontend`, agent: `kimi`,
			resumeArgs: []string{`task`, `resume`, `--ref`, `feat/delegate`},
			check: func(t *testing.T, dir, out string, st *taskpipeline.TaskState) {
				if !strings.Contains(out, `已自动认领任务 feat/delegate（kimi）`) {
					t.Errorf(`应 stderr 提示已自动认领，实得 %q`, out)
				}
				if st == nil || st.Assignment == nil || st.Assignment.Status != taskpipeline.AssignClaimed {
					t.Fatalf(`auto-claim 后应 claimed，state=%v`, st)
				}
				if got := taskpipeline.ReadActiveTaskRef(dir, `delegate-test-sid`); got != `feat/delegate` {
					t.Errorf(`应锚定 active-task-ref-delegate-test-sid=feat/delegate，实得 %q`, got)
				}
			},
		},
		{
			// 普通（从未分派的）任务不自动认领。
			name: "not offered", title: `普通任务`, agent: `kimi`,
			resumeArgs: []string{`task`, `resume`, `--ref`, `feat/delegate`},
			check: func(t *testing.T, dir, out string, st *taskpipeline.TaskState) {
				if strings.Contains(out, `已自动认领`) {
					t.Errorf(`未分派的任务不应自动认领，实得 %q`, out)
				}
				if st == nil || st.Assignment != nil {
					t.Fatalf(`未分派任务应无 Assignment，state=%v`, st)
				}
			},
		},
		{
			// 派给 reasonix 的任务 kimi 不自动认领。
			name: "other agent", title: `派给 reasonix`, assignTo: `reasonix`, assignRole: `backend`, agent: `kimi`,
			resumeArgs: []string{`task`, `resume`, `--ref`, `feat/delegate`},
			check: func(t *testing.T, dir, out string, st *taskpipeline.TaskState) {
				if strings.Contains(out, `已自动认领`) {
					t.Errorf(`派给 reasonix 的任务 kimi 不应自动认领，实得 %q`, out)
				}
				if st == nil || st.Assignment == nil || st.Assignment.Status != taskpipeline.AssignOffered || st.Assignment.Agent != `reasonix` {
					t.Fatalf(`应仍 offered 给 reasonix，state=%v`, st)
				}
			},
		},
		{
			// agent 探测不到时跳过自动认领——任务保持 offered。（IsOfferedTo 与
			// Claim 之间真实的 TOCTOU 竞态本质非确定，此处不断言；非致命错误路径
			// 由 review 覆盖。）
			name: "no agent", title: `被分派`, assignTo: `kimi`, assignRole: `frontend`, agent: ``, clearSid: true,
			resumeArgs: []string{`task`, `resume`, `--ref`, `feat/delegate`, `--no-attach`},
			check: func(t *testing.T, dir, out string, st *taskpipeline.TaskState) {
				if strings.Contains(out, `已自动认领`) {
					t.Errorf(`agent 未知时不应自动认领，实得 %q`, out)
				}
				if st == nil || st.Assignment == nil || st.Assignment.Status != taskpipeline.AssignOffered {
					t.Fatalf(`应仍 offered（未认领），state=%v`, st)
				}
			},
		},
		{
			// --json：auto-claim 照旧生效（状态反映在 JSON）但 stderr 提示被抑制，
			// 输出保持合法 JSON。
			name: "json suppresses notice", title: `被分派`, assignTo: `kimi`, assignRole: `frontend`, agent: `kimi`,
			resumeArgs: []string{`task`, `resume`, `--ref`, `feat/delegate`, `--json`, `--no-attach`},
			check: func(t *testing.T, dir, out string, st *taskpipeline.TaskState) {
				if strings.Contains(out, `已自动认领`) {
					t.Errorf(`--json 下应抑制 auto-claim stderr，实得 %q`, out)
				}
				var v interface{}
				if e := json.Unmarshal([]byte(out), &v); e != nil {
					t.Fatalf(`--json 输出应为合法 JSON: %v；实得 %q`, e, out)
				}
				if !strings.Contains(out, `claimed`) {
					t.Errorf(`JSON 应含 claimed（auto-claim 已生效），实得 %q`, out)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupDelegateProject(t)
			if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/delegate`, `--title`, tc.title); code != 0 {
				t.Fatalf(`task start exit %d: %s`, code, out)
			}
			if tc.assignTo != "" {
				if out, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/delegate`, `--to`, tc.assignTo, `--role`, tc.assignRole, `--by`, `claude-code`); code != 0 {
					t.Fatalf(`task assign exit %d: %s`, code, out)
				}
			}
			t.Setenv(`FORGE_AGENT`, tc.agent)
			if tc.clearSid {
				t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
			}
			out, _, code := runForge(t, dir, tc.resumeArgs...)
			if code != 0 {
				t.Fatalf(`resume exit %d: %s`, code, out)
			}
			st, _ := taskpipeline.LoadTaskState(dir, `feat/delegate`)
			tc.check(t, dir, out, st)
		})
	}
}

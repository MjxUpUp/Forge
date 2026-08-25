package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupAcceptanceTask creates a session-scoped active task with the given acceptance criteria, returning (dir, taskRef).
// Reuses the pattern from #2 review_status_test: SetActiveTaskRef + SaveTaskState, so runTaskVerifyAcceptanceAt
// can find the task via ActiveTaskState(sessionID) (rather than a stale shared file).
//
// setupAcceptanceTask 建一个 session-scoped 活动任务并写入给定验收标准，返回 (dir, taskRef)。
// 复用 #2 review_status_test 的范式：SetActiveTaskRef + SaveTaskState，让 runTaskVerifyAcceptanceAt
// 经 ActiveTaskState(sessionID) 能找到任务（而非陈旧共享文件）。
func setupAcceptanceTask(t *testing.T, acceptRaw []string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	const sid = `test-session-accept`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)
	const taskRef = `feat/spec-persist`
	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	state := &taskpipeline.TaskState{
		TaskRef:    taskRef,
		SessionID:  sid,
		Branch:     `feat/spec-persist`,
		StartedAt:  time.Now(),
		Acceptance: taskpipeline.ParseAcceptance(acceptRaw),
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	return dir, taskRef
}

// findAcceptanceEntry finds the CheckNameAcceptance entry in checklog (pointer for convenient field access).
//
// findAcceptanceEntry 在 checklog 里找 CheckNameAcceptance 条目（指针，便于读字段）。
func findAcceptanceEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == taskpipeline.CheckNameAcceptance {
			return &entries[i]
		}
	}
	return nil
}

// TestRunTaskVerifyAcceptanceAt_RecordsDeterministic is the core guard for #3: after green acceptance criteria are actually run,
// checklog must record a CheckNameAcceptance entry with Passed=true, Source=deterministic (forge runs the
// command itself to see the result, unforgeable), and TaskState.Acceptance[].Passed is backfilled to true. This is the wire point turning
// dev-workflow Plan Run+Expected from plan text into unforgeable evidence — VerifyAcceptance / recording wiring /
// Source bucketing, any break is caught.
//
// TestRunTaskVerifyAcceptanceAt_RecordsDeterministic 是 #3 的核心守卫：绿验收标准实跑后，
// checklog 必须记 CheckNameAcceptance 条目、Passed=true、Source=deterministic（forge 自己跑
// 命令看结果，不可伪造），且 TaskState.Acceptance[].Passed 回填为 true。这是把 dev-workflow
// Plan 的 "Run+Expected" 从 plan 文本变成不可伪造证据的接入点——VerifyAcceptance / 记录接线 /
// Source 分桶任一断裂即被抓。
func TestRunTaskVerifyAcceptanceAt_RecordsDeterministic(t *testing.T) {
	dir, taskRef := setupAcceptanceTask(t, []string{`go version :: go version`})

	var runErr error
	out := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr != nil {
		t.Fatalf(`green acceptance should not error: %v`, runErr)
	}

	// TaskState backfill: Passed=true.
	//
	// TaskState 回填：Passed=true
	loaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if !loaded.Acceptance[0].Passed {
		t.Errorf(`criterion Passed 未回填为 true（实跑结果未落盘）`)
	}

	// checklog: CheckNameAcceptance / Passed=true / deterministic.
	//
	// checklog：CheckNameAcceptance / Passed=true / deterministic
	rec := findAcceptanceEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckNameAcceptance entry 未记录——verify-acceptance 未把实跑结果写入 checklog`)
	}
	if !rec.Passed {
		t.Errorf(`acceptance Passed = false on green, want true`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`acceptance Source = %s, want deterministic（forge 实跑应归 deterministic，非 agent-claim）`, rec.Source)
	}
	if !strings.Contains(out, `全部通过`) {
		t.Errorf(`输出缺"全部通过"摘要: %s`, out)
	}
}

// TestRunTaskVerifyAcceptanceAt_RecordsFailure pins down the RED path: failed acceptance criteria also record
// a CheckNameAcceptance entry (Passed=false, Checked=true, deterministic), return a non-nil error, and the failed
// criterion backfills Output for debugging. Without this test, if someone later moves checklog.Record into the `if allPassed`
// branch, failure evidence would be silently dropped while green-only tests still pass — exactly the agent-self-claim-satisfies-acceptance blind spot that #3 plugs.
//
// TestRunTaskVerifyAcceptanceAt_RecordsFailure 钉住 RED 路径：失败的验收标准也照常记一条
// CheckNameAcceptance（Passed=false、Checked=true、deterministic）、返回非 nil error，且失败
// criterion 回填 Output 供排查。没有本测试，未来若有人把 checklog.Record 挪进 `if allPassed`
// 分支，失败证据会被静默丢弃而 green-only 测试照过——正中 #3 要堵的"agent 自述满足验收"盲区。
func TestRunTaskVerifyAcceptanceAt_RecordsFailure(t *testing.T) {
	dir, taskRef := setupAcceptanceTask(t, []string{`go version :: NONEXISTENT_SUBSTRING`})

	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr == nil {
		t.Fatal(`failing acceptance should return a non-nil error`)
	}

	loaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if loaded.Acceptance[0].Passed {
		t.Errorf(`criterion Passed = true on red, want false（期望子串缺失应判失败）`)
	}
	if loaded.Acceptance[0].Output == `` {
		t.Errorf(`失败 criterion 应回填 Output 供排查（实跑输出未落盘）`)
	}

	rec := findAcceptanceEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckNameAcceptance entry 未记录 on failure——fail path 丢弃了记录`)
	}
	if rec.Passed {
		t.Errorf(`acceptance Passed = true on red, want false`)
	}
	if !rec.Checked {
		t.Errorf(`acceptance Checked = false, want true（失败也应标记已检查）`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`acceptance Source = %s, want deterministic（forge 实跑的失败也是 deterministic）`, rec.Source)
	}
}

// TestRunTaskVerifyAcceptanceAt_NoAcceptanceSilent verifies that tasks without registered acceptance criteria exit silently,
// write no checklog (no noise entries), and return no error.
//
// TestRunTaskVerifyAcceptanceAt_NoAcceptanceSilent 验证未登记验收标准的任务静默退出、
// 不写 checklog（不留噪声条目），且不报错。
func TestRunTaskVerifyAcceptanceAt_NoAcceptanceSilent(t *testing.T) {
	// No acceptance criteria.
	dir, _ := setupAcceptanceTask(t, nil) // 无验收标准

	var runErr error
	out := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr != nil {
		t.Fatalf(`no-acceptance path should not error: %v`, runErr)
	}
	if !strings.Contains(out, `未登记`) {
		t.Errorf(`无验收标准时应提示"未登记": %s`, out)
	}
	entries, _ := checklog.LoadAll(dir)
	if len(entries) != 0 {
		t.Errorf(`无验收标准 → 不应写 checklog 条目，got %d`, len(entries))
	}
}

// TestTaskStart_AcceptGoTestAutoVerbose pins the CLI wiring of the go-test -v
// auto-fill: `task start --accept "go test ./... :: PASS"` must persist the criterion
// ALREADY rewritten to `go test -v ./...` (verify-acceptance later runs exactly what
// start persisted) and must announce the rewrite in the start output (never a silent
// rewrite). A bare exit-code-only criterion must pass through untouched.
//
// TestTaskStart_AcceptGoTestAutoVerbose 钉住 go test 自动补 -v 的 CLI 接线：
// `task start --accept "go test ./... :: PASS"` 落盘的必须是已改写的
// `go test -v ./...`（verify-acceptance 之后实跑的正是 start 落盘的命令），且
// start 输出必须明示改写（绝不静默）。只看退出码的裸命令原样不动。
func TestTaskStart_AcceptGoTestAutoVerbose(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-accept-gov`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}
	out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/gov`,
		`--accept`, `go test ./... :: PASS`,
		`--accept`, `go build ./...`)
	if code != 0 {
		t.Fatalf(`task start failed: %s`, out)
	}
	if !strings.Contains(out, `自动补 -v`) || !strings.Contains(out, `go test ./...`) {
		t.Errorf(`start 输出应明示 go test 自动补 -v 及原命令: %s`, out)
	}
	loaded, err := taskpipeline.LoadTaskState(dir, `feat/gov`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if len(loaded.Acceptance) != 2 {
		t.Fatalf(`应落盘 2 条验收标准, got %d`, len(loaded.Acceptance))
	}
	if got := loaded.Acceptance[0].Run; got != `go test -v ./...` {
		t.Errorf(`go test + Expected 应自动补 -v, got %q`, got)
	}
	if got := loaded.Acceptance[1].Run; got != `go build ./...` {
		t.Errorf(`非 go test 命令不应改写, got %q`, got)
	}
}

// TestRunTaskVerifyAcceptanceAt_ExplicitRef pins `task verify-acceptance --ref`
// (gate-family --ref parity; usage-log fix: every gate command took --ref except
// verify-acceptance, which errored "unknown flag"). A task that is NOT the session's
// active task must still verify when pinned by ref; a nonexistent ref must error
// instead of falling back to active detection.
//
// TestRunTaskVerifyAcceptanceAt_ExplicitRef 钉住 `task verify-acceptance --ref`
// （门禁族 --ref 一致性；usage 日志修复：gate 族命令都认 --ref 唯独它报 unknown
// flag）。非本 session 活跃任务的 task 经 --ref 指定也必须能验收；不存在的 ref
// 必须报错而非回落活跃检测。
func TestRunTaskVerifyAcceptanceAt_ExplicitRef(t *testing.T) {
	// Task with acceptance criteria but NOT active (no SetActiveTaskRef). A second
	// incomplete task makes active-task detection ambiguous (the priority-3 fallback
	// needs exactly one), so the bare call must error and only --ref can route.
	//
	// 带验收标准但不活跃的任务（不 SetActiveTaskRef）。第二个未完成任务让活跃检测
	// 无歧义兜底可吃（优先级 3 兜底要求恰好一个），故裸调用必须报错、只有 --ref 能路由。
	dir := t.TempDir()
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `test-session-accept-ref`)
	const taskRef = `feat/accept-ref`
	state := &taskpipeline.TaskState{
		TaskRef:    taskRef,
		Branch:     `feat/accept-ref`,
		StartedAt:  time.Now(),
		Acceptance: taskpipeline.ParseAcceptance([]string{`go version :: go version`}),
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SaveTaskState(dir, &taskpipeline.TaskState{
		TaskRef: `feat/accept-other`, Branch: `feat/accept-other`, StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// Without --ref and no unambiguous active task: must error (legacy behavior
	// unchanged).
	//
	// 不带 --ref 且无明确活跃任务：必须报错（旧行为不变）。
	var noRefErr error
	_ = captureStdout(t, func() { noRefErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if noRefErr == nil {
		t.Fatal(`无明确活跃任务且不带 --ref 应报错`)
	}

	// --ref pins the task explicitly: runs, passes, backfills.
	//
	// --ref 显式指定任务：实跑、通过、回填。
	var runErr error
	out := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, taskRef, false) })
	if runErr != nil {
		t.Fatalf(`verify-acceptance --ref should not error: %v`, runErr)
	}
	if !strings.Contains(out, `全部通过`) {
		t.Errorf(`输出缺"全部通过"摘要: %s`, out)
	}
	loaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if !loaded.Acceptance[0].Passed {
		t.Errorf(`--ref 任务的 criterion Passed 未回填为 true`)
	}

	// Nonexistent ref → hard error, no silent fallback.
	//
	// 不存在的 ref → 硬报错，不静默回落。
	var ghostErr error
	_ = captureStdout(t, func() { ghostErr = runTaskVerifyAcceptanceAt(dir, `feat/ghost`, false) })
	if ghostErr == nil {
		t.Fatal(`--ref 指向不存在任务应报错`)
	}
}

// TestRunTaskVerifyAcceptanceAt_ForeignRequiresTrust pins the foreign-acceptance trust gate (2026-08-15
// trust-boundary fix): a task whose acceptance came from an untrusted source (task import / .forge
// migrate → AcceptanceForeign=true) must NOT execute any Run command without --trust-foreign — the
// command list is printed for human review instead, and re-running without the flag cannot shake the
// marker off. With --trust-foreign, the first run clears the marker and executes normally.
//
// TestRunTaskVerifyAcceptanceAt_ForeignRequiresTrust 钉住外来验收受信门（2026-08-15 信任边界
// 修复）：验收来自不可信源的任务（task import / .forge migrate → AcceptanceForeign=true）在无
// --trust-foreign 时绝不能执行任何 Run 命令——改为打印命令清单供人工审阅，且不带 flag 重跑
// 无法甩掉标记。带 --trust-foreign 的首次运行清除标记并正常执行。
func TestRunTaskVerifyAcceptanceAt_ForeignRequiresTrust(t *testing.T) {
	dir, taskRef := setupAcceptanceTask(t, []string{`go version :: go version`})

	// Simulate the foreign marker exactly as StripForeignGateSignals sets it (import/migrate path).
	//
	// 按 StripForeignGateSignals（import/migrate 路径）设置外来标记，模拟同一状态。
	if err := taskpipeline.MutateTaskState(dir, taskRef, func(s *taskpipeline.TaskState) error {
		s.AcceptanceForeign = true
		return nil
	}); err != nil {
		t.Fatalf(`MutateTaskState: %v`, err)
	}

	// 1. Without --trust-foreign: refuse BEFORE any execution — criterion untouched, error returned,
	//    command list printed for review, and the foreign marker PERSISTS (no save-side shake-off).
	//
	// 1. 无 --trust-foreign：在任何执行前拒绝——criterion 不被跑、返回 error、打印命令清单
	//    供审阅，且外来标记保留（落盘侧甩不掉）。
	var runErr error
	out := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr == nil {
		t.Fatal(`foreign acceptance without --trust-foreign must error（未审阅的外来命令不得执行）`)
	}
	if !strings.Contains(out, `--trust-foreign`) {
		t.Errorf(`refusal output should point at --trust-foreign, got: %s`, out)
	}
	if !strings.Contains(out, `go version`) {
		t.Errorf(`refusal output should list the command for review, got: %s`, out)
	}
	refused, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if refused.Acceptance[0].Passed || refused.Acceptance[0].Output != `` {
		t.Errorf(`refusal must not execute the criterion: Passed=%v Output=%q`, refused.Acceptance[0].Passed, refused.Acceptance[0].Output)
	}
	if !refused.AcceptanceForeign {
		t.Errorf(`foreign marker must persist after an untrusted refusal（不带 flag 重跑不能甩掉标记）`)
	}
	entries, _ := checklog.LoadAll(dir)
	if len(entries) != 0 {
		t.Errorf(`refusal path should record no checklog evidence, got %d entries`, len(entries))
	}

	// 2. With --trust-foreign: marker cleared once, commands actually run (green), evidence recorded.
	//    The human-terminal discriminator must be overridden to TRUE for this leg — `go test`
	//    stdin is whatever the test runner got (often NUL/dev-null, char-device on some
	//    platforms, pipe on others), so the env-dependent default would make the trusted leg
	//    flaky across shells. The false side is pinned separately below.
	//
	// 2. 带 --trust-foreign：标记一次性清除、命令真跑（绿）、证据落盘。此段必须把真人终端
	//    判别器覆写为 TRUE——`go test` 的 stdin 是测试运行器给的（各平台有的是 NUL/dev-null
	//    char-device，有的是管道），依赖环境的默认值会让受信段跨 shell 间歇失败。false 侧
	//    由下方独立测试钉住。
	origTTY := stdinIsHumanTerminal
	stdinIsHumanTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsHumanTerminal = origTTY })
	out2 := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", true) })
	if runErr != nil {
		t.Fatalf(`trusted foreign acceptance should run normally: %v`, runErr)
	}
	if !strings.Contains(out2, `全部通过`) {
		t.Errorf(`trusted run output missing 全部通过: %s`, out2)
	}
	trusted, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if trusted.AcceptanceForeign {
		t.Errorf(`--trust-foreign 首次运行后外来标记应被清除`)
	}
	if !trusted.Acceptance[0].Passed {
		t.Errorf(`trusted run should backfill Passed=true`)
	}

	// 3. Post-trust re-run (no flag): plain local evidence path — no refusal, marker stays cleared.
	//
	// 3. 受信后重跑（无 flag）：普通本机证据路径——不再拒绝，标记保持清除。
	out3 := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr != nil {
		t.Fatalf(`post-trust re-run should not hit the trust gate: %v`, runErr)
	}
	if strings.Contains(out3, `--trust-foreign`) {
		t.Errorf(`post-trust re-run should not mention --trust-foreign: %s`, out3)
	}
}

// TestRunTaskVerifyAcceptanceAt_TrustForeignRequiresHumanTTY pins the agent-self-trust guard
// (2026-08-15 trust fix, review round 2): --trust-foreign is a HUMAN review decision — the very
// threat model of this fix is hostile content steering an LLM agent, and an injected agent can
// simply follow the refusal text's own instruction and add the flag. When stdin is NOT a char
// device (an agent's Bash-spawned pipe), even --trust-foreign must refuse: no execution, no
// marker clear. The true side of the discriminator is covered by the trusted leg of
// TestRunTaskVerifyAcceptanceAt_ForeignRequiresTrust (var overridden there).
//
// TestRunTaskVerifyAcceptanceAt_TrustForeignRequiresHumanTTY 钉住 agent 自我受信守卫
// （2026-08-15 信任修复，复审第二轮）：--trust-foreign 是真人审阅决策——本修复的威胁模型
// 正是恶意内容操纵 LLM agent，被注入的 agent 大可直接照拒绝文案的指引加 flag。stdin 非
// char device（agent 的 Bash 管道）时，即便带 --trust-foreign 也必须拒绝：不执行、不清
// 标记。判别器的 true 侧由 TestRunTaskVerifyAcceptanceAt_ForeignRequiresTrust 的受信段覆盖
// （那里覆写了变量）。
func TestRunTaskVerifyAcceptanceAt_TrustForeignRequiresHumanTTY(t *testing.T) {
	dir, taskRef := setupAcceptanceTask(t, []string{`go version :: go version`})
	if err := taskpipeline.MutateTaskState(dir, taskRef, func(s *taskpipeline.TaskState) error {
		s.AcceptanceForeign = true
		return nil
	}); err != nil {
		t.Fatalf(`MutateTaskState: %v`, err)
	}

	origTTY := stdinIsHumanTerminal
	stdinIsHumanTerminal = func() bool { return false } // agent 的管道 stdin
	t.Cleanup(func() { stdinIsHumanTerminal = origTTY })

	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", true) })
	if runErr == nil {
		t.Fatal(`agent 环境（stdin 非终端）带 --trust-foreign 也必须拒绝——人工审阅决策不得由 agent 自我完成`)
	}
	if !strings.Contains(runErr.Error(), `真人`) {
		t.Errorf(`拒绝信息应说明须真人终端运行, got: %v`, runErr)
	}
	loaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if !loaded.AcceptanceForeign {
		t.Errorf(`agent 自我受信被拒后外来标记必须保留（未被清除）`)
	}
	if loaded.Acceptance[0].Passed || loaded.Acceptance[0].Output != `` {
		t.Errorf(`拒绝路径绝不能执行外来命令: Passed=%v Output=%q`, loaded.Acceptance[0].Passed, loaded.Acceptance[0].Output)
	}
}

// TestRunTaskVerifyAcceptanceAt_MinttyGuidance pins the mintty caveat handling (review
// 2026-08-16): under Git Bash/mintty a real human's stdin is also a named pipe, so the
// char-device discriminator refuses them too. The refusal must stay a REFUSAL (no bypass —
// anything an agent can set is not a discriminator) but carry actionable guidance: switch to
// a ConPTY terminal (Windows Terminal / PowerShell).
//
// TestRunTaskVerifyAcceptanceAt_MinttyGuidance 钉住 mintty 局限的处理（2026-08-16 复审）：
// Git Bash/mintty 下真人的 stdin 同样是命名管道，char device 判别器会连真人一起拒。拒绝必须
// 仍是拒绝（不设旁路——agent 能设的东西都不是判别器），但要给出可行动指引：换 ConPTY 终端
// （Windows Terminal / PowerShell）。
func TestRunTaskVerifyAcceptanceAt_MinttyGuidance(t *testing.T) {
	dir, taskRef := setupAcceptanceTask(t, []string{`go version :: go version`})
	if err := taskpipeline.MutateTaskState(dir, taskRef, func(s *taskpipeline.TaskState) error {
		s.AcceptanceForeign = true
		return nil
	}); err != nil {
		t.Fatalf(`MutateTaskState: %v`, err)
	}
	origTTY := stdinIsHumanTerminal
	stdinIsHumanTerminal = func() bool { return false } // mintty 真人也是管道
	t.Cleanup(func() { stdinIsHumanTerminal = origTTY })
	t.Setenv(`TERM_PROGRAM`, `mintty`)

	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", true) })
	if runErr == nil {
		t.Fatal(`mintty 管道 stdin 下即便带 --trust-foreign 也必须拒绝（判别器无法区分真人与 agent）`)
	}
	for _, want := range []string{`mintty`, `Windows Terminal`} {
		if !strings.Contains(runErr.Error(), want) {
			t.Errorf(`mintty 拒绝信息应含 %q 指引, got: %v`, want, runErr)
		}
	}
	loaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if !loaded.AcceptanceForeign {
		t.Error(`拒绝路径外来标记必须保留`)
	}
}

// TestTaskVerifyAcceptance_TrustForeignFlagRegistered pins the cobra flag binding: the foreign
// trust gate is only reachable via --trust-foreign, so a dropped flag registration would
// silently reduce the gate to "always refuse" with no compile error.
//
// TestTaskVerifyAcceptance_TrustForeignFlagRegistered 钉住 cobra flag 绑定：外来受信门只
// 经 --trust-foreign 可达，flag 注册一旦丢失会把门禁静默降级为「永远拒绝」且无编译错误。
func TestTaskVerifyAcceptance_TrustForeignFlagRegistered(t *testing.T) {
	if taskVerifyAcceptanceCmd.Flags().Lookup(`trust-foreign`) == nil {
		t.Fatal(`verify-acceptance 须注册 --trust-foreign flag（外来验收受信门的唯一入口）`)
	}
}

// TestTaskAcceptance_E2E_FlagToStatusToVerify end-to-end pins the user path: task start --accept (multiple entries,
// verifying StringArray is not comma-split) → task status shows ⏳ unverified → task verify-acceptance actually runs
// all green, records deterministic evidence. Covers the full chain of cobra flag binding + status rendering + actual-run recording.
// Uses the real `go` subcommand (not echo — Windows has no echo.exe, strings.Fields+exec path would fail).
//
// TestTaskAcceptance_E2E_FlagToStatusToVerify 端到端钉住用户路径：task start --accept（多条，
// 验证 StringArray 不被逗号切分）→ task status 展示 ⏳ 未验证 → task verify-acceptance 实跑
// 全绿、记 deterministic 证据。覆盖 cobra flag 绑定 + 状态渲染 + 实跑记录的完整链路。
// 用真实 `go` 子命令（非 echo——Windows 无 echo.exe，strings.Fields+exec 路径会失败）。
func TestTaskAcceptance_E2E_FlagToStatusToVerify(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-accept`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}

	// Two --accept entries (StringArray: whole entry not split, spaces/:: not broken apart). The second `go version ::` is
	// a trailing bare :: (no expected), also verifies ParseAcceptance trailing-:: compatibility.
	//
	// 两条 --accept（StringArray：整条不切，含空格/:: 不被拆）。第二条 `go version ::` 是
	// 尾部裸 ::（无 expected），顺带验证 ParseAcceptance 的尾部 :: 兼容。
	startOut, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/spec-e2e`,
		`--accept`, `go version :: go version`,
		`--accept`, `go version ::`)
	if code != 0 {
		t.Fatalf(`task start --accept failed: %s`, startOut)
	}
	if !strings.Contains(startOut, `验收标准`) {
		t.Errorf(`task start 输出缺验收标准块: %s`, startOut)
	}

	// status: acceptance criteria listed as ⏳ unverified (before actual run).
	//
	// status：验收标准列为 ⏳ 未验证（实跑前）
	statusOut, _, code := runForge(t, dir, `task`, `status`)
	if code != 0 {
		t.Fatalf(`task status failed: %s`, statusOut)
	}
	for _, want := range []string{`验收标准`, `go version`, `未验证`} {
		if !strings.Contains(statusOut, want) {
			t.Errorf(`status 缺 %q: %s`, want, statusOut)
		}
	}

	// verify-acceptance: actual run all green, exit 0, records deterministic.
	//
	// verify-acceptance：实跑全绿、exit 0、记 deterministic
	verifyOut, _, code := runForge(t, dir, `task`, `verify-acceptance`)
	if code != 0 {
		t.Fatalf(`verify-acceptance 应 exit 0（全绿）, got %d: %s`, code, verifyOut)
	}
	for _, want := range []string{`全部通过`, `deterministic`, `checklog: acceptance`} {
		if !strings.Contains(verifyOut, want) {
			t.Errorf(`verify-acceptance 输出缺 %q: %s`, want, verifyOut)
		}
	}

	// After verify, status should show ✅ pass (no longer ⏳).
	//
	// verify 后 status 应显示 ✅ 通过（不再 ⏳）
	statusOut2, _, _ := runForge(t, dir, `task`, `status`)
	if strings.Contains(statusOut2, `未验证`) {
		t.Errorf(`verify 后不应再有"未验证"项: %s`, statusOut2)
	}
	if !strings.Contains(statusOut2, `通过`) {
		t.Errorf(`verify 后 status 应显示"通过": %s`, statusOut2)
	}
}

// TestTaskStart_PlanFileExtractsAcceptance end-to-end pins --plan-file auto-extraction: write a plan.md with Run:/Expected:
// blocks, after task start --plan-file the state.Acceptance should contain extracted entries (no need to manually copy --accept).
// Also verifies that when explicit --accept and plan coexist, --accept wins and dedup by Run. This is the closed-loop guard
// eliminating acceptance-dimension idle-spin (manual copy break) — extraction/merge/visibility, any break is caught.
//
// TestTaskStart_PlanFileExtractsAcceptance 端到端钉住 --plan-file 自动提取：写含 Run:/Expected:
// 块的 plan.md，task start --plan-file 后 state.Acceptance 应含提取条目（无需手抄 --accept）。
// 同时验证显式 --accept 与 plan 共存时 --accept 优先、按 Run 去重。这是消除 acceptance 维度
// 空转（手动复制断口）的闭环守卫——提取/merge/可见性任一断裂即被抓。
func TestTaskStart_PlanFileExtractsAcceptance(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `planfile-accept`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}

	// plan.md: two Run/Expected blocks.
	//
	// plan.md：两条 Run/Expected 块
	planPath := filepath.Join(dir, `plan.md`)
	planBody := "Run: go version\nExpected: go version go\nRun: echo hi\nExpected: hi\n"
	if err := os.WriteFile(planPath, []byte(planBody), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Pure --plan-file: acceptance should contain 2 extracted entries + visibility hint.
	//
	// 1. 纯 --plan-file：acceptance 应含 2 条提取条目 + 可见性提示
	startOut, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/plan-only`,
		`--plan-file`, planPath)
	if code != 0 {
		t.Fatalf(`task start --plan-file failed: %s`, startOut)
	}
	// Net-add count (Minor fix): pure plan-file has no dedup, all 2 extracted entries land; the hint reports 2 entries auto-extracted.
	// If one misuses pre-extraction len(extracted) the count also happens to match, but this locks the net-add semantics for the coexist path below.
	//
	// 净增计数（Minor 修复）：纯 plan-file 无去重，2 条提取全入库，提示应为「其中 2 条」。
	// 若误用提取前的 len(extracted) 计数碰巧也对，但此处锁定净增语义供下方共存路径对照。
	if !strings.Contains(startOut, `其中 2 条从 --plan-file 自动提取`) {
		t.Errorf(`纯 plan-file 应提示"其中 2 条从 --plan-file 自动提取"（净增=2）, got: %s`, startOut)
	}
	st, err := taskpipeline.LoadTaskState(dir, `feat/plan-only`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if len(st.Acceptance) != 2 {
		t.Fatalf(`纯 plan-file 应提取 2 条, got %d (%v)`, len(st.Acceptance), st.Acceptance)
	}
	if st.Acceptance[0].Run != `go version` || st.Acceptance[0].Expected != `go version go` {
		t.Errorf(`[0] = %+v, want {go version, go version go}`, st.Acceptance[0])
	}
	if st.Acceptance[1].Run != `echo hi` || st.Acceptance[1].Expected != `hi` {
		t.Errorf(`[1] = %+v, want {echo hi, hi}`, st.Acceptance[1])
	}

	// 2. --accept and --plan-file coexist: explicit --accept wins, dedup by Run.
	//
	// 2. --accept 与 --plan-file 共存：显式 --accept 优先，同 Run 去重
	startOut2, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/plan-and-accept`,
		`--accept`, `go version :: OVERRIDE`,
		`--plan-file`, planPath)
	if code != 0 {
		t.Fatalf(`task start --accept+--plan-file failed: %s`, startOut2)
	}
	st2, err := taskpipeline.LoadTaskState(dir, `feat/plan-and-accept`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	runs := map[string]string{}
	for _, c := range st2.Acceptance {
		runs[c.Run] = c.Expected
	}
	// Explicit --accept go version keeps OVERRIDE; plan echo hi supplements; plan go version is deduped away.
	//
	// 显式 --accept 的 go version 保留 OVERRIDE；plan 的 echo hi 补充；plan 的 go version 被去重
	if runs[`go version`] != `OVERRIDE` {
		t.Errorf(`显式 --accept 的 go version 应保留 OVERRIDE, got %q`, runs[`go version`])
	}
	if runs[`echo hi`] != `hi` {
		t.Errorf(`plan 的 echo hi 应补充, got %q`, runs[`echo hi`])
	}
	// Net-add count (Minor fix): --accept go version takes the slot, plan go version is deduped and dropped,
	// only echo hi nets +1. The hint should say `其中 1 条` — if one misuses pre-extraction len(extracted)=2 it would show 2 (misleading).
	//
	// 净增计数（Minor 修复）：--accept 的 go version 占位，plan 的 go version 被去重丢弃，
	// 只有 echo hi 净增 1 条。提示应是"其中 1 条"——若误用提取前 len(extracted)=2 会显示 2（误导）。
	if !strings.Contains(startOut2, `其中 1 条从 --plan-file 自动提取`) {
		t.Errorf(`共存路径净增应为 1 条（echo hi），提示"其中 1 条", got: %s`, startOut2)
	}
}

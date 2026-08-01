package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestRenderResumeSections verifies that the resume view renders all continuity fields — so the handoff party sees at a glance
// goal/plan/decisions/next-step/blockers/findings/artifacts + git changed-but-uncommitted. This is the core deliverable of the continuity source of truth.
//
// TestRenderResumeSections 验证 resume 视图把各接续字段都渲染出来——接手方一眼即见
// 目标/计划/决策/下一步/阻塞/发现/产物 + git 已改未提交。这是接续真相源的核心交付。
func TestRenderResumeSections(t *testing.T) {
	state := &taskpipeline.TaskState{
		TaskRef:    "feat/demo",
		Branch:     "feat/demo",
		Kind:       "generic",
		OriginTool: "claude-code",
		Summary:    "演示任务",
		Goal:       "做 X",
		Plan:       "step1\nstep2",
	}
	state.AddSession("s1", "claude-code")
	state.AddSession("s2", "pi")
	state.AddDecision(taskpipeline.Decision{Content: "用 PG", By: "[pi]", Rationale: "运维经验"})
	state.AddNext("写测试")
	state.AddBlocker(taskpipeline.Blocker{Content: "超时"})
	state.AddFinding(taskpipeline.Finding{Content: "内存泄漏", Source: "[claude-code]", Evidence: "pool.go:42"})

	out := renderResume(state, []string{"M internal/db/pool.go", "?? internal/db/pool_test.go"})
	for _, want := range []string{
		"feat/demo", "演示任务", "做 X", "step1", "step2",
		"已确认决策", "用 PG", "运维经验",
		"下一步", "写测试",
		"阻塞", "超时",
		"跨工具发现", "内存泄漏", "pool.go:42",
		"参与工具", "claude-code", "pi",
		"门禁进度",
		"M internal/db/pool.go",
		"session-continuity",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("resume 输出应含 %q\n---OUTPUT---\n%s", want, out)
		}
	}
}

// TestRenderResume_ExternalOrigin pins the origin visibility of the proof-of-work loop: when the task carries an external issue
// source (--from_issue), the resume view shows tracker/identifier/URL, so the handoff party sees at a glance which issue the task is anchored to.
//
// TestRenderResume_ExternalOrigin 钉住 proof-of-work 闭环的 origin 可见性：task 带外部 issue
// 来源（--from_issue）时，resume 视图显示 tracker/identifier/URL，接手方一眼知 task 锚在哪个 issue。
func TestRenderResume_ExternalOrigin(t *testing.T) {
	state := &taskpipeline.TaskState{
		TaskRef: "feat/fix",
		Branch:  "feat/fix",
		Summary: "修 bug",
		ExternalOrigin: taskpipeline.ExternalOrigin{
			Tracker:    "linear",
			Identifier: "ABC-123",
			URL:        "https://linear.app/forge/issue/ABC-123",
		},
	}
	out := renderResume(state, nil)
	for _, want := range []string{`外部来源`, `linear`, `ABC-123`, `https://linear.app/forge/issue/ABC-123`} {
		if !strings.Contains(out, want) {
			t.Errorf(`resume 输出应含 %q\n---OUTPUT---\n%s`, want, out)
		}
	}
}

// TestRenderResumeEmpty gives a minimal status card when continuity content is empty (no error, hints how to supplement) — resume always succeeds.
//
// TestRenderResumeEmpty 空接续内容时给最小状态卡（不报错、提示如何补充）——resume 永远成功。
func TestRenderResumeEmpty(t *testing.T) {
	state := &taskpipeline.TaskState{TaskRef: "feat/empty", Branch: "feat/empty"}
	out := renderResume(state, nil)
	if !strings.Contains(out, "尚无结构化接续字段") {
		t.Errorf("空接续应给补充提示，输出:\n%s", out)
	}
	if !strings.Contains(out, "工作区干净") {
		t.Errorf("无 git 改动应提示干净，输出:\n%s", out)
	}
	if !strings.Contains(out, "feat/empty") {
		t.Errorf("应显示 task ref，输出:\n%s", out)
	}
}

// TestRenderResumeBlockerStatus verifies that the status markers (open/resolved/fixed) of blockers/findings render correctly.
//
// TestRenderResumeBlockerStatus 验证阻塞/发现的状态标记（open/resolved/fixed）渲染正确。
func TestRenderResumeBlockerStatus(t *testing.T) {
	state := &taskpipeline.TaskState{TaskRef: "feat/st", Branch: "feat/st"}
	state.AddBlocker(taskpipeline.Blocker{Content: "open 阻塞"})
	state.AddBlocker(taskpipeline.Blocker{Content: "已解决"})
	state.Blockers[1].Status = "resolved"
	state.AddFinding(taskpipeline.Finding{Content: "已修", Source: "[x]"})
	state.Findings[0].Status = "fixed"

	out := renderResume(state, nil)
	if !strings.Contains(out, "open 阻塞") || !strings.Contains(out, "已解决") {
		t.Errorf("应渲染两条阻塞，输出:\n%s", out)
	}
	if !strings.Contains(out, "resolved") || !strings.Contains(out, "fixed") {
		t.Errorf("应渲染状态标记，输出:\n%s", out)
	}
}

// TestRenderResume_StripsANSI: the resume output strips ANSI escape sequences (external markdown read via --plan-file
// may contain malicious ANSI clear-screen/recolor sequences), keeping normal content. Symmetric to the HTML side html/template escaping.
//
// TestRenderResume_StripsANSI：resume 输出剥离 ANSI 转义序列（--plan-file 读入的外部 markdown
// 可能含恶意 ANSI 清屏/改色序列），保留正常内容。对称 HTML 端 html/template 转义。
func TestRenderResume_StripsANSI(t *testing.T) {
	esc := string(rune(0x1b))
	state := &taskpipeline.TaskState{
		TaskRef: "feat/a", Branch: "feat/a",
		Goal: "正常" + esc + "[31m红" + esc + "[0m文本",
	}
	out := renderResume(state, nil)
	if strings.Contains(out, esc) {
		t.Errorf("ANSI 转义（ESC）未被剥离，终端会被解释执行: %q", out)
	}
	for _, want := range []string{"正常", "红", "文本"} {
		if !strings.Contains(out, want) {
			t.Errorf("正常内容 %q 应保留: %q", want, out)
		}
	}
}

// TestRenderHookResume_NoActiveTask: SessionStart hook mode with no active task -> returns empty string (silent, no injection,
// no error). A fresh project (init but never task start) opening a new session should not inject any continuity context.
//
// TestRenderHookResume_NoActiveTask：SessionStart hook 模式无活跃任务 → 返空串（静默，不注入、
// 不报错）。fresh 项目（init 后未 task start）开新会话不应注入任何接续上下文。
func TestRenderHookResume_NoActiveTask(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-none")
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("无活跃任务应静默不报错: %v", err)
	}
	if out != "" {
		t.Errorf("无活跃任务应返空串静默，实得 %q", out)
	}
}

// TestRenderHookResume_WithActiveTask: with an active task -> returns `PASS\n` + HANDOFF view (including task ref/
// goal/gate progress), and auto-attaches the current session (silent, tool=claude-code).
//
// TestRenderHookResume_WithActiveTask：有活跃任务 → 返 "PASS\n"+HANDOFF 视图（含 task ref/
// goal/门禁进度），且自动 attach 当前 session（silent，tool=claude-code）。
func TestRenderHookResume_WithActiveTask(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{
		TaskRef: "feat/hook-demo", Branch: "feat/hook-demo",
		Kind: "code", Goal: "会话启动自动恢复",
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-hook-1")

	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Fatalf("hook 输出须以 PASS 前缀开头（runHook extractDetail 据此取 detail），实得 %q", out)
	}
	for _, want := range []string{"feat/hook-demo", "会话启动自动恢复", "门禁进度"} {
		if !strings.Contains(out, want) {
			t.Errorf("hook 输出应含 %q\n---OUT---\n%s", want, out)
		}
	}
	// attach side effect: silent mode silently anchors the current session to the task.
	//
	// attach 副作用：silent 模式静默把当前 session 锚到任务
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/hook-demo")
	if reloaded == nil || !reloaded.HasSession("sid-hook-1") {
		t.Errorf("hook 模式应自动 attach 当前 session sid-hook-1，state=%v", reloaded)
	}
}

// TestRenderHookResume_IdempotentAttach: the same session running repeatedly (multiple SessionStart) attaches idempotently —
// an already-anchored session is not added again, no error (guaranteed by the HasSession branch of attachCurrentSession).
//
// TestRenderHookResume_IdempotentAttach：同 session 重复跑（多次 SessionStart）attach 幂等——
// 已锚定 session 不重复添加、不报错（attachCurrentSession 的 HasSession 分支保证）。
func TestRenderHookResume_IdempotentAttach(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/idem", Branch: "feat/idem", Goal: "幂等"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-idem")
	if _, err := renderHookResume(root); err != nil {
		t.Fatalf("first renderHookResume: %v", err)
	}
	first, _ := taskpipeline.LoadTaskState(root, "feat/idem")
	firstLen := len(first.SessionLinks)
	if _, err := renderHookResume(root); err != nil {
		t.Fatalf("second renderHookResume (idempotent): %v", err)
	}
	second, _ := taskpipeline.LoadTaskState(root, "feat/idem")
	if len(second.SessionLinks) != firstLen {
		t.Errorf("幂等 attach：重复跑不应增 SessionLinks（%d → %d）", firstLen, len(second.SessionLinks))
	}
}

// TestRenderResumeTldr: when there are continuity fields, renderResume output near the top contains a tl;dr block (goal first line / doing now / open blockers).
// tl;dr is compact and near the top to survive compression — mitigating context rot (gap#2 cross-host mitigation layer). Resolved blockers do not enter the summary.
//
// TestRenderResumeTldr：有接续字段时 renderResume 输出靠前含 tl;dr 块（目标首行/现在做/open 阻塞）。
// tl;dr 紧凑靠前是为压缩后存活——缓解 context rot（gap#2 跨 host 缓解层）。已解决阻塞不进摘要。
func TestRenderResumeTldr(t *testing.T) {
	state := &taskpipeline.TaskState{
		TaskRef: "feat/tldr", Branch: "feat/tldr",
		Goal: "落地 tl;dr tier",
	}
	state.AddNext("写测试")
	state.AddBlocker(taskpipeline.Blocker{Content: "超时"})
	state.AddBlocker(taskpipeline.Blocker{Content: "已解决"})
	state.Blockers[1].Status = "resolved"

	out := renderResume(state, nil)
	for _, want := range []string{"tl;dr", "落地 tl;dr tier", "写测试", "超时"} {
		if !strings.Contains(out, want) {
			t.Errorf("tl;dr 应含 %q\n---OUT---\n%s", want, out)
		}
	}
	// tl;dr should come before the detailed Goal section (near the top helps it survive compression).
	//
	// tl;dr 应在详细【目标】段之前（靠前利于压缩后存活）
	tldrIdx := strings.Index(out, "tl;dr")
	detailIdx := strings.Index(out, "【目标】")
	if tldrIdx < 0 || detailIdx < 0 || tldrIdx > detailIdx {
		t.Errorf("tl;dr 应在【目标】详细段之前（tldr=%d, 目标=%d）", tldrIdx, detailIdx)
	}
}

// TestRenderResumeTldr_NoContinuity: when there are no continuity fields (HasContinuity=false), tl;dr is not rendered —
// an empty tl;dr has no value and would be redundant alongside the `no structured continuity fields yet` hint.
//
// TestRenderResumeTldr_NoContinuity：无接续字段（HasContinuity=false）时不渲染 tl;dr——
// 空 tl;dr 无价值，与"尚无结构化接续字段"提示并存会冗余。
func TestRenderResumeTldr_NoContinuity(t *testing.T) {
	state := &taskpipeline.TaskState{TaskRef: "feat/none", Branch: "feat/none"}
	out := renderResume(state, nil)
	if strings.Contains(out, "tl;dr") {
		t.Errorf("无接续字段不应渲染 tl;dr，输出:\n%s", out)
	}
}

// TestRenderHookCompactFlag: PostCompact hook (gap#2 set-flag half) with a session ID
// writes a per-session sentinel and leaves the shared task json untouched (ResumeStale
// stays false). No active task -> silent, no error; repeated calls -> idempotent.
//
// TestRenderHookCompactFlag：PostCompact hook（gap#2 设标志半边）有 session ID 时写
// per-session sentinel，不动共享 task json（ResumeStale 保持 false）。无活跃任务静默
// 不报错；重复调用幂等。
func TestRenderHookCompactFlag(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	// No active task → silent, no error.
	//
	// 无活跃任务 → 静默不报错
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("无活跃任务应静默不报错: %v", err)
	}
	state := &taskpipeline.TaskState{TaskRef: "feat/compact", Branch: "feat/compact", Goal: "压缩恢复"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-compact")
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/compact")
	if reloaded == nil || reloaded.ResumeStale {
		t.Errorf("有 session ID 时不应动 task json 的 ResumeStale，state=%v", reloaded)
	}
	if !taskpipeline.ConsumeResumeStale(root, "sid-compact") {
		t.Error("PostCompact hook 应写本 session 的 sentinel")
	}
	// Idempotent: calling again does not error and re-marks.
	//
	// 幂等：再调不报错且重新标记
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("幂等 renderHookCompactFlag: %v", err)
	}
	if !taskpipeline.ConsumeResumeStale(root, "sid-compact") {
		t.Error("幂等重标后 sentinel 应再次存在")
	}
}

// TestRenderHookCompactFlag_LegacyNoSession: without any session ID the hook falls back
// to the legacy task-scoped ResumeStale bool (set + persisted).
//
// TestRenderHookCompactFlag_LegacyNoSession：完全无 session ID 时回落到 legacy 的
// task-scoped ResumeStale bool（置位并持久化）。
func TestRenderHookCompactFlag_LegacyNoSession(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/legacy", Branch: "feat/legacy", Goal: "legacy"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "")
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/legacy")
	if reloaded == nil || !reloaded.ResumeStale {
		t.Errorf("无 session ID 应回落置 ResumeStale=true，state=%v", reloaded)
	}
}

// TestRenderHookReinject: UserPromptSubmit hook (gap#2 re-inject half) only re-injects
// the full handoff when this session was marked (sentinel), and consumes the mark —
// re-injection happens only once. Unmarked session -> silent empty return.
//
// TestRenderHookReinject：UserPromptSubmit hook（gap#2 重注入半边）仅在本 session 有
// 标记（sentinel）时重注入完整 handoff 并消费标记——只重注入一次。无标记静默返空。
func TestRenderHookReinject(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/reinject", Branch: "feat/reinject", Goal: "压缩后恢复"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-reinject")

	// No mark → silent empty return.
	//
	// 无标记 → 静默返空
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (unmarked): %v", err)
	}
	if out != "" {
		t.Errorf("无标记应静默返空，实得 %q", out)
	}

	// compact-flag marks this session → reinject re-injects the full handoff.
	//
	// compact-flag 标记本 session → reinject 重注入完整 handoff
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}
	out, err = renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (marked): %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Errorf("有标记应返 PASS+handoff，实得 %q", out)
	}
	if !strings.Contains(out, "feat/reinject") || !strings.Contains(out, "压缩后恢复") {
		t.Errorf("reinject 应含 task ref/goal，实得 %q", out)
	}
	// The mark is consumed (silent next time, re-inject only once).
	//
	// 标记已被消费（下次静默，只重注入一次）
	out2, _ := renderHookReinject(root)
	if out2 != "" {
		t.Errorf("标记消费后应静默返空，实得 %q", out2)
	}
}

// TestRenderHookReinject_PerSessionIsolation (multi-session fix): with N sessions on one
// task (shared user-level DataDir), session B's prompt must NOT consume session A's
// compaction mark — B stays silent, A still gets its re-injection.
//
// TestRenderHookReinject_PerSessionIsolation（多 session 修复）：N 个 session 共享一个
// task（用户级共享 DataDir）时，session B 的 prompt 不能消费 session A 的压缩标记——
// B 保持静默，A 仍能拿到自己的重注入。
func TestRenderHookReinject_PerSessionIsolation(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/iso", Branch: "feat/iso", Goal: "多 session 隔离"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	// Session A compacts.
	//
	// session A 发生压缩
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-a")
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}

	// Session B's next prompt: silent, and must not consume A's mark.
	//
	// session B 的下个 prompt：静默，且不能消费 A 的标记
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-b")
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (sid-b): %v", err)
	}
	if out != "" {
		t.Errorf("sid-b 未被压缩应静默返空，实得 %q", out)
	}

	// Session A's next prompt still gets the re-injection.
	//
	// session A 的下个 prompt 仍能拿到重注入
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-a")
	out, err = renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (sid-a): %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") || !strings.Contains(out, "多 session 隔离") {
		t.Errorf("sid-a 的标记不应被 sid-b 消费，实得 %q", out)
	}
}

// TestRenderHookReinject_LegacyBool: a task carrying the legacy task-scoped
// ResumeStale=true (written by the no-session fallback or an older binary) is honored
// once: re-inject fires and the bool is cleared + persisted.
//
// TestRenderHookReinject_LegacyBool：带 legacy task-scoped ResumeStale=true 的 task
// （无 session 回落或旧版 binary 所留）被兑现一次：触发重注入并清零持久化。
func TestRenderHookReinject_LegacyBool(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/legacy-ri", Branch: "feat/legacy-ri", Goal: "legacy 重注入", ResumeStale: true}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-legacy")
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject: %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Errorf("legacy ResumeStale=true 应触发重注入，实得 %q", out)
	}
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/legacy-ri")
	if reloaded == nil || reloaded.ResumeStale {
		t.Errorf("legacy 标记兑现后应清零持久化，state=%v", reloaded)
	}
	out2, _ := renderHookReinject(root)
	if out2 != "" {
		t.Errorf("legacy 标记只兑现一次，实得 %q", out2)
	}
}

// TestRenderHookReinject_SparseContinuityNudge (plan 4 mid-way checkpoint active driving): after compression, during re-injection,
// if the task has not persisted any mid-way thread (decisions/next-step), a strong hint is appended to the end of the handoff to push the agent to persist explicitly —
// what compression loses is exactly this working memory, otherwise the next compression rebuilds from scratch. When NextSteps already exist, nothing is appended (the thread is already on disk,
// restoring is enough). Goal does not count (task start already persisted, not a compression loss item). Two roots isolate the positive and negative cases
// (different git-root -> different project key -> different task dir, ActiveTaskState each only scans its own one).
//
// TestRenderHookReinject_SparseContinuityNudge（方案4·中途 checkpoint 主动驱动）：压缩后重注入
// 时，若任务未落盘任何中途线程（决策/下一步），handoff 末尾追加强提示推 agent 显式落盘——
// 压缩丢的正是这段工作记忆，下次压缩否则从零重建。已有 NextSteps 时不追加（线程已在盘上，
// 复原即可）。Goal 不算（task start 已落盘，非压缩丢失项）。两个 root 隔离正负用例
// （不同 git-root → 不同 project key → 不同 task dir，ActiveTaskState 各自只扫到自己那一个）。
func TestRenderHookReinject_SparseContinuityNudge(t *testing.T) {
	// Sparse thread (has Goal, no decide/next) -> append strong hint
	//
	// 稀疏线程（有 Goal 无 decide/next）→ 追加强提示
	rootA, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/sparse", Branch: "feat/sparse", Goal: "实现 X"}
	if err := taskpipeline.SaveTaskState(rootA, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-sparse")
	if err := renderHookCompactFlag(rootA); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}
	out, err := renderHookReinject(rootA)
	if err != nil {
		t.Fatalf("renderHookReinject: %v", err)
	}
	if !strings.Contains(out, "刚发生") || !strings.Contains(out, "forge task decide") {
		t.Errorf("稀疏线程应追加压缩落盘强提示，输出:\n%s", out)
	}

	// Already persisted next-step (thread on disk) -> do not append
	//
	// 已落盘下一步（线程在盘上）→ 不追加
	rootB, _ := forgedatatest.RealProject(t)
	state2 := &taskpipeline.TaskState{TaskRef: "feat/rich", Branch: "feat/rich", Goal: "实现 Y"}
	state2.AddNext("写测试")
	if err := taskpipeline.SaveTaskState(rootB, state2); err != nil {
		t.Fatalf("SaveTaskState state2: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-rich")
	if err := renderHookCompactFlag(rootB); err != nil {
		t.Fatalf("renderHookCompactFlag state2: %v", err)
	}
	out2, err := renderHookReinject(rootB)
	if err != nil {
		t.Fatalf("renderHookReinject state2: %v", err)
	}
	if strings.Contains(out2, "刚发生") {
		t.Errorf("已有 NextSteps 不应追加压缩落盘提示，输出:\n%s", out2)
	}
}

// TestDetectOriginTool_FallbackChain (multi-host): explicit wins; FORGE_AGENT (injected
// by runHook from the resolved --agent flag) identifies hook-spawned kimi/windsurf
// processes; CLAUDE_CODE_SESSION_ID is the claude-code fallback; all empty -> "".
//
// TestDetectOriginTool_FallbackChain（多 host）：显式优先；FORGE_AGENT（runHook 从
// 解析出的 --agent 注入）识别 hook 派生的 kimi/windsurf 进程；CLAUDE_CODE_SESSION_ID
// 是 claude-code 兜底；全空 → ""。
func TestDetectOriginTool_FallbackChain(t *testing.T) {
	t.Setenv("FORGE_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if got := detectOriginTool(""); got != "" {
		t.Errorf("全空应返回 \"\": %q", got)
	}
	if got := detectOriginTool("pi"); got != "pi" {
		t.Errorf("显式应优先: %q", got)
	}

	t.Setenv("FORGE_AGENT", "kimi")
	if got := detectOriginTool(""); got != "kimi" {
		t.Errorf("FORGE_AGENT 注入: %q", got)
	}
	if got := detectOriginTool("pi"); got != "pi" {
		t.Errorf("显式仍应优先于 FORGE_AGENT: %q", got)
	}

	t.Setenv("FORGE_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-sess")
	if got := detectOriginTool(""); got != "claude-code" {
		t.Errorf("claude-code 兜底: %q", got)
	}
}

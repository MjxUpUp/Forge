package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

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
	// Host-agnostic loading form (2026-08-25 prompt-copy fix): the footer names the
	// session-continuity skill in natural language; the slash-command form
	// (/session-continuity) is Claude Code-only dead text on other hosts.
	//
	// 宿主无关的加载形态（2026-08-25 文案修复）：尾部用自然语言点名
	// session-continuity skill；slash command 形态（/session-continuity）在
	// 其他宿主是死文本。
	if !strings.Contains(out, "接续纪律用 session-continuity skill：") {
		t.Errorf("resume 尾部应使用自然语言 skill 引用\n---OUTPUT---\n%s", out)
	}
	if strings.Contains(out, "/session-continuity") {
		t.Errorf("resume 尾部不得含 Claude-only slash command 形态（/session-continuity）\n---OUTPUT---\n%s", out)
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
			t.Errorf(`resume 输出应含 %q`+"\n"+`---OUTPUT---`+"\n"+`%s`, want, out)
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

// TestRenderHookResume_InventoryAmbiguous (interaction flow step 2): with ≥2 incomplete
// tasks and no context match, the SessionStart hook must NOT stay silent — it injects a
// compact candidate inventory (refs/gate progress/next step) plus the instruction to let
// the user pick via the agent's structured-question tool. No session is anchored (the
// user has not chosen a task yet).
//
// TestRenderHookResume_InventoryAmbiguous（交互流程第 2 步）：≥2 个未完成任务且无
// 上下文匹配时，SessionStart hook 不能静默——注入紧凑候选盘点（ref/门禁进度/下一步）
// 并指示 agent 用结构化提问工具让用户选择。不锚定 session（用户尚未选定任务）。
func TestRenderHookResume_InventoryAmbiguous(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	a := &taskpipeline.TaskState{TaskRef: "feat/inv-a", Branch: "feat/inv-a", Summary: "任务 A"}
	a.AddNext("做 A 的下一步")
	b := &taskpipeline.TaskState{TaskRef: "feat/inv-b", Branch: "feat/inv-b", Summary: "任务 B"}
	for _, s := range []*taskpipeline.TaskState{a, b} {
		if err := taskpipeline.SaveTaskState(root, s); err != nil {
			t.Fatalf("SaveTaskState: %v", err)
		}
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-inv")

	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Fatalf("歧义时应注入候选清单（PASS 前缀），实得 %q", out)
	}
	for _, want := range []string{"检测到 2 个未完成任务", "feat/inv-a", "任务 A", "做 A 的下一步", "feat/inv-b", "AskUserQuestion", "forge task resume --ref", "forge task start"} {
		if !strings.Contains(out, want) {
			t.Errorf("候选清单应含 %q\n---OUT---\n%s", want, out)
		}
	}
	// No attach: the user has not picked a task, so no session link may be written.
	//
	// 不锚定：用户尚未选定任务，不应写入任何 session 链接
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/inv-a")
	if reloaded != nil && len(reloaded.SessionLinks) > 0 {
		t.Errorf("盘点注入不应锚定 session，state=%v", reloaded.SessionLinks)
	}
}

// TestRenderHookResume_OtherTasksFooter: with an unambiguous active task plus other
// in-flight tasks, the handoff resumes the current one AND names the others in one
// line — the handoff party sees the full in-flight set.
//
// TestRenderHookResume_OtherTasksFooter：有无歧义活跃任务且另有在进行任务时，handoff
// 自动接续当前任务，并用一行列出其余任务——接手方看到完整的在进行集合。
func TestRenderHookResume_OtherTasksFooter(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	cur := &taskpipeline.TaskState{TaskRef: "feat/current", Branch: "feat/current", Goal: "当前任务"}
	other := &taskpipeline.TaskState{TaskRef: "feat/other", Branch: "feat/other", Summary: "另一个"}
	for _, s := range []*taskpipeline.TaskState{cur, other} {
		if err := taskpipeline.SaveTaskState(root, s); err != nil {
			t.Fatalf("SaveTaskState: %v", err)
		}
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-cur")
	if err := taskpipeline.SetActiveTaskRef(root, "sid-cur", "feat/current"); err != nil {
		t.Fatalf("SetActiveTaskRef: %v", err)
	}

	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if !strings.Contains(out, "feat/current") || !strings.Contains(out, "当前任务") {
		t.Errorf("应自动接续当前任务\n---OUT---\n%s", out)
	}
	if !strings.Contains(out, "另有 1 个未完成任务: feat/other") {
		t.Errorf("应一行列出其余未完成任务\n---OUT---\n%s", out)
	}
}

// TestRenderHookResume_InventoryCap: beyond inventoryListCap the list truncates with a
// "…还有 N 个" line — SessionStart injection must stay compact no matter how many
// zombie tasks accumulate.
//
// TestRenderHookResume_InventoryCap：超过 inventoryListCap 时清单截断并给「…还有
// N 个」——无论僵尸任务堆多少，SessionStart 注入都必须保持紧凑。
func TestRenderHookResume_InventoryCap(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	for i := 0; i < inventoryListCap+2; i++ {
		s := &taskpipeline.TaskState{TaskRef: fmt.Sprintf("feat/cap-%02d", i), Branch: fmt.Sprintf("feat/cap-%02d", i)}
		if err := taskpipeline.SaveTaskState(root, s); err != nil {
			t.Fatalf("SaveTaskState: %v", err)
		}
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-cap")
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if !strings.Contains(out, "还有 2 个") {
		t.Errorf("超过上限应给截断提示\n---OUT---\n%s", out)
	}
}

// TestRenderHookResume_InventorySkipsCompleted: completed tasks never enter the
// inventory — a single incomplete task auto-resumes even alongside completed ones
// (no false ambiguity), and a fully-completed set stays silent.
//
// TestRenderHookResume_InventorySkipsCompleted：已完成任务不进盘点——唯一未完成任务
// 即使伴着已完成任务也自动接续（不产生假歧义）；全部已完成时保持静默。
func TestRenderHookResume_InventorySkipsCompleted(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	now := time.Now()
	done := &taskpipeline.TaskState{TaskRef: "feat/done", Branch: "feat/done", CompletedAt: &now}
	open := &taskpipeline.TaskState{TaskRef: "feat/still-open", Branch: "feat/still-open", Goal: "进行中"}
	for _, s := range []*taskpipeline.TaskState{done, open} {
		if err := taskpipeline.SaveTaskState(root, s); err != nil {
			t.Fatalf("SaveTaskState: %v", err)
		}
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-mix")
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if !strings.Contains(out, "feat/still-open") || !strings.Contains(out, "进行中") {
		t.Errorf("唯一未完成任务应自动接续\n---OUT---\n%s", out)
	}
	if strings.Contains(out, "feat/done") {
		t.Errorf("已完成任务不应出现\n---OUT---\n%s", out)
	}

	// All completed → silent.
	//
	// 全部已完成 → 静默
	root2, _ := forgedatatest.RealProject(t)
	done2 := &taskpipeline.TaskState{TaskRef: "feat/done2", Branch: "feat/done2", CompletedAt: &now}
	if err := taskpipeline.SaveTaskState(root2, done2); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out2, err := renderHookResume(root2)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if out2 != "" {
		t.Errorf("全部已完成应静默，实得 %q", out2)
	}
}

// TestRenderHookResume_InventoryBranchAndANSI: the inventory renders the [分支 x]
// suffix when Branch differs from TaskRef, and strips ANSI control from user-controlled
// fields (ref/summary come from --ref/--title input — same threat model as
// TestRenderResume_StripsANSI).
//
// TestRenderHookResume_InventoryBranchAndANSI：Branch 与 TaskRef 不同时盘点渲染
// [分支 x] 后缀；用户可控字段（ref/标题来自 --ref/--title 输入）的 ANSI 控制字符
// 被剥离——与 TestRenderResume_StripsANSI 同一威胁模型。
func TestRenderHookResume_InventoryBranchAndANSI(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	esc := string(rune(0x1b))
	a := &taskpipeline.TaskState{TaskRef: "feat/ansi-a", Branch: "topic/branch-a", Summary: "正常" + esc + "[31m红"}
	b := &taskpipeline.TaskState{TaskRef: "feat/ansi-b", Branch: "feat/ansi-b"}
	for _, s := range []*taskpipeline.TaskState{a, b} {
		if err := taskpipeline.SaveTaskState(root, s); err != nil {
			t.Fatalf("SaveTaskState: %v", err)
		}
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-ansi")
	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if strings.Contains(out, esc) {
		t.Errorf("盘点输出不应含 ANSI 转义: %q", out)
	}
	if !strings.Contains(out, "[分支 topic/branch-a]") {
		t.Errorf("Branch≠TaskRef 应渲染 [分支] 后缀\n---OUT---\n%s", out)
	}
	if !strings.Contains(out, "正常") || !strings.Contains(out, "红") {
		t.Errorf("剥离 ANSI 后正常内容应保留\n---OUT---\n%s", out)
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

// TestRenderHookReinject_KimiColdStartBackfill (P3): kimi drops SessionStart hook output, so
// the SessionStart task-resume handoff never reaches the model. The UserPromptSubmit
// resume-reinject backfills it on the first prompt of a kimi session, gated by a per-session
// sentinel so it fires exactly once — the second prompt is silent. No compaction mark is
// involved here (that path is exercised separately).
//
// TestRenderHookReinject_KimiColdStartBackfill（P3）：kimi 丢弃 SessionStart hook 输出，故
// SessionStart task-resume 的 handoff 到不了模型。UserPromptSubmit 的 resume-reinject 在 kimi
// session 首个 prompt 回填它，由 per-session sentinel 去重只触发一次——第二个 prompt 静默。
// 此处不涉及压缩标记（那条路径另行测试）。
func TestRenderHookReinject_KimiColdStartBackfill(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/kimi-cold", Branch: "feat/kimi-cold", Goal: "kimi 冷启动回填"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	// Simulate a hook-spawned kimi session (runHook injects FORGE_SESSION_ID + FORGE_AGENT).
	//
	// 模拟 hook 派生的 kimi session（runHook 注入 FORGE_SESSION_ID + FORGE_AGENT）
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "kimi-cold-1")
	t.Setenv("FORGE_AGENT", "kimi")

	// First prompt: no compaction mark, but kimi drops SessionStart → backfill the handoff.
	//
	// 首个 prompt：无压缩标记，但 kimi 丢弃 SessionStart → 回填 handoff
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (first prompt): %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Errorf("kimi 首个 prompt 应回填 PASS+handoff，实得 %q", out)
	}
	if !strings.Contains(out, "feat/kimi-cold") || !strings.Contains(out, "kimi 冷启动回填") {
		t.Errorf("回填应含 task ref/goal，实得 %q", out)
	}
	if !taskpipeline.IsColdStartInjected(root, "kimi-cold-1") {
		t.Error("首个 prompt 回填后应设 cold-start sentinel")
	}

	// Second prompt: sentinel dedupes → silent (no double-inject).
	//
	// 第二个 prompt：sentinel 去重 → 静默（不双注）
	out2, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (second prompt): %v", err)
	}
	if out2 != "" {
		t.Errorf("sentinel 去重后应静默返空，实得 %q", out2)
	}
}

// TestRenderHookReinject_KimiColdStartAfterCompactReinject (P3): a compact-reinject delivers
// a full handoff (satisfying cold-start too), so it marks the cold-start sentinel — otherwise
// the NEXT prompt (stale now consumed) would hit the cold-start path and double-inject. The
// sentinel set during compact-reinject keeps the following prompt silent.
//
// TestRenderHookReinject_KimiColdStartAfterCompactReinject（P3）：compact-reinject 交付了
// 完整 handoff（也满足冷启动），故它设 cold-start sentinel——否则下个 prompt（stale 已消费）
// 会命中冷启动路径造成双注。compact-reinject 设的 sentinel 使后续 prompt 静默。
func TestRenderHookReinject_KimiColdStartAfterCompactReinject(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/kimi-compact", Branch: "feat/kimi-compact", Goal: "kimi 压缩后不双注"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "kimi-compact-1")
	t.Setenv("FORGE_AGENT", "kimi")

	// Compact just happened → compact-reinject fires (full handoff) AND marks cold-start.
	//
	// 刚压缩 → compact-reinject 触发（完整 handoff）并设 cold-start
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (compact): %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Errorf("压缩后应返 PASS+handoff，实得 %q", out)
	}
	if !taskpipeline.IsColdStartInjected(root, "kimi-compact-1") {
		t.Error("compact-reinject 后应设 cold-start sentinel 防双注")
	}

	// Next prompt: stale consumed AND cold-start marked → silent (no double-inject).
	//
	// 下个 prompt：stale 已消费且 cold-start 已设 → 静默（不双注）
	out2, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (after compact): %v", err)
	}
	if out2 != "" {
		t.Errorf("compact-reinject 已设 cold-start，下个 prompt 应静默返空，实得 %q", out2)
	}
}

// TestRenderHookReinject_ColdStartNonKimiExcluded (P3): the cold-start backfill is kimi-only
// — hosts that inject SessionStart output (Claude Code, codex, ...) already received the
// handoff at SessionStart, so backfilling on UserPromptSubmit would duplicate it. A
// claude-code session with an active task and no compaction mark stays silent.
//
// TestRenderHookReinject_ColdStartNonKimiExcluded（P3）：冷启动回填仅限 kimi——注入 SessionStart
// 输出的 host（Claude Code、codex 等）在 SessionStart 已拿到 handoff，UserPromptSubmit 再回填会
// 重复。有活跃任务且无压缩标记的 claude-code session 保持静默。
func TestRenderHookReinject_ColdStartNonKimiExcluded(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/cc-cold", Branch: "feat/cc-cold", Goal: "CC 不回填"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	// claude-code session: CLAUDE_CODE_SESSION_ID set, no FORGE_AGENT. SessionStart output
	// IS injected on CC, so cold-start backfill must NOT fire.
	//
	// claude-code session：CLAUDE_CODE_SESSION_ID 已设，无 FORGE_AGENT。CC 注入 SessionStart
	// 输出，故冷启动回填不得触发
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-cold-1")
	t.Setenv("FORGE_AGENT", "")

	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject: %v", err)
	}
	if out != "" {
		t.Errorf("claude-code 不应触发冷启动回填（SessionStart 已注入），实得 %q", out)
	}
	if taskpipeline.IsColdStartInjected(root, "cc-cold-1") {
		t.Error("claude-code 不应设 cold-start sentinel")
	}
}

// TestRenderHookReinject_KimiColdStartNoActiveTask (P3): with no active task there is
// nothing to backfill — the cold-start path is unreachable (the state==nil guard returns
// first). A kimi session with no task stays silent and sets no sentinel.
//
// TestRenderHookReinject_KimiColdStartNoActiveTask（P3）：无活跃任务则无可回填——冷启动路径
// 不可达（state==nil 守卫先返回）。无任务的 kimi session 保持静默且不设 sentinel。
func TestRenderHookReinject_KimiColdStartNoActiveTask(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "kimi-notask-1")
	t.Setenv("FORGE_AGENT", "kimi")

	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject: %v", err)
	}
	if out != "" {
		t.Errorf("无活跃任务应静默返空（无可回填），实得 %q", out)
	}
	if taskpipeline.IsColdStartInjected(root, "kimi-notask-1") {
		t.Error("无活跃任务不应设 cold-start sentinel")
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

// TestRenderHookResume_MultiHostAttach (fix 1 end-to-end at Go level): a hook-spawned
// kimi session (FORGE_SESSION_ID + FORGE_AGENT injected by runHook) auto-attaches with
// the correct tool — before the fallback chain this was silently skipped.
//
// TestRenderHookResume_MultiHostAttach（修复 1 的 Go 层端到端）：hook 派生的 kimi
// session（runHook 注入 FORGE_SESSION_ID + FORGE_AGENT）以正确工具自动锚定——
// 加兜底链之前这一步被静默跳过。
func TestRenderHookResume_MultiHostAttach(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/kimi-attach", Branch: "feat/kimi-attach", Goal: "多 host 锚定"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "kimi-sess-1")
	t.Setenv("FORGE_AGENT", "kimi")

	out, err := renderHookResume(root)
	if err != nil {
		t.Fatalf("renderHookResume: %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Errorf("hook 输出须以 PASS 前缀开头，实得 %q", out)
	}
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/kimi-attach")
	if reloaded == nil || !reloaded.HasSession("kimi-sess-1") {
		t.Fatalf("kimi session 应被锚定，state=%v", reloaded)
	}
	for _, l := range reloaded.SessionLinks {
		if l.SessionID == "kimi-sess-1" && l.Tool != "kimi" {
			t.Errorf("锚定工具应为 kimi，实得 %q", l.Tool)
		}
	}
}

// TestRenderHookResume_AnchoredNoTool: an already-anchored session whose tool can no
// longer be detected (sid only, no FORGE_AGENT/CLAUDE env) must not error, not
// duplicate the link, and not misattribute — attach is a side action.
//
// TestRenderHookResume_AnchoredNoTool：已锚定的 session 在工具探测失败时（仅有 sid，
// 无 FORGE_AGENT/CLAUDE env）不报错、不重复锚定、不错误归属——锚定是附加动作。
func TestRenderHookResume_AnchoredNoTool(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/notool", Branch: "feat/notool", Goal: "锚定无工具"}
	state.AddSession("kimi-sid", "kimi")
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_AGENT", "")
	t.Setenv("FORGE_SESSION_ID", "kimi-sid")

	if _, err := renderHookResume(root); err != nil {
		t.Fatalf("已锚定+无工具不应报错: %v", err)
	}
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/notool")
	if reloaded == nil || len(reloaded.SessionLinks) != 1 {
		t.Errorf("不应新增锚定（仍 1 条），state=%v", reloaded)
	}
	if reloaded.SessionLinks[0].Tool != "kimi" {
		t.Errorf("既有锚定的工具归属不应被改写，实得 %q", reloaded.SessionLinks[0].Tool)
	}
}

// ── phase-1 remainder: offered-chain + offered-block + auto-claim tests ──
// All literals below are backtick raw strings: Windows quote-corrosion turns ASCII " into CJK
// curly quotes in Go source, so no double-quoted literal is introduced here. No raw string holds
// a \n (a literal backslash-n under backticks); the only newline inside renderOfferedBlock is built
// from the numeric byte 10 (realNewlineString) in the production code itself.
//
// 以下为 phase-1 剩余的 offered-chain / offered-block / 自动认领测试。所有字面量均为反引号 raw
// string：Windows 引号腐蚀会把 Go 源里的 ASCII " 转成 CJK 弯引号，故此处不引入双引号字面。
// raw string 内不含 \n（反引号下是字面 backslash-n）；renderOfferedBlock 内唯一换行在生产代码里
// 用数值字节 10（realNewlineString）构造。

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
	t.Setenv(`FORGE_AGENT`, `kimi`)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	t.Setenv(`FORGE_SESSION_ID`, `sid-x`)
	active := &taskpipeline.TaskState{TaskRef: `feat/me`, Branch: `feat/me`, Goal: `编排主任务`}
	saveAll(t, root, active,
		offeredKimiTask(`feat/a`, ``, `任务甲`),
		offeredKimiTask(`feat/b`, ``, `任务乙`),
	)
	if e := taskpipeline.SetActiveTaskRef(root, `sid-x`, `feat/me`); e != nil {
		t.Fatalf(`SetActiveTaskRef: %v`, e)
	}
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
	t.Setenv(`FORGE_AGENT`, `kimi`)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	t.Setenv(`FORGE_SESSION_ID`, `sid-x`)
	active := &taskpipeline.TaskState{TaskRef: `feat/me`, Branch: `feat/me`, ParentTaskRef: `feat/orch`, Goal: `编排链主任务`}
	saveAll(t, root, active,
		offeredKimiTask(`feat/sib-a`, `feat/orch`, `兄弟甲`),
		offeredKimiTask(`feat/sib-b`, `feat/orch`, `兄弟乙`),
		offeredKimiTask(`feat/other`, `feat/x`, `非同链`),
	)
	if e := taskpipeline.SetActiveTaskRef(root, `sid-x`, `feat/me`); e != nil {
		t.Fatalf(`SetActiveTaskRef: %v`, e)
	}
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
	t.Setenv(`FORGE_AGENT`, `kimi`)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	t.Setenv(`FORGE_SESSION_ID`, `sid-x`)
	active := &taskpipeline.TaskState{TaskRef: `feat/me`, Branch: `feat/me`, ParentTaskRef: `feat/orch`, Goal: `编排链主任务`}
	dep := &taskpipeline.TaskState{TaskRef: `feat/dep`, Branch: `feat/dep`, Goal: `前置依赖`} // incomplete → blocks
	sibReady := offeredKimiTask(`feat/sib-ready`, `feat/orch`, `就绪兄弟`)
	sibBlocked := offeredKimiTask(`feat/sib-blocked`, `feat/orch`, `阻塞兄弟`)
	sibBlocked.DependsOn = []string{`feat/dep`}
	saveAll(t, root, active, dep, sibReady, sibBlocked)
	if e := taskpipeline.SetActiveTaskRef(root, `sid-x`, `feat/me`); e != nil {
		t.Fatalf(`SetActiveTaskRef: %v`, e)
	}
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
	t.Setenv(`FORGE_AGENT`, `kimi`)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	t.Setenv(`FORGE_SESSION_ID`, `sid-x`)
	active := offeredKimiTask(`feat/me`, ``, `被接手的 offered 任务`)
	saveAll(t, root, active, offeredKimiTask(`feat/other`, ``, `另一个 offered`))
	if e := taskpipeline.SetActiveTaskRef(root, `sid-x`, `feat/me`); e != nil {
		t.Fatalf(`SetActiveTaskRef: %v`, e)
	}
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

// TestTaskResume_AutoClaim_HappyPath: resume of an offered-to-me task auto-claims it (offered→
// claimed), prints the stderr notice, and anchors the session — the manual `task claim` step is
// gone (design §3).
func TestTaskResume_AutoClaim_HappyPath(t *testing.T) {
	dir := setupDelegateProject(t)
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `被分派`); code != 0 {
		t.Fatalf(`task start exit %d: %s`, code, out)
	}
	if out, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/delegate`, `--to`, `kimi`, `--role`, `frontend`, `--by`, `claude-code`); code != 0 {
		t.Fatalf(`task assign exit %d: %s`, code, out)
	}
	t.Setenv(`FORGE_AGENT`, `kimi`)
	out, _, code := runForge(t, dir, `task`, `resume`, `--ref`, `feat/delegate`)
	if code != 0 {
		t.Fatalf(`resume exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `已自动认领任务 feat/delegate（kimi）`) {
		t.Errorf(`应 stderr 提示已自动认领，实得 %q`, out)
	}
	st, _ := taskpipeline.LoadTaskState(dir, `feat/delegate`)
	if st == nil || st.Assignment == nil || st.Assignment.Status != taskpipeline.AssignClaimed {
		t.Fatalf(`auto-claim 后应 claimed，state=%v`, st)
	}
	if got := taskpipeline.ReadActiveTaskRef(dir, `delegate-test-sid`); got != `feat/delegate` {
		t.Errorf(`应锚定 active-task-ref-delegate-test-sid=feat/delegate，实得 %q`, got)
	}
}

// TestTaskResume_AutoClaim_NotOffered: a plain (never-assigned) task is not auto-claimed.
func TestTaskResume_AutoClaim_NotOffered(t *testing.T) {
	dir := setupDelegateProject(t)
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `普通任务`); code != 0 {
		t.Fatalf(`task start exit %d: %s`, code, out)
	}
	t.Setenv(`FORGE_AGENT`, `kimi`)
	out, _, code := runForge(t, dir, `task`, `resume`, `--ref`, `feat/delegate`)
	if code != 0 {
		t.Fatalf(`resume exit %d: %s`, code, out)
	}
	if strings.Contains(out, `已自动认领`) {
		t.Errorf(`未分派的任务不应自动认领，实得 %q`, out)
	}
	st, _ := taskpipeline.LoadTaskState(dir, `feat/delegate`)
	if st == nil || st.Assignment != nil {
		t.Fatalf(`未分派任务应无 Assignment，state=%v`, st)
	}
}

// TestTaskResume_AutoClaim_OtherAgent: a task offered to reasonix is not auto-claimed by kimi.
func TestTaskResume_AutoClaim_OtherAgent(t *testing.T) {
	dir := setupDelegateProject(t)
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `派给 reasonix`); code != 0 {
		t.Fatalf(`task start exit %d: %s`, code, out)
	}
	if out, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/delegate`, `--to`, `reasonix`, `--role`, `backend`, `--by`, `claude-code`); code != 0 {
		t.Fatalf(`task assign exit %d: %s`, code, out)
	}
	t.Setenv(`FORGE_AGENT`, `kimi`)
	out, _, code := runForge(t, dir, `task`, `resume`, `--ref`, `feat/delegate`)
	if code != 0 {
		t.Fatalf(`resume exit %d: %s`, code, out)
	}
	if strings.Contains(out, `已自动认领`) {
		t.Errorf(`派给 reasonix 的任务 kimi 不应自动认领，实得 %q`, out)
	}
	st, _ := taskpipeline.LoadTaskState(dir, `feat/delegate`)
	if st == nil || st.Assignment == nil || st.Assignment.Status != taskpipeline.AssignOffered || st.Assignment.Agent != `reasonix` {
		t.Fatalf(`应仍 offered 给 reasonix，state=%v`, st)
	}
}

// TestTaskResume_AutoClaim_NoAgent: when no agent can be attributed, auto-claim is skipped — the
// task stays offered. (A genuine TOCTOU race between IsOfferedTo and Claim is inherently
// non-deterministic and is not asserted here; the non-fatal error path is covered by review.)
func TestTaskResume_AutoClaim_NoAgent(t *testing.T) {
	dir := setupDelegateProject(t)
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `被分派`); code != 0 {
		t.Fatalf(`task start exit %d: %s`, code, out)
	}
	if out, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/delegate`, `--to`, `kimi`, `--role`, `frontend`, `--by`, `claude-code`); code != 0 {
		t.Fatalf(`task assign exit %d: %s`, code, out)
	}
	t.Setenv(`FORGE_AGENT`, ``)
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, ``)
	out, _, code := runForge(t, dir, `task`, `resume`, `--ref`, `feat/delegate`, `--no-attach`)
	if code != 0 {
		t.Fatalf(`resume exit %d: %s`, code, out)
	}
	if strings.Contains(out, `已自动认领`) {
		t.Errorf(`agent 未知时不应自动认领，实得 %q`, out)
	}
	st, _ := taskpipeline.LoadTaskState(dir, `feat/delegate`)
	if st == nil || st.Assignment == nil || st.Assignment.Status != taskpipeline.AssignOffered {
		t.Fatalf(`应仍 offered（未认领），state=%v`, st)
	}
}

// TestTaskResume_AutoClaim_JSON: under --json the auto-claim still takes effect (status reflected
// in the JSON) but the stderr notice is suppressed, so the output stays valid JSON.
func TestTaskResume_AutoClaim_JSON(t *testing.T) {
	dir := setupDelegateProject(t)
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `被分派`); code != 0 {
		t.Fatalf(`task start exit %d: %s`, code, out)
	}
	if out, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/delegate`, `--to`, `kimi`, `--role`, `frontend`, `--by`, `claude-code`); code != 0 {
		t.Fatalf(`task assign exit %d: %s`, code, out)
	}
	t.Setenv(`FORGE_AGENT`, `kimi`)
	out, _, code := runForge(t, dir, `task`, `resume`, `--ref`, `feat/delegate`, `--json`, `--no-attach`)
	if code != 0 {
		t.Fatalf(`resume --json exit %d: %s`, code, out)
	}
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
}

package cli

// task_continuity_render_test.go —— 接续真相源的渲染族：纯 renderResume 输出
// （分节/外部来源/空态/状态标记/ANSI 剥离/tl;dr）与 SessionStart hook 视图
// renderHookResume（盘点歧义、截断、跳过已完成、自动锚定、多 host 锚定）。
// 自 task_continuity_test.go 按域拆分；compact/reinject hook 在
// task_continuity_hooks_test.go，offered/自动认领域在
// task_assignment_lifecycle_test.go。

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// TestRenderResumeSections verifies that the resume view renders all continuity
// fields.
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

// TestRenderResume_ExternalOrigin pins the origin visibility of the
// proof-of-work loop: when the task carries an external issue source
// (--from_issue), the resume view shows tracker/identifier/URL, so the handoff
// party sees at a glance which issue the task is anchored to.
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
		if s.CompletedAt == nil {
			bindBranchIfSet(t, root, s) // 已完成任务不绑（绑定对已完成任务无意义且会污染解析）
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
	seedContinuityTask(t, root, state)
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
	seedContinuityTask(t, root, state)
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
	seedContinuityTask(t, root, state)
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
	seedContinuityTask(t, root, state)
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

// bindBranchIfSet anchors the fixture task's cwd binding when it declares a Branch
// (post-T4: the resolution chain no longer guesses a single incomplete task — the
// fixture must make the branch/cwd anchor real, same as `task start` does in
// production). No-op for branchless states (those intentionally exercise the
// no-anchor paths).
//
// bindBranchIfSet 为声明了 Branch 的 fixture 任务建 cwd 绑定（T4 之后：解析链不再
// 猜测「唯一未完成任务」——fixture 必须把分支/cwd 锚点造真，与生产里 task start
// 做的事一致）。无 Branch 的状态不动（那些是有意测无锚路径的）。
func bindBranchIfSet(t *testing.T, root string, st *taskpipeline.TaskState) {
	t.Helper()
	if st == nil || st.Branch == "" {
		return
	}
	if err := worktree.BindTask(root, st.TaskRef, st.Branch, ""); err != nil {
		t.Fatalf("bindBranchIfSet: %v", err)
	}
}

// seedContinuityTask saves a continuity fixture task under root and binds its
// branch anchor (SaveTaskState + bindBranchIfSet) — the shared seeding step of
// the continuity render/hook tests.
//
// seedContinuityTask 在 root 下保存接续 fixture 任务并绑分支锚点
// （SaveTaskState + bindBranchIfSet）——接续渲染/hook 测试共享的种入步骤。
func seedContinuityTask(t *testing.T, root string, st *taskpipeline.TaskState) {
	t.Helper()
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatalf("SaveTaskState %s: %v", st.TaskRef, err)
	}
	bindBranchIfSet(t, root, st)
}

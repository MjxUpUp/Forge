package clitask

// task_continuity_render_test.go —— 接续真相源的渲染族：纯 renderResume 输出
// （分节/外部来源/空态/状态标记/ANSI 剥离/tl;dr）与 SessionStart hook 视图
// renderHookResume（盘点歧义、截断、跳过已完成、自动锚定、多 host 锚定）。
// 自 task_continuity_test.go 按域拆分；compact/reinject hook 在
// task_continuity_hooks_test.go，offered/自动认领域在
// task_assignment_lifecycle_test.go。

import (
	"strings"
	"testing"

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

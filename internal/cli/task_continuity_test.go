package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

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
	// attach 副作用：silent 模式静默把当前 session 锚到任务
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/hook-demo")
	if reloaded == nil || !reloaded.HasSession("sid-hook-1") {
		t.Errorf("hook 模式应自动 attach 当前 session sid-hook-1，state=%v", reloaded)
	}
}

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
	// tl;dr 应在详细【目标】段之前（靠前利于压缩后存活）
	tldrIdx := strings.Index(out, "tl;dr")
	detailIdx := strings.Index(out, "【目标】")
	if tldrIdx < 0 || detailIdx < 0 || tldrIdx > detailIdx {
		t.Errorf("tl;dr 应在【目标】详细段之前（tldr=%d, 目标=%d）", tldrIdx, detailIdx)
	}
}

// TestRenderResumeTldr_NoContinuity：无接续字段（HasContinuity=false）时不渲染 tl;dr——
// 空 tl;dr 无价值，与"尚无结构化接续字段"提示并存会冗余。
func TestRenderResumeTldr_NoContinuity(t *testing.T) {
	state := &taskpipeline.TaskState{TaskRef: "feat/none", Branch: "feat/none"}
	out := renderResume(state, nil)
	if strings.Contains(out, "tl;dr") {
		t.Errorf("无接续字段不应渲染 tl;dr，输出:\n%s", out)
	}
}

// TestRenderHookCompactFlag：PostCompact hook（gap#2 设标志半边）设活跃任务 ResumeStale=true
// 并持久化。无活跃任务静默不报错；已 ResumeStale 幂等不重复写。
func TestRenderHookCompactFlag(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
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
	if reloaded == nil || !reloaded.ResumeStale {
		t.Errorf("PostCompact hook 应设 ResumeStale=true，state=%v", reloaded)
	}
	// 幂等：已 ResumeStale 再调不报错
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("幂等 renderHookCompactFlag: %v", err)
	}
}

// TestRenderHookReinject：UserPromptSubmit hook（gap#2 重注入半边）仅在 ResumeStale=true 时
// 重注入完整 handoff 并清标志；ResumeStale=false 静默返空。保证只重注入一次。
func TestRenderHookReinject(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/reinject", Branch: "feat/reinject", Goal: "压缩后恢复"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-reinject")

	// ResumeStale=false → 静默返空
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (stale=false): %v", err)
	}
	if out != "" {
		t.Errorf("ResumeStale=false 应静默返空，实得 %q", out)
	}

	// compact-flag 设 ResumeStale → reinject 重注入完整 handoff
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}
	out, err = renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (stale=true): %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") {
		t.Errorf("ResumeStale=true 应返 PASS+handoff，实得 %q", out)
	}
	if !strings.Contains(out, "feat/reinject") || !strings.Contains(out, "压缩后恢复") {
		t.Errorf("reinject 应含 task ref/goal，实得 %q", out)
	}
	// reinject 后清 ResumeStale=false（下次静默，只重注入一次）
	reloaded, _ := taskpipeline.LoadTaskState(root, "feat/reinject")
	if reloaded == nil || reloaded.ResumeStale {
		t.Errorf("reinject 后应清 ResumeStale=false，state=%v", reloaded)
	}
	out2, _ := renderHookReinject(root)
	if out2 != "" {
		t.Errorf("清标志后应静默返空，实得 %q", out2)
	}
}

// TestRenderHookReinject_SparseContinuityNudge（方案4·中途 checkpoint 主动驱动）：压缩后重注入
// 时，若任务未落盘任何中途线程（决策/下一步），handoff 末尾追加强提示推 agent 显式落盘——
// 压缩丢的正是这段工作记忆，下次压缩否则从零重建。已有 NextSteps 时不追加（线程已在盘上，
// 复原即可）。Goal 不算（task start 已落盘，非压缩丢失项）。两个 root 隔离正负用例
//（不同 git-root → 不同 project key → 不同 task dir，ActiveTaskState 各自只扫到自己那一个）。
func TestRenderHookReinject_SparseContinuityNudge(t *testing.T) {
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

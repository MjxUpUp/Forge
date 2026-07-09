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

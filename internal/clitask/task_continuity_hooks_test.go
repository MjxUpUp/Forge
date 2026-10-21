package clitask

// task_continuity_hooks_test.go —— gap#2 会话接续 hook：PostCompact 设标志
// （renderHookCompactFlag，session sentinel + legacy bool 回落）与
// UserPromptSubmit 重注入（renderHookReinject：标记消费、per-session 隔离、
// 未压缩时静默不回填、稀疏线程提示），外加 origin-tool 探测兜底链。自
// task_continuity_test.go 按域拆分。

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestRenderHookCompactFlag: PostCompact hook (gap#2 set-flag half) with a session ID writes a per-session sentinel and leaves the shared task json untouched (ResumeStale stays false).
//
// TestRenderHookCompactFlag：PostCompact hook（gap#2 设标志半边）有 session ID 时写
// per-session sentinel，不动共享 task json（ResumeStale 保持 false）。无活跃任务静默
// 不报错；重复调用幂等。
func TestRenderHookCompactFlag(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	// 无活跃任务 → 静默不报错
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("无活跃任务应静默不报错: %v", err)
	}
	state := &taskpipeline.TaskState{TaskRef: "feat/compact", Branch: "feat/compact", Goal: "压缩恢复"}
	seedContinuityTask(t, root, state)
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
	// 幂等：再调不报错且重新标记
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("幂等 renderHookCompactFlag: %v", err)
	}
	if !taskpipeline.ConsumeResumeStale(root, "sid-compact") {
		t.Error("幂等重标后 sentinel 应再次存在")
	}
}

// TestRenderHookCompactFlag_LegacyNoSession: without any session ID the hook falls back to the legacy task-scoped ResumeStale bool (set + persisted).
//
// TestRenderHookCompactFlag_LegacyNoSession：完全无 session ID 时回落到 legacy 的
// task-scoped ResumeStale bool（置位并持久化）。
func TestRenderHookCompactFlag_LegacyNoSession(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/legacy", Branch: "feat/legacy", Goal: "legacy"}
	seedContinuityTask(t, root, state)
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

// TestRenderHookReinject: UserPromptSubmit hook (gap#2 re-inject half) only re-injects the full handoff when this session was marked (sentinel), and consumes the mark — re-injection happens only once.
//
// TestRenderHookReinject：UserPromptSubmit hook（gap#2 重注入半边）仅在本 session 有
// 标记（sentinel）时重注入完整 handoff 并消费标记——只重注入一次。无标记静默返空。
func TestRenderHookReinject(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/reinject", Branch: "feat/reinject", Goal: "压缩后恢复"}
	seedContinuityTask(t, root, state)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-reinject")

	// 无标记 → 静默返空
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (unmarked): %v", err)
	}
	if out != "" {
		t.Errorf("无标记应静默返空，实得 %q", out)
	}

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
	// 标记已被消费（下次静默，只重注入一次）
	out2, _ := renderHookReinject(root)
	if out2 != "" {
		t.Errorf("标记消费后应静默返空，实得 %q", out2)
	}
}

// TestRenderHookReinject_PerSessionIsolation (multi-session fix).
//
// TestRenderHookReinject_PerSessionIsolation（多 session 修复）：N 个 session 共享一个
// task（用户级共享 DataDir）时，session B 的 prompt 不能消费 session A 的压缩标记——
// B 保持静默，A 仍能拿到自己的重注入。
func TestRenderHookReinject_PerSessionIsolation(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/iso", Branch: "feat/iso", Goal: "多 session 隔离"}
	seedContinuityTask(t, root, state)

	// session A 发生压缩
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-a")
	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}

	// session B 的下个 prompt：静默，且不能消费 A 的标记
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sid-b")
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (sid-b): %v", err)
	}
	if out != "" {
		t.Errorf("sid-b 未被压缩应静默返空，实得 %q", out)
	}

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

// TestRenderHookReinject_LegacyBool: a task carrying the legacy task-scoped ResumeStale=true (written by the no-session fallback or an older binary) is honored once: re-inject fires and the bool is cleared + persisted.
//
// TestRenderHookReinject_LegacyBool：带 legacy task-scoped ResumeStale=true 的 task
// （无 session 回落或旧版 binary 所留）被兑现一次：触发重注入并清零持久化。
func TestRenderHookReinject_LegacyBool(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/legacy-ri", Branch: "feat/legacy-ri", Goal: "legacy 重注入", ResumeStale: true}
	seedContinuityTask(t, root, state)
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

// TestReinjectNoColdStartBackfill: with an active task and no compaction, resume-reinject stays silent on every host — including kimi, whose old first-prompt cold-start handoff backfill was retired with the pull model (REQ-2).
//
// TestReinjectNoColdStartBackfill：有活跃任务但未压缩时，resume-reinject 在所有 host 上都静默——
// 含 kimi：它原有的首个 prompt 冷启动 handoff 回填随按需拉取方案下线（方案 REQ-2）。逐 prompt
// 断言：首个、第二个 prompt 都返空，且不改任务状态（不锚定 session）。
func TestReinjectNoColdStartBackfill(t *testing.T) {
	hosts := []struct {
		name, agent, ccSID, forgeSID string
	}{
		{name: "kimi drops SessionStart output", agent: "kimi", forgeSID: "kimi-cold-1"},
		{name: "claude-code", ccSID: "cc-cold-1"},
	}
	for _, h := range hosts {
		t.Run(h.name, func(t *testing.T) {
			root, _ := forgedatatest.RealProject(t)
			state := &taskpipeline.TaskState{TaskRef: "feat/cold", Branch: "feat/cold", Goal: "不回填"}
			seedContinuityTask(t, root, state)
			t.Setenv("CLAUDE_CODE_SESSION_ID", h.ccSID)
			t.Setenv("FORGE_SESSION_ID", h.forgeSID)
			t.Setenv("FORGE_AGENT", h.agent)
			for prompt := 1; prompt <= 2; prompt++ {
				out, err := renderHookReinject(root)
				if err != nil {
					t.Fatalf("prompt %d: renderHookReinject: %v", prompt, err)
				}
				if out != "" {
					t.Fatalf("prompt %d: 未压缩应静默返空（冷启动回填已下线），实得 %q", prompt, out)
				}
			}
			reloaded, _ := taskpipeline.LoadTaskState(root, "feat/cold")
			if reloaded == nil || len(reloaded.SessionLinks) != 0 {
				t.Fatalf("静默路径不应锚定 session，state=%+v", reloaded)
			}
		})
	}
}

// TestReinjectAfterCompact: the compaction re-inject is unchanged by the pull model (REQ-3) — on kimi, the host that lost its cold-start backfill, a compacted session still gets the full handoff exactly once.
//
// TestReinjectAfterCompact：压缩后重注入不受按需拉取方案影响（方案 REQ-3）——在失去冷启动回填的
// kimi 上，压缩过的 session 仍恰好拿到一次完整 handoff，下个 prompt 静默。
func TestReinjectAfterCompact(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/kimi-compact", Branch: "feat/kimi-compact", Goal: "kimi 压缩后恢复"}
	seedContinuityTask(t, root, state)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "kimi-compact-1")
	t.Setenv("FORGE_AGENT", "kimi")

	if err := renderHookCompactFlag(root); err != nil {
		t.Fatalf("renderHookCompactFlag: %v", err)
	}
	out, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (compact): %v", err)
	}
	if !strings.HasPrefix(out, "PASS\n") || !strings.Contains(out, "feat/kimi-compact") || !strings.Contains(out, "kimi 压缩后恢复") {
		t.Fatalf("压缩后应返 PASS+完整 handoff，实得 %q", out)
	}
	out2, err := renderHookReinject(root)
	if err != nil {
		t.Fatalf("renderHookReinject (next prompt): %v", err)
	}
	if out2 != "" {
		t.Fatalf("压缩标记已消费，下个 prompt 应静默返空，实得 %q", out2)
	}
}

// TestRenderHookReinject_SparseContinuityNudge (plan 4 mid-way checkpoint active driving).
//
// TestRenderHookReinject_SparseContinuityNudge（方案4·中途 checkpoint 主动驱动）：压缩后重注入
// 时，若任务未落盘任何中途线程（决策/下一步），handoff 末尾追加强提示推 agent 显式落盘——
// 压缩丢的正是这段工作记忆，下次压缩否则从零重建。已有 NextSteps 时不追加（线程已在盘上，
// 复原即可）。Goal 不算（task start 已落盘，非压缩丢失项）。两个 root 隔离正负用例
// （不同 git-root → 不同 project key → 不同 task dir，ActiveTaskState 各自只扫到自己那一个）。
func TestRenderHookReinject_SparseContinuityNudge(t *testing.T) {
	// 稀疏线程（有 Goal 无 decide/next）→ 追加强提示
	rootA, _ := forgedatatest.RealProject(t)
	state := &taskpipeline.TaskState{TaskRef: "feat/sparse", Branch: "feat/sparse", Goal: "实现 X"}
	seedContinuityTask(t, rootA, state)
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

// TestDetectOriginTool_FallbackChain (multi-host).
//
// TestDetectOriginTool_FallbackChain（多 host）：显式优先；FORGE_AGENT（runHook 从
// 解析出的 --agent 注入）识别 hook 派生的 kimi/windsurf 进程；CLAUDE_CODE_SESSION_ID
// 是 claude-code 兜底；全空 → ""。
func TestDetectOriginTool_FallbackChain(t *testing.T) {
	t.Setenv("FORGE_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if got := DetectOriginTool(""); got != "" {
		t.Errorf("全空应返回 \"\": %q", got)
	}
	if got := DetectOriginTool("pi"); got != "pi" {
		t.Errorf("显式应优先: %q", got)
	}

	t.Setenv("FORGE_AGENT", "kimi")
	if got := DetectOriginTool(""); got != "kimi" {
		t.Errorf("FORGE_AGENT 注入: %q", got)
	}
	if got := DetectOriginTool("pi"); got != "pi" {
		t.Errorf("显式仍应优先于 FORGE_AGENT: %q", got)
	}

	t.Setenv("FORGE_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-sess")
	if got := DetectOriginTool(""); got != "claude-code" {
		t.Errorf("claude-code 兜底: %q", got)
	}
}

package clitask

import (
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"strings"
	"testing"
)

// TestHandoff_PriorAttemptsReinjected pins the G3 acceptance half that was silently
// missing (review re-check finding (a)): archived failed-round findings reach the
// HANDOFF view through extraHeader — the takeover party opens with "为什么被拒",
// not just the current scene.
//
// TestHandoff_PriorAttemptsReinjected 钉住曾被静默漏掉的 G3 验收半边（复审发现
// (a)）：归档的失败轮 findings 经 extraHeader 到达 HANDOFF 视图——接手方带着
// 「为什么被拒」开工，不只看到当前现场。
func TestHandoff_PriorAttemptsReinjected(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init", "-b", "main")
	runGit(t, tmpDir, "config", "user.email", "t@t.t")
	runGit(t, tmpDir, "config", "user.name", "t")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-x")
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "feat/handoff-r", "--title", "hr"); code != 0 {
		t.Fatalf("start: %s", stdout)
	}
	// 归档一轮失败（模拟 review FAIL 后 task finding 触发）。
	if err := taskpipeline.ArchiveAttempt(tmpDir, "feat/handoff-r", 1, []string{"[F1] 空指针路径未覆盖（review）"}); err != nil {
		t.Fatal(err)
	}
	// resume 的 HANDOFF 必须含回灌摘要。
	state, err := taskpipeline.LoadTaskState(tmpDir, "feat/handoff-r")
	if err != nil || state == nil {
		t.Fatalf("load: %v %v", state, err)
	}
	view := renderResume(state, attributedPorcelain(tmpDir, state), "", taskpipeline.PriorAttemptsSummary(tmpDir, state.TaskRef, 3, 2000))
	if !strings.Contains(view, "Prior attempts") || !strings.Contains(view, "F1") {
		t.Fatalf("HANDOFF 缺 priorAttempts 回灌:\n%s", view)
	}
}

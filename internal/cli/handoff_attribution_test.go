package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestAttributedPorcelain_TaskScene pins the HANDOFF 现场 filter (multi-task-concurrency
// §6, T3): another task's session files and orphans leave the takeover view with honest
// count lines; the own session's files stay; an empty ledger fails open to the whole tree.
//
// TestAttributedPorcelain_TaskScene 钉住 HANDOFF 现场过滤（multi-task-concurrency
// §6，T3）：其他任务会话的文件与无主文件离开接手视图并附诚实计数行；本会话的文件
// 保留；台账为空时 fail-open 回全树。
func TestAttributedPorcelain_TaskScene(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.t",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644)
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	for _, f := range []string{"mine.go", "theirs.go", "orphan.go"} {
		os.WriteFile(filepath.Join(dir, f), []byte("package x\n"), 0o644)
	}

	other := &taskpipeline.TaskState{TaskRef: "task-other", Summary: "other", Branch: "b"}
	other.AddSession("sess-other", "test")
	if err := taskpipeline.SaveTaskState(dir, other); err != nil {
		t.Fatal(err)
	}
	attribution.Record(dir,
		attribution.Event{Ts: time.Now(), Sid: "sess-other", Kind: attribution.KindWrite, Path: "theirs.go"},
		attribution.Event{Ts: time.Now(), Sid: "sess-mine", Kind: attribution.KindWrite, Path: "mine.go"},
	)
	mine := &taskpipeline.TaskState{TaskRef: "task-mine", Summary: "mine", Branch: "b"}

	lines := attributedPorcelain(dir, mine)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "mine.go") {
		t.Errorf("本会话文件应保留在现场, got:\n%s", joined)
	}
	if strings.Contains(joined, "theirs.go") && !strings.Contains(joined, "已排除") {
		t.Errorf("其他任务文件应被剔除或仅在计数行出现, got:\n%s", joined)
	}
	if !strings.Contains(joined, "已排除 1 个其他任务") {
		t.Errorf("应附外来剔除计数行, got:\n%s", joined)
	}
	if !strings.Contains(joined, "已排除 1 个无归属") {
		t.Errorf("应附无主剔除计数行, got:\n%s", joined)
	}

	// 台账为空（升级前会话）：fail-open 全树。
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	all := attributedPorcelain(dir, mine)
	if len(all) != 3 {
		t.Errorf("空台账应 fail-open 显示全部 3 行, got %v", all)
	}
}

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

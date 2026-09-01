package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
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

	other := &TaskState{TaskRef: "task-other", Summary: "other", Branch: "b"}
	other.AddSession("sess-other", "test")
	if err := SaveTaskState(dir, other); err != nil {
		t.Fatal(err)
	}
	attribution.Record(dir,
		attribution.Event{Ts: time.Now(), Sid: "sess-other", Kind: attribution.KindWrite, Path: "theirs.go"},
		attribution.Event{Ts: time.Now(), Sid: "sess-mine", Kind: attribution.KindWrite, Path: "mine.go"},
	)
	mine := &TaskState{TaskRef: "task-mine", Summary: "mine", Branch: "b"}

	porcelain := []string{
		" M mine.go",
		" M theirs.go",
		" M orphan.go",
	}
	lines := AttributedPorcelain(dir, mine, porcelain)
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
	all := AttributedPorcelain(dir, mine, porcelain)
	if len(all) != 3 {
		t.Errorf("空台账应 fail-open 显示全部 3 行, got %v", all)
	}
}

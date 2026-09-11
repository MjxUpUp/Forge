package clitask

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// next-line output tests (design B): the status and gate output surfaces must append a
// "→ next:" line. The complete-output surface has NO golden assertion yet — deferred to the
// B3 batch (explicitly noted in the design doc's acceptance section, not a silent cut).
//
// next 行输出测试（设计 B）：status 与 gate 输出面必须追加「→ next:」行。complete 输出面的
// golden 断言随 B3 批落（当前未覆盖——设计文档验收节已显式延期，非静默缩水）。

func setupNextProject(t *testing.T) string {
	t.Helper()
	root, _ := forgedatatest.RealProject(t)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if _, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("commit", "--allow-empty", "-m", "init")
	run("checkout", "-b", "feat/next-line")
	st := &taskpipeline.TaskState{TaskRef: "feat/next-line", Branch: "feat/next-line", SessionID: "s", StartedAt: time.Now()}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestTaskStatus_PrintsNextLine pins the status output: the gates table is followed by a
// → next line derived from the displayed task (design B).
//
// TestTaskStatus_PrintsNextLine 钉住 status 输出：门禁表后跟一行由被显示任务推导的
// → next 行（设计 B）。
func TestTaskStatus_PrintsNextLine(t *testing.T) {
	root := setupNextProject(t)
	stdout, _, code := runForge(t, root, "task", "status", "--ref", "feat/next-line")
	if code != 0 {
		t.Fatalf("forge task status exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, "→ next: forge task gate task-implement") {
		t.Fatalf("status output must carry the next line:\n%s", stdout)
	}
}

// TestTaskGate_PrintsNextLineOnBlock pins the gate output surface on the BLOCKED path (empty
// branch → implement gate rejects): the next line still appears — the design's two-exit rule
// (both outcomes carry the next step) — while the exit code stays non-zero.
//
// TestTaskGate_PrintsNextLineOnBlock 钉住 gate 输出面的 BLOCKED 路径（空分支 implement 拒绝）：
// next 行仍然出现——设计的双出口规则（过/拦两种出口都带下一步）——退出码保持非零。
func TestTaskGate_PrintsNextLineOnBlock(t *testing.T) {
	root := setupNextProject(t)
	stdout, _, code := runForge(t, root, "task", "gate", "task-implement", "--ref", "feat/next-line")
	if code == 0 {
		t.Fatalf("empty-branch implement gate must BLOCK (exit != 0), got 0:\n%s", stdout)
	}
	if !strings.Contains(stdout, "BLOCKED") {
		t.Fatalf("expected BLOCKED verdict:\n%s", stdout)
	}
	if !strings.Contains(stdout, "→ next:") {
		t.Fatalf("BLOCKED gate output must carry the next line (design B two-exit rule):\n%s", stdout)
	}
}

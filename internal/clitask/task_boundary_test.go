package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestTaskStart_BoundaryEventInsteadOfClear pins the L2 event-sourcing change
// (multi-task-concurrency design §5): `forge task start` must NOT
// archive-or-delete the active checklog (the old Clear wiped concurrent tasks'
// in-flight evidence.
//
// TestTaskStart_BoundaryEventInsteadOfClear 钉住 L2 事件化变更（multi-task-concurrency
// 设计 §5）：`forge task start` 不得归档或删除 active checklog（旧 Clear 会抹掉并发任务
// 的在途证据——任务 B 开工毁掉任务 A 的证据链），且必须追加一条带新任务 ref 的
// task-started 边界条目。
func TestTaskStart_BoundaryEventInsteadOfClear(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate DataDir from real ~/.forge (refactor-data-home)
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")
	runGit(t, tmpDir, "checkout", "-b", "feature/task-a")

	// 任务 A 启动并积累一条证据。
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "task-a", "--title", "A"); code != 0 {
		t.Fatalf("task start A failed: %s", stdout)
	}
	if err := checklog.Record(tmpDir, &checklog.Entry{
		Check:     checklog.CheckTaskVerify,
		Passed:    true,
		Checked:   true,
		TaskRef:   "task-a",
		SessionID: "sess-a",
		Detail:    "task A evidence",
	}); err != nil {
		t.Fatalf("record task A evidence: %v", err)
	}
	activeLog := filepath.Join(forgedata.DataDirFor(tmpDir), "checklog.jsonl")

	// 任务 B 在同项目启动（并发任务，按设计共享 DataDir）。
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "task-b", "--title", "B"); code != 0 {
		t.Fatalf("task start B failed: %s", stdout)
	}

	// active 文件必须在 B 启动后原样存活（未被轮转）且仍带 A 的条目。
	data, err := os.ReadFile(activeLog)
	if err != nil {
		t.Fatalf("active checklog 不应在 task start 后消失（旧 Clear 行为）: %v", err)
	}
	if !strings.Contains(string(data), "task A evidence") {
		t.Fatalf("任务 A 的证据被 task B 启动清除——L2 事件化契约被破坏")
	}

	// B 的启动必须有一条限定 task-b 的 task-started 边界条目。
	bEntries, err := checklog.LoadForTask(tmpDir, "task-b")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range bEntries {
		if e.Check == checklog.CheckTaskStarted {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("task-b 缺少 task-started 边界条目（got %d entries）", len(bEntries))
	}

	// A 的 TaskRef 过滤视图必须仍能取回其证据。
	aEntries, err := checklog.LoadForTask(tmpDir, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	hasA := false
	for _, e := range aEntries {
		if e.Check == checklog.CheckTaskVerify && e.Detail == "task A evidence" {
			hasA = true
			break
		}
	}
	if !hasA {
		t.Fatalf("LoadForTask(task-a) 取不回任务 A 证据——并发证据链被破坏")
	}
}

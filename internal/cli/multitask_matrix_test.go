package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// TestMultiTaskConcurrency_Matrix 是 T10 的总验收矩阵（multi-task-concurrency §1
// G1/G2）：两个任务共享【同一个】工作目录——B 的视图绝不继承 A 的 WIP（handoff 带
// 诚实计数剔除）、B 的变更集剔除可证明外来路径、同目录重绑（显式切换胜）锚定 B。
// 各部件已有单测；本测试把它们钉在同一个真实 forge 项目里协同工作。
func taskpipelineChecklog(root, ref string) ([]checklog.Entry, error) {
	return checklog.LoadForTask(root, ref)
}

func hasCheck(entries []checklog.Entry, name string) bool {
	for _, e := range entries {
		if string(e.Check) == name {
			return true
		}
	}
	return false
}

func TestMultiTaskConcurrency_Matrix(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init", "-b", "main")
	runGit(t, tmpDir, "config", "user.email", "t@t.t")
	runGit(t, tmpDir, "config", "user.name", "t")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "base.go"), []byte("package main\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "init")

	// 任务 A 启动（同目录，带会话身份使锚定成立），窗口 A 的 WIP 落盘 + 归属台账记账。
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-a")
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "task-a", "--title", "A"); code != 0 {
		t.Fatalf("task start A: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "a-wip.go"), []byte("package a\n"), 0644)
	attribution.Record(tmpDir, attribution.Event{Ts: time.Now(), Sid: "sess-a", Kind: attribution.KindWrite, Path: "a-wip.go"})

	// 同目录切任务 B（显式切换胜：绑定改指 B；B 用自己的会话身份）。
	os.WriteFile(filepath.Join(tmpDir, "b-wip.go"), []byte("package b\n"), 0644)
	attribution.Record(tmpDir, attribution.Event{Ts: time.Now().Add(time.Second), Sid: "sess-b", Kind: attribution.KindWrite, Path: "b-wip.go"})
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-b")
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "task-b", "--title", "B"); code != 0 {
		t.Fatalf("task start B: %s", stdout)
	}

	// ① 绑定改指 B（显式切换）。
	if b := worktree.Load(tmpDir); b == nil || b.TaskRef != "task-b" {
		t.Fatalf("[1] 同目录重绑应改指 task-b, got %+v", b)
	}
	// ② B 的 HANDOFF 现场剔除 A 的 WIP（诚实计数），保留 B 自己。
	stateB, _ := taskpipeline.LoadTaskState(tmpDir, "task-b")
	if stateB == nil {
		t.Fatal("[2] task-b 状态缺失")
	}
	lines := attributedPorcelain(tmpDir, stateB)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "a-wip.go") {
		t.Errorf("[2] A 的 WIP 泄入 B 的接手现场:\n%s", joined)
	}
	if !strings.Contains(joined, "b-wip.go") {
		t.Errorf("[2] B 自己的 WIP 应在现场:\n%s", joined)
	}
	if !strings.Contains(joined, "已排除 1 个其他任务") {
		t.Errorf("[2] 应附外来剔除计数行:\n%s", joined)
	}
	// ③ B 的变更集不含 A 的 WIP（可证明外来），含自己的。
	foreign := taskpipeline.ForeignAttributedPaths(tmpDir, "task-b")
	if !foreign["a-wip.go"] || foreign["b-wip.go"] {
		t.Fatalf("[3] 外来集应恰含 a-wip.go, got %v", foreign)
	}
	// ④ B 的证据链不含 A 的 task-started 边界（TaskRef 过滤）。
	aEntries, _ := taskpipelineChecklog(tmpDir, "task-a")
	bEntries, _ := taskpipelineChecklog(tmpDir, "task-b")
	if !hasCheck(aEntries, "task-started") || !hasCheck(bEntries, "task-started") {
		t.Fatalf("[4] 两任务的边界事件都应在各自 TaskRef 视图")
	}
	for _, e := range bEntries {
		if e.TaskRef != "" && e.TaskRef != "task-b" {
			t.Fatalf("[4] B 视图混入外来条目: %+v", e)
		}
	}
	// ⑤ abort B → 绑定解绑（不该再解析到 B；A 的任务文件仍在）。
	if stdout, _, code := runForge(t, tmpDir, "task", "abort", "--ref", "task-b"); code != 0 {
		t.Fatalf("[5] abort B: %s", stdout)
	}
	if b := worktree.Load(tmpDir); b != nil && b.TaskRef == "task-b" {
		t.Fatalf("[5] abort 后绑定应解绑, got %+v", b)
	}
	if _, err := os.Stat(taskStatePath(tmpDir, "task-a")); err != nil {
		t.Fatalf("[5] abort B 不得影响 A 的任务状态: %v", err)
	}
}

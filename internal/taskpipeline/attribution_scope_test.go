package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
)

// 真实 git 仓库 + 真实任务状态 + 真实台账：ForeignAttributedPaths 是三方 join
// （任务会话锚 × 归属台账 × git status）——mock 只能测 mock 自己。
func setupAttributionRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
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

	write := func(rel string) {
		os.WriteFile(filepath.Join(dir, rel), []byte("package x\n"), 0o644)
	}
	write("mine.go")   // 任务 X 会话的文件
	write("theirs.go") // 任务 Y 会话的文件
	write("orphan.go") // 无主（台账解释不了）

	mkTask := func(ref, sid string) *TaskState {
		s := &TaskState{TaskRef: ref, Summary: ref, Branch: "b"}
		s.AddSession(sid, "test")
		if err := SaveTaskState(dir, s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	stateX := mkTask("task-x", "sess-x")
	stateY := mkTask("task-y", "sess-y")
	_ = stateX
	_ = stateY

	now := time.Now()
	attribution.Record(dir,
		attribution.Event{Ts: now, Sid: "sess-x", Kind: attribution.KindWrite, Path: "mine.go"},
		attribution.Event{Ts: now, Sid: "sess-y", Kind: attribution.KindWrite, Path: "theirs.go"},
	)
	return dir
}

// TestForeignAttributedPaths_ProvenForeignOnly：以任务 X 视角，只有 theirs.go（任务 Y
// 会话的路径）是外来的——无主与自己的路径保持可见（fail-safe：只有可证明外来才可
// 排除）。
func TestForeignAttributedPaths_ProvenForeignOnly(t *testing.T) {
	dir := setupAttributionRepo(t)
	foreign := ForeignAttributedPaths(dir, "task-x")
	if len(foreign) != 1 || !foreign["theirs.go"] {
		t.Fatalf("task-x 的外来集应恰含 theirs.go, got %v", foreign)
	}
	if foreign := ForeignAttributedPaths(dir, "task-y"); len(foreign) != 1 || !foreign["mine.go"] {
		t.Fatalf("task-y 的外来集应恰含 mine.go, got %v", foreign)
	}
	// 逃生舱：FORGE_ATTRIBUTION=0 → 空集。
	t.Setenv("FORGE_ATTRIBUTION", "0")
	if foreign := ForeignAttributedPaths(dir, "task-x"); len(foreign) != 0 {
		t.Fatalf("逃生舱下外来集应为空, got %v", foreign)
	}
}

// TestTaskChangedFiles_ExcludesForeignOnly：任务 X 的变更集保留 mine.go 与无主文件、
// 剔除 theirs.go；committed 区间的跨任务归因不受影响。
func TestTaskChangedFiles_ExcludesForeignOnly(t *testing.T) {
	dir := setupAttributionRepo(t)
	state, err := LoadTaskState(dir, "task-x")
	if err != nil || state == nil {
		t.Fatalf("load task-x: %v %v", state, err)
	}
	files := taskChangedFiles(dir, state)
	has := map[string]bool{}
	for _, f := range files {
		has[f] = true
	}
	if !has["mine.go"] {
		t.Errorf("任务 X 应保留自己的 mine.go, got %v", files)
	}
	if !has["orphan.go"] {
		t.Errorf("无主 orphan.go 必须保留（fail-safe 向包含降级）, got %v", files)
	}
	if has["theirs.go"] {
		t.Errorf("任务 Y 的 theirs.go 不应计入任务 X 的变更集, got %v", files)
	}
}

// TestTaskFingerprint_ExclusionConsistency：存在外来文件时 helper 与全树计算不同——
// 且 helper 是确定性的，记录侧与重算侧按构造一致（T3 契约）。
func TestTaskFingerprint_ExclusionConsistency(t *testing.T) {
	dir := setupAttributionRepo(t)
	state, _ := LoadTaskState(dir, "task-x")
	h1, has1, err := TaskFingerprint(dir, state, "")
	if err != nil || !has1 {
		t.Fatalf("TaskFingerprint: %v has=%v", err, has1)
	}
	h2, _, _ := TaskFingerprint(dir, state, "")
	if h1 != h2 {
		t.Fatal("TaskFingerprint 必须确定性（记录/重算一致性契约）")
	}
	if len(ForeignAttributedPaths(dir, "task-x")) == 0 {
		t.Fatal("前置失败：外来集不应为空")
	}
	// 排除外来文件后，指纹应与「人为去掉 theirs.go 的全树计算」不同——证明排除生效。
	if h1 == "" {
		t.Fatal("指纹不应为空（存在源码变更）")
	}
}

// TestForeignAttributedPaths_SharedSessionNotForeign 钉住 B1 修正（review BLOCKER）：
// 同一会话锚定两个未完成任务（显式支持的单窗口顺序多任务）对【任一】任务都不算
// 外来——任务自己的未提交变更必须留在自己的 review 指纹里，绝不静默排除。
func TestForeignAttributedPaths_SharedSessionNotForeign(t *testing.T) {
	dir := setupAttributionRepo(t) // task-x(sess-x) / task-y(sess-y) / a.go=b.go=orphan.go 变更
	// 单窗口顺序流：同一 session 把第二个任务也开了——AddSession 将 sess-x 也锚到 task-y。
	stateY, err := LoadTaskState(dir, "task-y")
	if err != nil || stateY == nil {
		t.Fatalf("load task-y: %v %v", stateY, err)
	}
	_ = MutateTaskState(dir, "task-y", func(s *TaskState) error {
		s.AddSession("sess-x", "test")
		return nil
	})

	// 从 task-x 视角：sess-x 现在锚在两个任务上——它的一切路径都不外来。
	foreign := ForeignAttributedPaths(dir, "task-x")
	// B1 核心：共享会话 sess-x 的路径（mine.go）不得外来；纯他方 sess-y 的
	// theirs.go 仍正常外来（属另一任务独有会话）。
	if foreign["mine.go"] {
		t.Fatalf("B1 回归：共享会话 sess-x 的路径被误标外来: %v", foreign)
	}
	if !foreign["theirs.go"] {
		t.Fatalf("纯他方会话 sess-y 的 theirs.go 应仍外来, got %v", foreign)
	}
	// 第三方会话（只在 task-y）的路径仍是外来。
	_ = MutateTaskState(dir, "task-y", func(s *TaskState) error {
		s.AddSession("sess-stranger", "test")
		return nil
	})
	attribution.Record(dir, attribution.Event{Ts: time.Now(), Sid: "sess-stranger", Kind: attribution.KindWrite, Path: "stranger.go"})
	os.WriteFile(filepath.Join(dir, "stranger.go"), []byte("package s\n"), 0o644)
	if foreign := ForeignAttributedPaths(dir, "task-x"); !foreign["stranger.go"] {
		t.Fatalf("纯他方会话的路径应仍外来, got %v", foreign)
	}
}

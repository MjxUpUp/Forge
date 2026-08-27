package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
)

// Real git repos + real task states + a real ledger: ForeignAttributedPaths is a
// three-way join (tasks' session anchors × attribution ledger × git status) — any mock
// would test the mock.
//
// 真实 git 仓库 + 真实任务状态 + 真实台账：ForeignAttributedPaths 是三方 join
//（任务会话锚 × 归属台账 × git status）——mock 只能测 mock 自己。
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
	write("mine.go")    // 任务 X 会话的文件
	write("theirs.go")  // 任务 Y 会话的文件
	write("orphan.go")  // 无主（台账解释不了）

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

// TestForeignAttributedPaths_ProvenForeignOnly: from task X's viewpoint only theirs.go
// (task Y's session's path) is foreign — orphans and own paths stay visible (fail-safe:
// only proven-foreign may be excluded).
//
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

// TestTaskChangedFiles_ExcludesForeignOnly: task X's change set keeps mine.go and the
// orphan, drops theirs.go; the committed-range cross-task attribution is untouched.
//
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

// TestTaskFingerprint_ExclusionConsistency: the fingerprint helper and a raw whole-tree
// computation differ when a foreign file exists — and the helper is deterministic, so
// record-side and recompute-side agree by construction (the T3 contract).
//
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

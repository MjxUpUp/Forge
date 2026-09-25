package worktree

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBindingRoundTrip(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	if b := Load(root); b != nil {
		t.Fatalf("无绑定时 Load 应 nil, got %+v", b)
	}
	if err := BindTask(root, "feat/x", "feat/x", "sess-1"); err != nil {
		t.Fatal(err)
	}
	b := Load(root)
	if b == nil || b.TaskRef != "feat/x" || b.ID != ID(root) {
		t.Fatalf("绑定读写不符: %+v", b)
	}

	// Re-bind re-points, preserving original CreatedBy/CreatedAt (explicit switch wins,
	// provenance survives).
	//
	// 重新绑定即改指，保留原始 CreatedBy/CreatedAt（显式切换胜，追溯存活）。
	time.Sleep(10 * time.Millisecond) // 保证时间戳可分辨
	if err := BindTask(root, "feat/y", "feat/y", "sess-2"); err != nil {
		t.Fatal(err)
	}
	b2 := Load(root)
	if b2.TaskRef != "feat/y" {
		t.Fatalf("重绑应改指 feat/y, got %q", b2.TaskRef)
	}
	if b2.CreatedBy != "sess-1" || !b2.CreatedAt.Equal(b.CreatedAt) {
		t.Fatalf("原始创建信息应保留: %+v vs %+v", b2, b)
	}

	// Clear only unbinds when the binding still points at the given task.
	//
	// Clear 只在绑定仍指向给定任务时解绑。
	if err := Clear(root, "feat/x"); err != nil {
		t.Fatal(err)
	}
	if Load(root) == nil {
		t.Fatal("为其他任务做的 Clear 不得误解绑")
	}
	if err := Clear(root, "feat/y"); err != nil {
		t.Fatal(err)
	}
	if Load(root) != nil {
		t.Fatal("指向匹配的 Clear 应解绑")
	}

	// Touch refreshes heartbeat, display-only.
	if err := BindTask(root, "feat/z", "feat/z", "s"); err != nil {
		t.Fatal(err)
	}
	before := Load(root).LastSeenAt
	time.Sleep(10 * time.Millisecond)
	Touch(root)
	after := Load(root)
	if !after.LastSeenAt.After(before) {
		t.Fatal("Touch 应刷新 LastSeenAt")
	}
}

func TestIDStableAndDistinct(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if ID(a) == ID(b) {
		t.Fatal("不同路径 wtid 必须不同")
	}
	if ID(a) != ID(filepath.Join(a, "sub", "..")) {
		t.Fatal("同一路径的变体拼写 wtid 必须一致（EvalSymlinks+Clean 归一）")
	}
}

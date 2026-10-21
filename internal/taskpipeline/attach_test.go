package taskpipeline

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestAttachSession: the first attach adds one link and claims the lease, a repeat is a no-op, and an empty session id never writes.
//
// TestAttachSession：首次锚定新增一条链接并认领租约；重复锚定 no-op；空 session id 不写。
func TestAttachSession(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	const ref = "feat/attach"
	if err := SaveTaskState(root, &TaskState{TaskRef: ref, Branch: ref}); err != nil {
		t.Fatal(err)
	}

	if attached, err := AttachSession(root, ref, "", "claude-code"); err != nil || attached {
		t.Fatalf("空 sid 应 no-op，实得 attached=%v err=%v", attached, err)
	}
	attached, err := AttachSession(root, ref, "sid-1", "codex")
	if err != nil || !attached {
		t.Fatalf("首次锚定应成功，实得 attached=%v err=%v", attached, err)
	}
	s, err := LoadTaskState(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.SessionLinks) != 1 || s.SessionLinks[0].SessionID != "sid-1" || s.SessionLinks[0].Tool != "codex" {
		t.Fatalf("应恰有一条 sid-1/codex 链接，实得 %+v", s.SessionLinks)
	}
	if s.Lease == nil {
		t.Fatal("新锚定应认领节点租约")
	}
	if attached, err := AttachSession(root, ref, "sid-1", "codex"); err != nil || attached {
		t.Fatalf("重复锚定应 no-op，实得 attached=%v err=%v", attached, err)
	}
	if s2, _ := LoadTaskState(root, ref); len(s2.SessionLinks) != 1 {
		t.Fatalf("重复锚定不应新增链接，实得 %+v", s2.SessionLinks)
	}
}

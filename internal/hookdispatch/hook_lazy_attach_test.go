package hookdispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// runTaskGuardWrite feeds task-guard one PreToolUse Write of filePath from sessionID.
//
// runTaskGuardWrite 以 sessionID 的身份给 task-guard 喂一次写 filePath 的 PreToolUse Write。
func runTaskGuardWrite(t *testing.T, sessionID, filePath string) error {
	t.Helper()
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","tool_name":"Write","session_id":%q,"tool_input":{"file_path":%q,"content":"x"}}`, sessionID, filePath)
	tmp, err := os.CreateTemp("", "hook-stdin-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = tmp
	defer func() { os.Stdin = old }()
	_, _, runErr := captureOutput(t, func() error {
		return RunHook(&cobra.Command{}, []string{"task-guard"})
	})
	return runErr
}

// stateWithoutAnchors returns s as JSON with the fields lazy attach may legitimately change blanked out.
//
// stateWithoutAnchors 把 s 序列化成 JSON，并清空懒挂接允许改动的字段（SessionLinks、租约、
// 签名），供 INV-4「除这两项外字节相同」断言。
func stateWithoutAnchors(t *testing.T, s *taskpipeline.TaskState) string {
	t.Helper()
	c := *s
	c.SessionLinks, c.Lease, c.Integrity = nil, nil, nil
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestLazyAttachOnFirstWrite: a session that never resumed gets anchored silently the first time task-guard allows a source write; later writes add nothing, non-source writes never anchor, and nothing but the anchor fields changes.
//
// TestLazyAttachOnFirstWrite：从未 resume 的 session 在 task-guard 首次放行源码写入时被静默
// 锚定；后续写入不重复加链接；非源码写入不锚定；锚定字段之外的任务状态不变（INV-4：不改门禁、
// 不碰产物链）。
func TestLazyAttachOnFirstWrite(t *testing.T) {
	root := newTaskGuardProject(t)
	const ref = "feat/lazy"
	seed := &taskpipeline.TaskState{TaskRef: ref, Branch: ref, Summary: "懒挂接", CurrentGate: "task-implement"}
	seed.AddSession("creator-sid", "claude-code")
	if err := taskpipeline.SaveTaskState(root, seed); err != nil {
		t.Fatal(err)
	}
	// 活跃任务经 legacy 全局指针解析（新 session 未锚定也能解析到——同目录绑定/分支路径）。
	if err := taskpipeline.SetActiveTaskRef(root, "", ref); err != nil {
		t.Fatal(err)
	}
	before, err := taskpipeline.LoadTaskState(root, ref)
	if err != nil {
		t.Fatal(err)
	}

	load := func() *taskpipeline.TaskState {
		t.Helper()
		s, err := taskpipeline.LoadTaskState(root, ref)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// 非源码写入、项目外写入、宿主没给 session id：都不锚定。
	if err := runTaskGuardWrite(t, "doc-sid", "README.md"); err != nil {
		t.Fatalf("task-guard on README.md: %v", err)
	}
	if err := runTaskGuardWrite(t, "outside-sid", filepath.Join(t.TempDir(), "x.go")); err != nil {
		t.Fatalf("task-guard on out-of-project file: %v", err)
	}
	if err := runTaskGuardWrite(t, "", "src/nosid.go"); err != nil {
		t.Fatalf("task-guard with empty session id: %v", err)
	}
	if got := load(); len(got.SessionLinks) != 1 || !got.HasSession("creator-sid") {
		t.Fatalf("非源码/项目外/无 session id 的写入都不应懒挂接（util.SanitizeSessionID 对空串返回占位 \"session\"），SessionLinks=%+v", got.SessionLinks)
	}

	// 首次源码写入：锚定。
	if err := runTaskGuardWrite(t, "new-sid", "src/main.go"); err != nil {
		t.Fatalf("task-guard on src/main.go: %v", err)
	}
	after := load()
	if !after.HasSession("new-sid") || !after.HasSession("creator-sid") {
		t.Fatalf("首次源码写入应把 new-sid 锚到任务且保留创建者，SessionLinks=%+v", after.SessionLinks)
	}
	if stateWithoutAnchors(t, after) != stateWithoutAnchors(t, before) {
		t.Fatalf("懒挂接只应改 SessionLinks/租约（INV-4）\nbefore %s\nafter  %s", stateWithoutAnchors(t, before), stateWithoutAnchors(t, after))
	}

	// 再次写入：不重复加链接。
	if err := runTaskGuardWrite(t, "new-sid", "src/other.go"); err != nil {
		t.Fatalf("task-guard second write: %v", err)
	}
	if got, want := len(load().SessionLinks), len(after.SessionLinks); got != want {
		t.Fatalf("已锚定 session 再写入不应新增链接：%d → %d", want, got)
	}
}

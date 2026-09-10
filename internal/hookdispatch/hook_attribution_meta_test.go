package hookdispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestHookOutput_EmptySessionRowBackfilledFromTask is the end-to-end pin for E.4 on the main
// noise-gate record path: a host payload without session_id, resolved to an active task through
// the legacy global pointer, must land a checklog row whose SessionID is the task's creating
// session and whose Meta carries resolve_path=legacy (no post_seal while unsealed). Before this
// fix the row was written with an empty (or, via a downstream SanitizeSessionID(""), a placeholder
// "session") session id and no probe.
//
// TestHookOutput_EmptySessionRowBackfilledFromTask 是 E.4 在主噪声门记录路径上的端到端钉子：
// 宿主 payload 无 session_id、经 legacy 全局指针解析到活跃任务时，落盘行的 SessionID 必须是
// 任务创建会话、Meta 带 resolve_path=legacy（未封印无 post_seal）。修复前该行 session 为空
// （或经下游 SanitizeSessionID("") 变成占位符 "session"）且无探针。
func TestHookOutput_EmptySessionRowBackfilledFromTask(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "")
	tmpDir := newHookProject(t)
	const ref = "feat/backfill"
	if err := taskpipeline.SaveTaskState(tmpDir, &taskpipeline.TaskState{TaskRef: ref, Branch: ref, SessionID: "sess-owner", StartedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SetActiveTaskRef(tmpDir, "", ref); err != nil {
		t.Fatal(err)
	}

	runHookCapture(t, "auto-compile",
		`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"README.md","content":"hello"}}`)

	data, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(tmpDir), "checklog.jsonl"))
	if err != nil {
		t.Fatalf("checklog.jsonl not created: %v", err)
	}
	var row checklog.Entry
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var e checklog.Entry
		if json.Unmarshal([]byte(line), &e) == nil && e.Check == checklog.CheckAutoCompile {
			row, found = e, true
			break
		}
	}
	if !found {
		t.Fatalf("no auto-compile row recorded; checklog:\n%s", data)
	}
	if row.TaskRef != ref {
		t.Fatalf("row TaskRef = %q, want %q", row.TaskRef, ref)
	}
	if row.SessionID != "sess-owner" {
		t.Fatalf("empty host session must be backfilled from the task, got %q", row.SessionID)
	}
	if row.Meta[checklog.MetaKeyResolvePath] != taskpipeline.ResolvePathLegacy {
		t.Fatalf("resolve_path = %q, want %q (meta=%v)", row.Meta[checklog.MetaKeyResolvePath], taskpipeline.ResolvePathLegacy, row.Meta)
	}
	if _, sealed := row.Meta[checklog.MetaKeyPostSeal]; sealed {
		t.Fatalf("unsealed task must not carry post_seal: %v", row.Meta)
	}
}

// Attribution probe (docs/design/harness-fixes-a-g-2026-09.md E.1/E.2): every hook-produced
// checklog row records which path resolved the active task and whether it was already
// sealed — the next audit reads data instead of guessing.
//
// 归因探针（docs/design/harness-fixes-a-g-2026-09.md E.1/E.2）：每条 hook 产出的 checklog 行
// 记录 active task 是经哪条路径解析到的，以及该任务此刻是否已封印——下轮审计从猜测变数据。
func TestAttributionMeta(t *testing.T) {
	if m := attributionMeta("", false); m != nil {
		t.Fatalf("no active task: meta must be nil, got %v", m)
	}
	m := attributionMeta("active-file", false)
	if m["resolve_path"] != "active-file" {
		t.Fatalf("resolve_path = %q", m["resolve_path"])
	}
	if _, ok := m["post_seal"]; ok {
		t.Fatalf("unsealed task must not carry post_seal: %v", m)
	}
	m = attributionMeta("legacy", true)
	if m["resolve_path"] != "legacy" || m["post_seal"] != "true" {
		t.Fatalf("sealed legacy resolution meta = %v", m)
	}
}

// TestAttributedSession pins the hook-side session backfill (E.4): the host session always
// wins; only an attributed row (TaskRef set) with no host session inherits the task's
// creating session; unattributed rows stay empty.
//
// TestAttributedSession 钉死 hook 侧 session 回填（E.4）：宿主 session 恒优先；只有已归属
// （TaskRef 非空）且宿主未传 session 的行才继承任务创建会话；未归属的行保持空。
func TestAttributedSession(t *testing.T) {
	cases := []struct {
		host, task, ref, want string
	}{
		{"s1", "s2", "t", "s1"},
		{"", "s2", "", ""},
		{"", "s2", "t", "s2"},
		{"", "", "t", ""},
	}
	for _, c := range cases {
		if got := attributedSession(c.host, c.ref, c.task); got != c.want {
			t.Errorf("attributedSession(host=%q, ref=%q, task=%q) = %q, want %q", c.host, c.ref, c.task, got, c.want)
		}
	}
}

// TestTaskAttributionStamp pins the shared stamp used by all six hookdispatch record sites:
// existing Meta keys survive the merge, probe keys are added, and an empty SessionID is
// backfilled; an unresolved attribution is a no-op.
//
// TestTaskAttributionStamp 钉死六处 hook 写点共用的 stamp：既有 Meta 键保留、探针键并入、
// 空 SessionID 回填；未解析到任务时 no-op。
func TestTaskAttributionStamp(t *testing.T) {
	e := &checklog.Entry{TaskRef: "feat/x", Meta: map[string]string{"tool": "Bash"}}
	(taskAttribution{}).stamp(e)
	if len(e.Meta) != 1 || e.SessionID != "" {
		t.Fatalf("unresolved attribution must be a no-op, got %+v", e)
	}
	a := taskAttribution{TaskRef: "feat/x", ResolvePath: "workspace", TaskSession: "sess-owner", Sealed: true}
	a.stamp(e)
	if e.Meta["tool"] != "Bash" || e.Meta["resolve_path"] != "workspace" || e.Meta["post_seal"] != "true" {
		t.Fatalf("stamp must merge probe keys into existing Meta, got %v", e.Meta)
	}
	if e.SessionID != "sess-owner" {
		t.Fatalf("empty SessionID must be backfilled from the task session, got %q", e.SessionID)
	}
	e2 := &checklog.Entry{TaskRef: "feat/x", SessionID: "host"}
	a.stamp(e2)
	if e2.SessionID != "host" {
		t.Fatalf("explicit host session must be preserved, got %q", e2.SessionID)
	}
}

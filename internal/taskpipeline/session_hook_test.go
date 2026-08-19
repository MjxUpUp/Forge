package taskpipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// loadScopedSession reads DataDir/sessions/<sid>.json for assertions.
//
// loadScopedSession 读取 DataDir/sessions/<sid>.json 供断言。
func loadScopedSession(t *testing.T, root, sid string) *SessionRecord {
	t.Helper()
	data, err := os.ReadFile(sessionScopedFilePath(root, sid))
	if err != nil {
		return nil
	}
	var s SessionRecord
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal scoped session: %v", err)
	}
	return &s
}

// TestEnsureHookSession_RegistersWithDeclarativeAgent is the core of the 2026-08
// attribution fix: a hook-observed session must be REGISTERED (not just stamped)
// with the declarative --agent as AgentType — not the project-marker guess.
//
// TestEnsureHookSession_RegistersWithDeclarativeAgent 是 2026-08 归因修复的核心：
// hook 观察到的会话必须被登记（而非仅盖戳），AgentType 用声明式 --agent——而非
// 项目标记猜测。
func TestEnsureHookSession_RegistersWithDeclarativeAgent(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	// A .claude marker present — the marker guess would say claude-code; the
	// declarative agent (kimi) must win. This is the exact Forge-repo scenario:
	// kimi sessions misattributed to claude-code because .claude/ exists.
	//
	// .claude 标记存在——标记猜测会得 claude-code；声明式 agent（kimi）必须胜出。
	// 这正是 Forge 仓场景：因 .claude/ 存在，kimi 会话被误归 claude-code。
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}

	EnsureHookSession(root, "session_kimi-1", "kimi")

	s := loadScopedSession(t, root, "session_kimi-1")
	if s == nil {
		t.Fatal("session record was not created")
	}
	if s.AgentType != "kimi" {
		t.Errorf("AgentType = %q, want kimi (declarative beats marker)", s.AgentType)
	}
	// And the jsonl history carries the same agent (LoadSessions reads the jsonl).
	//
	// 且 jsonl 历史携带同一 agent（LoadSessions 读 jsonl）。
	records, err := LoadSessions(root)
	if err != nil || len(records) == 0 {
		t.Fatalf("LoadSessions: %v (n=%d), want the registered session", err, len(records))
	}
	if records[0].AgentType != "kimi" {
		t.Errorf("jsonl AgentType = %q, want kimi", records[0].AgentType)
	}
}

// TestEnsureHookSession_NeverOverwrites pins the fill-empty-only contract on the
// already-registered path: a later event with a different (or empty) agent must
// not clobber a correct attribution.
//
// TestEnsureHookSession_NeverOverwrites 钉住已登记路径的只填空契约：后续带不同
// （或空）agent 的事件不得冲掉正确的归属。
func TestEnsureHookSession_NeverOverwrites(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	EnsureHookSession(root, "s-1", "kimi")
	EnsureHookSession(root, "s-1", "cursor") // must NOT overwrite
	if s := loadScopedSession(t, root, "s-1"); s.AgentType != "kimi" {
		t.Errorf("AgentType = %q after second ensure, want kimi (never overwrite)", s.AgentType)
	}

	// Existing record with EMPTY agent gets filled (stamp semantics preserved).
	//
	// 已存在但 agent 为空的记录会被填充（保留盖戳语义）。
	if err := saveScopedSession(root, &SessionRecord{SessionID: "s-2", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	EnsureHookSession(root, "s-2", "codex")
	if s := loadScopedSession(t, root, "s-2"); s.AgentType != "codex" {
		t.Errorf("empty AgentType was not filled, got %q", s.AgentType)
	}
}

// TestEnsureHookSession_Guards: empty session id is a no-op (the legacy global
// path must stay stamp-only — a hook without a session id must not create or
// rotate legacy state); empty agent falls back to the project marker.
//
// TestEnsureHookSession_Guards：空 session id 是 no-op（legacy 全局路径必须保持
// 只盖戳——无 session id 的 hook 不得创建或轮换 legacy 状态）；空 agent 回落
// 项目标记。
func TestEnsureHookSession_Guards(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	EnsureHookSession(root, "", "kimi")
	if s, err := loadSession(root); err == nil && s != nil {
		t.Errorf("legacy session.json must not be created by a session-id-less hook, got %+v", s)
	}

	if err := os.MkdirAll(filepath.Join(root, ".cursor"), 0755); err != nil {
		t.Fatal(err)
	}
	EnsureHookSession(root, "s-3", "")
	if s := loadScopedSession(t, root, "s-3"); s == nil || s.AgentType != "cursor" {
		t.Errorf("empty agent must fall back to project marker, got %+v", s)
	}
}

// TestLastSessionPointer_FreshnessAndThrottle pins the pointer contract: a fresh
// pointer is adoptable; a stale one is not (mislabel guard); writes within the
// throttle window are skipped.
//
// TestLastSessionPointer_FreshnessAndThrottle 钉住指针契约：新鲜指针可采纳；过期
// 不可（防错标）；节流窗口内的写入被跳过。
func TestLastSessionPointer_FreshnessAndThrottle(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	// Absent → not adoptable.
	if _, _, ok := RecentHookSession(root); ok {
		t.Error("no pointer file: RecentHookSession must be not-ok")
	}

	TouchLastSession(root, "session_kimi-9", "kimi", "PostToolUse")
	sid, agent, ok := RecentHookSession(root)
	if !ok || sid != "session_kimi-9" || agent != "kimi" {
		t.Errorf("fresh pointer: got (%q, %q, %v)", sid, agent, ok)
	}

	// Throttled: an immediate second touch keeps the first write.
	//
	// 节流：紧接着的第二次 touch 保留首次写入。
	TouchLastSession(root, "session_other", "codex", "PreToolUse")
	if sid, _, ok := RecentHookSession(root); !ok || sid != "session_kimi-9" {
		t.Errorf("throttled rewrite must keep the first pointer, got sid=%q ok=%v", sid, ok)
	}

	// Stale pointer → not adoptable.
	//
	// 过期指针 → 不可采纳。
	stale := LastSessionPointer{SessionID: "old", Agent: "kimi", Epoch: time.Now().Add(-time.Hour).Unix()}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(lastSessionPath(root), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := RecentHookSession(root); ok {
		t.Error("stale pointer must not be adopted (would mislabel a human's terminal work)")
	}
}

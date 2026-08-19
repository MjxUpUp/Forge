package hostcap

import "testing"

// TestLookup_CoversAllSupportedHosts pins the registry against the ten
// agentbridge.AgentType names — a host missing here falls back to the
// Claude-compatible default everywhere, silently losing its identity signals.
//
// TestLookup_CoversAllSupportedHosts 把注册表钉在十个 agentbridge.AgentType 名上——
// 缺失的宿主在所有查表处静默回落 Claude 兼容默认，丢失其身份信号。
func TestLookup_CoversAllSupportedHosts(t *testing.T) {
	for _, name := range []string{
		"claude-code", "cursor", "copilot", "windsurf", "codex",
		"opencode", "cline", "kimi", "codebuddy", "reasonix",
	} {
		if h := Lookup(name); h == nil {
			t.Errorf("Lookup(%q) = nil, want registry row", name)
		} else if h.Name != name {
			t.Errorf("Lookup(%q).Name = %q", name, h.Name)
		}
	}
	if h := Lookup("no-such-host"); h != nil {
		t.Errorf("Lookup(unknown) = %+v, want nil", h)
	}
}

// TestLookup_CursorConversationID pins cursor's two-field session identity —
// dropping conversation_id here reopens the "cursor events land on the legacy
// global key" gap.
//
// TestLookup_CursorConversationID 钉住 cursor 的双字段会话身份——此处丢掉
// conversation_id 会重开「cursor 事件落 legacy 全局键」的缺口。
func TestLookup_CursorConversationID(t *testing.T) {
	h := Lookup("cursor")
	if h == nil {
		t.Fatal("cursor row missing")
	}
	found := false
	for _, f := range h.StdinSessionFields {
		if f == "conversation_id" {
			found = true
		}
	}
	if !found {
		t.Errorf("cursor StdinSessionFields = %v, want conversation_id fallback", h.StdinSessionFields)
	}
}

// TestProbeShellIdentity verifies the env probe: claude-code's
// CLAUDE_CODE_SESSION_ID resolves to (claude-code, sid); no host env → empty.
//
// TestProbeShellIdentity 验证 env 探测：claude-code 的 CLAUDE_CODE_SESSION_ID
// 解析为 (claude-code, sid)；无宿主 env → 空。
func TestProbeShellIdentity(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if host, sid := ProbeShellIdentity(); host != "" || sid != "" {
		t.Errorf("no env: got (%q, %q), want empty", host, sid)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-123")
	host, sid := ProbeShellIdentity()
	if host != "claude-code" || sid != "sess-123" {
		t.Errorf("claude env: got (%q, %q), want (claude-code, sess-123)", host, sid)
	}
}

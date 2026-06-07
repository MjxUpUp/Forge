package agentbridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAgents_None(t *testing.T) {
	dir := t.TempDir()
	agents := DetectAgents(dir)
	if len(agents) != 0 {
		t.Fatalf("expected no agents, got %v", agents)
	}
}

func TestDetectAgents_ClaudeCode(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentClaudeCode {
		t.Fatalf("expected [claude-code], got %v", agents)
	}
}

func TestDetectAgents_Cursor(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentCursor {
		t.Fatalf("expected [cursor], got %v", agents)
	}
}

func TestDetectAgents_Copilot(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".github", "instructions"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentCopilot {
		t.Fatalf("expected [copilot], got %v", agents)
	}
}

func TestDetectAgents_Windsurf(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".windsurfrules"), []byte("rules"), 0644)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentWindsurf {
		t.Fatalf("expected [windsurf], got %v", agents)
	}
}

func TestDetectAgents_Multiple(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %v", agents)
	}
	// Order should be deterministic
	if agents[0] != AgentClaudeCode || agents[1] != AgentCursor {
		t.Fatalf("expected [claude-code, cursor], got %v", agents)
	}
}

func TestParseAgentFlag_Auto(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	agents := ParseAgentFlag(dir, "auto")
	if len(agents) != 1 || agents[0] != AgentClaudeCode {
		t.Fatalf("expected [claude-code] from auto-detect, got %v", agents)
	}
}

func TestParseAgentFlag_Explicit(t *testing.T) {
	dir := t.TempDir()
	agents := ParseAgentFlag(dir, "claude-code,cursor")
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %v", agents)
	}
	if agents[0] != AgentClaudeCode || agents[1] != AgentCursor {
		t.Fatalf("expected [claude-code, cursor], got %v", agents)
	}
}

func TestParseAgentFlag_Unknown(t *testing.T) {
	dir := t.TempDir()
	agents := ParseAgentFlag(dir, "unknown-agent")
	if len(agents) != 0 {
		t.Fatalf("expected no agents for unknown name, got %v", agents)
	}
}

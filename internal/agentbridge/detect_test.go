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

func TestDetectAgents_Codex(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".codex"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentCodex {
		t.Fatalf("expected [codex], got %v", agents)
	}
}

// TestParseAgentFlag_CoversAllTranslators 守卫 ParseAgentFlag 的 switch 漏 agent。
// E2E 抓到的真实 Bug：switch 只列了 5 个 agent（claude/cursor/copilot/windsurf/codex），
// 漏 opencode/pi，导致 `forge init --agents opencode` 被静默丢弃——opencode.json 不生成，
// 用户拿不到 forge MCP 工具。单元测试（TestTranslatorsEmitMCP 单独调 Translate）绕过了
// flag 解析，掩盖了此 Bug。本测试从 AllTranslators()（单一真相源）派生全集，确保任何
// 新增 translator 的 AgentType 都自动被 ParseAgentFlag 认识——加 agent 忘加 case 的 drift
// 不再可能。
func TestParseAgentFlag_CoversAllTranslators(t *testing.T) {
	translators := AllTranslators()
	if len(translators) == 0 {
		t.Fatal("AllTranslators returned empty — cannot derive coverage set")
	}
	known := map[AgentType]bool{}
	for _, tr := range translators {
		known[tr.AgentType()] = true
	}
	// 用每个 agent 的名字拼成 flag，ParseAgentFlag 必须原样认回每一个。
	for at := range known {
		got := ParseAgentFlag("/nonexistent-dir-for-auto", string(at))
		found := false
		for _, g := range got {
			if g == at {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ParseAgentFlag(%q) silently dropped %q — switch case missing this agent", at, at)
		}
	}
}

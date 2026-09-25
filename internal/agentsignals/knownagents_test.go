package agentsignals_test

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/agentsignals"
)

// TestKnownAgents_ContainsCanonicalSet verifies the closed set forge task assign validates --assignee against.
//
// TestKnownAgents_ContainsCanonicalSet 验证 forge task assign 校验 --assignee 所用的封闭集。
// 每个 project marker 能解析出的 agent 都必须在此——否则分派给一个有标记的合法 agent 会被
// 误警告为未知（防黑洞守卫对合法目标误开火）。cline 虽有两个标记（.cline + .clinerules）
// 但必须只出现一次。
func TestKnownAgents_ContainsCanonicalSet(t *testing.T) {
	got := agentsignals.KnownAgents()
	if len(got) == 0 {
		t.Fatal("KnownAgents 不应为空（assign 校验依赖此集）")
	}
	// Every marker-bearing agent must be present.
	want := []string{`claude-code`, `cursor`, `copilot`, `windsurf`, `codex`, `opencode`, `cline`, `kimi`, `reasonix`, `zcode`}
	in := map[string]bool{}
	for _, a := range got {
		if in[a] {
			t.Errorf("KnownAgents 出现重复 %q（应去重）", a)
		}
		in[a] = true
	}
	for _, w := range want {
		if !in[w] {
			t.Errorf("KnownAgents 缺少已知 agent %q（got %v）", w, got)
		}
	}
	// cline dedup contract: two markers (.cline + .clinerules) collapse to one entry.
	count := 0
	for _, a := range got {
		if a == `cline` {
			count++
		}
	}
	if count != 1 {
		t.Errorf("cline 应只出现一次（去重），实际 %d 次 (got %v)", count, got)
	}
}

// TestIsKnownAgent pins the single-lookup predicate: true for every canonical agent, false for an absent/typo'd name.
//
// TestIsKnownAgent 钉住单次查找谓词：每个 canonical agent 为 true，缺省/拼错的名字为 false。
// 这是 assign 命令「警告 vs 合法」的分支判定。
func TestIsKnownAgent(t *testing.T) {
	for _, a := range []string{`claude-code`, `kimi`, `reasonix`, `cursor`, `codex`} {
		if !agentsignals.IsKnownAgent(a) {
			t.Errorf("IsKnownAgent(%q) 应为 true（canonical agent）", a)
		}
	}
	for _, a := range []string{``, `codebuddy`, `Kimi`, `claude_code`, `unknown`} {
		if agentsignals.IsKnownAgent(a) {
			t.Errorf("IsKnownAgent(%q) 应为 false（非已知 agent）", a)
		}
	}
}

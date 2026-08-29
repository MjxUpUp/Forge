package agentbridge

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDetectAgents_None(t *testing.T) {
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
	dir := t.TempDir()
	agents := DetectAgents(dir)
	if len(agents) != 0 {
		t.Fatalf("expected no agents, got %v", agents)
	}
}

func TestDetectAgents_ClaudeCode(t *testing.T) {
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentClaudeCode {
		t.Fatalf("expected [claude-code], got %v", agents)
	}
}

func TestDetectAgents_Cursor(t *testing.T) {
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentCursor {
		t.Fatalf("expected [cursor], got %v", agents)
	}
}

func TestDetectAgents_Copilot(t *testing.T) {
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".github", "instructions"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentCopilot {
		t.Fatalf("expected [copilot], got %v", agents)
	}
}

func TestDetectAgents_Windsurf(t *testing.T) {
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".windsurfrules"), []byte("rules"), 0644)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentWindsurf {
		t.Fatalf("expected [windsurf], got %v", agents)
	}
}

func TestDetectAgents_Multiple(t *testing.T) {
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
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
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
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
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".codex"), 0755)

	agents := DetectAgents(dir)
	if len(agents) != 1 || agents[0] != AgentCodex {
		t.Fatalf("expected [codex], got %v", agents)
	}
}

// TestParseAgentFlag_CoversAllTranslators guards ParseAgentFlag's switch against
// missing an agent.
//
// TestParseAgentFlag_CoversAllTranslators 守卫 ParseAgentFlag 的 switch 漏 agent。
// E2E 抓到的真实 Bug：switch 只列了 5 个 agent（claude/cursor/copilot/windsurf/codex），
// 漏 opencode，导致 `forge init --agents opencode` 被静默丢弃——opencode.json 不生成，
// 用户拿不到 forge CLI 质量流程集成。单元测试（单独调 Translate，不经 flag 解析）绕过了
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

// TestDetectAgents_OpencodeXDGConfigHome pins the XDG fix: opencode's global
// config dir resolves via $XDG_CONFIG_HOME/opencode (same as OpenCodeConfigDir's
// write path), so a config home that exists ONLY under XDG_CONFIG_HOME must be
// detected.
//
// TestDetectAgents_OpencodeXDGConfigHome 钉死 XDG 修复：opencode 的全局配置目录
// 按 $XDG_CONFIG_HOME/opencode 解析（与 OpenCodeConfigDir 的写入路径一致），
// 故只存在于 XDG_CONFIG_HOME 下的配置目录必须被检出——只看
// ~/.config/opencode 会漏检。
func TestDetectAgents_OpencodeXDGConfigHome(t *testing.T) {
	home := isolateHome(t)
	// isolateHome points XDG_CONFIG_HOME at <home>/.config; create the opencode
	// dir there (the default ~/.config/opencode path is the same location here,
	// so additionally point XDG at a DIFFERENT dir to prove the env is honored).
	xdg := filepath.Join(home, "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "opencode"), 0755); err != nil {
		t.Fatal(err)
	}

	agents := DetectAgents(t.TempDir())
	found := false
	for _, a := range agents {
		if a == AgentOpencode {
			found = true
		}
	}
	if !found {
		t.Errorf("opencode under XDG_CONFIG_HOME not detected, got %v", agents)
	}
}

// TestDetectAgents_WindsurfUserLevel pins the windsurf user-level detection:
// ~/.codeium (the config root WindsurfTranslator writes into) exists iff
// windsurf is installed, so it must be a detection signal alongside the legacy
// project-level .windsurfrules.
//
// TestDetectAgents_WindsurfUserLevel 钉死 windsurf 用户级检测：~/.codeium
// （WindsurfTranslator 写入的配置根）存在 = windsurf 已安装，必须与遗留的
// 项目级 .windsurfrules 并列为检测信号。
func TestDetectAgents_WindsurfUserLevel(t *testing.T) {
	home := isolateHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".codeium", "windsurf"), 0755); err != nil {
		t.Fatal(err)
	}

	agents := DetectAgents(t.TempDir())
	if len(agents) != 1 || agents[0] != AgentWindsurf {
		t.Fatalf("expected [windsurf] from user-level ~/.codeium, got %v", agents)
	}
}

// TestDetectAgents_ZcodeUserLevel pins the zcode user-level detection: ~/.zcode
// (the config root ZcodeTranslator writes into) exists iff the ZCode desktop app
// has run, so it is a detection signal alongside the project-level .zcode
// marker.
//
// TestDetectAgents_ZcodeUserLevel 钉死 zcode 用户级检测：~/.zcode
// （ZcodeTranslator 写入的配置根）存在 = ZCode 桌面端跑过，与项目级 .zcode
// 标记并列为检测信号。
func TestDetectAgents_ZcodeUserLevel(t *testing.T) {
	home := isolateHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".zcode", "cli"), 0755); err != nil {
		t.Fatal(err)
	}

	agents := DetectAgents(t.TempDir())
	if len(agents) != 1 || agents[0] != AgentZcode {
		t.Fatalf("expected [zcode] from user-level ~/.zcode, got %v", agents)
	}
}

// TestDetectAgents_CodexNotTriggeredByAgentsMd: AGENTS.md must NOT trigger codex
// detection: forge generates AGENTS.md as a universal cross-agent instruction
// source, so treating it as a codex signal makes every `forge init` self-trigger
// codex wiring (.codex/ cascade).
//
// TestDetectAgents_CodexNotTriggeredByAgentsMd：AGENTS.md 不得触发 codex 检测：
// forge 把 AGENTS.md 生成为通用跨 agent 指令源，把它当 codex 信号会让每次
// `forge init` 自触发 codex 接线（.codex/ 级联）。codex 检测只认 .codex/；
// 纯 codex-CLI 用户走 --agents codex。
func TestDetectAgents_CodexNotTriggeredByAgentsMd(t *testing.T) {
	isolateHome(t) // DetectAgents also scans user-level install dirs — keep the real home out
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# project"), 0644)
	if slices.Contains(DetectAgents(dir), AgentCodex) {
		t.Error("should NOT detect with only AGENTS.md (forge generates it universally; codex needs .codex/)")
	}
}

// TestDetectAgents_ProjectMarkersForAllAgents pins that DetectAgents detects the
// project-level markers for the agents whose detection was NOT previously
// covered at this layer (opencode/cline/clinerules/kimi/reasonix).
//
// TestDetectAgents_ProjectMarkersForAllAgents 钉死 DetectAgents 对此前未在本层覆盖的
// agent（opencode/cline/clinerules/kimi/reasonix）项目级标记的检测。这些现经共享
// agentsignals 表路由（DetectAgents → agentsignals.ProjectAgentMarkers），故该委托的回归
// 会静默丢掉它们——正是"53% agent_type 缺失"的失败模式。每条断言恰好一个 agent
// （isolateHome 中和用户级检测，故只算项目标记）。
func TestDetectAgents_ProjectMarkersForAllAgents(t *testing.T) {
	cases := []struct {
		name  string
		setup func(dir string)
		want  AgentType
	}{
		{`opencode`, func(d string) { os.MkdirAll(filepath.Join(d, `.opencode`), 0755) }, AgentOpencode},
		{`cline`, func(d string) { os.MkdirAll(filepath.Join(d, `.cline`), 0755) }, AgentCline},
		{`clinerules`, func(d string) { os.MkdirAll(filepath.Join(d, `.clinerules`), 0755) }, AgentCline},
		{`kimi`, func(d string) { os.MkdirAll(filepath.Join(d, `.kimi-code`), 0755) }, AgentKimi},
		{`reasonix`, func(d string) { os.MkdirAll(filepath.Join(d, `.reasonix`), 0755) }, AgentReasonix},
		{`zcode`, func(d string) { os.MkdirAll(filepath.Join(d, `.zcode`), 0755) }, AgentZcode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateHome(t) // keep the real home out of user-level detection
			dir := t.TempDir()
			tc.setup(dir)
			agents := DetectAgents(dir)
			if len(agents) != 1 || agents[0] != tc.want {
				t.Fatalf("expected [%s], got %v", tc.want, agents)
			}
		})
	}
}

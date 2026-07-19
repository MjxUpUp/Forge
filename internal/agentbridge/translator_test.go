package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
)

func testInput() *TranslationInput {
	return &TranslationInput{
		Protocol:  protocol.DefaultProtocol(),
		HookNames: hooks.HookNames(),
	}
}

// TestCodexWiringMirrorsClaudeSettings guards the hand-maintained sync between
// codex.go (buildCodexHooks) and hooks/settings.go (GenerateSettings). Both
// tables wire the same `forge hook <name>` commands so Forge
// gates enforce identically on Claude Code and Codex — the only two agents
// whose hooks actually block. codex.go's buildCodexHooks comment warns this
// sync is manual; without a guard, adding a hook to one side and forgetting the
// other silently disables a gate on one agent.
//
// 644b142 caused exactly that drift: it deleted tool-track, and this test's
// PRIOR form used a hardcoded `keep` roster that itself drifted and missed the
// deletion (tool-track was absent from the list). This version derives the
// expected set from settings.go's actual generated output — the single source
// of truth — so the guard can no longer be defeated by forgetting to update a
// list alongside the code.
func TestCodexWiringMirrorsClaudeSettings(t *testing.T) {
	// Generate both wirings into temp dirs and parse the hook commands per event.
	claudeDir := t.TempDir()
	if err := hooks.GenerateSettings(claudeDir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	codexDir := t.TempDir()
	if err := (&CodexTranslator{}).Translate(codexDir, testInput()); err != nil {
		t.Fatalf("codex Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	codex := hookCommandsByEvent(t, filepath.Join(codexDir, ".codex", "hooks.json"))

	// Codex models only PreToolUse/PostToolUse/Stop (no SessionStart lifecycle
	// hook). For every event Codex declares, Claude Code must wire the SAME
	// command set — drift in either direction fails. SessionStart/skill-scan is
	// Claude-Code-only by design and is not compared (it has no Codex analogue).
	if len(codex) == 0 {
		t.Fatal("codex wiring has no events — generator or parser broken")
	}
	for event, codexCmds := range codex {
		claudeCmds, ok := claude[event]
		if !ok {
			t.Errorf("Claude Code settings missing event %q that Codex wires", event)
			continue
		}
		if !stringSetEqual(claudeCmds, codexCmds) {
			t.Errorf("hook commands for %q drifted between Claude Code and Codex — keep settings.go GenerateSettings and codex.go buildCodexHooks in sync:\n  claude: %s\n  codex:  %s",
				event, sortedSet(claudeCmds), sortedSet(codexCmds))
		}
	}

	// Regression guard: sunk/deleted hooks must not resurface. settings.go is
	// the source of truth, so checking its output suffices.
	sunk := []string{"read-check", "scope-guard", "clone-check", "experience-check", "security-check", "dependency-check", "test-coverage-check", "session-health"}
	for cmd := range claude["PostToolUse"] {
		for _, s := range sunk {
			if strings.Contains(cmd, "forge hook "+s) {
				t.Errorf("sunk hook %q resurfaced in Claude Code settings: %s", s, cmd)
			}
		}
	}
}

// TestCodexHooksExcludeSessionLifecycle 守卫 gap#2 的跨 host 边界：claude-code 特有的
// SessionStart/PostCompact/UserPromptSubmit lifecycle（含 task-resume 注入 + compact-resume/
// resume-reinject 重注入链）必须被 codex 白名单排除——codex 无 compaction/prompt lifecycle，
// 装上不支持的 event 会静默失效。TestCodexWiringMirrorsClaudeSettings 只查"codex 声明的 event
// 命令集一致"（单子集断言），不查"codex 不该声明某 event"：若误把 PostCompact 加回 codex 白
// 名单且命令集恰好一致，那条测试仍过。本测试补正向+反向断言，把白名单钉死。
func TestCodexHooksExcludeSessionLifecycle(t *testing.T) {
	raw := buildCodexHooks()
	hooksMap, ok := raw[`hooks`].(map[string][]hooks.HookMatcher)
	if !ok {
		t.Fatalf(`codex wiring shape unexpected: %T`, raw[`hooks`])
	}
	for _, banned := range []string{`SessionStart`, `PostCompact`, `UserPromptSubmit`} {
		if _, present := hooksMap[banned]; present {
			t.Errorf(`codex must not wire %s (claude-code-only lifecycle, no codex analogue)`, banned)
		}
	}
	for _, required := range []string{`PreToolUse`, `PostToolUse`, `Stop`} {
		if _, present := hooksMap[required]; !present {
			t.Errorf(`codex must wire %s (block-enforcing gate): missing`, required)
		}
	}
}

// TestCursorWiringMirrorsClaudeSettings guards the sync between cursor.go
// (buildCursorHooks) and hooks/settings.go (GenerateSettings). Cursor's
// hooks.json is flat with camelCase event names (preToolUse/postToolUse/stop),
// but the hook COMMANDS per event must match Claude Code's PascalCase wiring —
// drift silently disables a gate on Cursor. Maps cursor events to Claude
// events and asserts command-set equality. Parallel to TestCodexWiringMirrorsClaudeSettings.
func TestCursorWiringMirrorsClaudeSettings(t *testing.T) {
	claudeDir := t.TempDir()
	if err := hooks.GenerateSettings(claudeDir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	cursorDir := t.TempDir()
	if err := (&CursorTranslator{}).Translate(cursorDir, testInput()); err != nil {
		t.Fatalf("cursor Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	cursor := cursorHookCommandsByEvent(t, filepath.Join(cursorDir, ".cursor", "hooks.json"))

	// Cursor camelCase → Claude PascalCase event mapping.
	eventMap := map[string]string{
		"preToolUse":  "PreToolUse",
		"postToolUse": "PostToolUse",
		"stop":        "Stop",
	}
	if len(cursor) == 0 {
		t.Fatal("cursor wiring has no events — generator or parser broken")
	}
	for cursorEvt, cursorCmds := range cursor {
		claudeEvt, ok := eventMap[cursorEvt]
		if !ok {
			t.Errorf("cursor event %q has no Claude Code mapping — new event not accounted for", cursorEvt)
			continue
		}
		claudeCmds, ok := claude[claudeEvt]
		if !ok {
			t.Errorf("Claude Code settings missing event %q that Cursor wires", claudeEvt)
			continue
		}
		if !stringSetEqual(claudeCmds, cursorCmds) {
			t.Errorf("hook commands for cursor %q / claude %q drifted — keep settings.go GenerateSettings and cursor.go buildCursorHooks in sync:\n  claude: %s\n  cursor: %s",
				cursorEvt, claudeEvt, sortedSet(claudeCmds), sortedSet(cursorCmds))
		}
	}

	// Regression guard: sunk/deleted hooks must not resurface on Cursor either.
	sunk := []string{"read-check", "scope-guard", "clone-check", "experience-check", "security-check", "dependency-check", "test-coverage-check", "session-health"}
	for cmd := range cursor["postToolUse"] {
		for _, s := range sunk {
			if strings.Contains(cmd, "forge hook "+s) {
				t.Errorf("sunk hook %q resurfaced in Cursor hooks: %s", s, cmd)
			}
		}
	}
}

// cursorHookCommandsByEvent parses Cursor's flat hooks.json into event → set of
// command strings. Unlike Claude Code/Codex's nested {matcher,hooks:[{command}]}
// shape, Cursor's format is hooks.<event>[].{command,matcher} — the command sits
// directly on each entry, no inner hooks array.
func cursorHookCommandsByEvent(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	out := make(map[string]map[string]bool)
	for event, entries := range cfg.Hooks {
		set := make(map[string]bool)
		for _, e := range entries {
			if e.Command != "" {
				set[e.Command] = true
			}
		}
		out[event] = set
	}
	return out
}

// hookCommandsByEvent parses a hooks config (settings.local.json or codex
// hooks.json — same schema) into event → set of hook command strings, flattening
// across matchers. Matchers are intentionally ignored: the gate-enforcement
// contract is about WHICH commands run per event, not the matcher regex.
func hookCommandsByEvent(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	out := make(map[string]map[string]bool)
	for event, matchers := range cfg.Hooks {
		set := make(map[string]bool)
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if h.Command != "" {
					set[h.Command] = true
				}
			}
		}
		out[event] = set
	}
	return out
}

func stringSetEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedSet(s map[string]bool) string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	slices.Sort(out)
	return "[" + strings.Join(out, ", ") + "]"
}

func TestCursorTranslator_Translate(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	translator := &CursorTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".cursor", "rules", "forge-quality.mdc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "description: \"Forge quality protocol\"") {
		t.Error("missing MDC frontmatter")
	}
	if !strings.Contains(content, "alwaysApply: true") {
		t.Error("missing alwaysApply")
	}
	if !strings.Contains(content, "质量标准") {
		t.Error("missing quality standards section")
	}
	if !strings.Contains(content, "代码编译") {
		t.Error("missing compile standard")
	}
}

func TestCursorTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&CursorTranslator{}).Detect(dir) {
		t.Error("should not detect without .cursor/")
	}
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)
	if !(&CursorTranslator{}).Detect(dir) {
		t.Error("should detect with .cursor/")
	}
}

func TestCopilotTranslator_Translate(t *testing.T) {
	dir := t.TempDir()

	translator := &CopilotTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".github", "instructions", "forge-quality.instructions.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "applyTo:") {
		t.Error("missing applyTo frontmatter")
	}
	// v0.25 advisory rewrite: compile-gate/no-assertion-weaken dropped from
	// error to warning severity (auto-compile.sh / assertion-check.sh no longer
	// block), so Copilot instructions render [WARNING] not [ERROR]. Guard the
	// advisory severity — [ERROR] would mislead Copilot into treating the
	// advisory hooks as hard blocks.
	if !strings.Contains(content, "[WARNING]") {
		t.Error("missing WARNING severity (v0.25: advisory standards are warning, not error)")
	}
	if !strings.Contains(content, "ALWAYS:") {
		t.Error("missing ALWAYS rules")
	}
}

func TestCopilotTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&CopilotTranslator{}).Detect(dir) {
		t.Error("should not detect without .github/instructions/")
	}
	os.MkdirAll(filepath.Join(dir, ".github", "instructions"), 0755)
	if !(&CopilotTranslator{}).Detect(dir) {
		t.Error("should detect with .github/instructions/")
	}
}

func TestWindsurfTranslator_Translate(t *testing.T) {
	dir := t.TempDir()

	translator := &WindsurfTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".windsurfrules")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, forgeRulesStart) {
		t.Error("missing FORGE:START marker")
	}
	if !strings.Contains(content, forgeRulesEnd) {
		t.Error("missing FORGE:END marker")
	}
	if !strings.Contains(content, "代码编译") {
		t.Error("missing compile standard")
	}
}

// TestWindsurfWiringMirrorsClaudeSettings guards the sync between windsurf.go
// (buildWindsurfHooks) and hooks/settings.go (GenerateSettings). Windsurf uses
// snake_case events (pre_write_code/post_run_command/...) that map N:M onto
// Claude Code's PascalCase PreToolUse/PostToolUse; we flatten each agent's
// wiring to a command set and compare per Claude event. `--agent windsurf` must
// be present on every intercept hook (task-guard/bash-guard/etc.) since
// Windsurf's stdin schema differs from Claude Code's.
func TestWindsurfWiringMirrorsClaudeSettings(t *testing.T) {
	claudeDir := t.TempDir()
	if err := hooks.GenerateSettings(claudeDir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	windsurfDir := t.TempDir()
	if err := (&WindsurfTranslator{}).Translate(windsurfDir, testInput()); err != nil {
		t.Fatalf("windsurf Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	windsurf := windsurfHookCommandsByClaudeEvent(t, filepath.Join(windsurfDir, ".windsurf", "hooks.json"))

	// Every enforcement command must carry --agent windsurf (skill-scan /
	// task-verify are session events with no tool_input, so no agent flag).
	for _, cmds := range windsurf {
		for cmd := range cmds {
			if strings.Contains(cmd, "forge hook task-guard") ||
				strings.Contains(cmd, "forge hook assertion-check") ||
				strings.Contains(cmd, "forge hook bash-guard") ||
				strings.Contains(cmd, "forge hook hazard-guard") ||
				strings.Contains(cmd, "forge hook auto-compile") ||
				strings.Contains(cmd, "forge hook file-sentinel") ||
				strings.Contains(cmd, "forge hook tool-track") {
				if !strings.Contains(cmd, "--agent windsurf") {
					t.Errorf("windsurf hook command missing --agent windsurf (stdin would fail to normalize): %s", cmd)
				}
			}
		}
	}

	for _, claudeEvt := range []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart"} {
		want := claude[claudeEvt]
		got := windsurf[claudeEvt]
		if got == nil {
			got = map[string]bool{}
		}
		// Strip the `--agent windsurf` suffix so the command surfaces match
		// Claude Code's (`forge hook <name>`).
		stripped := map[string]bool{}
		for cmd := range got {
			stripped[strings.TrimSuffix(cmd, " --agent windsurf")] = true
		}
		if !stringSetEqual(want, stripped) {
			t.Errorf("windsurf commands for claude %q drifted:\n  claude: %s\n  windsurf: %s",
				claudeEvt, sortedSet(want), sortedSet(stripped))
		}
	}
}

// windsurfHookCommandsByClaudeEvent parses Windsurf's flat hooks.json and folds
// its snake_case events onto Claude Code's PascalCase events (PreToolUse =
// pre_write_code/pre_read_code/pre_run_command; PostToolUse = post_*; Stop =
// session_end; SessionStart = session_start). Returns claude-event → command set.
func windsurfHookCommandsByClaudeEvent(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	out := make(map[string]map[string]bool)
	for event, entries := range cfg.Hooks {
		claudeEvt := ""
		switch {
		case strings.HasPrefix(event, "pre_"):
			claudeEvt = "PreToolUse"
		case strings.HasPrefix(event, "post_"):
			claudeEvt = "PostToolUse"
		case event == "session_start":
			claudeEvt = "SessionStart"
		case event == "session_end":
			claudeEvt = "Stop"
		default:
			continue
		}
		if out[claudeEvt] == nil {
			out[claudeEvt] = map[string]bool{}
		}
		for _, e := range entries {
			if e.Command != "" {
				out[claudeEvt][e.Command] = true
			}
		}
	}
	return out
}

func TestWindsurfTranslator_PreserveContent(t *testing.T) {
	dir := t.TempDir()
	existing := "# My custom rules\nDo something cool.\n\n"
	os.WriteFile(filepath.Join(dir, ".windsurfrules"), []byte(existing), 0644)

	translator := &WindsurfTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".windsurfrules"))
	content := string(data)
	if !strings.Contains(content, "My custom rules") {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, forgeRulesStart) {
		t.Error("forge section should be appended")
	}
}

func TestWindsurfTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&WindsurfTranslator{}).Detect(dir) {
		t.Error("should not detect without .windsurfrules")
	}
	os.WriteFile(filepath.Join(dir, ".windsurfrules"), []byte("rules"), 0644)
	if !(&WindsurfTranslator{}).Detect(dir) {
		t.Error("should detect with .windsurfrules")
	}
}

func TestBridge_TranslateForAgents(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	errs := TranslateForAgents(dir, []AgentType{AgentCursor}, testInput())
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Verify file was created
	path := filepath.Join(dir, ".cursor", "rules", "forge-quality.mdc")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cursor rules file not created: %v", err)
	}
}

func TestBridge_TranslateForAgents_Empty(t *testing.T) {
	dir := t.TempDir()
	errs := TranslateForAgents(dir, nil, testInput())
	if len(errs) != 0 {
		t.Fatalf("expected no errors for empty agents, got %v", errs)
	}
}

func TestAllTranslators(t *testing.T) {
	translators := AllTranslators()
	if len(translators) != 7 {
		t.Fatalf("expected 7 translators, got %d", len(translators))
	}
	types := make(map[AgentType]bool)
	for _, tr := range translators {
		types[tr.AgentType()] = true
	}
	for _, expected := range []AgentType{AgentClaudeCode, AgentCursor, AgentCopilot, AgentWindsurf, AgentCodex, AgentOpencode, AgentCline} {
		if !types[expected] {
			t.Errorf("missing translator for %s", expected)
		}
	}
}

func TestClineTranslator_Translate(t *testing.T) {
	dir := t.TempDir()
	if err := (&ClineTranslator{}).Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}
	// .clinerules/forge-quality.md — guidance rules (Cline has no hooks)
	rules := readOrFail(t, filepath.Join(dir, ".clinerules", "forge-quality.md"))
	for _, want := range []string{
		"# Forge 质量协议",
		"质量标准",
		"会话行为规则",
		"接入 forge MCP",            // manual MCP wiring section (Cline has no project-level MCP)
		"cline_mcp_settings.json", // points at the global file Cline actually reads
		"Configure MCP Servers",   // Cline panel step
		"AGENTS.md",               // points at cross-agent protocol
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("cline rules missing %q", want)
		}
	}

	// Cline must NOT auto-load project-level MCP: .cline/mcp.json is an invented
	// convention Cline ignores (global only, per docs.cline.bot + feature request
	// cline/cline#2418). Guard that Translate does not write a misleading file
	// that implies forge MCP is wired when Cline will not load it.
	if _, err := os.Stat(filepath.Join(dir, ".cline", "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("Cline must not write .cline/mcp.json (Cline ignores project-level MCP; would mislead users) — err=%v", err)
	}
}

func TestClineTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&ClineTranslator{}).Detect(dir) {
		t.Error("should not detect without .cline/ or .clinerules/")
	}
	os.MkdirAll(filepath.Join(dir, ".cline"), 0755)
	if !(&ClineTranslator{}).Detect(dir) {
		t.Error("should detect with .cline/")
	}
	dir2 := t.TempDir()
	os.MkdirAll(filepath.Join(dir2, ".clinerules"), 0755)
	if !(&ClineTranslator{}).Detect(dir2) {
		t.Error("should detect with .clinerules/")
	}
}

func TestCodexTranslator_Translate(t *testing.T) {
	dir := t.TempDir()

	if err := (&CodexTranslator{}).Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("hooks.json not created: %v", err)
	}

	content := string(data)
	// Codex hooks.json must mirror the Claude Code wiring so Forge gates
	// actually enforce on Codex. All three lifecycle events + the
	// gate-enforcing commands must be present.
	for _, want := range []string{
		`"PreToolUse"`,
		`"PostToolUse"`,
		`"Stop"`,
		`forge hook task-guard`,
		`forge hook auto-compile`,
		`forge hook file-sentinel`,
		`forge hook bash-guard`,
		`forge hook hazard-guard`,
		`forge hook review-stop`,
		`forge hook task-verify`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("codex hooks.json missing %q", want)
		}
	}

	// Regression guard: Forge must never emit a glob-style matcher like
	// Bash(...) — Codex compiles matcher as a regex, so that form is invalid.
	if strings.Contains(content, "Bash(") {
		t.Error("codex hooks.json uses glob-style matcher Bash(...) — invalid as Codex regex")
	}
}

func TestCodexTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&CodexTranslator{}).Detect(dir) {
		t.Error("should not detect without .codex/")
	}
	os.MkdirAll(filepath.Join(dir, ".codex"), 0755)
	if !(&CodexTranslator{}).Detect(dir) {
		t.Error("should detect with .codex/")
	}
	// AGENTS.md must NOT trigger codex detection: forge generates AGENTS.md as a
	// universal cross-agent instruction source, so treating it as a codex signal
	// makes every `forge init` self-trigger codex wiring (.codex/ cascade). Codex
	// detection is .codex/ only; pure codex-CLI users pass --agents codex.
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "AGENTS.md"), []byte("# project"), 0644)
	if (&CodexTranslator{}).Detect(dir2) {
		t.Error("should NOT detect with only AGENTS.md (forge generates it universally; codex needs .codex/)")
	}
}

// TestOpencodePluginWiring verifies the generated .opencode/plugins/forge.ts is
// a REAL, block-capable plugin: it must (1) register the only pre-tool entry
// point opencode offers ("tool.execute.before"), (2) block by throwing (opencode
// has no return-value block API — verified in opencode source), (3) wire the
// same `forge hook <name>` set Claude Code uses so gates enforce identically.
// Drift in any of these silently disables a gate on opencode.
func TestOpencodePluginWiring(t *testing.T) {
	dir := t.TempDir()
	if err := (&OpencodeTranslator{}).Translate(dir, testInput()); err != nil {
		t.Fatalf("opencode Translate: %v", err)
	}
	ts := readOrFail(t, filepath.Join(dir, ".opencode", "plugins", "forge.ts"))

	for _, want := range []string{
		`"tool.execute.before"`,   // the single pre-tool entry point
		`throw new Error`,         // block mechanism (opencode blocks via throw)
		`"forge hook task-guard"`, // PreToolUse write gate
		`"forge hook assertion-check"`,
		`"forge hook bash-guard"`,   // PreToolUse bash gate
		`"forge hook hazard-guard"`, // PreToolUse bash gate (hazardous cmds)
		`"forge hook auto-compile"`, // PostToolUse
		`"forge hook file-sentinel"`,
		`"forge hook tool-track"`,
		// Block is read from forge's JSON decision field, NOT an exit code —
		// cobra surfaces forge's internal errors as exit 1, indistinguishable
		// from a deny. Guard this so no one re-introduces fragile exit-code logic.
		`j?.decision === "block"`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("opencode plugin missing %q", want)
		}
	}
	// Fail-open on forge error — locking the agent out of all tools would be
	// worse than no enforcement. The before-hook returns (doesn't throw) when
	// forge is absent.
	if !strings.Contains(ts, "FAIL OPEN") {
		t.Error("opencode plugin must document fail-open behavior")
	}
	// Field mapping: opencode write uses filePath; must surface as file_path in
	// the Claude-shape stdin the plugin builds.
	if !strings.Contains(ts, "file_path = args.filePath") {
		t.Error("opencode plugin must map args.filePath → file_path")
	}
}

func TestOpencodeTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&OpencodeTranslator{}).Detect(dir) {
		t.Error("should not detect without .opencode/")
	}
	os.MkdirAll(filepath.Join(dir, ".opencode"), 0755)
	if !(&OpencodeTranslator{}).Detect(dir) {
		t.Error("should detect with .opencode/")
	}
}

// TestClaudeCodeTranslatorSkipsGenerateSettingsWhenPluginInstalled: when forge
// plugin is installed at user level, ClaudeCodeTranslator.Translate must NOT
// call GenerateSettings — user-level plugin.json already registers ForgeHookSpec
// machine-wide. Writing project-level hooks is redundant and creates a fragile
// "write then immediately strip" pattern.
func TestClaudeCodeTranslatorSkipsGenerateSettingsWhenPluginInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	// Write plugin fixture: forge@mp, scope=user.
	regDir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(regDir, 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	reg := `{"version":2,"plugins":{"forge@mp":[{"scope":"user"}]}}`
	if err := os.WriteFile(filepath.Join(regDir, "installed_plugins.json"), []byte(reg), 0644); err != nil {
		t.Fatalf("write plugin fixture: %v", err)
	}

	// Pre-populate settings.local.json with user fields only (no hooks).
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	userSettings := `{"env":{"KEY":"val"},"model":"gpt-4"}`
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, []byte(userSettings), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// Run Translate — should skip GenerateSettings because plugin IS installed.
	if err := (&ClaudeCodeTranslator{}).Translate(dir, testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	// Verify: settings.local.json must be untouched (no hooks field added).
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after Translate: %v", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if _, hasHooks := parsed["hooks"]; hasHooks {
		t.Error("plugin installed: Translate must not call GenerateSettings — hooks found in settings.local.json")
	}
	if string(parsed["env"]) != `{"KEY":"val"}` {
		t.Errorf("user env field was modified: got %s", string(parsed["env"]))
	}
}

// (pi tests removed: refactor-data-home 锁定 5 专精再缩到 4，pi 已退出
// 5-专精名单 —— 见 forge-refactor-data-home-progress memory / BREAKING change
// commit break-pi-exit-forge-mgr。)

func readOrFail(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

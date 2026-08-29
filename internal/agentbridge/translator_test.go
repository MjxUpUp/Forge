package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

// isolateHome 把 user-home 派生的配置位置（os.UserHomeDir、XDG_CONFIG_HOME、
// CLAUDE_CONFIG_DIR、CODEX_HOME、DSH_HOME）指向临时目录，user-level translator 与
// DetectAgents 的用户级安装检测绝不触碰真实 home。HOME（unix）与 USERPROFILE
// （windows）都设——os.UserHomeDir 按平台取其一。CLAUDE_CONFIG_DIR/CODEX_HOME
// 指向不存在的子目录（设为空的 env 与未设后回落隔离 home 一样能把检测/解析引离
// 真实 home）；需要真实 codex home 的测试随后再自行设置 CODEX_HOME。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	// DSH_HOME 对 dsh 的用户级安装指示（hostcap 行 Env:"DSH_HOME", Path:"~/.dsh"）
	// 覆盖整个 home。dsh 会向每个子进程环境注入 DSH_HOME（本测试 runner 本身就
	// 由一个 dsh 会话驱动），故若不在此隔离，真实 "~/.dsh" 会以 [dsh] 漏进来——
	// 与上面 CLAUDE_CONFIG_DIR/CODEX_HOME 用"重定向到不存在子目录"同一模式。
	// 否则 TestDetectAgents_* 得到的会是 [x dsh] 而非 [x]，只因本机恰好装了 dsh。
	t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
	return home
}

// TestCodexWiringMirrorsClaudeSettings guards the hand-maintained sync between
// codex.go (buildCodexHooks) and hooks/settings.go (ForgeHookSpec). Both
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
	// Generate both wirings and parse the hook commands per event. Codex registers
	// at user level ($CODEX_HOME/hooks.json) — point CODEX_HOME at a temp dir.
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	claudeDir := t.TempDir()
	writeClaudeSettingsFixture(t, claudeDir)
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("codex Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	codex := hookCommandsByEvent(t, filepath.Join(codexHome, "hooks.json"))

	// Codex's event names are the same PascalCase as Claude Code's (verified
	// against https://developers.openai.com/codex/hooks), so the mapping is
	// identity — for every event Codex declares, Claude Code must wire the SAME
	// command set under the same event name; drift in either direction fails.
	// The codex whitelist (buildCodexHooks) decides WHICH spec events are wired;
	// this test guards that whatever is wired matches Claude exactly. Since
	// Wave 1b every codex command carries the `--agent codex` suffix
	// (output-protocol selection), enforced per-command by the helper.
	assertHostMirrorsClaude(t, "codex", codex, claude, map[string]string{
		"PreToolUse": "PreToolUse", "PostToolUse": "PostToolUse", "Stop": "Stop",
		"SessionStart": "SessionStart", "UserPromptSubmit": "UserPromptSubmit",
		"PostCompact": "PostCompact", "PostToolUseFailure": "PostToolUseFailure",
		"SubagentStop": "SubagentStop",
	})

	// Regression guard: sunk/deleted hooks must not resurface. settings.go is
	// the source of truth, so checking its output suffices.
	assertNoSunkHooks(t, "Claude Code settings", claude["PostToolUse"])
}

// TestCodexHooks_OnlyLegalCodexEvents pins the codex event whitelist against the official roster (https://developers.openai.com/codex/hooks): SessionStart, SessionEnd, PreToolUse, PermissionRequest, PostToolUse, PreCompact, PostCompact, UserPromptSubmit, SubagentStart, SubagentStop, Stop.
//
// TestCodexHooks_OnlyLegalCodexEvents 把 codex event 白名单钉在官方名册上
// （https://developers.openai.com/codex/hooks）：SessionStart、SessionEnd、
// PreToolUse、PermissionRequest、PostToolUse、PreCompact、PostCompact、
// UserPromptSubmit、SubagentStart、SubagentStop、Stop。接名册外的 event 永远
// 不触发（静默 no-op），故生成接线出现名册外 event 即失败。有 codex 对应物的
// 六个 ForgeHookSpec event 必须全部在位（PreToolUse/PostToolUse/Stop 门禁 +
// SessionStart 组 + UserPromptSubmit 重注入 + PostCompact compact-resume）；
// 无 spec 对应物的 codex 侧 event（SessionEnd/PermissionRequest/PreCompact/
// SubagentStart/SubagentStop）必须保持缺席。仿
// TestWindsurfHooks_OnlyLegalCascadeEvents。
func TestCodexHooks_OnlyLegalCodexEvents(t *testing.T) {
	raw := buildCodexHooks()
	hooksMap, ok := raw[`hooks`].(map[string][]hooks.HookMatcher)
	if !ok {
		t.Fatalf(`codex wiring shape unexpected: %T`, raw[`hooks`])
	}
	legal := map[string]bool{
		"SessionStart": true, "SessionEnd": true,
		"PreToolUse": true, "PermissionRequest": true, "PostToolUse": true,
		"PreCompact": true, "PostCompact": true,
		"UserPromptSubmit": true,
		"SubagentStart":    true, "SubagentStop": true,
		"Stop": true,
	}
	// The six ForgeHookSpec events that HAVE a codex analogue must all be present
	// (PreToolUse/PostToolUse/Stop gates + SessionStart group + UserPromptSubmit
	// re-injection + PostCompact compact-resume); the codex-only events without a
	// spec counterpart must stay absent.
	assertOnlyLegalEvents(t, "codex", hooksMap, legal,
		[]string{`PreToolUse`, `PostToolUse`, `Stop`, `SessionStart`, `UserPromptSubmit`, `PostCompact`, `SubagentStop`},
		[]string{`SessionEnd`, `PermissionRequest`, `PreCompact`, `SubagentStart`},
		"no ForgeHookSpec analogue")
}

// TestCursorWiringMirrorsClaudeSettings guards the sync between cursor.go
// (buildCursorHooks) and hooks/settings.go (ForgeHookSpec). Cursor's
// hooks.json is flat with camelCase event names (preToolUse/sessionStart/
// beforeSubmitPrompt/...), but the hook COMMANDS per event must match Claude
// Code's PascalCase wiring — drift silently disables a gate on Cursor. Maps
// cursor events to Claude events and asserts command-set equality. Parallel to
// TestCodexWiringMirrorsClaudeSettings.
func TestCursorWiringMirrorsClaudeSettings(t *testing.T) {
	// Cursor registers at user level (~/.cursor/hooks.json) — isolate the home.
	home := isolateHome(t)
	claudeDir := t.TempDir()
	writeClaudeSettingsFixture(t, claudeDir)
	if err := (&CursorTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("cursor Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	cursor := flatHookCommandsByEvent(t, filepath.Join(home, ".cursor", "hooks.json"))

	// Cursor camelCase → Claude PascalCase event mapping (verified against
	// https://cursor.com/docs/agent/hooks). PostCompact has no Cursor analogue
	// (only the observe-only preCompact) and is intentionally not wired;
	// postToolUseFailure/subagentStop joined 2026-08-22 (#4-A follow-up).
	// Since Wave 1b every cursor command carries the `--agent cursor` suffix
	// (output-protocol selection), enforced per-command by the helper.
	assertHostMirrorsClaude(t, "cursor", cursor, claude, map[string]string{
		"preToolUse":         "PreToolUse",
		"postToolUse":        "PostToolUse",
		"stop":               "Stop",
		"sessionStart":       "SessionStart",
		"beforeSubmitPrompt": "UserPromptSubmit",
		"postToolUseFailure": "PostToolUseFailure",
		"subagentStop":       "SubagentStop",
	})

	// Regression guard: sunk/deleted hooks must not resurface on Cursor either.
	assertNoSunkHooks(t, "Cursor hooks", cursor["postToolUse"])
}

// TestCursorHooks_OnlyLegalCursorEvents pins the cursor event whitelist against the official Cursor Agent roster (https://cursor.com/docs/agent/hooks).
//
// TestCursorHooks_OnlyLegalCursorEvents 把 cursor event 白名单钉在官方 Cursor
// Agent 名册上（https://cursor.com/docs/agent/hooks）。接名册外的 event 永不
// 触发（静默 no-op），故生成接线出现名册外 event 即失败。有 Cursor 对应物的
// 七个 ForgeHookSpec event 必须全部在位（postToolUseFailure/subagentStop 于
// 2026-08-22 加入，#4-A 后续）；PostCompact 必须保持缺席（Cursor 只有
// observe-only 的 preCompact——承载不了 compact-resume 的重注入契约）。仿
// TestWindsurfHooks_OnlyLegalCascadeEvents。
func TestCursorHooks_OnlyLegalCursorEvents(t *testing.T) {
	raw := buildCursorHooks()
	hooksMap, ok := raw[`hooks`].(map[string][]cursorHookEntry)
	if !ok {
		t.Fatalf(`cursor wiring shape unexpected: %T`, raw[`hooks`])
	}
	legal := map[string]bool{
		"sessionStart": true, "sessionEnd": true,
		"preToolUse": true, "postToolUse": true, "postToolUseFailure": true,
		"subagentStart": true, "subagentStop": true,
		"beforeShellExecution": true, "afterShellExecution": true,
		"beforeMCPExecution": true, "afterMCPExecution": true,
		"beforeReadFile": true, "afterFileEdit": true,
		"beforeSubmitPrompt": true, "preCompact": true, "stop": true,
		"afterAgentResponse": true, "afterAgentThought": true,
		"beforeTabFileRead": true, "afterTabFileEdit": true,
	}
	// The seven ForgeHookSpec events with a Cursor analogue must all be present
	// (postToolUseFailure/subagentStop joined 2026-08-22, #4-A follow-up); PostCompact
	// must stay absent (Cursor ships only the observe-only preCompact — it cannot
	// carry compact-resume's re-injection contract).
	assertOnlyLegalEvents(t, "cursor", hooksMap, legal,
		[]string{`preToolUse`, `postToolUse`, `stop`, `sessionStart`, `beforeSubmitPrompt`, `postToolUseFailure`, `subagentStop`},
		[]string{`postCompact`, `PostCompact`},
		"no Cursor analogue — only observe-only preCompact exists")
}

// flatHookCommandsByEvent 把扁平 hooks 配置（{hooks:{event:[{command}]}}——command
// 直接挂在每个条目上、无内层 hooks 数组）解析成 event → 命令字符串集合。由使用
// 该形态的 host 共享：Cursor（~/.cursor/hooks.json）与 reasonix（<reasonix
// home>/settings.json）。
func flatHookCommandsByEvent(t *testing.T, path string) map[string]map[string]bool {
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

// assertHostMirrorsClaude 是每个 *WiringMirrorsClaudeSettings 守卫（codex/cursor/
// windsurf/reasonix/zcode）共享的骨架：host 接的每个 event，映射后的 Claude Code
// event 必须存在且剥掉 ` --agent <host>` 归因后缀后携带同一命令集，且每条 host
// 命令必须带该后缀（host 的 stdin 归一与输出协议选择都依赖它）。eventMap 把
// host 事件名译成 Claude 的 PascalCase 名（原样复用者为恒等映射）；未映射的
// host 事件即失败——新事件必须在此补账。任一方向漂移都失败。
func assertHostMirrorsClaude(t *testing.T, host string, hostWiring, claude map[string]map[string]bool, eventMap map[string]string) {
	t.Helper()
	if len(hostWiring) == 0 {
		t.Fatalf("%s wiring has no events — generator or parser broken", host)
	}
	suffix := " --agent " + host
	for hostEvt, hostCmds := range hostWiring {
		claudeEvt, ok := eventMap[hostEvt]
		if !ok {
			t.Errorf("%s event %q has no Claude Code mapping — new event not accounted for", host, hostEvt)
			continue
		}
		claudeCmds, ok := claude[claudeEvt]
		if !ok {
			t.Errorf("Claude Code settings missing event %q that %s wires", claudeEvt, host)
			continue
		}
		stripped := map[string]bool{}
		for cmd := range hostCmds {
			if !strings.HasSuffix(cmd, suffix) {
				t.Errorf("%s hook command missing %q suffix (stdin normalization and output protocol both need the host): %s", host, suffix, cmd)
			}
			stripped[strings.TrimSuffix(cmd, suffix)] = true
		}
		if !stringSetEqual(claudeCmds, stripped) {
			t.Errorf("hook commands for %s %q / claude %q drifted — keep ForgeHookSpec (settings.go) and the %s hook builder in sync:\n  claude: %s\n  %s: %s",
				host, hostEvt, claudeEvt, host, sortedSet(claudeCmds), host, sortedSet(stripped))
		}
	}
}

// sunkHookNames 列出已删除且任何 host 接线都不得复活的 hook 命令（wiring-mirror
// 测试共享的回归守卫）。
var sunkHookNames = []string{"read-check", "scope-guard", "clone-check", "experience-check", "security-check", "dependency-check", "test-coverage-check", "session-health"}

// assertNoSunkHooks 在 cmds 中出现任何已删除的 forge hook 命令时失败。
// settings.go 是真相源，守它的（或 host 镜像的）PostToolUse 集合即足够。
func assertNoSunkHooks(t *testing.T, where string, cmds map[string]bool) {
	t.Helper()
	for cmd := range cmds {
		for _, s := range sunkHookNames {
			if strings.Contains(cmd, "forge hook "+s) {
				t.Errorf("sunk hook %q resurfaced in %s: %s", s, where, cmd)
			}
		}
	}
}

// assertTranslateIdempotent 用 tr 翻译两次，要求每个列出的路径跨两次运行逐字节
// 一致（输出确定——无重复段、无多余改写）。
func assertTranslateIdempotent(t *testing.T, tr Translator, paths ...string) {
	t.Helper()
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate 1: %v", err)
	}
	first := make([]string, len(paths))
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s (first run): %v", p, err)
		}
		first[i] = string(data)
	}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate 2: %v", err)
	}
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s (second run): %v", p, err)
		}
		if string(data) != first[i] {
			t.Errorf("second Translate changed %s — output must be deterministic (idempotent)", p)
		}
	}
}

// assertOnlyLegalEvents 是每个 *OnlyLegalEvents 守卫共享的骨架：已接线的每个
// event 必须落在该 host 的合法名册内（名册外的 event 永不触发——静默 no-op），
// 每个 required event 必须接线，每个 banned event 必须缺席（bannedWhy 记录封禁
// 缘由，让失败解释契约而非只报症状）。
func assertOnlyLegalEvents[T any](t *testing.T, host string, wiring map[string]T, legal map[string]bool, required, banned []string, bannedWhy string) {
	t.Helper()
	for event := range wiring {
		if !legal[event] {
			t.Errorf("illegal %s hook event %q (not in the official roster — never fires)", host, event)
		}
	}
	for _, req := range required {
		if _, present := wiring[req]; !present {
			t.Errorf("%s must wire %s (has a ForgeHookSpec analogue): missing", host, req)
		}
	}
	for _, b := range banned {
		if _, present := wiring[b]; present {
			t.Errorf("%s must not wire %s (%s)", host, b, bannedWhy)
		}
	}
}

// TestCursorTranslator_Translate verifies the user-level registration: Translate
// writes ~/.cursor/hooks.json (flat, camelCase events, gate-enforcing commands) and
// no longer writes any project-level file (the .cursor/rules/forge-quality.mdc
// guidance moved to the skillgen layer; project-level residue is cleaned elsewhere).
func TestCursorTranslator_Translate(t *testing.T) {
	home := isolateHome(t)
	dir := t.TempDir()

	translator := &CursorTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".cursor", "hooks.json"))
	if err != nil {
		t.Fatalf("user-level hooks.json not created: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`"version": 1`,
		`"preToolUse"`,
		`"postToolUse"`,
		`"stop"`,
		`forge hook task-guard`,
		`forge hook bash-guard`,
		`forge hook review-stop`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("cursor user-level hooks.json missing %q", want)
		}
	}

	// No project-level writes: the user-level translator must leave the project
	// directory untouched.
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("cursor Translate must not write into the project dir (entries=%v, err=%v)", entries, err)
	}
}

// TestCopilotTranslator_Translate pins the user-level-assets contract: Copilot has
// no lifecycle hooks and no writable user-level instruction channel, so the
// translator is a deliberate no-op — it must succeed and write NOTHING into the
// project directory (zero-project-write default; legacy project files are stripped
// by the cleanup layer).
func TestCopilotTranslator_Translate(t *testing.T) {
	dir := t.TempDir()

	translator := &CopilotTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("copilot Translate must be a no-op (entries=%v, err=%v)", entries, err)
	}
}

func TestWindsurfTranslator_Translate(t *testing.T) {
	isolateHome(t) // Translate writes the user-level hooks.json + global_rules.md
	dir := t.TempDir()

	translator := &WindsurfTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	rulesPath, err := WindsurfGlobalRulesPath()
	if err != nil {
		t.Fatalf("WindsurfGlobalRulesPath: %v", err)
	}
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("user-level global_rules.md not created: %v", err)
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
// (buildWindsurfHooks) and hooks/settings.go (ForgeHookSpec). Windsurf uses
// snake_case events (pre_write_code/post_run_command/...) that map N:M onto
// Claude Code's PascalCase PreToolUse/PostToolUse; we flatten each agent's
// wiring to a command set and compare per Claude event. `--agent windsurf` must
// be present on every intercept hook (task-guard/bash-guard/etc.) since
// Windsurf's stdin schema differs from Claude Code's.
func TestWindsurfWiringMirrorsClaudeSettings(t *testing.T) {
	// Windsurf registers at user level (~/.codeium/windsurf/hooks.json) — isolate the home.
	home := isolateHome(t)
	claudeDir := t.TempDir()
	writeClaudeSettingsFixture(t, claudeDir)
	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("windsurf Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	windsurf := windsurfHookCommandsByClaudeEvent(t, filepath.Join(home, ".codeium", "windsurf", "hooks.json"))

	// Every forge command must carry --agent windsurf. Before Wave 2b the two
	// session-lifecycle groups (pre_user_prompt / post_cascade_response) were
	// missing the flag — those hooks then emitted Claude-protocol stdout on a host
	// with no stdout channel. Tightened to ALL forge commands (stdin normalization
	// AND output protocol both need the host); the helper enforces the suffix on
	// every command of every mapped event, which is all of them here.
	assertHostMirrorsClaude(t, "windsurf", windsurf, claude, map[string]string{
		"PreToolUse": "PreToolUse", "PostToolUse": "PostToolUse",
		"Stop": "Stop", "SessionStart": "SessionStart",
	})
	// 在场钉住：折叠后的 map 必须带全四个 Claude 侧组——helper 只访问 host 已接
	// 的事件，静默丢掉的组需要这条显式检查（旧固定方向循环靠空集比对抓它）。
	for _, evt := range []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart"} {
		if len(windsurf[evt]) == 0 {
			t.Errorf("windsurf must wire the claude %s group (folded map came back empty)", evt)
		}
	}

	// Negative pin: UserPromptSubmit and PostCompact groups must NOT exist on Windsurf.
	// UserPromptSubmit (resume-reinject/skill-trigger) is unwired because Windsurf has
	// no PostCompact event → compact-resume never sets the reinject flag; PostCompact
	// doesn't exist in Cascade's hook registry. This is a deliberate gap, not a drift.
	// Pin on HOOK COMMAND names (resume-reinject / compact-resume), not event names — the
	// command text never contains the event name, so a "PostCompact" literal would be a
	// vacuously-true assertion forever (2026-08-16 review finding).
	// If Windsurf adds these events in the future, this test forces explicit wiring changes.
	for _, cmds := range windsurf {
		for cmd := range cmds {
			stripped := strings.TrimSuffix(cmd, " --agent windsurf")
			if strings.Contains(stripped, "resume-reinject") || strings.Contains(stripped, "compact-resume") {
				t.Errorf("windsurf must NOT have UserPromptSubmit (resume-reinject) or PostCompact (compact-resume) hooks (deliberate gap, not drift): %s", cmd)
			}
		}
	}
}

// windsurfHookCommandsByClaudeEvent parses Windsurf's flat hooks.json and folds
// its snake_case events onto Claude Code's PascalCase events (PreToolUse =
// pre_write_code/pre_read_code/pre_run_command; PostToolUse = post_*; Stop =
// post_cascade_response; SessionStart = pre_user_prompt — Cascade has no
// session_start/session_end, so those groups hang on the prompt/response
// lifecycle events Cascade actually emits). Returns claude-event → command set.
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
		case event == "pre_user_prompt":
			claudeEvt = "SessionStart"
		case event == "post_cascade_response" || event == "post_cascade_response_with_transcript":
			claudeEvt = "Stop"
		case strings.HasPrefix(event, "pre_"):
			claudeEvt = "PreToolUse"
		case strings.HasPrefix(event, "post_"):
			claudeEvt = "PostToolUse"
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
	isolateHome(t) // Translate writes the user-level hooks.json + global_rules.md
	dir := t.TempDir()
	rulesPath, err := WindsurfGlobalRulesPath()
	if err != nil {
		t.Fatalf("WindsurfGlobalRulesPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0755); err != nil {
		t.Fatal(err)
	}
	existing := "# My custom rules\nDo something cool.\n\n"
	os.WriteFile(rulesPath, []byte(existing), 0644)

	translator := &WindsurfTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(rulesPath)
	content := string(data)
	if !strings.Contains(content, "My custom rules") {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, forgeRulesStart) {
		t.Error("forge section should be appended")
	}
}

// TestWindsurfTranslator_ReadErrorNoOverwrite pins the data-loss guard: when
// reading the user-level global_rules.md fails with anything OTHER than NotExist
// (permissions, IO — here simulated by making the path a directory, which
// os.ReadFile rejects), Translate must return the error instead of falling through
// to the whole-file overwrite, which would silently destroy the user's existing
// rules. Same contract as kimi.go's config.toml handling.
func TestWindsurfTranslator_ReadErrorNoOverwrite(t *testing.T) {
	isolateHome(t) // Translate writes the user-level hooks.json + global_rules.md
	dir := t.TempDir()
	rulesPath, err := WindsurfGlobalRulesPath()
	if err != nil {
		t.Fatalf("WindsurfGlobalRulesPath: %v", err)
	}
	// global_rules.md as a DIRECTORY → os.ReadFile returns a non-NotExist error.
	if err := os.MkdirAll(rulesPath, 0755); err != nil {
		t.Fatal(err)
	}
	err = (&WindsurfTranslator{}).Translate(dir, testInput())
	if err == nil {
		t.Fatal("Translate must fail on a non-NotExist read error (silent whole-file overwrite would lose user rules)")
	}
	if !strings.Contains(err.Error(), "windsurf") {
		t.Errorf("error must name the windsurf path, got: %v", err)
	}
}

func TestBridge_TranslateForAgents(t *testing.T) {
	home := isolateHome(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	errs := TranslateForAgents(dir, []AgentType{AgentCursor}, testInput())
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Cursor registers at user level (~/.cursor/hooks.json), not in the project.
	path := filepath.Join(home, ".cursor", "hooks.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cursor user-level hooks.json not created: %v", err)
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
	if len(translators) != 12 {
		t.Fatalf("expected 12 translators, got %d", len(translators))
	}
	types := make(map[AgentType]bool)
	for _, tr := range translators {
		types[tr.AgentType()] = true
	}
	for _, expected := range []AgentType{AgentClaudeCode, AgentCursor, AgentCopilot, AgentWindsurf, AgentCodex, AgentOpencode, AgentCline, AgentKimi, AgentCodeBuddy, AgentReasonix, AgentDsh, AgentZcode} {
		if !types[expected] {
			t.Errorf("missing translator for %s", expected)
		}
	}
}

// TestClineTranslator_Translate pins the zero-project-write half of the cline contract: since v3.36 cline HAS lifecycle hooks and the translator wires real wrapper scripts (see cline_test.go) — but only at the USER level (~/Documents/Cline/Rules/Hooks/); the project directory must stay untouched (zero-project-write default since v1.22; legacy project files are stripped by the cleanup layer).
//
// TestClineTranslator_Translate 钉死 cline 契约的零项目写入半边：v3.36 起 cline 有
// lifecycle hooks、translator 接线真正的 wrapper 脚本（见 cline_test.go）——但只在
// 用户级（~/Documents/Cline/Rules/Hooks/）；项目目录必须不被触碰（v1.22 起的零项目
// 写入默认；遗留项目文件由清理层剥除）。
func TestClineTranslator_Translate(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	if err := (&ClineTranslator{}).Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("cline Translate must not write into the project dir (entries=%v, err=%v)", entries, err)
	}
}

func TestCodexTranslator_Translate(t *testing.T) {
	// Codex registers at user level ($CODEX_HOME/hooks.json) — point CODEX_HOME at
	// a temp dir and confirm Translate writes there, not into the project.
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	dir := t.TempDir()

	if err := (&CodexTranslator{}).Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(codexHome, "hooks.json"))
	if err != nil {
		t.Fatalf("user-level hooks.json not created: %v", err)
	}

	// No project-level writes: the user-level translator must leave the project
	// directory untouched.
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("codex Translate must not write into the project dir (entries=%v, err=%v)", entries, err)
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

// TestOpencodePluginWiring verifies the generated forge.ts (installed at the
// user-level global plugin dir) is a REAL, block-capable plugin: it must (1) register the only pre-tool entry
// point opencode offers ("tool.execute.before"), (2) block by throwing (opencode
// has no return-value block API — verified in opencode source), (3) wire the
// same `forge hook <name>` set Claude Code uses so gates enforce identically.
//
// The roster assertion is DERIVED from hooks.ForgeHookSpec (the single source
// of truth), not a hardcoded list — a hardcoded `want` list drifts alongside
// the code it guards (644b142 lesson, see TestCodexWiringMirrorsClaudeSettings).
// Expected commands are computed per Claude tool from the spec's PreToolUse /
// PostToolUse matchers, minus the explicit opencodeExemptions below, and
// compared for SET EQUALITY against the `forge hook <name>` call sites
// extracted from the generated TS rosters.
func TestOpencodePluginWiring(t *testing.T) {
	// Opencode registers at user level ($XDG_CONFIG_HOME/opencode/plugins/forge.ts)
	// — isolate the home so Translate never touches the real config dir.
	home := isolateHome(t)
	if err := (&OpencodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("opencode Translate: %v", err)
	}
	ts := readOrFail(t, filepath.Join(home, ".config", "opencode", "plugins", "forge.ts"))

	for _, want := range []string{
		`"tool.execute.before"`, // the single pre-tool entry point
		`throw new Error`,       // block mechanism (opencode blocks via throw)
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

	// Parity: spec-derived expected roster vs the roster extracted from the TS.
	pre := opencodeTSRoster(t, ts, "PRE_HOOKS")
	post := opencodeTSRoster(t, ts, "POST_HOOKS")
	assertOpencodeRosterParity(t, "PreToolUse", pre)
	assertOpencodeRosterParity(t, "PostToolUse", post)
}

// opencodeExemptions lists spec hook commands intentionally NOT wired into the
// opencode TS plugin, with the reason. Anything not listed here must appear in
// the generated PRE_HOOKS/POST_HOOKS rosters — adding a new exemption requires
// justifying it here, which is exactly the review tripwire a hardcoded list
// never provides.
var opencodeExemptions = map[string]string{
	"skill-trigger": "skill 触发提示 hook（建议/记录型，非门禁）；opencode 插件只接门禁与 PostToolUse 记录 hook，不为非门禁 hook 增加每次工具调用的 fork 成本",
}

// opencodeToolUniverse is the set of Claude tool names the opencode plugin can
// route (values of CLAUDE_TOOL in the generated TS). Spec matchers that name
// tools outside this universe (Skill/Agent/Grep/Glob in the PostToolUse
// tool-track matcher) have no opencode analogue and are excluded from parity.
var opencodeToolUniverse = map[string]bool{
	"Write": true, "Edit": true, "Bash": true, "Read": true,
}

// opencodeTSRoster extracts a `const <NAME>: Record<string, string[]>` block
// from the generated TS into tool → set of `forge hook <name>` commands.
func opencodeTSRoster(t *testing.T, ts, constName string) map[string]map[string]bool {
	t.Helper()
	start := strings.Index(ts, "const "+constName)
	if start < 0 {
		t.Fatalf("generated TS missing const %s", constName)
	}
	rest := ts[start:]
	end := strings.Index(rest, "};")
	if end < 0 {
		t.Fatalf("generated TS const %s block unterminated", constName)
	}
	block := rest[:end]
	lineRe := regexp.MustCompile(`(\w+):\s*\[([^\]]*)\]`)
	cmdRe := regexp.MustCompile(`forge hook [a-z0-9-]+`)
	out := map[string]map[string]bool{}
	for _, m := range lineRe.FindAllStringSubmatch(block, -1) {
		set := map[string]bool{}
		for _, c := range cmdRe.FindAllString(m[2], -1) {
			set[c] = true
		}
		out[m[1]] = set
	}
	if len(out) == 0 {
		t.Fatalf("generated TS const %s parsed to an empty roster (extractor broken?)", constName)
	}
	return out
}

// assertOpencodeRosterParity derives the expected tool → command set for one
// Claude event from hooks.ForgeHookSpec and asserts set equality with the
// roster extracted from the generated TS, per tool and in both directions.
func assertOpencodeRosterParity(t *testing.T, event string, actual map[string]map[string]bool) {
	t.Helper()
	expected := map[string]map[string]bool{}
	for _, m := range hooks.ForgeHookSpec()[event] {
		for _, tool := range strings.Split(m.Matcher, "|") {
			if !opencodeToolUniverse[tool] {
				continue // Skill/Agent: no opencode analogue (opencodeToolUniverse comment)
			}
			if expected[tool] == nil {
				expected[tool] = map[string]bool{}
			}
			for _, h := range m.Hooks {
				name := strings.TrimPrefix(h.Command, "forge hook ")
				if _, exempt := opencodeExemptions[name]; exempt {
					continue
				}
				expected[tool][h.Command] = true
			}
		}
	}
	for tool, want := range expected {
		got := actual[tool]
		if got == nil {
			got = map[string]bool{}
		}
		if !stringSetEqual(want, got) {
			t.Errorf("opencode %s roster for tool %s drifted from ForgeHookSpec:\n  spec-derived: %s\n  opencode TS:  %s",
				event, tool, sortedSet(want), sortedSet(got))
		}
	}
	for tool := range actual {
		if _, ok := expected[tool]; !ok {
			t.Errorf("opencode %s roster wires unexpected tool %s (not derivable from ForgeHookSpec): %s",
				event, tool, sortedSet(actual[tool]))
		}
	}
}

// TestClaudeCodeTranslatorSkipsGenerateSettingsWhenPluginInstalled: when forge
// plugin is installed at user level, ClaudeCodeTranslator.Translate must NOT
// write project-level hooks — user-level plugin.json already registers ForgeHookSpec
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

	// Run Translate — should skip the project-level hooks write because plugin IS installed.
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
		t.Error("plugin installed: Translate must not write project-level hooks — hooks found in settings.local.json")
	}
	if string(parsed["env"]) != `{"KEY":"val"}` {
		t.Errorf("user env field was modified: got %s", string(parsed["env"]))
	}
}

// (pi tests removed: refactor-data-home locked 5 specializations then narrowed to 4, pi has exited
// the 5-specialization list — see forge-refactor-data-home-progress memory / BREAKING change
// commit break-pi-exit-forge-mgr.)
//
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

// writeClaudeSettingsFixture writes a project-level .claude/settings.local.json whose
// hooks section is exactly hooks.ForgeHookSpec — the test-side stand-in for the removed
// hooks.GenerateSettings writer. Host parity tests read the file as the Claude Code
// wiring ground truth.
//
// writeClaudeSettingsFixture 写项目级 .claude/settings.local.json，hooks 段恰为
// hooks.ForgeHookSpec——已删除的 hooks.GenerateSettings writer 的测试侧替身。
// host parity 测试把它读作 Claude Code 接线基准。
func writeClaudeSettingsFixture(t *testing.T, dir string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"hooks": hooks.ForgeHookSpec()})
	if err != nil {
		t.Fatalf("marshal ForgeHookSpec: %v", err)
	}
	path := filepath.Join(dir, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		t.Fatalf("write settings.local.json: %v", err)
	}
}

// TestConventionsHooks_MirrorWiringPinned pins the wiring DECISIONS for the two conventions-profile injection hooks in the hand-maintained mirrors.
//
// TestConventionsHooks_MirrorWiringPinned 钉住 conventions-profile 两个注入 hook
// 在手工维护镜像里的**接线决策**（超出通用 spec-parity 守卫的范围）：opencode 的
// PRE_HOOKS 带 conventions-write 且刻意不进 opencodeExemptions（它是 opencode
// 唯一能承载的 conventions 投递层——其 SessionStart 组在该宿主永不触发）；
// windsurf 把 conventions-write 映射到 pre_write_code、conventions-context 映射到
// pre_user_prompt（SessionStart 组的 Cascade 挂点），两者同乘「诚实记录、绝不
// 静默投递」契约。
func TestConventionsHooks_MirrorWiringPinned(t *testing.T) {
	ts := buildOpencodePlugin()
	if !strings.Contains(ts, "conventions-write") {
		t.Error("opencode PRE_HOOKS must carry conventions-write (the only conventions layer opencode can deliver — its SessionStart group never fires there)")
	}
	if _, exempted := opencodeExemptions["conventions-write"]; exempted {
		t.Error("conventions-write must NOT be an opencode exemption: exempting it deletes the entire conventions layer on opencode")
	}

	raw := buildWindsurfHooks()
	entries, _ := raw["hooks"].(map[string][]windsurfHookEntry)
	has := func(event, hookName string) bool {
		for _, e := range entries[event] {
			if strings.Contains(e.Command, "forge hook "+hookName) {
				return true
			}
		}
		return false
	}
	if !has("pre_write_code", "conventions-write") {
		t.Error("windsurf pre_write_code must carry conventions-write")
	}
	if !has("pre_user_prompt", "conventions-context") {
		t.Error("windsurf pre_user_prompt must carry conventions-context (the SessionStart group's Cascade mount)")
	}
}

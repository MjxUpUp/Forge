package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// TestReasonixTranslator_Translate: a reasonix home that exists (reasonix installed) gets the
// user-level forge-quality skill written under <home>/skills/forge-quality/SKILL.md —
// reasonix's native skill mechanism. Content carries the shared conditional-activation wording
// (visible in every project, effective only in forge-registered ones) and drops the project-info
// section. (The enforcement hooks settings.json is covered by TranslateWritesHooks below.)
//
// TestReasonixTranslator_Translate：reasonix home 存在（reasonix 已装）时，用户级
// forge-quality skill 写到 <home>/skills/forge-quality/SKILL.md——reasonix 的原生 skill 机制。
// 内容带共享条件激活措辞（对所有项目可见，仅在 forge 注册项目中生效）且移除项目信息章节。
// （enforcement hooks settings.json 由下文 TranslateWritesHooks 覆盖。）
func TestReasonixTranslator_Translate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate BackupOriginal (writes under forgedata.GlobalHome)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir reasonix home: %v", err)
	}

	if err := (&ReasonixTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	path := filepath.Join(home, "skills", "forge-quality", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v (want SKILL.md under reasonix skills dir)", err)
	}
	content := string(data)
	for _, want := range []string{
		"name: forge-quality",
		"## 质量标准",
		"## Task Bridge Protocol",
		"仅当当前项目已执行过 `forge init`", // conditional activation, not unconditional "本项目"
	} {
		if !strings.Contains(content, want) {
			t.Errorf("skill content missing %q", want)
		}
	}
	if strings.Contains(content, "## 当前项目信息") {
		t.Errorf("user-level skill must drop the project-info section")
	}
}

// TestReasonixTranslator_TranslateWritesHooks: Translate also writes the enforcement hooks into
// <home>/settings.json (flat schema derived from ForgeHookSpec). Pins both an enforcement event
// (PreToolUse → task-guard) and a session event (SessionStart → skill-scan) so the
// reasonixEventName whitelist is exercised across event kinds. Mirrors
// TestGenerateUserSettings_CreatesFile.
//
// TestReasonixTranslator_TranslateWritesHooks：Translate 同时把 enforcement hooks 写进
// <home>/settings.json（由 ForgeHookSpec 派生的扁平 schema）。同时钉一个强制 event
// （PreToolUse → task-guard）和一个会话 event（SessionStart → skill-scan），以跨 event 种类
// 覆盖 reasonixEventName 白名单。镜像 TestGenerateUserSettings_CreatesFile。
func TestReasonixTranslator_TranslateWritesHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate BackupOriginal (writes under forgedata.GlobalHome)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir reasonix home: %v", err)
	}

	if err := (&ReasonixTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		`"hooks"`,
		`forge hook task-guard`, // PreToolUse enforcement
		`forge hook skill-scan`, // SessionStart
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings.json missing %q", want)
		}
	}
	// reasonix's flat entry shape: match (not matcher) + bare command (no type wrapper).
	if strings.Contains(body, `"matcher"`) {
		t.Errorf("settings.json must use reasonix's flat `match` field, not CC's `matcher`: %s", body)
	}
}

// TestReasonixTranslator_HooksMergePreservesUserContent: a pre-existing settings.json with user
// top-level keys and user hook entries (incl. an unknown field) is preserved verbatim; forge
// hooks are added exactly once. Mirrors TestGenerateUserSettings_MergePreservesUserHooks — the
// raw-JSON merge contract that prevents "merge eats user config".
//
// TestReasonixTranslator_HooksMergePreservesUserContent：既有 settings.json 的用户顶层键与
// 用户 hook 条目（含未知字段）原样保留；forge hooks 恰好追加一次。镜像
// TestGenerateUserSettings_MergePreservesUserHooks——防"merge 吃掉用户配置"的 raw-JSON 合并契约。
func TestReasonixTranslator_HooksMergePreservesUserContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate BackupOriginal (writes under forgedata.GlobalHome)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir reasonix home: %v", err)
	}
	// Seed: a user top-level key + a user hook entry carrying an unknown field (timeout), which a
	// typed round-trip would silently drop.
	seed := `{
		"myTheme": "dark",
		"hooks": {
			"PreToolUse": [
				{ "match": "Bash", "command": "echo user-hook", "timeout": 99 }
			]
		}
	}`
	settingsPath := filepath.Join(home, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(seed), 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := (&ReasonixTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	body := string(data)
	// User top-level key preserved.
	if !strings.Contains(body, `"myTheme"`) || !strings.Contains(body, `"dark"`) {
		t.Errorf("user top-level key not preserved: %s", body)
	}
	// User hook + its unknown field preserved byte-for-byte.
	if !strings.Contains(body, `echo user-hook`) || !strings.Contains(body, `"timeout": 99`) {
		t.Errorf("user hook / unknown field not preserved verbatim: %s", body)
	}
	// Forge hooks appended exactly once (strip-then-append → no duplicates).
	if got := strings.Count(body, `forge hook task-guard`); got != 1 {
		t.Errorf("forge hook task-guard appears %d times, want 1 (idempotent merge): %s", got, body)
	}
}

// TestReasonixTranslator_NoSelfPoison: a missing reasonix home (reasonix not installed) is a clean
// no-op — Forge must not create the agent's config home itself, or DetectAgents' project-
// independent signal would flip and re-wire on every init. Both the skill AND settings.json writes
// must be skipped.
//
// TestReasonixTranslator_NoSelfPoison：reasonix home 缺失（未安装）时干净 no-op——Forge 绝不
// 自行创建 agent 的配置 home，否则会让接线信号翻转、每次 init 都重新接线。skill 与
// settings.json 两处写入都必须跳过。
func TestReasonixTranslator_NoSelfPoison(t *testing.T) {
	home := filepath.Join(t.TempDir(), "no-such-reasonix-home")
	t.Setenv("REASONIX_HOME", home)

	if err := (&ReasonixTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("reasonix home must not be created when missing (self-poison guard), stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("settings.json must not be created when home is missing, stat err = %v", err)
	}
}

// TestReasonixTranslator_Idempotent: translating twice produces byte-identical skill AND
// settings.json content (no drift, no duplicate sections).
//
// TestReasonixTranslator_Idempotent：翻译两次产出逐字节一致的 skill 与 settings.json 内容
// （无漂移、无重复段）。
func TestReasonixTranslator_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate BackupOriginal (writes under forgedata.GlobalHome)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir reasonix home: %v", err)
	}

	tr := &ReasonixTranslator{}
	dir := t.TempDir()
	if err := tr.Translate(dir, testInput()); err != nil {
		t.Fatalf("Translate 1: %v", err)
	}
	firstSkill, err := os.ReadFile(filepath.Join(home, "skills", "forge-quality", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill 1: %v", err)
	}
	firstSettings, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("read settings 1: %v", err)
	}
	if err := tr.Translate(dir, testInput()); err != nil {
		t.Fatalf("Translate 2: %v", err)
	}
	secondSkill, err := os.ReadFile(filepath.Join(home, "skills", "forge-quality", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill 2: %v", err)
	}
	secondSettings, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("read settings 2: %v", err)
	}
	if string(firstSkill) != string(secondSkill) {
		t.Errorf("idempotency broken: second Translate changed the skill content")
	}
	if string(firstSettings) != string(secondSettings) {
		t.Errorf("idempotency broken: second Translate changed settings.json content")
	}
}

// TestReasonixTranslator_Registered guards AllTranslators membership — `forge init --agents
// reasonix` resolves through translatorMap, so an unregistered translator silently wires nothing.
//
// TestReasonixTranslator_Registered 守卫 AllTranslators 成员资格——`forge init --agents
// reasonix` 经 translatorMap 解析，未注册的 translator 会静默不接线。
func TestReasonixTranslator_Registered(t *testing.T) {
	for _, tr := range AllTranslators() {
		if tr.AgentType() == AgentReasonix {
			return
		}
	}
	t.Fatal("ReasonixTranslator not registered in AllTranslators")
}

// TestReasonixDetect: a project dir carrying .reasonix/ (the agent ran there at least once) is
// auto-detected; a clean dir is not. User-level home is deliberately NOT a signal (kimi
// philosophy) — covered implicitly by TestDetectAgents_None with the isolated home.
//
// TestReasonixDetect：项目目录带 .reasonix/（agent 至少在此跑过一次）会被 auto 检测；干净
// 目录不会。用户级 home 刻意不作为信号（kimi 哲学）——由隔离 home 下的
// TestDetectAgents_None 隐式覆盖。
func TestReasonixDetect(t *testing.T) {
	isolateHome(t) // keep the real home out of DetectAgents' user-level scan

	dir := t.TempDir()
	agents := DetectAgents(dir)
	for _, a := range agents {
		if a == AgentReasonix {
			t.Fatalf("clean project dir must not auto-detect reasonix, got %v", agents)
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, ".reasonix"), 0755); err != nil {
		t.Fatalf("mkdir .reasonix: %v", err)
	}
	agents = DetectAgents(dir)
	found := false
	for _, a := range agents {
		if a == AgentReasonix {
			found = true
		}
	}
	if !found {
		t.Fatalf("project dir with .reasonix/ must auto-detect reasonix, got %v", agents)
	}
}

// TestReasonixConfigHome_EnvAndDefault pins the home resolution: REASONIX_HOME wins when set;
// otherwise the OS user-config dir + "reasonix" (reasonix's real read path — %APPDATA%\reasonix
// on Windows, ~/.config/reasonix on Linux). The expected default is derived from the same
// os.UserConfigDir the function uses, so the assertion holds on every platform.
//
// TestReasonixConfigHome_EnvAndDefault 钉住 home 解析：设了 REASONIX_HOME 用它；否则 OS 用户
// 配置目录 + "reasonix"（reasonix 的真实读路径——Windows %APPDATA%\reasonix、Linux
// ~/.config/reasonix）。期望默认值由函数所用的同一 os.UserConfigDir 派生，故断言在所有平台成立。
func TestReasonixConfigHome_EnvAndDefault(t *testing.T) {
	t.Setenv("REASONIX_HOME", filepath.Join("C:", "fake", "reasonix"))
	got, err := ReasonixConfigHome()
	if err != nil {
		t.Fatalf("ReasonixConfigHome: %v", err)
	}
	if got != filepath.Join("C:", "fake", "reasonix") {
		t.Errorf("REASONIX_HOME override not honored, got %q", got)
	}

	t.Setenv("REASONIX_HOME", "")
	base := t.TempDir()
	// os.UserConfigDir reads APPDATA on Windows, XDG_CONFIG_HOME on Linux, HOME on macOS —
	// set all three so the default resolves under the test's temp base on every platform.
	t.Setenv("APPDATA", base)
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	got, err = ReasonixConfigHome()
	if err != nil {
		t.Fatalf("ReasonixConfigHome (default): %v", err)
	}
	if want := filepath.Join(configDir, "reasonix"); got != want {
		t.Errorf("default home = %q, want %q (user-config dir + reasonix)", got, want)
	}
}

// TestStripReasonixHooksUserLevel: the uninstall path removes forge hooks from reasonix's
// settings.json while preserving user entries (incl. unknown fields) and unknown top-level keys;
// re-running is a clean no-op; a missing file is a clean no-op. Mirrors the cursor/codex strip
// tests.
//
// TestStripReasonixHooksUserLevel：卸载路径从 reasonix settings.json 移除 forge hooks，同时
// 保留用户条目（含未知字段）与未知顶层键；重跑是干净 no-op；文件缺失是干净 no-op。镜像
// cursor/codex 的 strip 测试。
func TestStripReasonixHooksUserLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate BackupOriginal (writes under forgedata.GlobalHome)
	settingsPath := filepath.Join(home, "settings.json")

	// Seed: a forge hook + a user hook (with an unknown field) in one event, plus a user
	// top-level key.
	seed := `{
		"myKey": "keep-me",
		"hooks": {
			"PreToolUse": [
				{ "match": "Bash", "command": "forge hook bash-guard" },
				{ "match": "Bash", "command": "echo user-hook", "timeout": 7 }
			]
		}
	}`
	if err := os.WriteFile(settingsPath, []byte(seed), 0644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	stripped, err := StripReasonixHooksUserLevel()
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if !stripped {
		t.Fatal("strip reported no change; want forge entries removed")
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read after strip: %v", err)
	}
	body := string(data)
	if strings.Contains(body, "forge hook") {
		t.Errorf("forge hooks remain after strip: %s", body)
	}
	if !strings.Contains(body, "keep-me") || !strings.Contains(body, "echo user-hook") || !strings.Contains(body, `"timeout": 7`) {
		t.Errorf("user content / unknown field not preserved verbatim after strip: %s", body)
	}

	// Second strip is a clean no-op (no forge hooks left).
	stripped2, err := StripReasonixHooksUserLevel()
	if err != nil {
		t.Fatalf("strip 2: %v", err)
	}
	if stripped2 {
		t.Error("second strip must be a no-op (no forge hooks left)")
	}

	// Missing file is a clean no-op.
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "nope"))
	stripped3, err := StripReasonixHooksUserLevel()
	if err != nil {
		t.Fatalf("strip on missing home: %v", err)
	}
	if stripped3 {
		t.Error("strip on a missing file must be a no-op")
	}
}

// TestReasonixWiringMirrorsClaudeSettings guards the sync between reasonix.go
// (buildReasonixHooks) and hooks/settings.go (GenerateSettings). reasonix uses Claude
// Code's PascalCase event names verbatim (identity mapping — see reasonixEventName),
// so for every event reasonix wires, Claude Code must wire the SAME command set under
// the same event name; drift silently disables a gate on reasonix. The reasonix
// whitelist deliberately covers only 4 of the 6 spec events (PostCompact /
// UserPromptSubmit deferred pending empirical probe — see
// TestReasonixHooks_OnlyLegalReasonixEvents), so the comparison is one-directional:
// every reasonix event must match Claude, not vice-versa. Parallel to
// TestCursorWiringMirrorsClaudeSettings.
//
// TestReasonixWiringMirrorsClaudeSettings 守卫 reasonix.go（buildReasonixHooks）与
// hooks/settings.go（GenerateSettings）的同步。reasonix 原样使用 Claude Code 的
// PascalCase event 名（恒等映射——见 reasonixEventName），故 reasonix 接的每个
// event，Claude Code 必须在同 event 名下接同一命令集；drift 会静默废掉 reasonix
// 上的某个门禁。reasonix 白名单刻意只覆盖 6 个 spec event 中的 4 个
// （PostCompact / UserPromptSubmit 待经验探测后再加——见
// TestReasonixHooks_OnlyLegalReasonixEvents），故比较是单向的：reasonix 每个 event
// 必须匹配 Claude，反之不要求。仿 TestCursorWiringMirrorsClaudeSettings。
func TestReasonixWiringMirrorsClaudeSettings(t *testing.T) {
	// reasonix registers at user level (<reasonix home>/settings.json) — isolate the home.
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate BackupOriginal (writes under forgedata.GlobalHome)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir reasonix home: %v", err)
	}
	claudeDir := t.TempDir()
	if err := hooks.GenerateSettings(claudeDir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	if err := (&ReasonixTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("reasonix Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	reasonix := reasonixHookCommandsByEvent(t, filepath.Join(home, "settings.json"))

	// reasonix PascalCase → Claude PascalCase is identity (reasonixEventName is a filter,
	// not a remap).
	eventMap := map[string]string{
		"PreToolUse":   "PreToolUse",
		"PostToolUse":  "PostToolUse",
		"Stop":         "Stop",
		"SessionStart": "SessionStart",
	}
	if len(reasonix) == 0 {
		t.Fatal("reasonix wiring has no events — generator or parser broken")
	}
	for rEvt, rCmds := range reasonix {
		claudeEvt, ok := eventMap[rEvt]
		if !ok {
			t.Errorf("reasonix event %q has no Claude Code mapping — new event not accounted for", rEvt)
			continue
		}
		claudeCmds, ok := claude[claudeEvt]
		if !ok {
			t.Errorf("Claude Code settings missing event %q that reasonix wires", claudeEvt)
			continue
		}
		if !stringSetEqual(claudeCmds, rCmds) {
			t.Errorf("hook commands for reasonix %q / claude %q drifted — keep settings.go GenerateSettings and reasonix.go buildReasonixHooks in sync:\n  claude:  %s\n  reasonix: %s",
				rEvt, claudeEvt, sortedSet(claudeCmds), sortedSet(rCmds))
		}
	}

	// Regression guard: sunk/deleted hooks must not resurface on reasonix either.
	sunk := []string{"read-check", "scope-guard", "clone-check", "experience-check", "security-check", "dependency-check", "test-coverage-check", "session-health"}
	for cmd := range reasonix["PostToolUse"] {
		for _, s := range sunk {
			if strings.Contains(cmd, "forge hook "+s) {
				t.Errorf("sunk hook %q resurfaced in reasonix settings: %s", s, cmd)
			}
		}
	}
}

// TestReasonixHooks_OnlyLegalReasonixEvents pins the reasonix event whitelist
// (reasonixEventName) against the Claude-Code-compatible PascalCase roster. reasonix
// uses CC event names verbatim, so any event outside the CC lifecycle set would never
// fire. The four classic enforcement events (PreToolUse/PostToolUse/Stop/SessionStart)
// MUST be present — they carry all hard enforcement. PostCompact and UserPromptSubmit
// (compact-resume / resume-reinject) are deliberately DEFERRED until empirically
// confirmed supported via `reasonix hook status --json` (reasonix may reject the whole
// file on an unknown event, as it rejected the CC double-nested form); this test pins
// them ABSENT so re-enabling forces a conscious whitelist + test update. Modeled on
// TestCursorHooks_OnlyLegalCursorEvents.
//
// TestReasonixHooks_OnlyLegalReasonixEvents 把 reasonix event 白名单
// （reasonixEventName）钉在 Claude-Code 兼容的 PascalCase 名册上。reasonix 原样
// 用 CC event 名，故 CC lifecycle 集合外的 event 永不触发。四个经典强制 event
// （PreToolUse/PostToolUse/Stop/SessionStart）必须在位——承载全部硬强制。PostCompact
// 与 UserPromptSubmit（compact-resume / resume-reinject）刻意暂缓，待经
// `reasonix hook status --json` 确认支持后再加（reasonix 遇未知 event 可能拒绝整个
// 文件，正如它拒绝 CC 双层嵌套形态）；本测试把它们钉为缺席，故重新启用须同步更新
// 白名单与测试。仿 TestCursorHooks_OnlyLegalCursorEvents。
func TestReasonixHooks_OnlyLegalReasonixEvents(t *testing.T) {
	raw := buildReasonixHooks()
	hooksMap, ok := raw[`hooks`].(map[string][]reasonixHookEntry)
	if !ok {
		t.Fatalf(`reasonix wiring shape unexpected: %T`, raw[`hooks`])
	}
	// PascalCase events the CC-compatible reasonix hook system accepts (superset — the
	// reasonixEventName whitelist narrows to the confirmed subset).
	legal := map[string]bool{
		"PreToolUse": true, "PostToolUse": true,
		"Stop": true, "SessionStart": true,
		"PostCompact": true, "UserPromptSubmit": true,
	}
	for event := range hooksMap {
		if !legal[event] {
			t.Errorf("illegal reasonix hook event %q (not in the CC-compatible roster — never fires)", event)
		}
	}
	for _, required := range []string{`PreToolUse`, `PostToolUse`, `Stop`, `SessionStart`} {
		if _, present := hooksMap[required]; !present {
			t.Errorf(`reasonix must wire %s (carries hard enforcement): missing`, required)
		}
	}
	for _, deferred := range []string{`PostCompact`, `UserPromptSubmit`} {
		if _, present := hooksMap[deferred]; present {
			t.Errorf(`reasonix must not yet wire %s (deferred pending empirical probe — add a reasonixEventName case + update this test before re-enabling)`, deferred)
		}
	}
}

// reasonixHookCommandsByEvent parses reasonix's flat settings.json into event → set of
// command strings. reasonix's schema {hooks:{event:[{match,command}]}} shares cursor's
// flat command-on-each-entry shape, so this mirrors cursorHookCommandsByEvent (kept
// separate only so a reasonix test does not call a cursor-named helper).
func reasonixHookCommandsByEvent(t *testing.T, path string) map[string]map[string]bool {
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

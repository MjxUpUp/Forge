package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// TestCodexTranslator_Translate_CreatesFile verifies Translate works when the codex
// home (and hooks.json) does not exist yet — e.g. wiring before codex's first run.
func TestCodexTranslator_Translate_CreatesFile(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "not-yet-created")
	t.Setenv("CODEX_HOME", codexHome)

	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "hooks.json")); err != nil {
		t.Fatalf("hooks.json not created: %v", err)
	}
}

// TestCodexTranslator_MergePreservesUserEntries pins the merge contract: user-defined
// hook entries (commands not forge-sourced) and unknown top-level fields survive
// Translate; forge entries are present exactly once (replaced wholesale, not appended).
func TestCodexTranslator_MergePreservesUserEntries(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "hooks.json")

	existing := `{
  "notify": ["user-notify-script"],
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/lint.sh"}]}
    ],
    "PostToolUse": [
      {"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "forge hook stale-removed-hook"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// User top-level field and user hook entry preserved.
	if !strings.Contains(content, `"notify"`) || !strings.Contains(content, "user-notify-script") {
		t.Error("user top-level field not preserved")
	}
	if !strings.Contains(content, "./scripts/lint.sh") {
		t.Error("user hook entry not preserved")
	}
	// Stale forge entry replaced: a command that is no longer generated must be gone.
	if strings.Contains(content, "stale-removed-hook") {
		t.Error("stale forge hook entry not replaced")
	}
	// Forge wiring present exactly once per command. Since Wave 1b every generated
	// command carries the --agent codex suffix (output-protocol selection).
	if n := strings.Count(content, `"forge hook task-guard --agent codex"`); n != 1 {
		t.Errorf("forge hook task-guard appears %d times, want 1", n)
	}
	if !strings.Contains(content, "forge hook bash-guard --agent codex") {
		t.Error("generated forge wiring missing")
	}
}

// TestCodexTranslator_Idempotent verifies a second Translate is a byte-identical no-op.
func TestCodexTranslator_Idempotent(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "hooks.json")

	tr := &CodexTranslator{}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("second Translate not idempotent")
	}
}

// TestStripCodexHooksUserLevel covers the strip roundtrip: Translate then Strip
// removes every forge entry while preserving user entries; a second Strip and a
// missing file are clean no-ops.
func TestStripCodexHooksUserLevel(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "hooks.json")

	// Missing file → clean no-op.
	changed, err := StripCodexHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("missing file: changed=%v err=%v, want false/nil", changed, err)
	}

	// Seed a user entry, then Translate to add forge wiring.
	seed := `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/lint.sh"}]}]}}`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}

	changed, err = StripCodexHooksUserLevel()
	if err != nil || !changed {
		t.Fatalf("strip after Translate: changed=%v err=%v, want true/nil", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "forge hook") || strings.Contains(content, "forge gate") {
		t.Errorf("forge entries remain after strip:\n%s", content)
	}
	if !strings.Contains(content, "./scripts/lint.sh") {
		t.Error("user hook entry lost after strip")
	}
	// File itself must survive (user-level config is never deleted).
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("stripped file is not valid JSON: %v", err)
	}

	// Second strip → no-op.
	changed, err = StripCodexHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("second strip: changed=%v err=%v, want false/nil", changed, err)
	}
}

// TestCodexTranslator_MergePreservesUnknownFields pins the raw-merge fix: user
// hook entries carrying fields the typed struct does not declare (timeout,
// commandWindows) must survive Translate with values intact — a typed round-trip
// silently dropped them.
//
// TestCodexTranslator_MergePreservesUnknownFields 钉死 raw-merge 修复：携带类型化
// struct 未声明字段（timeout、commandWindows）的用户 hook 条目必须在 Translate
// 后值完整保留——类型化往返会静默丢弃它们。
func TestCodexTranslator_MergePreservesUnknownFields(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "hooks.json")

	existing := `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [
        {"type": "command", "command": "./scripts/lint.sh", "timeout": 30, "commandWindows": "lint.cmd"}
      ]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Field-level assertion (formatting-agnostic): the kept user entry must carry
	// every original field with its original value.
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	var userEntry map[string]any
	for _, m := range cfg.Hooks["PreToolUse"] {
		for _, h := range m.Hooks {
			if h["command"] == "./scripts/lint.sh" {
				userEntry = h
			}
		}
	}
	if userEntry == nil {
		t.Fatal("user hook entry not preserved")
	}
	if userEntry["timeout"] != float64(30) {
		t.Errorf("user entry unknown field timeout lost or altered: %v", userEntry["timeout"])
	}
	if userEntry["commandWindows"] != "lint.cmd" {
		t.Errorf("user entry unknown field commandWindows lost or altered: %v", userEntry["commandWindows"])
	}
}

// ---- config.toml [features] hooks = true (codex hooks feature gate) ----

// TestCodexTranslator_EnsuresHooksFeature_Fresh pins the blocker fix: codex
// lifecycle hooks are gated behind `[features] hooks = true` (default off), so
// Translate must create config.toml with the flag inside forge markers —
// otherwise the freshly-written hooks.json is silently inert.
//
// TestCodexTranslator_EnsuresHooksFeature_Fresh 钉死 blocker 修复：codex
// lifecycle hooks 由 `[features] hooks = true` 门控（默认关），Translate 必须
// 创建带该开关的 config.toml（forge 标记段内）——否则刚写好的 hooks.json
// 静默不生效。
func TestCodexTranslator_EnsuresHooksFeature_Fresh(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[features]") || !strings.Contains(content, "hooks = true") {
		t.Errorf("config.toml missing [features] hooks = true:\n%s", content)
	}
	if !strings.Contains(content, codexMarkStart) || !strings.Contains(content, codexMarkEnd) {
		t.Error("config.toml forge section missing markers")
	}
}

// TestCodexTranslator_EnsuresHooksFeature_Idempotent: a second Translate leaves
// config.toml byte-identical.
//
// TestCodexTranslator_EnsuresHooksFeature_Idempotent：第二次 Translate 后
// config.toml 逐字节一致。
func TestCodexTranslator_EnsuresHooksFeature_Idempotent(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "config.toml")

	tr := &CodexTranslator{}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second Translate changed config.toml:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestCodexTranslator_EnsuresHooksFeature_PreservesUserConfig: user content
// outside the markers survives; an existing [features] table without a hooks key
// gets `hooks = true` inserted under it (a second [features] table would be
// invalid TOML).
//
// TestCodexTranslator_EnsuresHooksFeature_PreservesUserConfig：标记外的用户内容
// 保留；已有 [features] 表但无 hooks 键时，在其表头下插入 `hooks = true`
// （再建第二个 [features] 表是非法 TOML）。
func TestCodexTranslator_EnsuresHooksFeature_PreservesUserConfig(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "config.toml")

	// Case 1: no [features] table — marked section appended, user config intact.
	existing := "model = \"o3\"\n\n[profiles.work]\nmodel = \"gpt-5\"\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `model = "o3"`) || !strings.Contains(content, "[profiles.work]") {
		t.Error("user config outside markers not preserved")
	}
	if !strings.Contains(content, codexMarkStart) {
		t.Error("marked forge section not appended")
	}

	// Case 2: [features] exists without hooks key — inserted under the header, no
	// duplicate table.
	existing2 := "model = \"o3\"\n\n[features]\nunified_exec = true\n"
	if err := os.WriteFile(path, []byte(existing2), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = string(data)
	if n := strings.Count(content, "[features]"); n != 1 {
		t.Errorf("[features] table appears %d times, want 1 (duplicate table is invalid TOML):\n%s", n, content)
	}
	if !strings.Contains(content, "unified_exec = true") {
		t.Error("existing [features] content not preserved")
	}
	hooksIdx := strings.Index(content, "hooks = true")
	if hooksIdx == -1 {
		t.Fatal("hooks = true not inserted into existing [features] table")
	}
	// The inserted line must sit inside the [features] table (before the next
	// table header or EOF), not appended at the end of the file.
	featuresIdx := strings.Index(content, "[features]")
	if next := strings.Index(content[featuresIdx+1:], "\n["); next != -1 && hooksIdx > featuresIdx+1+next {
		t.Errorf("hooks = true placed outside the [features] table:\n%s", content)
	}
}

// TestCodexTranslator_EnsuresHooksFeature_RespectsExplicitFalse: when the user
// explicitly disabled hooks (`hooks = false`), Translate must not flip it —
// the file stays untouched.
//
// TestCodexTranslator_EnsuresHooksFeature_RespectsExplicitFalse：用户显式
// `hooks = false` 时 Translate 不得改写——文件保持不动。
func TestCodexTranslator_EnsuresHooksFeature_RespectsExplicitFalse(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "config.toml")

	existing := "[features]\nhooks = false\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Errorf("explicit hooks = false must be respected (file untouched), got:\n%s", data)
	}
}

// TestCodexTranslator_EnsuresHooksFeature_AlreadyTrue: a user-managed
// `hooks = true` needs no marked section — the file stays untouched.
//
// TestCodexTranslator_EnsuresHooksFeature_AlreadyTrue：用户自己管理的
// `hooks = true` 无需标记段——文件保持不动。
func TestCodexTranslator_EnsuresHooksFeature_AlreadyTrue(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "config.toml")

	existing := "model = \"o3\"\n\n[features]\nhooks = true\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Errorf("existing hooks = true must be a no-op, got:\n%s", data)
	}
}

// TestCodexMatchers_ApplyPatchAndAgentSuffix pins the two codex-specific deltas
// applied on the spec copy (Wave 1b + 3a): every forge command gains
// ` --agent codex` (output-protocol selection — codex parses no decision:"approve"
// and blocks only via stderr+exit 2), and matchers containing Write/Edit are
// widened with apply_patch (codex reports file edits as tool_name "apply_patch";
// a plain Write|Edit regex never matches, silently disarming every file gate).
// The shared spec itself must stay untouched.
func TestCodexMatchers_ApplyPatchAndAgentSuffix(t *testing.T) {
	spec := hooks.ForgeHookSpec()
	// Snapshot the PreToolUse matchers before, to prove codexMatchers never
	// mutates the shared spec (settings.local.json / plugin pack share it).
	before := fmt.Sprintf("%v", spec["PreToolUse"])

	got := codexMatchers(spec["PreToolUse"])
	if len(got) == 0 {
		t.Fatal("codexMatchers returned no matchers")
	}
	sawApplyPatch, sawAgentSuffix := false, false
	for _, m := range got {
		for _, tok := range strings.Split(m.Matcher, "|") {
			if tok == "apply_patch" {
				sawApplyPatch = true
			}
		}
		for _, e := range m.Hooks {
			if strings.Contains(e.Command, "forge hook") && !strings.HasSuffix(e.Command, " --agent codex") {
				t.Errorf("forge command missing --agent codex suffix: %s", e.Command)
			}
			if strings.HasSuffix(e.Command, " --agent codex") {
				sawAgentSuffix = true
			}
		}
	}
	if !sawApplyPatch {
		t.Error("no Write|Edit matcher was widened with apply_patch — file gates would no-op on codex edits")
	}
	if !sawAgentSuffix {
		t.Error("no command carries --agent codex")
	}
	if after := fmt.Sprintf("%v", spec["PreToolUse"]); after != before {
		t.Error("codexMatchers must not mutate the shared ForgeHookSpec")
	}
}

// TestCodexApplyPatchMatcher covers the matcher-widening edge cases: only
// Write/Edit tokens trigger the widening, an already-widened matcher is not
// double-widened, and non-file matchers (Bash, Read|Skill|Agent, empty) pass
// through unchanged.
func TestCodexApplyPatchMatcher(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Write|Edit", "Write|Edit|apply_patch"},
		{"Bash", "Bash"},
		{"Read|Skill|Agent", "Read|Skill|Agent"},
		{"", ""},
		{"Write|Edit|apply_patch", "Write|Edit|apply_patch"},
		{"apply_patch", "apply_patch"},
	}
	for _, tc := range cases {
		if got := codexApplyPatchMatcher(tc.in); got != tc.want {
			t.Errorf("codexApplyPatchMatcher(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

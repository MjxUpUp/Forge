package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// TestKimiWiringMirrorsClaudeSettings guards that the kimi [[hooks]] block is derived
// from hooks.ForgeHookSpec (single source of truth): every spec entry appears exactly
// once, under the same event and matcher, with the command rewritten to carry
// `--agent kimi`. Mirrors TestCodexWiringMirrorsClaudeSettings.
//
// TestKimiWiringMirrorsClaudeSettings 守卫 kimi 的 [[hooks]] 块派生自
// hooks.ForgeHookSpec（单一真相源）：每条 spec 条目恰好出现一次，event 与
// matcher 不变，command 改写为带 `--agent kimi`。对齐
// TestCodexWiringMirrorsClaudeSettings。
func TestKimiWiringMirrorsClaudeSettings(t *testing.T) {
	toml := BuildKimiHooksTOML()
	spec := hooks.ForgeHookSpec()

	// Index blocks individually: the same command (skill-trigger) legitimately
	// appears under several events, so position-based lookup would mismatch.
	//
	// 逐块索引：同一 command（skill-trigger）合法地出现在多个事件下，按位置
	// 查找会错配。
	blocks := strings.Split(toml, "[[hooks]]\n")

	total := 0
	for event, matchers := range spec {
		for _, m := range matchers {
			for _, entry := range m.Hooks {
				total++
				cmd := kimiCommand(entry.Command)
				wantEvent := "event = " + tomlBasicString(event)
				wantCmd := "command = " + tomlBasicString(cmd)
				found := false
				for _, block := range blocks {
					if !strings.Contains(block, wantEvent) || !strings.Contains(block, wantCmd) {
						continue
					}
					if m.Matcher != "" && !strings.Contains(block, "matcher = "+tomlBasicString(m.Matcher)) {
						continue
					}
					found = true
					break
				}
				if !found {
					t.Errorf("no [[hooks]] block for event=%s matcher=%q command=%q", event, m.Matcher, cmd)
				}
				if !strings.HasSuffix(cmd, "--agent kimi") {
					t.Errorf("command %q not rewritten for kimi", entry.Command)
				}
			}
		}
	}
	// blocks[0] is the (empty) head before the first [[hooks]] marker.
	if got := len(blocks) - 1; got != total {
		t.Errorf("expected %d [[hooks]] entries (one per spec command), got %d", total, got)
	}
	// No entry may keep the bare Claude-shape command (without --agent kimi, runHook
	// would emit the Claude JSON envelope, which kimi would inject as garbage text).
	for _, line := range strings.Split(toml, "\n") {
		if strings.HasPrefix(line, "command = ") && !strings.Contains(line, "--agent kimi") {
			t.Errorf("command without --agent kimi: %s", line)
		}
	}
}

// TestKimiTranslator_Translate verifies the user-level config.toml merge: existing user
// config (model/provider/permission) is preserved byte-for-byte, the forge section is
// appended inside markers, and a second Translate is a no-op (idempotent).
func TestKimiTranslator_Translate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	userConfig := "default_model = \"kimi-code/k3\"\ntelemetry = false\n\n[[permission.rules]]\ndecision = \"allow\"\npattern = \"Read\"\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(userConfig), 0644); err != nil {
		t.Fatal(err)
	}

	tr := &KimiTranslator{}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, userConfig) {
		t.Errorf("user config not preserved at head of file:\n%s", got)
	}
	for _, want := range []string{kimiMarkStart, kimiMarkEnd, "[[hooks]]", `event = "PreToolUse"`, "--agent kimi"} {
		if !strings.Contains(got, want) {
			t.Errorf("merged config missing %q", want)
		}
	}

	// Idempotent: second Translate must not change the file.
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("second Translate failed: %v", err)
	}
	data2, _ := os.ReadFile(path)
	if string(data2) != got {
		t.Errorf("second Translate not idempotent")
	}
}

// TestKimiTranslator_Translate_CreatesFile verifies Translate works when config.toml
// (and the config home) does not exist yet — e.g. wiring before kimi's first run.
func TestKimiTranslator_Translate_CreatesFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-yet-created")
	t.Setenv("KIMI_CODE_HOME", home)

	if err := (&KimiTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	if !strings.Contains(string(data), kimiMarkStart) {
		t.Errorf("created config missing forge section")
	}
}

// TestKimiTranslator_Translate_ReplacesStaleSection verifies that an outdated forge
// section is replaced in place (not duplicated) on re-init after a forge upgrade.
func TestKimiTranslator_Translate_ReplacesStaleSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	stale := "default_model = \"x\"\n\n" + kimiMarkStart + "\n[[hooks]]\nevent = \"PreToolUse\"\ncommand = \"forge hook stale-hook --agent kimi\"\n" + kimiMarkEnd + "\n\n# user comment after section\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(stale), 0644); err != nil {
		t.Fatal(err)
	}

	if err := (&KimiTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if strings.Contains(got, "stale-hook") {
		t.Errorf("stale section not replaced")
	}
	if strings.Count(got, kimiMarkStart) != 1 || strings.Count(got, kimiMarkEnd) != 1 {
		t.Errorf("section duplicated after re-init")
	}
	if !strings.HasPrefix(got, "default_model = \"x\"\n") || !strings.HasSuffix(got, "# user comment after section\n") {
		t.Errorf("content outside markers not preserved:\n%s", got)
	}
}

func TestKimiTranslator_Detect(t *testing.T) {
	// Project-level signal: .kimi-code/ dir.
	dir := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "absent"))
	if (&KimiTranslator{}).Detect(dir) {
		t.Errorf("Detect true without any kimi signal")
	}
	os.MkdirAll(filepath.Join(dir, ".kimi-code"), 0755)
	if !(&KimiTranslator{}).Detect(dir) {
		t.Errorf("Detect false with .kimi-code/ present")
	}

	// User-level signal: existing $KIMI_CODE_HOME.
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	if !(&KimiTranslator{}).Detect(t.TempDir()) {
		t.Errorf("Detect false with existing KIMI_CODE_HOME")
	}
}

// TestStripKimiHooks verifies uninstall cleanup: Translate-then-Strip restores the
// user's original config byte-for-byte, and a second Strip is a clean no-op.
func TestStripKimiHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	userConfig := "default_model = \"kimi-code/k3\"\n"
	path := filepath.Join(home, "config.toml")
	os.WriteFile(path, []byte(userConfig), 0644)

	if err := (&KimiTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	stripped, err := StripKimiHooks()
	if err != nil || !stripped {
		t.Fatalf("StripKimiHooks = (%v, %v), want (true, nil)", stripped, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != userConfig {
		t.Errorf("config not restored byte-for-byte after strip:\n%q", string(data))
	}

	// Second strip: no markers → clean no-op.
	stripped, err = StripKimiHooks()
	if err != nil || stripped {
		t.Errorf("second StripKimiHooks = (%v, %v), want (false, nil)", stripped, err)
	}
}

// TestKimiCommandAndTimeout pins the per-command rewrite rules.
func TestKimiCommandAndTimeout(t *testing.T) {
	if got := kimiCommand("forge hook task-guard"); got != "forge hook task-guard --agent kimi" {
		t.Errorf("kimiCommand = %q", got)
	}
	if got := kimiCommand("forge task gate x"); got != "forge task gate x" {
		t.Errorf("non-hook command must stay untouched, got %q", got)
	}
	if got := kimiTimeout("forge hook auto-compile"); got != 60 {
		t.Errorf("auto-compile timeout = %d, want 60", got)
	}
	if got := kimiTimeout("forge hook task-guard"); got != 30 {
		t.Errorf("default timeout = %d, want 30", got)
	}
}

// TestBuildKimiHooksTOML_Deterministic guards golden stability: map iteration order must
// not leak into the output (idempotency and review diffs depend on it).
func TestBuildKimiHooksTOML_Deterministic(t *testing.T) {
	first := BuildKimiHooksTOML()
	for i := 0; i < 10; i++ {
		if got := BuildKimiHooksTOML(); got != first {
			t.Fatalf("BuildKimiHooksTOML not deterministic (run %d)", i)
		}
	}
}

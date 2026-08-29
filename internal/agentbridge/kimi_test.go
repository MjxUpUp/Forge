package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// TestKimiWiringMirrorsClaudeSettings guards that the kimi [[hooks]] block is derived
// from hooks.ForgeHookSpec (single source of truth): every spec entry on a
// kimi-supported event appears exactly once, under the same event and matcher, with
// the command rewritten to carry `--agent kimi`. Events outside kimiSupportedEvents
// are deliberately ABSENT from the TOML (unverified events can fail kimi's config
// validation and kill every hook — see BuildKimiHooksTOML), so parity is scoped the
// same way as the plugin-manifest test. Mirrors TestCodexWiringMirrorsClaudeSettings.
//
// TestKimiWiringMirrorsClaudeSettings 守卫 kimi 的 [[hooks]] 块派生自
// hooks.ForgeHookSpec（单一真相源）：kimi 支持事件上的每条 spec 条目恰好出现
// 一次，event 与 matcher 不变，command 改写为带 `--agent kimi`。
// kimiSupportedEvents 之外的事件刻意不进 TOML（未验证事件可能让 kimi config
// 校验失败、杀掉全部 hook——见 BuildKimiHooksTOML），故对等范围与 plugin
// manifest 测试同样收窄。对齐 TestCodexWiringMirrorsClaudeSettings。
func TestKimiWiringMirrorsClaudeSettings(t *testing.T) {
	toml := BuildKimiHooksTOML()
	spec := hooks.ForgeHookSpec()

	// Index blocks individually: the same command (skill-trigger) legitimately
	// appears under several events, so position-based lookup would mismatch.
	//
	// 逐块索引：同一 command（skill-trigger）合法地出现在多个事件下，按位置
	// 查找会错配。
	blocks := strings.Split(toml, "[[hooks]]\n")

	// Unsupported events must be absent entirely, not just uncounted.
	//
	// 不支持的事件必须整体缺席，而非仅不计数。
	for _, banned := range []string{"PostToolUseFailure", "SubagentStop"} {
		if strings.Contains(toml, "event = "+tomlBasicString(banned)) {
			t.Errorf("kimi config.toml must not carry unverified event %s (validation risk kills all hooks)", banned)
		}
	}

	total := 0
	for event, matchers := range spec {
		if !kimiSupportedEvents[event] {
			continue
		}
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

// TestKimiHooks_WireResumeReinjectOnUserPromptSubmit pins the channel dependency of the
// 2026-08-15 staleness-advisory move: the kimi [[hooks]] block must wire resume-reinject
// under UserPromptSubmit — the ONE stdout channel kimi 0.35.0 delivers to the model
// (next-prompt delivery). The plugin-stale advisory rides that hook
// (cli.kimiStaleRidesHook); if resume-reinject ever drops off UserPromptSubmit (spec
// edit, event rename), the advisory AND the P3 compaction handoff both go silent on
// kimi with nothing else failing — this is the only guard. Complements
// TestKimiWiringMirrorsClaudeSettings (which proves spec↔TOML parity but cannot catch a
// spec-level regression).
//
// TestKimiHooks_WireResumeReinjectOnUserPromptSubmit 钉死 2026-08-15 staleness advisory
// 迁移的通道依赖：kimi [[hooks]] 块必须把 resume-reinject 接在 UserPromptSubmit 下——
// kimi 0.35.0 唯一把 stdout 送达模型的通道（下一 prompt 送达）。plugin-stale advisory
// 搭载该 hook（cli.kimiStaleRidesHook）；若 resume-reinject 从 UserPromptSubmit 掉线
// （spec 改动、event 改名），advisory 与 P3 压缩 handoff 在 kimi 上双双静默且无其他
// 测试报警——本守卫是唯一防线。补充 TestKimiWiringMirrorsClaudeSettings（它证明
// spec↔TOML 对等，但抓不住 spec 层回退）。
func TestKimiHooks_WireResumeReinjectOnUserPromptSubmit(t *testing.T) {
	blocks := strings.Split(BuildKimiHooksTOML(), "[[hooks]]\n")
	found := false
	for _, block := range blocks {
		if !strings.Contains(block, "event = "+tomlBasicString("UserPromptSubmit")) {
			continue
		}
		if strings.Contains(block, "command = "+tomlBasicString(kimiCommand("forge hook resume-reinject"))) {
			found = true
			break
		}
	}
	if !found {
		t.Error("kimi [[hooks]] must wire resume-reinject under UserPromptSubmit — the stale advisory (kimiStaleRidesHook) and the P3 compaction handoff both ride it; losing this wiring silences both on kimi")
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

// TestKimiMarkerCorruption pins the data-loss guard: unpaired or inverted markers must
// produce an error (and leave the file untouched), never a guess — the region between an
// orphaned START and a later END is user config (model/provider/API keys).
func TestKimiMarkerCorruption(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	path := filepath.Join(home, "config.toml")

	corrupt := map[string]string{
		"orphan START": "default_model = \"x\"\n" + kimiMarkStart + "\ntelemetry = false\n",
		"orphan END":   "default_model = \"x\"\n" + kimiMarkEnd + "\n",
		"inverted":     kimiMarkEnd + "\ndefault_model = \"x\"\n" + kimiMarkStart + "\n",
	}
	for name, content := range corrupt {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if err := (&KimiTranslator{}).Translate(t.TempDir(), testInput()); err == nil {
			t.Errorf("%s: Translate must fail on corrupt markers", name)
		}
		data, _ := os.ReadFile(path)
		if string(data) != content {
			t.Errorf("%s: corrupt-marker Translate must not touch the file", name)
		}
		if stripped, err := StripKimiHooks(); err == nil || stripped {
			t.Errorf("%s: StripKimiHooks = (%v, %v), want (false, error)", name, stripped, err)
		}
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

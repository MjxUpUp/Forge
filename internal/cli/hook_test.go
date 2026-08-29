package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

func TestHookOutput_AllowOnMissingProject(t *testing.T) {
	// Run in a temp dir without .forge/ — allow must be SILENT (exit 0, no stdout).
	// The old contract printed {"decision":"approve"}, which bypassed Claude's
	// permission flow on PreToolUse and marked the hook failed on codex.
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Reset command output capture
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Simulate calling hook with no project
	err := runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout

	// Should not error (silently allow)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Read captured stdout
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))

	// Allow with no detail emits NOTHING — no approve envelope on any host.
	if output != "" {
		t.Errorf("allow on missing project must be silent, got stdout: %q", output)
	}
}

func TestHookOutput_UnknownHook(t *testing.T) {
	err := runHook(nil, []string{"nonexistent-hook"})
	if err == nil {
		t.Fatal("expected error for unknown hook")
	}
	if !strings.Contains(err.Error(), "unknown hook") {
		t.Errorf("error = %q, want 'unknown hook'", err.Error())
	}
}

func TestHookOutput_StructuredJSON(t *testing.T) {
	// Create a temp project with .forge/ directory
	tmpDir := t.TempDir()
	forgeDir := filepath.Join(tmpDir, ".forge", "hooks")
	os.MkdirAll(forgeDir, 0755)
	// Write a minimal state.json to make it look like a forge project
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Provide stdin JSON (simulating Claude Code input)
	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"src/main.go","content":"package main"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout

	// May error if the embedded script fails — the emission contract holds either way.
	_ = err

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))

	// Allow with no detail is silent; allow with detail is a BARE hookSpecificOutput
	// (no decision); a block is decision:"block". The one forbidden shape on the allow
	// path is decision:"approve" (bypasses Claude permissions, fails the hook on codex).
	if output == "" {
		return
	}

	// Must be valid JSON
	var result HookOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %q, err: %v", output, err)
	}

	if result.Decision != "" && result.Decision != "block" {
		t.Errorf("decision = %q, want \"\" (bare allow) or 'block'", result.Decision)
	}

	// If hookSpecificOutput is present, it must include hookEventName
	if result.HookSpecificOutput != nil && result.HookSpecificOutput.HookEventName == "" {
		t.Error("hookSpecificOutput has no hookEventName")
	}
	if result.HookSpecificOutput != nil && result.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q, want %q", result.HookSpecificOutput.HookEventName, "PostToolUse")
	}
}

func TestHookOutput_CheckLogRecorded(t *testing.T) {
	// Isolate the forge global home: checklog now records to the user-level
	// DataDir (forgedata.DataDirFor), never the project tree.
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	// Create a temp project with .forge/ directory
	tmpDir := t.TempDir()
	forgeDir := filepath.Join(tmpDir, ".forge", "hooks")
	os.MkdirAll(forgeDir, 0755)
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Provide stdin JSON
	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"README.md","content":"hello"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout
	r.Read(make([]byte, 8192))

	// Check that checklog.jsonl was created in the user-level DataDir
	checklogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "checklog.jsonl")
	data, err := os.ReadFile(checklogPath)
	if err != nil {
		t.Fatalf("checklog.jsonl not created: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line in checklog, got %d", len(lines))
	}

	// Parse the entry
	type logEntry struct {
		Check   string `json:"check"`
		Passed  bool   `json:"passed"`
		Checked bool   `json:"checked"`
		Detail  string `json:"detail"`
	}
	var entry logEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("checklog entry not valid JSON: %v", err)
	}
	if entry.Check != "auto-compile" {
		t.Errorf("check = %q, want %q", entry.Check, "auto-compile")
	}
	if !entry.Checked {
		t.Error("checked = false, want true")
	}
}

// TestFirstNonEmpty documents the checklog-detail contract: the detail fallback must be empty,
// not a `completed` placeholder. assertion-check/auto-compile pass silently in the common case,
// and a fake `completed` detail would pollute checklog stats (~713 placeholder entries/week,
// forge-weekly-audit-2026-08-09). The contract is locked here at the helper level so a call-site
// regression is caught directly.
//
// TestFirstNonEmpty 文档化 checklog-detail 契约：detail 回退须为空，而非 `completed` 占位符。
// assertion-check/auto-compile 常态静默通过，假的 `completed` detail 会污染 checklog 统计
// （每周 ~713 条占位条目，forge-weekly-audit-2026-08-09）。契约在 helper 层锁定，调用处
// 回归能被直接捕获。
func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty(empty, empty) = %q, want empty (no placeholder fallback)", got)
	}
	if got := firstNonEmpty("warn", "out"); got != "warn" {
		t.Errorf("firstNonEmpty(warn, out) = %q, want warn (stderr precedence)", got)
	}
	if got := firstNonEmpty("", "PASS"); got != "PASS" {
		t.Errorf("firstNonEmpty(empty, PASS) = %q, want PASS", got)
	}
}

// TestShouldRecordCheck is the truth table for the checklog noise gate.
// Scoring reads only the latest entry per check name, so a per-call PASS is
// redundant for any check scoring does not consume. FAIL is always recorded
// (block/warn signal); PASS is recorded only for scoring-dependent checks.
func TestShouldRecordCheck(t *testing.T) {
	cases := []struct {
		name   string
		check  checklog.CheckName
		passed bool
		want   bool
	}{
		{"scoring pass: assertion-check", checklog.CheckAssertion, true, true},
		{"scoring pass: auto-compile", checklog.CheckAutoCompile, true, true},
		{"non-scoring pass: bash-guard", "bash-guard", true, false},
		{"non-scoring pass: file-sentinel", "file-sentinel", true, false},
		{"non-scoring pass: task-guard", "task-guard", true, false},
		{"scoring fail still recorded", checklog.CheckAssertion, false, true},
		{"non-scoring fail still recorded", "bash-guard", false, true},
		{"unknown check pass not recorded", "some-future-check", true, false},
		{"unknown check fail recorded", "some-future-check", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldRecordCheck(c.check, c.passed)
			if got != c.want {
				t.Errorf("shouldRecordCheck(%q, passed=%v) = %v, want %v", c.check, c.passed, got, c.want)
			}
		})
	}
}

// TestHookCheckLogNoiseGate_ScoringPassRecorded verifies the inverse: a
// scoring-dependent hook's PASS IS still recorded (scoring's LatestByCheck
// depends on it). auto-compile is scoring-dependent.
func TestHookCheckLogNoiseGate_ScoringPassRecorded(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".forge", "hooks"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	// Non-code file → auto-compile passes without compiling.
	tmpStdin.WriteString(`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"README.md","content":"hello"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout
	r.Read(make([]byte, 8192))

	// auto-compile is scoring-dependent → its PASS MUST be recorded (user-level DataDir).
	checklogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "checklog.jsonl")
	data, err := os.ReadFile(checklogPath)
	if err != nil {
		t.Fatalf("expected checklog entry for scoring PASS (auto-compile), got read err: %v", err)
	}
	if !strings.Contains(string(data), `"check":"auto-compile"`) {
		t.Errorf("checklog missing auto-compile entry, got: %s", strings.TrimSpace(string(data)))
	}
}

func TestToRelPath(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		absPath string
		want    string
	}{
		{
			name:    "absolute path to .forge state file",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/DevWorkbench/.forge/tasks/feature-v1-layout-refactor.json"),
			want:    ".forge/tasks/feature-v1-layout-refactor.json",
		},
		{
			name:    "absolute path to source file",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/DevWorkbench/src/components/chat/ChatView.tsx"),
			want:    "src/components/chat/ChatView.tsx",
		},
		{
			name:    "absolute path to .claude/settings",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/DevWorkbench/.claude/settings.local.json"),
			want:    ".claude/settings.local.json",
		},
		{
			name:    "empty root returns original",
			root:    "",
			absPath: filepath.FromSlash("E:/DevWorkbench/.forge/tasks/x.json"),
			want:    filepath.FromSlash("E:/DevWorkbench/.forge/tasks/x.json"),
		},
		{
			name:    "empty path returns empty",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: "",
			want:    "",
		},
		{
			name:    "path outside root uses ..",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/OtherProject/src/main.go"),
			want:    "../OtherProject/src/main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toRelPath(tt.root, tt.absPath)
			if got != tt.want {
				t.Errorf("toRelPath(%q, %q) = %q, want %q", tt.root, tt.absPath, got, tt.want)
			}
		})
	}
}

// TestToRelPath_SymlinkBoundary reproduces the macOS /var → /private/var
// divergence that broke task-guard on macOS CI: findProjectRoot returns the
// physical directory (via os.Getwd), but the tool_input file_path arrives in
// the symlink form (t.TempDir() + filepath.Join). Without resolving both sides,
// filepath.Rel crossed the link boundary and returned a ../.. path that failed
// to match the .forge/* glob — so task-guard did not block .forge/state.json
// writes. This test fails on the pre-fix toRelPath.
func TestToRelPath_SymlinkBoundary(t *testing.T) {
	// Physical dir plays the role of /private/var/folders/.../tmp.
	realDir := t.TempDir()
	forgeDir := filepath.Join(realDir, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	target := filepath.Join(forgeDir, "state.json")
	if err := os.WriteFile(target, []byte("{}"), 0644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}
	// Symlink plays the role of /var/folders/.../tmp pointing at the physical dir.
	linkDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("intentional skip: symlinks unavailable on this host (Windows may need developer mode/admin): %v", err)
	}
	// file_path as it arrives at the hook: via the symlink, not the physical path.
	absViaLink := filepath.Join(linkDir, ".forge", "state.json")
	got := toRelPath(realDir, absViaLink)
	if got != ".forge/state.json" {
		t.Errorf("toRelPath across symlink boundary = %q, want .forge/state.json (root physical, file_path via symlink — the macOS /var vs /private/var case)", got)
	}
}

// TestProjectTagFor_StableAndCleanInvariant verifies the project tag is a stable
// function of the canonical project root: the same directory expressed as
// relative/absolute/redundant-path forms must yield one tag, and distinct
// directories must yield distinct tags. This is what makes it a safe per-project
// scoping key (unlike $PWD/cksum, which varies with path case/form and host
// cksum format).
func TestProjectTagFor_StableAndCleanInvariant(t *testing.T) {
	dir := t.TempDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}

	tagDirect := projectTagFor(dir)
	tagAbs := projectTagFor(abs)
	tagRedundant := projectTagFor(filepath.Join(abs, "x", ".."))

	if tagDirect != tagAbs {
		t.Errorf("tag differs for temp dir vs absolute form: %q vs %q", tagDirect, tagAbs)
	}
	if tagDirect != tagRedundant {
		t.Errorf("tag changed after redundant path components (Clean invariance): %q vs %q", tagDirect, tagRedundant)
	}
	if tagDirect == "" {
		t.Error("project tag is empty")
	}

	// Distinct directories must not collide.
	other := t.TempDir()
	if projectTagFor(other) == tagDirect {
		t.Errorf("two different temp dirs produced the same project tag %q", tagDirect)
	}
}

// TestSanitizeForShell guards the env-var injection defense used by runHook to
// pass user-controlled content into bash hook scripts. It covers empty
// passthrough, control-char/NULL handling, and — critically — that overlong
// values truncate within maxEnvValueLen at a UTF-8 boundary (never mid-rune),
// which is what prevents both memory exhaustion and malformed-UTF-8 env vars.
func TestSanitizeForShell(t *testing.T) {
	if got := sanitizeForShell(""); got != "" {
		t.Errorf(`sanitizeForShell("") = %q, want ""`, got)
	}
	if got := sanitizeForShell("hello world"); got != "hello world" {
		t.Errorf("sanitizeForShell(ascii) = %q, want unchanged", got)
	}
	// NULL byte -> space; other control chars stripped; tab/nl/cr preserved.
	if got := sanitizeForShell("a\x00b\x01\x02"); got != "a b" {
		t.Errorf("sanitizeForShell(NULL+ctrl) = %q, want %q", got, "a b")
	}
	if got := sanitizeForShell("a\tb\nc\rd"); got != "a\tb\nc\rd" {
		t.Errorf("sanitizeForShell(tab/nl/cr) = %q, want preserved", got)
	}
	// Invalid UTF-8 bytes are dropped (utf8.RuneError skip), valid kept.
	if got := sanitizeForShell("a\xff\xfeb"); got != "ab" {
		t.Errorf("sanitizeForShell(bad-utf8) = %q, want %q", got, "ab")
	}

	// Overlong ASCII truncates to within [max-10, max].
	long := strings.Repeat("x", maxEnvValueLen+5000)
	got := sanitizeForShell(long)
	if len(got) > maxEnvValueLen || len(got) < maxEnvValueLen-10 {
		t.Errorf("overlong ascii truncated to len %d, want in [%d, %d]", len(got), maxEnvValueLen-10, maxEnvValueLen)
	}

	// Overlong multi-byte UTF-8 truncates at a rune boundary: the result must
	// stay valid UTF-8 (no mid-rune cut) and within the limit.
	emoji := "😀" // 4-byte rune
	multi := strings.Repeat(emoji, maxEnvValueLen/4+2000)
	gotMulti := sanitizeForShell(multi)
	if len(gotMulti) > maxEnvValueLen {
		t.Errorf("overlong utf8 len %d > %d", len(gotMulti), maxEnvValueLen)
	}
	if !utf8.ValidString(gotMulti) {
		t.Errorf("overlong utf8 produced invalid UTF-8 (mid-rune truncation): %x", gotMulti)
	}

	// Overlong value whose final 10-byte window is all invalid UTF-8: no
	// RuneStart exists in the window, so the boundary loop cannot truncate —
	// the fallback hard-truncation must still cap the length (invalid bytes
	// are then dropped by the rune-validation pass, so the result only shrinks).
	//
	// 超长值且末尾 10 字节窗口全是非法 UTF-8：窗口内找不到 RuneStart，边界
	// 循环截不断——兜底硬截断必须把长度压住（非法字节随后被 rune 校验剔除，
	// 结果只会更短）。
	tricky := strings.Repeat("a", maxEnvValueLen-5) + strings.Repeat("\xff", 100)
	gotTricky := sanitizeForShell(tricky)
	if len(gotTricky) > maxEnvValueLen {
		t.Errorf("overlong invalid-utf8-tail len %d > %d (fallback truncation missing)", len(gotTricky), maxEnvValueLen)
	}
}

// TestIsGlobalHook is the truth table for the global-hook gate in runHook.
// skill-scan (scans $HOME/.claude/skills) and init-suggest (detects forge-candidate
// projects from cwd) are global — both relevant in any project, so they run even when
// findProjectRoot fails (non-forge project); every project-scoped hook returns false
// and keeps the allow-and-exit behavior.
func TestIsGlobalHook(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"skill-scan", true},
		{"init-suggest", true},
		{"mcp-scan", true},
		{"auto-compile", false},
		{"task-guard", false},
		{"file-sentinel", false},
		{"bash-guard", false},
		{"", false},
		{"some-future-hook", false},
	}
	for _, c := range cases {
		if got := isGlobalHook(c.name); got != c.want {
			t.Errorf("isGlobalHook(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// chdirToNonForgeRoot chdir to a directory guaranteed to be outside any forge
// project, so findProjectRoot fails — needed to exercise the global-hook branch
// in runHook (where a non-forge project normally triggers allow-and-exit).
// t.TempDir() is NOT sufficient: it lives under the user HOME, which may itself
// contain a .forge/ dir (e.g. C:\Users\<user>\.forge from a global forge
// install), making findProjectRoot succeed up the tree (observed: tempdir under
// C:\Users\Administrator\AppData\Local\Temp resolved root to C:\Users\Administrator).
// The filesystem root (Windows drive root C:\, Unix /) has no parent, so
// findProjectRoot terminates there with an error. Skips the test if chdir fails
// or if even the root resolves to a forge project.
func chdirToNonForgeRoot(t *testing.T) func() {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := string(filepath.Separator)
	if vol := filepath.VolumeName(cwd); vol != "" {
		root = vol + string(filepath.Separator)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Skipf("cannot chdir to %s to simulate non-forge project: %v", root, err)
	}
	if _, ferr := findProjectRoot(); ferr == nil {
		os.Chdir(orig)
		t.Skipf("volume root %s still resolves to a forge project; cannot isolate non-forge scenario", root)
	}
	return func() { os.Chdir(orig) }
}

// TestHookOutput_GlobalHookRunsOutsideProject guards the global-hook path in
// runHook: skill-scan scans $HOME/.claude/skills (project-independent), so it
// MUST NOT be silently skipped by the non-forge-project allow-and-exit. In a
// non-forge project with no ~/.claude/skills it still runs and emits PASS
// advisory detail, whereas a project-scoped hook just allows silently. Pre-fix,
// skill-scan never fired outside a forge project — defeating its purpose
// (catch skills that entered outside the install gate, which is exactly the
// non-forge-project / global case).
func TestHookOutput_GlobalHookRunsOutsideProject(t *testing.T) {
	restore := chdirToNonForgeRoot(t) // findProjectRoot fails → exercises isGlobalHook branch
	defer restore()
	// No ~/.claude/skills under this HOME → skill-scan takes the "no skills" PASS
	// branch (does not depend on forge being in PATH).
	t.Setenv("HOME", t.TempDir())

	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"SessionStart","tool_name":"","tool_input":{}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runHook(nil, []string{"skill-scan"})

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("skill-scan outside forge project should pass, got err: %v", err)
	}
	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))

	var result HookOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output not valid JSON: %q, err: %v", output, err)
	}
	// The allow-with-detail shape is a BARE hookSpecificOutput — decision must be
	// empty (decision:"approve" bypasses Claude permissions / fails the hook on codex).
	if result.Decision != "" {
		t.Errorf("decision = %q, want \"\" (bare hookSpecificOutput on allow)", result.Decision)
	}
	// The hook ran (not silently allowed): advisory PASS detail is present.
	if result.HookSpecificOutput == nil || !strings.Contains(result.HookSpecificOutput.AdditionalContext, "skill-scan") {
		t.Errorf("skill-scan advisory detail missing outside forge project (hook was silently skipped), got: %+v", result.HookSpecificOutput)
	}
}

// TestHookOutput_ProjectScopedHookStillSkipsOutsideProject guards the inverse:
// a project-scoped hook (auto-compile) outside a forge project MUST still
// allow-and-exit silently (no AdditionalContext). The global-hook carve-out
// must not leak to other hooks.
func TestHookOutput_ProjectScopedHookStillSkipsOutsideProject(t *testing.T) {
	restore := chdirToNonForgeRoot(t) // findProjectRoot fails → project-scoped hook must allow-and-exit
	defer restore()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout
	if err != nil {
		t.Fatalf("auto-compile outside project should allow, got err: %v", err)
	}
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))

	// Silent allow on every host: exit 0 with NO stdout at all.
	if output != "" {
		t.Errorf("project-scoped hook outside forge project must allow silently, got stdout: %q", output)
	}
}

// TestReadsFilePath_DeterministicAndFilenameSafe pins the reads-log path contract of scheme 2:
// resolving the same session id twice must yield the same path (tool-track append and read-before-edit
// grep each call readsFilePath in different subprocesses; divergence would make the hook miss forever), and the path
// must contain only filename-safe characters ([A-Za-z0-9._-]) — session ids may contain path separators/spaces and must be
// collapsed to prevent escaping $TMPDIR or creating unexpected directories.
//
// TestReadsFilePath_DeterministicAndFilenameSafe 钉住方案2 的 reads-log 路径契约：
// 同一 session id 两次解析必须产出同一路径（tool-track append 与 read-before-edit
// grep 在不同子进程里各自调用 readsFilePath，不一致会让 hook 永远 miss），且路径
// 只含文件名安全字符（[A-Za-z0-9._-]）——session id 可能含路径分隔符/空格，必须被
// 折叠以免逃逸 $TMPDIR 或创建意外目录。
func TestReadsFilePath_DeterministicAndFilenameSafe(t *testing.T) {
	a := readsFilePath("/repo/alpha", "sess-abc-123")
	b := readsFilePath("/repo/alpha", "sess-abc-123")
	if a != b {
		t.Fatalf("readsFilePath not deterministic: %q vs %q", a, b)
	}
	// Hostile / unusual session ids must collapse to filename-safe tokens.
	for _, sid := range []string{"../etc/passwd", "a b/c", "with space", ""} {
		p := readsFilePath("/repo/alpha", sid)
		base := filepath.Base(p)
		for _, r := range base {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
				r == '.' || r == '_' || r == '-') {
				t.Errorf("readsFilePath(%q) produced non-safe basename %q (rune %q)", sid, base, r)
			}
		}
	}
	// Project scoping: same session id under two different roots must resolve to
	// different reads logs — $TMPDIR is shared, so without the project tag a reused/
	// short session id (e.g. test "sid-*") would let project B read project A's reads
	// log and the read-before-edit hook would false-pass an Edit-without-Read.
	alpha := readsFilePath("/repo/alpha", "sid-x")
	beta := readsFilePath("/repo/beta", "sid-x")
	if alpha == beta {
		t.Errorf("readsFilePath must scope per-project: alpha==beta for same sid under different roots (%q)", alpha)
	}
	// Empty session id falls back to a stable default, not an empty token.
	if got := readsFileKey(""); got != "default" {
		t.Errorf("readsFileKey(\"\") = %q, want \"default\"", got)
	}
}

// TestAppendSessionRead_RecordsAndMatches pins the side-channel write/read of scheme 2: after appending
// a repo-relative path, the file contains that path on its own line; the read-before-edit grep -qxF semantics
// is line-exact match — so the appended content must be a single line with no extra whitespace.
//
// TestAppendSessionRead_RecordsAndMatches 钉住方案2 的 side-channel 写/读：append
// 一个 repo-relative 路径后，文件按行含该路径；read-before-edit 的 grep -qxF 语义
// 即"整行精确匹配"——所以追加内容必须是单行无额外空白。
func TestAppendSessionRead_RecordsAndMatches(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "reads.log")
	appendSessionRead(logPath, "internal/cli/hook.go")
	appendSessionRead(logPath, "internal/hooks/embed.go")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reads log not created: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	want := map[string]bool{"internal/cli/hook.go": true, "internal/hooks/embed.go": true}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(data))
	}
	for _, ln := range lines {
		if !want[ln] {
			t.Errorf("unexpected line %q (want exact path, no whitespace)", ln)
		}
	}
}

// TestHookToolTrackRecordsSkillInput pins scheme C: the tool-track hook (matcher Read|Skill|Agent|Grep|Glob)
// records tool_input (skill name) for Skill calls, so toollog audits can see which quality skill the agent loaded.
// Read still omits tool_input (frequent; gate only needs tool_name+timestamp); Skill/Agent fill tool_input
// so whether quality skills were driven becomes traceable (root cause of zero quality-skill fires in advisory context is traceable).
//
// TestHookToolTrackRecordsSkillInput 钉死方案 C：tool-track hook（matcher Read|Skill|Agent|Grep|Glob）
// 对 Skill 调用记录 tool_input（skill 名），让 toollog 审计能看到 agent 加载了哪个质量技能。
// Read 仍省略 tool_input（频繁，gate 只需 tool_name+timestamp）；Skill/Agent 填 tool_input
// 让"质量 skill 是否被驱动"可追溯（advisory 语境下质量 skill 0 触发的根因可追溯）。
func TestHookToolTrackRecordsSkillInput(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".forge", "hooks"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"PostToolUse","tool_name":"Skill","tool_input":{"name":"test-discipline"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runHook(nil, []string{"tool-track"})

	w.Close()
	os.Stdout = oldStdout
	r.Read(make([]byte, 8192))

	// toollog is written to the user-level DataDir (forgedata.DataDirFor), same
	// path convention as checklog — never the project tree.
	//
	// toollog 写到用户级 DataDir（forgedata.DataDirFor），同 checklog 路径惯例——
	// 绝不写项目树。
	toollogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "toollog.jsonl")
	data, err := os.ReadFile(toollogPath)
	if err != nil {
		t.Fatalf("toollog.jsonl 未生成（Skill 调用未被 tool-track 记录——matcher 或 dispatch 缺失）: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"tool_name":"Skill"`) {
		t.Errorf("toollog 应含 tool_name=Skill 条目, got: %s", body)
	}
	if !strings.Contains(body, "test-discipline") {
		t.Errorf("toollog 应含 skill 名 test-discipline（方案 C：Skill tool_input 须记录）, got: %s", body)
	}
}

// TestHookToolTrackRecordsReadFilePath pins the production shape of Read tool_input
// (2026-08-16 review HIGH-1): tool-track records a minimal {"file_path":...} so the funnel
// join (skillseval.BuildTriggerFunnel → readFilePath) can attribute "loaded the skill after
// the trigger hit". Before the fix Read omitted tool_input entirely, making that join
// structurally dead on production data while funnel unit tests stayed green on hand-marshaled
// inputs. This test is the production-side half of the shape contract; funnel_test.go's mkRead
// is the join-side half — they must not diverge again.
//
// TestHookToolTrackRecordsReadFilePath 钉死 Read tool_input 的生产形状（2026-08-16 审查
// HIGH-1）：tool-track 记最小 {"file_path":...}，让漏斗 join（skillseval.BuildTriggerFunnel
// → readFilePath）能归因「命中后加载了该 skill」。修复前 Read 完全省略 tool_input，该
// join 在生产数据上结构性死亡，而漏斗单测用手工 marshal 的输入照样全绿。本测试是形状
// 契约的生产侧一半；funnel_test.go 的 mkRead 是 join 侧一半——两者不得再分叉。
func TestHookToolTrackRecordsReadFilePath(t *testing.T) {
	cases := []struct {
		name   string
		stdin  string
		assert func(t *testing.T, body string)
	}{
		{
			// 最小形状 + 最小性：原始 input 带 limit，落盘不得含——写入方回归成记完整
			// input 时此臂变红（复审 LOW(i)）。
			//
			// Minimal shape + minimality: the raw input carries limit, which must NOT
			// land — this arm goes red if the writer regresses to recording the full
			// input (re-review LOW(i)).
			name:  "minimal shape",
			stdin: `{"hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"file_path":"src/main.go","limit":50}}`,
			assert: func(t *testing.T, body string) {
				// tool_input 在 JSONL 里是转义过的内嵌 JSON（\"file_path\":\"...\"），
				// 断言按裸 token 查——字段名与路径值都在即覆盖最小形状语义。
				//
				// tool_input is an escaped embedded JSON inside JSONL
				// (\"file_path\":\"...\"), so assert on bare tokens — both the field
				// name and the path value present covers the minimal-shape semantics.
				if !strings.Contains(body, "file_path") || !strings.Contains(body, "src/main.go") {
					t.Errorf("Read 的 tool_input 须记最小 {\"file_path\":...}（漏斗 join 依赖，审查 HIGH-1）, got: %s", body)
				}
				if strings.Contains(body, "limit") {
					t.Errorf("最小形状契约：limit 等其余字段不得落盘（lean 契约，复审 LOW(i)）, got: %s", body)
				}
			},
		},
		{
			// 零回归臂：input 无 file_path（旧 host / 解析失败形状）→ 条目照旧无
			// tool_input（omitempty 整键缺席），与修复前逐字节等价（复审 LOW(ii)）。
			//
			// Zero-regression arm: input without file_path (legacy hosts / parse-failure
			// shape) → the entry lands WITHOUT tool_input as before (omitempty drops the
			// whole key), byte-identical to pre-fix behavior (re-review LOW(ii)).
			name:  "no file_path stays lean",
			stdin: `{"hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"offset":10}}`,
			assert: func(t *testing.T, body string) {
				if !strings.Contains(body, `"tool_name":"Read"`) {
					t.Errorf("toollog 应含 tool_name=Read 条目, got: %s", body)
				}
				if strings.Contains(body, "tool_input") || strings.Contains(body, "offset") {
					t.Errorf("无 file_path 的 Read 应照旧省略整个 tool_input 键（零回归）, got: %s", body)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FORGE_DATA_HOME", t.TempDir())
			tmpDir := t.TempDir()
			os.MkdirAll(filepath.Join(tmpDir, ".forge", "hooks"), 0755)
			os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

			originalWd, _ := os.Getwd()
			os.Chdir(tmpDir)
			defer os.Chdir(originalWd)

			oldStdin := os.Stdin
			tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
			tmpStdin.WriteString(tc.stdin)
			tmpStdin.Seek(0, 0)
			os.Stdin = tmpStdin
			defer func() {
				os.Stdin = oldStdin
				tmpStdin.Close()
				os.Remove(tmpStdin.Name())
			}()

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			runHook(nil, []string{"tool-track"})

			w.Close()
			os.Stdout = oldStdout
			r.Read(make([]byte, 8192))

			toollogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "toollog.jsonl")
			data, err := os.ReadFile(toollogPath)
			if err != nil {
				t.Fatalf("toollog.jsonl 未生成: %v", err)
			}
			tc.assert(t, string(data))
		})
	}
}

// TestHookToolTrackRecordsGrepInput pins the production shape of Grep/Glob
// tool_input (2026-08-23 drift fix): like Bash/Skill/Agent, exploration calls
// record the full tool input truncated — the pattern and path are the audit
// payload (which regex, which tree). Read stays minimal-shape (funnel join);
// Grep/Glob do not join any funnel, so the lean contract does not apply. The
// row itself is what ExploreCounts counts — no input would still count, but an
// input-less exploration log is worthless for behavior/hazard audits.
//
// TestHookToolTrackRecordsGrepInput 钉死 Grep/Glob tool_input 的生产形状
// （2026-08-23 漂移修复）：与 Bash/Skill/Agent 同待遇记完整 input 截断——
// pattern 与 path 就是审计载荷（查了什么正则、扫了哪棵树）。Read 保持最小
// 形状（漏斗 join）；Grep/Glob 不进任何漏斗，lean 契约不适用。条目本身即
// ExploreCounts 所数——没有 input 也照样计数，但无 input 的探索日志对
// 行为/风险审计毫无价值。
func TestHookToolTrackRecordsGrepInput(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".forge", "hooks"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	stdin := `{"hook_event_name":"PostToolUse","tool_name":"Grep","tool_input":{"pattern":"DSH_HOME","path":"internal/"}}`
	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(stdin)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runHook(nil, []string{"tool-track"})

	w.Close()
	os.Stdout = oldStdout
	r.Read(make([]byte, 8192))

	toollogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "toollog.jsonl")
	data, err := os.ReadFile(toollogPath)
	if err != nil {
		t.Fatalf("toollog.jsonl 未生成: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"tool_name":"Grep"`) {
		t.Fatalf("toollog 应含 tool_name=Grep 条目（matcher 补 Grep/Glob 的记录面）, got: %s", body)
	}
	if !strings.Contains(body, "DSH_HOME") || !strings.Contains(body, "internal/") {
		t.Errorf("Grep 的 tool_input 须记 pattern+path（审计载荷，与 Bash/Skill/Agent 同待遇）, got: %s", body)
	}
}

// TestHookStampsResolvedAgentOnSessionRecord is the end-to-end wiring test for the
// marker-absent attribution fix: a session created with NO project marker (empty
// agent_type — the kimi/reasonix/codex-without-marker case) gets its authoritative
// agent stamped by the first hook invocation carrying --agent/FORGE_HOOK_AGENT. This is
// the only path that attributes marker-absent agents correctly; without it such sessions
// misattribute to claude-code (the leaked CLAUDE_CODE_SESSION_ID default). The stamp
// fires right after root resolution, before any hook-specific logic, so it is robust to
// what the rest of the hook does.
//
// TestHookStampsResolvedAgentOnSessionRecord 是无标记归因修复的端到端接线测试：无项目
// 标记创建的 session（空 agent_type——无标记的 kimi/reasonix/codex 场景）被首个携带
// --agent/FORGE_HOOK_AGENT 的 hook 调用盖上权威 agent。这是唯一能正确归因无标记 agent
// 的路径；缺它这类 session 会误归 claude-code（泄漏的 CLAUDE_CODE_SESSION_ID 默认值）。
// 盖戳在项目根解析后立即触发、先于任何 hook 专属逻辑，故对 hook 其余部分做什么鲁棒。
func TestHookStampsResolvedAgentOnSessionRecord(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".forge", "hooks"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)
	// Deliberately NO project marker (.reasonix etc.) — detectAgentType is empty at
	// creation. This is exactly the case the stamp must recover.
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Pre-create the scoped session (as the SessionStart/resume hook would in prod).
	// No marker → AgentType empty (the precondition the stamp exists to fix).
	const sid = `stamp-wiring-sid`
	sess, err := taskpipeline.EnsureSession(tmpDir, sid)
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if sess.AgentType != "" {
		t.Fatalf("precondition: AgentType=%q, want empty (no project marker present)", sess.AgentType)
	}

	// Resolve agent via the --agent flag path (the same path translators set).
	oldAgent := hookAgent
	hookAgent = `reasonix`
	defer func() { hookAgent = oldAgent }()

	// Stdin carries the session id, as every host's hook payload does.
	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"PreToolUse","session_id":"` + sid + `","tool_name":"Write","tool_input":{"file_path":"src/main.go"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	// Drain stdout (the hook emits a decision JSON we do not assert here).
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	runHook(nil, []string{"tool-track"})
	w.Close()
	os.Stdout = oldStdout
	r.Read(make([]byte, 8192))

	// Reload the scoped record — it must now carry the stamped agent.
	reloaded, err := taskpipeline.EnsureSession(tmpDir, sid)
	if err != nil {
		t.Fatalf("reload EnsureSession: %v", err)
	}
	if reloaded.AgentType != `reasonix` {
		t.Errorf("AgentType after hook = %q, want reasonix (hook must stamp resolved agent onto marker-absent session)", reloaded.AgentType)
	}
}

// TestScoringPassUnchanged pins the state-change gating of scoring PASS
// records (weekly-hardening 4b): a repeat PASS of a scoring check is skipped
// (scoring's LatestByCheck still resolves to the earlier PASS — no regression),
// while the first PASS, a FAIL→PASS transition, non-scoring checks, and a PASS
// last seen in a DIFFERENT session are all still recorded.
//
// TestScoringPassUnchanged 钉死 scoring PASS 记录的状态变化门控（周复盘加固
// 4b）：scoring check 的重复 PASS 被跳过（scoring 的 LatestByCheck 仍解析到
// 更早的 PASS——不回归），而首个 PASS、FAIL→PASS 转换、非 scoring check、
// 上次 PASS 在其他 session 的情形都仍记录。
func TestScoringPassUnchanged(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())

	// No prior entry → record (the first PASS must land or scoring sees nothing).
	//
	// 无先前条目 → 记录（首个 PASS 必须落盘，否则 scoring 看不到）。
	if scoringPassUnchanged(dir, "s1", checklog.CheckAutoCompile) {
		t.Error("no prior entry → must record the first PASS")
	}
	// Non-scoring check → never consulted for dedup (its PASS is already dropped
	// by shouldRecordCheck; the helper must not change that).
	//
	// 非 scoring check → 不参与去重（它的 PASS 已被 shouldRecordCheck 丢弃，
	// helper 不得改变这一点）。
	if scoringPassUnchanged(dir, "s1", checklog.CheckBashGuard) {
		t.Error("non-scoring check must return false")
	}

	// Latest is FAIL → the FAIL→PASS transition is a state change, must record.
	//
	// 最新是 FAIL → FAIL→PASS 转换是状态变化，必须记录。
	if err := checklog.Record(dir, &checklog.Entry{Check: checklog.CheckAutoCompile, Passed: false, SessionID: "s1", Detail: "broke"}); err != nil {
		t.Fatal(err)
	}
	if scoringPassUnchanged(dir, "s1", checklog.CheckAutoCompile) {
		t.Error("latest FAIL → PASS transition must be recorded")
	}

	// Latest is PASS → repeat PASS is skipped.
	//
	// 最新是 PASS → 重复 PASS 跳过。
	if err := checklog.Record(dir, &checklog.Entry{Check: checklog.CheckAutoCompile, Passed: true, SessionID: "s1", Detail: "ok"}); err != nil {
		t.Fatal(err)
	}
	if !scoringPassUnchanged(dir, "s1", checklog.CheckAutoCompile) {
		t.Error("latest PASS → repeat PASS must be skipped")
	}
	// ...but only within the same session scope: another session's first PASS is
	// still written (accepted cost: one entry per session per check).
	//
	// ……但只在同一 session 范围内：其他 session 的首个 PASS 仍写（可接受成本：
	// 每 session 每 check 一条）。
	if scoringPassUnchanged(dir, "s2", checklog.CheckAutoCompile) {
		t.Error("previous PASS belongs to another session → this session's first PASS must record")
	}
}

// TestSanitizeSessionIDConverges pins the safety property behind runHook's
// entry-point sanitize (fix/cleanup-batch, 2026-08-29): hookInput.SessionID is
// sanitized ONCE at the entry, while downstream sites keep their own
// util.SanitizeSessionID calls (defense in depth) — that double application is
// safe because the function CONVERGES: sanitize(sanitize(x)) == sanitize applied
// again, for every input. Realistic session ids (UUIDs with hyphens, hex, dots,
// >64 chars) are straight-up idempotent; the only divergence is the pathological
// truncation edge (a >64-char id whose 64th char is a separator: truncation can
// leave a trailing '-'/'_' that a SECOND application trims — see the ordering of
// trim-then-truncate in util.SanitizeSessionID), and even there the third
// application is a no-op. Convergence (not naive idempotence) is exactly what
// makes "entry sanitize + downstream sanitize" produce one uniform key
// everywhere, whatever the host sends.
//
// TestSanitizeSessionIDConverges 钉住 runHook 入口归一（fix/cleanup-batch，
// 2026-08-29）背后的安全性质：hookInput.SessionID 在入口归一一次，下游各点保留
// 自己的 util.SanitizeSessionID 调用（纵深防御）——重复应用之所以安全，是因为
// 该函数【收敛】：sanitize(sanitize(x)) 再应用一次不变，对任何输入成立。现实
// session id（带连字符的 UUID、hex、点号、超 64 字符）完全幂等；唯一发散点是
// 病态的截断边界（>64 字符且第 64 位恰为分隔符：截断会留下尾部 '-'/'_'，第二次
// 应用会把它修剪掉——见 util.SanitizeSessionID 里 trim 先于 truncate 的顺序），
// 即便如此第三次应用也是 no-op。收敛性（而非朴素幂等）正是「入口归一 + 下游
// 归一」处处得到同一 key 的保证，无论宿主发什么。
func TestSanitizeSessionIDConverges(t *testing.T) {
	// Realistic corpus: straight-up idempotent (sanitize^2 == sanitize^1).
	//
	// 现实语料：完全幂等（sanitize² == sanitize¹）。
	realistic := []string{
		"3f0c1c9e-8b1a-4c2d-9e0f-1a2b3c4d5e6f", // Claude/kimi UUID（连字符合法字符）
		"a1b2c3d4e5f6789012345678",             // cursor/codex hex
		"session.with.dots.123",                // 点号被替换——归一后稳定
		strings.Repeat("a", 100),               // 纯字母超长：截断到 64，尾字符非分隔符
		"  <script>; rm -rf /  ",               // 攻击形态
	}
	for _, id := range realistic {
		once := util.SanitizeSessionID(id)
		twice := util.SanitizeSessionID(once)
		if once != twice {
			t.Errorf("SanitizeSessionID not idempotent for %q: %q != %q", id, once, twice)
		}
	}

	// Universal convergence: sanitize^2 == sanitize^3 for ANY input — including
	// the 64-boundary separator edge (the one input class where sanitize^1 !=
	// sanitize^2: truncation leaves a trailing separator the second pass trims).
	// This is the property the entry-point unification actually relies on: entry
	// sanitize + any number of downstream sanitizes land on the same key.
	//
	// 普适收敛：对任何输入 sanitize² == sanitize³——含 64 边界分隔符边缘（唯一一类
	// sanitize¹ != sanitize² 的输入：截断留下尾部分隔符、第二遍把它修剪）。入口
	// 统一真正依赖的正是这条：入口归一 + 任意次下游归一落在同一 key 上。
	edge := strings.Repeat("a", 63) + "-" + strings.Repeat("b", 10) // 第 64 位是 '-'：截断后尾随分隔符
	corpus := append(realistic, edge, strings.Repeat("-", 70), "")
	for _, id := range corpus {
		twice := util.SanitizeSessionID(util.SanitizeSessionID(id))
		thrice := util.SanitizeSessionID(twice)
		if twice != thrice {
			t.Errorf("SanitizeSessionID does not converge for %q: %q != %q", id, twice, thrice)
		}
	}

	// The edge case demonstrated: sanitize^1 ends with the separator cut in at
	// position 64, sanitize^2 trims it, sanitize^3 is stable. If this ever
	// becomes plain idempotent (trim moved after truncate in util), the test
	// above still passes — convergence is the contract, not the divergence.
	//
	// 边缘形态演示：sanitize¹ 以截断进来的第 64 位分隔符结尾，sanitize² 修剪它，
	// sanitize³ 稳定。若 util 把 trim 挪到 truncate 之后、此边缘变成完全幂等，
	// 上面的测试依然通过——契约是收敛，不是发散本身。
	if s1 := util.SanitizeSessionID(edge); len(s1) != 64 || s1[len(s1)-1] != '-' {
		t.Fatalf("fixture assumption broken: edge input should sanitize to a 64-char string ending in '-', got %q", s1)
	}
}

// TestRunHookSanitizesSessionIDAtEntry pins the entry-point unification end to
// end (fix/cleanup-batch, 2026-08-29): runHook must sanitize hookInput.SessionID
// right after parsing, so the RAW-id record sites it feeds — hook_track.go's
// observation hooks record hookInput.SessionID verbatim — write the sanitized
// form, matching the sanitized keys of session-scoped readers
// (checklog.LatestByCheckForSession* compares exact strings). A dotted host id
// is the canonical divergence case (dots are not in the safe charset).
//
// TestRunHookSanitizesSessionIDAtEntry 端到端钉住入口统一（fix/cleanup-batch，
// 2026-08-29）：runHook 必须在解析后立即归一 hookInput.SessionID，使它喂给的
// 原始值记录点——hook_track.go 的观察 hook 原样记录 hookInput.SessionID——写下的
// 是归一形态，与会话级读方的归一 key（checklog.LatestByCheckForSession* 精确串
// 比较）一致。带点号的宿主 id 是最典型的发散形态（点号不在安全字符集内）。
func TestRunHookSanitizesSessionIDAtEntry(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	// Minimal forge project so findProjectRoot resolves (project-scoped dispatch).
	//
	// 最小 forge 项目，让 findProjectRoot 解析成功（项目级分发）。
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".forge", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644); err != nil {
		t.Fatal(err)
	}
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// PostToolUseFailure with a dotted session id — the failure-track hook records
	// CheckToolFailure with hookInput.SessionID verbatim (pre-fix: raw id).
	//
	// 带点号 session id 的 PostToolUseFailure——failure-track hook 原样记录
	// hookInput.SessionID 到 CheckToolFailure（修复前：原始 id）。
	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"PostToolUseFailure","session_id":"sess.with.dots","tool_name":"Bash","error_message":"a network hiccup"}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	// Capture stdout: failure-track with a non-compile error emits nothing; the
	// contract under test is the recorded entry, not the emission. cmd must be a
	// real (bare) cobra command — the failure-track dispatch reads cmd.Root().Version.
	//
	// 捕获 stdout：非编译类错误的 failure-track 不发输出；被测契约是记录的
	// 条目，不是发射。cmd 必须是真实的（裸）cobra 命令——failure-track 分发会读
	// cmd.Root().Version。
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	_ = runHook(&cobra.Command{}, []string{"failure-track"})
	w.Close()
	os.Stdout = oldStdout
	buf := make([]byte, 4096)
	_, _ = r.Read(buf)

	entries, err := checklog.LoadAll(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	var sawToolFailure bool
	for _, e := range entries {
		if e.Check != checklog.CheckToolFailure {
			continue
		}
		sawToolFailure = true
		if e.SessionID != "sess_with_dots" {
			t.Errorf("CheckToolFailure SessionID = %q, want %q（入口归一后，点号被替换；原始值不得入盘）", e.SessionID, "sess_with_dots")
		}
	}
	if !sawToolFailure {
		t.Fatal("no CheckToolFailure entry recorded — dispatch did not reach failure-track")
	}
}

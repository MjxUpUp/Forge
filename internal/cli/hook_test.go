package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

func TestHookOutput_AllowOnMissingProject(t *testing.T) {
	// Run in a temp dir without .forge/ — should output allow JSON
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

	// Should be valid JSON with decision: allow
	var result HookOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %q, err: %v", output, err)
	}
	if result.Decision != "approve" {
		t.Errorf("decision = %q, want %q", result.Decision, "approve")
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

	// May error if go build fails — that's OK, we just check the JSON output
	_ = err

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))

	if output == "" {
		t.Fatal("no output from hook")
	}

	// Must be valid JSON
	var result HookOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %q, err: %v", output, err)
	}

	// Decision must be "approve" or "block"
	if result.Decision != "approve" && result.Decision != "block" {
		t.Errorf("decision = %q, want 'approve' or 'block'", result.Decision)
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

	// Check that checklog.jsonl was created
	checklogPath := filepath.Join(tmpDir, ".forge", "checklog.jsonl")
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

	// auto-compile is scoring-dependent → its PASS MUST be recorded.
	checklogPath := filepath.Join(tmpDir, ".forge", "checklog.jsonl")
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
	if result.Decision != "approve" {
		t.Errorf("decision = %q, want approve", result.Decision)
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

	var result HookOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output not valid JSON: %q, err: %v", output, err)
	}
	if result.Decision != "approve" {
		t.Errorf("decision = %q, want approve", result.Decision)
	}
	if result.HookSpecificOutput != nil {
		t.Errorf("project-scoped hook outside forge project must allow silently (no AdditionalContext), got: %+v", result.HookSpecificOutput)
	}
}

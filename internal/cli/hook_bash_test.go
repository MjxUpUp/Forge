package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestIsWSLBash pins the WSL-launcher recognition: System32/SysWOW64/WindowsApps bash
// are WSL (cannot see Windows temp paths), Git Bash/MSYS2/Cygwin are real Windows-native
// bash. Case-insensitive (PATH entries vary in case).
func TestIsWSLBash(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`C:\Windows\System32\bash.exe`, true},
		{`c:\windows\system32\bash.exe`, true},
		{`C:\Windows\SysWOW64\bash.exe`, true},
		{`C:\Users\Administrator\AppData\Local\Microsoft\WindowsApps\bash.exe`, true},
		{`C:/Windows/System32/bash.exe`, true},
		{`D:\Program Files\Git\usr\bin\bash.exe`, false},
		{`D:\Program Files\Git\bin\bash.exe`, false},
		{`C:\msys64\usr\bin\bash.exe`, false},
		{`/usr/bin/bash`, false},
	}
	for _, c := range cases {
		if got := isWSLBash(c.path); got != c.want {
			t.Errorf("isWSLBash(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestFindBash_SkipsWSL pins the root-cause fix: with a WSL launcher earlier in PATH
// than a real Git Bash, findBash must pick the Git Bash. The fake candidates only need
// to exist on disk — findBash stats, never executes.
func TestFindBash_SkipsWSL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("findBash filtering is Windows-only")
	}
	tmp := t.TempDir()
	wslDir := filepath.Join(tmp, "Windows", "System32")
	gitDir := filepath.Join(tmp, "Git", "usr", "bin")
	for _, dir := range []string{wslDir, gitDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bash.exe"), []byte("fake"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", wslDir+string(os.PathListSeparator)+gitDir)

	got, err := findBash()
	if err != nil {
		t.Fatalf("findBash: %v", err)
	}
	want := filepath.Join(gitDir, "bash.exe")
	if got != want {
		t.Errorf("findBash = %q, want %q (WSL launcher must be skipped)", got, want)
	}
}

// exitErrorOf runs a trivial command that exits with the given code, producing a real
// *exec.ExitError for isHookInfraFailure tests.
func exitErrorOf(t *testing.T, code int) error {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit", strconv.Itoa(code))
	} else {
		cmd = exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
	}
	err := cmd.Run()
	if err == nil {
		t.Fatalf("command with exit %d returned nil error", code)
	}
	return err
}

// TestIsHookInfraFailure pins the fail-open boundary: spawn errors and bash 126/127 are
// infrastructure (fail open); script exit 1 (gate FAIL) and exit 2 (review-stop) remain
// gate verdicts.
func TestIsHookInfraFailure(t *testing.T) {
	if isHookInfraFailure(nil) {
		t.Error("nil error is not an infra failure")
	}
	if !isHookInfraFailure(exec.ErrNotFound) {
		t.Error("spawn error (non-ExitError) must be infra failure")
	}
	for _, code := range []int{126, 127} {
		if !isHookInfraFailure(exitErrorOf(t, code)) {
			t.Errorf("exit %d must be infra failure", code)
		}
	}
	for _, code := range []int{1, 2} {
		if isHookInfraFailure(exitErrorOf(t, code)) {
			t.Errorf("exit %d (script gate verdict) must NOT be infra failure", code)
		}
	}
}

// TestEmitInfraAllow pins the fail-open output contract: kimi gets plain stdout text +
// nil error (exit 0); claude gets an approve JSON envelope whose hookSpecificOutput
// carries the event name — without it Claude drops additionalContext and the warning
// would be silently lost.
func TestEmitInfraAllow(t *testing.T) {
	stdout, _, err := captureOutput(t, func() error {
		return emitInfraAllow("kimi", "PreToolUse", "[forge] hook x 基础设施失败，fail-open 放行")
	})
	if err != nil {
		t.Fatalf("kimi infra allow must return nil (exit 0), got %v", err)
	}
	if !strings.Contains(stdout, "基础设施失败") {
		t.Errorf("kimi warning must go to stdout, got %q", stdout)
	}

	stdout, _, err = captureOutput(t, func() error {
		return emitInfraAllow("", "PreToolUse", "warn")
	})
	if err != nil {
		t.Fatalf("claude infra allow must return nil, got %v", err)
	}
	var out HookOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("claude path must emit valid HookOutput JSON: %v\n%s", err, stdout)
	}
	if out.Decision != "approve" {
		t.Errorf("decision = %q, want approve", out.Decision)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookSpecificOutput.hookEventName missing/wrong: %+v", out.HookSpecificOutput)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.AdditionalContext != "warn" {
		t.Errorf("additionalContext missing: %+v", out.HookSpecificOutput)
	}
}

package shellexec

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// WSL-launcher recognition + resolution-order tests. These moved from
// internal/cli/hook_bash_test.go together with the implementation (FindBash /
// isWSLBash) when the logic was shared with the gate path
// (taskpipeline.runEmbeddedHook) — same assertions, same strictness, relocated.

// TestIsWSLBash pins the WSL-launcher recognition: System32/SysWOW64/WindowsApps bash
// are WSL (cannot see Windows temp paths), Git Bash/MSYS2/Cygwin are real Windows-native
// bash. Case-insensitive (PATH entries vary in case) and separator-agnostic — the
// classified path is always a WINDOWS path conceptually, so mixed separators must work
// on any host OS (regression: v1.19.1 CI on Linux failed this test because
// filepath.ToSlash is a no-op there and backslashes survived).
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
		{`C:\Windows/SYSWOW64\bash.exe`, true}, // 混合分隔符（CI Linux 回归钉）
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
// than a real Git Bash, FindBash must pick the Git Bash. The fake candidates only need
// to exist on disk — FindBash stats, never executes.
func TestFindBash_SkipsWSL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("FindBash filtering is Windows-only")
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

	got, err := FindBash()
	if err != nil {
		t.Fatalf("FindBash: %v", err)
	}
	want := filepath.Join(gitDir, "bash.exe")
	if got != want {
		t.Errorf("FindBash = %q, want %q (WSL launcher must be skipped)", got, want)
	}
}

// TestFindBash_GitDerived pins the native-Windows-PATH case: Git for Windows puts only
// Git\cmd on PATH (no bash.exe there) — FindBash must walk from the git binary to the
// MSYS layout (Git\usr\bin\bash.exe) instead of falling back to the WSL launcher.
func TestFindBash_GitDerived(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("FindBash filtering is Windows-only")
	}
	tmp := t.TempDir()
	wslDir := filepath.Join(tmp, "Windows", "System32")
	gitCmdDir := filepath.Join(tmp, "Git", "cmd")
	gitBashDir := filepath.Join(tmp, "Git", "usr", "bin")
	for _, dir := range []string{wslDir, gitCmdDir, gitBashDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct{ dir, name string }{
		{wslDir, "bash.exe"},
		{gitCmdDir, "git.exe"},
		{gitBashDir, "bash.exe"},
	} {
		if err := os.WriteFile(filepath.Join(f.dir, f.name), []byte("fake"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// PATH carries the WSL launcher and Git\cmd but NO bash.exe — the user's real
	// native-Windows layout (kimi TUI environment).
	t.Setenv("PATH", wslDir+string(os.PathListSeparator)+gitCmdDir)

	got, err := FindBash()
	if err != nil {
		t.Fatalf("FindBash: %v", err)
	}
	want := filepath.Join(gitBashDir, "bash.exe")
	if got != want {
		t.Errorf("FindBash = %q, want %q (must derive from git location, not fall back to WSL)", got, want)
	}
}

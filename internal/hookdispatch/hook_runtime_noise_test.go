package hookdispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// installNoisyBashShim puts a `bash` wrapper first on PATH that reproduces the
// Windows CI shape (run 39384523816): Git Bash printed "could not find /tmp, please
// create!" to stderr on the FIRST hook spawn only. It also appends the TMPDIR each
// spawn saw to <dir>/tmpdir.log. Returns that log path.
//
// installNoisyBashShim 在 PATH 最前放一个 `bash` 包装，复现 Windows CI 形态（run
// 39384523816）：Git Bash 只在**第一次** hook 拉起时往 stderr 打「could not find
// /tmp, please create!」。并把每次拉起看到的 TMPDIR 追加到 <dir>/tmpdir.log，
// 返回该日志路径。
func installNoisyBashShim(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shim is a POSIX sh script; the Windows shape itself is what it simulates")
	}
	realBash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"if [ ! -f '" + dir + "/warned' ]; then : > '" + dir + "/warned'; echo 'bash.exe: warning: could not find /tmp, please create!' >&2; fi\n" +
		"printf '%s\\n' \"${TMPDIR-<unset>}\" >> '" + dir + "/tmpdir.log'\n" +
		"exec '" + realBash + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bash"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(dir, "tmpdir.log")
}

// TestHook_RuntimeNoiseDoesNotSplitBlockDedup pins the root cause of the Windows
// flake "double-fired single Edit event must produce exactly ONE checklog entry,
// got 2": the interpreter warning landed only in the first delivery's stderr,
// cmp.Or(stderr, stdout) made it that delivery's checklog detail, so the two
// identical blocks fingerprinted differently and both were recorded. With the
// noise stripped: one record, and its detail is the real block reason.
//
// TestHook_RuntimeNoiseDoesNotSplitBlockDedup 钉住 Windows flake「double-fired
// single Edit event must produce exactly ONE checklog entry, got 2」的根因：解释器
// 告警只落在第一次投递的 stderr，cmp.Or(stderr, stdout) 让它成了那次的 checklog
// detail，两次完全相同的阻断指纹不同、都被记录。剔噪后：只记一条，且 detail 是
// 真实阻断理由。
func TestHook_RuntimeNoiseDoesNotSplitBlockDedup(t *testing.T) {
	root := newTaskGuardProject(t)
	installNoisyBashShim(t)
	sess := fmt.Sprintf("noise-dedup-%d", time.Now().UnixNano())
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":%q,"tool_input":{"command":"rm -rf ./important-data"}}`, sess)

	for i := 1; i <= 2; i++ {
		if _, err := runHookCapture(t, "hazard-guard", payload); err == nil {
			t.Fatalf("delivery #%d: hazardous command must still be blocked", i)
		}
	}

	var details []string
	for _, e := range readChecklogEntries(t, root) {
		if e.Check == "hazard-guard" && !e.Passed {
			details = append(details, e.Detail)
		}
	}
	if len(details) != 1 {
		t.Fatalf("double-fired identical block must record exactly ONE entry, got %d: %q", len(details), details)
	}
	if strings.Contains(details[0], "could not find /tmp") || !strings.Contains(details[0], "FAIL") {
		t.Errorf("recorded detail must be the block reason, not interpreter noise: %q", details[0])
	}
}

// TestHook_ScriptTMPDIRPinned pins the second half of the fix: with TMPDIR unset
// the script sees the Go-resolved temp dir (so ${TMPDIR:-/tmp} never falls back
// to the MSYS /tmp mount), and an explicit TMPDIR reaches the script untouched.
//
// TestHook_ScriptTMPDIRPinned 钉住修复的另一半：TMPDIR 未设置时脚本看到 Go 侧
// 解析的临时目录（${TMPDIR:-/tmp} 永不回落到 MSYS /tmp 挂载）；显式 TMPDIR 原样
// 到达脚本。
func TestHook_ScriptTMPDIRPinned(t *testing.T) {
	explicit := t.TempDir()
	for _, tc := range []struct {
		name   string
		tmpdir *string // nil = unset
		want   func() string
	}{
		{"unset-pinned", nil, func() string { return filepath.ToSlash(os.TempDir()) }},
		{"explicit-kept", &explicit, func() string { return explicit }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newTaskGuardProject(t)
			logPath := installNoisyBashShim(t)
			t.Setenv("TMPDIR", "")
			if tc.tmpdir == nil {
				_ = os.Unsetenv("TMPDIR")
			} else {
				t.Setenv("TMPDIR", *tc.tmpdir)
			}
			want := tc.want()
			payload := `{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":"tmpdir-pin","tool_input":{"command":"ls"}}`
			if _, err := runHookCapture(t, "bash-guard", payload); err != nil {
				t.Fatalf("read-only command must pass: %v", err)
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("shim never ran (hook did not spawn bash?): %v", err)
			}
			for _, got := range strings.Fields(string(data)) {
				if got != want {
					t.Errorf("script saw TMPDIR=%q, want %q", got, want)
				}
			}
		})
	}
}

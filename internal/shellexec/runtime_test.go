package shellexec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStripRuntimeNoise pins the interpreter-warning filter: the exact MSYS line
// seen on Windows CI (run 39384523816, 39390818763) and glibc's setlocale warning
// are dropped; script-raised warnings ("line N:") and every verdict line survive.
//
// TestStripRuntimeNoise 钉住解释器告警过滤：Windows CI 上实见的 MSYS 行（run
// 39384523816、39390818763）与 glibc setlocale 告警被剔除；脚本自身告警（带
// 「line N:」）与所有结论行保留。
func TestStripRuntimeNoise(t *testing.T) {
	const verdict = "FAIL [read-before-edit] target.go 未在本会话 Read 过"
	for _, tc := range []struct {
		name, in, want string
	}{
		{"msys-tmp-only", "bash.exe: warning: could not find /tmp, please create!", ""},
		{"msys-tmp-before-verdict", "bash.exe: warning: could not find /tmp, please create!\n" + verdict, verdict},
		{"msys-tmp-crlf", "bash.exe: warning: could not find /tmp, please create!\r\nPASS", "PASS"},
		{"abs-path-interpreter", `C:\Program Files\Git\usr\bin\bash.exe: warning: could not find /tmp, please create!` + "\nPASS", "PASS"},
		{"unix-setlocale", "/bin/bash: warning: setlocale: LC_ALL: cannot change locale (en_US.UTF-8)\nPASS", "PASS"},
		{"sh-exe", "sh.exe: warning: could not find /tmp, please create!\nPASS", "PASS"},
		{"script-line-warning-kept", "bash: line 3: warning: here-document at line 1 delimited by end-of-file\nPASS", "bash: line 3: warning: here-document at line 1 delimited by end-of-file\nPASS"},
		{"verdict-mentioning-warning-kept", "FAIL [x] go vet: warning: unused", "FAIL [x] go vet: warning: unused"},
		{"no-warning-untouched", verdict, verdict},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripRuntimeNoise(tc.in); got != tc.want {
				t.Errorf("StripRuntimeNoise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPinTempDir pins the TMPDIR contract: absent or empty → the Go-resolved temp
// dir with forward slashes (appended last, so os/exec's last-wins dedup picks it
// over an inherited empty value); a caller-set TMPDIR is never overridden.
//
// TestPinTempDir 钉住 TMPDIR 契约：缺失或为空 → 正斜杠的 Go 临时目录（追加在末尾，
// os/exec 后者胜出的去重压过继承来的空值）；调用方显式设置的 TMPDIR 永不覆盖。
func TestPinTempDir(t *testing.T) {
	pinned := "TMPDIR=" + filepath.ToSlash(os.TempDir())
	for _, tc := range []struct {
		name string
		in   []string
		want string // the effective (last) TMPDIR entry
	}{
		{"absent", []string{"PATH=/bin"}, pinned},
		{"empty-inherited", []string{"TMPDIR=", "PATH=/bin"}, pinned},
		{"explicit-kept", []string{"TMPDIR=/custom/tmp"}, "TMPDIR=/custom/tmp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := PinTempDir(append([]string(nil), tc.in...))
			last := ""
			for _, kv := range got {
				if strings.HasPrefix(kv, "TMPDIR=") {
					last = kv
				}
			}
			if last != tc.want {
				t.Errorf("effective TMPDIR = %q, want %q (env %v)", last, tc.want, got)
			}
			if strings.Contains(last, `\`) {
				t.Errorf("pinned TMPDIR must use forward slashes for bash: %q", last)
			}
		})
	}
}

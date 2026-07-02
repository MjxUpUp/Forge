package util

import (
	"strings"
	"testing"
)

func TestSanitizeSessionID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain safe id", "session123", "session123"},
		{"path separators become underscore", "a/b\\c", "a_b_c"},
		{"control chars replaced", "a\x00b\x01c", "a_b_c"},
		{"dangerous chars replaced", `a<b>c:d|e`, "a_b_c_d_e"},
		{"collapse consecutive separators", "a--b__c", "a_b_c"},
		{"trim leading and trailing", "_-abc-_.", "abc"},
		{"all unsafe collapses to fallback", "\x00\x01", "session"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SanitizeSessionID(c.in); got != c.want {
				t.Errorf("SanitizeSessionID(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSanitizeSessionIDTruncatesLongInput(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := SanitizeSessionID(long)
	if len(got) != 64 {
		t.Errorf("SanitizeSessionID(len=%d) returned len %d, want 64", len(long), len(got))
	}
}

// TestSanitizeSessionID_AttackVectors guards the security boundary: path
// traversal and shell-metacharacter payloads must not survive sanitization,
// since the result feeds .forge/ filenames (taskpipeline) and shell env vars
// (cli hooks). The allowlist replaces every dangerous byte with '_', so no
// path separator, traversal sequence, or shell metacharacter can remain.
func TestSanitizeSessionID_AttackVectors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"unix traversal", "../../etc/passwd"},
		{"windows traversal", `..\..\windows\system32`},
		{"shell command sep", "a;rm -rf /"},
		{"shell background", "a&whoami"},
		{"shell var", "a$HOME"},
		{"shell backtick", "a`whoami`"},
		{"shell pipe", "a|cat /etc/passwd"},
		{"null byte", "a\x00b"},
		{"mixed traversal+shell", "../x;cat /etc/passwd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SanitizeSessionID(c.in)
			if strings.Contains(got, "/") || strings.Contains(got, `\`) {
				t.Errorf("SanitizeSessionID(%q) = %q: path separator survived", c.in, got)
			}
			if strings.Contains(got, "..") {
				t.Errorf("SanitizeSessionID(%q) = %q: traversal sequence '..' survived", c.in, got)
			}
			for _, bad := range []string{";", "&", "$", "`", "|", "<", ">"} {
				if strings.Contains(got, bad) {
					t.Errorf("SanitizeSessionID(%q) = %q: shell metachar %q survived", c.in, got, bad)
				}
			}
		})
	}
}

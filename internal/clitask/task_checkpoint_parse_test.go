package clitask

import "testing"

// git 会把部分 warning（如 core.autocrlf 的 LF→CRLF 提示）写进 stdout，
// 直接拿 TrimSpace 后的整段当对象名会让 update-ref 收到
// "warning...\n<sha>" 并以 128 失败（2026-11-20 windows CI 实录）。
func TestParseStashRefSkipsWarningLines(t *testing.T) {
	sha := "d059e063e7addbd0279a179e87a6048214c1bd48"
	cases := []struct{ name, in, want string }{
		{"裸 SHA", sha, sha},
		{"警告行在对象名之前", "warning: in the working copy of 'keep.txt', LF will be replaced by CRLF the next time Git touches it\n" + sha, sha},
		{"带尾随空白与空行", sha + "\n\n", sha},
		{"只有警告", "warning: something\n", ""},
		{"空输出", "", ""},
	}
	for _, c := range cases {
		if got := parseStashRef(c.in); got != c.want {
			t.Errorf("%s: parseStashRef(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

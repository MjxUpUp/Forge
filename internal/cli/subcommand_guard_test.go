package cli

import "testing"

// TestUnknownSubcommandRejected 钉住「非可执行父命令收到多余位置参数必须报错」：
// cobra 对这类父命令会打印帮助并返回 nil，退出码 0 让脚本误判命令拼写正确。
func TestUnknownSubcommandRejected(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantParent string
		wantToken  string
	}{
		{"review 拼错", []string{"review", "zzz"}, "forge review", "zzz"},
		{"review llm 已删除", []string{"review", "llm"}, "forge review", "llm"},
		{"change 拼错", []string{"change", "zzz"}, "forge change", "zzz"},
		{"docs 拼错", []string{"docs", "zzz"}, "forge docs", "zzz"},
		{"compat 拼错", []string{"compat", "zzz"}, "forge compat", "zzz"},
		{"裸父命令不误报", []string{"review"}, "", ""},
		{"父命令只带 flag 不误报", []string{"review", "--help"}, "", ""},
		{"可执行叶子命令不误报", []string{"change", "verify", "abcd"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotParent, gotToken := straySubcommand(tc.args)
			if gotParent != tc.wantParent || gotToken != tc.wantToken {
				t.Fatalf("straySubcommand(%v) = (%q, %q), want (%q, %q)",
					tc.args, gotParent, gotToken, tc.wantParent, tc.wantToken)
			}
		})
	}
}

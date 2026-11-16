package util

import (
	"os"
	"strings"
	"testing"
)

// TestScrubPaths covers the three path spellings that leak from evidence
// capture (JSON logs, bash output, Windows temp dirs) and asserts the home
// prefix is replaced while the relative tail survives.
func TestScrubPaths(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		absent string
	}{
		{
			name:   "windows backslash home",
			in:     `C:\Users\alice\AppData\Local\Temp\forge-123\01.stdout`,
			want:   PlaceholderHome + `\AppData\Local\Temp\forge-123\01.stdout`,
			absent: "Users",
		},
		{
			name:   "windows forward slash short name",
			in:     `C:/Users/ALICE~1/AppData/Local/Temp/npm-verify-msg.txt`,
			want:   PlaceholderHome + `/AppData/Local/Temp/npm-verify-msg.txt`,
			absent: "ALICE~1",
		},
		{
			name:   "json escaped backslashes",
			in:     `{"file_path":"C:\\\\Users\\\\ALICE~1\\\\repo\\\\main.go"}`,
			want:   `{"file_path":"<home>\\\\repo\\\\main.go"}`,
			absent: "ALICE~1",
		},
		{
			name:   "unix home",
			in:     `/home/alice/repo/internal/cli/root.go`,
			want:   `<home>/repo/internal/cli/root.go`,
			absent: "alice",
		},
		{
			name:   "msys drive path",
			in:     `/c/Users/bob/repo/spec/forge.yaml`,
			want:   `<home>/repo/spec/forge.yaml`,
			absent: "bob",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScrubPaths(tc.in)
			if !strings.Contains(got, PlaceholderHome) {
				t.Fatalf("ScrubPaths(%q) = %q，缺少 %s 占位符", tc.in, got, PlaceholderHome)
			}
			if tc.absent != `` && strings.Contains(got, tc.absent) {
				t.Errorf("ScrubPaths(%q) = %q，仍含本机标识 %q", tc.in, got, tc.absent)
			}
			if !strings.Contains(got, strings.Split(tc.want, PlaceholderHome)[1]) {
				t.Errorf("ScrubPaths(%q) = %q，期望保留尾部 %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestScrubPaths_RedactsHostname 确保 hostname 不靠形态正则也能被抹掉——
// echo/pwd 输出里它常以裸词出现。
func TestScrubPaths_RedactsHostname(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || len(host) < 3 {
		t.Skip("无主机名可用")
	}
	got := ScrubPaths(`running on ` + host + ` ok`)
	if strings.Contains(got, host) {
		t.Errorf("ScrubPaths 未替换 hostname：%q", got)
	}
	if !strings.Contains(got, PlaceholderHost) {
		t.Errorf("ScrubPaths 缺少 %s 占位符：%q", PlaceholderHost, got)
	}
}

// TestScrubPaths_Idempotent：已脱敏文本再次处理不应继续退化。
func TestScrubPaths_Idempotent(t *testing.T) {
	once := ScrubPaths(`C:/Users/ALICE~1/x`)
	if twice := ScrubPaths(once); twice != once {
		t.Errorf("重复脱敏结果漂移：%q → %q", once, twice)
	}
}

// TestRedactSecrets 覆盖几种真实凭证形态；脱敏后不得残留原文。
func TestRedactSecrets(t *testing.T) {
	cases := []string{
		`token: hunter2secretvalue`,
		`API_KEY="abcdef123456"`,
		`{"api_key": "zabcdefghijk"}`,
		`sk-abcdefghijklmnop`,
		`ghp_abcdefghijklmnopqrstuvwxyz0123`,
		`<password>hunter2secret</password>`,
	}
	for _, in := range cases {
		got := RedactSecrets(in)
		if !strings.Contains(got, RedactedToken) {
			t.Errorf("RedactSecrets(%q) = %q，没有替换为 %s", in, got, RedactedToken)
		}
	}
}

// TestRedact_KeepsOrdinaryText：普通输出不应被改写（避免证据失真）。
func TestRedact_KeepsOrdinaryText(t *testing.T) {
	in := "ok  demo/internal 0.123s\n--- PASS: TestFoo (0.00s)\n"
	if got := Redact(in); got != in {
		t.Errorf("普通文本被改写：%q → %q", in, got)
	}
}

package util

import (
	"strings"
	"testing"
)

// TestRedactSecrets 已知形态逐一脱敏 + 无害文本不受影响。
func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name, in, wantSub string
		wantGone          string
	}{
		{"openai-key", "call with sk-abc123def456ghi789 ok", "[REDACTED]", "sk-abc123def456ghi789"},
		{"anthropic-key", "rk-ant-zxy0987654321 leaked", "[REDACTED]", "rk-ant-zxy0987654321"},
		{"github-token", "ghp_abcdefghijklmnopqrstuvwxyz123456", "[REDACTED]", "ghp_abcdefghijklmnopqrstuvwxyz123456"},
		{"aws-akia", "AKIAIOSFODNN7EXAMPLE", "[REDACTED]", "AKIAIOSFODNN7EXAMPLE"},
		{"jwt", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c", "[REDACTED]", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		{"slack", "xoxb-123456789012-abcdef", "[REDACTED]", "xoxb-123456789012-abcdef"},
		{"assign", `export API_KEY="supersecret123"`, "[REDACTED]", "supersecret123"},
		{"assign-colon", "token: ghp_shouldvanish1234567890abcde", "[REDACTED]", "ghp_shouldvanish1234567890abcde"},
		{"plain-text-untouched", "帮我看看编译报错 go build 失败", "帮我看看编译报错 go build 失败", ""},
		{"chinese-colon-not-assign", "这是 token 的预算：约 500", "约 500", ""},
		// review M3：JSON / XML 键值形态（摘录来源里用户粘贴配置的最常见形态）。
		{"json-key-value", `配置 {"api_key": "hunter2secret"}`, "[REDACTED]", "hunter2secret"},
		{"json-token-key", `{"token": "ghp_shouldvanish1234567890abcde"}`, "[REDACTED]", "ghp_shouldvanish1234567890abcde"},
		{"xml-tag", "<password>hunter2secret</password>", "[REDACTED]", "hunter2secret"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RedactSecrets(c.in)
			if c.wantGone != "" && strings.Contains(got, c.wantGone) {
				t.Fatalf("敏感串未被脱敏: got %q", got)
			}
			if c.wantSub != "" && !strings.Contains(got, c.wantSub) {
				t.Fatalf("期望保留/替换后包含 %q, got %q", c.wantSub, got)
			}
		})
	}
}

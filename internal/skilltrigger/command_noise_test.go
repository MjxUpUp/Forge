package skilltrigger

import (
	"strings"
	"testing"
)

// TestSanitizeCommand 验证 heredoc body 剥离：body 与终止行被丢弃，marker 行与其余行保留。
// 这是 PreToolUse keywords 降噪的核心——分析脚本 body 里的 "npm publish" 字样不再参与匹配。
//
// TestSanitizeCommand verifies heredoc body stripping: body and terminator lines are
// dropped, the marker line and all other lines are kept. This is the core of the
// PreToolUse keyword noise reduction — "npm publish" mentioned inside an analysis
// script body no longer participates in matching.
func TestSanitizeCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "无heredoc原样返回",
			in:   "git tag v1.0 && git push --tags",
			want: "git tag v1.0 && git push --tags",
		},
		{
			name: "剥离quoted-heredoc body与终止行",
			in:   "python - <<'EOF'\nprint('npm publish')\nEOF\necho done",
			want: "python - <<'EOF'\necho done",
		},
		{
			name: "剥离unquoted-heredoc body",
			in:   "cat > x.sh <<EOF\ngit tag v9\nEOF",
			want: "cat > x.sh <<EOF",
		},
		{
			name: " <<-终止行带前导tab",
			in:   "run <<-EOF\n\tbody git tag\n\tEOF",
			want: "run <<-EOF",
		},
		{
			name: "算术左移不误判",
			in:   "echo $((1<<3))\ngit tag v1",
			want: "echo $((1<<3))\ngit tag v1",
		},
		{
			name: "fd重定向2<<不误判",
			in:   "cmd 2<< notes\nmore lines",
			want: "cmd 2<< notes\nmore lines",
		},
		{
			name: "token内x<<word不误判",
			in:   "echo a<<bin\nkeep me",
			want: "echo a<<bin\nkeep me",
		},
		{
			name: "圆括号后的heredoc被识别",
			in:   "(cat <<A\nnpm publish\nA\n)",
			want: "(cat <<A\n)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeCommand(c.in); got != c.want {
				t.Fatalf("sanitizeCommand(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSanitizeCommand_UnterminatedBody 兜底：heredoc 未终止（命令被截断）时，其后所有行
// 都按 body 丢弃——宁缺匹配不误报。
//
// TestSanitizeCommand_UnterminatedBody guard: when a heredoc is never terminated
// (truncated command), everything after it is treated as body — prefer missing a
// match over a false positive.
func TestSanitizeCommand_UnterminatedBody(t *testing.T) {
	got := sanitizeCommand("python - <<'EOF'\nprint('git tag')")
	if got != "python - <<'EOF'" {
		t.Fatalf("未终止 heredoc 应丢弃后续所有行, got %q", got)
	}
}

// TestMatchKeywords_HeredocNoise 端到端：关键词只出现在 heredoc body 时不命中；出现在
// marker 行（真实命令）时命中；stdout 关键词不受 command 剥离影响。
//
// TestMatchKeywords_HeredocNoise end-to-end: keywords appearing only inside a heredoc
// body do not hit; on the marker line (a real command) they do; stdout keywords are
// unaffected by command stripping.
func TestMatchKeywords_HeredocNoise(t *testing.T) {
	kw := []string{"npm publish"}
	ctx := func(cmd string) Context {
		return Context{Event: "PreToolUse", ToolName: "Bash", ToolInput: map[string]any{"command": cmd}}
	}
	if matchKeywords(kw, ctx("python - <<'EOF'\ns='npm publish'\nprint(s)\nEOF")) {
		t.Fatal("heredoc body 内的 'npm publish' 字样不应命中")
	}
	if !matchKeywords(kw, ctx("cd pkg && npm publish --access public")) {
		t.Fatal("真实 npm publish 命令应命中")
	}
	if !matchKeywords([]string{"goreleaser"}, ctx("goreleaser release --config <<EOF\nnoop body\nEOF")) {
		t.Fatal("marker 行关键词应命中（'goreleaser' 类关键词在命令位）")
	}
	post := Context{
		Event:      "PostToolUse",
		ToolName:   "Bash",
		ToolInput:  map[string]any{"command": "python - <<'EOF'\nprint('npm publish')\nEOF"},
		ToolOutput: map[string]any{"stdout": "npm publish ok"},
	}
	if !matchKeywords(kw, post) {
		t.Fatal("stdout 内关键词应命中（stdout 不剥离）")
	}
}

// TestSanitizeCommand_PromptUntouched 确认 prompt 匹配路径不经 sanitize——用户原话里的
// "npm publish"（如"帮我 npm publish"）必须照常触发。
//
// TestSanitizeCommand_PromptUntouched confirms the prompt matching path bypasses
// sanitize — "npm publish" in the user's own words ("帮我 npm publish") must still
// trigger.
func TestSanitizeCommand_PromptUntouched(t *testing.T) {
	ctx := Context{Event: "UserPromptSubmit", Prompt: "帮我 npm publish 发版"}
	if !matchKeywords([]string{"npm publish"}, ctx) {
		t.Fatal("prompt 内关键词应命中")
	}
	if strings.Contains(sanitizeCommand("python - <<'EOF'\nEOF"), "body") {
		t.Fatal("sanity: 不应包含 body")
	}
}

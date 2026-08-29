package agentbridge

import (
	"path/filepath"
	"testing"
)

// TestClaudeConfigHomeDir_WrapperContract 钉死导出 wrapper 与内部实现的单源契约：
// doctor（跨 agent 审计）依赖 ClaudeConfigHomeDir 与检测路径 claudeConfigHome 永远
// 指向同一位置——若有人日后让二者分叉（例如只改其一的 env 约定），该测试立即红。
// 这是 doctor 不持有第二份 host 路径约定的守卫（见 detect.go 注释）。
func TestClaudeConfigHomeDir_WrapperContract(t *testing.T) {
	isolateHome(t)
	if ClaudeConfigHomeDir() != claudeConfigHome() {
		t.Fatalf("wrapper 与内部实现分叉: %q != %q", ClaudeConfigHomeDir(), claudeConfigHome())
	}
	want := filepath.Join(t.TempDir(), "claude-cfg")
	t.Setenv("CLAUDE_CONFIG_DIR", want)
	if got := ClaudeConfigHomeDir(); got != want {
		t.Fatalf("CLAUDE_CONFIG_DIR 优先未生效: got %q want %q", got, want)
	}
}

// TestCodeBuddyWorkBuddyHome_WrapperContract 同上，钉 WorkBuddy config home 的单源
// 契约：WORKBUDDY_CONFIG_DIR 优先、默认 ~/.workbuddy，导出与内部实现一致。
func TestCodeBuddyWorkBuddyHome_WrapperContract(t *testing.T) {
	home := isolateHome(t)
	if got, err := CodeBuddyWorkBuddyHome(); err != nil || got != filepath.Join(home, ".workbuddy") {
		t.Fatalf("默认应为 <home>/.workbuddy, got %q err %v", got, err)
	}
	if _, err := codebuddyWorkBuddyHome(); err != nil {
		t.Fatalf("内部实现不应报错: %v", err)
	}
	want := filepath.Join(t.TempDir(), "wb")
	t.Setenv("WORKBUDDY_CONFIG_DIR", want)
	got, err := CodeBuddyWorkBuddyHome()
	if err != nil || got != want {
		t.Fatalf("WORKBUDDY_CONFIG_DIR 优先未生效: got %q err %v", got, err)
	}
}

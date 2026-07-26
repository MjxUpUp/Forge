package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// CodexTranslator 生成 .codex/hooks.json，镜像 Claude Code 的 hook 接线。Codex 的
// lifecycle hooks（PreToolUse/PostToolUse/Stop）与 Claude Code 的 schema 兼容——
// matcher/hooks/type/command 结构相同，stdin/stdout JSON 协议也相同——故同一批
// `forge hook <name>` 命令原样跑。与 claude-code、cursor 并列，codex 是 hook 真正
// enforce Forge gate 的 agent 之一；copilot/windsurf 仍只发 guidance 文本。
// cursor 的扁平 schema 变体见 CursorTranslator。
//
// Matcher 注意：Codex 把 matcher 编译为针对 tool_name 的 regex，而 Claude Code 把它
// 当 tool-name 匹配。纯名（Bash）与 alternation（Write|Edit）都是合法 regex，在两者
// 中匹配结果一致，故 Claude 接线可直接迁移。Forge 从不发 glob 风格的 `Bash(...)` 形式——
// 它在 Codex 里不是合法 matcher。
type CodexTranslator struct{}

func (t *CodexTranslator) Detect(projectDir string) bool {
	// 仅 .codex/——AGENTS.md 不是 codex 信号（forge 把 AGENTS.md 通用生成为跨 agent
	// 指令；见 DetectAgents 注释）。
	return dirExists(filepath.Join(projectDir, ".codex"))
}

func (t *CodexTranslator) Translate(projectDir string, input *TranslationInput) error {
	codexDir := filepath.Join(projectDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("codex: failed to create .codex dir: %w", err)
	}
	data, err := json.MarshalIndent(buildCodexHooks(), "", "  ")
	if err != nil {
		return fmt.Errorf("codex: failed to marshal hooks.json: %w", err)
	}
	path := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("codex: failed to write hooks.json: %w", err)
	}
	return nil
}

func (t *CodexTranslator) AgentType() AgentType {
	return AgentCodex
}

// buildCodexHooks 从 hooks.ForgeHookSpec 派生 codex 的 hooks.json——该 spec 是与
// settings.local.json、plugin pack 共享的单一真相源。Codex 的 hook schema 与 Claude Code
// 的嵌套 {matcher, hooks:[{type,command}]} 结构相同（Codex 把 matcher 编译为针对
// tool_name 的 regex；Forge 只发纯名与 alternation，均合法 regex），故 spec 可原样
// marshal 为合法 codex hooks.json。Codex 无 SessionStart lifecycle hook，故该 event
// 被过滤（skill-scan 是 Claude-Code 专属）。无手工副本 → 无 drift。
// TestCodexWiringMirrorsClaudeSettings 守卫命令集对等。
func buildCodexHooks() map[string]any {
	spec := hooks.ForgeHookSpec()
	codex := make(map[string][]hooks.HookMatcher, len(spec))
	for event, matchers := range spec {
		// 白名单：codex 只支持 PreToolUse/PostToolUse/Stop（无 SessionStart/PostCompact/
		// UserPromptSubmit 等会话/压缩/prompt lifecycle）。其余 claude-code 特有 event——含
		// gap#2 的 PostCompact/UserPromptSubmit 重注入链——自动跳过。
		if event != "PreToolUse" && event != "PostToolUse" && event != "Stop" {
			continue
		}
		codex[event] = matchers
	}
	return map[string]any{
		`hooks`: codex,
	}
}

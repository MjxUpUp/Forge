package cli

import "testing"

// TestContextChannelDelivered 钉死每宿主通道判定表——checklog 送达章（Delivered/Channel）
// 的唯一真相源。每行对应一个 emitXxxOutput 家族的实证语义（出处见 contextChannelDelivered
// 各 case 注释）。改通道语义（如 kimi 新版本补齐 Stop 通道）须同步这张表与 hook.go。
//
// TestContextChannelDelivered pins the per-host channel table — the single source of truth
// for checklog delivery stamps (Delivered/Channel). Each row mirrors the verified semantics
// of one emitXxxOutput family member (see the per-case comments in contextChannelDelivered).
// Changing channel semantics (e.g. a future kimi carrying Stop stdout) must update this table
// together with hook.go.
func TestContextChannelDelivered(t *testing.T) {
	cases := []struct {
		agent, event string
		delivered    bool
		channel      string
	}{
		// kimi：仅 UserPromptSubmit 的 stdout 进模型上下文（wire.jsonl 实证）。
		// kimi: only UserPromptSubmit stdout reaches the model (wire.jsonl-verified).
		{"kimi", "UserPromptSubmit", true, "kimi/stdout-UserPromptSubmit"},
		{"kimi", "PostToolUse", false, "kimi/no-channel"},
		{"kimi", "Stop", false, "kimi/no-channel"},
		{"kimi", "SessionStart", false, "kimi/no-channel"},
		// codex：hookSpecificOutput.additionalContext 仅四事件被采纳。
		// codex: hookSpecificOutput.additionalContext honored on four events only.
		{"codex", "SessionStart", true, "codex/hookSpecificOutput"},
		{"codex", "PreToolUse", true, "codex/hookSpecificOutput"},
		{"codex", "PostToolUse", true, "codex/hookSpecificOutput"},
		{"codex", "UserPromptSubmit", true, "codex/hookSpecificOutput"},
		{"codex", "Stop", false, "codex/no-channel"},
		{"codex", "PreCompact", false, "codex/no-channel"},
		// cursor：顶层 additional_context 仅 PostToolUse/SessionStart 被读。
		// cursor: top-level additional_context read only on PostToolUse/SessionStart.
		{"cursor", "PostToolUse", true, "cursor/additional_context"},
		{"cursor", "SessionStart", true, "cursor/additional_context"},
		{"cursor", "UserPromptSubmit", false, "cursor/no-channel"},
		{"cursor", "Stop", false, "cursor/no-channel"},
		// copilot：camelCase additionalContext 仅两事件；UPS stdout 被丢。
		// copilot: camelCase additionalContext on two events; UPS stdout dropped.
		{"copilot", "PostToolUse", true, "copilot/additionalContext"},
		{"copilot", "SessionStart", true, "copilot/additionalContext"},
		{"copilot", "UserPromptSubmit", false, "copilot/no-channel"},
		// windsurf：完全没有 stdout JSON 协议——恒未送达。
		// windsurf: no stdout JSON protocol at all — never delivered.
		{"windsurf", "PostToolUse", false, "windsurf/no-context-channel"},
		{"windsurf", "SessionStart", false, "windsurf/no-context-channel"},
		// cline：contextModification 每个扇出事件都注入。
		// cline: contextModification injected on every fanned-out event.
		{"cline", "PostToolUse", true, "cline/contextModification"},
		{"cline", "Stop", true, "cline/contextModification"},
		// 默认行（claude-code / codebuddy / opencode / pi 等无 --agent 的 Claude-JSON
		// 宿主）：additionalContext 每事件都注入。agent="" 是 debug 路径（skill-trigger
		// 命令直跑）——落 claude 默认行。
		// Default row (claude-code / codebuddy / opencode / pi — Claude-JSON hosts without
		// --agent): additionalContext injected on every event. agent="" is the debug path
		// (running the skill-trigger command directly) — lands on the claude default row.
		{"", "UserPromptSubmit", true, "claude/additionalContext"},
		{"", "PostToolUse", true, "claude/additionalContext"},
		{"claude-code", "Stop", true, "claude/additionalContext"},
		{"codebuddy", "PreToolUse", true, "claude/additionalContext"},
		{"unknown-host", "SessionStart", true, "claude/additionalContext"},
	}
	for _, c := range cases {
		delivered, channel := contextChannelDelivered(c.agent, c.event)
		if delivered != c.delivered || channel != c.channel {
			t.Errorf("contextChannelDelivered(%q, %q) = (%v, %q), want (%v, %q)",
				c.agent, c.event, delivered, channel, c.delivered, c.channel)
		}
	}
}

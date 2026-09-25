package agentbridge

// copilot_hooks_test.go —— #4-A 后续（2026-08-22）的守卫：copilot 白名单接入
// PostToolUseFailure/SubagentStop，承载 failure-track/subagent-track（按
// spec-research4 跨宿主矩阵官方在列）。钉住让接线成立的三个契约：两事件在位且带
// `--agent copilot` delta、matcher 原样透传（copilot 匹配 Claude 工具名）、
// PostCompact 仍被过滤（observe-only 的 preCompact 承载不了重注入契约）。

import (
	"encoding/json"
	"strings"
	"testing"
)

// copilotHookEntriesOf 抽取某个 event 键下接线的条目。
func copilotHookEntriesOf(t *testing.T, raw map[string]any, event string) []copilotHookEntry {
	t.Helper()
	hooksMap, ok := raw[`hooks`].(map[string][]copilotHookEntry)
	if !ok {
		t.Fatalf(`copilot wiring shape unexpected: %T`, raw[`hooks`])
	}
	return hooksMap[event]
}

// TestBuildCopilotHooks_IncludesObservationEvents 钉住 #4-A 后续：两个观察事件
// 在 copilot 官方名册，其 hook 必须流进 manifest——failure-track 挂
// PostToolUseFailure（Bash matcher 原样；copilot 匹配前把 bash/powershell→Bash，
// Bash 单独即全覆盖），subagent-track 挂 SubagentStop（match-all，无
// matcher）。每条命令带 ` --agent copilot`（输出协议选择——接线测试惯例）。
func TestBuildCopilotHooks_IncludesObservationEvents(t *testing.T) {
	raw := buildCopilotHooks()

	failures := copilotHookEntriesOf(t, raw, "PostToolUseFailure")
	if len(failures) != 1 {
		t.Fatalf("PostToolUseFailure entries = %d, want 1 (failure-track)", len(failures))
	}
	if !strings.Contains(failures[0].Command, "forge hook failure-track") {
		t.Errorf("PostToolUseFailure must carry failure-track, got: %s", failures[0].Command)
	}
	if !strings.Contains(failures[0].Command, " --agent copilot") {
		t.Errorf("copilot hook command missing --agent copilot suffix: %s", failures[0].Command)
	}
	if failures[0].Matcher != "Bash" {
		t.Errorf("matcher must pass through verbatim (copilot maps bash/powershell→Bash before matching), got: %q", failures[0].Matcher)
	}

	stops := copilotHookEntriesOf(t, raw, "SubagentStop")
	if len(stops) != 1 {
		t.Fatalf("SubagentStop entries = %d, want 1 (subagent-track)", len(stops))
	}
	if !strings.Contains(stops[0].Command, "forge hook subagent-track") {
		t.Errorf("SubagentStop must carry subagent-track, got: %s", stops[0].Command)
	}
	if !strings.Contains(stops[0].Command, " --agent copilot") {
		t.Errorf("copilot hook command missing --agent copilot suffix: %s", stops[0].Command)
	}
	if stops[0].Matcher != "" {
		t.Errorf("subagent-track is match-all, expected empty matcher, got: %q", stops[0].Matcher)
	}

	// PostCompact 保持过滤：copilot 名册只有 observe-only 的 preCompact——接线它
	// 有载入期条目级丢弃风险。
	if entries := copilotHookEntriesOf(t, raw, "PostCompact"); len(entries) != 0 {
		t.Errorf("PostCompact must stay filtered (no copilot analogue), got %d entries", len(entries))
	}
}

// TestCopilotEventName_UnknownEventsFiltered 钉住白名单的失败模式：无 copilot
// 对应物的 spec event 一律丢弃、绝不发布——未知 event 键有载入期条目级丢弃风险
// （kimi 教训的镜像：那边整个插件死掉；这边只丢条目，但纪律相同——只接名册
// 验证过的事件）。
func TestCopilotEventName_UnknownEventsFiltered(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit", "PostToolUseFailure", "SubagentStop"} {
		if _, ok := copilotEventName(event); !ok {
			t.Errorf("copilotEventName(%q) must accept the rostered event", event)
		}
	}
	for _, event := range []string{"PostCompact", "Notification", "PreCompact", "SomeFutureEvent"} {
		if _, ok := copilotEventName(event); ok {
			t.Errorf("copilotEventName(%q) must filter the un-rostered event", event)
		}
	}
}

// TestBuildCopilotHooks_JSONShapeRoundTrip 名册扩后对 manifest 形状的健全性钉：
// version 1 + PascalCase event 键 + 扁平条目——committed plugins/<name>/hooks.json
// 守卫（pluginpack_test.go）字节比对的形状；形状漂移在这里以更清晰的信号快速
// 失败，好过 committed manifest 不匹配的远端报错。
func TestBuildCopilotHooks_JSONShapeRoundTrip(t *testing.T) {
	data, err := json.Marshal(buildCopilotHooks())
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Version int                           `json:"version"`
		Hooks   map[string][]copilotHookEntry `json:"hooks"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("manifest must round-trip: %v", err)
	}
	if back.Version != 1 {
		t.Errorf("version = %d, want 1", back.Version)
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit", "PostToolUseFailure", "SubagentStop"} {
		if _, ok := back.Hooks[event]; !ok {
			t.Errorf("round-tripped manifest missing event %q", event)
		}
	}
	if _, ok := back.Hooks["PostCompact"]; ok {
		t.Error("round-tripped manifest must not carry PostCompact")
	}
}

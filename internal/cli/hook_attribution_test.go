package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// runHookWithStdin chdirs into a fresh forge project (temp dir + .forge/state.json,
// the same fixture shape as TestHookOutput_StructuredJSON), feeds stdinJSON to a
// hook invocation, and returns the project root for DataDir assertions.
// FORGE_DATA_HOME is isolated per test; the agent-selecting env/flag surfaces are
// cleared so only the payload under test drives attribution.
//
// runHookWithStdin chdir 进一个全新的 forge 项目（temp dir + .forge/state.json，
// 与 TestHookOutput_StructuredJSON 同 fixture 形态），把 stdinJSON 喂给一次 hook
// 调用，返回项目 root 供 DataDir 断言。FORGE_DATA_HOME 按测试隔离；agent 选择相关
// 的 env/flag 面被清空，使归因只受被测 payload 驱动。
func runHookWithStdin(t *testing.T, hookName, stdinJSON string) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("FORGE_HOOK_AGENT", "")
	t.Setenv("FORGE_AGENT", "")

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644); err != nil {
		t.Fatal(err)
	}
	originalWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })

	oldStdin := os.Stdin
	tmpStdin, err := os.CreateTemp("", "hook-stdin-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpStdin.WriteString(stdinJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpStdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = tmpStdin
	t.Cleanup(func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	})

	captureStdout(t, func() {
		// auto-compile/tool-track may error inside the embedded script in a bare
		// temp project — the attribution side effects under test run before that.
		// A minimal cobra root (not nil): the Go-internal hooks' dispatch reads
		// cmd.Root().Version and would nil-panic on nil.
		//
		// auto-compile/tool-track 在裸 temp 项目里嵌脚本可能报错——被测的归因副
		// 作用在那之前已发生。传最小 cobra root（非 nil）：Go 内 hook 的分派读
		// cmd.Root().Version，nil 会空指针。
		_ = runHook(&cobra.Command{}, []string{hookName})
	})
	return root
}

// TestHook_CursorConversationIDRegistersSession pins the cursor attribution fix:
// cursor's tool events carry ONLY conversation_id (no session_id) — the hook path
// must adopt it as the session id, register the session, and refresh the
// last-session pointer (before the fix, every such event collapsed onto the
// legacy global key and cursor sessions were never registered).
//
// TestHook_CursorConversationIDRegistersSession 钉住 cursor 归因修复：cursor 的
// 工具事件只带 conversation_id（无 session_id）——hook 路径必须采纳它为
// session id、登记会话并刷新 last-session 指针（修复前每个此类事件都坍缩到
// legacy 全局键，cursor 会话从未被登记）。
func TestHook_CursorConversationIDRegistersSession(t *testing.T) {
	root := runHookWithStdin(t, "auto-compile",
		`{"hook_event_name":"PostToolUse","tool_name":"Write","conversation_id":"conv-cursor-1","tool_input":{"file_path":"src/main.go","content":"package main"}}`)

	sid, _, ok := taskpipeline.RecentHookSession(root)
	if !ok || sid != "conv-cursor-1" {
		t.Errorf("last-session pointer = (%q, %v), want conv-cursor-1 adopted from conversation_id", sid, ok)
	}
	records, err := taskpipeline.LoadSessions(root)
	if err != nil || len(records) == 0 {
		t.Fatalf("cursor session must be registered, LoadSessions: %v (n=%d)", err, len(records))
	}
	if records[0].SessionID != "conv-cursor-1" {
		t.Errorf("registered session id = %q, want conv-cursor-1", records[0].SessionID)
	}
}

// TestHook_ForgeAgentPayloadAttributesSession pins the opencode attribution
// channel: a host constructing Claude-shape stdin in-process declares its
// identity via the forge_agent payload field (its wiring test pins the command
// roster, so no --agent suffix) — the session must be registered AND stamped
// with that agent.
//
// TestHook_ForgeAgentPayloadAttributesSession 钉住 opencode 归因通道：在进程内
// 构造 Claude 形 stdin 的宿主经 forge_agent payload 字段声明身份（其 wiring
// 测试钉死命令名册，故不加 --agent 后缀）——会话必须被登记且盖该 agent 的戳。
func TestHook_ForgeAgentPayloadAttributesSession(t *testing.T) {
	root := runHookWithStdin(t, "auto-compile",
		`{"hook_event_name":"PostToolUse","tool_name":"Write","session_id":"oc-sess-1","forge_agent":"opencode","tool_input":{"file_path":"src/main.go","content":"package main"}}`)

	records, err := taskpipeline.LoadSessions(root)
	if err != nil || len(records) == 0 {
		t.Fatalf("opencode session must be registered, LoadSessions: %v (n=%d)", err, len(records))
	}
	if records[0].AgentType != "opencode" {
		t.Errorf("AgentType = %q, want opencode (from forge_agent payload)", records[0].AgentType)
	}
	_, agent, ok := taskpipeline.RecentHookSession(root)
	if !ok || agent != "opencode" {
		t.Errorf("pointer agent = (%q, %v), want opencode", agent, ok)
	}
}

// TestHook_KimiSessionRegistersWithDeclarativeAgent is the fleet-gap regression
// guard: a kimi hook event (real session id + --agent kimi) must register the
// session AS kimi even in a project whose only marker is .claude/ — the exact
// misattribution found in this repo's sessions.jsonl (2026-08 audit).
//
// TestHook_KimiSessionRegistersWithDeclarativeAgent 是全宿主缺口的回归守卫：kimi
// hook 事件（真实 session id + --agent kimi）必须把会话登记为 kimi，即便项目
// 唯一的标记是 .claude/——正是本仓 sessions.jsonl 里发现的误归属（2026-08
// 审计）。
func TestHook_KimiSessionRegistersWithDeclarativeAgent(t *testing.T) {
	t.Setenv("FORGE_HOOK_AGENT", "kimi")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	// runHookWithStdin clears FORGE_HOOK_AGENT — set it again after the fixture
	// runs, before the hook invocation. Simpler: inline the flow here.
	//
	// runHookWithStdin 会清 FORGE_HOOK_AGENT——在其 fixture 之后、hook 调用之前
	// 重新设置。更简单：此处内联整个流程。
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("FORGE_HOOK_AGENT", "kimi")
	t.Setenv("FORGE_AGENT", "")
	if err := os.MkdirAll(filepath.Join(root, ".forge", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644); err != nil {
		t.Fatal(err)
	}
	originalWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })

	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	// kimi stdin dialect: snake fields like Claude but prompt is a content-block
	// array; for a tool event the plain shape parses fine through kimiNormalize.
	//
	// kimi stdin 方言：字段同 Claude 的 snake 形，但 prompt 是 content-block
	// 数组；工具事件的简单形状可正常过 kimiNormalize。
	if _, err := tmpStdin.WriteString(`{"hook_event_name":"PostToolUse","tool_name":"Write","session_id":"session_kimi-42","cwd":"" ,"tool_input":{"file_path":"src/main.go","content":"package main"}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpStdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = tmpStdin
	t.Cleanup(func() { os.Stdin = oldStdin; tmpStdin.Close(); os.Remove(tmpStdin.Name()) })

	captureStdout(t, func() { _ = runHook(nil, []string{"auto-compile"}) })

	records, err := taskpipeline.LoadSessions(root)
	if err != nil || len(records) == 0 {
		t.Fatalf("kimi session must be registered, LoadSessions: %v (n=%d)", err, len(records))
	}
	if records[0].AgentType != "kimi" {
		t.Errorf("AgentType = %q, want kimi (declarative --agent beats the .claude marker)", records[0].AgentType)
	}
}

// TestResolveOriginTool_PointerFallback pins the CLI-side adoption: with no
// identity env at all, a fresh last-session pointer supplies the origin tool;
// without a pointer the result stays empty (never invent attribution).
//
// TestResolveOriginTool_PointerFallback 钉住 CLI 侧采纳：完全无身份 env 时，
// 新鲜的 last-session 指针提供 origin tool；无指针时结果保持空（绝不编造归属）。
func TestResolveOriginTool_PointerFallback(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("FORGE_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	root := t.TempDir()

	if got := resolveOriginTool(root, ""); got != "" {
		t.Errorf("no pointer: got %q, want empty", got)
	}
	taskpipeline.TouchLastSession(root, "session_kimi-1", "kimi", "UserPromptSubmit")
	if got := resolveOriginTool(root, ""); got != "kimi" {
		t.Errorf("fresh pointer: got %q, want kimi", got)
	}
	if got := resolveOriginTool(root, "explicit-tool"); got != "explicit-tool" {
		t.Errorf("explicit flag must win over pointer, got %q", got)
	}
}

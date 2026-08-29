package cli

// hook_track_test.go —— 三个 #4-A 观察 hook（failure-track / subagent-track /
// test-nudge，hook_track.go）的守卫。每个 hook 三个维度：checklog 观察落盘
// （Check 名、观察语义）；advisory 通道只在应发时发（编译 marker / 阈值）、
// 永不阻断（无 HookBlockError）；静默契约成立（无活跃任务→无状态文件；
// 非编译失败→无 stdout；SubagentStop→纯记录）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// trackTestProject 隔离 FORGE_DATA_HOME 并返回临时项目根。三个 hook 经
// checklog.Record(root, ...) 记录，DataDir 从隔离 home 解析——不污染真实 home。
func trackTestProject(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	return t.TempDir()
}

// startTrackTask 把活跃任务绑定到 session（taskRefForSession 随之非空），
// 打开 test-nudge 的活跃任务门。与 review_status_test.go 的
// renderStatusWithEvidence 同构：SetActiveTaskRef + 最小未完成 TaskState
// （ActiveTaskState 要求状态文件可加载且未完成）。
func startTrackTask(t *testing.T, root, sessionID, taskRef string) {
	t.Helper()
	if err := taskpipeline.SetActiveTaskRef(root, sessionID, taskRef); err != nil {
		t.Fatal(err)
	}
	state := &taskpipeline.TaskState{TaskRef: taskRef, SessionID: sessionID, Branch: "feat/x", StartedAt: time.Now()}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
}

// resetNudgeState 删除 os.TempDir() 下的 per-session 计数器文件。该状态刻意住在
// 真实 temp 目录（生产选择：短命、OS 清理），上一次测试运行的残留会污染连写计数
// ——每个 nudge 测试前后都清一次，做跨运行隔离。
func resetNudgeState(t *testing.T, sessionID string) {
	t.Helper()
	path := filepath.Join(os.TempDir(), "forge-testnudge", util.SanitizeSessionID(sessionID)+".json")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
}

// wantSilent 断言捕获的 hook 输出为空——观察 hook 非发射路径的静默契约（label
// 携带原各处的上下文）。
func wantSilent(t *testing.T, label, out string) {
	t.Helper()
	if out != "" {
		t.Errorf("%s, got: %q", label, out)
	}
}

// findTrackEntries 加载 root 的全部 checklog 条目并按 check 名过滤。
func findTrackEntries(t *testing.T, root string, check checklog.CheckName) []checklog.Entry {
	t.Helper()
	all, err := checklog.LoadAllAll(root)
	if err != nil {
		t.Fatalf("checklog LoadAllAll: %v", err)
	}
	var out []checklog.Entry
	for _, e := range all {
		if e.Check == check {
			out = append(out, e)
		}
	}
	return out
}

// TestRunFailureTrackHook_RecordsAndNudgesOnCompileFailure pins the compile-failure path: the observation lands as CheckToolFailure AND the factual compile-fix-loop pointer rides the allow-with-detail channel (hookSpecificOutput JSON on stdout — never a block: PostToolUseFailure cannot un-fail the command).
//
// TestRunFailureTrackHook_RecordsAndNudgesOnCompileFailure 钉住编译失败路径：
// 观察以 CheckToolFailure 落盘，且事实性 compile-fix-loop 指引走 allow-with-detail
// 通道（stdout 的 hookSpecificOutput JSON——绝不阻断：PostToolUseFailure 救不回
// 已失败的命令）。
func TestRunFailureTrackHook_RecordsAndNudgesOnCompileFailure(t *testing.T) {
	root := trackTestProject(t)
	in := HookInput{
		HookEventName: "PostToolUseFailure",
		SessionID:     "sess-ft-1",
		ToolName:      "Bash",
		Error:         "go build ./... failed: undefined: util.Foo",
	}
	out := captureStdout(t, func() {
		if err := runFailureTrackHook(in, root, "test", ""); err != nil {
			t.Fatalf("failure-track must never error: %v", err)
		}
	})
	if !strings.Contains(out, "compile-fix-loop") {
		t.Errorf("compile-marker failure must emit the compile-fix-loop pointer, got stdout: %q", out)
	}
	// 只给 skill 名的契约（2026-08-25 文案修复）：nudge 用自然语言点名 skill；
	// 仓库相对 skills/... 路径在 Forge 仓库外是 404，绝不得出现。
	if !strings.Contains(out, "Load the compile-fix-loop skill") {
		t.Errorf("nudge must use the natural-language loading form, got stdout: %q", out)
	}
	if strings.Contains(out, "skills/compile-fix-loop") {
		t.Errorf("nudge must not carry the repo-relative skills/ path (dead outside the Forge repo), got stdout: %q", out)
	}
	if strings.Contains(out, `"decision":"block"`) {
		t.Errorf("failure-track must never block, got block JSON: %q", out)
	}
	entries := findTrackEntries(t, root, checklog.CheckToolFailure)
	if len(entries) != 1 {
		t.Fatalf("CheckToolFailure entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Detail, "undefined: util.Foo") {
		t.Errorf("detail should carry the (redacted) error text, got: %q", entries[0].Detail)
	}
	// 发射路径：Delivered 必须落章（值随 hostcap 通道裁定——此处契约是存在性）。
	if entries[0].Delivered == nil {
		t.Error("compile-marker path emits, so Delivered must be stamped (non-nil)")
	}
}

// TestRunFailureTrackHook_GenericFailureRecordsSilently pins the silence contract: a non-compile failure (network timeout) still records the observation (the model already sees the failed output in its own tool result) but emits nothing.
//
// TestRunFailureTrackHook_GenericFailureRecordsSilently 钉住静默契约：非编译类
// 失败（网络超时）仍落观察记录（模型本就看到自己工具结果里的失败输出），
// 但不发任何输出。
func TestRunFailureTrackHook_GenericFailureRecordsSilently(t *testing.T) {
	root := trackTestProject(t)
	in := HookInput{
		HookEventName: "PostToolUseFailure",
		SessionID:     "sess-ft-2",
		ToolName:      "Bash",
		Error:         "network timeout while fetching",
	}
	out := captureStdout(t, func() {
		_ = runFailureTrackHook(in, root, "test", "")
	})
	if out != "" {
		t.Errorf("non-compile failure must stay silent, got stdout: %q", out)
	}
	if n := len(findTrackEntries(t, root, checklog.CheckToolFailure)); n != 1 {
		t.Errorf("generic failure still records (observation), entries = %d, want 1", n)
	}
	// 静默契约含送达章：零发射则 Delivered 保持 nil（复审 2026-08-22——漏斗
	// 只计 Delivered=true，未发射却盖章正是那个 bug 类）。
	entries := findTrackEntries(t, root, checklog.CheckToolFailure)
	if len(entries) != 1 || entries[0].Delivered != nil {
		t.Errorf("non-emitting path must leave Delivered nil, got %+v", entries)
	}
}

// TestRunSubagentTrackHook_RecordsAttribution pins the SubagentStop observation: agent_id/agent_type land in Meta, the delivery summary carries length + first line (never the full message — checklog is not a message archive), and nothing is emitted (SubagentStop stdout is not a context channel).
//
// TestRunSubagentTrackHook_RecordsAttribution 钉住 SubagentStop 观察：
// agent_id/agent_type 落 Meta，交付摘要带长度+首行（绝不存全文——checklog 不是
// 消息归档），且零输出（SubagentStop 的 stdout 不是上下文通道）。
func TestRunSubagentTrackHook_RecordsAttribution(t *testing.T) {
	root := trackTestProject(t)
	in := HookInput{
		HookEventName:        "SubagentStop",
		SessionID:            "sess-st-1",
		AgentID:              "agent-42",
		AgentTypeHook:        "code-reviewer",
		LastAssistantMessage: "Reviewed 3 files, found 1 medium issue.\nDetails omitted.",
	}
	out := captureStdout(t, func() {
		if err := runSubagentTrackHook(in, root, "test", ""); err != nil {
			t.Fatalf("subagent-track must never error: %v", err)
		}
	})
	if out != "" {
		t.Errorf("subagent-track is observe-only, got stdout: %q", out)
	}
	entries := findTrackEntries(t, root, checklog.CheckSubagentStop)
	if len(entries) != 1 {
		t.Fatalf("CheckSubagentStop entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Meta["agent_type"] != "code-reviewer" || e.Meta["agent_id"] != "agent-42" {
		t.Errorf("meta attribution missing: %+v", e.Meta)
	}
	if want := fmt.Sprintf("%d", len(in.LastAssistantMessage)); e.Meta["message_len"] != want {
		t.Errorf("message_len = %q, want %q (len of full message)", e.Meta["message_len"], want)
	}
	if !strings.Contains(e.Detail, "code-reviewer") || !strings.Contains(e.Detail, "Reviewed 3 files") {
		t.Errorf("detail should carry type + first line, got: %q", e.Detail)
	}
	if strings.Contains(e.Detail, "Details omitted") {
		t.Errorf("detail must NOT store the full message (archive concern), got: %q", e.Detail)
	}
}

// TestRunSubagentTrackHook_UnknownTypeFallsBack pins the agent_type fallback: a missing agent_type records "unknown" instead of an empty Meta value (absent-key semantics live in the entry's absence, not in empty strings).
//
// TestRunSubagentTrackHook_UnknownTypeFallsBack 钉住 agent_type 兜底：缺
// agent_type 时记 "unknown" 而非空 Meta 值（缺键语义靠条目缺席表达，不靠空串）。
func TestRunSubagentTrackHook_UnknownTypeFallsBack(t *testing.T) {
	root := trackTestProject(t)
	in := HookInput{HookEventName: "SubagentStop", SessionID: "sess-st-2"}
	captureStdout(t, func() { _ = runSubagentTrackHook(in, root, "test", "") })
	entries := findTrackEntries(t, root, checklog.CheckSubagentStop)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Meta["agent_type"] != "unknown" {
		t.Errorf("agent_type fallback = %q, want unknown", entries[0].Meta["agent_type"])
	}
}

// writeSourceForNudge 模拟一次经 runTestNudgeHook 的 PostToolUse Write 事件，
// 返回捕获的 stdout。
func writeSourceForNudge(t *testing.T, root, sessionID, path string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"file_path": path})
	in := HookInput{
		HookEventName: "PostToolUse",
		SessionID:     sessionID,
		ToolName:      "Write",
		ToolInput:     raw,
	}
	return captureStdout(t, func() {
		if err := runTestNudgeHook(in, root, "test", ""); err != nil {
			t.Fatalf("test-nudge must never error: %v", err)
		}
	})
}

// TestRunTestNudgeHook_SilentWithoutTask pins the active-task gate: no bound task → not even a counter state file (exploratory editing outside a task is not the nudge's business; the test-coverage gate does not apply either).
//
// TestRunTestNudgeHook_SilentWithoutTask 钉住活跃任务门：无绑定任务→连计数器
// 状态文件都不落（任务外的探索性编辑不归 nudge 管；test-coverage 门禁同样不适用）。
func TestRunTestNudgeHook_SilentWithoutTask(t *testing.T) {
	root := trackTestProject(t)
	resetNudgeState(t, "sess-tn-0")
	out := writeSourceForNudge(t, root, "sess-tn-0", filepath.Join(root, "a.go"))
	if out != "" {
		t.Errorf("no active task → silent, got stdout: %q", out)
	}
	statePath := filepath.Join(os.TempDir(), "forge-testnudge", util.SanitizeSessionID("sess-tn-0")+".json")
	if _, err := os.Stat(statePath); err == nil {
		t.Errorf("no active task → no counter state file must be written: %s exists", statePath)
	}
}

// TestRunTestNudgeHook_ThresholdNudgeAndReset pins the streak contract end-to-end: writes 1..2 are silent, the 3rd fires ONE factual reminder (test-discipline), the 4th stays silent (per-streak single nudge — no spam), a test-file write resets the streak, and the next 3-source-write streak fires again.
//
// TestRunTestNudgeHook_ThresholdNudgeAndReset 端到端钉住连写契约：第 1..2 次
// 写入静默，第 3 次发出一次事实性提醒（test-discipline），第 4 次仍静默（每连写
// 只提示一次——不刷屏），测试文件写入重置连写，之后 3 次源码写入再次触发。
// 观察以 CheckTestNudge 连带计数落盘。
func TestRunTestNudgeHook_ThresholdNudgeAndReset(t *testing.T) {
	root := trackTestProject(t)
	const sid = "sess-tn-1"
	resetNudgeState(t, sid)
	startTrackTask(t, root, sid, "feat/nudge-test")

	wantSilent(t, `write #1 must be silent`, writeSourceForNudge(t, root, sid, filepath.Join(root, "a.go")))
	wantSilent(t, `write #2 must be silent`, writeSourceForNudge(t, root, sid, filepath.Join(root, "b.go")))
	out3 := writeSourceForNudge(t, root, sid, filepath.Join(root, "c.go"))
	if !strings.Contains(out3, "test-discipline") {
		t.Errorf("write #3 (threshold) must fire the test-discipline reminder, got: %q", out3)
	}
	// 与 failure-track 同款只给 skill 名的契约（2026-08-25 文案修复）：自然语言
	// 引用 skill，不含仓库相对 skills/ 路径。
	if !strings.Contains(out3, "Load the test-discipline skill") {
		t.Errorf("nudge must use the natural-language loading form, got: %q", out3)
	}
	if strings.Contains(out3, "skills/test-discipline") {
		t.Errorf("nudge must not carry the repo-relative skills/ path (dead outside the Forge repo), got: %q", out3)
	}
	if strings.Contains(out3, `"decision":"block"`) {
		t.Errorf("test-nudge must never block, got block JSON: %q", out3)
	}
	wantSilent(t, `write #4 after the nudge must stay silent (one nudge per streak)`, writeSourceForNudge(t, root, sid, filepath.Join(root, "d.go")))

	// 测试写入重置连写并重新武装提示。
	wantSilent(t, `test-file write must be silent`, writeSourceForNudge(t, root, sid, filepath.Join(root, "a_test.go")))
	wantSilent(t, `post-reset write #1 must be silent`, writeSourceForNudge(t, root, sid, filepath.Join(root, "e.go")))
	wantSilent(t, `post-reset write #2 must be silent`, writeSourceForNudge(t, root, sid, filepath.Join(root, "f.go")))
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "g.go")); !strings.Contains(out, "test-discipline") {
		t.Errorf("post-reset write #3 must re-fire the reminder, got: %q", out)
	}

	entries := findTrackEntries(t, root, checklog.CheckTestNudge)
	if len(entries) != 2 {
		t.Fatalf("CheckTestNudge entries = %d, want 2 (two streaks, one nudge each)", len(entries))
	}
	if entries[0].Meta["source_writes"] != "3" {
		t.Errorf("first nudge source_writes = %q, want 3", entries[0].Meta["source_writes"])
	}
	if entries[0].TaskRef != "feat/nudge-test" {
		t.Errorf("nudge entry must carry the active task ref, got %q", entries[0].TaskRef)
	}
}

// TestHook_FailureTrackCursorPayloadAdapted pins the #4-A follow-up's payload adaptation for cursor: cursor's postToolUseFailure classifies via failure_type instead of an error string and carries conversation_id instead of session_id.
//
// TestHook_FailureTrackCursorPayloadAdapted 钉住 #4-A 后续的 cursor payload 适配：
// cursor 的 postToolUseFailure 用 failure_type 分类而非 error 字符串、带
// conversation_id 而非 session_id。走完整 runHook 路径：(1) 仅分类的 payload 由
// failure_type 填空 Error（Meta 记录供按类聚合）且保持静默——枚举值不命中任何
// 编译 marker，无误发提示；(2) 真实 error 字符串在场时优先（填空语义），marker
// 命中、发出 compile-fix-loop 指引并盖 Delivered 章；(3) cursor 文档的双字段
// 形态（error_message 文本 + failure_type 枚举）用**文本**填 Error——枚举只随
// Meta 记录；(4) 观察经 conversation_id 完成会话归因。
func TestHook_FailureTrackCursorPayloadAdapted(t *testing.T) {
	// 阶段 1：仅分类的 payload（cursor 文档形态）——静默观察，Error 由
	// failure_type 填空，会话取 conversation_id。
	root1 := runHookWithStdin(t, "failure-track",
		`{"hook_event_name":"PostToolUseFailure","tool_name":"Shell","conversation_id":"conv-cursor-fail-1","failure_type":"timeout"}`)
	entries := findTrackEntries(t, root1, checklog.CheckToolFailure)
	if len(entries) != 1 {
		t.Fatalf("CheckToolFailure entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.SessionID != "conv-cursor-fail-1" {
		t.Errorf("session = %q, want conversation_id adopted", e.SessionID)
	}
	if e.Meta["failure_type"] != "timeout" {
		t.Errorf("meta failure_type = %q, want timeout (class aggregation)", e.Meta["failure_type"])
	}
	if !strings.Contains(e.Detail, "timeout") {
		t.Errorf("detail should carry the filled Error text, got: %q", e.Detail)
	}
	if e.Delivered != nil {
		t.Errorf("enum-only failure must not emit → Delivered nil, got %v", *e.Delivered)
	}

	// 阶段 2：真实错误文本在场——优先于 failure_type（填空语义），编译 marker
	// 命中，指引连同 Delivered 章一起发出。
	root2 := runHookWithStdin(t, "failure-track",
		`{"hook_event_name":"PostToolUseFailure","tool_name":"Shell","conversation_id":"conv-cursor-fail-2","failure_type":"error","error":"go build ./... failed: undefined: util.Foo"}`)
	entries2 := findTrackEntries(t, root2, checklog.CheckToolFailure)
	if len(entries2) != 1 {
		t.Fatalf("phase-2 CheckToolFailure entries = %d, want 1", len(entries2))
	}
	if !strings.Contains(entries2[0].Detail, "undefined: util.Foo") {
		t.Errorf("real error text must win over the enum, got detail: %q", entries2[0].Detail)
	}
	if entries2[0].Delivered == nil {
		t.Error("compile-marker path emits, so Delivered must be stamped (non-nil)")
	}

	// 阶段 3：cursor 文档的双字段形态——error_message 文本与 failure_type 枚举
	// 同发。文本填 Error（胜过枚举），编译 marker 在真实文本上命中，Meta 仍记录
	// 分类。
	root3 := runHookWithStdin(t, "failure-track",
		`{"hook_event_name":"PostToolUseFailure","tool_name":"Shell","conversation_id":"conv-cursor-fail-3","failure_type":"error","error_message":"go build ./... failed: undefined: util.Foo"}`)
	entries3 := findTrackEntries(t, root3, checklog.CheckToolFailure)
	if len(entries3) != 1 {
		t.Fatalf("phase-3 CheckToolFailure entries = %d, want 1", len(entries3))
	}
	if !strings.Contains(entries3[0].Detail, "undefined: util.Foo") {
		t.Errorf("error_message text must fill Error and beat the enum, got detail: %q", entries3[0].Detail)
	}
	if entries3[0].Meta["failure_type"] != "error" {
		t.Errorf("meta failure_type = %q, want error (class still recorded)", entries3[0].Meta["failure_type"])
	}
	if entries3[0].Delivered == nil {
		t.Error("real compile-failure text in error_message must fire the nudge → Delivered stamped")
	}
}

// TestHook_CursorWorkspaceRootsResolvesProject pins the MAJOR-1 fix: cursor's user-level hooks run from ~/.cursor and its payload has NO cwd — the project only enters via workspace_roots.
//
// TestHook_CursorWorkspaceRootsResolvesProject 钉住 MAJOR-1 修复：cursor 用户级
// hook 从 ~/.cursor 运行、payload 无 cwd——项目只能经 workspace_roots 进入。
// 进程 cwd 在项目之外时，带 workspace_roots 的 failure-track 事件仍须解析出
// 项目并落观察（修复前：findProjectRoot 失败→静默放行→永不记录）。
func TestHook_CursorWorkspaceRootsResolvesProject(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("FORGE_HOOK_AGENT", "cursor")
	t.Setenv("FORGE_AGENT", "")

	projRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projRoot, ".forge", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projRoot, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644); err != nil {
		t.Fatal(err)
	}
	outer := t.TempDir() // process cwd: deliberately NOT a forge project
	originalWd, _ := os.Getwd()
	if err := os.Chdir(outer); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })

	// payload 用 json.Marshal 构造——Windows 路径反斜杠在 JSON stdin 里的转义
	// 才稳。
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUseFailure",
		"tool_name":       "Shell",
		"conversation_id": "conv-cursor-wsr-1",
		"failure_type":    "timeout",
		"workspace_roots": []string{projRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	tmpStdin, err := os.CreateTemp("", "hook-stdin-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpStdin.Write(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpStdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = tmpStdin
	t.Cleanup(func() { os.Stdin = oldStdin; tmpStdin.Close(); os.Remove(tmpStdin.Name()) })

	captureStdout(t, func() { _ = runHook(&cobra.Command{}, []string{"failure-track"}) })

	entries := findTrackEntries(t, projRoot, checklog.CheckToolFailure)
	if len(entries) != 1 {
		t.Fatalf("workspace_roots must resolve the project from an outside cwd, CheckToolFailure entries = %d, want 1", len(entries))
	}
	if entries[0].SessionID != "conv-cursor-wsr-1" {
		t.Errorf("session = %q, want conversation_id adopted", entries[0].SessionID)
	}
	if entries[0].Meta["failure_type"] != "timeout" {
		t.Errorf("meta failure_type = %q, want timeout", entries[0].Meta["failure_type"])
	}
}

// TestHook_SubagentTrackCursorFieldsAdapted pins cursor's subagentStop dialect: subagent_type/status/result (cursor's spellings) must reach the attribution the CC-schema fields drive — before the fill they were dropped and every cursor entry recorded agent_type=unknown with 0 chars forever.
//
// TestHook_SubagentTrackCursorFieldsAdapted 钉住 cursor 的 subagentStop 方言：
// subagent_type/status/result（cursor 拼法）必须抵达 CC schema 字段驱动的归因
// ——填空之前它们被丢弃，每个 cursor 条目永远记 agent_type=unknown、0 字符。
func TestHook_SubagentTrackCursorFieldsAdapted(t *testing.T) {
	root := runHookWithStdin(t, "subagent-track",
		`{"hook_event_name":"SubagentStop","conversation_id":"conv-cursor-sas-1","subagent_type":"code-reviewer","status":"completed","result":"Reviewed 3 files."}`)
	entries := findTrackEntries(t, root, checklog.CheckSubagentStop)
	if len(entries) != 1 {
		t.Fatalf("CheckSubagentStop entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Meta["agent_type"] != "code-reviewer" {
		t.Errorf("meta agent_type = %q, want code-reviewer (from subagent_type)", e.Meta["agent_type"])
	}
	if e.Meta["status"] != "completed" {
		t.Errorf("meta status = %q, want completed", e.Meta["status"])
	}
	if want := fmt.Sprintf("%d", len("Reviewed 3 files.")); e.Meta["message_len"] != want {
		t.Errorf("message_len = %q, want %q (from result)", e.Meta["message_len"], want)
	}
	if !strings.Contains(e.Detail, "code-reviewer") || !strings.Contains(e.Detail, "Reviewed 3 files.") {
		t.Errorf("detail should carry cursor attribution, got: %q", e.Detail)
	}
}

// TestRunTestNudgeHook_WhitelistAndNonSourceIgnored pins the classification inputs: whitelist files (cmd/ entry point, main.go) and non-source files (.md) neither count toward the streak nor reset it — same rules as the task-verify gate (taskpipeline.ClassifyChangedPath single source of truth).
//
// TestRunTestNudgeHook_WhitelistAndNonSourceIgnored 钉住分类输入：白名单文件
// （cmd/ 入口、main.go）与非源码文件（.md）既不计入连写也不重置——与
// task-verify 门禁同规（taskpipeline.ClassifyChangedPath 单一真相源）。
func TestRunTestNudgeHook_WhitelistAndNonSourceIgnored(t *testing.T) {
	root := trackTestProject(t)
	const sid = "sess-tn-2"
	resetNudgeState(t, sid)
	startTrackTask(t, root, sid, "feat/nudge-class")

	for _, p := range []string{
		filepath.Join(root, "cmd", "x", "main.go"),
		filepath.Join(root, "README.md"),
	} {
		if out := writeSourceForNudge(t, root, sid, p); out != "" {
			t.Errorf("whitelist/non-source write must be silent (%s), got: %q", p, out)
		}
	}
	// 两次白名单 + 一次文档写入，随后两次真实源码写入——仍低于阈值（只有两个
	// .go 文件被计数）。
	wantSilent(t, `source write #1 must be silent`, writeSourceForNudge(t, root, sid, filepath.Join(root, "x.go")))
	wantSilent(t, `source write #2 must be silent (2 counted < 3)`, writeSourceForNudge(t, root, sid, filepath.Join(root, "y.go")))
	// 第三次真实源码写入越过阈值。
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "z.go")); !strings.Contains(out, "test-discipline") {
		t.Errorf("source write #3 must fire the reminder, got: %q", out)
	}
}

// ---- tool-track（hook.go 分发 → toollog.jsonl）----
//
// hook_test.go 按域拆分时自该文件迁入；函数体改用共享 newHookProject/
// runHookCapture helper（hook_test.go）。

// TestHookToolTrackRecordsSkillInput pins scheme C: the tool-track hook (matcher Read|Skill|Agent|Grep|Glob) records tool_input (skill name) for Skill calls, so toollog audits can see which quality skill the agent loaded.
//
// TestHookToolTrackRecordsSkillInput 钉死方案 C：tool-track hook（matcher Read|Skill|Agent|Grep|Glob）
// 对 Skill 调用记录 tool_input（skill 名），让 toollog 审计能看到 agent 加载了哪个质量技能。
// Read 仍省略 tool_input（频繁，gate 只需 tool_name+timestamp）；Skill/Agent 填 tool_input
// 让"质量 skill 是否被驱动"可追溯（advisory 语境下质量 skill 0 触发的根因可追溯）。
func TestHookToolTrackRecordsSkillInput(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := newHookProject(t)

	runHookCapture(t, "tool-track",
		`{"hook_event_name":"PostToolUse","tool_name":"Skill","tool_input":{"name":"test-discipline"}}`)

	// toollog 写到用户级 DataDir（forgedata.DataDirFor），同 checklog 路径惯例——
	// 绝不写项目树。
	toollogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "toollog.jsonl")
	data, err := os.ReadFile(toollogPath)
	if err != nil {
		t.Fatalf("toollog.jsonl 未生成（Skill 调用未被 tool-track 记录——matcher 或 dispatch 缺失）: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"tool_name":"Skill"`) {
		t.Errorf("toollog 应含 tool_name=Skill 条目, got: %s", body)
	}
	if !strings.Contains(body, "test-discipline") {
		t.Errorf("toollog 应含 skill 名 test-discipline（方案 C：Skill tool_input 须记录）, got: %s", body)
	}
}

// TestHookToolTrackRecordsReadFilePath pins the production shape of Read tool_input (2026-08-16 review HIGH-1): tool-track records a minimal {"file_path":...} so the funnel join (skillseval.BuildTriggerFunnel → readFilePath) can attribute "loaded the skill after the trigger hit".
//
// TestHookToolTrackRecordsReadFilePath 钉死 Read tool_input 的生产形状（2026-08-16 审查
// HIGH-1）：tool-track 记最小 {"file_path":...}，让漏斗 join（skillseval.BuildTriggerFunnel
// → readFilePath）能归因「命中后加载了该 skill」。修复前 Read 完全省略 tool_input，该
// join 在生产数据上结构性死亡，而漏斗单测用手工 marshal 的输入照样全绿。本测试是形状
// 契约的生产侧一半；funnel_test.go 的 mkRead 是 join 侧一半——两者不得再分叉。
func TestHookToolTrackRecordsReadFilePath(t *testing.T) {
	cases := []struct {
		name   string
		stdin  string
		assert func(t *testing.T, body string)
	}{
		{
			// 最小形状 + 最小性：原始 input 带 limit，落盘不得含——写入方回归成记完整
			// input 时此臂变红（复审 LOW(i)）。
			name:  "minimal shape",
			stdin: `{"hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"file_path":"src/main.go","limit":50}}`,
			assert: func(t *testing.T, body string) {
				// tool_input 在 JSONL 里是转义过的内嵌 JSON（\"file_path\":\"...\"），
				// 断言按裸 token 查——字段名与路径值都在即覆盖最小形状语义。
				if !strings.Contains(body, "file_path") || !strings.Contains(body, "src/main.go") {
					t.Errorf("Read 的 tool_input 须记最小 {\"file_path\":...}（漏斗 join 依赖，审查 HIGH-1）, got: %s", body)
				}
				if strings.Contains(body, "limit") {
					t.Errorf("最小形状契约：limit 等其余字段不得落盘（lean 契约，复审 LOW(i)）, got: %s", body)
				}
			},
		},
		{
			// 零回归臂：input 无 file_path（旧 host / 解析失败形状）→ 条目照旧无
			// tool_input（omitempty 整键缺席），与修复前逐字节等价（复审 LOW(ii)）。
			name:  "no file_path stays lean",
			stdin: `{"hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"offset":10}}`,
			assert: func(t *testing.T, body string) {
				if !strings.Contains(body, `"tool_name":"Read"`) {
					t.Errorf("toollog 应含 tool_name=Read 条目, got: %s", body)
				}
				if strings.Contains(body, "tool_input") || strings.Contains(body, "offset") {
					t.Errorf("无 file_path 的 Read 应照旧省略整个 tool_input 键（零回归）, got: %s", body)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FORGE_DATA_HOME", t.TempDir())
			tmpDir := newHookProject(t)

			runHookCapture(t, "tool-track", tc.stdin)

			toollogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "toollog.jsonl")
			data, err := os.ReadFile(toollogPath)
			if err != nil {
				t.Fatalf("toollog.jsonl 未生成: %v", err)
			}
			tc.assert(t, string(data))
		})
	}
}

// TestHookToolTrackRecordsGrepInput pins the production shape of Grep/Glob tool_input (2026-08-23 drift fix): like Bash/Skill/Agent, exploration calls record the full tool input truncated — the pattern and path are the audit payload (which regex, which tree).
//
// TestHookToolTrackRecordsGrepInput 钉死 Grep/Glob tool_input 的生产形状
// （2026-08-23 漂移修复）：与 Bash/Skill/Agent 同待遇记完整 input 截断——
// pattern 与 path 就是审计载荷（查了什么正则、扫了哪棵树）。Read 保持最小
// 形状（漏斗 join）；Grep/Glob 不进任何漏斗，lean 契约不适用。条目本身即
// ExploreCounts 所数——没有 input 也照样计数，但无 input 的探索日志对
// 行为/风险审计毫无价值。
func TestHookToolTrackRecordsGrepInput(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := newHookProject(t)

	runHookCapture(t, "tool-track",
		`{"hook_event_name":"PostToolUse","tool_name":"Grep","tool_input":{"pattern":"DSH_HOME","path":"internal/"}}`)

	toollogPath := filepath.Join(forgedata.DataDirFor(tmpDir), "toollog.jsonl")
	data, err := os.ReadFile(toollogPath)
	if err != nil {
		t.Fatalf("toollog.jsonl 未生成: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"tool_name":"Grep"`) {
		t.Fatalf("toollog 应含 tool_name=Grep 条目（matcher 补 Grep/Glob 的记录面）, got: %s", body)
	}
	if !strings.Contains(body, "DSH_HOME") || !strings.Contains(body, "internal/") {
		t.Errorf("Grep 的 tool_input 须记 pattern+path（审计载荷，与 Bash/Skill/Agent 同待遇）, got: %s", body)
	}
}

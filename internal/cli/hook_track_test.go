package cli

// hook_track_test.go — guards for the three #4-A observation hooks (failure-track /
// subagent-track / test-nudge, hook_track.go). Three dimensions per hook:
//   - the checklog observation lands (Check name, observation semantics);
//   - the advisory channel fires only when it should (compile marker / threshold),
//     never blocks (no HookBlockError);
//   - silence contracts hold (no active task → no state file; non-compile failure →
//     no stdout; SubagentStop → pure record).
//
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
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// trackTestProject isolates FORGE_DATA_HOME and returns a temp project root. The
// three hooks record via checklog.Record(root, ...) which resolves the DataDir
// from the isolated home, so no real-home pollution.
//
// trackTestProject 隔离 FORGE_DATA_HOME 并返回临时项目根。三个 hook 经
// checklog.Record(root, ...) 记录，DataDir 从隔离 home 解析——不污染真实 home。
func trackTestProject(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	return t.TempDir()
}

// startTrackTask binds an active task to the session (taskRefForSession then
// returns non-empty), so test-nudge's active-task gate opens. Same setup shape as
// review_status_test.go's renderStatusWithEvidence: SetActiveTaskRef + a minimal
// incomplete TaskState (ActiveTaskState requires the state file to load and be
// uncompleted).
//
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

// resetNudgeState removes the per-session counter file under os.TempDir(). The
// state deliberately lives in the REAL temp dir (production choice: short-lived,
// OS-cleaned), so a leftover from an earlier test run would corrupt streak counts —
// clear it before AND after each nudge test for cross-run isolation.
//
// resetNudgeState 删除 os.TempDir() 下的 per-session 计数器文件。该状态刻意住在
// 真实 temp 目录（生产选择：短命、OS 清理），上一次测试运行的残留会污染连写计数
// ——每个 nudge 测试前后都清一次，做跨运行隔离。
func resetNudgeState(t *testing.T, sessionID string) {
	t.Helper()
	path := filepath.Join(os.TempDir(), "forge-testnudge", util.SanitizeSessionID(sessionID)+".json")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
}

// findTrackEntries loads all checklog entries for root filtered by check name.
//
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

// TestRunFailureTrackHook_RecordsAndNudgesOnCompileFailure pins the compile-failure
// path: the observation lands as CheckToolFailure AND the factual compile-fix-loop
// pointer rides the allow-with-detail channel (hookSpecificOutput JSON on stdout —
// never a block: PostToolUseFailure cannot un-fail the command).
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
	// Name-only pointer contract (2026-08-25 prompt-copy fix): the nudge names the
	// skill in natural language; a repo-relative skills/... path is a 404 outside
	// the Forge repo, so it must never appear.
	//
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
	// Emit path: Delivered must be stamped (value follows the hostcap channel
	// verdict — presence is the contract here).
	//
	// 发射路径：Delivered 必须落章（值随 hostcap 通道裁定——此处契约是存在性）。
	if entries[0].Delivered == nil {
		t.Error("compile-marker path emits, so Delivered must be stamped (non-nil)")
	}
}

// TestRunFailureTrackHook_GenericFailureRecordsSilently pins the silence contract:
// a non-compile failure (network timeout) still records the observation (the model
// already sees the failed output in its own tool result) but emits nothing.
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
	// Silence contract includes the delivery stamp: nothing was emitted, so
	// Delivered stays nil (review 2026-08-22 — funnel counts Delivered=true only,
	// an unstamped-emit inflation was the bug class).
	//
	// 静默契约含送达章：零发射则 Delivered 保持 nil（复审 2026-08-22——漏斗
	// 只计 Delivered=true，未发射却盖章正是那个 bug 类）。
	entries := findTrackEntries(t, root, checklog.CheckToolFailure)
	if len(entries) != 1 || entries[0].Delivered != nil {
		t.Errorf("non-emitting path must leave Delivered nil, got %+v", entries)
	}
}

// TestRunSubagentTrackHook_RecordsAttribution pins the SubagentStop observation:
// agent_id/agent_type land in Meta, the delivery summary carries length + first
// line (never the full message — checklog is not a message archive), and nothing
// is emitted (SubagentStop stdout is not a context channel).
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

// TestRunSubagentTrackHook_UnknownTypeFallsBack pins the agent_type fallback:
// a missing agent_type records "unknown" instead of an empty Meta value (absent-key
// semantics live in the entry's absence, not in empty strings).
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

// writeSourceForNudge simulates one PostToolUse Write event through runTestNudgeHook
// and returns the captured stdout.
//
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

// TestRunTestNudgeHook_SilentWithoutTask pins the active-task gate: no bound task →
// not even a counter state file (exploratory editing outside a task is not the
// nudge's business; the test-coverage gate does not apply either).
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

// TestRunTestNudgeHook_ThresholdNudgeAndReset pins the streak contract end-to-end:
// writes 1..2 are silent, the 3rd fires ONE factual reminder (test-discipline), the
// 4th stays silent (per-streak single nudge — no spam), a test-file write resets the
// streak, and the next 3-source-write streak fires again. The observation lands as
// CheckTestNudge with the streak count.
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

	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "a.go")); out != "" {
		t.Errorf("write #1 must be silent, got: %q", out)
	}
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "b.go")); out != "" {
		t.Errorf("write #2 must be silent, got: %q", out)
	}
	out3 := writeSourceForNudge(t, root, sid, filepath.Join(root, "c.go"))
	if !strings.Contains(out3, "test-discipline") {
		t.Errorf("write #3 (threshold) must fire the test-discipline reminder, got: %q", out3)
	}
	// Same name-only pointer contract as failure-track (2026-08-25 prompt-copy fix):
	// natural-language skill reference, no repo-relative skills/ path.
	//
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
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "d.go")); out != "" {
		t.Errorf("write #4 after the nudge must stay silent (one nudge per streak), got: %q", out)
	}

	// Test write resets the streak AND re-arms the nudge.
	//
	// 测试写入重置连写并重新武装提示。
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "a_test.go")); out != "" {
		t.Errorf("test-file write must be silent, got: %q", out)
	}
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "e.go")); out != "" {
		t.Errorf("post-reset write #1 must be silent, got: %q", out)
	}
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "f.go")); out != "" {
		t.Errorf("post-reset write #2 must be silent, got: %q", out)
	}
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

// TestHook_FailureTrackCursorPayloadAdapted pins the #4-A follow-up's payload
// adaptation for cursor: cursor's postToolUseFailure classifies via failure_type
// instead of an error string and carries conversation_id instead of session_id.
// Through the FULL runHook path: (1) a classification-only payload gets Error
// filled from failure_type (recorded in Meta for class aggregation) and stays
// silent — the enum matches no compile marker, so no false nudge; (2) when a real
// error string IS present it wins (fill-empty), the marker fires, and the
// compile-fix-loop pointer is emitted with a Delivered stamp; (3) cursor's
// documented dual-field shape (error_message text + failure_type enum) fills
// Error with the TEXT — enum only rides in Meta; (4) the observation
// lands session-attributed via conversation_id.
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
	// Phase 1: classification-only payload (cursor's documented shape) — silent
	// observation, Error filled from failure_type, session via conversation_id.
	//
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

	// Phase 2: real error text present — it wins over failure_type (fill-empty),
	// the compile marker fires, and the pointer is emitted with a Delivered stamp.
	//
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

	// Phase 3: cursor's documented dual-field shape — error_message text WITH the
	// failure_type enum. The text fills Error (beats the enum), the compile marker
	// fires on the real text, and Meta still records the class.
	//
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

// TestHook_CursorWorkspaceRootsResolvesProject pins the MAJOR-1 fix: cursor's
// user-level hooks run from ~/.cursor and its payload has NO cwd — the project
// only enters via workspace_roots. With the process cwd OUTSIDE the project, a
// failure-track event carrying workspace_roots must still resolve the project
// and land the observation (before the fix: findProjectRoot failed → silent
// allow → nothing ever recorded).
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

	// Payload marshaled via json.Marshal so Windows path backslashes escape
	// correctly in the JSON stdin.
	//
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

// TestHook_SubagentTrackCursorFieldsAdapted pins cursor's subagentStop dialect:
// subagent_type/status/result (cursor's spellings) must reach the attribution
// the CC-schema fields drive — before the fill they were dropped and every
// cursor entry recorded agent_type=unknown with 0 chars forever.
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

// TestRunTestNudgeHook_WhitelistAndNonSourceIgnored pins the classification inputs:
// whitelist files (cmd/ entry point, main.go) and non-source files (.md) neither
// count toward the streak nor reset it — same rules as the task-verify gate
// (taskpipeline.ClassifyChangedPath single source of truth).
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
	// Two whitelist + one doc write, then two real source writes — still below
	// threshold (only the two .go files counted).
	//
	// 两次白名单 + 一次文档写入，随后两次真实源码写入——仍低于阈值（只有两个
	// .go 文件被计数）。
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "x.go")); out != "" {
		t.Errorf("source write #1 must be silent, got: %q", out)
	}
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "y.go")); out != "" {
		t.Errorf("source write #2 must be silent (2 counted < 3), got: %q", out)
	}
	// Third real source write crosses the threshold.
	//
	// 第三次真实源码写入越过阈值。
	if out := writeSourceForNudge(t, root, sid, filepath.Join(root, "z.go")); !strings.Contains(out, "test-discipline") {
		t.Errorf("source write #3 must fire the reminder, got: %q", out)
	}
}

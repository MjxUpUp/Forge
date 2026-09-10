package harnessaudit

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// Fixtures mirror the two-machine audit shapes (docs/design/harness-fixes-a-g-2026-09.md M):
// every metric is a pure function over constructed entries/calls/tasks/events — no disk.
//
// 夹具镜像两机审计的数据形态（设计 M）：每个指标都是对构造出的 entries/calls/tasks/events
// 的纯函数——无磁盘。

var t0 = time.Date(2026, 9, 7, 22, 50, 0, 0, time.UTC)

func trig(skill, event, session string, at time.Time, meta map[string]string) checklog.Entry {
	return checklog.Entry{Check: checklog.CheckSkillTrigger, Passed: true, Level: checklog.LevelAdvisory,
		SessionID: session, RecordedAt: at, Detail: checklog.DetailForSkillTrigger(skill, event, "keywords 命中"), Meta: meta}
}

func call(tool, session, input string, at time.Time) toolusage.ToolCall {
	return toolusage.ToolCall{ToolName: tool, SessionID: session, ToolInput: input, Timestamp: at}
}

func TestDedupeCalls_PrePostDoubleRecord(t *testing.T) {
	calls := []toolusage.ToolCall{
		call("Bash", "s1", `{"command":"go test ./..."}`, t0),
		call("Bash", "s1", `{"command":"go test ./..."}`, t0.Add(500*time.Millisecond)), // Post 双记
		call("Bash", "s1", `{"command":"go test ./..."}`, t0.Add(30*time.Second)),       // 真实重跑
		call("Bash", "s2", `{"command":"go test ./..."}`, t0.Add(time.Second)),          // 他会话
	}
	got := DedupeCalls(calls, DefaultCaliber().DedupWindow)
	if len(got) != 3 {
		t.Fatalf("dedupe = %d calls, want 3 (Post duplicate collapsed, rerun and other session kept)", len(got))
	}
}

func TestSkillTriggerMetrics_ChannelConversionAndPrecision(t *testing.T) {
	entries := []checklog.Entry{
		trig("merge-release-choreography", "UserPromptSubmit", "s1", t0, nil),
		trig("implementation-discipline", "UserPromptSubmit", "s1", t0.Add(time.Minute), nil),
		trig("test-discipline", "PreToolUse", "s1", t0.Add(2*time.Minute), nil),
		trig("verification-driver", "PostToolUse", "s1", t0.Add(3*time.Minute), nil),
		trig("verification-driver", "PostToolUse", "s1", t0.Add(10*time.Minute), nil),
		trig("compile-fix-loop", "PostToolUse", "s2", t0.Add(4*time.Minute), map[string]string{
			checklog.MetaKeyTriggerMode: checklog.TriggerModeInline, checklog.MetaKeyFollowPattern: `go build`}),
	}
	calls := []toolusage.ToolCall{
		// merge-release-choreography 命中后 2 分钟 Read SKILL.md → 加载
		call("Read", "s1", `{"file_path":"C:\\Users\\x\\.forge\\skills-cache\\embedded\\merge-release-choreography\\SKILL.md"}`, t0.Add(2*time.Minute)),
		// verification-driver 第一次触发：同会话 ±3s 内的 Bash 是测试命令 → 精度命中
		call("Bash", "s1", `{"command":"go test ./internal/x/ 2>&1 | tail -3"}`, t0.Add(3*time.Minute-time.Second)),
		// 第二次触发：邻近 Bash 不是测试命令（输出关键词误触形态）
		call("Bash", "s1", `{"command":"grep -rn compile ./"}`, t0.Add(10*time.Minute-time.Second)),
		// inline 命中后 5 分钟出现匹配 follow 的 Bash → 跟随
		call("Bash", "s2", `{"command":"go build ./..."}`, t0.Add(9*time.Minute)),
	}
	m := SkillTriggerMetrics(entries, calls, 2.0, DefaultCaliber())
	if m.Total != 6 || m.DailyTriggers != 3.0 {
		t.Fatalf("total=%d daily=%.1f, want 6 / 3.0", m.Total, m.DailyTriggers)
	}
	ups := m.ByEvent["UserPromptSubmit"]
	if ups.Hits != 2 || ups.Loaded != 1 {
		t.Fatalf("UPS = %+v, want hits 2 loaded 1", ups)
	}
	if m.ByEvent["PreToolUse"].Loaded != 0 || m.ByEvent["PostToolUse"].Hits != 3 {
		t.Fatalf("channel stats off: %+v", m.ByEvent)
	}
	if m.Conversion.Num != 1 || m.Conversion.Den != 6 {
		t.Fatalf("overall conversion = %+v, want 1/6", m.Conversion)
	}
	if m.VerificationDriverPrecision.Num != 1 || m.VerificationDriverPrecision.Den != 2 {
		t.Fatalf("verification-driver precision = %+v, want 1/2", m.VerificationDriverPrecision)
	}
	if m.InlineFollow == nil || m.InlineFollow.Num != 1 || m.InlineFollow.Den != 1 {
		t.Fatalf("inline follow = %+v, want 1/1", m.InlineFollow)
	}
}

func TestSkillTriggerMetrics_NoInlineRowsMeansNA(t *testing.T) {
	m := SkillTriggerMetrics([]checklog.Entry{trig("x", "Stop", "s", t0, nil)}, nil, 1, DefaultCaliber())
	if m.InlineFollow != nil {
		t.Fatalf("no inline rows → InlineFollow must be nil (n/a), got %+v", m.InlineFollow)
	}
}

func TestNextHintMetrics_Adoption(t *testing.T) {
	entries := []checklog.Entry{
		{Check: checklog.CheckNextHint, SessionID: "s1", RecordedAt: t0, Meta: map[string]string{checklog.MetaKeySuggested: "forge task gate task-verify --ref feat/x"}},
		{Check: checklog.CheckNextHint, SessionID: "s1", RecordedAt: t0.Add(time.Hour), Meta: map[string]string{checklog.MetaKeySuggested: "forge task complete --ref feat/x"}},
	}
	calls := []toolusage.ToolCall{
		call("Bash", "s1", `{"command":"cd E:/Forge && forge task gate task-verify --ref feat/x"}`, t0.Add(3*time.Minute)),
		call("Bash", "s1", `{"command":"git status"}`, t0.Add(time.Hour+time.Minute)),
	}
	m := NextHintMetrics(entries, calls, DefaultCaliber())
	if m.Adoption.Num != 1 || m.Adoption.Den != 2 {
		t.Fatalf("adoption = %+v, want 1/2", m.Adoption)
	}
}

func TestGateCmdFormMetrics(t *testing.T) {
	calls := []toolusage.ToolCall{
		call("Bash", "s", `{"command":"forge task gate task-verify --ref x"}`, t0),
		call("Bash", "s", `{"command":"cd E:/Forge && forge task gate task-implement --ref x 2>&1 | tail -3"}`, t0.Add(time.Minute)),
		call("Bash", "s", `{"command":"forge task gate task-verify --ref x; forge task gate task-complete --ref x"}`, t0.Add(2*time.Minute)),
		call("Bash", "s", `{"command":"forge task complete --ref x | grep -E \"completed|Score\""}`, t0.Add(3*time.Minute)),
		call("Bash", "s", `{"command":"go test ./..."}`, t0.Add(4*time.Minute)), // 非门禁：不计
	}
	m := GateCmdFormMetrics(calls)
	if m.Total != 4 || m.Standalone != 1 || m.PipeTruncated != 2 || m.Semicolon != 1 || m.MultiGate != 1 || m.GrepMasked != 1 {
		t.Fatalf("form counts off: %+v", m)
	}
	if m.C1.Num != 2 || m.C2.Num != 2 || m.C3.Num != 1 {
		t.Fatalf("C1/C2/C3 = %+v / %+v / %+v, want 2/4, 2/4, 1/4", m.C1, m.C2, m.C3)
	}
}

func sealedTask(ref string, start time.Time, sealAfter time.Duration) *tasktypes.TaskState {
	s := &tasktypes.TaskState{TaskRef: ref, StartedAt: start, SessionID: "owner"}
	for _, g := range []string{tasktypes.GateImplement, tasktypes.GateVerify, tasktypes.GateComplete} {
		s.RecordGateResult(g, true, "")
	}
	s.History[2].CompletedAt = start.Add(sealAfter)
	return s
}

func TestAttributionMetrics_PostSealAndSessionShare(t *testing.T) {
	task := sealedTask("fix/x", t0, time.Hour)
	seal := t0.Add(time.Hour)
	entries := []checklog.Entry{
		{Check: checklog.CheckTaskVerify, TaskRef: "fix/x", SessionID: "owner", RecordedAt: t0.Add(30 * time.Minute), Meta: map[string]string{checklog.MetaKeyResolvePath: "active-file"}},
		// complete 仪式行：封印后 1 分钟（宽限内）——探针会打标，但不算泄漏
		{Check: checklog.CheckTaskComplete, TaskRef: "fix/x", SessionID: "owner", RecordedAt: seal.Add(time.Minute), Meta: map[string]string{checklog.MetaKeyPostSeal: "true"}},
		{Check: checklog.CheckTaskVerify, TaskRef: "fix/x", SessionID: "", RecordedAt: t0.Add(20 * time.Hour), Meta: map[string]string{checklog.MetaKeyPostSeal: "true", checklog.MetaKeyResolvePath: "workspace"}},
		{Check: checklog.CheckTaskVerify, TaskRef: "fix/x", SessionID: "", RecordedAt: t0.Add(21 * time.Hour)}, // 修复前的旧行：无探针但落在封印后
		{Check: checklog.CheckAutoCompile, TaskRef: "", SessionID: "", RecordedAt: t0.Add(2 * time.Minute)},
	}
	m := AttributionMetrics(entries, []*tasktypes.TaskState{task}, DefaultCaliber())
	if m.PostSealRows != 2 || m.TasksWithPostSealRows != 1 {
		t.Fatalf("post-seal leak = %d rows / %d tasks, want 2 / 1 (ceremony row inside grace excluded)", m.PostSealRows, m.TasksWithPostSealRows)
	}
	if m.ProbeFlaggedRows != 2 {
		t.Fatalf("probe flagged = %d, want 2 (raw Meta count includes the ceremony row)", m.ProbeFlaggedRows)
	}
	if m.NoSessionShare.Num != 3 || m.NoSessionShare.Den != 5 {
		t.Fatalf("no-session share = %+v, want 3/5", m.NoSessionShare)
	}
	if m.ResolvePathCounts["active-file"] != 1 || m.ResolvePathCounts["workspace"] != 1 {
		t.Fatalf("resolve_path counts = %v", m.ResolvePathCounts)
	}
}

func TestHazardMetrics_DedupAndRelease(t *testing.T) {
	fp := hazard.Fingerprint("git push origin --force")
	events := []hazard.Event{
		{Ts: t0, Type: hazard.EventBlock, Fingerprint: fp, SessionID: "s1"},
		{Ts: t0.Add(time.Second), Type: hazard.EventBlock, Fingerprint: fp, SessionID: "s1"}, // 双投递
		{Ts: t0.Add(time.Minute), Type: hazard.EventConfirm, Fingerprint: fp},
		{Ts: t0.Add(2 * time.Minute), Type: hazard.EventRelease, Fingerprint: fp},
		{Ts: t0.Add(time.Hour), Type: hazard.EventBlock, Fingerprint: hazard.Fingerprint("shred x"), SessionID: "s1"},
	}
	m := HazardMetrics(events, DefaultCaliber())
	if m.Blocks != 3 || m.DoubleDeliveries != 1 || m.Incidents != 2 {
		t.Fatalf("blocks=%d dup=%d incidents=%d, want 3/1/2", m.Blocks, m.DoubleDeliveries, m.Incidents)
	}
	if m.ReleasedIncidents.Num != 1 || m.ReleasedIncidents.Den != 2 {
		t.Fatalf("released = %+v, want 1/2", m.ReleasedIncidents)
	}
}

func TestCoverageMetrics_Conversion(t *testing.T) {
	entries := []checklog.Entry{
		{Check: checklog.CheckName("test-coverage-gate"), TaskRef: "a", Level: checklog.LevelFail, RecordedAt: t0},
		{Check: checklog.CheckName("test-coverage-gate"), TaskRef: "a", Level: checklog.LevelPass, RecordedAt: t0.Add(time.Minute)},
		{Check: checklog.CheckName("test-coverage-gate"), TaskRef: "b", Level: checklog.LevelFail, RecordedAt: t0},
		{Check: checklog.CheckName("test-coverage-gate"), TaskRef: "b", Level: checklog.LevelFail, RecordedAt: t0.Add(time.Minute)},
		{Check: checklog.CheckCheatScan, TaskRef: "b", Level: checklog.LevelFail, RecordedAt: t0},
		{Check: checklog.CheckUnusedScan, TaskRef: "b", Level: checklog.LevelFail, RecordedAt: t0},
	}
	m := CoverageMetrics(entries)
	if m.CoverageFixAfterFail.Num != 1 || m.CoverageFixAfterFail.Den != 3 {
		t.Fatalf("coverage conversion = %+v, want 1/3", m.CoverageFixAfterFail)
	}
	if m.CheatScanFails != 1 || m.UnusedScanFails != 1 {
		t.Fatalf("scan fails = %d/%d", m.CheatScanFails, m.UnusedScanFails)
	}
}

func TestBuild_EndToEndShape(t *testing.T) {
	r := Build(Input{
		Entries: []checklog.Entry{trig("x", "UserPromptSubmit", "s", t0, nil)},
		Calls:   []toolusage.ToolCall{call("Bash", "s", `{"command":"forge task gate task-verify --ref x"}`, t0.Add(time.Minute))},
		Version: "test",
	})
	if r.ForgeVersion != "test" || r.Caliber.DedupWindow != DefaultCaliber().DedupWindow {
		t.Fatalf("report header off: %+v", r)
	}
	if r.Window.Days <= 0 {
		t.Fatalf("window days must be positive, got %v", r.Window.Days)
	}
	if r.C.Total != 1 || r.A.Total != 1 {
		t.Fatalf("sections not populated: C=%+v A=%+v", r.C, r.A)
	}
}

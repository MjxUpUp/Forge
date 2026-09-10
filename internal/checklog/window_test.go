package checklog

import (
	"testing"
	"time"
)

// Evidence-window tests (docs/design/harness-fixes-a-g-2026-09.md E.4): one fix per scoring
// reader — ① ForTask gains until so the conclusion chain stops at the seal instant; ② the
// session-level latest-value reader keeps empty-SessionID rows only when they belong to the
// task being scored, instead of unconditionally mixing them into any task.
//
// 证据窗口测试（docs/design/harness-fixes-a-g-2026-09.md E.4）：两个评分读取点各修一处——
// ① ForTask 增 until，结论证据链截断在封印时刻之后追加的行；② 会话级最新值读方对
// SessionID 为空的条目改为按 TaskRef 归属保留，不再无条件混入任意任务。

func TestLoadForTaskUntil_ExcludesRowsAfterSeal(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 7, 22, 50, 0, 0, time.UTC)
	seal := base.Add(56 * time.Minute)
	writeEntry(t, dir, Entry{Check: CheckTaskVerify, Passed: true, TaskRef: "fix/x", SessionID: "s1", RecordedAt: base.Add(30 * time.Minute)})
	writeEntry(t, dir, Entry{Check: CheckTaskVerify, Passed: true, TaskRef: "fix/x", SessionID: "", RecordedAt: seal.Add(20 * time.Hour)})
	writeEntry(t, dir, Entry{Check: CheckName("hazard-guard"), Passed: false, TaskRef: "fix/x", SessionID: "", RecordedAt: seal.Add(22 * time.Hour)})

	all, err := LoadForTask(dir, "fix/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("LoadForTask (unbounded, trace semantics) = %d rows, want 3", len(all))
	}
	win, err := LoadForTaskUntil(dir, "fix/x", seal)
	if err != nil {
		t.Fatal(err)
	}
	if len(win) != 1 {
		t.Fatalf("LoadForTaskUntil(seal) = %d rows, want 1 (post-seal rows excluded)", len(win))
	}
	if !win[0].RecordedAt.Equal(base.Add(30 * time.Minute)) {
		t.Fatalf("kept wrong row: %+v", win[0])
	}
}

func TestForTaskUntil_EvidenceNotInflatedByPostSealClaims(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 7, 22, 50, 0, 0, time.UTC)
	seal := base.Add(56 * time.Minute)
	// 封印前：3 条确定性 + 1 条自述 → ratio 0.75 Strong
	for i := 0; i < 3; i++ {
		writeEntry(t, dir, Entry{Check: CheckAutoCompile, Passed: true, Source: EvidenceDeterministic, TaskRef: "fix/x", SessionID: "s1", RecordedAt: base.Add(time.Duration(i+1) * time.Minute)})
	}
	writeEntry(t, dir, Entry{Check: CheckTaskVerify, Passed: true, Source: EvidenceAgentClaim, TaskRef: "fix/x", SessionID: "s1", RecordedAt: base.Add(10 * time.Minute)})
	// 封印后两天：10 次重复 verify 自述（env-hermetic-registry 实录形态）
	for i := 0; i < 10; i++ {
		writeEntry(t, dir, Entry{Check: CheckTaskVerify, Passed: true, Source: EvidenceAgentClaim, TaskRef: "fix/x", SessionID: "", RecordedAt: seal.Add(time.Duration(i+1) * time.Hour)})
	}

	unbounded, err := ForTask(dir, "fix/x")
	if err != nil {
		t.Fatal(err)
	}
	if unbounded.AgentClaim != 11 || unbounded.Deterministic != 3 {
		t.Fatalf("unbounded chain = det %d / claim %d, want 3/11 (documents the leak)", unbounded.Deterministic, unbounded.AgentClaim)
	}
	sealed, err := ForTaskUntil(dir, "fix/x", seal)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Deterministic != 3 || sealed.AgentClaim != 1 {
		t.Fatalf("sealed chain = det %d / claim %d, want 3/1", sealed.Deterministic, sealed.AgentClaim)
	}
	if sealed.Strength() != Strong {
		t.Fatalf("sealed strength = %v, want Strong", sealed.Strength())
	}
}

func TestLatestByCheckForTaskWindow_EmptySessionKeptOnlyForOwnTask(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 7, 22, 50, 0, 0, time.UTC)
	// 本任务本会话的 assertion-check 通过
	writeEntry(t, dir, Entry{Check: CheckAssertion, Passed: true, Checked: true, TaskRef: "fix/x", SessionID: "s1", RecordedAt: base.Add(time.Minute)})
	// 空 session、归属其他任务的 assertion-check 失败——旧语义会无条件保留并覆盖上一条
	writeEntry(t, dir, Entry{Check: CheckAssertion, Passed: false, Checked: true, TaskRef: "chore/other", SessionID: "", RecordedAt: base.Add(2 * time.Minute)})
	// 空 session、归属本任务——仍应保留（CLI 无 session 调用是常态）
	writeEntry(t, dir, Entry{Check: CheckAutoCompile, Passed: true, Checked: true, TaskRef: "fix/x", SessionID: "", RecordedAt: base.Add(3 * time.Minute)})
	// 封印后的本任务行——until 截断
	writeEntry(t, dir, Entry{Check: CheckAutoCompile, Passed: false, Checked: true, TaskRef: "fix/x", SessionID: "s1", RecordedAt: base.Add(2 * time.Hour)})

	got, err := LatestByCheckForTaskWindow(dir, "s1", "fix/x", base, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if e := got[CheckAssertion]; e == nil || !e.Passed || e.TaskRef != "fix/x" {
		t.Fatalf("assertion latest = %+v, want own-task pass (foreign empty-session row must not override)", e)
	}
	if e := got[CheckAutoCompile]; e == nil || !e.Passed {
		t.Fatalf("auto-compile latest = %+v, want pre-seal own-task pass (empty session, own TaskRef kept; post-seal excluded)", e)
	}

	// 兼容包装：旧签名语义不变（空 session 无条件保留、无上界）
	legacy, err := LatestByCheckForSessionSince(dir, "s1", base)
	if err != nil {
		t.Fatal(err)
	}
	if e := legacy[CheckAssertion]; e == nil || e.Passed {
		t.Fatalf("legacy wrapper must keep old semantics (foreign empty-session row wins by time), got %+v", e)
	}
}

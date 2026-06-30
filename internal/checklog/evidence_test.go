package checklog

import (
	"testing"
)

// TestSourceForCheck 锁定默认来源推断：hook/gate 代码产生的检查归 deterministic，
// task-verify gate 语义上是 advisory（agent 自检）归 agent-claim，未知检查默认
// deterministic（未来新增的 hook 记录点不致被误判为 agent 自述）。
func TestSourceForCheck(t *testing.T) {
	cases := []struct {
		check CheckName
		want  EvidenceSource
	}{
		{CheckAutoCompile, EvidenceDeterministic},
		{CheckAssertion, EvidenceDeterministic},
		{CheckFileSentinel, EvidenceDeterministic},
		{CheckTaskVerify, EvidenceAgentClaim},
		{CheckTaskComplete, EvidenceAgentClaim},
		{CheckName("some-future-check"), EvidenceDeterministic},
	}
	for _, c := range cases {
		if got := SourceForCheck(c.check); got != c.want {
			t.Fatalf(`SourceForCheck(%s) = %s, want %s`, c.check, got, c.want)
		}
	}
}

// TestRecordSetsSourceDefault 验证 Record 的兜底：调用方未显式标 Source 时，
// 按 CheckName 推断写入磁盘。这是历史记录点无需逐个改造即可进证据链的关键。
func TestRecordSetsSourceDefault(t *testing.T) {
	dir := t.TempDir()

	if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true}); err != nil {
		t.Fatal(err)
	}
	if err := Record(dir, &Entry{Check: CheckTaskVerify, Passed: true}); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadAll(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf(`LoadAll: err=%v, len=%d`, err, len(entries))
	}
	if entries[0].Source != EvidenceDeterministic {
		t.Fatalf(`auto-compile Record should default Source to deterministic, got %s`, entries[0].Source)
	}
	if entries[1].Source != EvidenceAgentClaim {
		t.Fatalf(`task-verify Record should default Source to agent-claim, got %s`, entries[1].Source)
	}
}

// TestBuildEvidenceChain_BucketsAndLegacyFallback 验证分桶 + 旧数据兜底：
// 显式标 Source 的条目按标注分桶，空 Source 的旧条目按 SourceForCheck 兜底后分桶。
func TestBuildEvidenceChain_BucketsAndLegacyFallback(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckTaskVerify, Source: EvidenceAgentClaim, TaskRef: "t"},
		{Check: CheckFileSentinel, Source: "", TaskRef: "t"}, // 旧数据无 Source，兜底为 deterministic
		{Check: CheckTaskGuard, Source: "", TaskRef: "t"},    // 旧数据，兜底为 deterministic
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 4 {
		t.Fatalf(`deterministic bucket: got %d, want 4 (auto-compile+assertion+file-sentinel+task-guard)`, ec.Deterministic)
	}
	if ec.AgentClaim != 1 {
		t.Fatalf(`agent-claim bucket: got %d, want 1 (task-verify)`, ec.AgentClaim)
	}
	if len(ec.Entries) != 5 {
		t.Fatalf(`entries preserved: got %d, want 5`, len(ec.Entries))
	}
}

// TestForTask_LoadsAndBuckets 端到端：Record 写入 → ForTask 加载聚合。
func TestForTask_LoadsAndBuckets(t *testing.T) {
	dir := t.TempDir()
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "feat/e"})
	Record(dir, &Entry{Check: CheckTaskVerify, Passed: true, TaskRef: "feat/e"})
	Record(dir, &Entry{Check: CheckAssertion, Passed: true, TaskRef: "feat/e"})

	ec, err := ForTask(dir, "feat/e")
	if err != nil {
		t.Fatal(err)
	}
	if ec.Deterministic != 2 || ec.AgentClaim != 1 {
		t.Fatalf(`ForTask buckets: deterministic=%d agent-claim=%d, want 2/1`, ec.Deterministic, ec.AgentClaim)
	}
}

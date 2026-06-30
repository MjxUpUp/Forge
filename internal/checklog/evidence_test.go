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

// TestStrengthClassification 锁定 Strength 档位与 Ratio/Total：完成声明的可信度按
// deterministic 占比离散成 review 可行动的档位（NoData/Unverified/Weak/Strong），阈值 0.5。
// 这是把"ratio 仅可观测"升级为"驱动 review 校准"的判定核心——档位决定是否给 reviewer
// 注入"核验声称的验证是否真跑过"的指令。
func TestStrengthClassification(t *testing.T) {
	cases := []struct {
		name       string
		det, claim int
		wantStr    EvidenceStrength
		wantRatio  float64
		wantTotal  int
	}{
		{`NoData: 零证据`, 0, 0, NoData, 0, 0},
		{`Unverified: 零 deterministic 全 agent-claim`, 0, 2, Unverified, 0, 2},
		{`Weak: agent-claim 占多数 (1/4=0.25)`, 1, 3, Weak, 0.25, 4},
		{`Weak 边界: 1/3≈0.33 仍 <0.5`, 1, 2, Weak, 1.0 / 3.0, 3},
		{`Strong 边界: 2/4=0.5 ≥0.5`, 2, 2, Strong, 0.5, 4},
		{`Strong: deterministic 占多数 (4/5=0.8)`, 4, 1, Strong, 0.8, 5},
	}
	for _, c := range cases {
		ec := EvidenceChain{Deterministic: c.det, AgentClaim: c.claim}
		if got := ec.Strength(); got != c.wantStr {
			t.Errorf(`%s: Strength=%s, want %s`, c.name, got, c.wantStr)
		}
		if got := ec.Total(); got != c.wantTotal {
			t.Errorf(`%s: Total=%d, want %d`, c.name, got, c.wantTotal)
		}
		if got := ec.Ratio(); got != c.wantRatio {
			t.Errorf(`%s: Ratio=%g, want %g`, c.name, got, c.wantRatio)
		}
	}
}

package checklog

import (
	"testing"
)

// TestSourceForCheck pins down default source inference: checks emitted by hook/gate code are deterministic,
// task-verify gate is semantically advisory (agent self-check) and thus agent-claim, and unknown checks default
// to deterministic (so future hook record points are not misclassified as agent self-report).
//
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

// TestRecordSetsSourceDefault verifies Record's fallback: when the caller does not explicitly set Source,
// it is inferred from CheckName and written to disk. This is the key reason legacy record points can join the evidence chain without per-point migration.
//
// TestRecordSetsSourceDefault 验证 Record 的兜底：调用方未显式标 Source 时，
// 按 CheckName 推断写入磁盘。这是历史记录点无需逐个改造即可进证据链的关键。
func TestRecordSetsSourceDefault(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

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

// TestBuildEvidenceChain_BucketsAndLegacyFallback verifies bucketing + legacy fallback:
// entries with explicit Source are bucketed by the label; old entries with empty Source are bucketed after fallback via SourceForCheck.
//
// TestBuildEvidenceChain_BucketsAndLegacyFallback 验证分桶 + 旧数据兜底：
// 显式标 Source 的条目按标注分桶，空 Source 的旧条目按 SourceForCheck 兜底后分桶。
func TestBuildEvidenceChain_BucketsAndLegacyFallback(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckTaskVerify, Source: EvidenceAgentClaim, TaskRef: "t"},
		// Legacy data without Source, falls back to deterministic.
		//
		{Check: CheckFileSentinel, Source: "", TaskRef: "t"}, // 旧数据无 Source，兜底为 deterministic
		// Legacy data, falls back to deterministic.
		//
		{Check: CheckTaskGuard, Source: "", TaskRef: "t"}, // 旧数据，兜底为 deterministic
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

// TestBuildEvidenceChain_ScopeDriftExcluded pins that CheckScopeDrift is excluded from evidence strength:
// it is an advisory observation (agent modified source outside the plan), not "verification evidence". Counting it would inflate Strength —
// drift is also usually a negative signal, so counting it as positive evidence is doubly wrong. Entries are still kept in Entries for forge trace display;
// only the bucketing count skips them. Without this guard, after scope-drift ships, tasks with drift would look better-evidenced.
//
// TestBuildEvidenceChain_ScopeDriftExcluded 钉住 CheckScopeDrift 不计入证据强度：
// 它是 advisory 观测（agent 改了计划外的源码），非"验证证据"。计入会虚高 Strength——
// drift 还常是负信号，当正向证据更是错上加错。条目仍保留在 Entries 供 forge trace 展示，
// 只是分桶计数跳过。无此守卫，scope-drift 上线后会让有 drift 的任务反而看起来证据更足。
func TestBuildEvidenceChain_ScopeDriftExcluded(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckScopeDrift, Source: EvidenceDeterministic, TaskRef: "t"},
		// Multiple drift entries are also excluded.
		//
		{Check: CheckScopeDrift, Source: EvidenceDeterministic, TaskRef: "t"}, // 多条 drift 也不计入
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 1 {
		t.Fatalf(`CheckScopeDrift 不应计入 deterministic: got %d, want 1（仅 auto-compile）`, ec.Deterministic)
	}
	if ec.AgentClaim != 0 {
		t.Fatalf(`agent-claim 应为 0, got %d`, ec.AgentClaim)
	}
	if len(ec.Entries) != 3 {
		t.Fatalf(`drift 条目仍应保留在 Entries 供 trace: got %d, want 3`, len(ec.Entries))
	}
}

// TestBuildEvidenceChain_CheatScanExcluded pins that CheckCheatScan is likewise excluded from evidence strength:
// mechanically detected suspected-cheat patterns are advisory observations, and hits are negative signals — treating them as positive evidence would inflate Strength
// in the wrong direction. Entries are still kept in Entries for trace.
//
// TestBuildEvidenceChain_CheatScanExcluded 钉住 CheckCheatScan 同样不计入证据强度：
// 机械检测的疑似作弊模式是 advisory 观测，命中是负信号——当正向证据会虚高 Strength
// 且错向。条目仍保留在 Entries 供 trace。
func TestBuildEvidenceChain_CheatScanExcluded(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckCheatScan, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckCheatScan, Source: EvidenceDeterministic, TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 1 {
		t.Fatalf(`CheckCheatScan 不应计入 deterministic: got %d, want 1`, ec.Deterministic)
	}
	if len(ec.Entries) != 3 {
		t.Fatalf(`cheat-scan 条目仍应保留在 Entries 供 trace: got %d, want 3`, len(ec.Entries))
	}
}

// TestBuildEvidenceChain_ReviewPassPlanFirstExcluded pins that CheckReviewPass / CheckPlanFirst
// are excluded from evidence-strength bucketing: both are observation-class markers (a review
// stamp was placed / a task had no plan recorded) — neither says any verification actually ran,
// so counting them as deterministic would inflate Strength. Entries are still kept for trace.
//
// TestBuildEvidenceChain_ReviewPassPlanFirstExcluded 钉住 CheckReviewPass / CheckPlanFirst
// 不计入证据强度：两者都是 observation 类标记（审查打戳 / 无方案记录）——都不代表任何
// 验证实跑，计入 deterministic 会虚高 Strength。条目仍保留供 trace。
func TestBuildEvidenceChain_ReviewPassPlanFirstExcluded(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckReviewPass, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckPlanFirst, Source: EvidenceDeterministic, TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 1 {
		t.Fatalf(`review-pass/plan-first 不应计入 deterministic: got %d, want 1`, ec.Deterministic)
	}
	if len(ec.Entries) != 3 {
		t.Fatalf(`条目仍应保留在 Entries 供 trace: got %d, want 3`, len(ec.Entries))
	}
}

// TestBuildEvidenceChain_UnusedScanExcluded pins that CheckUnusedScan is likewise excluded from evidence
// strength: it is an advisory observation (a newly-added exported symbol with zero production references —
// suspected wiring miss), not "verification evidence". Counting it would inflate Strength in the wrong direction
// (a wiring-miss signal must not read as positive evidence). Entries are still kept in Entries for forge trace.
//
// TestBuildEvidenceChain_UnusedScanExcluded 钉住 CheckUnusedScan 同样不计入证据强度：它是 advisory
// 观测（新增导出符号在生产行零引用——疑似接线缺失），非"验证证据"。计入会虚高 Strength 且方向
// 反了（接线缺失信号绝不能读成正向证据）。条目仍保留在 Entries 供 forge trace。
func TestBuildEvidenceChain_UnusedScanExcluded(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckUnusedScan, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckUnusedScan, Source: EvidenceDeterministic, TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 1 {
		t.Fatalf(`CheckUnusedScan 不应计入 deterministic: got %d, want 1（仅 auto-compile）`, ec.Deterministic)
	}
	if ec.AgentClaim != 0 {
		t.Fatalf(`agent-claim 应为 0, got %d`, ec.AgentClaim)
	}
	if len(ec.Entries) != 3 {
		t.Fatalf(`unused-scan 条目仍应保留在 Entries 供 trace: got %d, want 3`, len(ec.Entries))
	}
}

// TestBuildEvidenceChain_EscapeHatchExcludedAndFlags pins plan 5: CheckEscapeHatch is excluded from the
// deterministic bucket — escape is an observation of "skipped some gate", not "performed verification"; SourceForCheck defaults it to
// deterministic, so counting it would inflate Strength in the wrong direction (a signal meant to lower confidence would raise it instead). Set the
// UsedEscapeHatch flag for Strength to cap. Entries are still kept in Entries for trace.
//
// TestBuildEvidenceChain_EscapeHatchExcludedAndFlags 钉住方案5：CheckEscapeHatch 不计入
// deterministic 桶——逃生是"跳过了某 gate"的观察，非"做了验证"；SourceForCheck 默认把它
// 归 deterministic，计入会虚高 Strength 且方向反了（本该降信心的信号反而抬高它）。改设
// UsedEscapeHatch 标志供 Strength cap。条目仍留 Entries 供 trace。
func TestBuildEvidenceChain_EscapeHatchExcludedAndFlags(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckEscapeHatch, Source: EvidenceDeterministic, TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 2 {
		t.Fatalf(`CheckEscapeHatch 不应计入 deterministic: got %d, want 2（auto-compile+assertion）`, ec.Deterministic)
	}
	if !ec.UsedEscapeHatch {
		t.Fatal(`UsedEscapeHatch = false, want true（有 escape-hatch 条目应置标志供 Strength cap）`)
	}
	if len(ec.Entries) != 3 {
		t.Fatalf(`escape-hatch 条目仍应保留在 Entries 供 trace: got %d, want 3`, len(ec.Entries))
	}
}

// TestStrength_EscapeHatchCapsToWeak pins plan 5: tasks that used an escape hatch have Strength capped at Weak even when deterministic is the
// majority (which would normally be Strong) — giving escape a cost, countering the "hard gate + global escape =
// fake hard gate" backlash. Downgrade rather than block, keeping escape legitimate while no longer free.
//
// TestStrength_EscapeHatchCapsToWeak 钉住方案5：用了逃生舱的任务，即便 deterministic 占
// 多数（本该 Strong），Strength 也 cap 到 Weak——让逃生有代价，对冲"硬门禁 + 全局逃生 =
// 假硬门禁"反噬。用降档而非阻断，既保逃生合法又让它不再免费。
func TestStrength_EscapeHatchCapsToWeak(t *testing.T) {
	// 4 deterministic + 1 agent-claim = ratio 0.8 → would normally be Strong, but escape used → capped to Weak.
	//
	// 4 deterministic + 1 agent-claim = ratio 0.8 → 本该 Strong，但用了逃生 → cap Weak
	ec := EvidenceChain{Deterministic: 4, AgentClaim: 1, UsedEscapeHatch: true}
	if got := ec.Strength(); got != Weak {
		t.Fatalf(`escape used + ratio 0.8: Strength=%s, want Weak (capped)`, got)
	}
	// Same data without escape → Strong (guard: cap only triggers on escape, does not affect normal tasks).
	//
	// 同样数据无逃生 → Strong（守卫：cap 只在逃生时触发，不误伤正常任务）
	ecNoEsc := EvidenceChain{Deterministic: 4, AgentClaim: 1}
	if got := ecNoEsc.Strength(); got != Strong {
		t.Fatalf(`no escape + ratio 0.8: Strength=%s, want Strong`, got)
	}
}

// TestForTask_LoadsAndBuckets is end-to-end: Record writes → ForTask loads and aggregates.
//
// TestForTask_LoadsAndBuckets 端到端：Record 写入 → ForTask 加载聚合。
func TestForTask_LoadsAndBuckets(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)
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

// TestStrengthClassification locks Strength tiers to Ratio/Total: the credibility of the done claim is discretized
// by deterministic proportion into review-actionable tiers (NoData/Unverified/Weak/Strong), threshold 0.5.
// This is the core judgment that upgrades "ratio is only observable" to "drives review calibration" — the tier determines whether to inject
// "verify whether the claimed verification actually ran" instructions into the reviewer.
//
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

// TestBuildEvidenceChain_UnknownSourceCountsAsAgentClaim pins the forgery-backdoor fix:
// checklog.jsonl is agent-writable, so an unknown Source value must NEVER fall into the
// deterministic bucket via a catch-all else. Credibility requires a positive match —
// anything not explicitly "deterministic" (after the empty-Source SourceForCheck fallback)
// is counted as agent-claim.
//
// TestBuildEvidenceChain_UnknownSourceCountsAsAgentClaim 钉死伪造后门修复：
// checklog.jsonl 是 agent 可写的，未知 Source 值绝不能经兜底 else 落进
// deterministic 桶。可信必须正向匹配——空 Source 经 SourceForCheck 兜底后仍未
// 正向命中 deterministic 的一律计为 agent-claim。
func TestBuildEvidenceChain_UnknownSourceCountsAsAgentClaim(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckTaskVerify, Source: EvidenceAgentClaim, TaskRef: "t"},
		// Forged/typoed source strings an agent could write into checklog.jsonl —
		// must be bucketed as agent-claim, not deterministic.
		//
		// agent 可能写进 checklog.jsonl 的伪造/笔误 source 字符串——
		// 必须计为 agent-claim，不是 deterministic。
		{Check: CheckAutoCompile, Source: EvidenceSource("deterministic-typo"), TaskRef: "t"},
		{Check: CheckAutoCompile, Source: EvidenceSource("DETERMINISTIC"), TaskRef: "t"},
		{Check: CheckAutoCompile, Source: EvidenceSource("hook-verified"), TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 1 {
		t.Fatalf(`未知 Source 不得计入 deterministic: got %d, want 1（仅正向命中的 auto-compile）`, ec.Deterministic)
	}
	if ec.AgentClaim != 4 {
		t.Fatalf(`未知 Source 应计入 agent-claim: got %d, want 4（task-verify + 3 个未知值）`, ec.AgentClaim)
	}
	if len(ec.Entries) != 5 {
		t.Fatalf(`entries preserved: got %d, want 5`, len(ec.Entries))
	}
}

// TestBuildEvidenceChain_WorkActivityEscapeDoesNotCap pins the precision fix: work-activity is a RHYTHM gate
// (requires tool calls between gates — prevents jumping straight to the gate without reading code), NOT a
// verification gate. Using it does NOT mean the "done" claim rests on skipped verification, so a work-activity
// escape-hatch entry must NOT set UsedEscapeHatch and Strength must NOT be capped. Refactor/migration/demolition
// tasks legitimately use the batch-refactor escape and typically have ample deterministic evidence (compiles,
// assertions, existing tests). Verification-class escapes (test-coverage/acceptance/skill-decisions) still cap.
//
// Without this fix, a refactor-heavy week inflates the blind-spot rate to ~50% and mis-fires RetrospectiveNudge on
// well-evidenced tasks — exactly the false signal this round of review uncovered.
//
// TestBuildEvidenceChain_WorkActivityEscapeDoesNotCap 钉住精度修复：work-activity 是节奏门禁
// （要求门禁间有工具调用——防 agent 不读代码直跳门禁），非验证门禁。用它不代表"完成"靠
// 跳过验证撑，故 work-activity escape-hatch 条目不得置 UsedEscapeHatch、Strength 不得 cap。
// 重构/迁移/拆除任务合理用批量重构逃生舱，且确定性证据通常充分（编译/断言/既有测试）。
// 验证类逃生舱（test-coverage/acceptance/skill-decisions）仍 cap。
//
// 无此修复，重构密集周会让盲区率虚高到 ~50%，对证据充分的任务误触
// RetrospectiveNudge——正是本轮回流审查发现的假信号。
func TestBuildEvidenceChain_WorkActivityEscapeDoesNotCap(t *testing.T) {
	// work-activity escape: rhythm gate, must NOT cap. 4 det + 1 escape(work-activity) →
	// escape excluded from bucketing, ratio stays 4/4=1.0, Strength=Strong (not capped).
	//
	// work-activity 逃生：节奏门禁，不得 cap。4 det + 1 escape(work-activity) →
	// escape 不计入分桶，ratio 仍 4/4=1.0，Strength=Strong（不被 cap）。
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckEscapeHatch, Source: EvidenceDeterministic, TaskRef: "t",
			Detail: `escape-hatch: work-activity gate bypassed (per-task override or FORGE_WORK_ACTIVITY=disable)`},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.UsedEscapeHatch {
		t.Fatalf(`work-activity escape 不应置 UsedEscapeHatch（节奏门禁非验证门禁）: got true`)
	}
	if got := ec.Strength(); got != Strong {
		t.Fatalf(`work-activity escape + ratio 1.0: Strength=%s, want Strong（不被 cap）`, got)
	}

	// Verification-class escape (test-coverage): still caps. Same evidence shape, escape detail is test-coverage.
	//
	// 验证类逃生（test-coverage）：仍 cap。同样证据形状，escape detail 是 test-coverage。
	entriesVerify := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckEscapeHatch, Source: EvidenceDeterministic, TaskRef: "t",
			Detail: `escape-hatch: test-coverage gate bypassed (per-task override or FORGE_TEST_COVERAGE=disable)`},
	}
	ecVerify := BuildEvidenceChain(entriesVerify, "t")
	if !ecVerify.UsedEscapeHatch {
		t.Fatal(`test-coverage escape 应置 UsedEscapeHatch（验证类，cap 触发）: got false`)
	}
	if got := ecVerify.Strength(); got != Weak {
		t.Fatalf(`test-coverage escape + ratio 1.0: Strength=%s, want Weak（被 cap）`, got)
	}
}

// TestBuildEvidenceChain_SkillTriggerExcluded pins that CheckSkillTrigger is excluded from evidence strength:
// a skill firing (passive injection) is an observation, not verification evidence — counting it would inflate
// Strength in the wrong direction (a skill firing says nothing about whether the task's verification actually ran).
// Entries are still kept in Entries for trace.
//
// TestBuildEvidenceChain_SkillTriggerExcluded 钉住 CheckSkillTrigger 不计入证据强度：
// skill 触发（被动注入）是观测，非验证证据——计入会虚高 Strength 且方向错（skill 触发
// 不说明本任务的验证真跑过）。条目仍留 Entries 供 trace。
func TestBuildEvidenceChain_SkillTriggerExcluded(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckSkillTrigger, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckSkillTrigger, Source: EvidenceDeterministic, TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 1 {
		t.Fatalf(`CheckSkillTrigger 不应计入 deterministic: got %d, want 1（仅 auto-compile）`, ec.Deterministic)
	}
	if len(ec.Entries) != 3 {
		t.Fatalf(`skill-trigger 条目仍应保留在 Entries 供 trace: got %d, want 3`, len(ec.Entries))
	}
}

// TestBuildEvidenceChain_KimiPluginStaleExcluded pins that CheckKimiPluginStale is excluded
// from evidence strength (code-review F1, 2026-08-15): the entry records that the kimi plugin
// install lags the binary — a distribution-health observation, not verification evidence.
// It carries TaskRef (recorded while a task is active), so without the exclusion
// LoadForTask+BuildEvidenceChain would bucket it as deterministic and inflate Strength off
// a once-daily distribution warning.
//
// TestBuildEvidenceChain_KimiPluginStaleExcluded 钉住 CheckKimiPluginStale 不计入证据强度
// （code-review F1，2026-08-15）：该条目记录 kimi plugin 安装落后于二进制——分发健康度
// 观测，非验证证据。它带 TaskRef（任务活跃期间记录），不排除的话 LoadForTask+
// BuildEvidenceChain 会把它分桶成 deterministic，让每日一次的分发告警虚增 Strength。
func TestBuildEvidenceChain_KimiPluginStaleExcluded(t *testing.T) {
	entries := []Entry{
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckKimiPluginStale, Source: EvidenceDeterministic, TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 1 {
		t.Fatalf(`CheckKimiPluginStale 不应计入 deterministic: got %d, want 1（仅 auto-compile）`, ec.Deterministic)
	}
	if len(ec.Entries) != 2 {
		t.Fatalf(`kimi-plugin-stale 条目仍应保留在 Entries 供 trace: got %d, want 2`, len(ec.Entries))
	}
}

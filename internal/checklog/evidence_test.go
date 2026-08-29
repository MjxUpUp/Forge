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

// TestBuildEvidenceChain_ObservationChecksExcluded table-drives the observation-check
// exclusions (the 12 former per-check *Excluded tests, merged 2026-08-30 slim-down).
// Shared invariant: every check below is an OBSERVATION (advisory finding / process
// marker / boundary event / distribution-health signal), not "verification evidence"
// — it must never feed the evidence-strength buckets, or Strength inflates in the
// wrong direction (negative signals would read as positive evidence, and every task
// would earn free deterministic entries at birth). Entries are still kept in Entries
// for forge trace/timeline display; only the bucketing count skips them. The
// escape-hatch row additionally pins UsedEscapeHatch (skip ≠ flag-less: escape sets
// the flag for the Strength cap).
//
// Row-specific rationale (chronological, from the former tests):
//   - scope-drift: advisory observation (agent modified source outside the plan);
//     drift is usually a NEGATIVE signal — counting it as positive is doubly wrong.
//   - cheat-scan: mechanically detected suspected-cheat hits are negative signals.
//   - review-pass/plan-first: observation-class stamps — neither says any
//     verification ran.
//   - observation-hooks (#4-A): CheckToolFailure/SubagentStop/TestNudge +
//     conventions hooks record process observations, never task-gate verification.
//     This is the unknown-check-name trap: only explicitly listed names are excluded.
//   - unused-scan: suspected wiring miss — a wiring-miss signal must not read as
//     positive evidence.
//   - escape-hatch: "skipped some gate", not "performed verification"; SourceForCheck
//     defaults it to deterministic, so counting it would inflate Strength backwards
//     (a signal meant to LOWER confidence would raise it). Sets UsedEscapeHatch.
//   - skill-trigger: a skill firing (passive injection) says nothing about whether
//     the task's verification ran (code-review era row).
//   - kimi-plugin-stale: kimi plugin install lags the binary — distribution health,
//     once-daily warning (code-review F1, 2026-08-15).
//   - bundle-verify: import-time trust verdict about the MULTI-MACHINE surface
//     (who signed, was it accepted) — unrelated to THIS task's verification.
//   - project-sync: git-transport sync op outcome is infrastructure health.
//   - cross-repo-impact: whether a multi-repo task declared its impact — process
//     discipline, exactly like scope-drift.
//   - task-started: the L2 boundary event (multi-task-concurrency §5) — a timeline
//     marker is not any verification result.
//
// TestBuildEvidenceChain_ObservationChecksExcluded 表驱动观察类 check 的排除（2026-08-30
// 瘦身合并原 12 个逐 check 的 *Excluded 测试）。共享不变量：下列 check 都是【观察】
// （advisory 发现/过程标记/边界事件/分发健康信号），不是「验证证据」——绝不能喂进
// 证据强度分桶，否则 Strength 反向虚高（负信号被读成正证据、每个任务出生就白得
// deterministic 条目）。条目仍保留在 Entries 供 forge trace/时间线展示，仅分桶计数
// 跳过。escape-hatch 行额外钉 UsedEscapeHatch（跳过≠无标志：逃生会置标志供 Strength cap）。
//
// 逐行缘由（按原测试年代）：
//   - scope-drift：advisory 观测（agent 改了计划外源码）；drift 常是负信号——当正向证据错上加错。
//   - cheat-scan：机械检测的疑似作弊命中是负信号。
//   - review-pass/plan-first：观察类打戳——都不代表任何验证实跑。
//   - observation-hooks（#4-A）：CheckToolFailure/SubagentStop/TestNudge + conventions
//     记录过程观察，绝非任务门禁验证。这也是未知 check 名陷阱：只有显式列名的才排除。
//   - unused-scan：疑似接线缺失——接线缺失信号绝不能读成正向证据。
//   - escape-hatch：「跳过了某 gate」非「做了验证」；SourceForCheck 默认归 deterministic，
//     计入会让本该降信心的信号抬高它。置 UsedEscapeHatch。
//   - skill-trigger：skill 触发（被动注入）不说明本任务验证真跑过。
//   - kimi-plugin-stale：kimi plugin 安装落后于二进制——分发健康度，每日一次的告警。
//   - bundle-verify：导入侧对多机信任面的判定（谁签名、是否被接受）——与本任务验证无关。
//   - project-sync：git 通道同步成败是基建健康度。
//   - cross-repo-impact：多仓任务是否声明了跨仓影响——流程纪律，同 scope-drift。
//   - task-started：L2 边界事件（multi-task-concurrency §5）——时间线标记不是验证结果。
func TestBuildEvidenceChain_ObservationChecksExcluded(t *testing.T) {
	for _, tc := range []struct {
		name       string
		entries    []Entry
		wantDet    int
		wantTotal  int
		wantEscape bool
	}{
		{`scope-drift`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckScopeDrift, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckScopeDrift, Source: EvidenceDeterministic, TaskRef: "t"}, // 多条 drift 也不计入
		}, 1, 3, false},
		{`cheat-scan`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckCheatScan, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckCheatScan, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 3, false},
		{`review-pass/plan-first`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckReviewPass, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckPlanFirst, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 3, false},
		{`observation-hooks（tool-failure/subagent-stop/test-nudge/conventions）`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckToolFailure, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckSubagentStop, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckTestNudge, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckConventionsInject, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckConventionsLint, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 6, false},
		{`unused-scan`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckUnusedScan, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckUnusedScan, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 3, false},
		// escape-hatch：det=2（auto-compile+assertion 基线）+ 逃生条目被跳过但置标志。
		{`escape-hatch（跳过分桶，但置 UsedEscapeHatch）`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckAssertion, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckEscapeHatch, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 2, 3, true},
		{`skill-trigger`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckSkillTrigger, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckSkillTrigger, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 3, false},
		{`kimi-plugin-stale`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckKimiPluginStale, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 2, false},
		{`bundle-verify`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckBundleVerify, Source: EvidenceDeterministic, TaskRef: "t", Meta: map[string]string{MetaKeyVerdict: "verified"}},
		}, 1, 2, false},
		{`project-sync`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckProjectSync, Source: EvidenceDeterministic, TaskRef: "t", Meta: map[string]string{MetaKeySyncOp: "push"}},
		}, 1, 2, false},
		{`cross-repo-impact`, []Entry{
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckCrossRepoImpact, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 2, false},
		{`task-started`, []Entry{
			{Check: CheckTaskStarted, Source: EvidenceDeterministic, TaskRef: "t"},
			{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		}, 1, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ec := BuildEvidenceChain(tc.entries, "t")
			if ec.Deterministic != tc.wantDet {
				t.Fatalf(`%s 不应计入 deterministic: got %d, want %d（仅验证类基线条目）`, tc.name, ec.Deterministic, tc.wantDet)
			}
			if ec.AgentClaim != 0 {
				t.Fatalf(`%s: agent-claim 应为 0, got %d`, tc.name, ec.AgentClaim)
			}
			if len(ec.Entries) != tc.wantTotal {
				t.Fatalf(`%s 条目仍应保留在 Entries 供 trace: got %d, want %d`, tc.name, len(ec.Entries), tc.wantTotal)
			}
			if ec.UsedEscapeHatch != tc.wantEscape {
				t.Fatalf(`%s: UsedEscapeHatch = %v, want %v`, tc.name, ec.UsedEscapeHatch, tc.wantEscape)
			}
		})
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

// TestStrength_EscapeCapEvidenceAware pins the evidence-scaled escape cap: the plan-5
// downgrade stays, but only when the escape materially props the claim. When deterministic
// evidence overwhelmingly dominates (ratio>=0.85 AND det>=20), one bypassed gate is a
// marginal fraction of the evidence — capping Strength to Weak there produced a flat
// "escape tax" that fired on virtually every task (2026-08 calibration: 55/55 capped
// conclusions had ratio>=0.5, i.e. ALL of them were Strong-before-cap; none were true
// weak-evidence claims), inflating NudgeCount/盲区率 with noise and burying real signals.
//
// TestStrength_EscapeCapEvidenceAware 钉住证据缩放的逃生舱 cap：方案5 的降档保留，但只在
// 逃生确实撑住声明时生效。deterministic 证据压倒性占优（ratio>=0.85 且 det>=20）时，一次
// 绕过的 gate 只占证据的边际份额——此时仍 cap 到 Weak 会造成人人中招的"平价逃生税"
// （2026-08 校准：55/55 被 cap 的结论 ratio>=0.5，全部是本该 Strong 的；没有一条是真弱证据），
// 让 NudgeCount/盲区率虚高、淹没真信号。
func TestStrength_EscapeCapEvidenceAware(t *testing.T) {
	cases := []struct {
		name  string
		det   int
		claim int
		want  EvidenceStrength
	}{
		// 100/2: ratio≈0.98, det 100 ≥20 → 边际逃生，不 cap
		{`重证据+逃生（ratio 0.98, det 100）→ Strong（cap 不触发）`, 100, 2, Strong},
		// 20/3: ratio≈0.87 ≥0.85, det 20 ≥20 → 恰过双阈值的边界 → 不 cap
		{`边界（ratio 0.87, det 20）→ Strong（双阈值恰好满足）`, 20, 3, Strong},
		// 18/2: ratio 0.9 ≥0.85 但 det 18 <20 → 证据质量不够压倒性 → cap 维持
		{`ratio 够但 det 不足（ratio 0.9, det 18）→ Weak（cap 维持）`, 18, 2, Weak},
		// 30/10: ratio 0.75 <0.85 → 占比不够压倒性 → cap 维持
		{`det 够但 ratio 不足（ratio 0.75, det 30）→ Weak（cap 维持）`, 30, 10, Weak},
		// 17/3: ratio 恰好 0.85 但 det 17 <20 → cap 维持（双条件必须同时满足）
		{`ratio 恰 0.85 但 det 17 → Weak（须双条件同时满足）`, 17, 3, Weak},
	}
	for _, c := range cases {
		ec := EvidenceChain{Deterministic: c.det, AgentClaim: c.claim, UsedEscapeHatch: true}
		if got := ec.Strength(); got != c.want {
			t.Errorf(`%s: Strength=%s, want %s（证据缩放 cap 判定回归？）`, c.name, got, c.want)
		}
	}
}

// TestEscapeDowngradedStrength pins the exported predicate consumed by review.go's
// blind-spot advisory: it must stay in lockstep with Strength()'s cap (single source —
// review.go derives from it rather than re-encoding the rule).
//
// TestEscapeDowngradedStrength 钉住导出谓词（review.go 盲区 ADVISORY 消费）：必须与
// Strength() 的 cap 完全同步（单一真相源——review.go 派生消费，不重复编码规则）。
func TestEscapeDowngradedStrength(t *testing.T) {
	// escape + would-be-Strong + 非边际 → cap 生效（true）
	if !(EvidenceChain{Deterministic: 4, AgentClaim: 1, UsedEscapeHatch: true}).EscapeDowngradedStrength() {
		t.Error(`det=4/claim=1+escape：cap 应生效（want true）`)
	}
	// escape + would-be-Strong + 边际（重证据）→ cap 不生效（false）
	if (EvidenceChain{Deterministic: 100, AgentClaim: 2, UsedEscapeHatch: true}).EscapeDowngradedStrength() {
		t.Error(`det=100/claim=2+escape：边际逃生 cap 不应生效（want false）`)
	}
	// 无逃生 → 恒 false
	if (EvidenceChain{Deterministic: 100, AgentClaim: 2}).EscapeDowngradedStrength() {
		t.Error(`无逃生：谓词应恒 false`)
	}
	// escape 但 ratio<0.5（本就 Weak，无 Strong 可降）→ false
	if (EvidenceChain{Deterministic: 1, AgentClaim: 3, UsedEscapeHatch: true}).EscapeDowngradedStrength() {
		t.Error(`ratio<0.5+escape：本就 Weak，谓词应 false（无 Strong 可降）`)
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

// TestBuildEvidenceChain_VerificationWhitelist pins the denylist→whitelist
// inversion (fix/cleanup-batch, 2026-08-29): only positively-listed
// verification checks may feed evidence strength. A check OUTSIDE the
// whitelist is skipped whatever its Source says — the fail-safe default for
// NEW check names (the old 18-clause observation denylist silently bucketed
// any unlisted name as deterministic evidence, inflating Strength). The
// taskpipeline-defined verification checks (acceptance / test-run — forge
// actually ran the suite/criteria, unforgeable) are whitelisted via literals
// (checklog cannot import taskpipeline: cycle).
//
// TestBuildEvidenceChain_VerificationWhitelist 钉住黑名单→白名单反转
// （fix/cleanup-batch，2026-08-29）：只有正向列名的验证类 check 才可喂给
// evidence strength。白名单之外的 check 无论 Source 写什么都跳过——对新增
// check 名这是 fail-safe 默认（旧的 18 子句 observation 黑名单会把未列名的
// 名字静默分桶成 deterministic 证据、虚高 Strength）。taskpipeline 定义的验证
// check（acceptance / test-run——forge 实跑过套件/标准、不可伪造）以字面量列入
// 白名单（checklog 不能 import taskpipeline：会成环）。
func TestBuildEvidenceChain_VerificationWhitelist(t *testing.T) {
	entries := []Entry{
		// Whitelist members: bucketed by Source as before (semantic equivalence
		// for existing verification checks).
		//
		// 白名单成员：照旧按 Source 分桶（对既有验证 check 语义等价）。
		{Check: CheckAutoCompile, Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckFileSentinel, Source: "", TaskRef: "t"}, // 空 Source 兜底仍生效
		{Check: CheckTaskVerify, Source: EvidenceAgentClaim, TaskRef: "t"},
		{Check: CheckName("acceptance"), Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckName("test-run"), Source: EvidenceDeterministic, TaskRef: "t"},
		// OUTSIDE the whitelist: skipped even with a forged/explicit
		// Source=deterministic — new and unknown check names default to
		// observation (fail-safe).
		//
		// 白名单之外：即便 Source 显式/伪造为 deterministic 也跳过——
		// 新增与未知 check 名默认 observation（fail-safe）。
		{Check: CheckName("some-future-check"), Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckName("resume-reinject"), Source: EvidenceDeterministic, TaskRef: "t"},
		{Check: CheckName("assignment-unclaimed"), Source: EvidenceDeterministic, TaskRef: "t"},
	}
	ec := BuildEvidenceChain(entries, "t")
	if ec.Deterministic != 4 {
		t.Fatalf(`白名单内应计入 deterministic: got %d, want 4（auto-compile + file-sentinel 兜底 + acceptance + test-run）`, ec.Deterministic)
	}
	if ec.AgentClaim != 1 {
		t.Fatalf(`task-verify 应计 agent-claim: got %d, want 1`, ec.AgentClaim)
	}
	// Whitelist-outside entries are preserved for trace but never bucketed.
	//
	// 白名单外条目保留供 trace，但绝不分桶。
	if len(ec.Entries) != 8 {
		t.Fatalf(`entries preserved: got %d, want 8`, len(ec.Entries))
	}

	// A whitelist member with an UNKNOWN Source still follows the
	// forgery-backdoor rule: agent-claim, never deterministic.
	//
	// 白名单成员带未知 Source 仍走伪造后门规则：agent-claim，绝不 deterministic。
	ec2 := BuildEvidenceChain([]Entry{
		{Check: CheckAutoCompile, Source: EvidenceSource("hook-verified"), TaskRef: "t"},
	}, "t")
	if ec2.Deterministic != 0 || ec2.AgentClaim != 1 {
		t.Fatalf(`未知 Source 应计 agent-claim: det=%d claim=%d, want 0/1`, ec2.Deterministic, ec2.AgentClaim)
	}
}

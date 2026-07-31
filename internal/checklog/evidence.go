package checklog

// EvidenceChain aggregates evidence entries scattered across checklog.jsonl (including archived history)
// for a task into a structured view of "what verifications were claimed, and how many have deterministic evidence".
//
// It is the core output of route 1 (evidence-chain foundation): the review sub-agent and scoring no longer rely solely on diff,
// but first check whether the task has enough deterministic evidence to support the "done" claim — countering the LLM-judge
// blind spot of not seeing "agent skipped verification and claimed done".
//
// EvidenceChain 把一个任务散落在 checklog.jsonl（含归档历史）里的证据条目，
// 聚成"声称做了哪些验证、其中多少有 deterministic 证据"的结构化视图。
//
// 它是路线 1（证据链底座）的核心产出：review 子 agent 和评分不再只能看 diff，
// 而是先看本任务是否有足够 deterministic 证据支撑"完成"声明——对冲 LLM-judge
// 看不出"agent 跳过前置就声明完成"的盲区。
type EvidenceChain struct {
	TaskRef string
	// Entries are time-ordered (from LoadForTask). Old entries with empty Source are inferred via SourceForCheck
	// at bucketing time as a fallback, but the original value is preserved within Entries (not backfilled); callers needing explicit inference manage it themselves.
	//
	// Entries 按时间序（来自 LoadForTask）。Source 为空的旧条目按 SourceForCheck
	// 在分桶时兜底推断，但 Entries 内保留原值（不回填），调用方需显式推断时自理。
	Entries       []Entry
	// Deterministic: count of entries with Source=deterministic (including empty-Source fallback).
	//
	Deterministic int // Source=deterministic（含空 Source 兜底）的条目数
	// AgentClaim: count of entries with Source=agent-claim.
	//
	AgentClaim    int // Source=agent-claim 的条目数
	// UsedEscapeHatch reports whether this task has used an escape hatch (FORGE_WORK_ACTIVITY,
	// FORGE_TEST_COVERAGE, etc. — gate-bypass). Escape is a legitimate tool, but when the "done" claim relies on
	// skipping gates, credibility must be discounted — Strength is capped at Weak accordingly (plan 5: give escape
	// a cost, countering the backlash of "hard gate + global escape hatch = fake hard gate").
	//
	// UsedEscapeHatch 报告本任务是否用过逃生舱（FORGE_WORK_ACTIVITY/
	// FORGE_TEST_COVERAGE 等 gate-bypass）。逃生是合法工具，但"完成"声明若靠
	// 跳过 gate 撑住，可信度必须打折——Strength 据此 cap 到 Weak（方案5：让逃生
	// 有代价，对冲"硬门禁 + 全局逃生舱 = 假硬门禁"的反噬）。
	UsedEscapeHatch bool
}

// Total returns the total number of evidence entries (deterministic + agent-claim).
//
// Total 返回证据条目总数（deterministic + agent-claim）。
func (ec EvidenceChain) Total() int {
	return ec.Deterministic + ec.AgentClaim
}

// Ratio returns the deterministic proportion of the evidence: Deterministic/Total.
// 1.0 = fully supported by deterministic; 0.0 = no deterministic evidence. Returns 0 when Total==0 —
// callers should check Strength()==NoData first to distinguish "no evidence" from "all agent-claim".
//
// Ratio 返回证据中 deterministic 占比：Deterministic/Total。
// 1.0 = 完全由 deterministic 支撑；0.0 = 无 deterministic 证据。Total==0 时返回 0——
// 调用方应先看 Strength()==NoData 来区分「无证据」与「全是 agent-claim」。
func (ec EvidenceChain) Ratio() float64 {
	if ec.Total() == 0 {
		return 0
	}
	return float64(ec.Deterministic) / float64(ec.Total())
}

// EvidenceStrength discretizes Ratio into tiers that review can act on. The point is not the number itself,
// but what it should "drive": when a "done" claim rests mostly on agent self-report (Weak/Unverified),
// that is exactly the LLM-judge blind spot (agent skipped real verification and claimed done) — the reviewer must
// then verify whether the claimed verification actually happened, not just read the diff. Strong = deterministic majority, claim is credible.
//
// Tiers (threshold 0.5 = "deterministic majority"):
//   - NoData:     no evidence at all (total 0) — neutral, nothing to calibrate.
//   - Unverified: has agent-claim but zero deterministic — claim has no real-run support, the highest-signal blind spot.
//   - Weak:       has deterministic but agent-claim is the majority (ratio<0.5).
//   - Strong:     deterministic is the majority (ratio>=0.5).
//
// EvidenceStrength 把 Ratio 离散成 review 可据以行动的档位。重点不是数字本身，
// 而是它该"驱动"什么：一个"完成"声明主要靠 agent 自述撑着（Weak/Unverified），
// 正是 LLM-judge 盲区所在（agent 跳过真实验证就声明完成）——此时 reviewer 必须
// 核查声称的验证是否真发生过，而不只读 diff。Strong=deterministic 占多数，声明可信。
//
// 档位（阈值 0.5 = "deterministic 占多数"）：
//   - NoData:     无任何证据（total 0）——中性，无可校准。
//   - Unverified: 有 agent-claim 但零 deterministic——声明全无实跑支撑，最高信号盲区。
//   - Weak:       有 deterministic 但 agent-claim 占多数（ratio<0.5）。
//   - Strong:     deterministic 占多数（ratio>=0.5）。
type EvidenceStrength int

const (
	NoData EvidenceStrength = iota
	Unverified
	Weak
	Strong
)

// String returns the human-readable tier name, for trace/review-status output.
//
// String 返回人类可读的档位名，供 trace/review-status 输出。
func (s EvidenceStrength) String() string {
	switch s {
	case NoData:
		return "NoData"
	case Unverified:
		return "Unverified"
	case Weak:
		return "Weak"
	case Strong:
		return "Strong"
	}
	return "Unknown"
}

// Strength buckets the evidence chain into review-actionable tiers. Semantics and thresholds: see EvidenceStrength doc.
//
// Strength 把证据链分到 review 可行动的档位。语义与阈值见 EvidenceStrength 文档。
func (ec EvidenceChain) Strength() EvidenceStrength {
	if ec.Total() == 0 {
		return NoData
	}
	if ec.Deterministic == 0 && ec.AgentClaim > 0 {
		return Unverified
	}
	s := Weak
	if ec.Ratio() >= 0.5 {
		s = Strong
	}
	// Plan 5: using an escape hatch = the "done" claim is propped up by skipping gates, so it cannot be rated Strong. Cap at Weak —
	// give escape a cost rather than merely logging it, countering the backlash of "hard gate + global escape hatch = fake hard gate". Use downgrade
	// rather than block: keep escape legitimate (doc-only, generated code, etc.) while making it no longer free.
	//
	// 方案5：用了逃生舱 = "完成"声明靠跳过 gate 撑住，不可评 Strong。cap 到 Weak——
	// 让逃生有代价而非仅记 log，对冲"硬门禁 + 全局逃生舱 = 假硬门禁"的反噬。用降档
	// 而非阻断：既保逃生合法（doc-only/生成码等正当场景），又让它不再免费。
	if ec.UsedEscapeHatch && s == Strong {
		s = Weak
	}
	return s
}

// BuildEvidenceChain is a pure function: buckets entries already belonging to a task by source. Entries with empty
// Source (legacy data, or un-migrated record points) fall back to SourceForCheck, ensuring old checklog
// is bucketed correctly and the foundation can ship without backfilling history.
//
// BuildEvidenceChain 是纯函数：对已属于某任务的 entries 按来源分桶。Source 为空
// 的条目（旧数据，或未改造的记录点写入）按 SourceForCheck 兜底，保证旧 checklog
// 也能正确分桶，底座上线无需回填历史。
func BuildEvidenceChain(entries []Entry, taskRef string) EvidenceChain {
	ec := EvidenceChain{TaskRef: taskRef, Entries: entries}
	for _, e := range entries {
		// Advisory/meta checks record OBSERVATIONS rather than verification results —
		// they must never feed into evidence strength. scope-drift is an advisory signal (agent modified undeclared
		// source); treating it as deterministic verification would inflate Strength and mask exactly the blind spot
		// EvidenceChain is meant to expose. Entries are still kept in Entries (forge trace will display them); only the
		// bucketing count skips them. Drift is usually a negative signal — counting it as positive evidence is doubly wrong.
		// cheat-scan is the same kind (mechanical scan for AI-cheat suspicion patterns) — hits are negative signals, not verification
		// evidence. CheckEscapeHatch is the same: using a gate-bypass is an observation of "skipping", not "verifying" —
		// treating it as deterministic would inflate Strength and hide the signal it should expose (task slipping through by dodging gates).
		// It sets UsedEscapeHatch so Strength can be capped at Weak.
		//
		// Advisory/meta check 记录的是 OBSERVATIONS（观察）而非 verification 结果——
		// 绝不可计入 evidence strength。scope-drift 是 advisory 信号（agent 改了未声明的
		// 源码）；把它当作 deterministic verification 会让 Strength 虚高、正好掩盖
		// EvidenceChain 要暴露的盲区。条目仍落进 Entries（forge trace 会展示），只是分桶
		// 计数跳过它。Drift 通常还是负向信号——当作正向证据是双重错误。
		// cheat-scan 同类（机械扫描 AI-cheat 嫌疑模式）——命中是负向信号，非 verification
		// 证据。CheckEscapeHatch 同类：用过 gate-bypass 是「跳过」的观察、不是「验证」——
		// 当成 deterministic 会让 Strength 虚高、正好隐藏它该暴露的信号（task 靠躲 gate
		// 蒙混过关）。它置 UsedEscapeHatch，让 Strength 能 cap 到 Weak。
		if e.Check == CheckScopeDrift || e.Check == CheckCheatScan || e.Check == CheckUnusedScan || e.Check == CheckEscapeHatch {
			if e.Check == CheckEscapeHatch {
				ec.UsedEscapeHatch = true
			}
			continue
		}
		src := e.Source
		if src == "" {
			src = SourceForCheck(e.Check)
		}
		// Credibility requires a positive match: checklog.jsonl is agent-writable, so an
		// unknown Source value (typo, hand-edited, or injected) must never fall into the
		// deterministic bucket via a catch-all else — that would be a forgery backdoor.
		// Anything not positively "deterministic" is counted as agent-claim.
		//
		// 可信必须正向匹配：checklog.jsonl 是 agent 可写的，未知 Source 值（笔误、
		// 手改、注入）绝不能经兜底 else 落进 deterministic 桶——那是伪造后门。
		// 凡未正向命中 deterministic 的一律计为 agent-claim。
		switch src {
		case EvidenceDeterministic:
			ec.Deterministic++
		case EvidenceAgentClaim:
			ec.AgentClaim++
		default:
			// Unknown value after the empty-Source fallback: bucket as agent-claim.
			//
			// 空 Source 兜底后仍未知的值：计为 agent-claim。
			ec.AgentClaim++
		}
	}
	return ec
}

// ForTask loads all evidence for a task from disk (including archived checklog-*.jsonl) and aggregates it.
// Equivalent to LoadForTask + BuildEvidenceChain. Current consumer: forge trace; reserved for future
// scoring/review sub-agents to fetch the evidence chain in one call (instead of each repeating LoadForTask + bucketing).
//
// ForTask 从磁盘加载一个任务的全部证据（含归档 checklog-*.jsonl）并聚合。
// 等价于 LoadForTask + BuildEvidenceChain。当前消费者：forge trace；预留给
// 未来评分/review 子 agent 一行取到证据链（避免各自重复 LoadForTask+分桶）。
func ForTask(root, taskRef string) (EvidenceChain, error) {
	entries, err := LoadForTask(root, taskRef)
	if err != nil {
		return EvidenceChain{}, err
	}
	return BuildEvidenceChain(entries, taskRef), nil
}

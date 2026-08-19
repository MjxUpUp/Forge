package taskpipeline

// merge.go — task-state merge semantics shared by every cross-machine path:
// `forge task import --merge` (cli/task_port.go) and `forge project import`
// (project-sync, datamerge TaskUnion policy). Exported from taskpipeline so the
// merge contract (local identity authoritative, collaborative records unioned by
// ID/key, never downgraded, idempotent) has ONE implementation and ONE test suite.
//
// merge.go —— 跨机器路径共享的任务状态合并语义：`forge task import --merge`
// （cli/task_port.go）与 `forge project import`（project-sync，datamerge 的
// TaskUnion 策略）。从 taskpipeline 导出，使合并契约（本地身份权威、协作记录按
// ID/键并集、绝不降级、幂等）只有一份实现与一套测试。

// MergeTaskState unions incoming collaborative records into local (mutates local).
// Local task identity/definition (Goal/Plan/Summary/Kind/OriginTool/Assignment/
// DependsOn/ParentTaskRef/gates) is authoritative and untouched — merge only grows
// the shared evidence/decision/history sets, so a re-merge is idempotent for the TASK
// STATE. Incoming session links are expected to be already ghosted (Imported=true) by
// the caller (StripForeignGateSignals or GhostForeignSessions).
//
// MergeTaskState 把传入的协作记录并集进 local（改 local）。本地任务身份/定义
// （Goal/Plan/Summary/Kind/OriginTool/Assignment/DependsOn/ParentTaskRef/门禁）
// 为权威不动——合并只增长共享的证据/决策/历史集合，故重复合并幂等。传入的
// session 链接由调用方预先幽灵化（StripForeignGateSignals 或
// GhostForeignSessions）。
func MergeTaskState(local, incoming *TaskState) {
	unionCollaborative(local, incoming)
	// History (gate results): keyed by Gate — keep local's result when both have one
	// (authoritative, never downgraded by a remote snapshot), else add the incoming
	// gate the local task hasn't reached.
	//
	// History（门禁结果）：按 Gate 键——两侧都有时保 local（权威，绝不被远端快照
	// 降级），否则补 local 未到达的传入门禁。
	local.History = UnionGateHistory(local.History, incoming.History)
}

// MergeTaskStateSync is the same-identity dual-machine sync variant (project-sync):
// union semantics PLUS two monotonic rules without which a sync loop never converges —
//
//  1. Gate history prefer-Passed: when the two sides disagree on a gate (one Passed,
//     one Failed), the PASSED entry wins regardless of side. The executor's
//     prerequisite walk only counts Passed entries, so a local-authoritative union
//     would deadlock a task whose failed gate was fixed on the peer machine (the
//     incoming Passed is dropped, the local Failed stays, and the gate never re-runs
//     on this machine once the peer's completion is adopted below). Same-result
//     conflicts keep the local entry (stable, idempotent).
//  2. Completion block monotonic adoption: an incoming COMPLETED snapshot of the same
//     task completes the local incomplete copy wholesale (CompletedAt / ReviewPassed /
//     review anchors / Score / Assignment / Acceptance results) — the peer machine
//     finished the work, and a local-authoritative union would keep the local
//     incomplete state forever, flip-flopping on every sync round. A local completion
//     is never downgraded (incoming incomplete loses; both complete keep local).
//
// Trusted-only by contract (the caller passes TrustResults only for same-identity
// lineage): preserving foreign completion/Assignment is exactly the power the
// untrusted path strips via StripForeignGateSignals.
//
// MergeTaskStateSync 是同身份双机同步变体（project-sync）：并集语义 + 两条缺之
// 则同步循环永不收敛的单调规则——
//
//  1. 门禁 history prefer-Passed：两侧对同一门禁结论不一致（一侧 Passed 一侧
//     Failed）时，Passed 条目胜，不分本机对端。executor 的前置链只认 Passed
//     条目，本地权威的并集会把任务死锁——对端修复的 Passed 被丢弃、本地 Failed
//     留存、且因下方的完成块采纳本机不会再重跑该门禁。同结论冲突保本地条目
//     （稳定、幂等）。
//  2. 完成块单调采纳：同任务的传入「已完成」快照整块完成本地未完成副本
//     （CompletedAt / ReviewPassed / review 锚 / Score / Assignment / 验收结果）
//     ——对端做完了工作，本地权威并集会让本地永远停在未完成、每轮同步来回翻。
//     本地已完成绝不被降级（传入未完成者败；双已完成保本地）。
//
// 按契约仅用于受信路径（调用方仅在同身份 lineage 时传 TrustResults）：保留外来
// 完成/Assignment 恰是不可信路径经 StripForeignGateSignals 剥掉的权力。
func MergeTaskStateSync(local, incoming *TaskState) {
	// Rule 2 first: adoption must happen before the unions so the adopted completion
	// block is the base the unions grow onto (and an incoming incomplete snapshot
	// never touches a local completion).
	//
	// 规则 2 先行：采纳必须发生在并集之前，使采纳的完成块成为并集生长的基座
	// （且传入的未完成快照永不触碰本地完成态）。
	if incoming.CompletedAt != nil && local.CompletedAt == nil {
		local.CompletedAt = incoming.CompletedAt
		local.ReviewPassed = incoming.ReviewPassed
		local.ReviewedHeadCommit = incoming.ReviewedHeadCommit
		local.ReviewedChangeHash = incoming.ReviewedChangeHash
		// ReviewRounds travels with the completion block: the adopted Score may be
		// recomputed locally later, and the review-pass history is the raw material of
		// the rework metric — adopting the score but dropping the rounds would
		// undercount on a local rescore.
		//
		// ReviewRounds 随完成块一起走：采纳的 Score 之后可能在本地重算，而
		// review-pass 历史是返工度量的原料——采纳了分数却丢轮次会让本地重算
		// 少计。
		local.ReviewRounds = incoming.ReviewRounds
		local.Score = incoming.Score
		local.Assignment = incoming.Assignment
		local.Acceptance = incoming.Acceptance
	}
	unionCollaborative(local, incoming)
	local.History = unionGateHistoryPreferPassed(local.History, incoming.History)
	// Re-derive the current gate from the merged history: after a healed gate or an
	// adopted completion the stored CurrentGate would otherwise show a stale position
	// (display-only field; NextGate is the canonical derivation — same mechanism
	// StripForeignGateSignals uses).
	//
	// 从合并后的 history 重推当前门禁：门禁愈合或完成块采纳后，存量 CurrentGate
	// 会显示过期位置（纯展示字段；NextGate 是 canonical 推导——与
	// StripForeignGateSignals 同机制）。
	local.CurrentGate = local.NextGate()
}

// unionCollaborative unions the shared collaborative record sets (everything except
// gate History and the completion block, which the two merge variants resolve
// differently).
//
// unionCollaborative 并集共享协作记录集（除门禁 History 与完成块外的一切——两者
// 由两个合并变体分别裁决）。
func unionCollaborative(local, incoming *TaskState) {
	// SessionLinks: append incoming ghost links whose sid isn't already present (any
	// link, local or ghost — a re-import of the same ghost must not duplicate).
	//
	// SessionLinks：追加本机不存在的传入幽灵链接（本地或幽灵皆算——同一幽灵
	// 重复导入不得翻倍）。
	for _, in := range incoming.SessionLinks {
		if !local.HasAnySession(in.SessionID) {
			local.SessionLinks = append(local.SessionLinks, in)
		}
	}
	local.Decisions = UnionDecisions(local.Decisions, incoming.Decisions)
	local.Findings = UnionFindings(local.Findings, incoming.Findings)
	local.Blockers = UnionBlockers(local.Blockers, incoming.Blockers)
	local.NextSteps = UnionStrings(local.NextSteps, incoming.NextSteps)
	local.Artifacts = UnionArtifacts(local.Artifacts, incoming.Artifacts)
}

// unionGateHistoryPreferPassed merges gate results keyed by Gate with the
// prefer-Passed conflict rule (see MergeTaskStateSync rule 1). Local entries seed the
// order; an incoming Passed replaces a local Failed for the same gate.
//
// unionGateHistoryPreferPassed 按 Gate 键合并门禁结果，冲突采用 prefer-Passed 规则
// （见 MergeTaskStateSync 规则 1）。本地条目定序；同 Gate 的传入 Passed 替换本地
// Failed。
func unionGateHistoryPreferPassed(local, incoming []TaskGateResult) []TaskGateResult {
	merged := make([]TaskGateResult, 0, len(local)+len(incoming))
	index := make(map[string]int, len(local)+len(incoming))
	for _, h := range local {
		index[h.Gate] = len(merged)
		merged = append(merged, h)
	}
	for _, h := range incoming {
		i, ok := index[h.Gate]
		if !ok {
			index[h.Gate] = len(merged)
			merged = append(merged, h)
			continue
		}
		// Disagreement resolves to Passed (peer healed it); agreement keeps local
		// (stable across re-merge).
		//
		// 结论不一致取 Passed（对端已修复）；一致保本地（重复合并稳定）。
		if h.Passed && !merged[i].Passed {
			merged[i] = h
		}
	}
	return merged
}

func UnionDecisions(local, incoming []Decision) []Decision {
	// Forge always assigns unique IDs (newContinuityID); an empty ID = malformed bundle.
	// Empty-ID entries are appended as-is and never deduped (deduping them would
	// collapse N distinct entries into one, silently losing data). Only non-empty IDs
	// participate in the seen-set.
	//
	// Forge 恒赋唯一 ID（newContinuityID）；空 ID = 畸形 bundle。空 ID 条目原样追加、
	// 永不参与去重（去重会把 N 条不同条目压成一条，静默丢数据）。只有非空 ID 进
	// seen 集合。
	seen := map[string]bool{}
	for _, d := range local {
		if d.ID != `` {
			seen[d.ID] = true
		}
	}
	for _, d := range incoming {
		if d.ID != `` && seen[d.ID] {
			continue
		}
		if d.ID != `` {
			seen[d.ID] = true
		}
		local = append(local, d)
	}
	return local
}

func UnionFindings(local, incoming []Finding) []Finding {
	seen := map[string]bool{}
	for _, f := range local {
		if f.ID != `` {
			seen[f.ID] = true
		}
	}
	for _, f := range incoming {
		if f.ID != `` && seen[f.ID] {
			continue
		}
		if f.ID != `` {
			seen[f.ID] = true
		}
		local = append(local, f)
	}
	return local
}

func UnionBlockers(local, incoming []Blocker) []Blocker {
	seen := map[string]bool{}
	for _, b := range local {
		if b.ID != `` {
			seen[b.ID] = true
		}
	}
	for _, b := range incoming {
		if b.ID != `` && seen[b.ID] {
			continue
		}
		if b.ID != `` {
			seen[b.ID] = true
		}
		local = append(local, b)
	}
	return local
}

// UnionGateHistory merges gate results keyed by Gate; local wins on conflict
// (authoritative), and an incoming gate absent locally is appended so a remote task
// further along contributes its gate progression without overriding what the local
// task already passed.
//
// UnionGateHistory 按 Gate 合并门禁结果；冲突时 local 胜（权威），本地缺失的门禁
// 追加进来，使进度更远的远端 task 贡献其门禁进度而不覆盖本地已通过的。
func UnionGateHistory(local, incoming []TaskGateResult) []TaskGateResult {
	seen := map[string]bool{}
	for _, h := range local {
		seen[h.Gate] = true
	}
	for _, h := range incoming {
		if !seen[h.Gate] {
			seen[h.Gate] = true
			local = append(local, h)
		}
	}
	return local
}

func UnionStrings(local, incoming []string) []string {
	seen := map[string]bool{}
	for _, s := range local {
		seen[s] = true
	}
	for _, s := range incoming {
		if !seen[s] {
			seen[s] = true
			local = append(local, s)
		}
	}
	return local
}

func UnionArtifacts(local, incoming []Artifact) []Artifact {
	seen := map[string]bool{}
	for _, a := range local {
		if a.Path != `` {
			seen[a.Path] = true
		}
	}
	for _, a := range incoming {
		if a.Path != `` && seen[a.Path] {
			continue
		}
		if a.Path != `` {
			seen[a.Path] = true
		}
		local = append(local, a)
	}
	return local
}

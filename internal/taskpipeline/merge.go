package taskpipeline

import "fmt"

// merge.go —— 跨机器路径共享的任务状态合并语义：`forge task import --merge`
// （cli/task_port.go）与 `forge project import`（project-sync，datamerge 的
// TaskUnion 策略）。从 taskpipeline 导出，使合并契约（本地身份权威、协作记录按
// ID/键并集、绝不降级、幂等）只有一份实现与一套测试。

// MergeTaskState unions incoming collaborative records into local (mutates local).
//
// MergeTaskState 把传入的协作记录并集进 local（改 local）。本地任务身份/定义
// （Goal/Plan/Summary/Kind/OriginTool/Assignment/DependsOn/ParentTaskRef/门禁）
// 为权威不动——合并只增长共享的证据/决策/历史集合，故重复合并幂等。传入的
// session 链接由调用方预先幽灵化（StripForeignGateSignals 或
// GhostForeignSessions）。
func MergeTaskState(local, incoming *TaskState) {
	unionCollaborative(local, incoming)
	// History（门禁结果）：按 Gate 键——两侧都有时保 local（权威，绝不被远端快照
	// 降级），否则补 local 未到达的传入门禁。
	local.History = UnionGateHistory(local.History, incoming.History)
}

// MergeTaskStateSync is the same-identity dual-machine sync variant (project-sync): union semantics PLUS two monotonic rules without which a sync loop never converges.
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
	// 收敛层（merge_converge.go）。顺序关键：完成裁决（双完成 + 采纳）先定局完成
	// 块；之后标量决胜才把 ReviewRounds 并到已定局的胜者上——先并集再采纳会让
	// 采纳按方向不同地覆盖并集结果。
	resolveBothCompleteSync(local, incoming)
	// 规则 2：传入「已完成」快照整块完成本地未完成副本（传入未完成快照永不触碰
	// 本地完成态）。
	if incoming.CompletedAt != nil && local.CompletedAt == nil {
		adoptCompletionBlock(local, incoming)
	}
	resolveScalarTiebreaksSync(local, incoming)
	unionCollaborative(local, incoming)
	// 确定性同 ID 冲突决胜（收敛层；仅受信路径）。
	resolveRecordConflictsSync(local, incoming)
	// 门禁 history 按全内容键并集（不按 gate 折叠）：重试 provenance（Failed 后重跑
	// Passed）是 ReworkRounds/评分/feed 的承重数据——按 gate 折叠会静默删除它。
	// gatePassed 仍能看到 Passed 条目，旧 prefer-Passed 规则的愈合防死锁保障保留
	// （对端的 Passed 进入并集）。
	local.History = unionGateHistoryByContent(local.History, incoming.History)
	// 规范排序：交换律收尾（到达顺序不得渗入字节）。
	canonicalizeSync(local)
	// 从合并后的 history 重推当前门禁：门禁愈合或完成块采纳后，存量 CurrentGate
	// 会显示过期位置（纯展示字段；NextGate 是 canonical 推导——与
	// StripForeignGateSignals 同机制）。
	local.CurrentGate = local.NextGate()
}

// unionCollaborative 并集共享协作记录集（除门禁 History 与完成块外的一切——两者
// 由两个合并变体分别裁决）。
func unionCollaborative(local, incoming *TaskState) {
	// SessionLinks：追加本机不存在的传入链接（本地或幽灵皆算——同一幽灵重复导入
	// 不得翻倍）。sid 冲突本地胜：本并集是共享（含不可信）路径，本地锚定保持权威；
	// SYNC 路径随后的 canonicalizeSync 用确定性胜者（normalizeSessionLinks）裁决，
	// 在不削弱不可信契约的前提下恢复交换律。
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

// unionGateHistoryByContent 按全内容键（Gate+CompletedAt+HeadCommit+Passed）并集
// 门禁结果：Failed 与其后的 Passed 重跑都存活（返工 provenance），完全重复折叠，
// 结果方向无关。规范序由 canonicalizeSync 随后施加。
func unionGateHistoryByContent(local, incoming []TaskGateResult) []TaskGateResult {
	seen := map[string]bool{}
	out := make([]TaskGateResult, 0, len(local)+len(incoming))
	for _, h := range append(append([]TaskGateResult{}, local...), incoming...) {
		k := gateContentKey(h)
		if !seen[k] {
			seen[k] = true
			out = append(out, h)
		}
	}
	return out
}

// gateContentKey 是门禁结果的全身份键（全字段，重试/重跑保持区分；时间零填充
// 保持字典序 == 数值序）。
func gateContentKey(h TaskGateResult) string {
	return h.Gate + "\x00" + pad20(h.CompletedAt.UnixNano()) + "\x00" + h.HeadCommit + "\x00" + fmt.Sprint(h.Passed)
}

func UnionDecisions(local, incoming []Decision) []Decision {
	// Forge 恒赋唯一 ID（NewContinuityID）；空 ID = 畸形 bundle。空 ID 条目原样追加、
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

// UnionGateHistory merges gate results keyed by Gate; local wins on conflict (authoritative).
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

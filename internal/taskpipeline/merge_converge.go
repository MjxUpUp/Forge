package taskpipeline

// merge_converge.go —— MergeTaskStateSync 的收敛层
// （docs/design/sync-convergence.md §2 B 类：tasks/*.json 字段级分流）。
//
// 基础合并（并集 + prefer-Passed + 完成块采纳）保证不丢数据与单调前进，但不保证
// 交换律：记录集合保留到达顺序、同结论冲突保留本地——merge(A,B) 与 merge(B,A)
// 得到同集合不同字节，双机同步循环会永远来回翻转。本文件补上确定性层，让受信
// 同身份合并成为可交换、幂等的函数：
//
//  1. 规范排序——每个记录集合并集后按内容键排序，到达顺序无法渗进字节；
//  2. 确定性胜者——每个冲突按方向无关规则裁决：时间戳早者胜（首次达成是事实，
//     其后是重跑），并列时按规范 JSON 字典序最终决胜（确定性优先于正确性：
//     两台机器必须收敛到相同字节）；
//  3. 标量决胜——身份字段（Summary/Branch/Goal/…）同身份任务实践上恒等；病理
//     性不一致时字典序小者胜（记录为「任意但收敛」）。
//
// 字段级 HLC 在此不适用：TaskState 字段不携带逐字段时间戳，HLC 无从比较——
// 规范排序 + 确定性胜者不改 schema 即达成同样的收敛性。
//
// 仅受信路径：这些规则跑在 MergeTaskStateSync（同身份 lineage）。不可信路径
// （MergeTaskState）刻意保持本地权威语义不变。

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// canonicalJSON 渲染 v 供确定性字节比较（encoding/json 的字段序是 struct 声明
// 序——跨进程跨平台稳定）。
func canonicalJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ``
	}
	return string(raw)
}

// tiebreakScalar 返回两个不同标量值的确定性胜者：空输给非空；否则字典序小者胜。
func tiebreakScalar(local, incoming string) string {
	if local == `` {
		return incoming
	}
	if incoming == `` || incoming >= local {
		return local
	}
	return incoming
}

// tiebreakTime 返回两个时间戳的确定性胜者：早者胜（首次达成）。经 UnixNano 比较，
// 表示差异（时区/单调位）无法渗入。
func tiebreakTime(local, incoming time.Time) time.Time {
	if local.IsZero() {
		return incoming
	}
	if incoming.IsZero() || incoming.UnixNano() >= local.UnixNano() {
		return local
	}
	return incoming
}

// resolveScalarTiebreaksSync 对 MergeTaskStateSync 原本本地权威的身份/定义标量施加
// 确定性、方向无关的决胜。同身份任务实践中恒等；本规则为「同一 task_ref 在两台
// 机器独立创建」的病理情形兜底。
func resolveScalarTiebreaksSync(local, incoming *TaskState) {
	local.Branch = tiebreakScalar(local.Branch, incoming.Branch)
	local.Source = tiebreakScalar(local.Source, incoming.Source)
	local.Summary = tiebreakScalar(local.Summary, incoming.Summary)
	local.Kind = tiebreakScalar(local.Kind, incoming.Kind)
	local.OriginTool = tiebreakScalar(local.OriginTool, incoming.OriginTool)
	local.Goal = tiebreakScalar(local.Goal, incoming.Goal)
	local.Plan = tiebreakScalar(local.Plan, incoming.Plan)
	local.HeadCommit = tiebreakScalar(local.HeadCommit, incoming.HeadCommit)
	local.SessionID = tiebreakScalar(local.SessionID, incoming.SessionID)
	local.ParentTaskRef = tiebreakScalar(local.ParentTaskRef, incoming.ParentTaskRef)
	local.StartedAt = tiebreakTime(local.StartedAt, incoming.StartedAt)
	local.ResumeStale = local.ResumeStale || incoming.ResumeStale
	if local.TTL == 0 || (incoming.TTL != 0 && incoming.TTL < local.TTL) {
		local.TTL = incoming.TTL
	}
	// Lease：fencing 高者胜（fencing token 的全部意义）；全完成态适用（租约与
	// 完成块正交）。
	mergeLeaseSync(local, incoming)
	// 块结构字段：非空优先（空绝不顶替真实数据——"null"/"{}"/"[]" 字典序恒小于
	// 填充值，裸字节比较会在这些规则为之存在的病理双创建情形里系统性删数据）；
	// 非空间并列按规范 JSON 字节序。
	if canonBlockLess(incoming.ExternalOrigin, local.ExternalOrigin) {
		local.ExternalOrigin = incoming.ExternalOrigin
	}
	if local.CompletedAt == nil && incoming.CompletedAt == nil {
		// 完成前即落的 review 锚（review-pass 在 task-verify 与 task-complete 之间）。
		// 只有双未完成才走标量规则：一旦有完成参与，它们属于完成块（由
		// resolveBothCompleteSync / 规则 2 一致采纳）——彼处走标量决胜会把一侧的
		// hash 混进另一侧的采纳块，既不连贯又方向相关。
		local.ReviewPassed = local.ReviewPassed || incoming.ReviewPassed
		local.ReviewedHeadCommit = tiebreakScalar(local.ReviewedHeadCommit, incoming.ReviewedHeadCommit)
		local.ReviewedChangeHash = tiebreakScalar(local.ReviewedChangeHash, incoming.ReviewedChangeHash)
		// 信任标志进决胜键：同命令不同标志是不同块——只比 Acceptance 会让两侧各保
		// 各的标志，同内容冲突时把方向渗进字节。
		if canonBlockLess([]any{incoming.Acceptance, incoming.AcceptanceForeign}, []any{local.Acceptance, local.AcceptanceForeign}) {
			local.Acceptance = incoming.Acceptance
			local.AcceptanceForeign = incoming.AcceptanceForeign
		}
		if canonBlockLess(incoming.Assignment, local.Assignment) {
			local.Assignment = incoming.Assignment
		}
		if canonBlockLess(incoming.Score, local.Score) {
			local.Score = incoming.Score
		}
		if canonBlockLess(incoming.PlanScope, local.PlanScope) {
			local.PlanScope = incoming.PlanScope
		}
		if canonBlockLess(incoming.Overrides, local.Overrides) {
			local.Overrides = incoming.Overrides
		}
	}
	// 集合结构字段：先并集，规范序由 canonicalizeSync 收尾。
	local.DependsOn = UnionStrings(local.DependsOn, incoming.DependsOn)
	local.DesignPhases = unionDesignPhases(local.DesignPhases, incoming.DesignPhases)
	local.ReviewRounds = unionReviewRounds(local.ReviewRounds, incoming.ReviewRounds)
}

// resolveBothCompleteSync 确定性解决双完成冲突：更早的完成赢整个完成块（先完成者
// 是真实完成）；时间戳相等时按完成块规范 JSON 序。
func resolveBothCompleteSync(local, incoming *TaskState) {
	if local.CompletedAt == nil || incoming.CompletedAt == nil {
		return
	}
	switch {
	case incoming.CompletedAt.UnixNano() < local.CompletedAt.UnixNano():
		adoptCompletionBlock(local, incoming)
	case incoming.CompletedAt.UnixNano() == local.CompletedAt.UnixNano():
		if completionCanon(incoming) < completionCanon(local) {
			adoptCompletionBlock(local, incoming)
		}
	}
}

// canonBlockLess 报告块决胜中传入是否应顶替本地：空（"null"/"{}"/"[]"/`""`）恒
// 败；两个非空块按规范 JSON 字节序。
func canonBlockLess(incoming, local any) bool {
	ci, cl := canonicalJSON(incoming), canonicalJSON(local)
	ei, el := isEmptyCanon(ci), isEmptyCanon(cl)
	if ei != el {
		return !ei // incoming 非空、本地空 → 传入胜
	}
	return ci < cl
}

func isEmptyCanon(c string) bool {
	return c == `` || c == `null` || c == `{}` || c == `[]` || c == `""`
}

// completionCanon 是完成块的规范字节形（决胜键）。刻意不含 ReviewRounds：它走
// 并集（方向无关）而非替换，不应动摇块身份。含 AcceptanceForeign——同命令不同
// 信任标志是不同块（标志即执行闸；按方向渗漏破坏交换律）。
func completionCanon(s *TaskState) string {
	return canonicalJSON([]any{s.CompletedAt, s.ReviewPassed, s.ReviewedHeadCommit, s.ReviewedChangeHash, s.Score, s.Assignment, s.Acceptance, s.AcceptanceForeign})
}

// adoptCompletionBlock 整块采纳传入完成块——ReviewRounds 除外：它是只追加历史，
// 被取代侧累积的 review pass 并集进胜者而非被覆盖（覆盖会丢本地 review 历史，
// 且因只有一个方向发生采纳而按方向分叉）。
func adoptCompletionBlock(local, incoming *TaskState) {
	local.CompletedAt = incoming.CompletedAt
	local.ReviewPassed = incoming.ReviewPassed
	local.ReviewedHeadCommit = incoming.ReviewedHeadCommit
	local.ReviewedChangeHash = incoming.ReviewedChangeHash
	local.ReviewRounds = unionReviewRounds(local.ReviewRounds, incoming.ReviewRounds)
	local.Score = incoming.Score
	local.Assignment = incoming.Assignment
	local.Acceptance = incoming.Acceptance
	// AcceptanceForeign 随验收块一起走（防外来 Run 命令任意执行的执行闸——
	// project_import 在受信路径设置它）；只采纳命令不采纳标志会静默解除该闸。
	local.AcceptanceForeign = incoming.AcceptanceForeign
}

// canonicalizeSync 按内容键排序每个记录集合——让合并输出与到达顺序无关的收尾步。
// 每个集合先按身份键去重并取确定性胜者：合并时的并集只过滤传入对本地，单侧既有
// 的重复否则会幸存并把方向渗进字节。
func canonicalizeSync(s *TaskState) {
	s.History = dedupByKey(s.History, gateContentKey, func(a, b TaskGateResult) TaskGateResult { return a })
	// 时间序：末条必须是最新门禁活动——lastGateAt（僵尸剪枝锚，state.go）读
	// History 尾部，feed 也按时间重排。完全并列时 Passed 排在 Failed 后，让
	// 取末条的「愈合」判读稳定。
	sort.SliceStable(s.History, func(i, j int) bool {
		a, b := s.History[i], s.History[j]
		if a.CompletedAt.UnixNano() != b.CompletedAt.UnixNano() {
			return a.CompletedAt.UnixNano() < b.CompletedAt.UnixNano()
		}
		if a.Gate != b.Gate {
			return a.Gate < b.Gate
		}
		if a.HeadCommit != b.HeadCommit {
			return a.HeadCommit < b.HeadCommit
		}
		return !a.Passed && b.Passed
	})
	s.Decisions = dedupByKey(s.Decisions, decisionKey, func(a, b Decision) Decision { return earlierTime(a, b, a.DecidedAt, b.DecidedAt) })
	sort.SliceStable(s.Decisions, func(i, j int) bool { return decisionKey(s.Decisions[i]) < decisionKey(s.Decisions[j]) })
	s.Findings = dedupByKey(s.Findings, findingKey, func(a, b Finding) Finding { return earlierTime(a, b, a.RaisedAt, b.RaisedAt) })
	sort.SliceStable(s.Findings, func(i, j int) bool { return findingKey(s.Findings[i]) < findingKey(s.Findings[j]) })
	s.Blockers = dedupByKey(s.Blockers, blockerKey, func(a, b Blocker) Blocker { return earlierTime(a, b, a.RaisedAt, b.RaisedAt) })
	sort.SliceStable(s.Blockers, func(i, j int) bool { return blockerKey(s.Blockers[i]) < blockerKey(s.Blockers[j]) })
	s.Artifacts = dedupByKey(s.Artifacts, artifactKey, func(a, b Artifact) Artifact {
		if canonicalJSON(a) <= canonicalJSON(b) {
			return a
		}
		return b
	})
	sort.SliceStable(s.Artifacts, func(i, j int) bool { return artifactKey(s.Artifacts[i]) < artifactKey(s.Artifacts[j]) })
	s.NextSteps = dedupStrings(s.NextSteps)
	sort.Strings(s.NextSteps)
	s.DependsOn = dedupStrings(s.DependsOn)
	sort.Strings(s.DependsOn)
	s.SessionLinks = normalizeSessionLinks(s.SessionLinks)
	sort.SliceStable(s.SessionLinks, func(i, j int) bool { return sessionLinkKey(s.SessionLinks[i]) < sessionLinkKey(s.SessionLinks[j]) })
	s.ReviewRounds = dedupByKey(s.ReviewRounds, reviewRoundKey, func(a, b ReviewRound) ReviewRound { return a })
	sort.SliceStable(s.ReviewRounds, func(i, j int) bool { return reviewRoundKey(s.ReviewRounds[i]) < reviewRoundKey(s.ReviewRounds[j]) })
	s.DesignPhases = dedupByKey(s.DesignPhases, func(p DesignPhase) string { return canonicalJSON(p) }, func(a, b DesignPhase) DesignPhase { return a })
	sort.SliceStable(s.DesignPhases, func(i, j int) bool { return canonicalJSON(s.DesignPhases[i]) < canonicalJSON(s.DesignPhases[j]) })
}

// dedupByKey 折叠共享身份键的条目，保留 pick 选出的确定性胜者。首次出现占位；
// 胜者原位替换（顺序无关——调用方随后排序）。
func dedupByKey[T any](xs []T, key func(T) string, pick func(cur, cand T) T) []T {
	// 保持 nil/空表示：make([]T,0,…) 会把 nil 切片变非 nil 空切片，而
	// canonicalJSON 区分两者（"null" vs "[]"）——这种表示翻转会让决胜键跨同步
	// 轮次翻转。
	if len(xs) == 0 {
		return xs
	}
	idx := map[string]int{}
	out := make([]T, 0, len(xs))
	for _, x := range xs {
		k := key(x)
		if i, ok := idx[k]; ok {
			out[i] = pick(out[i], x)
			continue
		}
		idx[k] = len(out)
		out = append(out, x)
	}
	return out
}

// earlierTime 返回时间戳更早的条目；完全并列回落到规范 JSON 小者（完全确定性）。
func earlierTime[T any](a, b T, ta, tb time.Time) T {
	if ta.UnixNano() != tb.UnixNano() {
		if ta.UnixNano() < tb.UnixNano() {
			return a
		}
		return b
	}
	if canonicalJSON(a) <= canonicalJSON(b) {
		return a
	}
	return b
}

// dedupStrings 折叠重复字符串。
func dedupStrings(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// 内容键：带 ID 记录按 ID 键；空 ID 退化情形（存量/畸形）按 "\xff"+内容键，
// 不同的空 ID 条目仍确定性排序，相同条目保持可互换。
func decisionKey(d Decision) string {
	if d.ID != `` {
		return d.ID
	}
	return "\xff" + d.Content
}

func findingKey(f Finding) string {
	if f.ID != `` {
		return f.ID
	}
	return "\xff" + f.Content
}

func blockerKey(b Blocker) string {
	if b.ID != `` {
		return b.ID
	}
	return "\xff" + b.Content
}

func artifactKey(a Artifact) string {
	if a.Path != `` {
		return a.Path + "\x00" + a.Kind
	}
	return "\xff" + a.Kind + "\x00" + a.Note
}

func sessionLinkKey(l SessionLink) string {
	return l.SessionID + "\x00" + pad20(l.JoinedAt.UnixNano()) + "\x00" + l.Tool
}

func reviewRoundKey(r ReviewRound) string {
	return pad20(r.ReviewedAt.UnixNano()) + "\x00" + r.HeadCommit + "\x00" + r.ChangeHash
}

// pad20 把 int64 零填充到定宽，使键的字典序 == 数值序（裸十进制串跨位宽会错序：
// "10" < "9"）。
func pad20(n int64) string {
	return fmt.Sprintf(`%020d`, n)
}

// normalizeSessionLinks 折叠单个状态内的重复 SessionID（合并去重只过滤传入对
// 本地——本地既有重复会幸存）。胜者规则与合并规则一致：非 Imported 优先，其次
// 最早 JoinedAt。
func normalizeSessionLinks(links []SessionLink) []SessionLink {
	byID := map[string]int{}
	out := make([]SessionLink, 0, len(links))
	for _, l := range links {
		idx, ok := byID[l.SessionID]
		if !ok {
			byID[l.SessionID] = len(out)
			out = append(out, l)
			continue
		}
		cur := out[idx]
		switch {
		case cur.Imported && !l.Imported:
			out[idx] = l
		case cur.Imported == l.Imported && l.JoinedAt.UnixNano() < cur.JoinedAt.UnixNano():
			out[idx] = l
		}
	}
	return out
}

// resolveRecordConflictsSync 用确定性胜者（主时间戳早者，规范 JSON 破平）裁决两侧
// 同 ID 不同内容的冲突。并集只拿传入过滤本地（本地保留自己的副本），不做本步则
// 双侧编辑的胜者是碰巧为本地的那侧——方向渗进字节。仅受信路径：不可信合并刻意
// 保持本地权威（外来内容不得顶替本地）。
func resolveRecordConflictsSync(local, incoming *TaskState) {
	local.Decisions = resolveByID(local.Decisions, incoming.Decisions, func(d Decision) string { return d.ID }, func(d Decision) time.Time { return d.DecidedAt })
	local.Findings = resolveByID(local.Findings, incoming.Findings, func(f Finding) string { return f.ID }, func(f Finding) time.Time { return f.RaisedAt })
	local.Blockers = resolveByID(local.Blockers, incoming.Blockers, func(b Blocker) string { return b.ID }, func(b Blocker) time.Time { return b.RaisedAt })
	for _, in := range incoming.Artifacts {
		for i, l := range local.Artifacts {
			if artifactKey(l) == artifactKey(in) && canonicalJSON(in) < canonicalJSON(l) {
				local.Artifacts[i] = in
			}
		}
	}
	// SessionLinks：同 session 不同内容冲突。unionCollaborative 在 sid 冲突时保本地
	// （不可信权威），方向相关——此处用与 normalizeSessionLinks 相同的确定性规则
	// 裁决：非 Imported（本机锚定）胜幽灵链，其次更早 JoinedAt。
	for _, in := range incoming.SessionLinks {
		for i, l := range local.SessionLinks {
			if l.SessionID != in.SessionID {
				continue
			}
			switch {
			case l.Imported && !in.Imported:
				local.SessionLinks[i] = in
			case l.Imported == in.Imported && in.JoinedAt.UnixNano() < l.JoinedAt.UnixNano():
				local.SessionLinks[i] = in
			}
		}
	}
}

// resolveByID 当传入侧携带同非空 ID 但内容不同的条目时，用确定性胜者替换本地条目。
func resolveByID[T any](local, incoming []T, id func(T) string, at func(T) time.Time) []T {
	for _, in := range incoming {
		if id(in) == `` {
			continue
		}
		for i, l := range local {
			if id(l) != id(in) || canonicalJSON(l) == canonicalJSON(in) {
				continue
			}
			local[i] = earlierTime(l, in, at(l), at(in))
		}
	}
	return local
}

// unionReviewRounds 按内容键并集 review rounds（双未完成侧各自累积本地 review
// pass；完成情形由完成块采纳覆盖）。
func unionReviewRounds(local, incoming []ReviewRound) []ReviewRound {
	seen := map[string]bool{}
	for _, r := range local {
		seen[reviewRoundKey(r)] = true
	}
	for _, r := range incoming {
		if !seen[reviewRoundKey(r)] {
			seen[reviewRoundKey(r)] = true
			local = append(local, r)
		}
	}
	return local
}

// unionDesignPhases 按规范 JSON 键并集设计阶段。
func unionDesignPhases(local, incoming []DesignPhase) []DesignPhase {
	seen := map[string]bool{}
	for _, p := range local {
		seen[canonicalJSON(p)] = true
	}
	for _, p := range incoming {
		if !seen[canonicalJSON(p)] {
			seen[canonicalJSON(p)] = true
			local = append(local, p)
		}
	}
	return local
}

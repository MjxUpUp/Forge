package taskpipeline

import (
	"fmt"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// health.go: read-only delegation-health detection (design §12/§15 phase 5 可观测). A task's
// delegation can stall in three ways: a zombie (offered unclaimed / claimed with the claimer
// gone / a question left unanswered / a task no one keeps) or a deadlock (a dependency that can
// never deliver). This file REPORTS those signals; it never mutates state. The actual claimed→
// offered recovery is Abandon() in types.go, triggered by `forge task reclaim` (cli/task_assignment.go)
// — task health only surfaces the signal so a stuck task is not silently invisible (§16 phase 5
// milestone: 卡住的任务主动暴露). All time-based helpers take `now` so tests can mock the clock.
//
// health.go：只读的分派健康检测（设计 §12/§15 阶段5 可观测）。任务的分派可能以三种方式停滞：
// 僵尸（offered 无人认领 / claimed 但认领方失联 / 问题无人答复 / 反复无人接手）或死锁（依赖永不可达）。
// 本文件只「报告」这些信号，绝不改状态。真正的 claimed→offered 回收是 types.go 的 Abandon()，
// 由 forge task reclaim（cli/task_assignment.go）触发——task health 只把信号上浮，使卡住的任务不致
// 静默隐形（§16 阶段5 里程碑：卡住的任务主动暴露）。所有基于时间的 helper 都接收 now，使测试可 mock 时钟。

// Zombie / deadlock thresholds (design §3/§12). A delegation idle past these windows is a zombie.
//
// 僵尸/死锁阈值（设计 §3/§12）。分派空闲超过这些窗口即为僵尸。
const (
	OfferedZombieTTL       = 7 * 24 * time.Hour // offered 自上次（重）派发无人认领超过此时长 → 僵尸
	ClaimedZombieTTL       = 7 * 24 * time.Hour // claimed 无 checklog 活动超过此时长 → 僵尸
	InputReqZombieTTL      = 7 * 24 * time.Hour // input-required 无活动超过此时长 → 僵尸
	RepeatAbandonThreshold = 2                  // AbandonedCount ≥ 此值 → 反复回收僵尸
)

// TaskHealth is the read-only health classification of one task's delegation. It carries the
// signals surfaced by task health / mine / dashboard so all three renderers share ONE truth about
// what "zombie" and "deadlocked" mean. Empty (no reasons, both flags false) == healthy.
//
// TaskHealth 是单个任务分派的只读健康分类。承载 task health / mine / 看板上浮的信号，使三处
// 渲染对「僵尸」「死锁」共享同一真相。空（无 reason、两标志皆 false）= 健康。
type TaskHealth struct {
	TaskRef        string   `json:"task_ref"`
	IsZombie       bool     `json:"is_zombie"`
	ZombieReasons  []string `json:"zombie_reasons,omitempty"`
	Deadlocked     bool     `json:"deadlocked"`
	DeadlockReason string   `json:"deadlock_reason,omitempty"`
}

// pointerTime dereferences a *time.Time, returning the zero time when nil. Centralized so every
// age calc treats a missing timestamp the same (cannot be aged → not flagged), avoiding a nil
// deref or a false-positive from a zero-but-present value.
//
// pointerTime 解引用 *time.Time，nil 时返回零值。集中化使每个年龄计算对缺失时间戳一致处理
// （无法老化 → 不标记），避免 nil 解引用或「存在但零值」导致的假阳性。
func pointerTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// lastChecklogActivity returns the newest RecordedAt among the task's checklog entries, or the
// zero time if there are none (or the log is unreadable). This is the "is anyone still working?"
// signal reused by every TTL check (design §15: "复用 checklog 活动判定"): if the newest evidence
// is older than the TTL window, the claimer/orchestrator has gone away.
//
// lastChecklogActivity 返回任务 checklog 条目中最新的 RecordedAt，无条目（或日志不可读）时
// 返回零值。这是「是否仍有人在推进?」的信号，被每个 TTL 检查复用（设计 §15「复用 checklog 活动判定」）：
// 若最新证据老于 TTL 窗口，认领方/编排器已失联。
func lastChecklogActivity(root, taskRef string) time.Time {
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, e := range entries {
		if e.RecordedAt.After(latest) {
			latest = e.RecordedAt
		}
	}
	return latest
}

// assignmentInFlight reports whether a task's delegation is still actively pending on an agent —
// offered (waiting claim), claimed (being worked), or input-required (waiting answer). Terminal
// states (delivered/failed/canceled), non-delegated tasks, and COMPLETED tasks are NOT in flight,
// so no zombie signal applies to them. This is the common gate for every zombie check. The
// IsComplete precondition honors the semantic IsZombie's doc comment already declared (a completed
// task is never a zombie): before the 2026-08-18 脱节修复 the pipeline and the assignment state
// machine did not talk, so a completed task could keep a suspended offered/claimed assignment —
// without this check it would be misreported as a zombie 7 days later.
//
// assignmentInFlight 报告任务的分派是否仍在某 agent 上活跃待办——offered（待认领）、claimed
// （进行中）或 input-required（待答复）。终态（delivered/failed/canceled）、非分派任务与已完成
// 任务都不在途，故不适用任何僵尸信号。这是所有僵尸检查的公共前置。IsComplete 前置兑现 IsZombie
// 注释早已声明的语义（已完成的任务永不是僵尸）：2026-08-18 脱节修复之前管线与分派状态机互不知
// 情，已完成任务可能悬置 offered/claimed 分派——没有此检查，它 7 天后会被误报僵尸。
func assignmentInFlight(s *TaskState) bool {
	if s == nil || s.Assignment == nil || s.IsComplete() {
		return false
	}
	switch s.Assignment.Status {
	case AssignOffered, AssignClaimed, AssignInputRequired:
		return true
	}
	return false
}

// effectiveTTL returns the per-task zombie-window override when the task sets one (s.TTL > 0),
// otherwise the supplied global default. This is the single read point for design §3/§9 --ttl:
// IsOfferedZombie / IsClaimedStale / IsInputReqStale each pass their own global constant here, so a
// task with a TTL is overridden uniformly and a task without is untouched. A nil state or
// non-positive TTL falls back — zero is the omitempty default, so legacy tasks pre-dating the field
// behave exactly as before.
//
// effectiveTTL 在任务设置了 per-task TTL（s.TTL > 0）时返回它，否则返回传入的全局默认。这是设计
// §3/§9 --ttl 的唯一读取点：IsOfferedZombie / IsClaimedStale / IsInputReqStale 各把自己的全局常量
// 传进来，设了 TTL 的任务被统一覆盖，没设的任务不受影响。nil state 或非正 TTL 回落——零值是
// omitempty 默认，故早于该字段的 legacy 任务行为完全不变。
func effectiveTTL(s *TaskState, globalDefault time.Duration) time.Duration {
	if s != nil && s.TTL > 0 {
		return s.TTL
	}
	return globalDefault
}

// IsOfferedZombie reports whether an offered task has sat unclaimed past OfferedZombieTTL, and
// its age since the baseline. The baseline is the NEWEST of OfferedAt and AbandonedAt: Abandon()
// (claimed→offered recovery) sets AbandonedAt=now, so a freshly-reclaimed task resets its clock
// and is not falsely flagged until it idles again. A missing OfferedAt (legacy state) cannot be
// aged → not flagged (avoids false positives).
//
// IsOfferedZombie 报告一个 offered 任务是否无人认领超过 OfferedZombieTTL，及其自基线的年龄。
// 基线取 OfferedAt 与 AbandonedAt 的最新值：Abandon()（claimed→offered 回收）置 AbandonedAt=now，
// 故刚被回收的任务重置时钟，不会在再次空闲前被误判。缺失 OfferedAt（legacy state）无法老化 →
// 不标记（避免假阳性）。
func IsOfferedZombie(s *TaskState, now time.Time) (bool, time.Duration) {
	if s == nil || s.Assignment == nil || s.Assignment.Status != AssignOffered {
		return false, 0
	}
	baseline := pointerTime(s.Assignment.OfferedAt)
	if ab := pointerTime(s.Assignment.AbandonedAt); ab.After(baseline) {
		baseline = ab
	}
	if baseline.IsZero() {
		return false, 0
	}
	age := now.Sub(baseline)
	return age > effectiveTTL(s, OfferedZombieTTL), age
}

// IsClaimedStale reports whether a claimed task has had no checklog activity past ClaimedZombieTTL
// — the claimer has gone away (design §3 "claimed 7 天无 checklog 活动"). The activity baseline is
// the newest of ClaimedAt and the task's last checklog RecordedAt: any gate progress since the
// claim resets the window, so an actively-worked task is never a false positive.
//
// IsClaimedStale 报告一个 claimed 任务是否已无 checklog 活动超过 ClaimedZombieTTL——认领方已
// 失联（设计 §3「claimed 7 天无 checklog 活动」）。活动基线取 ClaimedAt 与任务最新 checklog
// RecordedAt 的最新值：认领后任何门禁推进都重置窗口，故正在推进的任务永不会假阳性。
func IsClaimedStale(root string, s *TaskState, now time.Time) (bool, time.Duration) {
	if s == nil || s.Assignment == nil || s.Assignment.Status != AssignClaimed {
		return false, 0
	}
	baseline := pointerTime(s.Assignment.ClaimedAt)
	if act := lastChecklogActivity(root, s.TaskRef); act.After(baseline) {
		baseline = act
	}
	if baseline.IsZero() {
		return false, 0
	}
	age := now.Sub(baseline)
	return age > effectiveTTL(s, ClaimedZombieTTL), age
}

// IsInputReqStale reports whether an input-required task has had no progress past InputReqZombieTTL
// — the question has gone unanswered too long. The baseline is the NEWEST of ClaimedAt, QuestionAt
// (set by Question()), and last checklog activity. Question() requires Claim(), so ClaimedAt is
// always set for input-required state and is the fallback baseline when no gate has run yet: a
// task that was claimed, immediately questioned, then left silent must still surface (the canonical
// stuck-question case — QuestionAt≈ClaimedAt there, so it still flags — exactly what phase 5 exists
// to expose). QuestionAt dominating ClaimedAt prevents the symmetric false positive: a task claimed
// long ago but only recently sent back for clarification is not stale merely because the claim is
// old. Any newer checklog activity dominates both, so an actively-worked task is never a false
// positive; a missing ClaimedAt with no activity cannot be aged → not flagged.
//
// IsInputReqStale 报告一个 input-required 任务是否已无进展超过 InputReqZombieTTL——问题已太久未答复。
// Question() 要求 Claim()，故 input-required 态的 ClaimedAt 总已设置，并在尚未跑过门禁时作为兜底基线：
// 一个被认领后立刻回抛、随后无人响应的任务仍须上浮（典型卡问题场景——正是阶段5要暴露的）。任何更新的
// checklog 活动占优，故正在推进的任务永不会假阳性；ClaimedAt 缺失且无活动则无法老化 → 不标记。
func IsInputReqStale(root string, s *TaskState, now time.Time) (bool, time.Duration) {
	if s == nil || s.Assignment == nil || s.Assignment.Status != AssignInputRequired {
		return false, 0
	}
	baseline := pointerTime(s.Assignment.ClaimedAt)
	// QuestionAt (set by Question()) dominates ClaimedAt when newer: a task claimed long ago but
	// only recently sent back for clarification should NOT be flagged stale merely because the claim
	// is old — the question is the freshest signal. In the canonical claim→immediately-question→
	// silent case, QuestionAt≈ClaimedAt so the stuck task still surfaces.
	//
	// QuestionAt（Question() 设置）新于 ClaimedAt 时占优：很久前认领、近期才回抛的任务不应仅因认领
	// 久远就被标僵尸——回抛才是最新信号。在典型「认领→立即回抛→静默」场景下 QuestionAt≈ClaimedAt，
	// 故卡住的任务仍上浮。
	if qa := pointerTime(s.Assignment.QuestionAt); qa.After(baseline) {
		baseline = qa
	}
	if act := lastChecklogActivity(root, s.TaskRef); act.After(baseline) {
		baseline = act
	}
	if baseline.IsZero() {
		return false, 0
	}
	age := now.Sub(baseline)
	return age > effectiveTTL(s, InputReqZombieTTL), age
}

// IsRepeatAbandon reports whether an in-flight task has been reclaimed (claimed→offered via
// Abandon) RepeatAbandonThreshold or more times — a flaky task no worker keeps claiming.
// Terminal/complete tasks with an abandonment history are not flagged: the signal is "this is
// stuck NOW", and a delivered task is not stuck.
//
// IsRepeatAbandon 报告一个在途任务是否已被回收（claimed→offered 经 Abandon）达到
// RepeatAbandonThreshold 次或更多——一个无人愿意持续认领的反复任务。带回收历史但已终态/完成的
// 任务不标记：信号是「此刻卡住」，已交付的任务并未卡住。
func IsRepeatAbandon(s *TaskState) bool {
	if !assignmentInFlight(s) {
		return false
	}
	return s.Assignment.AbandonedCount >= RepeatAbandonThreshold
}

// IsZombie reports whether a task shows ANY zombie signal, collecting the human-readable reasons.
// Shared by task mine, the dashboard, and task health so all three agree on "zombie" (a single
// truth source — never drift between renderers). A task not in flight, or complete, is never a
// zombie. The reasons use the design's shorthand (offered>7d / claimed>TTL / abandoned_count≥2 /
// input-required>7d) so the CLI, JSON, and docs all read the same vocabulary.
//
// IsZombie 报告一个任务是否有任何僵尸信号，并收集人类可读的 reason。被 task mine、看板、
// task health 共享，使三处对「僵尸」达成一致（单一真相源——渲染之间永不漂移）。不在途或已完成
// 的任务永不是僵尸。reason 用设计的简写（offered>7d / claimed>TTL / abandoned_count≥2 /
// input-required>7d），使 CLI、JSON、文档读到的词汇一致。
func IsZombie(root string, s *TaskState, now time.Time) (bool, []string) {
	if !assignmentInFlight(s) {
		return false, nil
	}
	var reasons []string
	if IsRepeatAbandon(s) {
		reasons = append(reasons, `abandoned_count≥2`)
	}
	if ok, _ := IsOfferedZombie(s, now); ok {
		reasons = append(reasons, `offered>7d`)
	}
	if ok, _ := IsClaimedStale(root, s, now); ok {
		reasons = append(reasons, `claimed>TTL`)
	}
	if ok, _ := IsInputReqStale(root, s, now); ok {
		reasons = append(reasons, `input-required>7d`)
	}
	return len(reasons) > 0, reasons
}

// DeadlockedDependency reports the first DependsOn ref that can NEVER deliver — pointing at a
// task in a terminally-failed assignment state (failed/canceled) or at a missing task (aborted/
// deleted). A dependent on such a ref is permanently blocked (design §12 废弃链). Cycles are a
// separate concern (HasDependencyCycle): a dependency that merely isn't delivered YET is pending,
// not deadlocked, and is intentionally not reported here. lookup returns (nil, err) for a missing
// ref; a missing ref is itself a dead-chain root.
//
// DeadlockedDependency 报告第一个永不可达的 DependsOn ref——指向处于终态失败分派（failed/
// canceled）的任务，或缺失（已 abort/删除）的任务。依赖此类 ref 的任务被永久阻塞（设计 §12
// 废弃链）。环是另一回事（HasDependencyCycle）：尚未交付但终会交付的依赖是 pending 而非死锁，
// 此处刻意不报。lookup 对缺失 ref 返回 (nil, err)；缺失 ref 本身即死链根。
func DeadlockedDependency(s *TaskState, lookup func(string) (*TaskState, error)) (string, bool) {
	if s == nil {
		return ``, false
	}
	for _, ref := range s.DependsOn {
		if ref == `` {
			continue
		}
		dep, err := lookup(ref)
		if err != nil || dep == nil {
			return ref, true // 依赖缺失（已 abort）→ 永久阻塞
		}
		if dep.Assignment != nil {
			switch dep.Assignment.Status {
			case AssignFailed, AssignCanceled:
				return ref, true // 依赖终态失败 → 永久阻塞
			}
		}
	}
	return ``, false
}

// HasDependencyCycle does a defensive DFS over a task's transitive DependsOn to detect ANY
// reachable cycle — one involving the task itself OR one deeper in its dependency subtree (a task
// whose upstream can never deliver is permanently blocked either way). Cycles are rejected at
// AddDependency write time, so a cycle in state can only come from import or direct file
// corruption — this catches it and reports it rather than letting `task mine --blocked` spin
// forever. Uses a gray-set (in-stack) DFS — the standard back-edge test — so a cycle anywhere in
// the reachable graph is found; a `done` set bounds the walk on acyclic paths. lookup returns nil
// for a missing ref (treated as a leaf).
//
// HasDependencyCycle 对任务的传递 DependsOn 做防御性 DFS，检测「任何」可达环——含任务自身的环，或其
// 依赖子树更深处的环（上游永不可达则任务无论如何都被永久阻塞）。环在 AddDependency 写入时即被拒，故
// state 中的环只能来自 import 或文件损坏——此处捕获并报告，而非让 task mine --blocked 无限打转。
// 用 gray-set（在栈中）DFS——标准回边检测——故可达图任意位置的环都能被发现；done 集在无环路径上界定
// 遍历。lookup 对缺失 ref 返回 nil（视作叶子）。
func HasDependencyCycle(s *TaskState, lookup func(string) *TaskState) bool {
	if s == nil {
		return false
	}
	inStack := map[string]bool{}
	done := map[string]bool{}
	var dfs func(ref string) bool
	dfs = func(ref string) bool {
		if inStack[ref] {
			return true // 回边：ref 仍在当前 DFS 栈上 → 成环
		}
		if done[ref] {
			return false // 已完全探明且无环 → 跳过
		}
		inStack[ref] = true
		dep := lookup(ref)
		if dep != nil {
			for _, d := range dep.DependsOn {
				if d == `` {
					continue
				}
				if dfs(d) {
					return true
				}
			}
		}
		inStack[ref] = false
		done[ref] = true
		return false
	}
	return dfs(s.TaskRef)
}

// ClassifyTaskHealth computes the full read-only health of one task: its zombie signals (with
// reasons) plus its first deadlock cause. `root` is needed for checklog-activity judgments;
// `now` is injected for testability; the dependency lookups take the full state set so chains and
// cycles can be walked. Returned even for healthy tasks (all flags false) so callers can build a
// uniform table; IsZombie/Deadlocked flags are the predicates to filter on.
//
// ClassifyTaskHealth 计算单个任务的完整只读健康：僵尸信号（带 reason）+ 首个死锁成因。root
// 供 checklog 活动判断所需；now 注入便于测试；依赖 lookup 接收完整 state 集使链与环可遍历。
// 即便健康任务也返回（两标志皆 false），使调用方能构建一致表格；按 IsZombie/Deadlocked 标志过滤。
func ClassifyTaskHealth(root string, s *TaskState, now time.Time, lookupState func(string) (*TaskState, error), lookupCycle func(string) *TaskState) TaskHealth {
	h := TaskHealth{TaskRef: s.TaskRef}
	if zombie, reasons := IsZombie(root, s, now); zombie {
		h.IsZombie = true
		h.ZombieReasons = reasons
	}
	if ref, dead := DeadlockedDependency(s, lookupState); dead {
		h.Deadlocked = true
		h.DeadlockReason = fmt.Sprintf(`依赖 %s 已失败/撤回/缺失，永久阻塞`, ref)
	} else if HasDependencyCycle(s, lookupCycle) {
		h.Deadlocked = true
		h.DeadlockReason = `DependsOn 存在环（写入本应拒绝，疑似 import/损坏数据）`
	}
	return h
}

// childTerminal reports whether a child task has reached a state the orchestrator need not wait on
// further: delivered (success), or failed/canceled (terminal-failure). Mirrors IsDelivered's
// assigned-vs-unassigned split for the success case (an assigned child's delivery is its Assignment
// status alone; an unassigned child — ordinary/generic — has only IsComplete), and additionally
// accepts the two failure terminals per design §5 ("delivered 或终态"). Reusing IsDelivered's rule
// keeps "is this child done?" consistent with "is this dependency done?" (DependencyReady), so a
// code-task child that finishes its gates counts just like a delivered delegated child. An unassigned
// generic child reaches IsComplete via `forge task complete` (its own gates) — it is NOT stuck; the
// orchestrator just runs complete on the child (no Deliver path, since it has no Assignment).
//
// childTerminal 报告子任务是否已达编排器无需再等的态：delivered（成功）或 failed/canceled（终态失败）。
// 成功分支镜像 IsDelivered 的有/无分派拆分（有分派子任务的交付只看 Assignment 状态；无分派子任务
// ——普通/generic——只有 IsComplete），并按设计 §5「delivered 或终态」额外接受两种失败终态。复用
// IsDelivered 的规则使「子任务是否完成?」与「依赖是否完成?」（DependencyReady）一致——跑完门禁的
// code-task 子任务与已 delivered 的分派子任务同等对待。无分派的 generic 子任务经 forge task complete
// （其自身门禁）达 IsComplete——不会卡死，编排器只需对该子任务跑 complete（无 Assignment 故无 Deliver 路径）。
func childTerminal(s *TaskState) bool {
	if s == nil {
		return false
	}
	if s.Assignment != nil {
		switch s.Assignment.Status {
		case AssignDelivered, AssignFailed, AssignCanceled:
			return true
		}
		return false
	}
	return s.IsComplete()
}

// OrchestrationReady reports whether a parent (orchestrator) task is ready to complete: true when
// it has NO child tasks, or when ALL children (tasks whose ParentTaskRef points at parentRef) have
// reached a terminal state (delivered/failed/canceled, or a complete unassigned child — see
// childTerminal). When not ready, pending returns the still-non-terminal child refs in stable
// iteration order. This is the single truth source for design §5 "全部子任务 delivered 或终态 → 编排
// task 可 complete": surfaced as an advisory at complete-time and as a "ready" hint in task health.
// A parent with non-terminal children is still COMPLETABLE (design: 不强制 complete) — this helper
// only reports readiness, it never gates.
//
// OrchestrationReady 报告父（编排器）任务是否就绪可 complete：无子任务，或全部子任务（ParentTaskRef
// 指向 parentRef 的）已达终态（delivered/failed/canceled，或无分派子任务 complete——见 childTerminal）
// 时为 true。未就绪时 pending 返回仍非终态的子任务 ref，按稳定遍历序。这是设计 §5「全部子任务
// delivered 或终态 → 编排 task 可 complete」的单一真相源：在 complete 时 advisory 提示、在 task
// health 作「就绪」提示上浮。有非终态子任务的父任务仍可 complete（设计：不强制 complete）——本 helper
// 只报就绪态，从不作门禁。
func OrchestrationReady(states []*TaskState, parentRef string) (ready bool, pending []string) {
	if parentRef == `` {
		return true, nil
	}
	for _, s := range states {
		if s == nil || s.ParentTaskRef != parentRef {
			continue
		}
		if !childTerminal(s) {
			pending = append(pending, s.TaskRef)
		}
	}
	return len(pending) == 0, pending
}

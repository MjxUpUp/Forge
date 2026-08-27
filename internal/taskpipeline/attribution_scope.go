package taskpipeline

import (
	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/MjxUpUp/Forge/internal/review"
)

// TaskFingerprint computes the task-scoped source-change fingerprint with L3 attribution
// exclusions applied. CONTRACT: every task-mode fingerprint site — recording (review pass,
// acceptance snapshots) AND recomputation (task-complete gate, freshness checks) — must go
// through this one function: a hash recorded with exclusions and recomputed without them
// mismatches, and a mismatch on the recompute side means a false "审查后变更" hard block.
// Non-task mode keeps review.SourceChangesSince (whole tree — review is fail-safe there).
//
// TaskFingerprint 计算 task 作用域的源码变更指纹，应用 L3 归属排除。契约：所有
// task 模式指纹位点——记录侧（review pass、验收快照）与重算侧（task-complete 门禁、
// freshness 检查）——必须同走本函数：带排除记录的 hash 用不带排除重算必然失配，
// 而重算侧失配 = 假「审查后变更」硬阻断。非 task 模式保持 review.SourceChangesSince
//（全树——那边 review 是 fail-safe 方向）。
func TaskFingerprint(root string, state *TaskState, baseCommit string) (hash string, hasChanges bool, err error) {
	ref := ""
	if state != nil {
		ref = state.TaskRef
	}
	return review.SourceChangesSinceExcluded(root, baseCommit, ForeignAttributedPaths(root, ref))
}

// ForeignAttributedPaths returns the set of currently-changed working-tree paths that the
// L3 attribution ledger assigns to sessions anchored on OTHER INCOMPLETE tasks
// (multi-task-concurrency design §6, T3). This is the provably-foreign portion — the only
// thing any consumer may exclude. Everything not in this set (the own task's sessions'
// paths, and orphans the ledger cannot explain) stays visible: attribution is best-effort,
// and a miss must degrade toward inclusion (fail-safe), never toward hiding changes.
//
// Semantics contract for consumers:
//   - review fingerprints / taskChangedFiles: exclude ONLY this set (orphans stay —
//     unexplained changes must be reviewed/counted);
//   - HANDOFF 现场视图: excludes this set AND orphans (with honest count lines) — a
//     takeover view reconstructs THIS task's scene, not the whole tree's noise.
//
// FORGE_ATTRIBUTION=0 → empty set (the escape hatch: everything behaves pre-L3).
//
// ForeignAttributedPaths 返回 L3 归属台账判定为「其他未完成任务的锚定会话」所拥有的
// 当前工作树变更路径集合（multi-task-concurrency 设计 §6，T3）。这是可证明外来的
// 部分——消费方唯一可以排除的东西。不在此集合内的一切（本任务会话的路径、台账解释
// 不了的无主路径）保持可见：归属是尽力而为，漏判必须向包含方向降级（fail-safe），
// 绝不向隐藏变更方向降级。
//
// 消费方语义契约：
//   - review 指纹 / taskChangedFiles：只排除本集合（无主保留——解释不了的变更必须
//     被审查/计数）；
//   - HANDOFF 现场视图：排除本集合与无主（附诚实计数行）——接手视图还原的是本任务
//     现场，不是整棵树的噪音。
//
// FORGE_ATTRIBUTION=0 → 空集（逃生舱：一切回到 L3 之前的行为）。
func ForeignAttributedPaths(root, ownTaskRef string) map[string]bool {
	foreign := map[string]bool{}
	if !attribution.Enabled() || root == "" {
		return foreign
	}
	tasks, err := ListTaskStates(root)
	if err != nil {
		return foreign // 基建读不出任务清单：无外来可证，fail-safe 全保留
	}
	foreignSids := map[string]bool{}
	for _, t := range tasks {
		if t == nil || t.TaskRef == ownTaskRef || t.IsComplete() {
			continue
		}
		for _, l := range t.SessionLinks {
			if !l.Imported && l.SessionID != "" {
				foreignSids[l.SessionID] = true
			}
		}
	}
	if len(foreignSids) == 0 {
		return foreign
	}
	v := attribution.Reconcile(root)
	for sid, paths := range v.BySession {
		if !foreignSids[sid] {
			continue
		}
		for _, p := range paths {
			foreign[p] = true
		}
	}
	return foreign
}

package taskpipeline

import (
	"fmt"
	"path/filepath"
	"strings"

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
// （全树——那边 review 是 fail-safe 方向）。
func TaskFingerprint(root string, state *TaskState, baseCommit string) (hash string, hasChanges bool, err error) {
	ref := ""
	if state != nil {
		ref = state.TaskRef
	}
	return review.SourceChangesSinceExcluded(root, baseCommit, ForeignAttributedPaths(root, ref))
}

// ForeignAttributedPaths returns the set of currently-changed working-tree paths
// the L3 attribution ledger assigns to sessions anchored on OTHER INCOMPLETE tasks.
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
	ownSids := map[string]bool{}
	for _, t := range tasks {
		if t == nil || t.IsComplete() {
			continue
		}
		if t.TaskRef == ownTaskRef {
			for _, l := range t.SessionLinks {
				if !l.Imported && l.SessionID != "" {
					ownSids[l.SessionID] = true
				}
			}
			continue
		}
		for _, l := range t.SessionLinks {
			if !l.Imported && l.SessionID != "" {
				foreignSids[l.SessionID] = true
			}
		}
	}
	// B1 修正（review BLOCKER）：同时锚定在本任务与其他任务上的会话【绝不】算外来
	// ——单窗口顺序多任务（同 session 先开 A 再开 B，A 未完成）是显式支持形态，
	// 此时该 sid 在 A 视角被误标外来，A 自己的未提交变更会被静默排除出 review
	// 指纹（review-bypass）与接手视图（藏自己 WIP 在计数行后）。按契约 fail-safe
	// 向包含降级：判不出归属的宁可保留，绝不藏变更。
	for sid := range ownSids {
		delete(foreignSids, sid)
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

// AttributedPorcelain filters porcelain status lines down to this task's
// working scene: L3-ledger attribution excludes lines owned by other live task
// sessions and unattributable paths, each exclusion honestly counted.
//
// AttributedPorcelain 把 porcelain 状态行过滤成本任务现场（multi-task-concurrency
// §6，T3）：L3 台账可证明归属其他未完成任务会话的行被剔除，无主路径同样
// 剔除——两者都以诚实的计数行补在末尾（接手视图还原的是本任务现场，不是整树噪音）。
// 降级向旧全树行为 fail-open：归属关闭、台账为空（升级前会话/无身份宿主）或
// 空集自然穿透（无行可滤）——藏掉任务自己的 WIP（空现场）比多显示噪音更糟。
// （2026-09 普查 A1 补齐：自 cli/task_continuity.go 下沉——领域核
// ForeignAttributedPaths 本就住本包，组装与它同址；行由调用方经
// attribution.PorcelainLines 取得。）
func AttributedPorcelain(root string, state *TaskState, lines []string) []string {
	if !attribution.Enabled() {
		return lines
	}
	view := attribution.Reconcile(root)
	// fail-open 触发：台账对变更集的归属数为零——升级前会话、无身份宿主、或台账没看
	// 见的纯 bash 编辑。此时归属不携带任何信息，把全部标成「无主剔除」会渲染出空现场
	//（藏掉任务自己的 WIP——严格劣于带噪音的旧视图）。至少有一个路径被归属时，
	// 自己/外来/无主的切分才开始有意义。
	if len(view.BySession) == 0 {
		return lines
	}
	ref := ""
	if state != nil {
		ref = state.TaskRef
	}
	foreign := ForeignAttributedPaths(root, ref)
	orphanSet := map[string]bool{}
	for _, p := range view.Orphans {
		orphanSet[p] = true
	}
	kept := make([]string, 0, len(lines))
	excludedForeign, excludedOrphan := 0, 0
	for _, l := range lines {
		if len(l) < 3 {
			kept = append(kept, l)
			continue
		}
		p := l[3:]
		if idx := strings.Index(p, " -> "); idx >= 0 {
			p = p[idx+4:]
		}
		p = filepath.ToSlash(filepath.Clean(strings.Trim(p, `"`)))
		switch {
		case foreign[p]:
			excludedForeign++
		case orphanSet[p]:
			excludedOrphan++
		default:
			kept = append(kept, l)
		}
	}
	if excludedForeign > 0 {
		kept = append(kept, fmt.Sprintf("· 已排除 %d 个其他任务会话的未提交文件（非本任务现场）", excludedForeign))
	}
	if excludedOrphan > 0 {
		kept = append(kept, fmt.Sprintf("· 已排除 %d 个无归属未提交文件（台账无法解释，git status 可查）", excludedOrphan))
	}
	return kept
}

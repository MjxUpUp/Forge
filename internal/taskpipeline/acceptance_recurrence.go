package taskpipeline

// acceptance_recurrence.go —— 验收同因复发判定（loop-contract B1-1）。
// 外层循环最常用的停止条件是"连续两次失败原因相同 → 停，交给人"。这里把它做成
// 机械判定：同一验收最近 N 次失败（N = loop.recurrence_threshold）指纹全部相同、
// 且相邻两次之间 ChangeHash 都不同——改了代码、失败没变。没改动的重跑不计数，
// 避免"什么都没做、连跑两次"被误判。盲区（ChangeHash 为空、验收命令自改源码）
// 在 meta 里如实标注，由 docs/plans/loop-contract-2026-10-proposal.md 第五节的
// 精确度度量兜底。
//
// acceptance_recurrence.go — same-cause acceptance recurrence (loop-contract
// B1-1). The outer loop's most common stop rule is "two consecutive failures
// with the same cause → stop, escalate". This makes it mechanical: the last N
// failures of one criterion (N = loop.recurrence_threshold) share a
// fingerprint and every adjacent pair differs in ChangeHash — code changed,
// the failure did not. Reruns without a change do not count. Blind spots
// (empty ChangeHash, self-mutating acceptance commands) are flagged in meta.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// AcceptanceRecurrence is one detected same-cause recurrence.
//
// AcceptanceRecurrence 是一次检出的同因复发。
type AcceptanceRecurrence struct {
	// CriterionIdx is the 0-based index into TaskState.Acceptance.
	CriterionIdx int
	Run          string
	Fingerprint  string
	// ChangeHashes are the pre-run hashes of the counted failures, oldest first.
	ChangeHashes []string
	// Count is how many consecutive same-fingerprint failures were counted.
	Count int
	// ChangedBetween lists files whose content digest differs between the first
	// and last counted failure (added, removed or modified) — what was touched
	// while the failure stayed the same.
	//
	// ChangedBetween 列出首末两次失败之间内容指纹有变化的文件（新增、删除或修改）
	// ——失败没变时动过的文件。
	ChangedBetween []string
	// Note carries the blind-spot annotation ("" when none applies).
	//
	// Note 是盲区标注（无则为空）。
	Note string
}

// DetectAcceptanceRecurrences scans every criterion's FailHistory and returns
// the recurrences at the configured threshold (none when threshold ≤ 0). Pure.
//
// DetectAcceptanceRecurrences 扫描每条验收的 FailHistory，按配置阈值返回复发
// （阈值 ≤ 0 时不判）。纯函数。
func DetectAcceptanceRecurrences(cfg LoopConfig, acceptance []tasktypes.AcceptanceCriterion) []AcceptanceRecurrence {
	n := cfg.RecurrenceThreshold
	if n <= 0 {
		return nil
	}
	var out []AcceptanceRecurrence
	for i, c := range acceptance {
		if c.Passed || len(c.FailHistory) < n {
			continue
		}
		window := c.FailHistory[len(c.FailHistory)-n:]
		if r, ok := recurrenceInWindow(window); ok {
			r.CriterionIdx = i
			r.Run = c.Run
			out = append(out, r)
		}
	}
	return out
}

// recurrenceInWindow applies the two conditions to a window of failures:
// identical fingerprints, and a different ChangeHash between every adjacent
// pair. A window with any empty ChangeHash is unmeasurable (no git / no HEAD)
// and never counts.
//
// recurrenceInWindow 对一个失败窗口套用两个条件：指纹全同、相邻两次 ChangeHash
// 都不同。任一 ChangeHash 为空（非 git / 无 HEAD）即不可度量，不计。
func recurrenceInWindow(window []tasktypes.AcceptanceFailure) (AcceptanceRecurrence, bool) {
	r := AcceptanceRecurrence{Fingerprint: window[0].Fingerprint, Count: len(window)}
	for i, f := range window {
		if f.Fingerprint != r.Fingerprint {
			return r, false
		}
		if f.ChangeHash == "" {
			return r, false
		}
		if i > 0 && f.ChangeHash == window[i-1].ChangeHash {
			return r, false
		}
		r.ChangeHashes = append(r.ChangeHashes, f.ChangeHash)
	}
	r.ChangedBetween = digestDiff(window[0].Changed, window[len(window)-1].Changed)
	// 验收命令自改源码的疑点：hash 变了但没有任何文件的内容指纹变化——改动落在
	// TaskChangedFiles 之外（例如 genMarks 之外的生成物），最多多计一次尝试。
	if len(r.ChangedBetween) == 0 {
		r.Note = "self-mutating=suspect（ChangeHash 变化但改动文件内容指纹未变）"
	}
	return r, true
}

// digestDiff lists paths whose digest differs between a and b (sorted).
//
// digestDiff 列出 a 与 b 之间指纹不同的路径（已排序）。
func digestDiff(a, b map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for p, h := range a {
		if bh, ok := b[p]; !ok || bh != h {
			seen[p] = true
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			seen[p] = true
		}
	}
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// DeadEndContent renders the auto-recorded dead-end decision for a recurrence
// (the Decision.Content; idempotency key of the auto record).
//
// DeadEndContent 渲染复发对应的自动死路记录正文（Decision.Content；自动记录的
// 幂等键）。
func (r AcceptanceRecurrence) DeadEndContent() string {
	return fmt.Sprintf("验收 #%d（%s）在 %d 次改动后仍以同一指纹 %s 失败", r.CriterionIdx+1, r.Run, r.Count, r.Fingerprint)
}

// DeadEndRationale renders the evidence line of the auto-recorded dead end.
//
// DeadEndRationale 渲染自动死路记录的证据行。
func (r AcceptanceRecurrence) DeadEndRationale() string {
	hashes := make([]string, 0, len(r.ChangeHashes))
	for _, h := range r.ChangeHashes {
		if len(h) > 8 {
			h = h[:8]
		}
		hashes = append(hashes, h)
	}
	s := "改动指纹 " + strings.Join(hashes, " → ")
	if len(r.ChangedBetween) > 0 {
		s += "；动过的文件: " + strings.Join(r.ChangedBetween, ", ")
	}
	if r.Note != "" {
		s += "；" + r.Note
	}
	return s
}

// RecordRecurrenceDeadEnds appends one dead-end Decision per recurrence to the
// state (By = "forge"), skipping any whose Content is already recorded. Returns
// how many were added. Caller persists.
//
// RecordRecurrenceDeadEnds 为每个复发在 state 上追加一条死路 Decision（By =
// "forge"），Content 已存在的跳过。返回新增条数，调用方负责落盘。
func RecordRecurrenceDeadEnds(state *TaskState, recs []AcceptanceRecurrence) int {
	added := 0
	for _, r := range recs {
		content := r.DeadEndContent()
		dup := false
		for _, d := range state.Decisions {
			if d.IsDeadEnd() && d.Content == content {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		state.AddDecision(tasktypes.Decision{
			Content:   content,
			By:        "forge",
			Rationale: r.DeadEndRationale(),
			Kind:      tasktypes.DecisionKindDeadEnd,
		})
		added++
	}
	return added
}

package taskpipeline

// loopedge.go —— 审查回环回边（artifact-chain-workflow.md「回边语义」节，G2）：
// 轮次预算 + 复发检测 + exhausted 升级人工。无限循环在架构上不可表达：回环
// 要么收敛（指纹不复现）、要么耗尽（轮龄到顶）、要么升级（到人）——终止权外置。
//
// 机械判定三事实（复用既有轮次结构——ReviewRounds 与 Finding.Round 已由
// review pass / finding 登记维护；唯一新增的是人工重置写下的 LoopResetRound）：
//  1. 轮龄：open finding 的存活轮龄 = len(ReviewRounds) - max(Finding.Round,
//     LoopResetRound) + 1；
//     轮龄 ≥ schema 回边 max_rounds 且仍 open → 耗尽（rounds-exhausted）。
//  2. 复发：finding 指纹 = 规范化内容 sha256[0:16]；finding 标 fixed 时指纹进
//     ResolvedPrints；此后同指纹新 finding 登记 = 复活 → 立即耗尽
//     （finding-recurrence）。
//  3. exhausted → complete BLOCKED（升级人工），仅人工 --reset-loop 或 abort
//     可清除；逃生舱沿 artifact-chain 域（FORGE_ARTIFACT_CHAIN）落审计。
//     --reset-loop 记 LoopResetRound，轮龄从该轮重新起算（发放完整新预算）。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// LoopFingerprint computes the normalized content fingerprint of a finding
// (trim → lowercase → whitespace-collapsed → sha256[0:16]).
//
// LoopFingerprint 计算 finding 的规范化内容指纹（trim → 小写 → 空白折叠 →
// sha256 前 16 hex）——大小写与空白差异不算复现，实质同文即复现。
func LoopFingerprint(content string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(content)), " ")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])[:16]
}

// loopBudgetRounds returns the review→implement edge's max_rounds (default 3
// when the edge is not declared).
func loopBudgetRounds(root string) int {
	chain, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}
	if e, ok := chain.EdgeByFrom(artifactchain.EdgeFromReview); ok {
		return e.MaxRounds
	}
	return artifactchain.DefaultMaxRounds
}

// findingLoopAge returns how many review cycles an open finding has survived
// (≥1) within the current loop budget. Legacy findings without Round count from
// cycle 1; after a human reset the age counts from LoopResetRound (a fresh
// budget — loop-contract B0).
//
// findingLoopAge 返回 open finding 在当前回环预算内存活的审查轮数（≥1）。无 Round
// 的旧 finding 从第 1 轮起算；人工重置后从 LoopResetRound 起算（新预算，B0）。
func findingLoopAge(state *TaskState, f tasktypes.Finding) int {
	round := f.Round
	if round < state.LoopResetRound {
		round = state.LoopResetRound
	}
	if round < 1 {
		round = 1
	}
	current := len(state.ReviewRounds)
	age := current - round + 1
	if age < 1 {
		age = 1
	}
	return age
}

// evaluateLoopExhaustion recomputes exhaustion from scratch: any open finding
// whose loop age reached the budget → exhausted (rounds-exhausted). Returns
// the exhaustion marker to set (nil = not exhausted).
//
// evaluateLoopExhaustion 从零重算耗尽态：任一 open finding 轮龄达到预算 → 耗尽。
// 幂等——每次调用都基于当前状态重新判定，不依赖历史标记。
func evaluateLoopExhaustion(root string, state *TaskState) *LoopExhaustion {
	maxRounds := loopBudgetRounds(root)
	for i := range state.Findings {
		f := &state.Findings[i]
		if f.Status != "open" && f.Status != "" {
			continue
		}
		if findingLoopAge(state, *f) >= maxRounds {
			return &LoopExhaustion{
				Reason: LoopReasonRounds,
				Detail: fmt.Sprintf("finding %q 轮龄 %d ≥ 预算 %d（open 未修）", f.ID, findingLoopAge(state, *f), maxRounds),
				At:     time.Now(),
			}
		}
	}
	return nil
}

// MarkLoopExhaustedIfDue re-evaluates the repair budget and persists the
// exhaustion marker when due (idempotent — markers don't double). Consumed by
// review pass and finding registration. Returns the marker set this call.
//
// MarkLoopExhaustedIfDue 重估修复预算并在到顶时持久化耗尽标记（幂等）。消费方：
// review pass 与 finding 登记。
func MarkLoopExhaustedIfDue(root string, state *TaskState) *LoopExhaustion {
	if state == nil || state.LoopExhausted != nil {
		return nil
	}
	marker := evaluateLoopExhaustion(root, state)
	if marker == nil {
		return nil
	}
	if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
		if s.LoopExhausted == nil {
			s.LoopExhausted = marker
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "[loopedge] exhaustion persist failed:", err)
		return nil
	}
	state.LoopExhausted = marker // 调用方内存与盘上一致（Merge 只改锁内副本）
	fmt.Fprintf(os.Stderr, "[loopedge] ⚠️ 回环耗尽（%s: %s）——升级人工：forge task finding --reset-loop --note \"<裁决>\"\n",
		marker.Reason, marker.Detail)
	return marker
}

// RecordResolvedPrint appends a finding's content fingerprint to
// ResolvedPrints when it transitions out of open (fixed/wontfix) — the memory
// that makes revival detectable (progress: fingerprint).
//
// RecordResolvedPrint 在 finding 离开 open（fixed/wontfix）时把内容指纹记入
// ResolvedPrints（去重）——复发可检测的记忆本体。
func RecordResolvedPrint(root string, state *TaskState, content string) {
	fp := LoopFingerprint(content)
	_ = MergeOrPersistTaskState(root, state, func(s *TaskState) error {
		for _, p := range s.ResolvedPrints {
			if p == fp {
				return nil
			}
		}
		s.ResolvedPrints = append(s.ResolvedPrints, fp)
		return nil
	})
}

// CheckFindingRevival is called on finding registration: a fingerprint already
// in ResolvedPrints is a REVIVAL (same issue declared fixed, came back) →
// immediately exhausts the loop. Exhaustion is persisted. Returns true when
// revival detected.
//
// CheckFindingRevival 在 finding 登记时调用：指纹命中 ResolvedPrints 即复活
// （同一问题宣称修复后回来）→ 立即耗尽回环。返回是否检出复活。
func CheckFindingRevival(root string, state *TaskState, content string) bool {
	fp := LoopFingerprint(content)
	found := false
	for _, p := range state.ResolvedPrints {
		if p == fp {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if state.LoopExhausted == nil {
		marker := &LoopExhaustion{
			Reason: LoopReasonRecurrence,
			Detail: fmt.Sprintf("fingerprint=%s 已解决 finding 复活", fp),
			At:     time.Now(),
		}
		state.LoopExhausted = marker // 调用方内存与盘上一致（Merge 只改锁内副本）
		if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
			if s.LoopExhausted == nil {
				s.LoopExhausted = marker
			}
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, "[finding] revival persist failed:", err)
		}
	}
	fmt.Fprintf(os.Stderr, "[finding] ⚠️ 已解决 finding 复活（指纹 %s）——回环立即耗尽，升级人工\n", fp)
	return true
}

// CheckLoopExhausted is the task-complete pre-flight segment (「回边语义」机制一):
// exhaustion is recomputed from live findings AND the persisted marker is
// honored — either blocks complete with an escalate-to-human message.
// Escape: FORGE_ARTIFACT_CHAIN / override --artifact-chain (chain + loop edges
// are one declared subsystem, shared to avoid env sprawl).
//
// CheckLoopExhausted 是 task-complete 的回环耗尽 pre-flight：重算与持久标记双轨
// 任一命中即拦——耗尽后唯一出口是人工重置。逃生舱沿 artifact-chain 域落审计。
func CheckLoopExhausted(root string, state *TaskState) []string {
	if state == nil {
		return nil
	}
	if escapeDisabled(state, escapeArtifactChain, artifactChainDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  "escape-hatch: loop exhaustion bypassed (per-task override or FORGE_ARTIFACT_CHAIN=disable)",
			Meta:    map[string]string{"escape.gate": "artifact-chain", "escape.reason": checklog.EscapeReasonOverride, "escape.owner": state.TaskRef},
		})
		return nil
	}
	// 双轨：重算（幂等，轮龄事实）优先；持久标记（复活的不可逆记录）兜底。
	// 重算命中先置内存标记再落盘（loop-contract B0）：MergeOrPersistTaskState 只改
	// 锁内副本，不回写调用方 state——只落盘会让本次 pre-flight 拿旧内存判定、首次
	// 检出放行一次。落盘失败同样当场拦（fail-closed），标记待下次重算再落盘。
	// 落盘时对锁内重载的副本重新判定：调用方快照之后若恰有一次 --reset-loop
	// 落盘，旧快照的判定不得把刚重置的盘上状态再标回耗尽（本次仍按快照拦）。
	if marker := evaluateLoopExhaustion(root, state); marker != nil && state.LoopExhausted == nil {
		state.LoopExhausted = marker
		if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
			if s.LoopExhausted == nil && evaluateLoopExhaustion(root, s) != nil {
				s.LoopExhausted = marker
			}
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, "[loopedge] exhaustion persist failed (blocking anyway):", err)
		}
	}
	if state.LoopExhausted == nil {
		return nil
	}
	ex := state.LoopExhausted
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckLoopExhausted,
		Passed:  false,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail: fmt.Sprintf("loop exhausted (%s): %s — escalation required, complete blocked",
			ex.Reason, ex.Detail),
	})
	return []string{fmt.Sprintf("回环耗尽（%s）: %s——升级人工裁决（forge task finding --reset-loop --note \"<裁决>\"）", ex.Reason, ex.Detail)}
}

// clearFailHistory drops every criterion's FailHistory — the human reset is the
// only exit of an acceptance-recurrence verdict (loop-contract B1-1), so the
// window must restart from the new direction.
//
// clearFailHistory 清空每条验收的 FailHistory——人工重置是 acceptance-recurrence
// 判词的唯一出口（loop 契约 B1-1），窗口必须从新方向重新起算。
func clearFailHistory(s *TaskState) {
	for i := range s.Acceptance {
		s.Acceptance[i].FailHistory = nil
	}
}

// ResetLoopExhaustion clears the exhaustion state after a human decision
// (`forge task finding --reset-loop --note`) and restarts the loop budget
// (and clears every FailHistory, see clearFailHistory):
// LoopResetRound = len(ReviewRounds)+1, so open findings age from the next
// review cycle (a full fresh budget of max_rounds). Audit row lands; the
// caller's state is updated in step with disk. ResolvedPrints are retained —
// a later revival still trips, unless the human also clears.
//
// ResetLoopExhaustion 在人工裁决后清除耗尽态并重开回环预算：LoopResetRound =
// len(ReviewRounds)+1，open finding 从下一轮起重新计龄（一份完整的 max_rounds
// 新预算）。审计行落地；调用方内存 state 与盘上同步。ResolvedPrints 保留——
// 后续复活仍会被抓，除非人工同时清理。
func ResetLoopExhaustion(root string, state *TaskState, note string) error {
	now := time.Now()
	var resetRound int
	if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
		s.LoopExhausted = nil
		resetRound = len(s.ReviewRounds) + 1
		s.LoopResetRound = resetRound
		s.LoopResetAt = &now
		clearFailHistory(s)
		return nil
	}); err != nil {
		return err
	}
	state.LoopExhausted = nil
	state.LoopResetRound = resetRound
	state.LoopResetAt = &now
	clearFailHistory(state)
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckLoopExhausted,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelWarn,
		TaskRef: state.TaskRef,
		Detail:  LoopResetDetailPrefix + note,
	})
	return nil
}

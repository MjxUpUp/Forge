package taskpipeline

// loopedge.go —— 审查回环回边（artifact-chain-workflow.md「回边语义」节，G2）：
// 轮次预算 + 复发检测 + exhausted 升级人工。无限循环在架构上不可表达：回环
// 要么收敛（指纹不复现）、要么耗尽（轮龄到顶）、要么升级（到人）——终止权外置。
//
// 机械判定三事实（全部复用既有轮次结构——ReviewRounds 与 Finding.Round 已由
// review pass / finding 登记维护，本文件零新计数器）：
//  1. 轮龄：open finding 的存活轮龄 = len(ReviewRounds) - Finding.Round + 1；
//     轮龄 ≥ schema 回边 max_rounds 且仍 open → 耗尽（rounds-exhausted）。
//  2. 复发：finding 指纹 = 规范化内容 sha256[0:16]；finding 标 fixed 时指纹进
//     ResolvedPrints；此后同指纹新 finding 登记 = 复活 → 立即耗尽
//     （finding-recurrence）。
//  3. exhausted → complete BLOCKED（升级人工），仅人工 --reset-loop 或 abort
//     可清除；逃生舱沿 artifact-chain 域（FORGE_ARTIFACT_CHAIN）落审计。

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
// (≥1). Legacy findings without Round count from cycle 1.
func findingLoopAge(state *TaskState, f tasktypes.Finding) int {
	round := f.Round
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
		if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
			if s.LoopExhausted == nil {
				s.LoopExhausted = &LoopExhaustion{
					Reason: LoopReasonRecurrence,
					Detail: fmt.Sprintf("fingerprint=%s 已解决 finding 复活", fp),
					At:     time.Now(),
				}
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
	if marker := evaluateLoopExhaustion(root, state); marker != nil && state.LoopExhausted == nil {
		_ = MergeOrPersistTaskState(root, state, func(s *TaskState) error {
			if s.LoopExhausted == nil {
				s.LoopExhausted = marker
			}
			return nil
		})
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

// ResetLoopExhaustion clears the exhaustion state after a human decision
// (`forge task finding --reset-loop --note`). Audit row lands. ResolvedPrints
// are retained — a later revival still trips, unless the human also clears.
//
// ResetLoopExhaustion 在人工裁决后清除耗尽态与轮龄（审计行落地）。
// ResolvedPrints 保留——后续复活仍会被抓，除非人工同时清理。
func ResetLoopExhaustion(root string, state *TaskState, note string) error {
	if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
		s.LoopExhausted = nil
		return nil
	}); err != nil {
		return err
	}
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckLoopExhausted,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelWarn,
		TaskRef: state.TaskRef,
		Detail:  "loop reset by human decision: " + note,
	})
	return nil
}

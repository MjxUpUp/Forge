package taskpipeline

import (
	"testing"
	"time"
)

// TestStripForeignGateSignals_ControlFlow pins the 2026-08-15 trust-boundary fix: the strip must
// clear CONTROL-FLOW fields (CompletedAt / Overrides / forged task-complete History / stale
// CurrentGate), not just the result fields — a bundle or repo-committed task file carrying them
// would otherwise disable every CompletedAt==nil-guarded hard check and auto-pass the complete gate.
//
// TestStripForeignGateSignals_ControlFlow 钉住 2026-08-15 信任边界修复：剥离必须清「控制流」字段
// （CompletedAt / Overrides / 伪造的 task-complete History / 陈旧 CurrentGate），而非只有结果字段——
// 带着它们的 bundle 或 repo 提交的 task 文件否则会关掉所有 CompletedAt==nil 守卫的硬检查并让
// complete 门禁自动通过。
func TestStripForeignGateSignals_ControlFlow(t *testing.T) {
	now := time.Now()
	s := &TaskState{
		TaskRef:            `feat/foreign`,
		CurrentGate:        ``, // set via RecordGateResult below
		Kind:               TaskKindGeneric,
		ReviewPassed:       true,
		SessionLinks: []SessionLink{
			{SessionID: `src-machine-session`},
		},
		ReviewedHeadCommit: `aaa111`,
		ReviewedChangeHash: `hash-aaa`,
		CompletedAt:        &now,
		Overrides:          TaskOverrides{WorkActivity: `disable`, TestCoverage: `disable`},
		Assignment: &Assignment{
			Agent:  `kimi`,
			Role:   `frontend`,
			Status: AssignDelivered,
		},
		Acceptance: []AcceptanceCriterion{
			{Run: `go test ./...`, Expected: `ok`, Passed: true, AcceptedHeadCommit: `aaa111`, Output: `ok`},
		},
	}
	s.RecordGateResult(`task-implement`, true, `aaa111`)
	s.RecordGateResult(`task-verify`, true, `aaa111`)
	s.RecordGateResult(`task-complete`, true, `aaa111`)

	StripForeignGateSignals(s)

	if s.ReviewPassed || s.ReviewedHeadCommit != `` || s.ReviewedChangeHash != `` {
		t.Errorf(`result signals should be cleared: ReviewPassed=%v HeadCommit=%q ChangeHash=%q`, s.ReviewPassed, s.ReviewedHeadCommit, s.ReviewedChangeHash)
	}
	if s.CompletedAt != nil {
		t.Error(`CompletedAt should be nil (master switch of the CompletedAt==nil hard-check guards)`)
	}
	if s.Kind != `` {
		t.Errorf(`Kind should be reset (generic short-circuits every gate AND skips the acceptance pre-flight via completeGenericTask; re-establishing it is a local abort+restart decision, not inherited), got %q`, s.Kind)
	}
	if s.IsGeneric() {
		t.Error(`IsGeneric() must be false after strip (one foreign string must not disable all hard checks)`)
	}
	if s.Overrides != (TaskOverrides{}) {
		t.Errorf(`Overrides should be zeroed (escape hatches are local decisions), got %+v`, s.Overrides)
	}
	for _, h := range s.History {
		// EVERY passed entry must go, not just task-complete: executor's gate-prerequisite walk
		// treats a foreign `task-verify: Passed` as satisfying the chain and skips every hard
		// check living inside task-verify (review follow-up 2026-08-15).
		//
		// 所有已通过条目都必须剔除，不只是 task-complete：executor 的门禁前置链会把外来的
		// `task-verify: Passed` 当已满足，跳过 task-verify 内部的全部硬检查（2026-08-15 复审）。
		if h.Passed {
			t.Errorf(`passed History entries must be dropped (gates are earned locally), got %+v`, s.History)
		}
	}
	if s.CurrentGate != `task-implement` {
		t.Errorf(`CurrentGate should re-derive to task-implement (task re-walks all gates locally), got %q`, s.CurrentGate)
	}
	if s.IsComplete() {
		t.Error(`IsComplete() must be false after strip (task never completed locally)`)
	}
	if s.Assignment != nil {
		t.Errorf(`Assignment should be dropped (foreign delivered claim must not release local DependsOn gates), got %+v`, s.Assignment)
	}
	for i, l := range s.SessionLinks {
		if !l.Imported {
			t.Errorf(`SessionLinks[%d] should be ghosted Imported=true (foreign session ids never anchor local sessions), got %+v`, i, l)
		}
	}
	if len(s.Acceptance) != 1 || s.Acceptance[0].Run != `go test ./...` {
		t.Fatalf(`acceptance spec should survive as handoff: %+v`, s.Acceptance)
	}
	if s.Acceptance[0].Passed || s.Acceptance[0].AcceptedHeadCommit != `` || s.Acceptance[0].Output != `` {
		t.Errorf(`acceptance result signals should be cleared, got %+v`, s.Acceptance[0])
	}
	if !s.AcceptanceForeign {
		t.Error(`AcceptanceForeign should be set when acceptance entries survive the strip`)
	}
}

// TestStripForeignGateSignals_NoAcceptanceNoMarker pins the no-acceptance edge: with zero criteria
// there is nothing executable to distrust, so AcceptanceForeign stays false (verify-acceptance's
// trust gate must not fire on an empty spec).
//
// TestStripForeignGateSignals_NoAcceptanceNoMarker 钉住无验收边界：零条 criterion 时无可执行物可
// 不信任，AcceptanceForeign 保持 false（verify-acceptance 的受信门不得对空 spec 触发）。
func TestStripForeignGateSignals_NoAcceptanceNoMarker(t *testing.T) {
	s := &TaskState{TaskRef: `feat/foreign-empty`}
	now := time.Now()
	s.CompletedAt = &now
	StripForeignGateSignals(s)
	if s.AcceptanceForeign {
		t.Error(`AcceptanceForeign should stay false with no acceptance entries`)
	}
	if s.CompletedAt != nil {
		t.Error(`CompletedAt should still be cleared`)
	}
}

// TestStripForeignGateSignals_FailedGateHistoryKept pins provenance semantics: a FAILED gate entry
// (e.g. a task-verify that failed on the source machine) is history, not a trust signal — it is kept
// verbatim so the import shows the real gate progression. Only task-complete is special (it is the
// completion CLAIM).
//
// TestStripForeignGateSignals_FailedGateHistoryKept 钉住溯源语义：失败的门禁条目（如源机器上跑挂的
// task-verify）是历史而非信任信号——原样保留，让 import 显示真实门禁进度。只有 task-complete
// 特殊（它是完成「声明」）。
func TestStripForeignGateSignals_FailedGateHistoryKept(t *testing.T) {
	s := &TaskState{TaskRef: `feat/foreign-failed`}
	s.RecordGateResult(`task-implement`, true, `aaa111`)
	s.RecordGateResult(`task-verify`, false, `aaa111`)
	StripForeignGateSignals(s)
	foundFailedVerify := false
	for _, h := range s.History {
		if h.Gate == `task-verify` && !h.Passed {
			foundFailedVerify = true
		}
	}
	if !foundFailedVerify {
		t.Errorf(`failed task-verify History is provenance and should be kept, got %+v`, s.History)
	}
}

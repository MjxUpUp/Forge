package taskpipeline

import (
	"strings"
	"testing"
)

// TestAdviseAcceptance pins both branches of the acceptance advisory:
// (a) gated (non-generic) task with ZERO acceptance criteria must be reminded —
// completing without --accept leaves the evidence chain claim-dominated (2026-09-07
// real-world: project-policy-p234 ratio 0.08, 4 det / 47 agent-claim; the only
// unforgeable whitelisted evidence, acceptance/test-run, was zero because none was
// registered at start and CheckAcceptanceFresh passes silently on empty);
// (b) generic tasks are exempt (research/handoff tasks legitimately carry no specs);
// (c) the existing branch — registered but not all passed — keeps its reminder.
//
// TestAdviseAcceptance 钉住 acceptance advisory 的两个分支：
// （a）门禁任务（非 generic）零验收必须提醒——零 --accept 完成时证据链必然自述主导
// （2026-09-07 实证：project-policy-p234 ratio 0.08，4 det / 47 agent-claim；白名单里
// 不可伪造的 acceptance/test-run 两类证据为零，且 CheckAcceptanceFresh 对空验收静默
// 放行，缺口对 agent 不可见）；
// （b）generic 任务豁免（调研/接续任务本就无验收）；
// （c）既有分支——登记了但未全通过——提醒保持。
func TestAdviseAcceptance(t *testing.T) {
	t.Run(`zero_acceptance_gated_task_advised`, func(t *testing.T) {
		got := acceptanceAdvisory(&TaskState{TaskRef: `feat/big`, Kind: ``})
		if got == `` {
			t.Fatal(`门禁任务零验收应有 advisory（缺口不可见是 2026-09-07 nudge 根因）`)
		}
		for _, want := range []string{`验收`, `spec`, `自述`} {
			if !strings.Contains(got, want) {
				t.Errorf(`advisory 缺关键词 %q：got %q`, want, got)
			}
		}
	})
	t.Run(`zero_acceptance_generic_task_silent`, func(t *testing.T) {
		if got := acceptanceAdvisory(&TaskState{TaskRef: `research/x`, Kind: `generic`}); got != `` {
			t.Errorf(`generic 任务零验收应静默（无验收是合法形态），got %q`, got)
		}
	})
	t.Run(`registered_not_passed_keeps_reminder`, func(t *testing.T) {
		st := &TaskState{TaskRef: `feat/y`, Kind: ``}
		st.Acceptance = []AcceptanceCriterion{{Run: `go test ./...`}}
		got := acceptanceAdvisory(st)
		if !strings.Contains(got, `verify-acceptance`) {
			t.Errorf(`登记未跑分支应保留既有提醒（spec-as-gate），got %q`, got)
		}
	})
	t.Run(`registered_and_passed_silent`, func(t *testing.T) {
		st := &TaskState{TaskRef: `feat/z`, Kind: ``}
		st.Acceptance = []AcceptanceCriterion{{Run: `go test ./...`, Passed: true}}
		if got := acceptanceAdvisory(st); got != `` {
			t.Errorf(`已登记已实跑应静默，got %q`, got)
		}
	})
}

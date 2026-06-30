package cli

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestAppendConclusion_WritesAndDirectives 钉住 task.go 的接线边界：appendConclusion
// 必须把结论落盘到 .forge/act/conclusions.jsonl，且 Directive 反映证据强度。这是 act 包
// 单测（BuildConclusion/Append 已全测）之外、TaskState→落盘 的胶水层覆盖。
func TestAppendConclusion_WritesAndDirectives(t *testing.T) {
	root := t.TempDir()

	// 裸 state：无验收、无评分、无证据 → NoData 强度 → 不 nudge → Directive 空。结论仍应落盘。
	state := &taskpipeline.TaskState{TaskRef: `feat/wire`, SessionID: `sess-wire`}
	d := appendConclusion(root, state)
	if d != `` {
		t.Errorf(`bare state Directive=%q want 空（NoData 不 nudge）`, d)
	}
	c, err := act.Latest(root)
	if err != nil {
		t.Fatalf(`Latest: %v`, err)
	}
	if c == nil {
		t.Fatal(`结论未落盘（appendConclusion 没写 .forge/act/conclusions.jsonl）`)
	}
	if c.TaskRef != `feat/wire` {
		t.Errorf(`TaskRef=%q want feat/wire`, c.TaskRef)
	}
	if c.RetrospectiveNudge {
		t.Error(`NoData+nil score 不应 RetrospectiveNudge`)
	}
}

func TestAppendConclusion_AcceptanceCounted(t *testing.T) {
	root := t.TempDir()
	// 验收 1/2 通过：接线应把 pass/total 准确传给结论（防漏传字段静默归零）。
	state := &taskpipeline.TaskState{
		TaskRef: `feat/acc`,
		Acceptance: []taskpipeline.AcceptanceCriterion{
			{Run: `echo ok`, Expected: `ok`, Passed: true},
			{Run: `false`, Passed: false},
		},
	}
	appendConclusion(root, state)
	c, err := act.Latest(root)
	if err != nil || c == nil {
		t.Fatalf(`Latest: %v / nil`, err)
	}
	if c.AcceptancePass != 1 || c.AcceptanceTotal != 2 {
		t.Errorf(`Acceptance=%d/%d want 1/2（接线漏传 pass/total）`, c.AcceptancePass, c.AcceptanceTotal)
	}
}

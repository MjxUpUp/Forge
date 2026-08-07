package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/skilltrigger"
)

// TestRecordSkillTriggerHits pins that each fired canonical skill is recorded to checklog with the right shape:
// CheckSkillTrigger / Passed / Checked / deterministic source / session id preserved / per-skill detail. This is the
// fix for the dogfood 0-trigger blind spot — without these records, `forge skills usage`/`effectiveness` cannot see
// which canonical skills actually fired (skill-trigger injected silently into AdditionalContext with zero trail).
//
// TestRecordSkillTriggerHits 钉住：每个触发的 canonical skill 按正确形状落进 checklog——
// CheckSkillTrigger / Passed / Checked / deterministic 来源 / session id 保留 / per-skill detail。这是
// dogfood 0 触发盲区的修复——无此记录，`forge skills usage`/`effectiveness` 看不到哪些 canonical skill 真触发过
// （skill-trigger 静默注入 AdditionalContext、零轨迹）。
func TestRecordSkillTriggerHits(t *testing.T) {
	dir := t.TempDir()
	ctx := skilltrigger.Context{
		Event:     "UserPromptSubmit",
		ToolName:  "Write",
		SessionID: "sess-abc",
	}
	hits := []skilltrigger.Hit{
		{Skill: "implementation-discipline", Reason: "coding_intent"},
		{Skill: "tdd-cycle", Reason: "test_keyword"},
	}
	recordSkillTriggerHits(dir, ctx, hits)

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != len(hits) {
		t.Fatalf("recorded %d entries, want %d", len(entries), len(hits))
	}
	seen := map[string]bool{}
	for i, e := range entries {
		if e.Check != checklog.CheckSkillTrigger {
			t.Fatalf("entry %d Check=%q, want %q", i, e.Check, checklog.CheckSkillTrigger)
		}
		if !e.Passed || !e.Checked {
			t.Fatalf("entry %d Passed=%v Checked=%v, want both true", i, e.Passed, e.Checked)
		}
		if e.Source != checklog.EvidenceDeterministic {
			t.Fatalf("entry %d Source=%q, want deterministic", i, e.Source)
		}
		if e.SessionID != "sess-abc" {
			t.Fatalf("entry %d SessionID=%q, want sess-abc", i, e.SessionID)
		}
		seen[e.Detail] = true
	}
	// 每个 hit 的 skill 名必须出现在某条 detail 里（被动触发可观测的核心）。
	//
	// Each hit's skill name must appear in some detail line (the core of passive-trigger observability).
	for _, h := range hits {
		found := false
		for d := range seen {
			if strings.Contains(d, h.Skill) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("skill %q 未在任何 detail 中出现（被动触发应可观测）", h.Skill)
		}
	}
}

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
	recordSkillTriggerHits(dir, ctx, hits, t.TempDir(), "", "1.99.0-test")

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
		// L1 送达章：agent="" → claude 默认行（UserPromptSubmit 上 additionalContext 可达），
		// Delivered/Channel/ForgeVersion 必须逐条落盘——usage 漏斗的送达分母依赖这些字段。
		//
		// L1 delivery stamp: agent="" takes the claude default row (additionalContext reachable
		// on UserPromptSubmit); Delivered/Channel/ForgeVersion must be stamped on every entry —
		// the usage funnel's delivery denominator depends on these fields.
		if e.Delivered == nil || !*e.Delivered {
			t.Fatalf("entry %d Delivered=%v, want pointer to true", i, e.Delivered)
		}
		if e.Channel != "claude/additionalContext" {
			t.Fatalf("entry %d Channel=%q, want claude/additionalContext", i, e.Channel)
		}
		if e.ForgeVersion != "1.99.0-test" {
			t.Fatalf("entry %d ForgeVersion=%q, want 1.99.0-test", i, e.ForgeVersion)
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

// TestRecordSkillTriggerHits_Meta 钉住 v2 结构化证据载荷：matched_keyword/match_source/
// when/trigger_index/trigger_sig/prompt_hash/prompt_len 逐键落盘；cooldown 抑制计数在下次
// 真实触发时回填 Meta 并清零；摘录默认关（FORGE_TRIGGER_EXCERPT 未设时不落 excerpt 键）。
//
// TestRecordSkillTriggerHits_Meta pins the v2 structured evidence payload:
// matched_keyword/match_source/when/trigger_index/trigger_sig/prompt_hash/prompt_len all
// land; the cooldown suppression count backfills Meta at the next actual fire and resets;
// excerpts stay off by default (no excerpt key without FORGE_TRIGGER_EXCERPT).
func TestRecordSkillTriggerHits_Meta(t *testing.T) {
	dir := t.TempDir()
	counterDir := t.TempDir()
	ctx := skilltrigger.Context{
		Event:       "UserPromptSubmit",
		SessionID:   "sess-meta",
		ProjectRoot: "/proj/a",
		Prompt:      "编译报错了",
	}
	hits := []skilltrigger.Hit{{
		Skill:          "compile-fix-loop",
		Reason:         "r",
		MatchedKeyword: "编译报错",
		MatchSource:    skilltrigger.MatchSourcePrompt,
		TriggerIndex:   1,
		TriggerSig:     "ab12cd34",
		PromptHash:     "hash0000aaaa",
		PromptLen:      5,
		Trigger:        skilltrigger.Trigger{Event: "UserPromptSubmit", When: "", Keywords: []string{"编译报错"}},
	}}
	// 预置 3 次 cooldown 抑制：下次触发应回填 suppressed_since_last=3 且计数清零。
	counter := skilltrigger.NewFileSuppressedCounter(counterDir)
	for i := 0; i < 3; i++ {
		if err := counter.Incr("sess-meta", "compile-fix-loop"); err != nil {
			t.Fatalf("Incr: %v", err)
		}
	}
	recordSkillTriggerHits(dir, ctx, hits, counterDir, "", "1.99.0-test")

	entries, err := checklog.LoadAll(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("LoadAll err=%v entries=%d", err, len(entries))
	}
	meta := entries[0].Meta
	if meta == nil {
		t.Fatal("Meta 不应为 nil（v2 结构化证据缺失）")
	}
	wantKeys := map[string]string{
		checklog.MetaKeyMatchedKeyword:      "编译报错",
		checklog.MetaKeyMatchSource:         skilltrigger.MatchSourcePrompt,
		checklog.MetaKeyTriggerIndex:        "1",
		checklog.MetaKeyTriggerSig:          "ab12cd34",
		checklog.MetaKeyPromptHash:          "hash0000aaaa",
		checklog.MetaKeyPromptLen:           "5",
		checklog.MetaKeySuppressedSinceLast: "3",
	}
	for k, want := range wantKeys {
		if meta[k] != want {
			t.Errorf("Meta[%s]=%q, want %q", k, meta[k], want)
		}
	}
	if _, ok := meta[checklog.MetaKeyExcerpt]; ok {
		t.Error("摘录默认应关（无 FORGE_TRIGGER_EXCERPT 时不得落 excerpt 键）")
	}
	if _, ok := meta[checklog.MetaKeyWhen]; ok {
		t.Error("keyword-only 触发（When 空）不应落 when 键——缺键=不适用语义")
	}
	// 回填后计数清零：再次触发（同 session）不得再带上旧计数。
	recordSkillTriggerHits(dir, ctx, hits, counterDir, "", "1.99.0-test")
	entries2, _ := checklog.LoadAll(dir)
	last := entries2[len(entries2)-1]
	if v, ok := last.Meta[checklog.MetaKeySuppressedSinceLast]; ok && v != "0" {
		t.Fatalf("回填后计数应清零, got suppressed_since_last=%q", v)
	}
}

// TestRecordSuppressed_StopCapWarn 钉住 stop-max-rounds 抑制的 warn advisory：单条、
// Level=warn、Detail 无 " hit (" 标记（SkillFromTriggerDetail 返回 "" → usage/funnel
// 计数零污染）、Meta 带 cause/skills。
//
// TestRecordSuppressed_StopCapWarn pins the stop-max-rounds warn advisory: ONE entry,
// Level=warn, Detail without the " hit (" marker (SkillFromTriggerDetail returns "" →
// zero pollution of usage/funnel counts), Meta carrying cause/skills.
func TestRecordSuppressed_StopCapWarn(t *testing.T) {
	dir := t.TempDir()
	ctx := skilltrigger.Context{Event: "Stop", SessionID: "sess-cap"}
	suppressed := []skilltrigger.Suppressed{
		{Skill: "a", Cause: skilltrigger.SuppressStopCap},
		{Skill: "b", Cause: skilltrigger.SuppressStopCap},
	}
	recordSuppressed(dir, ctx, suppressed, t.TempDir(), "", "1.99.0-test")
	entries, err := checklog.LoadAll(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("stop-cap 应记 1 条 advisory, err=%v entries=%d", err, len(entries))
	}
	e := entries[0]
	if e.Level != checklog.LevelWarn {
		t.Fatalf("Level=%q, want warn", e.Level)
	}
	if checklog.SkillFromTriggerDetail(e.Detail) != "" {
		t.Fatalf("advisory Detail 不含 hit 标记（防计数污染）, got %q", e.Detail)
	}
	if e.Meta[checklog.MetaKeyCause] != skilltrigger.SuppressStopCap {
		t.Fatalf("Meta cause=%q", e.Meta[checklog.MetaKeyCause])
	}
	if e.Meta[checklog.MetaKeySkills] != "a,b" {
		t.Fatalf("Meta skills=%q, want a,b", e.Meta[checklog.MetaKeySkills])
	}
	// cooldown 抑制不落 log 条目（只进计数器）。
	recordSuppressed(dir, ctx, []skilltrigger.Suppressed{{Skill: "c", Cause: skilltrigger.SuppressCooldown}}, t.TempDir(), "", "1.99.0-test")
	entries2, _ := checklog.LoadAll(dir)
	if len(entries2) != 1 {
		t.Fatalf("cooldown 抑制不应另落条目（只计数回填）, got %d", len(entries2))
	}
}

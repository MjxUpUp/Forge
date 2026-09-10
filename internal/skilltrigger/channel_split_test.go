package skilltrigger

import (
	"strings"
	"testing"
	"time"
)

// Channel-split tests (design A, docs/design/harness-fixes-a-g-2026-09.md): decision points
// (UserPromptSubmit/SessionStart) keep the full load-mode injection; action points
// (PreToolUse/PostToolUse/Stop) accept only triggers that declare an Inline one-line action —
// load-mode "please load the skill" on action points measured 0–2% conversion on both machines
// and is now suppressed with cause=non-decision-point (burning no cooldown budget).
//
// 通道拆分测试（设计 A）：决策点（UserPromptSubmit/SessionStart）保持完整加载式推送；动作点
// （PreToolUse/PostToolUse/Stop）只接受声明了 Inline 一行动作的 trigger——动作点上的「请加载
// skill」两机实测转化 0–2%，现以 cause=non-decision-point 抑制（不消耗 cooldown 预算）。
func TestEval_ActionPointChannelSplit(t *testing.T) {
	all := []SkillTriggers{
		{Skill: "no-inline", Triggers: []Trigger{
			{Event: "PreToolUse", Match: "Bash", Keywords: []string{"git commit"}},
			{Event: "UserPromptSubmit", Keywords: []string{"发版"}},
		}},
		{Skill: "with-inline", Triggers: []Trigger{{Event: "PreToolUse", Match: "Bash", Keywords: []string{"git commit"}, Inline: "提交前先跑聚焦测试", Follow: `go test\b`}}},
	}
	ctx := Context{Event: "PreToolUse", ToolName: "Bash", SessionID: "s1", Now: time.Now(),
		ToolInput: map[string]any{"command": "git commit -m x"}}
	hits, sup := Eval(ctx, all, NewInMemoryNoiseController())
	if len(hits) != 1 || hits[0].Skill != "with-inline" {
		t.Fatalf("action point must keep only the Inline-declaring trigger, hits=%+v", hits)
	}
	if hits[0].Mode != "inline" || hits[0].FollowPattern == "" {
		t.Fatalf("action-point hit must carry mode=inline and the follow pattern, got %+v", hits[0])
	}
	if len(sup) != 1 || sup[0].Skill != "no-inline" || sup[0].Cause != SuppressNonDecisionPoint {
		t.Fatalf("load-mode trigger on action point must be suppressed with cause=non-decision-point, got %+v", sup)
	}
	// 被分流的命中不消耗 cooldown：no-inline 在 PreToolUse 被分流后，同 session 的决策点
	//（其另一条 UPS 规则）仍可命中——若分流烧了 cooldown，这里会是 suppressed。
	upHits, upSup := Eval(Context{Event: "UserPromptSubmit", Prompt: "准备发版", SessionID: "s1", Now: time.Now()}, all, nil)
	if len(upHits) != 1 || upHits[0].Skill != "no-inline" || upHits[0].Mode != "load" {
		t.Fatalf("decision point keeps load mode with budget intact, hits=%+v sup=%+v", upHits, upSup)
	}
}

// TestEval_LoadModeFiresWhenInlineEmptyOnDecisionPoint pins the inverse: a trigger with only
// Inline (no separate UPS rule) still renders load-mode at decision points — Inline is an
// action-point enrichment, not a requirement at decision points.
//
// TestEval_LoadModeFiresWhenInlineEmptyOnDecisionPoint 钉住反向：只声明了 Inline 的 trigger
// 在决策点仍按 load 模式命中——Inline 是动作点补充，不是决策点的必填项。
func TestEval_LoadModeFiresWhenInlineEmptyOnDecisionPoint(t *testing.T) {
	all := []SkillTriggers{{Skill: "s", Triggers: []Trigger{{Event: "UserPromptSubmit", Keywords: []string{"发版"}, Inline: "发版前跑 release-readiness 清单"}}}}
	hits, sup := Eval(Context{Event: "UserPromptSubmit", Prompt: "准备发版", SessionID: "s1", Now: time.Now()}, all, NewInMemoryNoiseController())
	if len(hits) != 1 || hits[0].Mode != "load" || len(sup) != 0 {
		t.Fatalf("decision point: expect one load-mode hit and no suppression, got %+v / %+v", hits, sup)
	}
}

// TestRender_InlineOneLine pins the action-point render: one line, no skill path, no 请加载
// pointer — the test-nudge form. Load-mode hits keep the full block.
//
// TestRender_InlineOneLine 钉住动作点渲染：一行、无 skill 路径、无「请加载」指引——
// test-nudge 形态。load 模式命中保持完整块。
func TestRender_InlineOneLine(t *testing.T) {
	hits := []Hit{
		{Skill: "test-discipline", Reason: "提交纪律", Mode: "inline", Trigger: Trigger{Inline: "提交前先跑聚焦测试"}},
		{Skill: "merge-release-choreography", Reason: "合并", Mode: "load", SkillDir: "C:/x/skills/merge-release-choreography"},
	}
	out := Render(hits, Context{Event: "PreToolUse"}, nil)
	if !strings.Contains(out, "【test-discipline】 提交前先跑聚焦测试") {
		t.Fatalf("inline hit must render as one line with the action text:\n%s", out)
	}
	if strings.Contains(out, "test-discipline/SKILL.md") {
		t.Fatalf("inline hit must NOT carry the skill path:\n%s", out)
	}
	if !strings.Contains(out, "merge-release-choreography/SKILL.md") {
		t.Fatalf("load-mode hit keeps the full pointer block:\n%s", out)
	}
}

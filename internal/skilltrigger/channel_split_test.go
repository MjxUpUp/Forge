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
	noise := NewInMemoryNoiseController()
	hits, sup := Eval(ctx, all, noise)
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
	// 同一 noise 实例：nil 控制器会短路 ShouldFire 使断言恒真（评审）——复用首调用的
	// 实例，分流若（回归性地）走到 ShouldFire/Mark 语义，这里会因 cooldown 被拦。
	upHits, upSup := Eval(Context{Event: "UserPromptSubmit", Prompt: "准备发版", SessionID: "s1", Now: time.Now()}, all, noise)
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

// TestMatchKeywords_OutputFailureGate_Negative pins the negative side of the output-source
// failure gate (design A acceptance item): a keyword in stdout must NOT match when the tool
// succeeded (exit_code 0) or when the host omits exit codes and the output carries no
// line-anchored failure signature — the exact noise shapes this gate exists to kill
// (review text mentioning "compile error", audit-script heredoc bodies).
//
// TestMatchKeywords_OutputFailureGate_Negative 钉住输出源失败门的负向面（设计 A 验收项）：
// 工具成功（exit_code 0）或宿主缺退出码且输出无行首失败签名时，stdout 里的关键词不得命中
// ——正是本门要杀的噪声形态（评审文本含 compile error 字样、审计脚本 heredoc 正文）。
func TestMatchKeywords_OutputFailureGate_Negative(t *testing.T) {
	kw := []string{"compile error"}
	mk := func(out map[string]any) (keywordMatch, bool) {
		return matchKeywords(kw, Context{Event: "PostToolUse", ToolName: "Bash",
			ToolInput: map[string]any{"command": "go build ./..."}, ToolOutput: out})
	}
	if _, ok := mk(map[string]any{"stdout": "main.go:10: compile error mentioned in review", "exit_code": 0}); ok {
		t.Fatal("exit_code 0 + keyword in stdout must NOT match (tool succeeded)")
	}
	if _, ok := mk(map[string]any{"stdout": "compile error appears in passing output"}); ok {
		t.Fatal("missing exit_code + no line-anchored failure signature must NOT match")
	}
	if m, ok := mk(map[string]any{"stdout": "compile error here", "exit_code": 1}); !ok || m.Source != MatchSourceStdout {
		t.Fatalf("exit_code 1 + keyword in stdout must match with stdout attribution, got %+v ok=%v", m, ok)
	}
}

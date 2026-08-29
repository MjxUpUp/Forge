package skilltrigger

import (
	"strings"
	"testing"
	"time"
)

// suppressed_test.go — v2 抑制可观测（辩论 P1）与分源匹配语义（辩论 R1）的钉子测试。

// TestMatchKeywords_PerSource_Boundary 钉死 R1 语义变更：关键词不得跨源边界命中。
// v1 拼接 haystack 下，prompt 结尾 "compile" + 命令开头 "error:" 会拼出 "compile error"
// 而误命中——既无法归因来源、又是不可追踪的误报。v2 分源判定必须使该场景为 miss；
// 同关键词完整落在单一来源时必须 hit。这是有意的行为变更（非增量），本测试就是防退化锚。
func TestMatchKeywords_PerSource_Boundary(t *testing.T) {
	kw := []string{"compile error"}
	ctx := Context{
		Event:      "PostToolUse",
		ToolName:   "Bash",
		Prompt:     "帮我看下 compile",
		ToolInput:  map[string]any{"command": "error: exit status 1"},
		ToolOutput: map[string]any{"stdout": ""},
	}
	if _, ok := matchKeywords(kw, ctx); ok {
		t.Fatal("跨源边界拼接出的 'compile error' 不得命中（prompt 尾 'compile' + 命令头 'error:'）")
	}
	// 同关键词完整落在单一来源 → 命中，且归因该来源。
	same := ctx
	same.Prompt = "帮我看下 compile error"
	m, ok := matchKeywords(kw, same)
	if !ok || m.Keyword != "compile error" || m.Source != MatchSourcePrompt {
		t.Fatalf("完整落在 prompt 的关键词应命中且归因 prompt, got %+v ok=%v", m, ok)
	}
	// 完整落在 stdout → 命中且归因 stdout（compile-fix-loop 的真实场景：编译输出命中）。
	out := Context{
		Event:      "PostToolUse",
		ToolName:   "Bash",
		ToolInput:  map[string]any{"command": "go build ./..."},
		ToolOutput: map[string]any{"stdout": "main.go:10:2: compile error"},
	}
	m, ok = matchKeywords(kw, out)
	if !ok || m.Source != MatchSourceStdout {
		t.Fatalf("完整落在 stdout 的关键词应命中且归因 stdout, got %+v ok=%v", m, ok)
	}
}

// TestMatchKeywords_SourcePriority 命中归因优先序 = v1 拼接顺序：prompt > command > stdout。
func TestMatchKeywords_SourcePriority(t *testing.T) {
	kw := []string{"deploy"}
	ctx := Context{
		Event:      "PostToolUse",
		ToolName:   "Bash",
		Prompt:     "please deploy it",
		ToolInput:  map[string]any{"command": "make deploy"},
		ToolOutput: map[string]any{"stdout": "deploy ok"},
	}
	m, ok := matchKeywords(kw, ctx)
	if !ok || m.Source != MatchSourcePrompt {
		t.Fatalf("三源同现时归因 prompt（最高优先）, got %+v ok=%v", m, ok)
	}
}

// TestEval_SuppressedCooldown cooldown 抑制必须以 Suppressed 返回（cause=cooldown），
// 命中计数不再静默吞掉被抑制的近似命中。
func TestEval_SuppressedCooldown(t *testing.T) {
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "Stop"}}}}
	noise := NewInMemoryNoiseController()
	t0 := time.Now()
	hits, sup := Eval(Context{Event: "Stop", SessionID: "s1", Now: t0}, all, noise)
	if len(hits) != 1 || len(sup) != 0 {
		t.Fatalf("首次应命中且无抑制, hits=%d sup=%d", len(hits), len(sup))
	}
	noise.Mark("s1", "foo", t0)
	hits, sup = Eval(Context{Event: "Stop", SessionID: "s1", Now: t0.Add(5 * time.Second)}, all, noise)
	if len(hits) != 0 || len(sup) != 1 || sup[0].Skill != "foo" || sup[0].Cause != SuppressCooldown {
		t.Fatalf("cooldown 内应抑制 1 条 cause=cooldown, hits=%d sup=%+v", len(hits), sup)
	}
}

// TestEval_SuppressedStopCap Stop 触顶后的「本会命中」以 cause=stop-max-rounds 返回。
func TestEval_SuppressedStopCap(t *testing.T) {
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "Stop"}}}}
	noise := NewInMemoryNoiseController()
	now := time.Now()
	for i := 0; i < MaxStopRounds; i++ {
		hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: now}, all, noise)
		if len(hits) != 1 {
			t.Fatalf("第 %d 轮应命中", i+1)
		}
		noise.IncrStopRound("s1") // 模拟 CLI 层落盘
	}
	hits, sup := Eval(Context{Event: "Stop", SessionID: "s1", Now: now}, all, noise)
	if len(hits) != 0 || len(sup) != 1 || sup[0].Cause != SuppressStopCap {
		t.Fatalf("触顶后应抑制 cause=stop-max-rounds, hits=%d sup=%+v", len(hits), sup)
	}
}

// TestEval_EvidenceFields 命中证据字段齐备：keyword/source/index/sig/hash；hash 项目盐
// （同 prompt 异项目 → 异 hash；同项目 → 稳定）；sig 对声明内容稳定。
func TestEval_EvidenceFields(t *testing.T) {
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{
		{Event: "UserPromptSubmit", Keywords: []string{"编译报错", "compile error"}},
	}}}
	hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "编译报错了", ProjectRoot: "/proj/a", SessionID: "s"}, all, nil)
	if len(hits) != 1 {
		t.Fatalf("应命中 1 条, got %d", len(hits))
	}
	h := hits[0]
	if h.MatchedKeyword != "编译报错" || h.MatchSource != MatchSourcePrompt {
		t.Fatalf("证据字段错误: keyword=%q source=%q", h.MatchedKeyword, h.MatchSource)
	}
	if h.TriggerIndex != 0 || h.TriggerSig == "" {
		t.Fatalf("规则身份缺失: index=%d sig=%q", h.TriggerIndex, h.TriggerSig)
	}
	if h.PromptHash == "" || h.PromptLen != len([]rune("编译报错了")) {
		t.Fatalf("prompt 证据缺失: hash=%q len=%d", h.PromptHash, h.PromptLen)
	}
	// 项目盐：同 prompt 异项目 → 异 hash；同项目重放 → 稳定。
	hits2, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "编译报错了", ProjectRoot: "/proj/b", SessionID: "s"}, all, nil)
	if hits2[0].PromptHash == h.PromptHash {
		t.Fatal("项目盐失效：异项目同 prompt 得到同 hash（跨项目可关联）")
	}
	hits3, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "编译报错了", ProjectRoot: "/proj/a", SessionID: "other"}, all, nil)
	if hits3[0].PromptHash != h.PromptHash {
		t.Fatal("同项目同 prompt hash 不稳定（破坏跨 session 聚类）")
	}
	// tool 事件（无 prompt）：hash 回退到命中来源文本（review m4——stdout 命中同样
	// 可挖矿去重），PromptLen 仍为 0（记录的是 prompt 长度）。
	toolAll := []SkillTriggers{{Skill: "bar", Triggers: []Trigger{{Event: "PostToolUse", Match: "Bash", Keywords: []string{"fail"}}}}}
	th, _ := Eval(Context{Event: "PostToolUse", ToolName: "Bash", ProjectRoot: "/proj/a", ToolOutput: map[string]any{"stdout": "build fail"}, SessionID: "s"}, toolAll, nil)
	if len(th) != 1 || th[0].PromptHash == "" || th[0].PromptLen != 0 {
		t.Fatalf("tool 事件应有来源回退 hash 且 PromptLen=0: %+v", th)
	}
	if th[0].MatchSource != MatchSourceStdout {
		t.Fatalf("tool 事件应归因 stdout: %q", th[0].MatchSource)
	}
}

// TestEval_TriggerSigMatchesDeclared 钉死 review M1：Eval 落盘的 sig 必须等于对「声明态」
// 规则重算的 sig——含多 trigger 取 maxCD 场景（覆写 Cooldown 后计算会让缺省 cooldown 的
// 规则带上归一化 60，同一声明规则劈裂出多个 sig，纵向统计 join 不回 SKILL.md）。
func TestEval_TriggerSigMatchesDeclared(t *testing.T) {
	declared := Trigger{Event: "UserPromptSubmit", Keywords: []string{"编译报错"}} // Cooldown 缺省（0）
	// 后一条 trigger 声明更大 cooldown：两 trigger 同 event 同 keywords 都命中 → maxCD=120。
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{
		declared,
		{Event: "UserPromptSubmit", Keywords: []string{"编译报错"}, Cooldown: 120},
	}}}
	hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "编译报错", ProjectRoot: "/p", SessionID: "s"}, all, nil)
	if len(hits) != 1 {
		t.Fatalf("应命中 1 条, got %d", len(hits))
	}
	h := hits[0]
	if h.Trigger.Cooldown != 120 {
		t.Fatalf("Hit.Trigger.Cooldown 应为应用值 120, got %d", h.Trigger.Cooldown)
	}
	wantSig := triggerSig(declared)
	if h.TriggerSig != wantSig {
		t.Fatalf("sig 必须对声明态计算（maxCD 覆写会劈裂规则身份）: got %q, want %q", h.TriggerSig, wantSig)
	}
}

// TestEval_NoRootNoHash 钉死 review m2：非 forge 目录（ProjectRoot 空）不落 hash——空盐
// 会让全部非 forge session 坍缩进同一全局桶，跨项目关联恰在盐缺位处成立。
func TestEval_NoRootNoHash(t *testing.T) {
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "UserPromptSubmit", Keywords: []string{"x"}}}}}
	hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "x here", SessionID: "s"}, all, nil)
	if len(hits) != 1 || hits[0].PromptHash != "" {
		t.Fatalf("无 ProjectRoot 不得落 hash（宁缺哈希不做全局桶）: %+v", hits)
	}
}

// TestEval_StopCapSkipsDisabled 钉死 review m1：显式禁用的 skill 在 stop-cap 触顶时
// 不得被记成"被抑制的潜在注入"。
func TestEval_StopCapSkipsDisabled(t *testing.T) {
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "Stop"}}}}
	noise := NewInMemoryNoiseController()
	now := time.Now()
	for i := 0; i < MaxStopRounds; i++ {
		noise.IncrStopRound("s1")
	}
	t.Setenv("FORGE_SKILL_TRIGGER_DISABLE", "foo")
	hits, sup := Eval(Context{Event: "Stop", SessionID: "s1", Now: now}, all, noise)
	if len(hits) != 0 || len(sup) != 0 {
		t.Fatalf("禁用 skill 即便 stop-cap 触顶也不算抑制: hits=%d sup=%+v", len(hits), sup)
	}
}

// TestTriggerSig_StableAndDistinct sig 对声明内容稳定（两次计算一致）、对不同规则可区分。
func TestTriggerSig_StableAndDistinct(t *testing.T) {
	a := Trigger{Event: "Stop", Keywords: []string{"x"}}
	if triggerSig(a) != triggerSig(a) {
		t.Fatal("同规则 sig 不稳定")
	}
	b := Trigger{Event: "Stop", Keywords: []string{"y"}}
	if triggerSig(a) == triggerSig(b) {
		t.Fatal("异规则 sig 不可区分")
	}
}

// TestExcerpt_Window 摘录窗口：±radius rune、命中词包含在内、找不到/空关键词返回空。
func TestExcerpt_Window(t *testing.T) {
	prompt := strings.Repeat("前", 100) + "编译报错" + strings.Repeat("后", 100)
	ctx := Context{Prompt: prompt}
	ex := Excerpt(ctx, MatchSourcePrompt, "编译报错", 8)
	if !strings.Contains(ex, "编译报错") {
		t.Fatalf("摘录应含命中词: %q", ex)
	}
	if got := len([]rune(ex)); got > 8+4+8 {
		t.Fatalf("摘录超长（应 ≤ 8+关键词+8 = 20 rune）: %d", got)
	}
	if !strings.HasPrefix(ex, strings.Repeat("前", 8)) {
		t.Fatalf("窗口应从命中点前 8 rune 开始: %q", ex)
	}
	if Excerpt(ctx, MatchSourcePrompt, "不存在词", 8) != "" {
		t.Fatal("找不到关键词应返回空")
	}
	if Excerpt(ctx, MatchSourcePrompt, "", 8) != "" {
		t.Fatal("空关键词应返回空")
	}
	if Excerpt(ctx, MatchSourceStdout, "编译报错", 8) != "" {
		t.Fatal("来源无文本应返回空")
	}
}

package skilltrigger

import (
	"strings"
	"testing"
)

func TestRender_Empty(t *testing.T) {
	if Render(nil, Context{}, nil) != "" {
		t.Fatal("空 hits 应返空")
	}
}

func TestRender_SingleHit(t *testing.T) {
	out := Render([]Hit{{
		Skill: "test-discipline", SkillDir: "/x/test-discipline",
		Reason: "测试命令失败——加载守卫", Trigger: Trigger{When: "test_command_failed"},
	}}, Context{Event: "PostToolUse"}, nil)
	for _, want := range []string{"test-discipline", "测试命令失败", "PostToolUse", "test_command_failed", "/x/test-discipline/SKILL.md", "FORGE_SKILL_TRIGGER=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应含 %q，got:\n%s", want, out)
		}
	}
}

func TestRender_MultipleHits(t *testing.T) {
	out := Render([]Hit{
		{Skill: "foo", SkillDir: "/x/foo", Reason: "r1", Trigger: Trigger{When: "c1"}},
		{Skill: "bar", SkillDir: "/x/bar", Reason: "r2", Trigger: Trigger{Keywords: []string{"k"}}},
	}, Context{Event: "Stop"}, nil)
	if !strings.Contains(out, "（2）：") {
		t.Errorf("应显示命中数 2，got:\n%s", out)
	}
	for _, want := range []string{"foo", "bar", "c1", "keywords"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应含 %q，got:\n%s", want, out)
		}
	}
}

func TestRender_StripsControlAndFlattens(t *testing.T) {
	out := Render([]Hit{{
		Skill: "foo", SkillDir: "/x/foo",
		Reason: "a\nb\x1b[31mred",
	}}, Context{}, nil)
	if strings.Contains(out, "\x1b") {
		t.Error("应剥 ANSI 转义")
	}
	if strings.Contains(out, "a\nb") {
		t.Error("reason 含换行应被压成单行")
	}
}

func TestRender_TruncatesLongReason(t *testing.T) {
	long := strings.Repeat("X", 300)
	out := Render([]Hit{{Skill: "foo", SkillDir: "/x/foo", Reason: long}}, Context{}, nil)
	if !strings.Contains(out, "...") {
		t.Error("长 reason 应被截断")
	}
}

// TestRender_FactualPhrasingNotImperative pins the #5 (2026-08-22) phrasing
// contract: banner/footer state facts (matched conditions, readable paths, opt-out)
// instead of issuing instructions (请加载/必须处理). Imperatives read as injected
// commands, can trip prompt-injection defenses, and the adherence audit showed they
// did not lift conversion anyway (kimi 0%, claude 22%).
//
// TestRender_FactualPhrasingNotImperative 钉住 #5（2026-08-22）措辞契约：
// banner/footer 陈述事实（命中条件、可读路径、关闭方式）而非下达指令
// （请加载/必须处理）。祈使句会被读成注入指令、可能触发 prompt-injection
// 防御，且遵循度审计显示祈使并未抬高转化率（kimi 0%、claude 22%）。
func TestRender_FactualPhrasingNotImperative(t *testing.T) {
	out := Render([]Hit{{Skill: "foo", SkillDir: "/x/foo", Reason: "r"}}, Context{Event: "PostToolUse"}, nil)
	for _, want := range []string{"供参考", "绝对路径", "完整方法论", "FORGE_SKILL_TRIGGER=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("factual banner/footer must contain %q, got:\n%s", want, out)
		}
	}
	for _, banned := range []string{"请按需加载", "请加载", "必须处理"} {
		if strings.Contains(out, banned) {
			t.Errorf("imperative phrasing %q must not appear (facts, not instructions):\n%s", banned, out)
		}
	}
}

// TestRender_MatchEvidence 钉住 2026-08 噪音审计的文案修复：注入文案必须带命中关键词与
// 来源（模板化文案只说「触发条件命中」是最大噪音抱怨——agent 不知道为何切题）。
// condition-only 命中（无关键词）不应出现证据后缀。
func TestRender_MatchEvidence(t *testing.T) {
	out := Render([]Hit{
		{Skill: "commit-guard", SkillDir: "/x/commit-guard", Reason: "r",
			MatchedKeyword: "git commit", MatchSource: MatchSourcePrompt,
			Trigger: Trigger{Keywords: []string{"git commit"}}},
		{Skill: "cond-only", SkillDir: "/x/cond-only", Reason: "r2",
			Trigger: Trigger{When: "source_changed_uncommitted"}},
	}, Context{Event: "UserPromptSubmit"}, nil)
	if !strings.Contains(out, "命中关键词「git commit」（来自你的输入）") {
		t.Errorf("应带命中词+来源证据，got:\n%s", out)
	}
	if strings.Contains(out, "cond-only】  事件 UserPromptSubmit · source_changed_uncommitted；命中关键词") {
		t.Errorf("condition-only 命中不应有证据后缀，got:\n%s", out)
	}
}

// TestRender_ReminderCompact 钉住 session 内第 2 次注入的短提醒形态：一行提醒 + 路径，
// 不重复 reason 全文（agent 上下文已有；wire 证据：重复注入从不被重读）。
func TestRender_ReminderCompact(t *testing.T) {
	out := Render([]Hit{{
		Skill: "test-discipline", SkillDir: "/x/test-discipline", Reminder: true,
		Reason:         "这是一条很长的完整指引不应该在短提醒里重复出现",
		MatchedKeyword: "go test", MatchSource: MatchSourceCommand,
		Trigger: Trigger{Keywords: []string{"go test"}},
	}}, Context{Event: "PreToolUse"}, nil)
	if !strings.Contains(out, "短提醒") {
		t.Errorf("Reminder 应标短提醒，got:\n%s", out)
	}
	if !strings.Contains(out, "命中关键词「go test」（来自命令）") {
		t.Errorf("短提醒仍应带命中证据，got:\n%s", out)
	}
	if strings.Contains(out, "这是一条很长的完整指引") {
		t.Errorf("短提醒不应重复 reason 全文，got:\n%s", out)
	}
	if !strings.Contains(out, "/x/test-discipline/SKILL.md") {
		t.Errorf("短提醒仍应给路径，got:\n%s", out)
	}
}

// TestRender_OverflowNote 钉住单次上限落选的一句带过：overflow 技能名出现在尾部说明里。
func TestRender_OverflowNote(t *testing.T) {
	out := Render([]Hit{{Skill: "foo", SkillDir: "/x/foo", Reason: "r"}}, Context{Event: "Stop"}, []string{"bar", "baz"})
	if !strings.Contains(out, "另有 2 个 skill 命中未注入") || !strings.Contains(out, "bar, baz") {
		t.Errorf("应带 overflow 尾部说明，got:\n%s", out)
	}
}

// TestRender_NoASCIIDoubleQuotes 钉住「render 输出不含 ASCII 双引号」契约的廉价断言：
// 全量/短提醒/overflow 三种形态统一检查；关键词里故意带 "（skill 作者声明不可信），
// 必须被 sanitizeEvidence 剥掉而非原样泄漏进文案。
func TestRender_NoASCIIDoubleQuotes(t *testing.T) {
	outs := map[string]string{
		"full": Render([]Hit{{
			Skill: "foo", SkillDir: "/x/foo", Reason: "r",
			MatchedKeyword: `say "hi"`, MatchSource: MatchSourcePrompt,
			Trigger: Trigger{Keywords: []string{`say "hi"`}},
		}}, Context{Event: "UserPromptSubmit"}, []string{"o1", "o2"}),
		"reminder": Render([]Hit{{
			Skill: "bar", SkillDir: "/x/bar", Reason: "r", Reminder: true,
			MatchedKeyword: `kw"oted`, MatchSource: MatchSourceCommand,
			Trigger: Trigger{Keywords: []string{`kw"oted`}},
		}}, Context{Event: "Stop"}, nil),
	}
	for name, out := range outs {
		if strings.Contains(out, `"`) {
			t.Errorf("%s 形态输出不应含 ASCII 双引号:\n%s", name, out)
		}
	}
	if !strings.Contains(outs["full"], "命中关键词「say hi」") {
		t.Errorf("关键词中的双引号应被剥掉后渲染，got:\n%s", outs["full"])
	}
}

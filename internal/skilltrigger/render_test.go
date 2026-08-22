package skilltrigger

import (
	"strings"
	"testing"
)

func TestRender_Empty(t *testing.T) {
	if Render(nil, Context{}) != "" {
		t.Fatal("空 hits 应返空")
	}
}

func TestRender_SingleHit(t *testing.T) {
	out := Render([]Hit{{
		Skill: "test-discipline", SkillDir: "/x/test-discipline",
		Reason: "测试命令失败——加载守卫", Trigger: Trigger{When: "test_command_failed"},
	}}, Context{Event: "PostToolUse"})
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
	}, Context{Event: "Stop"})
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
	}}, Context{})
	if strings.Contains(out, "\x1b") {
		t.Error("应剥 ANSI 转义")
	}
	if strings.Contains(out, "a\nb") {
		t.Error("reason 含换行应被压成单行")
	}
}

func TestRender_TruncatesLongReason(t *testing.T) {
	long := strings.Repeat("X", 300)
	out := Render([]Hit{{Skill: "foo", SkillDir: "/x/foo", Reason: long}}, Context{})
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
	out := Render([]Hit{{Skill: "foo", SkillDir: "/x/foo", Reason: "r"}}, Context{Event: "PostToolUse"})
	for _, want := range []string{"供参考", "可 read", "FORGE_SKILL_TRIGGER=0"} {
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

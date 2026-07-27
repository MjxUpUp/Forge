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

package skilltrigger

import (
	"strings"
	"testing"
)

// TestRender_ForgeIntegrationPointer.
//
// TestRender_ForgeIntegrationPointer — 推荐块对有 forge 集成笔记的 skill 追加
// 指针行（code-review-gate 是真实内嵌笔记名），且指针行不破坏「输出不含
// ASCII 双引号」契约；无笔记 skill（foo）不追加。
// TestRender_ForgeIntegrationPointer — recommendation blocks append the pointer
// line for skills that have a forge integration note (code-review-gate is a real
// embedded note name), the line preserves the no-ASCII-double-quotes output
// contract, and note-less skills (foo) get no pointer.
func TestRender_ForgeIntegrationPointer(t *testing.T) {
	out := Render([]Hit{
		{Skill: "code-review-gate", SkillDir: "/x/code-review-gate", Reason: "r", Trigger: Trigger{When: "c"}},
		{Skill: "foo", SkillDir: "/x/foo", Reason: "r", Trigger: Trigger{When: "c"}},
	}, Context{Event: "Stop"}, nil)
	if !strings.Contains(out, "forge skills integration code-review-gate") {
		t.Errorf("有笔记的 skill 应输出指针行, got:\n%s", out)
	}
	if strings.Contains(out, "forge skills integration foo") {
		t.Errorf("无笔记的 skill 不应输出指针行, got:\n%s", out)
	}
	if strings.Contains(out, `"`) {
		t.Errorf("输出不得含 ASCII 双引号（render 契约）, got:\n%s", out)
	}
}

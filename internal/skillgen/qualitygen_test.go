package skillgen

import (
	"strings"
	"testing"

	"github.com/Harness/forge/internal/pipeline"
	"github.com/Harness/forge/internal/protocol"
)

func TestQualitySkillContainsTaskRule(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "每次修改后确认编译通过", Severity: "error", Enabled: true},
		},
		SessionRules: []protocol.SessionRule{
			{Instruction: "修改前先说意图", Mandatory: true, Trigger: "on_edit"},
		},
	}
	p := &pipeline.Pipeline{
		Project: "test-project",
		Mode:    "small",
	}

	content := buildQualitySkillContent(proto, p)

	if !strings.Contains(content, "Task Bridge Protocol") {
		t.Error("quality SKILL.md missing Task Bridge Protocol section")
	}
	if !strings.Contains(content, "编码前必做") {
		t.Error("quality SKILL.md missing mandatory pre-coding task start instruction")
	}
	if !strings.Contains(content, "强制顺序") {
		t.Error("quality SKILL.md missing forced gate sequence instruction")
	}
	if !strings.Contains(content, "--branch") {
		t.Error("quality SKILL.md missing --branch flag reference")
	}
	if !strings.Contains(content, "forge task start") {
		t.Error("quality SKILL.md missing 'forge task start' command reference")
	}
	if !strings.Contains(content, "forge task list") {
		t.Error("quality SKILL.md missing 'forge task list' command reference")
	}
	if !strings.Contains(content, "--ref <ref>") {
		t.Error("quality SKILL.md missing --ref in gate commands")
	}
}

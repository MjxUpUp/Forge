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

	if !strings.Contains(content, "非平凡变更必须启动任务") {
		t.Error("quality SKILL.md missing mandatory task start section header")
	}
	if !strings.Contains(content, "此规则在所有分支上都适用") {
		t.Error("quality SKILL.md missing 'applies to all branches' statement")
	}
	if !strings.Contains(content, "forge task start") {
		t.Error("quality SKILL.md missing 'forge task start' command reference")
	}
}

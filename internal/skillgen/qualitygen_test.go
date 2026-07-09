package skillgen

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/protocol"
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

	content := buildQualitySkillContent(t.TempDir(), proto)

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

// TestQualitySkillDocumentsCommitTiming guards the same post-complete commit
// trap as TestClaudeMDDocumentsCommitTiming, but for the forge-quality skill:
// complete clears the active task ref, so the skill must tell agents to commit
// before complete.
func TestQualitySkillDocumentsCommitTiming(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "提交时机") {
		t.Error("quality SKILL.md missing commit-timing guidance (commit must precede complete)")
	}
}

// TestQualitySkillDocumentsAuxChecks guards that the remaining auxiliary hooks
// (assertion-check/auto-compile advisory), the sunk Red Flags rules, scoring thresholds,
// and the read-before-edit error are documented. read-check/scope-guard/
// clone-check were sunk from runtime hooks to the Red Flags section (layered
// noise treatment); agents still need the skill to explain session-health
// WARNs, the scoring A-F cutoffs (90/80/70/60), and the "passed without
// reading any code" fix.
func TestQualitySkillDocumentsAuxChecks(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "辅助质量检查") {
		t.Error("quality SKILL.md missing auxiliary-checks section")
	}
	if !strings.Contains(content, "Red Flags") {
		t.Error("quality SKILL.md missing Red Flags section (read-check/scope-guard/clone-check sunk here)")
	}
	if !strings.Contains(content, "先读再改") {
		t.Error("quality SKILL.md Red Flags missing the read-before-edit rule")
	}
	if !strings.Contains(content, "阈值") {
		t.Error("quality SKILL.md scoring table missing A-F thresholds")
	}
	if !strings.Contains(content, "passed without reading any code") {
		t.Error("quality SKILL.md error table missing 'passed without reading any code' row")
	}
	// Sunk hooks must NOT appear as runtime hook docs anymore.
	for _, gone := range []string{"read-check**（PreToolUse", "scope-guard**（PreToolUse", "clone-check**（PostToolUse"} {
		if strings.Contains(content, gone) {
			t.Errorf("quality SKILL.md still documents sunk hook %q as runtime — should be Red Flags text only", gone)
		}
	}
}

// TestQualitySkillDescriptionIsTriggerOriented guards the frontmatter description
// against regressing to a vague "what it is" phrasing (the old "每次开发会话自动
// 执行的质量标准" gave the model no signal for when to invoke the skill on
// demand). Per the Anthropic skill standard the description must name concrete
// trigger scenarios — advancing gates, recovering from guard warnings, aborting
// a stuck task. A skill no one knows when to load is the "没有什么用" failure.
func TestQualitySkillDescriptionIsTriggerOriented(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	// The vague non-trigger phrase must be gone.
	if strings.Contains(content, "每次开发会话自动执行的质量标准") {
		t.Error("quality SKILL.md description still uses vague non-trigger phrasing")
	}
	// The description must name concrete invocation scenarios.
	desc := content
	if !strings.Contains(desc, "门禁") {
		t.Error("description must mention gate advancement as a trigger")
	}
	if !strings.Contains(desc, "警告") {
		t.Error("description must mention guard-warning recovery as a trigger")
	}
}

// TestQualitySkillDocumentsAbort guards that the task command list includes
// `forge task abort` — the escape hatch for stuck/ghost tasks. Without it in the
// skill, an agent that needs to clean up a non-progressing task has no guidance.
func TestQualitySkillDocumentsAbort(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "forge task abort") {
		t.Error("quality SKILL.md task command list missing 'forge task abort'")
	}
}

// TestQualitySkillTaskVerifyIsAdvisory guards the advisory rewrite: task-verify
// must NOT be documented as blocking session end (the old "连续 3 次失败放行"
// semantics are gone — that counter was removed), and the mandatory-review
// force must be attributed to `forge task complete`, not the Stop hook. This is
// the doc-side guard for the enforcement transfer.
func TestQualitySkillTaskVerifyIsAdvisory(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	// Obsolete blocking / 3-fail phrasing must be gone.
	for _, gone := range []string{"连续 3 次失败", "阻塞会话结束", "task-verify 阻塞"} {
		if strings.Contains(content, gone) {
			t.Errorf("quality SKILL.md still uses obsolete blocking phrasing %q (task-verify is advisory)", gone)
		}
	}
	// Advisory wording must be present.
	if !strings.Contains(content, "advisory") {
		t.Error("quality SKILL.md must document task-verify as advisory")
	}
}

package protocol

import (
	"strings"
	"testing"
)

func renderTestStandards() []Standard {
	return []Standard{
		{Name: "编译", Description: "编译通过", EnforceHook: "auto-compile.sh", Severity: "warning", Enabled: true},
		{Name: "断言", Description: "不弱化断言", Severity: "error", Enabled: true},
		{Name: "禁用项", Description: "不应出现", Severity: "info", Enabled: false},
	}
}

// TestRenderStandardsPinsLines pins the exact rendered bytes for both label styles,
// the hook-note formatting, and the disabled-standard skip — the five call sites
// (quality skill + cursor/cline/windsurf/copilot) differ only in these parameters.
func TestRenderStandardsPinsLines(t *testing.T) {
	var sb strings.Builder
	RenderStandards(&sb, renderTestStandards(), StandardRenderStyle{
		SeverityLabel:  EmojiSeverityLabel,
		HookInfoFormat: "（自动检查: %s）",
		LineFormat:     "- %s **%s**: %s %s\n",
	})
	want := "- 🟡 **编译**: 编译通过 （自动检查: auto-compile.sh）\n" +
		"- 🔴 **断言**: 不弱化断言 \n"
	if sb.String() != want {
		t.Errorf("emoji style:\n got %q\nwant %q", sb.String(), want)
	}

	sb.Reset()
	RenderStandards(&sb, renderTestStandards(), StandardRenderStyle{
		SeverityLabel:  WordSeverityLabel,
		HookInfoFormat: " (enforced: %s)",
		LineFormat:     "- [%s] **%s**: %s%s\n",
	})
	want = "- [WARNING] **编译**: 编译通过 (enforced: auto-compile.sh)\n" +
		"- [ERROR] **断言**: 不弱化断言\n"
	if sb.String() != want {
		t.Errorf("word style:\n got %q\nwant %q", sb.String(), want)
	}
}

// TestRenderSessionRulesPinsLines pins the exact rendered bytes, the trigger-suffix
// mapping, and the explicit-index line format used by hosts that render no suffix
// (unreferenced operands must be dropped, not rendered as EXTRA).
func TestRenderSessionRulesPinsLines(t *testing.T) {
	rules := []SessionRule{
		{Instruction: "先说明意图", Trigger: "always", Mandatory: true},
		{Instruction: "大改先出设计", Trigger: "on_edit", Mandatory: false},
	}

	var sb strings.Builder
	RenderSessionRules(&sb, rules, SessionRuleRenderStyle{
		MandatoryLabel: CNMandatoryLabel,
		TriggerSuffix:  CNTriggerSuffix,
		LineFormat:     "- [%s] %s %s\n",
	})
	want := "- [必须] 先说明意图 \n" +
		"- [建议] 大改先出设计 （修改代码时）\n"
	if sb.String() != want {
		t.Errorf("cn style:\n got %q\nwant %q", sb.String(), want)
	}

	sb.Reset()
	RenderSessionRules(&sb, rules, SessionRuleRenderStyle{
		MandatoryLabel: MustShouldLabel,
		LineFormat:     "- %[1]s %[2]s\n",
	})
	want = "- [MUST] 先说明意图\n" +
		"- [SHOULD] 大改先出设计\n"
	if sb.String() != want {
		t.Errorf("indexed style:\n got %q\nwant %q", sb.String(), want)
	}

	sb.Reset()
	RenderSessionRules(&sb, rules, SessionRuleRenderStyle{
		MandatoryLabel: AlwaysPreferLabel,
		LineFormat:     "- %[1]s: %[2]s\n",
	})
	want = "- ALWAYS: 先说明意图\n" +
		"- PREFER: 大改先出设计\n"
	if sb.String() != want {
		t.Errorf("colon style:\n got %q\nwant %q", sb.String(), want)
	}
}

package agentbridge

import (
	"os"
	"strings"
	"testing"
)

// TestProtocolRenderConvergence pins the 5-copies → protocol.Render* refactor for the
// hosts that still render protocol sections: cursor (buildCursorMDC, retained for the
// skillgen layer) and windsurf (user-level global_rules.md). The byte-equality arms
// (cursor == cline, windsurf == copilot) were removed when the user-level-assets
// refactor turned cline/copilot into no-op translators (no guidance file to compare
// against) — what remains guarded here is that the shared protocol.Render* helpers
// are really wired into both retained renderers (rendered standards present, not
// bypassed), each in its own host's severity style.
//
// TestProtocolRenderConvergence 为仍渲染 protocol 段的 host（cursor 的
// buildCursorMDC——为 skillgen 层保留——与 windsurf 的用户级 global_rules.md）钉住
// 5 份渲染 → protocol.Render* 重构。逐字节相等臂（cursor==cline、windsurf==copilot）
// 在 user-level-assets 重构把 cline/copilot 变为 no-op translator 后移除（无 guidance
// 文件可比）——此处守卫的是共享 protocol.Render* helper 在两个保留的渲染器里真实
// 接线（有渲染产物，非旁路），各自使用本 host 的 severity 风格。
func TestProtocolRenderConvergence(t *testing.T) {
	isolateHome(t) // windsurf's Translate also writes the user-level hooks.json

	section := func(content, start, end string) string {
		i := strings.Index(content, start)
		if i < 0 {
			return ""
		}
		j := strings.Index(content[i+len(start):], end)
		if j < 0 {
			return content[i:]
		}
		return content[i : i+len(start)+j]
	}

	// Cursor no longer writes its .mdc from Translate (project-level instruction
	// text moved to the skillgen layer) — render it directly through the retained
	// buildCursorMDC renderer. Windsurf still renders its guidance via Translate,
	// now into the user-level global_rules.md.
	//
	// Cursor 的 .mdc 不再由 Translate 写出（项目级指令文本移到 skillgen 层）——
	// 直接经保留的 buildCursorMDC 渲染器产出。Windsurf 的 guidance 仍由 Translate
	// 渲染，现在写入用户级 global_rules.md。
	cursor := buildCursorMDC(testInput())
	dir := t.TempDir()
	if err := (&WindsurfTranslator{}).Translate(dir, testInput()); err != nil {
		t.Fatalf("windsurf Translate: %v", err)
	}
	rulesPath, err := WindsurfGlobalRulesPath()
	if err != nil {
		t.Fatalf("WindsurfGlobalRulesPath: %v", err)
	}
	windsurfData, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("read global_rules.md: %v", err)
	}
	windsurf := string(windsurfData)

	for name, content := range map[string]string{"cursor": cursor, "windsurf": windsurf} {
		// The hosts use different heading layouts (cursor has ## 质量标准 / ## 会话行为规则
		// subsections, windsurf a single # Forge Quality Standards header), so assert the
		// rendered products on the full content rather than on extracted sections.
		//
		// 各 host 标题布局不同（cursor 有 ## 质量标准 / ## 会话行为规则 小节，
		// windsurf 只有一个 # Forge Quality Standards 头），故渲染产物对全文断言，
		// 不做小节抽取。
		if !strings.Contains(content, "**") || !strings.Contains(content, "(enforced: ") {
			t.Errorf("%s 质量标准渲染产物缺失（加粗标准名 + enforced 标注）——共享 helper 可能未接线", name)
		}
		rules := section(content, "## 会话行为规则", "## ")
		if name == "windsurf" {
			rules = content // windsurf has no session-rules subheading; see above
		}
		if !strings.Contains(rules, "- ") {
			t.Errorf("%s 会话行为规则渲染产物缺失（规则列表）——共享 helper 可能未接线", name)
		}
	}
}

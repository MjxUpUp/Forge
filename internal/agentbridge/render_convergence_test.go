package agentbridge

import (
	"os"
	"strings"
	"testing"
)

// TestProtocolRenderConvergence 为共享 protocol.Render* helper 在保留的渲染器里
// 真实接线（有渲染产物，非旁路）钉住，使用本 host 的 severity 风格。cursor 臂随
// buildCursorMDC 一并移除（死代码，2026-08-29：Translate 早已不写 .mdc、skillgen
// 层渲染自己的文本——该渲染器零生产调用方）；windsurf 的用户级 global_rules.md
// 是仅存的 Translate 期 protocol 段渲染器。
func TestProtocolRenderConvergence(t *testing.T) {
	isolateHome(t) // windsurf's Translate also writes the user-level hooks.json

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
	content := string(windsurfData)

	// 渲染产物在场（加粗标准名 + enforced 标注）——共享 helper 真实接线、非旁路。
	if !strings.Contains(content, "**") || !strings.Contains(content, "(enforced: ") {
		t.Errorf("windsurf 质量标准渲染产物缺失（加粗标准名 + enforced 标注）——共享 helper 可能未接线")
	}
	if !strings.Contains(content, "- ") {
		t.Errorf("windsurf 会话行为规则渲染产物缺失（规则列表）——共享 helper 可能未接线")
	}
}

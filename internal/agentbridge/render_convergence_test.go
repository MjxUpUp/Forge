package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProtocolRenderConvergence pins the 5-copies → protocol.Render* refactor:
// the protocol standards/session-rules sections of hosts that shared
// byte-identical render code must stay identical when rendered through the
// shared helper (cursor == cline, windsurf == copilot), and the sections must
// actually contain rendered standards (helper really wired in, not bypassed).
//
// TestProtocolRenderConvergence 钉住 5 份渲染 → protocol.Render* 重构：
// 原本渲染代码逐行相同的 host（cursor==cline、windsurf==copilot）经共享
// helper 渲染后 protocol 段必须保持一致，且段落内确有渲染产物（共享 helper
// 真实接线，非旁路）。
func TestProtocolRenderConvergence(t *testing.T) {
	gen := func(t *testing.T, tr Translator, rel string) string {
		t.Helper()
		dir := t.TempDir()
		if err := tr.Translate(dir, testInput()); err != nil {
			t.Fatalf("Translate: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(data)
	}
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

	cursor := gen(t, &CursorTranslator{}, filepath.Join(".cursor", "rules", "forge-quality.mdc"))
	cline := gen(t, &ClineTranslator{}, filepath.Join(".clinerules", "forge-quality.md"))
	windsurf := gen(t, &WindsurfTranslator{}, ".windsurfrules")
	copilot := gen(t, &CopilotTranslator{}, filepath.Join(".github", "instructions", "forge-quality.instructions.md"))

	for _, sec := range []struct{ start, end string }{
		{"## 质量标准", "## 会话行为规则"},
		{"## 会话行为规则", "## "},
	} {
		if got, want := section(cursor, sec.start, sec.end), section(cline, sec.start, sec.end); got != want {
			t.Errorf("cursor 与 cline 的 %s 段经共享 helper 渲染后不再一致", sec.start)
		}
		if got, want := section(windsurf, sec.start, sec.end), section(copilot, sec.start, sec.end); got != want {
			t.Errorf("windsurf 与 copilot 的 %s 段经共享 helper 渲染后不再一致", sec.start)
		}
	}

	std := section(cursor, "## 质量标准", "## 会话行为规则")
	if !strings.Contains(std, "**") || !strings.Contains(std, "(enforced: ") {
		t.Error("cursor 质量标准段缺渲染产物（加粗标准名 + enforced 标注）——共享 helper 可能未接线")
	}
}

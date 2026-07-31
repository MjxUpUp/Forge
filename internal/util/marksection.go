package util

import "strings"

// ReplaceMarkedSection replaces the content between startMarker and endMarker with
// newSection; all content outside the markers is preserved as-is. When the markers
// are missing or inverted, newSection is appended instead. Trailing newlines of
// newSection are trimmed first so the spacing between the section and the preserved
// content is deterministic.
//
// Shared by skillgen's CLAUDE.md/AGENTS.md forge-section upsert (claudemd.go) and
// agentbridge's .windsurfrules forge-rules upsert (windsurf.go) — both wrap the same
// HTML-comment marker pair. kimi.go's upsertKimiSection is a different contract
// (TOML comment markers + corruption detection) and does NOT use this helper.
//
// ReplaceMarkedSection 用 newSection 替换 startMarker 与 endMarker 之间的内容，标记外
// 的内容原样保留；标记缺失或颠倒时改为追加 newSection。newSection 的尾部换行先被
// 裁剪，使 section 与保留内容之间的间距确定。
//
// 由 skillgen 的 CLAUDE.md/AGENTS.md forge 段 upsert（claudemd.go）与 agentbridge 的
// .windsurfrules forge-rules upsert（windsurf.go）共享——两者包裹同一对 HTML 注释
// 标记。kimi.go 的 upsertKimiSection 是另一种契约（TOML 注释标记 + 损坏检测），
// 不使用本 helper。
func ReplaceMarkedSection(content, newSection, startMarker, endMarker string) string {
	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		// Markers not found — append the section.
		//
		// 未找到标记——追加该 section
		return content + "\n" + newSection
	}

	before := content[:startIdx]
	after := content[endIdx+len(endMarker):]

	// The trailing newline of newSection comes from endMarker+newline; TrimRight it
	// first, to precisely control the spacing between markers and surrounding content.
	//
	// newSection 末尾的换行来自 endMarker+换行，先 TrimRight 掉它，
	// 以精确控制标记之间以及与后续内容之间的间距
	section := strings.TrimRight(newSection, "\n")

	result := before + section + "\n"

	// Strip leading blank lines from the after-content.
	//
	// 清除 after-content 的前导空白
	after = strings.TrimLeft(after, "\n")
	if after != "" {
		result += "\n" + after
	}
	return result
}

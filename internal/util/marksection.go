package util

import "strings"

// ForgeSectionStart/End 是包在用户指令文件（CLAUDE.md / AGENTS.md /
// .windsurfrules）里 forge 生成段外的那对 HTML 注释标记。「forge 生成了什么」
// 的单一真相源——需要只看项目**自身**文本的消费方（conventions 在给仓库
// AGENTS.md/CLAUDE.md 计指纹前剥离该段）借此保证 forge 升级重写自身协议段
// 绝不翻转它们的过期判定。住在 util，runtime 包可直接消费。
const (
	ForgeSectionStart = "<!-- FORGE:START -->"
	ForgeSectionEnd   = "<!-- FORGE:END -->"
)

// StripMarkedSection removes the content between startMarker and endMarker.
//
// StripMarkedSection 移除 startMarker 与 endMarker 之间的内容（含标记），
// 规整接缝使两侧之间保留单个空行；after 侧的尾部换行原样保留。接缝形态即
// conventions 指纹的输入——「更整洁」的尾部规整会让既有指纹一次性翻转，
// 正是剥离要防的自伤 STALE。标记缺失或颠倒时原样返回（幂等）。
func StripMarkedSection(content, startMarker, endMarker string) string {
	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return content
	}
	before := strings.TrimRight(content[:startIdx], "\n")
	after := strings.TrimLeft(content[endIdx+len(endMarker):], "\n")
	switch {
	case before == "" && after == "":
		return ""
	case before == "":
		return after + "\n"
	case after == "":
		return before + "\n"
	default:
		return before + "\n\n" + after + "\n"
	}
}

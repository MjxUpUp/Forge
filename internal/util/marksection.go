package util

import "strings"

// ReplaceMarkedSection replaces the content between startMarker and endMarker with newSection; all content outside the markers is preserved as-is.
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
		// 未找到标记——追加该 section
		return content + "\n" + newSection
	}

	before := content[:startIdx]
	after := content[endIdx+len(endMarker):]

	// newSection 末尾的换行来自 endMarker+换行，先 TrimRight 掉它，
	// 以精确控制标记之间以及与后续内容之间的间距
	section := strings.TrimRight(newSection, "\n")

	result := before + section + "\n"

	// 清除 after-content 的前导空白
	after = strings.TrimLeft(after, "\n")
	if after != "" {
		result += "\n" + after
	}
	return result
}

// ForgeSectionStart/End 是包在用户指令文件（CLAUDE.md / AGENTS.md /
// .windsurfrules）里 forge 生成段外的那对 HTML 注释标记。「forge 生成了什么」
// 的单一真相源——需要只看项目**自身**文本的消费方（conventions 在给仓库
// AGENTS.md/CLAUDE.md 计指纹前剥离该段）借此保证 forge 升级重写自身协议段
// 绝不翻转它们的过期判定。住在 util（非 skillgen）：runtime 包可用而无需
// import 生成器层（否则 taskpipeline→conventions 会经 skillgen 成环）。
const (
	ForgeSectionStart = "<!-- FORGE:START -->"
	ForgeSectionEnd   = "<!-- FORGE:END -->"
)

// StripMarkedSection removes the content between startMarker and endMarker.
//
// StripMarkedSection 移除 startMarker 与 endMarker 之间的内容（含标记），
// 规整接缝使两侧之间保留单个空行；after 侧的尾部换行原样保留。与搬迁来源
// skillgen 原实现**行为完全一致**——「更整洁」的尾部规整会让所有按旧接缝
// 形态算过的 conventions 指纹一次性翻转，恰是剥离要防的自伤 STALE 类
// （对抗审查发现 #3）。标记缺失或颠倒时原样返回（幂等）。
// ReplaceMarkedSection 的逆操作（其调用方在 newSection 里自带标记——
// claudemd 的 forge 段自带）。
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

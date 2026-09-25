package util

// TruncateRunes truncates s to at most n runes, appending an ellipsis when truncation happens.
//
// TruncateRunes 把 s 截断到最多 n 个 rune，发生截断时追加省略号。
// 与曾散落各包的本地 truncate（保 n-3+"..."，总长恰 n）不同：本实现保 n+"…"，
// 总长 n+1 且无 n≤3 特例——收敛时各消费方的省略号字形/总长差异以此声明为准
// （2026-09 普查 P3-1/A2-2/A2-3 多批收敛）。
// 按 rune（字符）而非字节切片：中文每字 3 字节，字节切片会在字符中间切断产生
// 无效 UTF-8，json.Marshal 会替换为 U+FFFD 导致审计日志乱码。省略号标记该值
// 已被截断，避免读者误当成完整原文。
func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

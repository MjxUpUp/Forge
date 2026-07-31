package util

// TruncateRunes truncates s to at most n runes, appending an ellipsis when
// truncation happens. Slices by rune (character), not by byte: a Chinese char is
// 3 bytes, and byte slicing would cut in the middle of a character, producing
// invalid UTF-8 that json.Marshal replaces with U+FFFD, corrupting audit logs.
// The ellipsis marks the stored value as truncated so a reader does not mistake
// it for the complete original.
//
// TruncateRunes 把 s 截断到最多 n 个 rune，发生截断时追加省略号。
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

package util

import (
	"regexp"
	"strings"
)

// sessionCollapseRe 包级预编译：SanitizeSessionID 在 hook 热路径上高频调用，
// 每次调用重新编译正则是纯浪费。
var sessionCollapseRe = regexp.MustCompile(`[_-]{2,}`)

// SanitizeSessionID collapses a session id into a filename- and shell-safe character set.
//
// SanitizeSessionID 把 session id 收敛到文件名与 shell 安全的字符集。
// 它是 cli（hook env vars）、taskpipeline（session 状态文件名）以及其他 util 调用方
// 共同使用的单一真相源——本地勿重复实现。
//
// 策略（allowlist + normalization）：
//   - 只保留 [a-zA-Z0-9_-]；其余 rune 一律替换为 '_'。allowlist 比 denylist 更严——
//     它同时中和 shell 元字符（; & $ `）以及仅基于文件系统的 denylist 会漏掉的
//     path-traversal 点号。
//   - 把连续的分隔符（_--、__）压缩成单个 '_'。
//   - 修剪首尾的分隔符与短横。
//   - 截断到 64 字符（文件系统可移植性）。
//   - 若结果为空则回退到 session。
func SanitizeSessionID(id string) string {
	// 不对 "" 提前返回：空输入一路走到下方的空结果兜底，与「全是脏字符」的输入一样
	// 得到 "session"——与 doc 承诺一致。
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	safe := b.String()

	// Collapse consecutive underscores/dashes into a single underscore.
	// 把连续的下划线/短横压缩成单个下划线。（包级预编译：本函数在 hook 热路径上被频繁调用。）
	safe = sessionCollapseRe.ReplaceAllString(safe, "_")

	// Trim leading/trailing separators and dashes.
	// 修剪首尾的分隔符与短横。
	safe = strings.Trim(safe, "_-")

	// Truncate to 64 chars for filesystem portability.
	// 截断到 64 字符，保证文件系统可移植性。
	if len(safe) > 64 {
		safe = safe[:64]
	}

	// Empty-result fallback (e.g. input is all unsafe characters).
	// 结果为空的兜底（如输入全是不安全字符）。
	if safe == "" {
		return "session"
	}

	return safe
}

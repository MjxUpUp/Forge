package util

import (
	"regexp"
	"strings"
)

// SanitizeSessionID collapses a session id into a filename- and shell-safe character set.
// It is the single source of truth shared by cli (hook env vars), taskpipeline (session state filenames),
// and other util callers — do not reimplement it locally.
//
// Strategy (allowlist + normalization):
//   - Keep only [a-zA-Z0-9_-]; every other rune is replaced with '_'. An allowlist is stricter than a denylist —
//     it neutralizes shell metacharacters (; & $ `) as well as path-traversal dots that a filesystem-only denylist would miss.
//   - Collapse consecutive separators (_--, __) into a single '_'.
//   - Trim leading/trailing separators and dashes.
//   - Truncate to 64 chars (filesystem portability).
//   - Fall back to session if the result is empty.
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
	if id == "" {
		return ""
	}

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
	// 把连续的下划线/短横压缩成单个下划线。
	safe = regexp.MustCompile(`[_-]{2,}`).ReplaceAllString(safe, "_")

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

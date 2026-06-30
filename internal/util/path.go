package util

import (
	"regexp"
	"strings"
)

// SanitizeSessionID reduces a session id to filename- and shell-safe characters.
// It is the single source of truth used by cli (hook env vars), taskpipeline
// (session state filenames), and util callers — do not reimplement locally.
//
// Strategy (allowlist + normalization):
//   - Keep only [a-zA-Z0-9_-]; replace every other rune with '_'. An allowlist
//     is stricter than a denylist — it also neutralizes shell metacharacters
//     (; & $ `) and path-traversal dots that a filesystem-only denylist leaves.
//   - Collapse runs of separators (_--, __) into a single '_'.
//   - Trim leading/trailing separators and dashes.
//   - Truncate to 64 chars (filesystem portability).
//   - Fall back to "session" if nothing remains.
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

	// Collapse runs of underscores/dashes into a single underscore.
	safe = regexp.MustCompile(`[_-]{2,}`).ReplaceAllString(safe, "_")

	// Trim leading/trailing separators and dashes.
	safe = strings.Trim(safe, "_-")

	// Truncate to 64 characters for filesystem portability.
	if len(safe) > 64 {
		safe = safe[:64]
	}

	// Fallback for empty result (e.g., input was all unsafe chars).
	if safe == "" {
		return "session"
	}

	return safe
}

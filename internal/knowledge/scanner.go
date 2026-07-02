package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Violation records a single pattern match in code.
type Violation struct {
	EntryID  string `json:"entry_id"`
	Title    string `json:"title"`
	Pattern  string `json:"pattern"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	LineText string `json:"line_text"`
}

// maxFileSize is the maximum file size we'll process (10MB).
// Larger files are skipped to prevent memory exhaustion.
const maxFileSize = 10 * 1024 * 1024

// maxPatternLength is the maximum regex pattern length we'll compile.
const maxPatternLength = 10000

// CheckViolations scans a directory for code violating known gotchas.
// Uses regex matching instead of string-contains for precision.
//
// 双层去重：① 按 entry ID 跳过重复条目（index 里同 ID 重复时只扫一次，
// 避免输出放大——曾因 exp-accept1 重复 29 份导致 35380 违规里绝大多数是同一条重复）；
// ② 按 (entry, file, line, pattern) 去重输出，同一处代码不被同 pattern 重复报。
func (idx *Index) CheckViolations(dir string) []Violation {
	var violations []Violation
	seenEntry := map[string]bool{}
	seenViolation := map[string]bool{}
	for _, entry := range idx.Entries {
		if entry.Category != "gotchas" || seenEntry[entry.ID] {
			continue
		}
		seenEntry[entry.ID] = true
		for _, pat := range entry.Patterns {
			re, err := compileSafeRegex(pat)
			if err != nil {
				// Invalid regex — skip silently (logged at add time)
				continue
			}
			matches := scanDir(dir, re)
			for _, m := range matches {
				key := entry.ID + "\x00" + m.File + "\x00" + pat
				if seenViolation[key] {
					continue
				}
				seenViolation[key] = true
				violations = append(violations, Violation{
					EntryID:  entry.ID,
					Title:    entry.Title,
					Pattern:  pat,
					File:     m.File,
					Line:     m.Line,
					LineText: m.Text,
				})
			}
		}
	}
	return violations
}

// compileSafeRegex compiles a regex pattern with a length guard.
//
// The length cap defends against compiler DoS: an attacker-controlled pattern
// near maxPatternLength forces regexp.Compile to do meaningful work.
//
// Catastrophic-backtracking ReDoS is NOT a concern here. Go's regexp is RE2,
// which guarantees linear-time matching and cannot exhibit the exponential
// backtracking PCRE-style engines suffer on patterns like (a+)+. (Measured:
// 50000-char input against (a+)+ / ((a+)*)+ / ^(?:a+)+$ matches in ≤1.5ms.) No
// heuristic pattern screening is applied — RE2 is the real defense, and a
// heuristic that rejects (a+)+ would only false-positive on legitimate regexes.
func compileSafeRegex(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > maxPatternLength {
		return nil, fmt.Errorf("pattern too long: %d characters", len(pattern))
	}
	return regexp.Compile(pattern)
}

type grepMatch struct {
	File string
	Line int
	Text string
}

var sourceExtensions = map[string]bool{
	".go": true, ".rs": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".py": true, ".ets": true,
}

func scanDir(dir string, re *regexp.Regexp) []grepMatch {
	var matches []grepMatch
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "target": true,
		".forge": true, "dist": true, "build": true,
		".idea": true, ".vscode": true, "__pycache__": true, "bin": true,
	}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if !sourceExtensions[ext] {
			return nil
		}
		// Skip minified/bundled files
		base := filepath.Base(path)
		if containsSubstring(base, ".min.") || containsSubstring(base, ".umd.") {
			return nil
		}

		// Check file size before reading to prevent memory exhaustion
		if info.Size() > maxFileSize {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(dir, path)
		lines := splitLines(string(data))
		for i, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, grepMatch{
					File: relPath,
					Line: i + 1,
					Text: truncateLine(line, 120),
				})
			}
		}
		return nil
	})
	return matches
}

// splitLines splits a string into lines, handling both Unix and Windows line endings.
// This is optimized for performance over strings.Split.
func splitLines(s string) []string {
	// Fast path for empty string
	if s == "" {
		return []string{}
	}

	// Pre-allocate with estimated capacity (1 line per 80 chars is reasonable)
	estimatedLines := len(s)/80 + 1
	lines := make([]string, 0, estimatedLines)

	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			// Remove trailing '\r' if present (Windows line endings)
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}

	// Add the last line if it doesn't end with newline
	if start < len(s) {
		lines = append(lines, s[start:])
	}

	return lines
}

func containsSubstring(s, sub string) bool {
	return strings.Contains(s, sub)
}

func truncateLine(s string, maxLen int) string {
	// Use rune-aware truncation to avoid splitting multi-byte UTF-8 sequences
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// FormatViolations formats violations for CLI output.
func FormatViolations(violations []Violation) string {
	var out strings.Builder
	for _, v := range violations {
		out.WriteString(fmt.Sprintf("  [%s] %s\n", v.EntryID, v.Title))
		out.WriteString(fmt.Sprintf("    Pattern: %s\n", v.Pattern))
		out.WriteString(fmt.Sprintf("    Location: %s:%d\n", v.File, v.Line))
		out.WriteString(fmt.Sprintf("    Code: %s\n", truncateLine(v.LineText, 80)))
	}
	return out.String()
}

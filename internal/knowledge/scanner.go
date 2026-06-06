package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// CheckViolations scans a directory for code violating known gotchas.
// Uses regex matching instead of string-contains for precision.
func (idx *Index) CheckViolations(dir string) []Violation {
	var violations []Violation
	for _, entry := range idx.Entries {
		if entry.Category != "gotchas" {
			continue
		}
		for _, pat := range entry.Patterns {
			re, err := regexp.Compile(pat)
			if err != nil {
				// Invalid regex — skip silently (logged at add time)
				continue
			}
			matches := scanDir(dir, re)
			for _, m := range matches {
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

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else if ch != '\r' {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func truncateLine(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// FormatViolations formats violations for CLI output.
func FormatViolations(violations []Violation) string {
	var out string
	for _, v := range violations {
		out += fmt.Sprintf("  [%s] %s\n", v.EntryID, v.Title)
		out += fmt.Sprintf("    Pattern: %s\n", v.Pattern)
		out += fmt.Sprintf("    Location: %s:%d\n", v.File, v.Line)
		out += fmt.Sprintf("    Code: %s\n", truncateLine(v.LineText, 80))
	}
	return out
}

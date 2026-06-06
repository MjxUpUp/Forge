package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// KnowledgeCheckEvaluator scans the project source for known gotcha patterns.
type KnowledgeCheckEvaluator struct{}

func (e *KnowledgeCheckEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{
			Name:   "knowledge_check",
			Passed: true,
			Detail: "cannot determine home directory",
		}
	}

	indexDir := filepath.Join(home, ".forge", "knowledge", "gotchas")
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		return Result{
			Name:   "knowledge_check",
			Passed: true,
			Detail: "no gotchas knowledge base found",
		}
	}

	var violations []string
	sourceExtensions := map[string]bool{
		".go": true, ".rs": true, ".ts": true, ".tsx": true,
		".js": true, ".jsx": true, ".py": true, ".ets": true,
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		mdPath := filepath.Join(indexDir, entry.Name())
		data, err := os.ReadFile(mdPath)
		if err != nil {
			continue
		}

		patterns := extractPatterns(string(data))
		for _, pat := range patterns {
			re, err := regexp.Compile(pat)
			if err != nil {
				continue
			}
			matches := scanDir(ctx.ProjectRoot, re, sourceExtensions)
			for _, m := range matches {
				violations = append(violations, fmt.Sprintf("%s:%d: %s", m.File, m.Line, m.Text))
			}
		}
	}

	if len(violations) > 0 {
		detail := fmt.Sprintf("%d violation(s) found", len(violations))
		if len(violations) <= 3 {
			for _, v := range violations {
				detail += fmt.Sprintf("\n  - %s", v)
			}
		}
		return Result{
			Name:    "knowledge_check",
			Passed:  false,
			Detail:  detail,
			Message: fmt.Sprintf("Found %d known experience violation(s)", len(violations)),
		}
	}

	return Result{
		Name:   "knowledge_check",
		Passed: true,
		Detail: "no known experience violations",
	}
}

type scanMatch struct {
	File string
	Line int
	Text string
}

func scanDir(root string, re *regexp.Regexp, exts map[string]bool) []scanMatch {
	var matches []scanMatch
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "target": true,
		".forge": true, "dist": true, "build": true,
		".idea": true, ".vscode": true, "__pycache__": true, "bin": true,
	}

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
		if !exts[ext] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(root, path)
		lines := splitLines(string(data))
		for i, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, scanMatch{
					File: relPath,
					Line: i + 1,
					Text: truncateString(line, 120),
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

func extractPatterns(content string) []string {
	var patterns []string
	for _, line := range splitLines(content) {
		for i := 0; i < len(line); i++ {
			if i+13 <= len(line) && line[i:i+13] == "**Patterns:**" {
				rest := line[i+13:]
				patterns = append(patterns, parsePatternList(rest)...)
				break
			}
		}
	}
	return patterns
}

func parsePatternList(s string) []string {
	var result []string
	current := ""
	for _, ch := range s {
		if ch == ',' {
			trimmed := trimSpaces(current)
			if trimmed != "" {
				result = append(result, trimmed)
			}
			current = ""
		} else {
			current += string(ch)
		}
	}
	trimmed := trimSpaces(current)
	if trimmed != "" {
		result = append(result, trimmed)
	}
	return result
}

func trimSpaces(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen-3]) + "..."
	}
	return s
}

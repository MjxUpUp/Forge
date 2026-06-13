// Package clone provides lightweight code duplication detection.
// Uses token-level Jaccard similarity to compare files.
package clone

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// SimilarityResult reports the similarity between two files.
type SimilarityResult struct {
	FileA     string  `json:"file_a"`
	FileB     string  `json:"file_b"`
	Similarity float64 `json:"similarity"` // 0.0 to 1.0
}

// DetectClones scans a directory for files too similar to the target file.
// Returns matches above the threshold (0.0–1.0). Uses plain text tokenization
// (whitespace split) for speed — full AST diff is a future enhancement.
func DetectClones(dir, targetPath string, threshold float64) ([]SimilarityResult, error) {
	targetTokens, err := tokenizeFile(targetPath)
	if err != nil || len(targetTokens) < 10 {
		return nil, err
	}

	ext := filepath.Ext(targetPath)
	var results []SimilarityResult

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Only compare same-language files
		if filepath.Ext(path) != ext {
			return nil
		}
		// Don't compare with self
		if path == targetPath {
			return nil
		}
		// Skip vendored and generated files
		if strings.Contains(path, "vendor/") || strings.Contains(path, "node_modules/") ||
			strings.Contains(path, ".git/") {
			return nil
		}

		tokens, err := tokenizeFile(path)
		if err != nil || len(tokens) < 10 {
			return nil
		}

		sim := jaccardSimilarity(targetTokens, tokens)
		if sim >= threshold {
			results = append(results, SimilarityResult{
				FileA:     targetPath,
				FileB:     path,
				Similarity: sim,
			})
		}
		return nil
	})

	return results, nil
}

// tokenizeFile reads a file and returns whitespace-split tokens.
func tokenizeFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var tokens []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip blank lines and comments for more meaningful comparison
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		tokens = append(tokens, parts...)
	}
	return tokens, scanner.Err()
}

// jaccardSimilarity computes the Jaccard index between two token sets.
// Uses a map for O(n+m) performance.
func jaccardSimilarity(a, b []string) float64 {
	setA := make(map[string]struct{}, len(a))
	for _, t := range a {
		setA[t] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, t := range b {
		setB[t] = struct{}{}
	}

	intersection := 0
	for t := range setA {
		if _, ok := setB[t]; ok {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

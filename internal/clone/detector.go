// Package clone 提供 lightweight 代码重复检测。
// 用 token 级 Jaccard 相似度比较文件。
package clone

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// SimilarityResult 报告两个文件之间的相似度。
type SimilarityResult struct {
	FileA      string  `json:"file_a"`
	FileB      string  `json:"file_b"`
	Similarity float64 `json:"similarity"` // 0.0 to 1.0
}

// normalizePath 返回绝对、正斜杠、cleaned 的路径。
// 保证跨平台比较可靠：CLI 传相对路径，filepath.Walk 产出绝对路径，
// 且 Windows 用反斜杠。
func normalizePath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// DetectClones 扫描某目录，找出与目标文件过于相似的文件。
// 返回相似度高于 threshold（0.0–1.0）的匹配。为速度采用纯文本 tokenization
// （按空白拆分）——完整 AST diff 是后续增强项。
func DetectClones(dir, targetPath string, threshold float64) ([]SimilarityResult, error) {
	// 把 target 归一为绝对、正斜杠形式，便于跨平台自比较。
	// CLI 传相对路径，filepath.Walk 产出绝对路径，故直接等值比较会失败，
	// target 会 100% 匹配到自己。
	normTarget := normalizePath(targetPath)

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
		normPath := normalizePath(path)
		// 只比较同语言文件
		if filepath.Ext(path) != ext {
			return nil
		}
		// 不与自己比较
		if normPath == normTarget {
			return nil
		}
		// 跳过 vendored 和 generated 文件（在正斜杠形式上比较，
		// 以正确匹配 Windows 反斜杠路径）。
		if strings.Contains(normPath, "/vendor/") || strings.Contains(normPath, "/node_modules/") ||
			strings.Contains(normPath, "/.git/") {
			return nil
		}

		tokens, err := tokenizeFile(path)
		if err != nil || len(tokens) < 10 {
			return nil
		}

		sim := jaccardSimilarity(targetTokens, tokens)
		if sim >= threshold {
			results = append(results, SimilarityResult{
				FileA:      targetPath,
				FileB:      path,
				Similarity: sim,
			})
		}
		return nil
	})

	return results, nil
}

// tokenizeFile 读取文件并返回按空白拆分的 token。
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
		// 跳过空行与注释，让比较更有意义
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		tokens = append(tokens, parts...)
	}
	return tokens, scanner.Err()
}

// jaccardSimilarity 计算两个 token 集合之间的 Jaccard index。
// 用 map 实现 O(n+m) 性能。
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

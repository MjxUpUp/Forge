// Package clone provides lightweight code duplication detection.
// It compares files using token-level Jaccard similarity.
//
// Package clone 提供 lightweight 代码重复检测。
// 用 token 级 Jaccard 相似度比较文件。
package clone

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SimilarityResult reports the similarity between two files.
//
// SimilarityResult 报告两个文件之间的相似度。
type SimilarityResult struct {
	FileA      string  `json:"file_a"`
	FileB      string  `json:"file_b"`
	Similarity float64 `json:"similarity"` // 0.0 to 1.0
}

// normalizePath returns an absolute, slash-separated, cleaned path.
// This keeps cross-platform comparison reliable: the CLI receives relative paths,
// filepath.Walk produces absolute paths, and Windows uses backslashes.
//
// normalizePath 返回绝对、正斜杠、cleaned 的路径。
// 保证跨平台比较可靠：CLI 传相对路径，filepath.Walk 产出绝对路径，
// 且 Windows 用反斜杠。
func normalizePath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// minTokens is the minimum token count for a meaningful Jaccard comparison: below it the
// similarity score is noise (a handful of shared keywords can swing it anywhere).
//
// minTokens 是 Jaccard 比对有意义的最低 token 数：低于它相似度分数就是噪声
// （几个共享关键字就能把分数甩到任意值）。
const minTokens = 10

// DetectClones scans a directory for files that are too similar to the target file.
// It returns matches whose similarity is above threshold (0.0–1.0). It uses
// plain-text tokenization (splitting on whitespace) for speed — a full AST diff is a
// future enhancement.
//
// Errors: a target file with fewer than minTokens tokens returns an explicit error (too few
// tokens to compare meaningfully) rather than a silent (nil, nil) that would masquerade as
// "scan complete, no clones"; a filepath.Walk failure on the root directory (missing/unreadable)
// is likewise returned instead of swallowed. Single-file errors inside the walk keep the
// skip-and-continue policy.
//
// DetectClones 扫描某目录，找出与目标文件过于相似的文件。
// 返回相似度高于 threshold（0.0–1.0）的匹配。为速度采用纯文本 tokenization
// （按空白拆分）——完整 AST diff 是后续增强项。
//
// 错误：目标文件 token 数少于 minTokens 时返回显式错误（token 太少无法有效比对），
// 而非静默 (nil, nil) 伪装成"扫描完成无克隆"；filepath.Walk 在根目录上的失败
// （目录不存在/不可读）同样返回而非吞掉。walk 内的单文件错误保持跳过继续策略。
func DetectClones(dir, targetPath string, threshold float64) ([]SimilarityResult, error) {
	// An out-of-range or NaN threshold would silently turn the gate off (>1 / NaN: every
	// comparison false → "no clones" with a green exit) or on (<0: everything matches) —
	// a quality gate must fail loud on an unusable knob, not report a clean result.
	//
	// 越界或 NaN 的 threshold 会静默关掉门禁（>1/NaN：所有比较为假→"无克隆"绿色退出）
	// 或全开（<0：全部命中）——门禁工具对不可用的旋钮必须响亮报错，而不是给出干净结果。
	if threshold < 0.0 || threshold > 1.0 || threshold != threshold {
		return nil, fmt.Errorf("threshold 必须在 [0.0, 1.0]（ got %v；NaN/越界会被拒绝而不是静默扫描出空结果）", threshold)
	}
	// Normalize target to absolute, slash-separated form for cross-platform
	// self-comparison. The CLI passes relative paths while filepath.Walk yields
	// absolute paths, so a direct equality check would fail and target would 100%
	// match itself.
	//
	// 把 target 归一为绝对、正斜杠形式，便于跨平台自比较。
	// CLI 传相对路径，filepath.Walk 产出绝对路径，故直接等值比较会失败，
	// target 会 100% 匹配到自己。
	normTarget := normalizePath(targetPath)

	targetTokens, err := tokenizeFile(targetPath)
	if err != nil {
		return nil, err
	}
	if len(targetTokens) < minTokens {
		return nil, fmt.Errorf("target file %s has only %d tokens (<%d): too few for a meaningful similarity comparison",
			targetPath, len(targetTokens), minTokens)
	}

	ext := filepath.Ext(targetPath)
	var results []SimilarityResult

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A root-directory error (missing/unreadable dir) must propagate — swallowing
			// it returns (nil, nil) and masquerades as "scan complete, no clones".
			// Single-file errors keep the skip-and-continue policy.
			//
			// 根目录错误（目录不存在/不可读）必须上抛——吞掉会返回 (nil, nil)，
			// 伪装成"扫描完成无克隆"。单文件错误保持跳过继续策略。
			if path == dir {
				return err
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		normPath := normalizePath(path)
		// Only compare files of the same language.
		//
		// 只比较同语言文件
		if filepath.Ext(path) != ext {
			return nil
		}
		// Do not compare the file with itself.
		//
		// 不与自己比较
		if normPath == normTarget {
			return nil
		}
		// Skip vendored and generated files (compared on the slash-separated
		// form to correctly match Windows backslash paths).
		//
		// 跳过 vendored 和 generated 文件（在正斜杠形式上比较，
		// 以正确匹配 Windows 反斜杠路径）。
		if strings.Contains(normPath, "/vendor/") || strings.Contains(normPath, "/node_modules/") ||
			strings.Contains(normPath, "/.git/") {
			return nil
		}

		tokens, err := tokenizeFile(path)
		if err != nil || len(tokens) < minTokens {
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
	if walkErr != nil {
		return nil, fmt.Errorf("scan directory %s: %w", dir, walkErr)
	}

	return results, nil
}

// tokenizeFile reads a file and returns tokens split on whitespace.
//
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
		// Skip blank lines and comments so the comparison is more meaningful.
		//
		// 跳过空行与注释，让比较更有意义
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		tokens = append(tokens, parts...)
	}
	return tokens, scanner.Err()
}

// jaccardSimilarity computes the Jaccard index between two token sets.
// It uses a map for O(n+m) performance.
//
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

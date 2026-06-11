package scoring

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Harness/forge/internal/scoringtypes"
)

// Evaluate scores a completed task across 8 dimensions and returns a ScoreResult.
func Evaluate(input *EvaluateInput, config *scoringtypes.ScoringConfig) *scoringtypes.ScoreResult {
	dimensions := []scoringtypes.DimensionScore{
		scoreProcess(input.GateHistory),
		scoreTesting(input.GitDiffTest),
		scoreCodeQuality(input.CompilePassed, input.CompileChecked),
		scoreAssertions(input.AssertionPassed, input.AssertionChecked),
		scoreScope(input.GitDiffStat),
		scoreEfficiency(input.StartedAt, input.CompletedAt),
		scoreToolSelection(input.ToolCalls, input.AntiPatterns),
		scoreSkillHit(input.SkillHits, input.RecommendedSkills),
	}

	overall := weightedOverall(dimensions, config.Weights)
	grade := scoringtypes.GradeFromScore(overall, config.Thresholds)

	return &scoringtypes.ScoreResult{
		Dimensions: dimensions,
		Overall:    overall,
		Grade:      grade,
		ScoredAt:   time.Now(),
	}
}

// --- Dimension scorers ---

func scoreProcess(h GateHistory) scoringtypes.DimensionScore {
	if h.TotalGates == 0 {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionProcess,
			Score:     70,
			Detail:    "No gate history available",
		}
	}

	base := 100
	penalty := h.Retries * 15
	score := base - penalty
	if score < 20 {
		score = 20
	}

	detail := fmt.Sprintf("Passed %d/%d gates, %d retries", h.Passed, h.TotalGates, h.Retries)
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionProcess,
		Score:     score,
		Detail:    detail,
	}
}

func scoreTesting(diffTest string) scoringtypes.DimensionScore {
	if diffTest == "" {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionTesting,
			Score:     20,
			Detail:    "No test file changes detected",
		}
	}

	// Count test-related lines vs total lines in the diff
	testLines := 0
	totalLines := 0
	for _, line := range splitLines(diffTest) {
		if len(line) > 0 && (line[0] == '+' || line[0] == '-') {
			totalLines++
			// Heuristic: test files contain "test", "spec", or "_test" patterns
			if isTestLine(line) {
				testLines++
			}
		}
	}

	if totalLines == 0 {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionTesting,
			Score:     20,
			Detail:    "Empty diff",
		}
	}

	ratio := float64(testLines) / float64(totalLines)
	// Score: 80-100 based on test/code ratio
	score := 80 + int(ratio*20)
	if score > 100 {
		score = 100
	}

	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionTesting,
		Score:     score,
		Detail:    fmt.Sprintf("Test ratio: %.0f%% (%d/%d changed lines)", ratio*100, testLines, totalLines),
	}
}

func scoreCodeQuality(passed, checked bool) scoringtypes.DimensionScore {
	if !checked {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionCodeQuality,
			Score:     50,
			Detail:    "Compile gate not run",
		}
	}
	if passed {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionCodeQuality,
			Score:     100,
			Detail:    "Compilation passed",
		}
	}
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionCodeQuality,
		Score:     0,
		Detail:    "Compilation failed",
	}
}

func scoreAssertions(passed, checked bool) scoringtypes.DimensionScore {
	if !checked {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionAssertions,
			Score:     70,
			Detail:    "Assertion check not run",
		}
	}
	if passed {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionAssertions,
			Score:     100,
			Detail:    "No assertion weakening detected",
		}
	}
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionAssertions,
		Score:     0,
		Detail:    "Assertion weakening detected",
	}
}

func scoreScope(diffStat string) scoringtypes.DimensionScore {
	if diffStat == "" {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionScope,
			Score:     70,
			Detail:    "Diff stat unavailable",
		}
	}

	totalLines := parseDiffStatLines(diffStat)
	var score int
	var detail string

	switch {
	case totalLines <= 50:
		score = 100
		detail = fmt.Sprintf("Small change: %d lines", totalLines)
	case totalLines <= 200:
		score = 80
		detail = fmt.Sprintf("Medium change: %d lines", totalLines)
	case totalLines <= 500:
		score = 60
		detail = fmt.Sprintf("Large change: %d lines", totalLines)
	default:
		score = 40
		detail = fmt.Sprintf("Very large change: %d lines", totalLines)
	}

	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionScope,
		Score:     score,
		Detail:    detail,
	}
}

func scoreEfficiency(startedAt, completedAt time.Time) scoringtypes.DimensionScore {
	if startedAt.IsZero() || completedAt.IsZero() {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionEfficiency,
			Score:     70,
			Detail:    "Time data unavailable",
		}
	}

	duration := completedAt.Sub(startedAt)
	minutes := duration.Minutes()

	var score int
	switch {
	case minutes <= 5:
		score = 100
	case minutes <= 15:
		score = 90
	case minutes <= 30:
		score = 80
	case minutes <= 60:
		score = 60
	default:
		score = 40
	}

	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionEfficiency,
		Score:     score,
		Detail:    fmt.Sprintf("Completed in %.0f minutes", minutes),
	}
}

func scoreToolSelection(totalCalls int, violations []AntiPatternHit) scoringtypes.DimensionScore {
	if totalCalls == 0 {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionToolSelection,
			Score:     70,
			Detail:    "No tool usage data available",
		}
	}

	majorCount := 0
	minorCount := 0
	for _, v := range violations {
		switch v.Severity {
		case "major":
			majorCount++
		case "minor":
			minorCount++
		}
	}

	// Penalty: -15 per major violation, -5 per minor violation
	score := 100 - (majorCount * 15) - (minorCount * 5)
	if score < 0 {
		score = 0
	}

	detail := fmt.Sprintf("%d tool calls, %d anti-patterns (%d major, %d minor)",
		totalCalls, len(violations), majorCount, minorCount)
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionToolSelection,
		Score:     score,
		Detail:    detail,
	}
}

func scoreSkillHit(hits []SkillHitData, recommendedCount int) scoringtypes.DimensionScore {
	if recommendedCount == 0 {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionSkillHit,
			Score:     70,
			Detail:    "No skill recommendations for this task",
		}
	}

	// Deduplicate skill names
	seen := make(map[string]bool)
	uniqueHits := 0
	var names []string
	for _, h := range hits {
		if !seen[h.SkillName] {
			seen[h.SkillName] = true
			uniqueHits++
			names = append(names, h.SkillName)
		}
	}

	ratio := float64(uniqueHits) / float64(recommendedCount)
	if ratio > 1.0 {
		ratio = 1.0
	}
	score := int(ratio * 100)

	detail := fmt.Sprintf("Skill hit rate: %.0f%% (%d/%d): %s",
		ratio*100, uniqueHits, recommendedCount, strings.Join(names, ", "))
	if len(names) == 0 {
		detail = fmt.Sprintf("Skill hit rate: 0%% (0/%d)", recommendedCount)
	}

	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionSkillHit,
		Score:     score,
		Detail:    detail,
	}
}

// --- Helpers ---

func weightedOverall(dimensions []scoringtypes.DimensionScore, weights map[string]float64) float64 {
	total := 0.0
	weightSum := 0.0

	for _, d := range dimensions {
		w, ok := weights[string(d.Dimension)]
		if !ok {
			w = 1.0 / float64(len(dimensions))
		}
		total += float64(d.Score) * w
		weightSum += w
	}

	if weightSum == 0 {
		return 0
	}
	return math.Round(total/weightSum*100) / 100
}

// parseDiffStatLines extracts total changed lines from git diff --stat output.
// Format: "file.go | 10 +++---"
func parseDiffStatLines(stat string) int {
	total := 0
	for _, line := range splitLines(stat) {
		// Look for the pipe separator
		pipeIdx := indexOfPipe(line)
		if pipeIdx < 0 {
			continue
		}
		// After the pipe, extract the number before +/-
		after := line[pipeIdx+1:]
		added, deleted := parseAddDelete(after)
		total += added + deleted
	}
	return total
}

// parseAddDelete parses " 10 +++---" into (added, deleted).
func parseAddDelete(s string) (int, int) {
	added := 0
	deleted := 0
	inNum := false
	numStart := -1

	// Skip leading spaces
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}

	// Parse first number (insertions)
	numStart = i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		inNum = true
		i++
	}
	if inNum && numStart < len(s) {
		fmt.Sscanf(s[numStart:i], "%d", &added)
	}

	// Skip spaces
	for i < len(s) && s[i] == ' ' {
		i++
	}

	// Parse second number (deletions) if present
	numStart = i
	inNum = false
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		inNum = true
		i++
	}
	if inNum && numStart < len(s) {
		fmt.Sscanf(s[numStart:i], "%d", &deleted)
	}

	return added, deleted
}

func indexOfPipe(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			return i
		}
	}
	return -1
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			result = append(result, line)
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

// isTestLine checks if a diff line is from a test file.
func isTestLine(line string) bool {
	// Check common test file patterns in the path portion
	testPatterns := []string{"_test.", "_spec.", ".test.", ".spec.", "test/", "tests/", "spec/", "specs/", "__tests__/"}
	for _, p := range testPatterns {
		if containsString(line, p) {
			return true
		}
	}
	return false
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

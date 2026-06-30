package scoring

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// Evaluate scores a completed task across 6 dimensions and returns a ScoreResult.
func Evaluate(input *EvaluateInput, config *scoringtypes.ScoringConfig) *scoringtypes.ScoreResult {
	dimensions := []scoringtypes.DimensionScore{
		scoreProcess(input.GateHistory),
		scoreTesting(input.TestCoveragePassed, input.TestCoverageChecked),
		scoreCodeQuality(input.CompilePassed, input.CompileChecked),
		scoreAssertions(input.AssertionPassed, input.AssertionChecked),
		scoreScope(input.GitDiffStat),
		scoreEfficiency(input.StartedAt, input.CompletedAt),
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

// scoreTesting scores the testing dimension from the test-coverage gate verdict.
//
// The previous implementation estimated coverage from the ratio of test-file
// lines in the git diff. That heuristic collapsed to a constant 20 whenever a
// task's changes were committed before `forge task start`: task start records
// HeadCommit=HEAD, so base..HEAD diffed empty, scoreTesting saw no test lines,
// and every such task was unfairly penalized to testing=20 — in one audited
// project, 49% of scored tasks. The fix reuses the SAME verdict the task-verify
// gate already computed (CheckTestCoverage), read from checklog's
// test-coverage-gate entry with a live fallback. Scoring and gating now agree
// by construction.
func scoreTesting(passed, checked bool) scoringtypes.DimensionScore {
	if !checked {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionTesting,
			Score:     70,
			Detail:    "Test coverage not checked",
		}
	}
	if passed {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionTesting,
			Score:     100,
			Detail:    "All changed source files have corresponding tests",
		}
	}
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionTesting,
		Score:     20,
		Detail:    "Source files changed without a corresponding test",
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

// parseDiffStatLines sums total changed lines from `git diff --numstat` output.
// numstat format: "<added>\t<deleted>\t<path>" (binary files show "-\t-\t<path>").
//
// numstat replaces `--stat`: --stat's second column is the insertions+deletions
// TOTAL and its trailing "+/-" bar is a width-limited visualization, not the
// real per-side counts. The previous parser read that total as "added" and
// never found a second number, so "deleted" was always 0 — the scope TOTAL was
// right only by coincidence (added==total), while the (added, deleted) contract
// was wrong. numstat gives true per-side counts and the same total.
func parseDiffStatLines(stat string) int {
	total := 0
	for _, line := range splitLines(stat) {
		if added, deleted, ok := parseNumstatLine(line); ok {
			total += added + deleted
		}
	}
	return total
}

// parseNumstatLine parses one "added\tdeleted\tpath" line. Binary entries
// ("-\t-\t...") are skipped. Returns ok=false for blank or malformed lines.
func parseNumstatLine(line string) (added, deleted int, ok bool) {
	tab1 := strings.IndexByte(line, '\t')
	if tab1 < 0 {
		return 0, 0, false
	}
	addedField := line[:tab1]
	rest := line[tab1+1:]
	tab2 := strings.IndexByte(rest, '\t')
	if tab2 < 0 {
		return 0, 0, false
	}
	deletedField := rest[:tab2]
	if addedField == "-" || deletedField == "-" {
		return 0, 0, false // binary file — no line counts
	}
	a, errA := strconv.Atoi(addedField)
	d, errD := strconv.Atoi(deletedField)
	if errA != nil || errD != nil {
		return 0, 0, false
	}
	return a, d, true
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

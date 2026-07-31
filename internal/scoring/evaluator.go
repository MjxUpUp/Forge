package scoring

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// Evaluate scores a completed task across 6 dimensions and returns a ScoreResult.
//
// Evaluate 对一个完成任务跨 6 个维度打分，返回 ScoreResult。
func Evaluate(input *EvaluateInput, config *scoringtypes.ScoringConfig) *scoringtypes.ScoreResult {
	dimensions := []scoringtypes.DimensionScore{
		scoreProcess(input.GateHistory),
		scoreTesting(input.TestCoverageCovered, input.TestCoverageTotal, input.TestAssertionCount, input.TestCoverageChecked),
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
		Evidence:   buildEvidenceSummary(input.EvidenceDeterministic, input.EvidenceAgentClaim),
	}
}

// buildEvidenceSummary summarizes the evidence-chain source counts into ScoreResult.Evidence. total=0 returns nil
// (no evidence data, e.g. old task checklog is empty), avoiding zero-value noise output.
//
// buildEvidenceSummary 把证据链来源计数摘要成 ScoreResult.Evidence。total=0 返回 nil
// （无证据数据，如旧任务 checklog 为空），避免输出零值噪声。
func buildEvidenceSummary(deterministic, agentClaim int) *scoringtypes.EvidenceSummary {
	total := deterministic + agentClaim
	if total == 0 {
		return nil
	}
	ratio := float64(deterministic) / float64(total)
	return &scoringtypes.EvidenceSummary{
		Deterministic: deterministic,
		AgentClaim:    agentClaim,
		Total:         total,
		Ratio:         ratio,
	}
}

// --- dimension scoring functions ---
//
// --- 维度打分函数 ---

func scoreProcess(h GateHistory) scoringtypes.DimensionScore {
	if h.TotalGates == 0 {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionProcess,
			Score:     70,
			Detail:    `No gate history available`,
		}
	}

	// Passed participates in scoring: a task that completed only part of the gate set (reachable
	// in production — legacy tasks recorded when DefaultGates was longer, or gates skipped via
	// overrides) must not score the same as a fully-gated task.
	//
	// Passed 参与计分：只过了一部分门禁的任务（生产可达——DefaultGates 变长前的老任务、
	// override 跳过的门禁）不得与全通过的任务同分。
	passed := h.Passed
	if passed > h.TotalGates {
		passed = h.TotalGates
	}
	if passed < 0 {
		passed = 0
	}
	base := int(math.Round(100.0 * float64(passed) / float64(h.TotalGates)))
	score := base - h.Retries*15
	if score < 20 {
		score = 20
	}

	detail := fmt.Sprintf(`Passed %d/%d gates, %d retries`, h.Passed, h.TotalGates, h.Retries)
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionProcess,
		Score:     score,
		Detail:    detail,
	}
}

// scoreTesting scores continuously by the ratio of source files with paired tests, overlaid with an assertion-density signal.
//
// The old implementation was binary (CheckTestCoverage ok -> 100/70/20): a single source file without a paired test would press the entire
// dimension down to 20, judging good work like 5 source files with 4 having tests as 20 points. Industry rubric evaluation emphasizes
// anchored continuous scoring (opposed to pure binary), so it was changed to a ratio:
//
//	ratio = covered/total, base = 30 + 70*ratio (none paired -> 30, all paired -> 100)
//
// Assertion density correction: if covered>0 but assertionCount==0, the test files only have setup/log without assertions
// (fake tests), base x 0.6. Based on the STREW Assertion-McCabe ratio — the number of assertions measures test adequacy.
// total==0 (no testable source) -> 100 (no target should not be penalized). checked=false -> 70 (neutral, not checked).
//
// scoreTesting 按"有配对测试的源码文件比例"连续打分，叠加断言密度信号。
//
// 旧实现是二值（CheckTestCoverage ok → 100/70/20）：一个源码文件没配对测试就把整个
// 维度压到 20，把"5 个源码 4 个有测试"这种良好工作判为 20 分。业界 rubric 评估强调
// anchored 连续打分（反对纯二值），故改为比例：
//
//	ratio = covered/total，base = 30 + 70*ratio（全没配对 30，全配对 100）
//
// 断言密度修正：若 covered>0 但 assertionCount==0，说明测试文件只有 setup/log 无断言
// （假测试），base × 0.6。依据 STREW 的 Assertion-McCabe ratio——断言数度量测试充分性。
// total==0（无可测源码）→ 100（无对象不该被惩罚）。checked=false → 70（中性，未检测）。
func scoreTesting(covered, total, assertionCount int, checked bool) scoringtypes.DimensionScore {
	if !checked {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionTesting,
			Score:     70,
			Detail:    `Test coverage not checked`,
		}
	}
	if total <= 0 {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionTesting,
			Score:     100,
			Detail:    `No source files requiring tests`,
		}
	}
	if covered > total {
		covered = total
	}
	if covered < 0 {
		covered = 0
	}
	ratio := float64(covered) / float64(total)
	base := 30.0 + 70.0*ratio
	if covered > 0 && assertionCount == 0 {
		base *= 0.6
	}
	score := int(math.Round(base))
	detail := fmt.Sprintf(`%d/%d source files covered; %d assertions`, covered, total, assertionCount)
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionTesting,
		Score:     score,
		Detail:    detail,
	}
}

func scoreCodeQuality(passed, checked bool) scoringtypes.DimensionScore {
	if !checked {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionCodeQuality,
			Score:     50,
			Detail:    `Compile gate not run`,
		}
	}
	if passed {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionCodeQuality,
			Score:     100,
			Detail:    `Compilation passed`,
		}
	}
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionCodeQuality,
		Score:     0,
		Detail:    `Compilation failed`,
	}
}

func scoreAssertions(passed, checked bool) scoringtypes.DimensionScore {
	if !checked {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionAssertions,
			Score:     70,
			Detail:    `Assertion check not run`,
		}
	}
	if passed {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionAssertions,
			Score:     100,
			Detail:    `No assertion weakening detected`,
		}
	}
	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionAssertions,
		Score:     0,
		Detail:    `Assertion weakening detected`,
	}
}

// scoreScope scores by change size. parseDiffStatLines has already excluded test files and non-source files —
// writing tests should not be reversely penalized (see parseDiffStatLines for details).
//
// scoreScope 按改动规模打分。parseDiffStatLines 已排除测试文件和非源码文件——
// 写测试不该被反向惩罚（详见 parseDiffStatLines）。
func scoreScope(diffStat string) scoringtypes.DimensionScore {
	if diffStat == "" {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionScope,
			Score:     70,
			Detail:    `Diff stat unavailable`,
		}
	}

	totalLines := parseDiffStatLines(diffStat)
	var score int
	var detail string

	switch {
	case totalLines <= 50:
		score = 100
		detail = fmt.Sprintf(`Small change: %d source lines`, totalLines)
	case totalLines <= 200:
		score = 80
		detail = fmt.Sprintf(`Medium change: %d source lines`, totalLines)
	case totalLines <= 500:
		score = 60
		detail = fmt.Sprintf(`Large change: %d source lines`, totalLines)
	default:
		score = 40
		detail = fmt.Sprintf(`Very large change: %d source lines`, totalLines)
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
			Detail:    `Time data unavailable`,
		}
	}

	duration := completedAt.Sub(startedAt)

	// Negative duration (clock skew / CompletedAt written before StartedAt / tampered TaskState)
	// is untrustworthy data, not an instant completion — treat it like missing data (neutral 70).
	// Otherwise completedAt < startedAt would hit the minutes<=15 bucket and hand out a free 100.
	//
	// 负 duration（时钟回拨 / CompletedAt 先于 StartedAt 写入 / TaskState 被改）是不可信数据，
	// 不是"秒完成"——按数据不可用处理（中性 70）。否则 completedAt < startedAt 会命中
	// minutes<=15 桶白拿 100，成为零成本刷分向量。
	if duration < 0 {
		return scoringtypes.DimensionScore{
			Dimension: scoringtypes.DimensionEfficiency,
			Score:     70,
			Detail:    fmt.Sprintf(`Time data untrustworthy (negative duration %.0f minutes)`, duration.Minutes()),
		}
	}

	minutes := duration.Minutes()

	// Threshold recalibration (dogfood measured the old thresholds as out of touch with reality: <=5min=100/<=60=60/>60=40 pressed 80% of real AI tasks
	// into the 40-60 range, the dimension had no discrimination). Real forge tasks median ~30-90min, the new thresholds give the common range a gradient:
	// <=15=100 (fast)/<=30=90 (agile)/<=60=75 (normal)/<=120=55 (slow)/>120=35 (sluggish).
	//
	// 阈值重校准（dogfood 实测旧阈值脱离实际：≤5min=100/≤60=60/>60=40 把 80% 真实 AI 任务
	// 压在 40-60，维度无区分度）。真实 forge 任务中位 ~30-90min，新阈值让常见区间有梯度：
	// ≤15=100（快速）/≤30=90（敏捷）/≤60=75（正常）/≤120=55（偏慢）/>120=35（拖沓）。
	var score int
	switch {
	case minutes <= 15:
		score = 100
	case minutes <= 30:
		score = 90
	case minutes <= 60:
		score = 75
	case minutes <= 120:
		score = 55
	default:
		score = 35
	}

	return scoringtypes.DimensionScore{
		Dimension: scoringtypes.DimensionEfficiency,
		Score:     score,
		Detail:    fmt.Sprintf(`Completed in %.0f minutes`, minutes),
	}
}

// --- helper functions ---
//
// --- 辅助函数 ---

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

// parseDiffStatLines sums the total changed lines from `git diff --numstat` output,
// excluding test files and non-source files.
//
// The old implementation summed all numstat lines — test files, docs, and config lines all counted into scope, causing writing tests to be
// reversely penalized (a hazard task had 523 lines about half of which were tests -> scope=40, pressing A-grade work down to C). Industry
// consensus (SLOCcount excludes generated code by default, SLOC excludes comments and blank lines): scale metrics should only count hand-written production source code.
// So test files and non-source extensions are not counted into scope — writing tests changes from a reverse incentive to neutral.
//
// numstat format: <added>\t<deleted>\t<path> (binary files show -\t-\t<path>).
//
// parseDiffStatLines 从 `git diff --numstat` 输出求和总变更行数，
// 排除测试文件和非源码文件。
//
// 旧实现对所有 numstat 行求和——测试文件、文档、配置的行都算进 scope，导致"写测试"被
// 反向惩罚（一次 hazard 任务 523 行约一半是测试 → scope=40，把 A 级工作压到 C）。业界
// 共识（SLOCcount 默认排除生成代码、SLOC 排除注释空行）：规模度量应只数"人写的产品源码"。
// 故测试文件和非源码后缀不计入 scope——写测试从反向激励变为中性。
//
// numstat 格式："<added>\t<deleted>\t<path>"（binary 文件显示"-\t-\t<path>"）。
func parseDiffStatLines(stat string) int {
	total := 0
	for _, line := range splitLines(stat) {
		added, deleted, path, ok := parseNumstatLine(line)
		if !ok {
			continue
		}
		if !countsAsScope(path) {
			continue
		}
		total += added + deleted
	}
	return total
}

// scopeSourceExts is the set of source extensions recognized by the scope dimension (same source as taskpipeline.testcoverage.sourceExts,
// not importing that package to avoid a circular dependency). Files not in this set (docs/config/generated) are not counted into scope.
//
// scopeSourceExts 是 scope 维度认定的源码后缀（与 taskpipeline.testcoverage.sourceExts
// 同源，不 import 该包以避免循环依赖）。非此集合的文件（文档/配置/生成物）不计入 scope。
var scopeSourceExts = map[string]bool{
	`.go`: true, `.rs`: true, `.ts`: true, `.tsx`: true,
	`.js`: true, `.jsx`: true, `.py`: true, `.java`: true,
	`.rb`: true, `.zig`: true, `.nim`: true,
}

// countsAsScope reports whether path counts into the scope dimension: must be a source file and not a test file.
// Test files are excluded, so writing tests is not reversely penalized as a large scope.
//
// countsAsScope 报告 path 是否计入 scope 维度：必须是源码文件且非测试文件。
// 测试文件被排除，写测试不会被反向惩罚为"large scope"。
func countsAsScope(path string) bool {
	if isTestPath(path) {
		return false
	}
	return scopeSourceExts[filepath.Ext(path)]
}

// isTestPath reports whether path looks like a test file (same heuristic as taskpipeline.isTestFile).
// Used to exclude test files in the scope dimension.
//
// isTestPath 报告 path 是否疑似测试文件（与 taskpipeline.isTestFile 同启发式）。
// 用于在 scope 维度排除测试文件。
func isTestPath(path string) bool {
	for _, pat := range []string{`_test.`, `_spec.`, `.test.`, `.spec.`, `test/`, `tests/`, `__tests__/`} {
		if strings.Contains(path, pat) {
			return true
		}
	}
	return false
}

// parseNumstatLine parses a single line of the form added\tdeleted\tpath and also returns path so the caller
// can filter (e.g. exclude test files from scope). Binary entries (-\t-\t...) return ok=false.
// Empty lines or format errors return ok=false.
//
// parseNumstatLine 解析单行"added\tdeleted\tpath"并同时返回 path 以便调用方
// 过滤（如把测试文件排除出 scope）。binary 条目（"-\t-\t..."）返 ok=false。
// 空行或格式错误返 ok=false。
func parseNumstatLine(line string) (added, deleted int, path string, ok bool) {
	tab1 := strings.IndexByte(line, '\t')
	if tab1 < 0 {
		return 0, 0, ``, false
	}
	addedField := line[:tab1]
	rest := line[tab1+1:]
	tab2 := strings.IndexByte(rest, '\t')
	if tab2 < 0 {
		return 0, 0, ``, false
	}
	deletedField := rest[:tab2]
	path = rest[tab2+1:]
	if addedField == "-" || deletedField == "-" {
		// binary file — no line count statistics
		return 0, 0, path, false // binary 文件——无行数统计
	}
	a, errA := strconv.Atoi(addedField)
	d, errD := strconv.Atoi(deletedField)
	if errA != nil || errD != nil {
		return 0, 0, path, false
	}
	return a, d, path, true
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

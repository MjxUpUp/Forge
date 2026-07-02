package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

func init() {
	verifyCmd.Flags().String("collect-golden", "", "从已完成任务采集真实 golden case 到 testdata/golden_real/（开发工具：固化真实评分形状进 CI 回归）")
}

// buildEvaluateInput 从 TaskState + checklog + git 构造评分输入。scoreTask（评分落盘）
// 与 CollectGoldenFromTask（采集真实 golden fixture）共用此函数——单一真相源，避免两份
// 组装逻辑漂移。
//
// 已知限制：GitDiffStat/TestAssertionCount/TestCoverage 依赖 state.HeadCommit 的 git 状态。
// 任务刚完成时（HEAD≈HeadCommit）精确；事后 HEAD 推进会让 git diff 含后续改动而漂移
// （scope 维度受影响最大）。故 golden 采集应在任务完成那刻或紧随其后，而非任意时刻。
func buildEvaluateInput(root string, state *taskpipeline.TaskState) (*scoring.EvaluateInput, *scoringtypes.ScoringConfig, error) {
	// Collect git data (non-fatal on failure)
	gitDiffStat, _ := scoring.CollectGitData(root, state.Branch, state.HeadCommit)

	// Determine hook results from gate history and check log.
	compilePassed := false
	compileChecked := false
	assertionPassed := false
	assertionChecked := false
	for _, r := range state.History {
		if r.Gate == "task-implement" {
			compileChecked = true
			compilePassed = r.Passed
		}
	}

	// Read check log for actual hook results (more reliable than gate history).
	// Scope by session so a concurrent session's check results don't contaminate
	// this task's scoring.
	// covered/total/passed 始终来自实时 CheckTestCoverage（客观，与门禁同输入同逻辑 → 必
	// 一致）。旧路径只从 checklog 读二值 passed，无法支撑 testing 维度的连续打分；实时算
	// 既准确又与门禁 verdict 一致（同 CheckTestCoverage 逻辑、同 task diff）。
	tcOK, tcMissing, tcTotal := taskpipeline.CheckTestCoverage(root, state)
	tcCovered := tcTotal - len(tcMissing)
	testCoveragePassed := tcOK
	// checked：门禁是否跑过（checklog 有 test-coverage-gate 条目）。无条目 → fallback 视为
	// checked（实时已算 covered/total/passed，评分仍可信）。
	testCoverageChecked := false
	if latestChecks, err := checklog.LatestByCheckForSession(root, state.SessionID); err == nil {
		if entry, ok := latestChecks[checklog.CheckAssertion]; ok {
			assertionChecked = entry.Checked
			assertionPassed = entry.Passed
		}
		if entry, ok := latestChecks[checklog.CheckAutoCompile]; ok {
			compileChecked = entry.Checked
			compilePassed = entry.Passed
		}
		if entry, ok := latestChecks[taskpipeline.CheckNameTestCoverage]; ok {
			testCoverageChecked = entry.Checked
		}
	}
	if !testCoverageChecked {
		testCoverageChecked = true
	}

	// Count retries: gates that appear multiple times with mixed results
	retries := 0
	gateAttempts := make(map[string][]bool)
	for _, r := range state.History {
		gateAttempts[r.Gate] = append(gateAttempts[r.Gate], r.Passed)
	}
	for _, attempts := range gateAttempts {
		hasFailure := false
		for _, passed := range attempts {
			if !passed {
				hasFailure = true
			}
		}
		if hasFailure && len(attempts) > 1 {
			retries++
		}
	}

	// Load scoring config from protocol
	var config *scoringtypes.ScoringConfig
	proto, err := protocol.Load(root)
	if err != nil || proto == nil || proto.Scoring == nil {
		config = &scoringtypes.ScoringConfig{
			Weights:    scoringtypes.DefaultWeights(),
			Thresholds: scoringtypes.DefaultThresholds(),
		}
	} else {
		config = proto.Scoring
	}

	completedAt := time.Now()
	if state.CompletedAt != nil {
		completedAt = *state.CompletedAt
	}

	// 断言密度（C）：统计本任务 changed 测试文件的断言数，供 testing 维度假测试检测。
	testAssertionCount, testFileCount := scoring.CollectAssertionDensity(root, state.Branch, state.HeadCommit)

	// 证据链来源分布（路线 Step 2）：从 checklog 聚合 deterministic/agent-claim，
	// 供 ScoreResult.Evidence 可观测（不参与打分）。ForTask 与 forge trace 同源。
	evDeterministic, evAgentClaim := 0, 0
	if ec, err := checklog.ForTask(root, state.TaskRef); err == nil {
		evDeterministic = ec.Deterministic
		evAgentClaim = ec.AgentClaim
	}

	input := &scoring.EvaluateInput{
		GateHistory: scoring.GateHistory{
			TotalGates: len(taskpipeline.DefaultGates()),
			Passed:     len(state.CompletedGates()),
			Retries:    retries,
		},
		StartedAt:             state.StartedAt,
		CompletedAt:           completedAt,
		GitDiffStat:           gitDiffStat,
		TestCoveragePassed:    testCoveragePassed,
		TestCoverageChecked:   testCoverageChecked,
		TestCoverageCovered:   tcCovered,
		TestCoverageTotal:     tcTotal,
		TestAssertionCount:    testAssertionCount,
		TestFileCount:         testFileCount,
		CompilePassed:         compilePassed,
		CompileChecked:        compileChecked,
		AssertionPassed:       assertionPassed,
		AssertionChecked:      assertionChecked,
		EvidenceDeterministic: evDeterministic,
		EvidenceAgentClaim:    evAgentClaim,
	}
	return input, config, nil
}

// CollectGoldenFromTask 从已完成任务的 TaskState 派生一个 golden case（真实评分形状），
// 供 forge verify --collect-golden 沉淀到 testdata/golden_real/ 做 CI 回归。
// 复用 buildEvaluateInput（与评分同输入同逻辑）→ GoldenCaseFromInput 算 Expected。
// 任务须已评分（state.Score != nil）。git 漂移限制见 buildEvaluateInput 注释。
func CollectGoldenFromTask(root, taskRef string) (*scoring.GoldenCase, error) {
	state, err := taskpipeline.LoadTaskState(root, taskRef)
	if err != nil {
		return nil, fmt.Errorf("load task %s: %w", taskRef, err)
	}
	if state.Score == nil {
		return nil, fmt.Errorf("task %s not scored — complete it first", taskRef)
	}
	input, config, err := buildEvaluateInput(root, state)
	if err != nil {
		return nil, err
	}
	name := goldenCaseName(taskRef)
	rationale := fmt.Sprintf(`真实 dogfood 任务 %s 的评分形状（采集自 TaskState+checklog+git）。钉真实组合不漂移——scoring 算法改动若让此任务评分漂移即 CI 挂。`, taskRef)
	gc := scoring.GoldenCaseFromInput(name, rationale, input, config)
	// Meta：自动采集来源 + git 漂移检测。任务完成后 HEAD 若已推进 → GitDiffStat 含事后
	// 改动 → scope 维度基于漂移 diff（固化后稳定但不真实）。检测到即标 drift_known，
	// CI 对 scope advisory 不 fail，其余维度照常断言。
	gc.Meta = scoring.GoldenMeta{Source: `auto-collected`}
	if taskpipeline.GetHeadCommit(root) != state.HeadCommit {
		gc.Meta.DriftKnown = []string{`scope`}
	}
	return gc, nil
}

// goldenCaseName 把 taskRef（如 feat/review-snapshot）转为 fixture 名片段（feat-review-snapshot）。
func goldenCaseName(taskRef string) string {
	return strings.ReplaceAll(taskRef, "/", "-")
}

// runCollectGoldenMode 是 forge verify --collect-golden <task-ref> 的入口。
// 写到 forge 仓库的 scoring testdata（开发者工具语义：采集进 CI golden 集）。
func runCollectGoldenMode(taskRef string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	gc, err := CollectGoldenFromTask(root, taskRef)
	if err != nil {
		return err
	}
	dir := filepath.Join(findRepoRoot(), "internal", "scoring", "testdata", "golden_real")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir golden_real: %w", err)
	}
	data, err := json.MarshalIndent(gc, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "golden_real_"+gc.Name+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("采集 golden case: %s\n", gc.Name)
	fmt.Printf("  overall %.2f (%s)\n", gc.Expected.Overall, gc.Expected.Grade)
	fmt.Printf("  → %s\n", path)
	fmt.Println("  提醒：若任务完成后 HEAD 已推进，scope/GitDiffStat 维度会漂移。最准的采集是任务完成那刻。")
	return nil
}

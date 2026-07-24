package taskpipeline

import (
	"fmt"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// BuildEvaluateInput 从 TaskState + checklog + git 构造评分输入。ScoreTask（评分落盘）与
// CLI CollectGoldenFromTask（采集真实 golden fixture）共用此函数——单一真相源，避免两份
// 组装逻辑漂移。从 cli/task_golden.go 下沉到 taskpipeline，让 MCP（forge_task_complete）与
// CLI 共用同一评分路径——proof-of-work 闭环的 complete 终点对 agent 可编程。
//
// 已知限制：GitDiffStat/TestAssertionCount/TestCoverage 依赖 state.HeadCommit 的 git 状态。
// 任务刚完成时（HEAD≈HeadCommit）精确；事后 HEAD 推进会让 git diff 含后续改动而漂移
// （scope 维度受影响最大）。故 golden 采集应在任务完成那刻或紧随其后。
func BuildEvaluateInput(root string, state *TaskState) (*scoring.EvaluateInput, *scoringtypes.ScoringConfig, error) {
	// Collect git data (non-fatal on failure)
	gitDiffStat, _ := scoring.CollectGitData(root, state.Branch, state.HeadCommit)

	// Determine hook results from gate history and check log.
	compilePassed := false
	compileChecked := false
	assertionPassed := false
	assertionChecked := false
	for _, r := range state.History {
		if r.Gate == `task-implement` {
			compileChecked = true
			compilePassed = r.Passed
		}
	}

	// covered/total/passed 始终来自实时 CheckTestCoverage（客观，与门禁同输入同逻辑 → 必
	// 一致）。旧路径只从 checklog 读二值 passed，无法支撑 testing 维度的连续打分；实时算
	// 既准确又与门禁 verdict 一致（同 CheckTestCoverage 逻辑、同 task diff）。
	tcOK, tcMissing, tcTotal := CheckTestCoverage(root, state)
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
		if entry, ok := latestChecks[CheckNameTestCoverage]; ok {
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

	// 证据链来源分布：从 checklog 聚合 deterministic/agent-claim，供 ScoreResult.Evidence
	// 可观测（不参与打分）。ForTask 与 forge trace 同源。
	evDeterministic, evAgentClaim := 0, 0
	if ec, err := checklog.ForTask(root, state.TaskRef); err == nil {
		evDeterministic = ec.Deterministic
		evAgentClaim = ec.AgentClaim
	}

	input := &scoring.EvaluateInput{
		GateHistory: scoring.GateHistory{
			TotalGates: len(DefaultGates()),
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

// ScoreTask 评分并落盘。已评分则 no-op。proof-of-work 闭环：complete 的终点产出 Score，
// 喂给 act/health/dashboard。从 cli/task.go 下沉，MCP complete 与 CLI 共用同一评分路径。
func ScoreTask(root string, state *TaskState) error {
	if state.Score != nil {
		return nil // already scored
	}

	input, config, err := BuildEvaluateInput(root, state)
	if err != nil {
		return fmt.Errorf(`build evaluate input: %w`, err)
	}

	result := scoring.Evaluate(input, config)
	result.TaskRef = state.TaskRef

	state.Score = result
	return SaveTaskState(root, state)
}

// AppendConclusion 构建 + 落盘一个完成任务的 Act 结论（证据驱动），返回 (conclusion, directive, err)：
// directive 空=无 RetrospectiveNudge；err 非 nil=project 解析或 act append 失败（调用方决定
// stderr/忽略——CLI 打 warning，MCP 塞进 Message）。聚合 checklog.ForTask 证据链 +
// state.Acceptance 通过率 + state.Score，调 act.BuildConclusion。从 cli/task.go 下沉，
// 让 MCP complete 与 CLI 共用 Act 反馈臂。
func AppendConclusion(root string, state *TaskState) (act.Conclusion, string, error) {
	ec, _ := checklog.ForTask(root, state.TaskRef)
	pass, total := 0, len(state.Acceptance)
	for _, c := range state.Acceptance {
		if c.Passed {
			pass++
		}
	}
	completedAt := time.Now()
	if state.CompletedAt != nil {
		completedAt = *state.CompletedAt
	}
	conc := act.BuildConclusion(state.TaskRef, state.SessionID, state.Score, ec, pass, total, completedAt, PhaseKeys(state.DesignPhases))
	directive := conc.Directive()
	proj, perr := forgedata.ProjectFor(root)
	if perr != nil {
		return conc, directive, fmt.Errorf(`act conclusion append skipped (project not resolved): %w`, perr)
	}
	if err := act.Append(proj, &conc); err != nil {
		return conc, directive, fmt.Errorf(`act conclusion append failed: %w`, err)
	}
	return conc, directive, nil
}

// PhaseKeys 把 DesignPhase slice 转 string slice（act.BuildConclusion 入参）。从 cli/task.go 下沉。
func PhaseKeys(phases []DesignPhase) []string {
	if len(phases) == 0 {
		return nil
	}
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = string(p)
	}
	return out
}

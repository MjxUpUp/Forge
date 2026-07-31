package taskpipeline

import (
	"fmt"
	"os"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// BuildEvaluateInput builds the scoring input from TaskState + checklog + git. ScoreTask (score
// persistence) and CLI CollectGoldenFromTask (collecting real golden fixtures) share this
// function—single source of truth, avoiding drift between two assembly logics. Sunk from
// cli/task_golden.go into taskpipeline so that MCP (forge_task_complete) and CLI share the same
// scoring path—the complete endpoint of the proof-of-work loop is programmable for agents.
//
// Known limitation: GitDiffStat/TestAssertionCount/TestCoverage depend on the git state of
// state.HeadCommit. Accurate right when the task completes (HEAD≈HeadCommit); a later HEAD advance
// lets subsequent changes leak into git diff and drift (scope dimension is most affected). Hence
// golden collection should happen at—or right after—task completion.
//
// BuildEvaluateInput 从 TaskState + checklog + git 构造评分输入。ScoreTask（评分落盘）与
// CLI CollectGoldenFromTask（采集真实 golden fixture）共用此函数——单一真相源，避免两份
// 组装逻辑漂移。从 cli/task_golden.go 下沉到 taskpipeline，让 MCP（forge_task_complete）与
// CLI 共用同一评分路径——proof-of-work 闭环的 complete 终点对 agent 可编程。
//
// 已知限制：GitDiffStat/TestAssertionCount/TestCoverage 依赖 state.HeadCommit 的 git 状态。
// 任务刚完成时（HEAD≈HeadCommit）精确；事后 HEAD 推进会让 git diff 含后续改动而漂移
// （scope 维度受影响最大）。故 golden 采集应在任务完成那刻或紧随其后。
func BuildEvaluateInput(root string, state *TaskState) (*scoring.EvaluateInput, *scoringtypes.ScoringConfig, error) {
	// Collect git data (failure is non-fatal: empty diffStat degrades the scope dimension to
	// neutral 70, but the error is surfaced on stderr instead of being silently swallowed).
	//
	// 采集 git 数据（失败不致命：空 diffStat 让 scope 维度走中性 70，但错误打到 stderr，
	// 不再静默吞掉）。
	gitDiffStat, gitErr := scoring.CollectGitData(root, state.Branch, state.HeadCommit)
	if gitErr != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: git diff stat unavailable (%v) — scope dimension scores neutral\n", gitErr)
	}

	// Infer hook results from gate history and check log.
	//
	// 从 gate history 与 check log 推断 hook 结果。
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

	// covered/total/passed always come from a live CheckTestCoverage (objective; same input and
	// logic as the gate → must agree). The old path only read a binary passed from checklog, which
	// cannot support continuous scoring for the testing dimension; computing live is both accurate
	// and consistent with the gate verdict (same CheckTestCoverage logic, same task diff).
	//
	// covered/total/passed 始终来自实时 CheckTestCoverage（客观，与门禁同输入同逻辑 → 必
	// 一致）。旧路径只从 checklog 读二值 passed，无法支撑 testing 维度的连续打分；实时算
	// 既准确又与门禁 verdict 一致（同 CheckTestCoverage 逻辑、同 task diff）。
	tcOK, tcMissing, tcTotal := CheckTestCoverage(root, state)
	tcCovered := tcTotal - len(tcMissing)
	testCoveragePassed := tcOK
	// checked: always true. covered/total/passed are computed live above (same
	// CheckTestCoverage logic and task diff as the gate), so the testing dimension does
	// not depend on a checklog entry being present — an earlier version looked up the
	// CheckNameTestCoverage entry here, but the result was unconditionally overwritten
	// to true right after, making the lookup a dead effect.
	//
	// checked：恒 true。covered/total/passed 上面实时算（与门禁同 CheckTestCoverage
	// 逻辑、同 task diff），testing 维度不依赖 checklog 条目是否存在——此前此处会查
	// CheckNameTestCoverage 条目，但紧接着被无条件覆写为 true，查询是死效果。
	testCoverageChecked := true
	if latestChecks, err := checklog.LatestByCheckForSession(root, state.SessionID); err == nil {
		if entry, ok := latestChecks[checklog.CheckAssertion]; ok {
			assertionChecked = entry.Checked
			assertionPassed = entry.Passed
		}
		if entry, ok := latestChecks[checklog.CheckAutoCompile]; ok {
			compileChecked = entry.Checked
			compilePassed = entry.Passed
		}
	}

	// Count retries: gates that appear multiple times with mixed outcomes.
	//
	// 统计 retry：多次出现且结果混合的 gate
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

	// Load scoring config from protocol.
	//
	// 从 protocol 加载 scoring 配置
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

	// Assertion density (C): count assertions in this task's changed test files, feeding fake-test
	// detection for the testing dimension.
	//
	// 断言密度（C）：统计本任务 changed 测试文件的断言数，供 testing 维度假测试检测。
	testAssertionCount, testFileCount := scoring.CollectAssertionDensity(root, state.Branch, state.HeadCommit)

	// Evidence-chain source breakdown: aggregate deterministic/agent-claim from checklog for
	// ScoreResult.Evidence observability (not part of scoring). ForTask shares the source with
	// forge trace.
	//
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

// ScoreTask scores and persists. No-op if already scored. Proof-of-work loop: the complete
// endpoint produces the Score that feeds act/health/dashboard. Sunk from cli/task.go; MCP
// complete and CLI share the same scoring path.
//
// ScoreTask 评分并落盘。已评分则 no-op。proof-of-work 闭环：complete 的终点产出 Score，
// 喂给 act/health/dashboard。从 cli/task.go 下沉，MCP complete 与 CLI 共用同一评分路径。
func ScoreTask(root string, state *TaskState) error {
	if state.Score != nil {
		return nil // 已评分
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

// AppendConclusion builds + persists an Act conclusion for a completed task (evidence-driven),
// returning (conclusion, directive, err): empty directive = no RetrospectiveNudge; non-nil err =
// project resolution or act append failure (caller decides stderr/ignore—CLI warns, MCP stuffs
// into Message). Aggregates the checklog.ForTask evidence chain + state.Acceptance pass rate +
// state.Score, then calls act.BuildConclusion. Sunk from cli/task.go so MCP complete and CLI
// share the Act feedback arm.
//
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
	conc := act.BuildConclusion(state.TaskRef, state.SessionID, state.Score, ec, pass, total, completedAt, phaseKeys(state.DesignPhases))
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

// phaseKeys converts a DesignPhase slice to a string slice (input for act.BuildConclusion). Sunk
// from cli/task.go.
//
// phaseKeys 把 DesignPhase slice 转 string slice（act.BuildConclusion 入参）。从 cli/task.go 下沉。
func phaseKeys(phases []DesignPhase) []string {
	if len(phases) == 0 {
		return nil
	}
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = string(p)
	}
	return out
}

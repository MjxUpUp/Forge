package taskpipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/doclint"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// BuildEvaluateInput builds the scoring input from TaskState + checklog + git.
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
	// 采集 git 数据（失败不致命：空 diffStat 让 scope 维度走中性 70，但错误打到 stderr，
	// 不再静默吞掉）。
	gitDiffStat, gitErr := scoring.CollectGitData(root, state.Branch, state.HeadCommit)
	if gitErr != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: git diff stat unavailable (%v) — scope dimension scores neutral\n", gitErr)
	}

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

	// covered/total/passed 始终来自实时 CheckTestCoverage（客观，与门禁同输入同逻辑 → 必
	// 一致）。旧路径只从 checklog 读二值 passed，无法支撑 testing 维度的连续打分；实时算
	// 既准确又与门禁 verdict 一致（同 CheckTestCoverage 逻辑、同 task diff）。
	tcOK, tcMissing, tcTotal := CheckTestCoverage(root, state)
	tcCovered := tcTotal - len(tcMissing)
	testCoveragePassed := tcOK
	// checked：恒 true。covered/total/passed 上面实时算（与门禁同 CheckTestCoverage
	// 逻辑、同 task diff），testing 维度不依赖 checklog 条目是否存在——此前此处会查
	// CheckNameTestCoverage 条目，但紧接着被无条件覆写为 true，查询是死效果。
	testCoverageChecked := true
	if escapeDisabled(state, "test-coverage", "FORGE_TEST_COVERAGE") {
		// override 免的是【门禁】，不是报告的诚实性：覆盖义务只是被绕过的任务
		// 报 100/「无需测试」会把缺口洗成满分维度（2026-08-29 功能发现）。维度
		// 改为中性（70，「未检测——已禁用」）；逃生的真实代价已由 89 封顶 +
		// Weak 证据体现。
		testCoverageChecked = false
	}
	if latestChecks, err := checklog.LatestByCheckForSessionSince(root, state.SessionID, state.StartedAt); err == nil {
		// INFRA:-prefixed entries are fail-open infrastructure failures (bash spawn
		// error / WSL exit 126/127), not quality verdicts — the gate treats them as
		// fail-open, so scoring must not read them as compile/assertion failures
		// (code-review 2026-08: gate放行与score扣分互相矛盾).
		//
		// INFRA: 前缀条目是 fail-open 基建故障（bash spawn 错误 / WSL exit 126/127），
		// 不是质量结论——gate 按 fail-open 放行，scoring 也不能把它们读成编译/断言
		// 失败（code-review 2026-08：gate 放行与 score 扣分互相矛盾）。
		if entry, ok := latestChecks[checklog.CheckAssertion]; ok && !strings.HasPrefix(entry.Detail, "INFRA: ") {
			assertionChecked = entry.Checked
			assertionPassed = entry.Passed
		}
		if entry, ok := latestChecks[checklog.CheckAutoCompile]; ok && !strings.HasPrefix(entry.Detail, "INFRA: ") {
			compileChecked = entry.Checked
			compilePassed = entry.Passed
		}
	}

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

	// 从 protocol 加载 scoring 配置
	var config *scoringtypes.ScoringConfig
	proto, err := protocol.Load(root)
	if err != nil {
		// 解析失败（用户手误/类型错）静默换默认权重会无痕改变分数——告警；
		// 无 Scoring 段保持安静缺省。
		fmt.Fprintf(os.Stderr, "[forge] warning: protocol.yml 加载失败（%v）——评分回退默认权重/阈值\n", err)
		config = &scoringtypes.ScoringConfig{
			Weights:    scoringtypes.DefaultWeights(),
			Thresholds: scoringtypes.DefaultThresholds(),
		}
	} else if proto == nil || proto.Scoring == nil {
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

	// 表达维度输入（输出→回检循环度量锚点）：评分时实时重算——与 doc gate 同
	// doclint + changedMarkdownDocs 逻辑，维度与门禁结论不会不一致。
	docDeliverables := changedMarkdownDocs(root, state)
	docLintHard := 0
	for _, d := range docDeliverables {
		issues, lerr := doclint.LintFile(filepath.Join(root, filepath.FromSlash(d)))
		if lerr != nil {
			continue
		}
		for _, iss := range issues {
			if iss.Hard() {
				docLintHard++
			}
		}
	}
	var docRubric *int
	if state.DocReview != nil && !state.DocReview.ReviewedAt.IsZero() {
		s := state.DocReview.RubricScore
		docRubric = &s
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

		HasDocDeliverables: len(docDeliverables) > 0,
		DocLintHardIssues:  docLintHard,
		DocRubricScore:     docRubric,
		DocGateEscaped:     escapeDisabled(state, escapeDocGate, docGateDisableEnv),
	}
	return input, config, nil
}

// escapeCapMaxScore 是用了逃生舱 override 的任务总分上限（见 ScoreTask）。取 B 档
// 上限 89：逃生任务拿不到 A，但合理的单次逃生（如死代码删除豁免测试）不被压到 C。
const escapeCapMaxScore = 89

// ScoreTask scores and persists.
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

	// 挂上审查-返工循环度量（仅可观测，非评分维度）——在 ScoreTask 而非
	// scoring.Evaluate 填充，因为原料（ReviewRounds + gate History）在 TaskState
	// 上，且 CLI 与 MCP complete 共用本路径。
	if passes, rejections := state.ReworkRounds(); passes > 0 || rejections > 0 {
		if result.Evidence == nil {
			result.Evidence = &scoringtypes.EvidenceSummary{}
		}
		result.Evidence.ReviewPasses = passes
		result.Evidence.CompleteRejections = rejections
	}

	// 逃生舱代价：用过任一逃生舱的任务总分封顶 escapeCapMaxScore，并按封顶值重算
	// grade——Weak 证据不能照拿 96-99/A。两个互补信号（code-review 2026-08）：
	//  - state.Overrides：设了 per-task override 但没走 bypass 分支的任务 checklog
	//    无条目，但逃生意图已留痕；
	//  - checklog CheckEscapeHatch 条目：env 形式逃生（FORGE_TEST_COVERAGE=disable
	//    等）经 escapeDisabled 绕过、不动 state.Overrides，但 bypass 分支必然记录
	//    逃生舱条目。只看 Overrides 会让 env 逃生照拿 A。
	// CappedReason 持久化原因，让 trace/dashboard 能显示 89 分从何来；
	// Conclusion.Score/Grade 抄这些值（act.BuildConclusion）自动一致。
	// 信任边界（接受）：DataDir 的 TaskState JSON 是 agent 可写的——手删 Overrides
	// 可躲信号 1，与现有快照基线同级。
	//
	// 与 evidence Strength cap 的独立性（2026-08）：本分数封顶对任一逃生（验证类
	// 或节奏类）生效，不携带 checklog.EscapeDowngradedStrength 的证据缩放豁免——
	// 有意为之。两道正交代价：分数封顶是逃生「价格」（分数层，设计上平价），
	// Strength cap 是证据「保真」（信号层，2026-08 起按证据份量缩放）。重证据的
	// 边际逃生保持 Strength=Strong（其实跑证据是真的），但分数仍 ≤89（绕过确实
	// 发生了）。两个信号都不说谎：证据确实强、gate 确实被跳过。
	escaped := usedAnyOverride(state.Overrides)
	if !escaped {
		if recorded, err := taskEscapeHatchRecorded(root, state.TaskRef); err == nil {
			escaped = recorded
		}
	}
	if escaped && result.Overall > escapeCapMaxScore {
		result.Overall = escapeCapMaxScore
		result.Grade = scoringtypes.GradeFromScore(result.Overall, config.Thresholds)
		result.CappedReason = "escape-hatch used (forge task override or env-form escape) — total score capped"
		fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[score] 本任务用过逃生舱（override 或 env 豁免）——总分封顶 %.0f（Grade=%s），逃生有代价", result.Overall, result.Grade))
	}

	state.Score = result
	return SaveTaskState(root, state)
}

// AppendConclusion builds + persists an Act conclusion for a completed task
// (evidence-driven), returning (conclusion, directive, err): empty directive =
// no RetrospectiveNudge; non-nil err = project resolution or act append failure
// (caller decides stderr/ignore.
//
// AppendConclusion 构建 + 落盘一个完成任务的 Act 结论（证据驱动），返回 (conclusion, directive, err)：
// directive 空=无 RetrospectiveNudge；err 非 nil=project 解析或 act append 失败（调用方决定
// stderr/忽略——CLI 打 warning，MCP 塞进 Message）。聚合 checklog.ForTask 证据链 +
// state.Acceptance 通过率 + state.Score，调 act.BuildConclusion。从 cli/task.go 下沉，
// 让 MCP complete 与 CLI 共用 Act 反馈臂。
func AppendConclusion(root string, state *TaskState) (act.Conclusion, string, error) {
	ec, err := checklog.ForTask(root, state.TaskRef)
	if err != nil {
		// An unreadable evidence chain would otherwise be silently persisted as a
		// "zero-evidence completion" — warn so the IO failure stays attributable.
		fmt.Fprintf(os.Stderr, "[forge] warning: evidence chain unavailable for %s: %v\n", state.TaskRef, err)
	}
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

package taskpipeline

// selfcheck.go — discipline-first-gates P2/P3：selfcheck 镜像命令的计算与查询层、
// 产物链的开工前预期探针。`forge selfcheck pairing|scope`（internal/cli/selfcheck.go）
// 对当前任务的改动集跑与 verify 门禁**同一代码路径**的纯计算——agent 能在任何时刻
// 秒级自检，落痕的 selfcheck 条目把后续失败门禁的 outcome 从 discovery（第一防线
// 缺位）升为 confirmation（已知未行动）。守纪律由此成为摩擦最小的路径。

import (
	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// SelfcheckPairing computes the unpaired-source list for the task's current
// change set — the same taskChangedFiles + coveragePairing pass the verify
// gate runs, WITHOUT the escape early-out: a self-check under an active escape
// still deserves facts (the escape silences the gate, not the truth).
//
// SelfcheckPairing 对任务当前改动集计算未配对源码清单——与 verify 门禁相同的
// taskChangedFiles + coveragePairing pass，但**不做**逃生早退：逃生激活下的
// 自检也要事实（逃生静音的是门禁，不是真相）。
func SelfcheckPairing(root string, state *TaskState) (missing []string, total int) {
	return coveragePairing(taskChangedFiles(root, state))
}

// SelfcheckScope computes the PlanScope drift for the task's current change
// set. An undeclared PlanScope (empty) is a no-op probe — nothing to mirror.
//
// SelfcheckScope 对任务当前改动集计算 PlanScope 漂移。PlanScope 未声明（空）时
// 探针空转——没有可镜像的声明。
func SelfcheckScope(root string, state *TaskState) (drift []string) {
	if len(state.PlanScope) == 0 {
		return nil
	}
	return ScopeDrift(taskChangedFiles(root, state), state.PlanScope)
}

// selfcheckRanForTask 报告 task 内是否跑过 pairing 自检（任意结果——跑过即持有
// 事实）。confirmation 判定的第二输入（第一是已送达 test-nudge）：
// 自检发现 missing 而未修、或自检后代码再漂移导致 verify 失败，都是纪律债
// 兑现的如实形态。读失败按未跑处理（保守方向：不虚增 confirmation）。
func selfcheckRanForTask(root, taskRef string) bool {
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Check == checklog.CheckSelfcheckPairing {
			return true
		}
	}
	return false
}

// TestCoverageEscapeActive reports whether the task's test-coverage escape is
// active (per-task override or env). Consumed by `forge selfcheck` for its
// mirror-honesty duty: under escape the GATE waives the check while selfcheck
// still reports facts — the command must label that divergence, or it teaches
// "escape = all clean" backwards.
//
// TestCoverageEscapeActive 报告任务的 test-coverage 逃生是否激活（per-task
// override 或 env）。`forge selfcheck` 消费它履行镜像诚实义务：逃生下**门禁**豁免
// 本检查而 selfcheck 照报事实——命令必须标注这层口径差，否则反向教出
// 「逃生=全干净」。
func TestCoverageEscapeActive(state *TaskState) bool {
	if state == nil {
		return false
	}
	return escapeDisabled(state, escapeTestCoverage, testCoverageDisableEnv)
}

// ArtifactChainExpectation returns the task's expected-but-unregistered artifact
// nodes and the next-node hint (P3): the START-TIME probe feeding `forge task
// start`'s heads-up — producing artifacts before code is the cheapest point to
// catch direction errors. Same truth source as the implement gate's
// CheckArtifactChainGate (artifactchain.Load); blocking AND advisory stages all
// count as expectation (the gate splits modes, the heads-up shows the whole
// chain). Escaped or nil state → empty: silence matches intent; the one-shot
// marker (ArtifactAdvisoryFired) stays untouched — implement-round semantics
// are unchanged.
//
// ArtifactChainExpectation 返回任务产物链里预期而未登记的节点与下一节点指引
// （P3）：开工前探针，喂给 `forge task start` 的提示——代码开工前产出产物是拦截
// 方向错误最便宜的时点。与 implement 门禁 CheckArtifactChainGate 同一真相源
// （artifactchain.Load）；blocking 与 advisory 节点都算预期（门禁分档执法，
// 开工提示展示整条链）。逃生或 nil state 返回空：静默与意图一致；一次性标记
// （ArtifactAdvisoryFired）不在此动——implement 轮语义不变。
func ArtifactChainExpectation(root string, state *TaskState) (missing []string, next string) {
	if state == nil {
		return nil, ""
	}
	if escapeDisabled(state, escapeArtifactChain, artifactChainDisableEnv) {
		return nil, ""
	}
	chain, _ := artifactchain.Load(root)
	for _, s := range chain.Stages {
		if _, has := state.SpecArtifacts[s.Name]; !has {
			missing = append(missing, s.Name)
		}
	}
	if len(missing) == 0 {
		return nil, ""
	}
	return missing, nextArtifactHint(chain, missing)
}

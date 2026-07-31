package skillseval

// judge_accept.go — machine acceptance criteria of the skill evolution loop (deterministic, not LLM-as-judge).
//
// The skill-evolution loop used to close out by asking the agent to self-report accept/reject — this is
// a gap (dogfood rule: pure self-discipline always misses; accept is a people-pleasing default). JudgeSkillAccept
// normalizes the deterministic signals of RegressionReport (matched-set regression) into accept/reject + reasons,
// for eval-report output and skill-evolution SKILL consumption — spec-as-executable: machine criteria replace self-report.
//
// Boundary: deterministic. It only reads Regressions already computed by CompareRuns (matched-set baseline pass →
// latest fail); it does not call LLM, does not guess semantics, does not compare pass-rate deltas. When not
// comparable (across versions/models/desc), it degrades to accept+advisory — where machine criteria are unreliable
// it does not force reject (which would falsely kill normal changes from forge upgrades/model swaps); it honestly
// escalates to human review. This mirrors the Emergence World proof-of-work principle: accept must be backed by
// a deterministic signal.
//
// Why not also judge pass-rate drops separately: CompareRuns's Regressions are based on CaseResult.Pass,
// so any matched-set regression (trigger/not-trigger) is already captured by len(Regressions)>0, and a
// pass-rate drop always comes with Regressions≥1; a separate criterion is redundant and would falsely
// reject during case-set turnover — a single regression signal is more honest.
//
// judge_accept.go — skill 进化闭环的机器验收判据（deterministic，非 LLM-as-judge）。
//
// skill-evolution 闭环曾靠 agent 自述"accept/reject"收尾——这是断口（dogfood 铁律：
// 纯靠自觉必漏，accept 是讨好型默认）。JudgeSkillAccept 把 RegressionReport 的确定信号
// （matched 集退化）归一成 accept/reject + reasons，供 eval-report 输出 + skill-evolution
// SKILL 消费——spec-as-executable：机器判据取代自述。
//
// 边界：deterministic。只看 CompareRuns 已算出的 Regressions（matched 集 baseline pass →
// latest fail），不调 LLM、不猜语义、不比对 pass-rate delta。不可比（跨版本/模型/desc）时
// 降级 accept+advisory——机器判据不可靠处不强 reject（会误杀 forge 升级/换模型的正常变更），
// 诚实交人工复核。对应 Emergence World 的「工作量证明」：accept 必须由确定信号支撑。
//
// 为什么不单独判通过率下降：CompareRuns 的 Regressions 基于 CaseResult.Pass，故 matched 集内任何
// 退化（trigger/not-trigger）已被 len(Regressions)>0 捕获，通过率下降必伴随
// Regressions≥1，单独判据冗余且在 case 集换血时会误 reject——单一退化信号更诚实。

import (
	"fmt"
	"strings"
)

// JudgeSkillAccept is the machine acceptance criterion for skill evolution: it judges accept/reject from RegressionReport.
//
// Criteria (short-circuit in order, first match decides the outcome):
//  1. nil → accept (reasons nil)
//  2. No baseline (HasBaseline=false) → accept + advisory reason (first run, baseline not yet anchored, machine
//     criteria unavailable — must first eval-baseline to establish a baseline before subsequent optimizations can be
//     judged for regression; this state must not be read as no regression)
//  3. Not comparable (Comparable=false) → accept + advisory reason (delta across versions/models/desc is a false
//     regression, machine criteria are unreliable, escalate to human review — not forcing reject avoids falsely
//     killing normal upgrades)
//  4. Matched-set has regression (len(Regressions)>0) → reject (clear regression of baseline pass → latest fail,
//     covering trigger/not-trigger cases)
//  5. Otherwise → accept (no regression signal, reasons nil)
//
// reasons: non-empty on reject (specific case ids, as reject evidence); on accept+advisory contains degradation
// notes (first run no baseline / not comparable); nil on pure accept (nil input or no regression). Consumers
// (eval-report / skill-evolution SKILL) decide based on accept, but must read reasons to distinguish truly-no-regression
// from machine-criteria-unavailable; reasons land in decisions.md.
//
// JudgeSkillAccept 是 skill 进化的机器验收判据：据 RegressionReport 判 accept/reject。
//
// 判据（按序短路，首个命中决定结果）：
//  1. nil → accept（reasons nil）
//  2. 无 baseline（HasBaseline=false）→ accept + advisory reason（首跑未锚定基线，机器判据
//     不可用——需先 eval-baseline 建立基线，后续优化才能判退化；此态不能当「无退化」读）
//  3. 不可比（Comparable=false）→ accept + advisory reason（跨版本/模型/desc 的 delta 是假
//     回归，机器判据不可靠，交人工复核——不强 reject 避免误杀正常升级）
//  4. matched 集有退化（len(Regressions)>0）→ reject（baseline pass → latest fail 的明确退化，
//     含 trigger/not-trigger 两类 case）
//  5. 否则 → accept（无退化信号，reasons nil）
//
// reasons：reject 时非空（具体 case id，作 reject 证据）；accept+advisory 时含降级说明（首跑
// 无 baseline / 不可比）；纯 accept（nil 或无退化）时 nil。consumer（eval-report / skill-evolution
// SKILL）据 accept 决策，但须读 reasons 区分「真无退化」与「机器判据不可用」，reasons 落进 decisions.md。
func JudgeSkillAccept(rep *RegressionReport) (accept bool, reasons []string) {
	if rep == nil {
		return true, nil
	}
	if !rep.HasBaseline {
		return true, []string{`首跑无 baseline，机器判据未锚定（先 eval-baseline 建立基线，后续优化才能判退化）`}
	}
	if !rep.Comparable {
		return true, []string{fmt.Sprintf(`不可比（%s）——机器判据降级，需人工复核`, rep.IncomparableReason)}
	}
	if len(rep.Regressions) > 0 {
		ids := make([]string, 0, len(rep.Regressions))
		for _, r := range rep.Regressions {
			ids = append(ids, r.CaseID)
		}
		return false, []string{fmt.Sprintf(`%d 条 matched case 退化：%s`, len(rep.Regressions), strings.Join(ids, ", "))}
	}
	return true, nil
}

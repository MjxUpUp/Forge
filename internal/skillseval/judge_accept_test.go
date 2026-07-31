package skillseval

import (
	"strings"
	"testing"
)

// TestJudgeSkillAccept locks the machine acceptance criteria for skill evolution — deterministic accept/reject, replacing
// agent self-report. Covers five states: nil/no baseline/incomparable degraded/regressions reject/no-regression accept.
// Any breakage breaks the skill-evolution loop's accept criteria (false reject kills normal optimization, false admit
// regression into baseline).
//
// TestJudgeSkillAccept 锁定 skill 进化的机器验收判据——deterministic accept/reject，取代
// agent 自述。覆盖五态：nil/无 baseline/不可比降级/regressions reject/无退化 accept。
// 任一断裂则 skill-evolution 闭环的 accept 判据失准（误 reject 杀正常优化，误 accept 放
// 退化进 baseline）。
func TestJudgeSkillAccept(t *testing.T) {
	cases := []struct {
		name      string
		rep       *RegressionReport
		wantAcc   bool
		wantSub   string // reasons join 后应含的子串（wantNoAdv=true 时忽略）
		wantNoAdv bool   // true=纯 accept（reasons 必须空）
	}{
		{
			name:      `nil report`,
			rep:       nil,
			wantAcc:   true,
			wantNoAdv: true,
		},
		{
			name:    `无 baseline（首跑未锚定，advisory 不能当无退化读）`,
			rep:     &RegressionReport{HasBaseline: false},
			wantAcc: true,
			wantSub: `首跑无 baseline`,
		},
		{
			name: `不可比降级 accept+advisory（不强 reject 避误杀升级）`,
			rep: &RegressionReport{
				HasBaseline:        true,
				Comparable:         false,
				IncomparableReason: `forge_version v1→v2`,
			},
			wantAcc: true,
			wantSub: `不可比`,
		},
		{
			name: `regressions 触发 reject（列退化 case id）`,
			rep: &RegressionReport{
				HasBaseline: true,
				Comparable:  true,
				Regressions: []CaseResult{{CaseID: "t1"}, {CaseID: "t2"}},
			},
			wantAcc: false,
			wantSub: `t1`,
		},
		{
			name: `comparable 无 regressions → accept（正常优化）`,
			rep: &RegressionReport{
				HasBaseline:  true,
				Comparable:   true,
				Improvements: []CaseResult{{CaseID: "i1"}}, // 改善不计退化
			},
			wantAcc:   true,
			wantNoAdv: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			accept, reasons := JudgeSkillAccept(c.rep)
			if accept != c.wantAcc {
				t.Errorf(`accept=%v, want %v (reasons=%v)`, accept, c.wantAcc, reasons)
			}
			if c.wantNoAdv {
				if len(reasons) != 0 {
					t.Errorf(`纯 accept 应无 reasons, got %v`, reasons)
				}
				return
			}
			if c.wantSub != "" {
				joined := strings.Join(reasons, `; `)
				if !strings.Contains(joined, c.wantSub) {
					t.Errorf(`reasons 应含 %q, got %q`, c.wantSub, joined)
				}
			}
		})
	}
}

// TestJudgeSkillAccept_RegressionViaRegressions pins the design where a matched-set regression is captured
// by Regressions: a case's pass→fail enters the matched set Regressions and triggers reject. This validates
// the rationale for not judging pass-rate drops separately — a single regression signal (Regressions) covers both case kinds.
//
// TestJudgeSkillAccept_RegressionViaRegressions 钉住「matched 集退化由 Regressions 捕获」
// 的设计：case 的 pass→fail 进 matched 集 Regressions，触发 reject。
// 这验证不单独判通过率下降的合理性——单一退化信号（Regressions）已覆盖两类 case。
func TestJudgeSkillAccept_RegressionViaRegressions(t *testing.T) {
	latest := &EvalRun{
		RunID: "run-latest", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "dh",
		Results: []CaseResult{
			{CaseID: "b1", Kind: KindTrigger, Pass: false}, // baseline pass → latest fail
			{CaseID: "b2", Kind: KindTrigger, Pass: false}, // baseline pass → latest fail
		},
	}
	baseline := &EvalRun{
		RunID: "run-base", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "dh",
		Results: []CaseResult{
			{CaseID: "b1", Kind: KindTrigger, Pass: true},
			{CaseID: "b2", Kind: KindTrigger, Pass: true},
		},
	}
	rep := CompareRuns(latest, baseline)
	if len(rep.Regressions) != 2 {
		t.Fatalf(`pass→fail 应进 Regressions, got len=%d`, len(rep.Regressions))
	}
	accept, reasons := JudgeSkillAccept(rep)
	if accept {
		t.Errorf(`matched 集退化应 reject, got accept (reasons=%v)`, reasons)
	}
	joined := strings.Join(reasons, `; `)
	if !strings.Contains(joined, `b1`) {
		t.Errorf(`reject reasons 应含退化 case id b1, got %q`, joined)
	}
}

// TestJudgeSkillAccept_MatchedTurnoverNoRegression pins that full matched-set turnover does not falsely reject.
//
// The construction is synthetic: latest and baseline case ids are entirely different (matched=0) while Comparable
// stays true (DescHash forced equal). trigger/not-trigger case ids are a deterministic function of the description
// (caseID=sha1(skill:fragment), cases.go), so same DescHash ⟹ same case ids in production — turnover with equal
// DescHash cannot occur naturally; this test guards the JudgeSkillAccept contract itself.
//
// In this state (comparable + matched=0), latest all-fail also does not enter Regressions — Regressions only looks at matched set
// baseline pass→latest fail, turnover has no matched. JudgeSkillAccept must accept (reasons nil), and must not
// falsely detect regression from latest absolute pass rate 0. This is the honest tradeoff of only looking at Regressions (not latest absolute pass rate).
//
// TestJudgeSkillAccept_MatchedTurnoverNoRegression 钉住「matched 集全换血不误 reject」。
//
// 构造是合成的：latest 与 baseline 的 case id 全不同（matched=0）而 Comparable 仍 true
// （DescHash 强制相等）。trigger/not-trigger 的 case id 是 description 的确定函数
// （caseID=sha1(skill:fragment)，cases.go），生产上同 DescHash ⟹ 同 case id——等指纹换血
// 不会自然发生；本测试守的是 JudgeSkillAccept 判据本身的契约。
//
// 此态（comparable + matched=0）latest 全 fail 也不进 Regressions——Regressions 只看 matched 集
// baseline pass→latest fail，换血没有 matched。JudgeSkillAccept 必 accept（reasons nil），不能因
// latest 绝对通过率为 0 误判退化。这是只看 Regressions（不看 latest 绝对通过率）的诚实取舍。
func TestJudgeSkillAccept_MatchedTurnoverNoRegression(t *testing.T) {
	latest := &EvalRun{
		RunID: "run-latest", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "dh",
		Results: []CaseResult{
			{CaseID: "x1", Kind: KindTrigger, Pass: false}, // 全新 case id，不在 baseline matched 集
			{CaseID: "x2", Kind: KindTrigger, Pass: false},
		},
	}
	baseline := &EvalRun{
		RunID: "run-base", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "dh",
		Results: []CaseResult{
			{CaseID: "a1", Kind: KindTrigger, Pass: true},
			{CaseID: "a2", Kind: KindTrigger, Pass: true},
		},
	}
	rep := CompareRuns(latest, baseline)
	if !rep.Comparable {
		t.Fatalf(`同版本/模型/desc 应 comparable, got 不可比: %s`, rep.IncomparableReason)
	}
	if rep.Matched != 0 {
		t.Fatalf(`case id 全不同应 matched=0（换血）, got %d`, rep.Matched)
	}
	if len(rep.Regressions) != 0 {
		t.Fatalf(`换血无 matched 退化, got Regressions=%v`, rep.Regressions)
	}
	accept, reasons := JudgeSkillAccept(rep)
	if !accept {
		t.Errorf(`matched 全换血（无退化信号）应 accept, got reject (reasons=%v)`, reasons)
	}
	if len(reasons) != 0 {
		t.Errorf(`comparable 无退化应纯 accept（reasons nil）, got %v`, reasons)
	}
}

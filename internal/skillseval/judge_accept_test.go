package skillseval

import (
	"strings"
	"testing"
)

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

// TestJudgeSkillAccept_BehaviorRegressionViaRegressions 钉住「behavior 退化由 Regressions 捕获」
// 的设计：behavior case 的 pass→fail（judgeBehavior 判定）进 matched 集 Regressions，触发 reject。
// 这验证不单独判 behavior pass-rate 的合理性——单一退化信号（Regressions）已覆盖三类 case。
func TestJudgeSkillAccept_BehaviorRegressionViaRegressions(t *testing.T) {
	latest := &EvalRun{
		RunID: "run-latest", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "dh",
		Results: []CaseResult{
			{CaseID: "b1", Kind: KindBehavior, Pass: false}, // baseline pass → latest fail
			{CaseID: "b2", Kind: KindBehavior, Pass: false}, // baseline pass → latest fail
		},
	}
	baseline := &EvalRun{
		RunID: "run-base", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "dh",
		Results: []CaseResult{
			{CaseID: "b1", Kind: KindBehavior, Pass: true},
			{CaseID: "b2", Kind: KindBehavior, Pass: true},
		},
	}
	rep := CompareRuns(latest, baseline)
	if len(rep.Regressions) != 2 {
		t.Fatalf(`behavior pass→fail 应进 Regressions, got len=%d`, len(rep.Regressions))
	}
	accept, reasons := JudgeSkillAccept(rep)
	if accept {
		t.Errorf(`behavior 退化应 reject, got accept (reasons=%v)`, reasons)
	}
	joined := strings.Join(reasons, `; `)
	if !strings.Contains(joined, `b1`) {
		t.Errorf(`reject reasons 应含退化 case id b1, got %q`, joined)
	}
}

// TestJudgeSkillAccept_MatchedTurnoverNoRegression 钉住「matched 集全换血不误 reject」。
//
// 真实换血路径只有 behavior 集：probes.yaml 的 case id 可变（手改 id / input+oracle 重算），
// 且 behavior 集 DescHash 留空、跳过 SubmitRun 的 DescHash 校验（runs.go:347-354）——故 latest
// 与 baseline 的 case id 可全不同（matched=0）而 Comparable 仍 true（DescHash 都空相等）。
// trigger/not-trigger 集则不然：caseID=sha1(skill:fragment) 是确定函数（cases.go），DescHash 一致
// ⟹ 同 description ⟹ 同 fragments ⟹ 同 case id，不可换血——故本测试用 KindBehavior 构造真实路径。
//
// 此态（comparable + matched=0）latest 全 fail 也不进 Regressions——Regressions 只看 matched 集
// baseline pass→latest fail，换血没有 matched。JudgeSkillAccept 必 accept（reasons nil），不能因
// latest 绝对通过率为 0 误判退化。这是只看 Regressions（不看 latest 绝对通过率）的诚实取舍。
func TestJudgeSkillAccept_MatchedTurnoverNoRegression(t *testing.T) {
	latest := &EvalRun{
		RunID: "run-latest", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "", // behavior 集不锚 description
		Results: []CaseResult{
			{CaseID: "x1", Kind: KindBehavior, Pass: false}, // 全新 case id（probes 重建），不在 baseline matched 集
			{CaseID: "x2", Kind: KindBehavior, Pass: false},
		},
	}
	baseline := &EvalRun{
		RunID: "run-base", Skill: "sk", ForgeVersion: "v1", AgentModel: "m", DescHash: "",
		Results: []CaseResult{
			{CaseID: "a1", Kind: KindBehavior, Pass: true},
			{CaseID: "a2", Kind: KindBehavior, Pass: true},
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

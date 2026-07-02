package mcpserver

// tools_eval.go — 3 个 skill eval 闭环 MCP 工具的核心逻辑（照 tools.go 的
// xxxInput/xxxOutput/xxxCore 三段式）。
//
// 这些工具不依赖 project root——canonical 是全局 skill 库、EvalDir 在 ~/——所以
// 注册时用 withoutRoot。cases/submit 需要解析 canonical（skillscanonical.Resolve），
// canonical 解析走 embed fallback 时依赖 forge 版本，故用闭包捕获 ver 注入。

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// =====================================================================
// forge_skill_eval_cases —— 生成 case 集 + dispatch 指令包（agent 据此跑回归）
// =====================================================================

type skillEvalCasesInput struct {
	Skill string `json:"skill" jsonschema:"要生成 eval case 集的 skill 名"`
}

type skillEvalCaseItem struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`   // trigger | not-trigger
	Prompt string `json:"prompt"` // 发给 subagent 的测试 prompt
	Target string `json:"target,omitempty"`
}

type skillEvalCasesOutput struct {
	Cases            []skillEvalCaseItem `json:"cases"`
	DispatchProtocol string              `json:"dispatch_protocol"`
	DescHash         string              `json:"desc_hash"`
}

// skillEvalCasesCore 用闭包捕获 ver（解析 canonical 的 embed fallback 需要）。
func skillEvalCasesCore(ver string) func(skillEvalCasesInput) (skillEvalCasesOutput, error) {
	return func(in skillEvalCasesInput) (skillEvalCasesOutput, error) {
		if in.Skill == "" {
			return skillEvalCasesOutput{}, fmt.Errorf("skill is required")
		}
		canonical, _, err := skillscanonical.Resolve(ver)
		if err != nil {
			return skillEvalCasesOutput{}, err
		}
		cases, err := skillseval.EvalCases(canonical, in.Skill)
		if err != nil {
			return skillEvalCasesOutput{}, fmt.Errorf("derive cases: %w", err)
		}
		// 落盘 case 集——submit 读它做 DescHash 校验 + 判定。与 CLI eval-gen --save
		// 行为一致：cases 工具是「准备回归基准」，生成即存。
		dir, err := skillseval.EvalDir()
		if err != nil {
			return skillEvalCasesOutput{}, err
		}
		if err := skillseval.SaveCases(dir, in.Skill, cases); err != nil {
			return skillEvalCasesOutput{}, fmt.Errorf("save cases: %w", err)
		}
		out := skillEvalCasesOutput{DispatchProtocol: dispatchProtocol(in.Skill, cases)}
		for _, c := range cases {
			out.Cases = append(out.Cases, skillEvalCaseItem{
				ID: c.ID, Kind: c.Kind, Prompt: c.Prompt, Target: c.Target,
			})
			if out.DescHash == "" {
				out.DescHash = c.DescHash
			}
		}
		return out, nil
	}
}

// dispatchProtocol 生成 agent 可执行的回归指令（markdown）。
// 半自动定位：forge spawn 不了 AI，由 agent 据此 dispatch fresh subagent 跑每个 prompt。
func dispatchProtocol(skill string, cases []skillseval.EvalCase) string {
	var b strings.Builder
	b.WriteString("# Skill Eval Dispatch Protocol\n\n")
	b.WriteString(fmt.Sprintf("目标 skill：%s\n\n", skill))
	b.WriteString("对每个 case 启动一个 **fresh subagent**（不共享上下文，避免它「知道」你在测哪个 skill），")
	b.WriteString("发送 case.prompt，问：「针对这个请求，你会加载哪个 skill？只回答 skill 名或 none。」\n\n")
	b.WriteString("## 判定规则（由 forge 在 submit 时执行，subagent 只需如实回答）\n")
	b.WriteString(fmt.Sprintf("- trigger 类：期望回答 = %s\n", skill))
	b.WriteString(fmt.Sprintf("- not-trigger 类：期望回答 ≠ %s（none 或任何其他 skill 都算通过）\n", skill))
	b.WriteString("- 回答格式：单个 skill 名 或 none（小写、无标点、无解释）\n\n")
	b.WriteString("## Case 列表\n")
	for _, c := range cases {
		b.WriteString(fmt.Sprintf("- %s [%s] %s\n", c.ID, c.Kind, c.Prompt))
	}
	b.WriteString("\n## 回填\n")
	b.WriteString("全部跑完后，调 forge_skill_eval_submit 整批提交 results（每个 case_id + actual_triggered + 可选 note）。\n")
	return b.String()
}

// =====================================================================
// forge_skill_eval_submit —— 整批回填 run（归一化 + 判定 + health + append）
// =====================================================================

type skillEvalSubmitResult struct {
	CaseID          string `json:"case_id" jsonschema:"case 的 id（来自 cases 工具输出）"`
	ActualTriggered string `json:"actual_triggered" jsonschema:"subagent 实际回答：skill 名 或 none"`
	Note            string `json:"note,omitempty" jsonschema:"可选：异常/理由标注"`
}

type skillEvalSubmitInput struct {
	Skill      string                  `json:"skill"`
	AgentModel string                  `json:"agent_model" jsonschema:"跑此 run 的 agent 模型（防跨模型假回归）"`
	Results    []skillEvalSubmitResult `json:"results"`
}

type skillEvalSubmitOutput struct {
	RunID          string  `json:"run_id"`
	HealthScore    float64 `json:"health_score"`
	NetRegressions int     `json:"net_regressions"`
	HasBaseline    bool    `json:"has_baseline"`
	Comparable     bool    `json:"comparable"` // 维度一致才可比；不可比时 net_regressions 降级为 advisory
}

// skillEvalSubmitCore 用闭包捕获 ver：既作 canonical 解析的 embed 版本，又作 run 的
// ForgeVersion 戳记（防跨版本假回归）。
func skillEvalSubmitCore(ver string) func(skillEvalSubmitInput) (skillEvalSubmitOutput, error) {
	return func(in skillEvalSubmitInput) (skillEvalSubmitOutput, error) {
		if in.Skill == "" {
			return skillEvalSubmitOutput{}, fmt.Errorf("skill is required")
		}
		dir, err := skillseval.EvalDir()
		if err != nil {
			return skillEvalSubmitOutput{}, err
		}
		canonical, _, err := skillscanonical.Resolve(ver)
		if err != nil {
			return skillEvalSubmitOutput{}, err
		}
		raw := make([]skillseval.SubmitResult, len(in.Results))
		for i, r := range in.Results {
			raw[i] = skillseval.SubmitResult{CaseID: r.CaseID, ActualTriggered: r.ActualTriggered, Note: r.Note}
		}
		run, err := skillseval.SubmitRun(dir, canonical, in.Skill, in.AgentModel, ver, raw)
		if err != nil {
			return skillEvalSubmitOutput{}, err
		}
		out := skillEvalSubmitOutput{
			RunID:       run.RunID,
			HealthScore: run.HealthScore,
			// Comparable 语义：「与 baseline 的比对是否可信」。无 baseline 时无可比
			// 对象，置 false（而非误导性的 true）；有 baseline 时由下方 CompareRuns 复算。
			HasBaseline: run.BaselineRunID != "",
			Comparable:  false,
		}
		// 若锁定了 baseline，复算 net_regressions + comparable 给 agent 一眼信号。
		if run.BaselineRunID != "" {
			if base, _ := skillseval.LoadRunByID(dir, in.Skill, run.BaselineRunID); base != nil {
				rep := skillseval.CompareRuns(run, base)
				out.NetRegressions = rep.NetRegressions
				out.Comparable = rep.Comparable
			}
		}
		return out, nil
	}
}

// =====================================================================
// forge_skill_eval_report —— latest run vs baseline 的回归报告
// =====================================================================

type skillEvalReportInput struct {
	Skill         string `json:"skill"`
	BaselineRunID string `json:"baseline_run_id,omitempty" jsonschema:"显式 baseline run-id；空则用该 skill 标记的 baseline"`
}

type skillEvalReportOutput struct {
	Report       *skillseval.RegressionReport `json:"report"`
	LatestHealth float64                      `json:"latest_health"`
}

// skillEvalReportCore 不需要 ver（不解析 canonical，只用 EvalDir + 已落盘 run）。
func skillEvalReportCore(in skillEvalReportInput) (skillEvalReportOutput, error) {
	if in.Skill == "" {
		return skillEvalReportOutput{}, fmt.Errorf("skill is required")
	}
	dir, err := skillseval.EvalDir()
	if err != nil {
		return skillEvalReportOutput{}, err
	}
	latest, err := skillseval.LatestRun(dir, in.Skill)
	if err != nil {
		return skillEvalReportOutput{}, err
	}
	if latest == nil {
		return skillEvalReportOutput{}, fmt.Errorf("skill %q 还没有 run——先 submit", in.Skill)
	}
	var baseline *skillseval.EvalRun
	if in.BaselineRunID != "" {
		baseline, err = skillseval.LoadRunByID(dir, in.Skill, in.BaselineRunID)
		if err != nil {
			return skillEvalReportOutput{}, err
		}
		if baseline == nil {
			return skillEvalReportOutput{}, fmt.Errorf("baseline run %q 不存在", in.BaselineRunID)
		}
	} else if bl, _ := skillseval.GetBaseline(dir, in.Skill); bl.RunID != "" {
		baseline, _ = skillseval.LoadRunByID(dir, in.Skill, bl.RunID)
	}
	rep := skillseval.CompareRuns(latest, baseline)
	return skillEvalReportOutput{Report: rep, LatestHealth: latest.HealthScore}, nil
}

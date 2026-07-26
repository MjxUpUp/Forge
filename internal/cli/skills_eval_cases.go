package cli

// skills_eval_cases.go — eval-cases subcommand: emits the redacted case set.
//
// When an agent dispatches a fresh subagent to run a prompt, this command is used to
// obtain case_id + prompt (instead of cat-ing cases/<skill>.json directly). Component C
// permission separation: the Oracle field of behavior cases is redacted — the agent that
// runs the probe only gets ProbeInput and must not see the oracle text (to prevent
// overfitting the oracle rather than genuinely improving the skill). forge reads back the
// Oracle from the persisted case set during internal judgment, and redacts it for external
// output (this command).
//
// The `half` of half-automatic: physically the oracle lives in the same file as the case,
// and access-layer redaction relies on this command plus agent discipline (not reading
// cases/<skill>.json directly). The skill-evolution skill guidance reinforces this rule.
//
// skills_eval_cases.go — eval-cases 子命令：输出脱敏 case 集。
//
// agent dispatch fresh subagent 跑 prompt 时，用本命令拿 case_id + prompt（而非直接
// cat cases/<skill>.json）。C 组件权限分离：behavior case 的 Oracle 字段 redact——
// 跑 probe 的 agent 只拿 ProbeInput，不应看 oracle 原文（防过拟合 oracle 而非真正
// 改进 skill）。forge 内部判定时从落盘 case 集读回 Oracle，对外（本命令）redact。
//
// half-automatic 的「半」：物理上 oracle 与 case 同文件，访问层脱敏靠本命令 + agent
// 纪律（不直接读 cases/<skill>.json）。skill-evolution skill 指引强化此纪律。

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var skCasesSkill string

var skillsEvalCasesCmd = &cobra.Command{
	Use:   "eval-cases",
	Short: "输出脱敏 case 集（agent dispatch 跑 prompt 的输入；behavior 的 oracle redact）",
	Long: `输出 case 集 JSON，供 agent dispatch subagent 跑 prompt 时拿 case_id + prompt。

脱敏（C 组件权限分离）：behavior case 的 Oracle 字段不输出——跑 probe 的 agent 只拿
ProbeInput，不应看 oracle 原文（防过拟合）。forge 内部判定时从落盘 case 集读回 Oracle。

trigger/not-trigger case：agent 跑 Prompt，回填 actual_triggered（是否触发本 skill）。
behavior case：agent 跑 ProbeInput，把 skill 实际输出回填 actual_output（eval-record）。

格式：
  [{"id","kind","prompt","target"(trigger),"probe_input"(behavior),"probe_rationale"(behavior)}]`,
	RunE: runSkillsEvalCases,
}

// redactedCase is the redacted view emitted by eval-cases (Oracle intentionally omitted).
//
// redactedCase 是 eval-cases 对外输出的脱敏视图（Oracle 故意不带）。
type redactedCase struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Prompt         string `json:"prompt"`
	Target         string `json:"target,omitempty"`         // trigger 类 = skill 名
	ProbeInput     string `json:"probe_input,omitempty"`    // behavior 类：跑给 skill 的输入
	ProbeRationale string `json:"probe_rationale,omitempty"` // behavior 类：oracle 的 why（可显，不含答案）
}

// redactCases converts the EvalCase set into the redacted view (Oracle field omitted).
// This is the core of component C permission separation: the agent running the probe
// obtains this view via eval-cases and cannot see the oracle text (to prevent overfitting
// the oracle rather than genuinely improving the skill). Extracted as an independent
// function so regression tests can guard this safety property.
//
// redactCases 把 EvalCase 集转成脱敏视图（Oracle 字段不带）。C 组件权限分离的核心：
// 跑 probe 的 agent 经 eval-cases 拿这个视图，看不到 oracle 原文（防过拟合 oracle 而非
// 真正改进 skill）。抽成独立函数便于回归测试守护此安全属性。
func redactCases(cases []skillseval.EvalCase) []redactedCase {
	out := make([]redactedCase, len(cases))
	for i, c := range cases {
		out[i] = redactedCase{
			ID:             c.ID,
			Kind:           c.Kind,
			Prompt:         c.Prompt,
			Target:         c.Target,
			ProbeInput:     c.ProbeInput,
			ProbeRationale: c.ProbeRationale,
			// Oracle intentionally omitted — redaction (component C).
			//
			// Oracle 故意不输出——脱敏（C 组件）
		}
	}
	return out
}

func runSkillsEvalCases(cmd *cobra.Command, args []string) error {
	if skCasesSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	dir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	cases, err := skillseval.LoadCases(dir, skCasesSkill)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		return fmt.Errorf("skill %q 无 case 集——先 eval-gen --skill %s --save", skCasesSkill, skCasesSkill)
	}
	out := redactCases(cases)
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func init() {
	skillsEvalCasesCmd.Flags().StringVar(&skCasesSkill, "skill", "", "输出哪个 skill 的脱敏 case 集")
	skillsCmd.AddCommand(skillsEvalCasesCmd)
}

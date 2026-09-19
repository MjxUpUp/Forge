package clitask

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
	"github.com/spf13/cobra"
)

// task_report.go — oracle-pipeline L5：一站式交付验收单。用户的出口站从「拼
// task status / trace / act show / score 四条命令」变成一条命令：考卷逐条结果
// （带层级）、评分与证据强度、未验证面、残留风险、考卷披露（manual 层/held-out/
// spec 审批）、逃生舱库存、审查状态——每条声明都给复现入口。证据束字段
// （UntestedAreas/RemainingRisks）由 ScoreTask 落盘，本命令只组装渲染。

var taskReportCmd = &cobra.Command{
	Use:   "report [--ref <ref>] [--json]",
	Short: "交付验收单：考卷逐条结果+证据、未验证面、残留风险、逃生舱库存（验收方单页视图）",
	Long: `一站式交付验收单（oracle-pipeline L5）：把散落在 TaskState / checklog / act
的交付证据组装成验收方单页视图——

  考卷（验收标准）：逐条 层级/命令/期望/结果，来源分布（spec-extract/start/
    conventions/manual 四级考卷梯）
  评分与证据强度：Score/Grade、deterministic vs agent-claim 占比
  未验证面：无配对测试的改动源文件（UntestedAreas——高可信=证据+显式披露未验证面）
  残留风险：完成时仍 open 的 findings（RemainingRisks）
  考卷披露：manual 层存在告警、held-out 保留集、spec 产物 human 档审批态
  逃生舱库存 / 审查状态 / 复现入口

--json 输出结构化形态（供面板/脚本消费）。`,
	RunE: runTaskReport,
}

func init() {
	Root.AddCommand(taskReportCmd)
	taskReportCmd.Flags().String("ref", "", "指定任务 ref（缺省取活跃任务；已完成的任务须 --ref）")
	taskReportCmd.Flags().Bool("json", false, "结构化 JSON 输出")
}

// taskReportJSON 是 --json 的结构化形态——人读渲染的单一数据源（renderTaskReport
// 消费同一结构，两侧永不漂移）。
type taskReportJSON struct {
	TaskRef           string                             `json:"task_ref"`
	Branch            string                             `json:"branch,omitempty"`
	Kind              string                             `json:"kind,omitempty"`
	Completed         bool                               `json:"completed"`
	Acceptance        []taskpipeline.AcceptanceCriterion `json:"acceptance"`
	TierCounts        map[string]int                     `json:"tier_counts"`
	HasManualTier     bool                               `json:"has_manual_tier"`
	Score             *scoringtypes.ScoreResult          `json:"score,omitempty"`
	EvidenceStrength  string                             `json:"evidence_strength,omitempty"`
	Deterministic     int                                `json:"deterministic"`
	AgentClaim        int                                `json:"agent_claim"`
	UntestedAreas     []string                           `json:"untested_areas,omitempty"`
	RemainingRisks    []string                           `json:"remaining_risks,omitempty"`
	Escapes           map[string]int                     `json:"escapes,omitempty"`
	ReviewPassed      bool                               `json:"review_passed"`
	ReviewRounds      int                                `json:"review_rounds"`
	ChecklistDone     int                                `json:"checklist_done"`
	ChecklistTotal    int                                `json:"checklist_total"`
	HeldoutRegistered bool                               `json:"heldout_registered"`
	HeldoutProbeErr   string                             `json:"heldout_probe_err,omitempty"`
	HasSpecArtifact   bool                               `json:"has_spec_artifact"`
	SpecApprovalBy    string                             `json:"spec_approval_by,omitempty"`
	SpecApprovedAt    string                             `json:"spec_approved_at,omitempty"`
	Repro             []string                           `json:"repro"`
}

// runTaskReport 加载任务状态与 checklog，组装 taskReportJSON 后按 --json 或人读
// 渲染输出。
func runTaskReport(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")
	asJSON, _ := cmd.Flags().GetBool("json")

	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return fmt.Errorf("加载任务 %q 失败: %w", explicitRef, err)
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return fmt.Errorf("failed to load task state: %w", err)
		}
	}
	if state == nil {
		return fmt.Errorf("no active task（已完成的任务请用 --ref <task-ref> 指定）")
	}

	rep, err := buildTaskReport(root, state)
	if err != nil {
		return err
	}
	if asJSON {
		body, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(body))
		return nil
	}
	fmt.Print(renderTaskReport(rep))
	return nil
}

// buildTaskReport 从 TaskState + checklog 组装报告数据。纯组装不改状态。
func buildTaskReport(root string, state *taskpipeline.TaskState) (*taskReportJSON, error) {
	rep := &taskReportJSON{
		TaskRef:       state.TaskRef,
		Branch:        state.Branch,
		Kind:          state.Kind,
		Completed:     state.CompletedAt != nil,
		Acceptance:    state.Acceptance,
		TierCounts:    taskpipeline.AcceptanceTierCounts(state.Acceptance),
		HasManualTier: taskpipeline.HasManualTierAcceptance(state.Acceptance),
		Score:         state.Score,
		ReviewPassed:  state.ReviewPassed,
		ReviewRounds:  len(state.ReviewRounds),
		Repro: []string{
			fmt.Sprintf("forge task verify-acceptance --ref %s", state.TaskRef),
			fmt.Sprintf("forge trace %s", state.TaskRef),
		},
	}
	for _, item := range state.Checklist {
		rep.ChecklistTotal++
		if item.Done {
			rep.ChecklistDone++
		}
	}

	// 证据链（评分外的实时口径：进行中任务也能看当前强度）。读失败如实标注
	//（审查 P2-3：「读不到」不得渲染成「零证据」——两者对验收方含义不同）。
	if entries, err := checklog.LoadForTask(root, state.TaskRef); err == nil {
		ec := checklog.BuildEvidenceChain(entries, state.TaskRef)
		rep.EvidenceStrength = ec.Strength().String()
		rep.Deterministic = ec.Deterministic
		rep.AgentClaim = ec.AgentClaim
		escapes := map[string]int{}
		for _, e := range entries {
			if e.Check == checklog.CheckEscapeHatch {
				g := checklog.EscapeGateOf(&e)
				if g == "" {
					g = "(legacy)"
				}
				escapes[g]++
			}
		}
		if len(escapes) > 0 {
			rep.Escapes = escapes
		}
	} else {
		rep.EvidenceStrength = "unknown（checklog 不可读：" + err.Error() + "）"
	}

	// 证据束披露字段：已评分任务直接读 ScoreTask 落盘的结果；未评分任务残留风险
	// 仍有廉价来源（open findings 原始清单），未验证面留空并在渲染层标注「未评分」。
	if state.Score != nil && state.Score.Evidence != nil {
		if len(state.Score.Evidence.UntestedAreas) > 0 {
			rep.UntestedAreas = state.Score.Evidence.UntestedAreas
		}
		if len(state.Score.Evidence.RemainingRisks) > 0 {
			rep.RemainingRisks = state.Score.Evidence.RemainingRisks
		}
	} else {
		for _, f := range state.Findings {
			// 与 scoring.go evidenceRemainingRisks 同判：open（含旧 findings 的
			// 空 severity）才算残留，fixed/wontfix 不是。
			if f.Status == "open" {
				rep.RemainingRisks = append(rep.RemainingRisks, formatFindingRisk(f))
			}
		}
	}

	// 考卷披露：held-out 保留集是否在册（只读探针，不跑——读失败与「未登记」
	// 分开披露，不向 understatement 方向错）；spec 产物 human 档审批态。
	if held, herr := taskpipeline.HeldoutRegistered(root, state.TaskRef); herr != nil {
		rep.HeldoutProbeErr = herr.Error()
	} else {
		rep.HeldoutRegistered = held
	}
	if _, ok := state.SpecArtifacts["spec"]; ok {
		rep.HasSpecArtifact = true
		if apr, ok := state.ArtifactApprovals["spec"]; ok {
			rep.SpecApprovalBy = apr.By
			rep.SpecApprovedAt = apr.At.Format("2006-01-02 15:04")
		}
	}
	return rep, nil
}

// formatFindingRisk 渲染单条 open finding 为「severity: content」残留风险行
// （与证据束 RemainingRisks 同格式——两侧消费同一渲染口径）。
func formatFindingRisk(f tasktypes.Finding) string {
	if f.Severity == "" {
		return f.Content
	}
	return fmt.Sprintf("%s: %s", f.Severity, f.Content)
}

// renderTaskReport 人读渲染。纯函数（消费 taskReportJSON，测试可断言全文）。
func renderTaskReport(rep *taskReportJSON) string {
	var b strings.Builder
	status := "进行中"
	if rep.Completed {
		status = "已完成"
	}
	fmt.Fprintf(&b, "╭─ 交付验收单: %s（%s", rep.TaskRef, status)
	if rep.Branch != "" {
		fmt.Fprintf(&b, "，分支 %s", rep.Branch)
	}
	fmt.Fprintf(&b, "）\n")

	// 考卷。
	passed := 0
	for _, c := range rep.Acceptance {
		if c.Passed {
			passed++
		}
	}
	fmt.Fprintf(&b, "│\n│ 考卷（验收标准 %d/%d 通过） 来源：%s\n", passed, len(rep.Acceptance), formatTierCounts(rep.TierCounts))
	for i, c := range rep.Acceptance {
		mark := "❌"
		if c.Passed {
			mark = "✅"
		}
		exp := c.Expected
		if exp == "" {
			exp = "(退出码 0)"
		}
		fmt.Fprintf(&b, "│   %s [%d] (%s) %s :: %s\n", mark, i+1, taskpipeline.FormatAcceptanceTier(c.Source), c.Run, exp)
		if !c.Passed && c.Output != "" {
			out := c.Output
			if lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n"); len(lines) > 0 {
				for _, l := range lines {
					if l = strings.TrimSpace(l); l != "" {
						fmt.Fprintf(&b, "│        %s\n", l)
						break // 只取失败输出首行——验收单要可扫读，全文在 --json
					}
				}
			}
		}
	}

	// 评分与证据。
	fmt.Fprintf(&b, "│\n│ 评分与证据：")
	if rep.Score != nil {
		fmt.Fprintf(&b, "%.0f (%s) · ", rep.Score.Overall, rep.Score.Grade)
	} else {
		fmt.Fprintf(&b, "未评分 · ")
	}
	fmt.Fprintf(&b, "证据 %s（deterministic %d / agent-claim %d）\n", rep.EvidenceStrength, rep.Deterministic, rep.AgentClaim)

	// 未验证面（高可信=证据+显式披露未验证面）。
	fmt.Fprintf(&b, "│ 未验证面：")
	switch {
	case len(rep.UntestedAreas) > 0:
		fmt.Fprintf(&b, "%d 个改动源文件无配对测试\n", len(rep.UntestedAreas))
		for _, f := range rep.UntestedAreas {
			fmt.Fprintf(&b, "│   - %s\n", f)
		}
	case rep.Score == nil:
		fmt.Fprintf(&b, "未知（任务未评分——complete 时计算）\n")
	default:
		fmt.Fprintf(&b, "无（改动源码均有配对测试）\n")
	}

	// 残留风险。
	fmt.Fprintf(&b, "│ 残留风险：")
	if len(rep.RemainingRisks) == 0 {
		fmt.Fprintf(&b, "无未决 finding\n")
	} else {
		fmt.Fprintf(&b, "%d 条 open\n", len(rep.RemainingRisks))
		for _, r := range rep.RemainingRisks {
			fmt.Fprintf(&b, "│   - %s\n", r)
		}
	}

	// 考卷披露。
	if rep.HasManualTier {
		fmt.Fprintf(&b, "│ ⚠ 考卷披露：含事后补登标准（manual 层）——部分考卷在实现者看过代码之后才写成\n")
	}
	heldout := "未登记"
	switch {
	case rep.HeldoutProbeErr != "":
		heldout = "读取异常（" + rep.HeldoutProbeErr + "）"
	case rep.HeldoutRegistered:
		heldout = "已登记"
	}
	fmt.Fprintf(&b, "│ 考卷披露：held-out 保留集 %s", heldout)
	switch {
	case rep.SpecApprovalBy != "":
		fmt.Fprintf(&b, " · spec 已由 %s 审批（%s）\n", rep.SpecApprovalBy, rep.SpecApprovedAt)
	case rep.HasSpecArtifact:
		fmt.Fprintf(&b, " · spec 产物未审批\n")
	default:
		fmt.Fprintf(&b, " · 无 spec 产物\n")
	}

	// 逃生舱库存。
	fmt.Fprintf(&b, "│ 逃生舱：")
	if len(rep.Escapes) == 0 {
		fmt.Fprintf(&b, "无\n")
	} else {
		parts := make([]string, 0, len(rep.Escapes))
		total := 0
		for g, n := range rep.Escapes {
			parts = append(parts, fmt.Sprintf("%s×%d", g, n))
			total += n
		}
		sort.Strings(parts)
		fmt.Fprintf(&b, "%d 次（%s）\n", total, strings.Join(parts, ", "))
	}

	// 审查与对账单。
	review := "未审"
	if rep.ReviewPassed {
		review = fmt.Sprintf("已过（%d 轮）", rep.ReviewRounds)
	}
	fmt.Fprintf(&b, "│ 审查：%s · 对账单：%d/%d 勾\n", review, rep.ChecklistDone, rep.ChecklistTotal)

	// 复现。
	fmt.Fprintf(&b, "│\n│ 复现入口（验收方抽查通道）：\n")
	for _, r := range rep.Repro {
		fmt.Fprintf(&b, "│   %s\n", r)
	}
	fmt.Fprintf(&b, "╰─\n")
	return b.String()
}

// formatTierCounts 渲染「考卷来源」分布行（层级×数量，稳定排序）。
func formatTierCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "无标准"
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}

package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// next.go —— `forge next`：单命令引导（vNext P1-2，LoopSpec nextSteps 语义）。
//
// 设计依据（2026-08-31 三轮调研）：8-30 事故的第四层是"任务入口自愿"——agent 自
// 选下一步于是绕开整个生命周期。next 从 git/任务状态推导出**恰好一条**下一步命令
// +理由，agent 的职责从"自己判断"变成"照单执行"（pull 侧引导，与 push 侧 hook 执
// 法互补——巡警 vs 火警的分工）。状态推导只读：git（分支/脏树）+ ActiveTaskState
// （门禁 History/ReviewPassed/CompletedAt）。
func init() {
	rootCmd.AddCommand(nextCmd)
	nextCmd.Flags().Bool("json", false, "JSON 格式输出（agent 协议主形态）")
}

var nextCmd = &cobra.Command{
	Use:   "next",
	// GroupID：quality 组（与 task/review 同组——单命令引导是任务质量链路的 pull 侧）。
	GroupID: "quality",
	Short:   "推导恰好一条下一步命令（从 git/任务状态——agent 不自选下一步）",
	Long: `Derive the single next command from current git + task state.

覆盖：无任务有脏树 → task start（或 wild 申报）；任务进行中 → 下一道门禁命令；
审前 → 派只读审查后 review pass；门禁齐 → complete；已完成未合并 → finish；
无事可做 → status。--json 是 agent 的机器接口：{"next","reason","state"}。`,
	RunE: runNext,
}

// nextResult 是 forge next 的输出契约。Next 恒非空（无事可做时回落 status）。
type nextResult struct {
	Next   string         `json:"next"`
	Reason string         `json:"reason"`
	State  map[string]any `json:"state"`
}

func runNext(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	sid := taskpipeline.CurrentSessionID()
	st, _ := taskpipeline.ActiveTaskState(root, sid)
	res := nextDecision(gitLine(root, "rev-parse", "--abbrev-ref", "HEAD"),
		gitDirty(root), st)

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(res)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "下一步：%s\n理由：%s\n", res.Next, res.Reason)
	return nil
}

// nextDecision 是纯决策函数（单测锚点）：给定分支、脏树、活跃任务状态，返回恰好
// 一条命令。门禁顺序实现（implement→verify→review→complete→合并）与 task 门禁
// 链一致；review 用 ReviewPassed（task-complete 的硬前置）。
func nextDecision(branch string, dirty bool, st *taskpipeline.TaskState) nextResult {
	gates := map[string]bool{}
	for _, h := range stGateHistory(st) {
		gates[h] = true
	}
	state := map[string]any{
		"branch":         branch,
		"dirty":          dirty,
		"active_task":    taskRef(st),
		"gates_passed":   stGateHistory(st),
		"review_passed":  stReviewPassed(st),
		"task_completed": stCompleted(st),
	}

	// 已完成/无活跃任务：归属问题优先于一切。
	if st == nil || st.CompletedAt != nil {
		if dirty {
			return nextResult{
				Next:   `forge task start --ref <ref> --branch --title <title>`,
				Reason: "工作区有未归属变更而无活跃任务——先建任务收编（刻意的一次性小改可改走 forge task wild \"<说明>\" 申报）",
				State:  state,
			}
		}
		return nextResult{
			Next:   "forge status",
			Reason: "无活跃任务且工作区干净——查看项目状态或认领任务（forge task mine）",
			State:  state,
		}
	}

	// 活跃任务：按门禁链给恰好一条。
	switch {
	case !gates["task-implement"]:
		return nextResult{Next: "forge task gate task-implement", Reason: "实现未确认（有提交即可过）", State: state}
	case !gates["task-verify"]:
		return nextResult{Next: "forge task verify-acceptance && forge task gate task-verify", Reason: "验收标准未实跑回扣——先实跑再过验证门", State: state}
	case !stReviewPassed(st):
		return nextResult{Next: "forge review pass", Reason: "验证已过而审查未过——派只读子代理审查当前 diff 后标记（task-complete 硬前置）", State: state}
	case !gates["task-complete"] && st.CompletedAt == nil:
		return nextResult{Next: "forge task complete", Reason: "实现/验证/审查齐备——完结并评分", State: state}
	}
	// 三门禁齐 + 完成标记已有：交付收尾。
	if branch != "main" && branch != "master" {
		return nextResult{Next: "forge task finish", Reason: "任务已完成——验证门禁后合并分支（主检出则手工合并）", State: state}
	}
	return nextResult{Next: "forge status", Reason: "任务已完成且在主干——查看项目状态", State: state}
}

// gitDirty 报告工作区是否有变更（porcelain 非空即脏；.gitignore 自动生效）。
func gitDirty(root string) bool {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// 下四个小函数把 *TaskState 的字段访问收拢一处，nil 安全且让决策函数可读。
func stGateHistory(st *taskpipeline.TaskState) []string {
	if st == nil {
		return nil
	}
	var out []string
	for _, h := range st.History {
		out = append(out, h.Gate)
	}
	return out
}

func taskRef(st *taskpipeline.TaskState) string {
	if st == nil {
		return ""
	}
	return st.TaskRef
}

func stReviewPassed(st *taskpipeline.TaskState) bool {
	return st != nil && st.ReviewPassed
}

func stCompleted(st *taskpipeline.TaskState) bool {
	return st != nil && st.CompletedAt != nil
}

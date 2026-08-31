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
	Short: "推导恰好一条下一步命令（从 git/任务状态——agent 不自选下一步）",
	Long: `Derive the single next command from current git + task state.

覆盖：无任务有脏树 → task start（或 wild 申报）；任务进行中 → 门禁链下一步
（implement → 验收实跑 → verify → review pass → complete 门 → complete）。
--json 是 agent 的机器接口：{"next","reason","state"}。ActiveTaskState 对已
完结任务返回 nil，故完成后的合并收尾不在本命令承诺内（用 forge task list /
git merge）。`,
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
// 一条命令。门禁顺序与真实链严格一致（P1 审查 FAIL 修正：`forge task complete`
// 自身要求三门禁全过——缺 task-complete gate 时必须先建议 `gate task-complete`；
// `forge task finish` 要求已完成——收官两步此前错位）：implement →（验收未实跑则
// verify-acceptance）→ gate task-verify → review pass → gate task-complete →
// task complete。每条 Next 恰一条命令（无 && 复合）。
func nextDecision(branch string, dirty bool, st *taskpipeline.TaskState) nextResult {
	gates := map[string]bool{}
	for _, h := range stGateHistory(st) {
		gates[h] = true
	}
	state := map[string]any{
		"branch":        branch,
		"dirty":         dirty,
		"active_task":   taskRef(st),
		"gates_passed":  stGateHistory(st),
		"review_passed": stReviewPassed(st),
	}

	// 无活跃任务（ActiveTaskState 对已完成任务返回 nil——完成态经此分支，不承诺
	// finish 引导）：归属问题优先于一切。
	if st == nil {
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

	// 活跃任务：按真实门禁链给恰好一条。
	switch {
	case !gates["task-implement"]:
		return nextResult{Next: "forge task gate task-implement", Reason: "实现未确认（有提交即可过）", State: state}
	case stAcceptancePending(st):
		return nextResult{Next: "forge task verify-acceptance", Reason: "验收标准尚未实跑回扣——先实跑（AcceptedHeadCommit 为空的标准待跑）", State: state}
	case !gates["task-verify"]:
		return nextResult{Next: "forge task gate task-verify", Reason: "验收已实跑——过验证门", State: state}
	case !stReviewPassed(st):
		return nextResult{Next: "forge review pass", Reason: "验证已过而审查未过——派只读子代理审查当前 diff 后标记（task-complete 门禁硬前置）", State: state}
	case !gates["task-complete"]:
		return nextResult{Next: "forge task gate task-complete", Reason: "实现/验证/审查齐备——过第三道门（forge task complete 要求三门禁全过）", State: state}
	default:
		// 三门禁齐 + 已过 review：完结（finish 需完结后才有资格，且 ActiveTaskState
		// 在完结后返回 nil——合并引导不在本命令承诺内，README 已注明）。
		return nextResult{Next: "forge task complete", Reason: "三门禁与审查齐备——完结并评分（此后手工/finish 合并分支）", State: state}
	}
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

// stAcceptancePending 报告是否有验收标准尚未实跑（AcceptedHeadCommit 为空）。
// 未登记验收标准的任务直接跳过 verify-acceptance 引导。
func stAcceptancePending(st *taskpipeline.TaskState) bool {
	if st == nil {
		return false
	}
	for _, a := range st.Acceptance {
		if a.AcceptedHeadCommit == "" {
			return true
		}
	}
	return false
}

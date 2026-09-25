package taskpipeline

// mutationgate.go — delivery-hardening 硬-1b：mutation 复发升硬。工具不被
// 消费的问题用消费的复发证据治（recurrent.go 框架，test-coverage 先例）：
// 项目连续多个带 Go 源码改动的任务完成而全程零 mutation 证据 → 本次 complete
// 硬前置"至少一条 mutation-sampling 行或显式逃生"。首发只按 streak 计数
//（不含测试维度低分条件——审查 P2-2：注释曾多写一项未实现的触发轴，如实
// 收窄为单轴）；advisory 首发轻推，硬门只在复发证据成立时咬人。

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameMutationGate 是 recurrence 升硬判定行（taskpipeline 字面量）。
const CheckNameMutationGate checklog.CheckName = "mutation-gate"

// mutationGateDisableEnv 是升硬门的逃生舱（沿 FORGE_TEST_COVERAGE 模式）。
const mutationGateDisableEnv = "FORGE_MUTATION_GATE"

// mutationRecurrenceThreshold 是连续零 mutation 完成次数的升硬阈值。
const mutationRecurrenceThreshold = 3

// mutationRanForTask 报告任务的 checklog 是否含 mutation-sampling 行。
func mutationRanForTask(root, taskRef string) bool {
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Check == CheckNameMutationSampling {
			return true
		}
	}
	return false
}

// historicalGoChanged 报告历史任务【其 HeadCommit 以来的已提交改动】里是否含
// Go 非测试源码（审查 P1-4：不含当前工作树——完成时点的未提交脏区不属于该
// 任务的归因；HeadCommit 空的历史任务无法归因，跳过）。
func historicalGoChanged(root string, s *TaskState) bool {
	if s.HeadCommit == "" {
		return false
	}
	// 复审 P1-4 修正：双 revision 形态 <commit>..HEAD = 纯已提交口径（单
	// revision 形态是 commit vs 工作树，未提交 tracked 修改仍会泄入）。
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", s.HeadCommit+"..HEAD").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := filepath.ToSlash(strings.TrimSpace(line))
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") &&
			!strings.Contains(f, "/vendor/") && !strings.HasPrefix(f, "vendor/") {
			return true
		}
	}
	return false
}

// CheckMutationRecurrence 是 task-complete 的 mutation 消费前置（硬-1b）：
// 本任务改动了 Go 源码且未跑 mutation 时，统计项目历史【已完成】任务里同类
// （Go 源码改动 + 零 mutation）的连续次数；≥ 阈值 → 硬拦（出路：跑
// forge task mutation 或逃生）；否则 advisory 提醒（首发轻推）。
// 遥测/checklog 读取失败 → fail-open（区分无法验证与验证通过）。
func CheckMutationRecurrence(root string, state *TaskState) (ok bool, reasons []string, advisory string) {
	if state == nil || state.IsGeneric() {
		return true, nil, ""
	}
	if len(changedGoSourceFiles(root, state)) == 0 {
		return true, nil, "" // 无 Go 源码改动：mutation 无的放矢
	}
	if mutationRanForTask(root, state.TaskRef) {
		return true, nil, "" // 本任务跑过：消费成立
	}
	if escapeDisabled(state, escapeMutationGate, mutationGateDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: mutation recurrence gate bypassed (FORGE_MUTATION_GATE=disable); 假绿面（断言强度）未经变异检验——review 须核查`,
			Meta:    map[string]string{"escape.gate": "mutation-gate", "escape.reason": checklog.EscapeReasonEnv, "escape.owner": "env"},
		})
		return true, nil, ""
	}
	// 复发计数（审查 P1-3/P1-4 重写）：按 CompletedAt 降序遍历（文件名字序与
	// 完成时间无关，曾致 streak 虚拦/虚放）；每个历史任务只看其 HeadCommit 以来的
	// 已提交改动（不含当前工作树）。
	streak := 0
	all, err := ListTaskStates(root)
	if err != nil {
		return true, nil, "" // 读不了历史 → fail-open
	}
	// 复审 P1-3 加固：ListTaskStates 返回全部 state（含进行中任务 CompletedAt
	// == nil）——比较器对 nil 解引用会 panic（当前任务自己在列表里）。先过滤
	// 已完成再排序。
	completed := make([]*TaskState, 0, len(all))
	for _, s := range all {
		if s.CompletedAt != nil {
			completed = append(completed, s)
		}
	}
	sort.Slice(completed, func(i, j int) bool {
		return completed[i].CompletedAt.After(*completed[j].CompletedAt)
	})
	all = completed
	for _, s := range all {
		if s.TaskRef == state.TaskRef || s.CompletedAt == nil {
			continue
		}
		if !historicalGoChanged(root, s) {
			continue // 不含 Go 源码改动的任务不计入 streak
		}
		if mutationRanForTask(root, s.TaskRef) {
			break // 最近一个跑过 mutation 的完成任务截断 streak
		}
		streak++
	}
	hint := "假绿治理工具未被消费：forge task mutation（有配对测试的改动文件抽样变异，默认 3 样本）"
	if streak < mutationRecurrenceThreshold {
		return true, nil, fmt.Sprintf("mutation 零消费连续 %d/%d 个任务——%s", streak, mutationRecurrenceThreshold, hint)
	}
	return false, []string{fmt.Sprintf(
		"mutation 零消费连续 %d 个任务（≥ 阈值 %d）——断言强度从未经变异检验（复发升硬，test-coverage 同款框架）。出路：跑 forge task mutation 后 complete；逃生（落 checklog 审计，降 evidence）: FORGE_MUTATION_GATE=disable",
		streak, mutationRecurrenceThreshold)}, ""
}

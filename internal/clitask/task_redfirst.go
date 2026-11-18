package clitask

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// CheckRedFirst 核对「先红后绿」：每条 --accept-red 命令必须在开工时确实失败，
// 且同一 Run 的 --accept 判据现在通过。
//
// 这是机制能证明「测试先行」的唯一形态：它不猜 git 提交顺序、不看提交信息，只看
// 开工时实跑留下的退出码，以及此刻的绿判据。按 as 空（老任务）时零行为变化。
//
// CheckRedFirst verifies the red-before-green fact: every --accept-red command must
// have failed at task start, and the green criterion with the same Run must pass now.
func CheckRedFirst(root string, s *tasktypes.TaskState) error {
	if s == nil || len(s.RedAttempts) == 0 {
		return nil
	}
	head := taskpipeline.GetHeadCommit(root)
	for _, r := range s.RedAttempts {
		run := strings.TrimSpace(r.Run)
		if run == "" {
			continue
		}
		if r.Exit == 0 {
			if s.AllowGreen {
				continue
			}
			return fmt.Errorf("先失败证明缺失：%q 在开工时就已经通过（exit 0）——它证明不了新行为先失败过。换一条针对新行为的判据，或下一次用 --allow-green 显式留痕", run)
		}
		if r.Head != "" && head != "" && !isGitAncestor(root, r.Head, head) {
			return fmt.Errorf("先失败证明缺失：%q 的红记录来自 %s，它不在当前分支历史里——红证据必须来自本任务开工时的同一历史", run, shortSHA(r.Head))
		}
		if !hasPassingGreen(s, run) {
			return fmt.Errorf("先失败证明未闭合：%q 开工时确实失败过，但同一 run 的绿判据现在没有通过记录。先跑 `forge task verify-acceptance` 让它转绿（task complete 不接受只红不绿）", run)
		}
	}
	return nil
}

// hasPassingGreen 找同一 Run 且已通过的绿判据。Run 按去空白后整串比较——
// 预跑与 verify-acceptance 用的是同一字符串，不做模糊匹配。
func hasPassingGreen(s *tasktypes.TaskState, run string) bool {
	for _, c := range s.Acceptance {
		if strings.TrimSpace(c.Run) == run && c.Passed {
			return true
		}
	}
	return false
}

// isGitAncestor 报告 ancestor 是否是 head 的祖先（含相等）。
func isGitAncestor(root, ancestor, head string) bool {
	c := exec.Command("git", "merge-base", "--is-ancestor", ancestor, head)
	c.Dir = root
	return c.Run() == nil
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// requireCriteriaForChange 是 F1 的机械保证：带 --change 的任务在开工时就必须
// 有至少一条验收判据（change 三份产物已经把「怎样算解决」写死在实现之前，
// 这里把同一件事落到 task 考卷上）。--allow-empty-criteria 是留痕例外。
func requireCriteriaForChange(changeID string, criteria []tasktypes.AcceptanceCriterion, allowEmpty bool, s *tasktypes.TaskState) error {
	if changeID == "" || len(criteria) > 0 {
		return nil
	}
	if allowEmpty {
		s.AllowEmptyCriteria = true
		return nil
	}
	return fmt.Errorf("task start 拒绝：带 --change 启动的任务必须至少登记一条验收判据（`--accept \"run :: expected\"`）——判据要写在实现之前，否则「先有判据」无法证明。确实没有可执行判据时用 --allow-empty-criteria 显式留痕")
}

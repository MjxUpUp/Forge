package clitask

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

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
		if strings.TrimSpace(r.Head) != strings.TrimSpace(s.HeadCommit) {
			return fmt.Errorf("先失败证明未绑定开工时刻：红快照 head=%s，任务开工 head=%s——红快照必须由 task start 当场拍下（事后 checkout 回基线补拍的记录不算）", shortSHA(r.Head), shortSHA(s.HeadCommit))
		}
		if older, err := commitsOlderThan(root, s.HeadCommit, s.StartedAt); err == nil && older != "" {
			return fmt.Errorf("先失败证明可疑：%s 的提交早于任务开工时间（红快照是事后补拍的）——请在开工前登记红判据，或对回归修复用 --allow-green", older)
		}
		if r.Head != "" && head != "" && !isGitAncestor(root, r.Head, head) {
			return fmt.Errorf("先失败证明缺失：%q 的红记录来自 %s，它不在当前分支历史里——红证据必须来自本任务开工时的同一历史", run, shortSHA(r.Head))
		}
		if !hasPassingGreen(s, run) {
			return fmt.Errorf("先失败证明未闭合：%q 开工时确实失败过，但同一 run 的绿判据现在没有通过记录（go test 判据会按 -v 归一化后比较）。确认红判据与绿判据是同一条命令，且绿判据已在 verify-acceptance 里通过", run)
		}
	}
	return nil
}

// hasPassingGreen 找同一 Run 且已通过的绿判据。两侧都过 NormalizeGoTestRun：
// task start 会把带 expected 的 `go test …` 改写成 `go test -v …`，而 --accept-red
// 原样记录——整串比较会让合法流程永不匹配（review-rfe-1 P1-c 实证）。
func hasPassingGreen(s *tasktypes.TaskState, run string) bool {
	want := taskpipeline.NormalizeGoTestRun(run)
	for _, c := range s.Acceptance {
		if taskpipeline.NormalizeGoTestRun(c.Run) == want && c.Passed {
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

// commitsOlderThan 返回 startHead..HEAD 里 committer 时间早于 startedAt 的提交
// （短 sha，顿号分隔）。红快照必须诚实：如果实现早就提交过（先实现、再 checkout
// 回基线 start 的补拍场景），这些提交的时间戳会早于 StartedAt，git 本身能证伪。
func commitsOlderThan(root, startHead string, startedAt time.Time) (string, error) {
	if startedAt.IsZero() || strings.TrimSpace(startHead) == "" {
		return "", nil
	}
	start := startedAt
	// --first-parent：只看主线提交。合并一条开工前就存在的分支是常规操作，
	// 把它的第二父历史也算进来会误判成「事后补拍」（review-rfe-1 P2-b）。
	out, err := exec.Command("git", "-C", root, "log", "--first-parent", "--format=%h %ct", startHead+"..HEAD").Output()
	if err != nil {
		return "", err
	}
	var old []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		var ts int64
		if _, err := fmt.Sscanf(parts[1], "%d", &ts); err != nil {
			continue
		}
		// 容差 5 秒：秒级时间戳下无法区分同秒提交；2 分钟会让「先实现、回基线
		// 补拍、再快进合并」在 1 秒内通过的攻击（review-rfe-1 P2-a 实测 Score 87）。
		if ts < start.Add(-5*time.Second).Unix() {
			old = append(old, parts[0])
		}
	}
	return strings.Join(old, "、"), nil
}

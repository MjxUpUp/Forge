package clitask

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_wild.go —— `forge task wild`：野外动作申报（vNext P1-1，INV-1 覆盖不变量的
// 合法出口之一）。
//
// 定位（vNext 设计 M3，2026-08-31）：INV-1 要求任何副作用动作必须归属（task ref /
// 已批准相位 / 显式野外申报，三者居一）。wild 申报是"无归属"一侧的诚实出口——
// 比静默绕过诚实（显式、留痕、计数），比强制建任务轻（一次性小改动不必背负三道
// 门禁）。它不是免检通道：申报记录进 DataDir 的 wild/declarations.jsonl，累计超限
// 触发审计（P2 接线；v1 先落数据）。
//
// 命名说明：vNext 设计稿原名 `forge act --wild`，但 `forge act` 已被 PDCA Act
// 结论命令占用，故挂 task 子命令——语义上也更近：它声明的是"任务管道之外的动
// 作"，与 task start 同层对偶。
func init() {
	Root.AddCommand(taskWildCmd)
}

var taskWildCmd = &cobra.Command{
	Use:   `wild "<说明>"`,
	Short: `申报一次野外动作（任务管道外的显式留痕——无归属动作的诚实出口）`,
	Long: `Declare a wild action: an out-of-pipeline side effect you are about to make
(or just made) deliberately, with an explicit note.

显式、留痕、计数：比静默绕过诚实，比强制建任务轻。声明记录进用户级数据目录
wild/declarations.jsonl（会话/分支/HEAD/是否已有活跃任务），累计超限触发审计。
适合：一次性小修、紧急 hotfix、实验性改动——事后可被 forge audit 回溯对账。`,
	Args: cobra.ExactArgs(1),
	RunE: RunTaskWild,
}

// WildDeclaration is one persisted wild-declaration record; enforcement reads
// the ledger to audit off-task source writes.
//
// WildDeclaration 是一条野外申报的落盘结构（enforcement 审计消费）。TaskActive 冗余记录申报时刻是否已有
// 活跃任务——有任务却申报 wild 是更强的异常信号（审计维度）。
type WildDeclaration struct {
	TS         string `json:"ts"`
	Session    string `json:"session"`
	Note       string `json:"note"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
	TaskActive bool   `json:"task_active"`
}

// RunTaskWild is the `forge task wild` entry — deliberate off-task source-write
// declaration; next_wild_test exercises it end-to-end from the next domain.
//
// RunTaskWild 是 `forge task wild` 入口——对任务外源码写入的显式申报；
// next 域的 next_wild_test 经它做端到端演练。
func RunTaskWild(cmd *cobra.Command, args []string) error {
	note := strings.TrimSpace(args[0])
	if note == "" {
		return fmt.Errorf("wild 申报必须带一句说明（做了什么、为什么不在任务内做）")
	}
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	dir := filepath.Join(forgedata.DataDirFor(root), "wild")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entry := WildDeclaration{
		TS:      time.Now().Format(time.RFC3339),
		Session: taskpipeline.CurrentSessionID(),
		Note:    note,
		Branch:  GitLine(root, "rev-parse", "--abbrev-ref", "HEAD"),
		Head:    GitLine(root, "rev-parse", "HEAD"),
	}
	if st, terr := taskpipeline.ActiveTaskState(root, entry.Session); terr == nil {
		entry.TaskActive = st != nil
	}
	// 补救语义（P3 审查 #1）：wild 申报是 task-guard 谱系的合法出口——申报即声明
	// "接下来的无任务编辑是刻意的"。清掉本会话的计数/窗口/违规标记（session 键控，
	// 与 hook 写端同名），否则第 1 次 advisory 后已申报补救的 agent 在第 2 次编辑仍
	// 被升档开窗，且窗口补救判据不成立 → 窗口耗尽落伪违规，污染 window_violations
	// 审计。删除失败不阻断申报（计数器残留最坏是多提示一次）。
	if sid := entry.Session; sid != "" {
		markers := filepath.Join(forgedata.DataDirFor(root), "markers")
		for _, prefix := range []string{
			"forge-taskguard-ignores-", "forge-taskguard-window-",
			"forge-taskguard-window-opened-", "forge-taskguard-violation-",
		} {
			_ = os.Remove(filepath.Join(markers, prefix+sid))
		}
	}

	f, err := os.OpenFile(filepath.Join(dir, "declarations.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(entry); err != nil {
		return err
	}
	n := countWildDeclarations(dir, entry.Session)
	scope := "本会话"
	if entry.Session == "" {
		scope = "本机累计（无会话身份——宿主未注入 session，所有匿名申报共享计数）"
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"已记录野外申报（第 %d 条，%s）：%s\n分支 %s @ %s；审计可经 wild/declarations.jsonl 回溯。\n",
		n, scope, note, entry.Branch, shortHead(entry.Head))
	return nil
}

// gitLine 跑一条只读 git 命令取首行，失败返回 "?"（申报不因 git 环境缺失去败）。
func GitLine(dir string, args ...string) string {
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(out))
}

func shortHead(head string) string {
	if len(head) > 8 {
		return head[:8]
	}
	return head
}

// countWildDeclarations 数本会话已累计的申报条数（超限审计的 P2 钩子在此消费）。
func countWildDeclarations(dir, session string) int {
	data, err := os.ReadFile(filepath.Join(dir, "declarations.jsonl"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var e WildDeclaration
		if json.Unmarshal([]byte(line), &e) == nil && e.Session == session {
			n++
		}
	}
	return n
}

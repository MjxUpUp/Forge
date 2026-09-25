package clitask

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_artifacts.go —— 三段工件的写入口（vNext P3，设计 M5 边界物体分层）：
// intent 段（追加式注记，永不覆写）/ checklist 段（勾选式对账单，complete 硬门禁）/
// invariants 段（--invariant，声明期校验后映射进 Acceptance 走既有机器对账）。
func init() {
	Root.AddCommand(taskIntentCmd)
	Root.AddCommand(taskChecklistCmd)
	taskChecklistCmd.AddCommand(taskChecklistAddCmd, taskChecklistTickCmd, taskChecklistDropCmd)
	taskStartCmd.Flags().StringArray("invariant", nil, `析出不变量（可重复）：格式同 --accept（"run :: expected" 或裸 "run"）——必须是可执行命令，叙述性约束会被拒绝（改用 checklist/intent）；声明后作为验收标准实跑对账，complete 前置 freshness 全覆盖`)
}

var taskIntentCmd = &cobra.Command{
	Use:   `intent "<注记>"`,
	Short: "追加一条意图注记（append-only——意图历史即决策史，永不覆写）",
	Args:  cobra.ExactArgs(1),
	RunE:  RunIntentCmd,
}

// RunIntentCmd is the `forge task intent` entry, exported for the artifacts E2E
// test that stays in cli (hook-domain fixtures).
//
// RunIntentCmd 是 `forge task intent` 入口；导出供留守 cli 的 artifacts E2E
// 测试消费（其夹具是 hook 域的）。
func RunIntentCmd(cmd *cobra.Command, args []string) error {
	text := strings.TrimSpace(args[0])
	if text == "" {
		return fmt.Errorf("intent 注记不能为空（写 why/约束背景，给人审读）")
	}
	n := 0
	if err := mutateActiveTask(func(st *taskpipeline.TaskState) error {
		st.IntentLog = append(st.IntentLog, taskpipeline.IntentEntry{
			TS:      time.Now(),
			Text:    text,
			Session: taskpipeline.CurrentSessionID(),
		})
		n = len(st.IntentLog)
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "intent 追加第 %d 条（append-only，无覆写入口）：%s\n", n, text)
	return nil
}

var taskChecklistCmd = &cobra.Command{
	Use:   "checklist",
	Short: "操作对账单（勾选即进度——task-complete 硬门禁要求全勾）",
}

var taskChecklistAddCmd = &cobra.Command{
	Use:   `add "<条目>"`,
	Short: "追加一条 checklist 项",
	Args:  cobra.ExactArgs(1),
	RunE:  RunChecklistAdd,
}

func RunChecklistAdd(cmd *cobra.Command, args []string) error {
	desc := strings.TrimSpace(args[0])
	if desc == "" {
		return fmt.Errorf("checklist 条目不能为空")
	}
	next, rem := 0, 0
	if err := mutateActiveTask(func(st *taskpipeline.TaskState) error {
		// ID 取 max+1（审查 #7：以尾条为基准会在删尾后复用 ID，stale tick 勾错新条目）。
		for _, c := range st.Checklist {
			if c.ID >= next {
				next = c.ID
			}
		}
		next++
		st.Checklist = append(st.Checklist, taskpipeline.ChecklistItem{ID: next, Desc: desc})
		rem = len(st.UntickedChecklist())
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "checklist #%d 已追加（未勾 %d 项——task-complete 前须全勾）\n", next, rem)
	return nil
}

var taskChecklistTickCmd = &cobra.Command{
	Use:   "tick <id>",
	Short: "勾选一条 checklist 项（随做随勾，勿最后批量补）",
	Args:  cobra.ExactArgs(1),
	RunE:  RunChecklistTick,
}

func RunChecklistTick(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return fmt.Errorf("checklist tick 需要数字 id（forge task status --json 查看），got %q", args[0])
	}
	rem := 0
	if err := mutateActiveTask(func(st *taskpipeline.TaskState) error {
		now := time.Now()
		hit := false
		for i := range st.Checklist {
			if st.Checklist[i].ID == id {
				st.Checklist[i].Done = true
				st.Checklist[i].DoneAt = &now
				hit = true
			}
		}
		if !hit {
			return fmt.Errorf("checklist #%d 不存在（forge task status --json 查看当前清单）", id)
		}
		rem = len(st.UntickedChecklist())
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "checklist #%d 已勾（剩余未勾 %d 项）\n", id, rem)
	return nil
}

var taskChecklistDropCmd = &cobra.Command{
	Use:   "drop <id>",
	Short: "删除一条 checklist 项（确认不需要才删——删的是对账项不是事实）",
	Args:  cobra.ExactArgs(1),
	RunE:  RunChecklistDrop,
}

func RunChecklistDrop(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return fmt.Errorf("checklist drop 需要数字 id，got %q", args[0])
	}
	left := 0
	if err := mutateActiveTask(func(st *taskpipeline.TaskState) error {
		out := st.Checklist[:0]
		dropped := false
		for _, c := range st.Checklist {
			if c.ID == id {
				dropped = true
				continue
			}
			out = append(out, c)
		}
		if !dropped {
			return fmt.Errorf("checklist #%d 不存在", id)
		}
		st.Checklist = out
		left = len(st.Checklist)
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "checklist #%d 已删除（剩余 %d 项）\n", id, left)
	return nil
}

// mutateActiveTask 在 per-task 锁内重载-变更-保存当前活跃任务（§13 丢更新防护，
// 与 verify-acceptance/resume/impact 同模式——审查 #3：裸读-改-写会与并发
// verify-acceptance 回写交错丢更新）。探测活跃任务用 ActiveTaskState，变更走
// MutateTaskState；fn 返回错误则整个变更不落盘。
func mutateActiveTask(fn func(*taskpipeline.TaskState) error) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	st, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("无活跃任务——工件命令作用于当前任务（forge task start / forge next）")
	}
	return taskpipeline.MutateTaskState(root, st.TaskRef, fn)
}

// validateInvariant 在声明期校验析出不变量（设计 M5：合同析出段必须映射到可执行
// validator，映射不到的**显式拒绝**并指引降级——降级发生在声明时而非跑时失败后）。
// 判据只算 :: 左侧 Run 段（审查 #4）：Expected 本就是自由文本子串匹配，长中文
// 期望不应连累命令段被误判。CJK 主导的 Run 段按叙述性约束处理（命令语言天然
// ASCII 主导）。
// ValidateInvariant checks an --invariant declaration is executable-command
// shaped (same rule as --accept).
//
// ValidateInvariant 校验 --invariant 声明是可执行命令形态（与 --accept 同规）。
// 导出原因：task_artifacts 的 E2E 测试留守 cli（其夹具是 hook 域的
// runHook/newTaskGuardProject），跨包消费本助手。
func ValidateInvariant(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("invariant 不能为空")
	}
	if i := strings.Index(v, "::"); i >= 0 {
		v = v[:i]
	}
	cjk, total := 0, 0
	for _, r := range v {
		if unicode.Is(unicode.Han, r) {
			cjk++
		}
		if !unicode.IsSpace(r) {
			total++
		}
	}
	if total > 0 && cjk*2 > total {
		return fmt.Errorf("invariant 的命令段 %q 看起来是叙述性约束而非可执行命令——析出段必须映射到 validator（可执行命令）；叙述性约束请用 forge task checklist add（对账单）或 forge task intent（意图注记）承载", v)
	}
	return nil
}

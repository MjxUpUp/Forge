package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_artifacts.go —— 三段工件的写入口（vNext P3，设计 M5 边界物体分层）：
// intent 段（追加式注记，永不覆写）/ checklist 段（勾选式对账单，complete 硬门禁）/
// invariants 段（--invariant，声明期校验后映射进 Acceptance 走既有机器对账）。
func init() {
	taskCmd.AddCommand(taskIntentCmd)
	taskCmd.AddCommand(taskChecklistCmd)
	taskChecklistCmd.AddCommand(taskChecklistAddCmd, taskChecklistTickCmd, taskChecklistDropCmd)
	taskStartCmd.Flags().StringArray("invariant", nil, `析出不变量（可重复）：格式同 --accept（"run :: expected" 或裸 "run"）——必须是可执行命令，叙述性约束会被拒绝（改用 checklist/intent）；声明后作为验收标准实跑对账，complete 前置 freshness 全覆盖`)
}

var taskIntentCmd = &cobra.Command{
	Use:   `intent "<注记>"`,
	Short: "追加一条意图注记（append-only——意图历史即决策史，永不覆写）",
	Args:  cobra.ExactArgs(1),
	RunE:  runIntentCmd,
}

func runIntentCmd(cmd *cobra.Command, args []string) error {
	text := strings.TrimSpace(args[0])
	if text == "" {
		return fmt.Errorf("intent 注记不能为空（写 why/约束背景，给人审读）")
	}
	st, root, err := loadActiveTaskForMutation()
	if err != nil {
		return err
	}
	st.IntentLog = append(st.IntentLog, taskpipeline.IntentEntry{
		TS:      time.Now(),
		Text:    text,
		Session: taskpipeline.CurrentSessionID(),
	})
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "intent 追加第 %d 条（append-only，无覆写入口）：%s\n", len(st.IntentLog), text)
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
	RunE:  runChecklistAdd,
}

func runChecklistAdd(cmd *cobra.Command, args []string) error {
	desc := strings.TrimSpace(args[0])
	if desc == "" {
		return fmt.Errorf("checklist 条目不能为空")
	}
	st, root, err := loadActiveTaskForMutation()
	if err != nil {
		return err
	}
	next := 1
	if n := len(st.Checklist); n > 0 {
		next = st.Checklist[n-1].ID + 1
	}
	st.Checklist = append(st.Checklist, taskpipeline.ChecklistItem{ID: next, Desc: desc})
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		return err
	}
	rem := len(st.UntickedChecklist())
	fmt.Fprintf(cmd.OutOrStdout(), "checklist #%d 已追加（未勾 %d 项——task-complete 前须全勾）\n", next, rem)
	return nil
}

var taskChecklistTickCmd = &cobra.Command{
	Use:   "tick <id>",
	Short: "勾选一条 checklist 项（随做随勾，勿最后批量补）",
	Args:  cobra.ExactArgs(1),
	RunE:  runChecklistTick,
}

func runChecklistTick(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return fmt.Errorf("checklist tick 需要数字 id（forge task status 查看），got %q", args[0])
	}
	st, root, err := loadActiveTaskForMutation()
	if err != nil {
		return err
	}
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
		return fmt.Errorf("checklist #%d 不存在（forge task status 查看当前清单）", id)
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "checklist #%d 已勾（剩余未勾 %d 项）\n", id, len(st.UntickedChecklist()))
	return nil
}

var taskChecklistDropCmd = &cobra.Command{
	Use:   "drop <id>",
	Short: "删除一条 checklist 项（确认不需要才删——删的是对账项不是事实）",
	Args:  cobra.ExactArgs(1),
	RunE:  runChecklistDrop,
}

func runChecklistDrop(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return fmt.Errorf("checklist drop 需要数字 id，got %q", args[0])
	}
	st, root, err := loadActiveTaskForMutation()
	if err != nil {
		return err
	}
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
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "checklist #%d 已删除（剩余 %d 项）\n", id, len(st.Checklist))
	return nil
}

// loadActiveTaskForMutation 取当前活跃任务（含项目根）供工件命令变更并保存。
func loadActiveTaskForMutation() (*taskpipeline.TaskState, string, error) {
	root, err := findProjectRoot()
	if err != nil {
		return nil, "", err
	}
	st, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return nil, "", err
	}
	if st == nil {
		return nil, "", fmt.Errorf("无活跃任务——工件命令作用于当前任务（forge task start / forge next）")
	}
	return st, root, nil
}

// validateInvariant 在声明期校验析出不变量（设计 M5：合同析出段必须映射到可执行
// validator，映射不到的**显式拒绝**并指引降级——降级发生在声明时而非跑时失败后）。
// 判据：内容以 CJK 为主的条目按叙述性约束处理（命令语言天然 ASCII 主导；误伤面
// 是含大量中文注释的命令——罕见，且报错文案给出保留方式：用引号包命令或改 accept）。
func validateInvariant(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("invariant 不能为空")
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
		return fmt.Errorf("invariant %q 看起来是叙述性约束而非可执行命令——析出段必须映射到 validator（可执行命令）；叙述性约束请用 forge task checklist add（对账单）或 forge task intent（意图注记）承载", v)
	}
	return nil
}

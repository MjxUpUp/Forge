package clitask

// task_watchdog.go — `forge task watchdog`（focus-batches §2d，方向 C always-on
// 治理）：长时任务停滞检测。2026 事故年（$6k 过夜账单 / 僵尸会话）的共同线程是
// "任务在跑但没人看"——watchdog 从两份台账（checklog/toollog）取每个未完成任务的
// 最后活动时间，超阈报停滞（marker 节流：每任务每小时至多一条 advisory，防轰炸）。
// token 熔断已有（TaskTokenBreaker），此处顺带展示。
//
// --release：对停滞且持有他机/过期租约的任务清租约（多机双跑的前兆处置）。
// v1 的停滞判定不杀进程（红线：不替代 agent 循环）——advisory + 状态可见性。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/spf13/cobra"
)

func init() {
	taskWatchdogCmd.Flags().Duration("stall", 45*time.Minute, "停滞阈值（最后活动距今超过该时长报停滞）")
	taskWatchdogCmd.Flags().Bool("release", false, "对停滞任务清租约（多机双跑处置）")
	Root.AddCommand(taskWatchdogCmd)
}

var taskWatchdogCmd = &cobra.Command{
	Use:   "watchdog [--stall 45m] [--release]",
	Short: "长时任务停滞检测（always-on 治理：最后活动超阈报停滞 + 可选清租约）",
	Long: `forge task watchdog 扫描未完成任务，从 checklog/toollog 两份台账取每个任务的
最后活动时间（最后一条带该 TaskRef 的记录），距今超过 --stall 阈值即报停滞：

- 停滞事件落 checklog（task-stalled，advisory；marker 节流每任务每小时至多一条）
- --release：停滞且持有租约的任务清租约（他机认领后失联的双跑处置）
- 顺带展示 token 熔断信号（TaskTokenBreaker，已有机制）

适用场景：overnight/watch 模式、多 agent 分派的僵尸认领（claimed 后失联）。`,
	RunE: runTaskWatchdog,
}

// stalledTask 是单个停滞任务的判定快照。
type stalledTask struct {
	Ref        string
	LastActive time.Time
	Idle       time.Duration
	HasLease   bool
	TokenWarn  string
}

func runTaskWatchdog(cmd *cobra.Command, args []string) error {
	stall, _ := cmd.Flags().GetDuration("stall")
	doRelease, _ := cmd.Flags().GetBool("release")
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return err
	}
	now := time.Now()
	var stalled []stalledTask
	checked := 0
	for _, s := range states {
		if s.CompletedAt != nil {
			continue
		}
		checked++
		last := lastActivityFor(root, s.TaskRef, s.StartedAt)
		if idle := now.Sub(last); idle > stall {
			warn, _ := toolusage.TaskTokenBreaker(root, s.TaskRef)
			stalled = append(stalled, stalledTask{
				Ref: s.TaskRef, LastActive: last, Idle: idle,
				HasLease: s.Lease != nil, TokenWarn: warn,
			})
		}
	}
	fmt.Printf("watchdog：%d 个未完成任务，%d 个停滞（阈值 %s）\n", checked, len(stalled), stall)
	for _, t := range stalled {
		fmt.Printf("  ⏸ %-28s 最后活动 %s 前（%s）", t.Ref, formatDuration(t.Idle), t.LastActive.Format("01-02 15:04"))
		if t.HasLease {
			fmt.Print(" · 持有租约")
		}
		fmt.Println()
		if t.TokenWarn != "" {
			fmt.Printf("     [breaker] %s\n", t.TokenWarn)
		}
		recordStalled(root, t, now)
		if doRelease && t.HasLease {
			if err := taskpipeline.MutateTaskState(root, t.Ref, func(s *taskpipeline.TaskState) error {
				s.Lease = nil
				return nil
			}); err != nil {
				fmt.Fprintf(os.Stderr, "⚠ 清租约失败 %s: %v\n", t.Ref, err)
			} else {
				fmt.Printf("     ↩ 租约已清（%s）\n", t.Ref)
			}
		}
	}
	return nil
}

// lastActivityFor 取任务最后活动：checklog 与 toollog 该 TaskRef 最新时间戳的较大者；
// 都没有时回落任务 StartedAt（刚开的任务不该立即算停滞）。
func lastActivityFor(root, ref string, startedAt time.Time) time.Time {
	last := startedAt
	if entries, err := checklog.LoadForTask(root, ref); err == nil {
		for _, e := range entries {
			if e.RecordedAt.After(last) {
				last = e.RecordedAt
			}
		}
	}
	if calls, err := toolusage.LoadForTaskAll(root, ref); err == nil {
		for _, c := range calls {
			if c.Timestamp.After(last) {
				last = c.Timestamp
			}
		}
	}
	return last
}

// recordStalled 落停滞 advisory（marker 节流：DataDir/markers/task-stalled-<ref>-<yymmddHH>，
// 每任务每小时至多一条——cron 式反复扫描不轰炸 checklog）。
func recordStalled(root string, t stalledTask, now time.Time) {
	marker := filepath.Join(markerDir(root), fmt.Sprintf("task-stalled-%s-%s.marker", sanitizeRef(t.Ref), now.Format("06010215")))
	if _, err := os.Stat(marker); err == nil {
		return // 本小时已记
	}
	// markers 目录可能从未创建（首次 watchdog 运行）——WriteFile 不自建父目录，
	// 静默失败会让节流永远不生效（每轮重扫都落行）。
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(marker, []byte(now.Format(time.RFC3339)), 0o644); err != nil {
		return
	}
	_ = checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckTaskStalled,
		Passed:  false,
		Checked: true,
		Level:   checklog.LevelWarn,
		TaskRef: t.Ref,
		Detail:  fmt.Sprintf("ADVISORY: 任务停滞 %s（最后活动 %s）——always-on 会话失联/僵尸认领信号；forge task resume 接续或 forge task fail 收尾", formatDuration(t.Idle), t.LastActive.Format("01-02 15:04")),
	})
}

func markerDir(root string) string {
	return filepath.Join(forgedata.DataDirFor(root), "markers")
}

func sanitizeRef(ref string) string {
	return strings.ReplaceAll(strings.ReplaceAll(ref, "/", "__"), "\\", "__")
}

func formatDuration(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.0fm", d.Minutes())
}

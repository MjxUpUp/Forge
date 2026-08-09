package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_health.go: the observability surface for delegation (design §12/§16 phase 5 可观测).
// `forge task health` scans the whole project for delegation trouble and REPORTS it — it never
// mutates state. Three classes of problem are surfaced:
//   - zombies: offered unclaimed >7d / claimed with the claimer gone >7d / input-required
//     unanswered >7d / a task reclaimed (abandoned) ≥2 times. Shown yellow elsewhere (mine,
//     dashboard); here they are listed with their reason.
//   - deadlocks: a DependsOn pointing at a failed/canceled/missing task (废弃链) — the dependent
//     can never proceed.
//   - dependency cycles: rejected at AddDependency write time, but import/corruption can still
//     introduce one; reported defensively.
//
// Healthy tasks are omitted from the human output (the point is to surface what's stuck); the
// JSON output likewise lists only flagged tasks. Detection is the shared taskpipeline.IsZombie /
// DeadlockedDependency / HasDependencyCycle so mine, dashboard, and health never disagree.
//
// task_health.go：分派的可观测表层（设计 §12/§16 阶段5 可观测）。forge task health 扫描全
// 项目的分派故障并「报告」——绝不改状态。上浮三类问题：
//   - 僵尸：offered 无人认领超 7d / claimed 认领方失联超 7d / input-required 无人答复超 7d /
//     被回收（abandon）≥2 次的任务。在别处（mine、看板）标黄；此处列出并附 reason。
//   - 死锁：DependsOn 指向 failed/canceled/缺失的任务（废弃链）——依赖方永不可推进。
//   - 依赖环：AddDependency 写入时已拒，但 import/损坏仍可能引入；防御性上报。
//
// 健康任务在人类输出中省略（目的是上浮卡住的）；JSON 输出同样只列被标记任务。检测复用共享的
// taskpipeline.IsZombie / DeadlockedDependency / HasDependencyCycle，使 mine、看板、health 永不分歧。
var taskHealthCmd = &cobra.Command{
	Use:   `health [--json]`,
	Short: `扫描全 project 的僵尸/死锁/长期未答复任务（只读告警，不改状态）`,
	Long: `forge task health 扫描当前项目的所有任务，上浮分派层的问题——不修改任何状态。

报告三类问题：
  • 僵尸：offered>7d（无人认领）/ claimed>TTL（认领方失联）/ input-required>7d（无人答复）
          / abandoned_count≥2（反复被回收）。
  • 死锁：DependsOn 指向已 failed/canceled 或已缺失（abort）的任务——依赖方永久阻塞。
  • 依赖环：DependsOn 形成环（写入时本应拒绝；出现即疑似 import/损坏数据）。

真正的 claimed→offered 回收（abandon）由后续 hook 触发；本命令只暴露信号，使卡住的任务不被
静默忽略。检测逻辑与 task mine / 看板共享同一真相源，三处对「僵尸」永不分歧。`,
	RunE: runTaskHealth,
}

func init() {
	taskCmd.AddCommand(taskHealthCmd)
	taskHealthCmd.Flags().Bool(`json`, false, `JSON 格式输出`)
}

// healthRow is one task's health in `task health --json` output. Only flagged tasks appear.
//
// healthRow 是 task health --json 输出中单个任务的健康项。只出现被标记的任务。
type healthRow struct {
	TaskRef        string   `json:"task_ref"`
	Title          string   `json:"title,omitempty"`
	Status         string   `json:"status,omitempty"`
	IsZombie       bool     `json:"is_zombie"`
	ZombieReasons  []string `json:"zombie_reasons,omitempty"`
	Deadlocked     bool     `json:"deadlocked"`
	DeadlockReason string   `json:"deadlock_reason,omitempty"`
}

func runTaskHealth(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool(`json`)
	now := time.Now()

	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return fmt.Errorf(`读取任务列表失败: %w`, err)
	}
	// Build a ref→state index once so dependency-chain and cycle walks resolve in-memory (no
	// per-ref file load during the scan). A task not in the index = missing (aborted/deleted),
	// which DeadlockedDependency treats as a dead-chain root.
	//
	// 一次性建 ref→state 索引，使依赖链与环遍历在内存解析（扫描期间不逐 ref 读文件）。
	// 不在索引中的 task = 缺失（abort/删除），DeadlockedDependency 视作死链根。
	byRef := map[string]*taskpipeline.TaskState{}
	for _, s := range states {
		if s != nil {
			byRef[s.TaskRef] = s
		}
	}
	lookupState := func(ref string) (*taskpipeline.TaskState, error) {
		if s, ok := byRef[ref]; ok {
			return s, nil
		}
		return nil, fmt.Errorf(`task %q not found`, ref)
	}
	lookupCycle := func(ref string) *taskpipeline.TaskState { return byRef[ref] }

	var rows []healthRow
	for _, s := range states {
		if s == nil {
			continue
		}
		h := taskpipeline.ClassifyTaskHealth(root, s, now, lookupState, lookupCycle)
		if !h.IsZombie && !h.Deadlocked {
			continue // healthy — omitted from the report
		}
		row := healthRow{
			TaskRef:        h.TaskRef,
			Title:          s.Summary,
			IsZombie:       h.IsZombie,
			ZombieReasons:  h.ZombieReasons,
			Deadlocked:     h.Deadlocked,
			DeadlockReason: h.DeadlockReason,
		}
		if s.Assignment != nil {
			row.Status = s.Assignment.Status
		}
		rows = append(rows, row)
	}

	if asJSON {
		if rows == nil {
			rows = []healthRow{}
		}
		out, _ := json.MarshalIndent(rows, ``, `  `)
		fmt.Println(string(out))
		return nil
	}
	if len(rows) == 0 {
		fmt.Println(`✓ 未发现僵尸/死锁/长期未答复任务`)
		return nil
	}
	fmt.Printf(`发现 %d 个需关注任务:`, len(rows))
	fmt.Println()
	for _, r := range rows {
		// NOTE: a raw-string format passed to fmt.Printf does NOT interpret \n (the lexer leaves
		// raw strings unescaped, and fmt only handles % verbs) — newlines come from Println(),
		// matching the codebase convention. See task_assignment.go runTaskMine.
		//
		// 注意：传给 fmt.Printf 的 raw-string 格式串不解释 \n（词法器不转义 raw string，fmt 只处理
		// % 动词）——换行来自 Println()，与代码库约定一致。见 task_assignment.go runTaskMine。
		marks := ``
		if r.IsZombie {
			marks += ` ⚠僵尸(` + strings.Join(r.ZombieReasons, `,`) + `)`
		}
		if r.Deadlocked {
			marks += ` 🔒死锁`
		}
		status := r.Status
		if status == `` {
			status = `-`
		}
		fmt.Printf(`  %s  [%s]%s  %s`, r.TaskRef, status, marks, r.Title)
		fmt.Println()
		if r.DeadlockReason != `` {
			fmt.Printf(`      → %s`, r.DeadlockReason)
			fmt.Println()
		}
	}
	fmt.Println()
	fmt.Println(`提示：真正的 claimed→offered 回收（abandon）由后续 hook 触发；本命令只读暴露信号。`)
	return nil
}

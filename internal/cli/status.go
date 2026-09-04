package cli

import (
	"github.com/MjxUpUp/Forge/internal/checklog"

	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/health"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().Bool("json", false, "JSON 格式输出")
	statusCmd.Flags().Bool("system", false, "系统级健康检查")
	// --tasks defaults to true: the task list is the main content of status, shown by default. Pass --tasks=false
	// to hide the task block (Project header + quality signals only), giving the flag real semantics rather than being a dead flag.
	//
	// --tasks 默认 true：任务列表是 status 的主体内容，默认显示。传 --tasks=false
	// 隐藏任务块（只看 Project 头 + 质量信号），让 flag 有真实语义而非 dead flag。
	statusCmd.Flags().Bool("tasks", true, "显示任务列表（默认开启；--tasks=false 隐藏）")
	statusCmd.Flags().Bool("agents", false, "显示检测到的 AI 编码工具")
}

var statusCmd = &cobra.Command{
	Use:   "status [--json] [--system] [--tasks] [--agents]",
	Short: "查看项目状态（任务管道 + 质量信号）",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	asSystem, _ := cmd.Flags().GetBool("system")
	showTasks, _ := cmd.Flags().GetBool("tasks")
	showAgents, _ := cmd.Flags().GetBool("agents")

	if asSystem {
		return runSystemStatus()
	}

	root, err := findProjectRoot()
	if err != nil {
		// declined 项目 Find 失败（IsMember 不认 declined）——把通用的"非 forge
		// 项目"错误换成面向用户的 declined 提示（指向 forge on）。status 非热路径，
		// 一次额外 State 查询可接受。--json 消费者拿 JSON 错误包裹（stdout），
		// cobra 的 Error: 行仍走 stderr 不污染机器可读输出。
		if cwd, gerr := os.Getwd(); gerr == nil {
			if _, state := registry.State(cwd); state == registry.StatusDeclined {
				if asJSON {
					msg, _ := json.Marshal(map[string]string{"error": registry.ErrDeclinedProject.Error(), "takeover": "declined"})
					fmt.Println(string(msg))
				}
				return registry.ErrDeclinedProject
			}
		}
		return err
	}

	// harness repo 状态行 + onboarding 引导（multi-task-concurrency §13：常态显示，
	// cooldown 防重复提示）。JSON 输出不混入 advisory。
	// 接管状态行（Project Policy Layer P1）：能走到这里的状态必然是 managed——
	// declined 已被上方 Find 分支拦截提示，此处无需分叉。
	if !asJSON {
		if home, herr := forgedata.GlobalHome(); herr == nil {
			fmt.Printf("Harness:       %s\n", harnessStateLabel(readHarnessState(home)))
		}
		fmt.Printf("归属覆盖:      %s\n", attributionCoverageLine(root))
		fmt.Printf("自评测:        %s\n", evalHealthLine(root))
		fmt.Printf("接管状态:      managed（forge 接管中）\n")
		MaybeOfferHarness("forge status")
	}

	// status is an aggregate view: a task-list failure must not fail the whole
	// render — warn on stderr and continue rendering the remaining blocks
	// (task.go:1538 / act.go:100 propagate the error because those commands
	// exist solely to list tasks; status degrades instead).
	//
	// status 是聚合视图：任务列表失败不应让整体渲染失败——stderr 告警后继续渲染
	// 其余区块（task.go:1538 / act.go:100 传播 error，因为那些命令的唯一职责就是
	// 列任务；status 降级处理）。
	taskStates, tsErr := taskpipeline.ListTaskStates(root)
	if tsErr != nil {
		fmt.Fprintf(os.Stderr, "warn: 无法列出任务状态（继续渲染其余区块）: %v\n", tsErr)
	}

	// Project-level quality signals (task→project rollup): surface the evidence-blind-spot rate / recurring low-score
	// dimensions at the status main entry. Otherwise deterministic signals computed in forge health are invisible to the
	// user in status (the where-is-the-project main entry) — a visibility gap. Omitted when conclusions is empty (no completed tasks yet).
	//
	// 项目级质量信号（task→project 上卷）：把证据盲区率/复发低分维度亮在 status 主入口。
	// 否则 deterministic 信号在 forge health 里算好了，但用户在 status（「项目在哪」主入口）
	// 看不到——可见性缺口。conclusions 为空时省略（项目还没完成任务）。
	var hs *health.Summary
	if proj, err := forgedata.ProjectFor(root); err == nil {
		if cs, err := act.LoadAll(proj); err == nil && len(cs) > 0 {
			s := health.Summarize(cs)
			hs = &s
		}
	}

	if asJSON {
		// Tasks has no omitempty: an empty task list (tasks: []) is also a valid state, and callers (dashboard/
		// scripts/tests) rely on this field existing for destructuring; omitempty would swallow the whole field when there are no tasks, breaking the contract.
		//
		// Tasks 不加 omitempty：空任务列表（「tasks»: []）也是有效状态，调用方（dashboard/
		// 脚本/测试）依赖该字段存在做解构；omitempty 会在无任务时吞掉整个字段，破坏契约。
		output, _ := json.MarshalIndent(struct {
			Tasks  []*taskpipeline.TaskState `json:"tasks"`
			Health *health.Summary           `json:"health,omitempty"`
		}{taskStates, hs}, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	// By default render the task pipeline status (replacing the old project pipeline rendering — project-level pipeline was removed).
	// Always print the project header: a fresh project (no tasks) running status should not output a blank — otherwise the user
	// cannot tell whether forge is in place. An empty task list is explicitly hinted to guide the next step. --tasks=false hides the task block.
	//
	// 默认显示任务管道状态（取代原 project pipeline 渲染——项目级管道已删除）。
	// 始终打印项目头：fresh 项目（无任务）跑 status 也不应输出空白——否则用户无法
	// 判断 forge 是否已就位。空任务列表显式提示，引导下一步。--tasks=false 隐藏任务块。
	fmt.Printf("Project: %s\n", filepath.Base(root))
	if showTasks {
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("Tasks:")
		if len(taskStates) == 0 {
			fmt.Println("  (no active tasks — `forge task start` to begin)")
		} else {
			for _, ts := range taskStates {
				fmt.Printf("  %s (%s) — ", ts.TaskRef, ts.Branch)
				if ts.CompletedAt != nil {
					fmt.Println("completed")
				} else {
					completed := len(ts.CompletedGates())
					total := len(taskpipeline.DefaultGates())
					fmt.Printf("%d/%d gates passed\n", completed, total)
				}
			}
		}
		fmt.Println(strings.Repeat("─", 60))
	}

	// Show detected agents
	// 显示检测到的 agent
	if showAgents {
		agents := agentbridge.DetectAgents(root)
		fmt.Println()
		fmt.Println("Detected Agents:")
		if len(agents) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, a := range agents {
				fmt.Printf("  %s\n", a)
			}
		}
	}

	// Project-level quality signals (same source as --json Health). A compact block, shown only when there are completed tasks:
	// the blind-spot rate is the headline (project-level LLM-judge blind spot), and recurring low-score dimensions come next. See forge health for full trends.
	//
	// 项目级质量信号（与 --json 的 Health 同源）。compact 一块，只在有完成任务时显示：
	// 盲区率是头条（项目级 LLM-judge 盲区），复发低分维度次之。forge health 看完整趋势。
	if hs != nil {
		fmt.Println()
		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf(`质量信号: %d 任务完成, 均分 %.0f, 证据盲区率 %.0f%%`+"\n",
			hs.TotalTasks, hs.AvgScore, hs.BlindSpotRate*100)
		if hs.BlindSpotRate >= 0.5 {
			fmt.Println(`  ⚠ 系统性盲区：过半完成声明缺 deterministic 证据——project 级该查验证为何没真跑`)
		}
		if len(hs.LowDims) > 0 {
			top := hs.LowDims[0]
			fmt.Printf(`  复发低分维度: %s ×%d（forge health 看全部）`+"\n", top.Dimension, top.Count)
		}
	}

	return nil
}

// evalHealthLine 渲染 `forge status` 的自评测健康行：最近一次 golden 基线摘要
// + 判分器/验签告警有无。数据全部来自 eval-* 观察行（forge eval 命令族落下的
// checklog），无任何现场计算——status 是只读快照入口，评测本体走 forge eval。
func evalHealthLine(root string) string {
	entries, err := checklog.LoadAll(root)
	if err != nil {
		return "未度量"
	}
	var latestGolden *checklog.Entry
	judgeWeak, forged := false, false
	for i := range entries {
		e := &entries[i]
		switch e.Check {
		case checklog.CheckEvalGoldenRun:
			latestGolden = e
		case checklog.CheckEvalJudgeWeak:
			if !e.Passed {
				judgeWeak = true
			}
		case checklog.CheckEvalRun:
			if strings.Contains(e.Detail, "伪造") && !e.Passed {
				forged = true
			}
		}
	}
	if latestGolden == nil {
		if judgeWeak || forged {
			return "有告警（判分器降级/伪造审计行）——forge eval dashboard 查看详情"
		}
		return "未度量（forge eval golden run 建立基线）"
	}
	detail := strings.TrimPrefix(latestGolden.Detail, "golden run: ")
	line := fmt.Sprintf("golden %s（%s）", detail, latestGolden.RecordedAt.Format("01-02 15:04"))
	switch {
	case forged:
		line += " · ⚠ 伪造审计行待溯源（forge eval audit-verify）"
	case judgeWeak:
		line += " · ⚠ 判分器降级 ADVISORY（κ<0.6）"
	}
	return line
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/harnessaudit"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/spf13/cobra"
)

// evalHarnessAuditCmd is `forge eval harness-audit`: the reproducible A–G metrics run over this project's ledgers (design M, docs/design/harness-fixes-a-g-2026-09.md). Loading and rendering only — every judgment lives in internal/harnessaudit as pure functions.
//
// evalHarnessAuditCmd 是 `forge eval harness-audit`：对本项目账本跑 A–G 度量的可复算命令
// （设计 M）。本文件只做加载与渲染——全部判定在 internal/harnessaudit 纯函数里；口径参数
// 随 JSON 输出，两机回测只对同口径的两份 JSON 作差。
var evalHarnessAuditCmd = &cobra.Command{
	Use:   "harness-audit [--json]",
	Short: "A–G harness 修复的事前/事后度量（checklog × toollog × 任务 × hazard，两机同口径）",
	Long: `对本项目 DataDir 的 checklog、toollog（2s Pre/Post 双记去重）、任务状态与 hazard 事件流
一次算出 docs/design/harness-fixes-a-g-2026-09.md 全部修复项的度量：
  A1 日均触发 / A2 按通道转化 / A3 verification-driver 精度 / A4 inline 跟随
  B1 next-hint 采纳 / B2 多门禁连刷 / B3 分号续行
  C1–C3 门禁命令形态（分号+grep 掩蔽 / 管道截断 / 合规）
  D1 refs-critical 下钻（按 host 分层；无声明时 n/a）
  E 封印后归因行 / 无 session 占比 / resolve_path 分布
  F hazard 双投递 / 事件 / 放行率
  G coverage fail 后转 pass / cheat-scan / unused-scan fail
--json 输出带口径字段（dedup/join/follow/drill 窗口）的机器面，落到 evals/ 作基线；
回测只比较同口径、同版本的两份 JSON。判定逻辑全部是纯函数（internal/harnessaudit）。`,
	RunE: runEvalHarnessAudit,
}

func runEvalHarnessAudit(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return fmt.Errorf("load checklog: %w", err)
	}
	calls, err := toolusage.LoadAllAll(root)
	if err != nil {
		return fmt.Errorf("load toollog: %w", err)
	}
	tasks, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: task states unavailable (%v) — E post-seal window and D host stratification degrade\n", err)
	}
	var events []hazard.Event
	if p, perr := findProject(); perr == nil {
		if evs, lerr := hazard.LoadEvents(p); lerr == nil {
			events = evs
		}
	}
	refs := map[string][]string{}
	if dir, _, rerr := skillscanonical.Resolve(cmd.Root().Version); rerr == nil && dir != "" {
		refs = harnessaudit.LoadRefsCritical(dir)
	}

	rep := harnessaudit.Build(harnessaudit.Input{
		Entries:      entries,
		Calls:        calls,
		Tasks:        tasks,
		Hazards:      events,
		RefsCritical: refs,
		Version:      cleanVersion(cmd.Root().Version),
	})
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	fmt.Print(harnessaudit.Render(rep))
	return nil
}

func init() {
	evalHarnessAuditCmd.Flags().Bool("json", false, "输出 JSON（含口径字段，供 evals/ 基线与两机回测）")
	evalCmd.AddCommand(evalHarnessAuditCmd)
}

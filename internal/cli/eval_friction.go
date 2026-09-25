package cli

// eval_friction.go — W2 摩擦净值实验架（脚本化模式）。
//
// SpecBench gap 的开/关对照在脚本化规模落地：同一批诱饵用例（golden 语料，
// kind=defective）在「门禁开」与「既有逃生舱全开」两臂下各实跑一遍，量四个数：
// 拦截率（ON 臂 flagged 占比）、残余（OFF 臂仍被拦占比——hazard 无 env 逃生是
// 刻意设计，故 hazard 类用例在 OFF 臂预期仍拦）、两臂墙钟均值与开销差。
// 这是「门禁摩擦 < 阻止返工」价值主张的量化基座，也是 lite 默认化的前置证据。
//
// 诚实边界：脚本化臂量的是【检查器成本】（hook 墙钟），不是端到端 agent 任务
// 的返工成本——后者需要真实模型运行（FORGE_EVAL_MODEL 通道），不在本命令范围。
// OFF 臂 = 既有逃生舱全开，不是「forge 全关」（hazard 的 HITL confirm 链无 env
// 逃生，刻意设计——自动化的 hook 拦截才可 env 关）。

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/MjxUpUp/Forge/internal/evalkit"
	"github.com/spf13/cobra"
)

// gatesOffEnv 是 OFF 臂的逃生舱全开环境（hazard 无 env 逃生——刻意设计，
// 报告按 gate 分组呈现这一残余）。
func gatesOffEnv() []string {
	return []string{
		"FORGE_TASK_GATE=disable",
		"FORGE_HELDOUT=disable",
		"FORGE_TEST_COVERAGE=disable",
		"FORGE_DOC_GATE=disable",
		"FORGE_ACCEPTANCE_GATE=disable",
		"FORGE_RECURRENT_HARDEN=disable",
		"FORGE_SKILL_TRIGGER=0",
		"FORGE_CONVENTIONS_LINT=0",
	}
}

type frictionRow struct {
	ID         string        `json:"id"`
	Gate       string        `json:"gate"`
	OnFlagged  bool          `json:"on_flagged"`
	OffFlagged bool          `json:"off_flagged"`
	OnWall     time.Duration `json:"on_wall_ns"`
	OffWall    time.Duration `json:"off_wall_ns"`
	Overhead   time.Duration `json:"overhead_ns"`
	Err        string        `json:"error,omitempty"`
}

func runEvalFriction(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	corpus, _ := cmd.Flags().GetString("corpus")
	timeoutStr, _ := cmd.Flags().GetString("timeout")
	if corpus == "" {
		corpus = evalAssetPath("golden")
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return fmt.Errorf("BLOCKED: 非法 timeout %q（示例 120s）", timeoutStr)
	}
	forgeBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("BLOCKED: 解析 forge 二进制失败: %v", err)
	}

	cases, err := evalkit.LoadGoldenDir(corpus)
	if err != nil {
		return fmt.Errorf("BLOCKED: 读语料失败: %v", err)
	}
	var bait []evalkit.GoldenCase
	for _, c := range cases {
		if c.Kind == "defective" { // 只有缺陷诱饵进实验——clean 用例量的是误拦，属另一实验
			bait = append(bait, c)
		}
	}
	if len(bait) == 0 {
		return fmt.Errorf("BLOCKED: 语料中无 defective 诱饵用例")
	}

	rows := make([]frictionRow, 0, len(bait))
	var onTotal, offTotal, onHits, offHits time.Duration
	onFlaggedN, offFlaggedN := 0, 0
	for _, c := range bait {
		onFlag, onWall, errOn := evalkit.FrictionProbeArm(c, forgeBin, nil, timeout)
		offFlag, offWall, errOff := evalkit.FrictionProbeArm(c, forgeBin, gatesOffEnv(), timeout)
		row := frictionRow{ID: c.ID, Gate: c.Gate, OnFlagged: onFlag, OffFlagged: offFlag, OnWall: onWall, OffWall: offWall, Overhead: onWall - offWall}
		if errOn != nil {
			row.Err = "on: " + errOn.Error()
		}
		if errOff != nil {
			if row.Err != "" {
				row.Err += "; "
			}
			row.Err += "off: " + errOff.Error()
		}
		if errOn == nil {
			onTotal += onWall
			if onFlag {
				onHits++
			}
		}
		if errOff == nil {
			offTotal += offWall
			if offFlag {
				offHits++
			}
		}
		if errOn == nil && onFlag {
			onFlaggedN++
		}
		if errOff == nil && offFlag {
			offFlaggedN++
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	if asJSON {
		out, _ := json.MarshalIndent(map[string]any{
			"cases": rows,
			"summary": map[string]any{
				"on_interception": fmt.Sprintf("%d/%d", onFlaggedN, len(bait)),
				"off_residual":    fmt.Sprintf("%d/%d", offFlaggedN, len(bait)),
				"on_mean_ns":      onTotal / time.Duration(len(bait)),
				"off_mean_ns":     offTotal / time.Duration(len(bait)),
			},
		}, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("摩擦净值实验（脚本化双臂；诱饵 %d 条；OFF 臂=既有逃生舱全开，hazard 的 HITL 链无 env 逃生属刻意设计）\n\n", len(bait))
	fmt.Printf("  %-44s %-8s %-8s %10s %10s %10s\n", "诱饵", "ON", "OFF", "ON 墙钟", "OFF 墙钟", "开销")
	for _, r := range rows {
		onS, offS := "pass", "pass"
		if r.OnFlagged {
			onS = "FLAGGED"
		}
		if r.OffFlagged {
			offS = "FLAGGED"
		}
		if r.Err != "" {
			onS, offS = "ERR", "ERR"
		}
		fmt.Printf("  %-44s %-8s %-8s %8dms %8dms %8dms\n", r.ID, onS, offS,
			r.OnWall.Milliseconds(), r.OffWall.Milliseconds(), r.Overhead.Milliseconds())
	}
	onMean := onTotal / time.Duration(len(bait))
	offMean := offTotal / time.Duration(len(bait))
	fmt.Printf("\n拦截率（ON）%d/%d ｜ 残余（OFF）%d/%d ｜ 检查器开销均值 %v（ON-OFF）\n",
		onFlaggedN, len(bait), offFlaggedN, len(bait), onMean-offMean)
	fmt.Println("诚实边界：本实验量检查器成本（hook 墙钟），非端到端 agent 返工成本——后者走 FORGE_EVAL_MODEL 通道的模型臂。")
	return nil
}

package cli

// skills_battery.go — battery subcommand: held-out regression battery over all anchored
// skill baselines in the resolved eval dir (user-level by default, --dir/FORGE_EVAL_DIR
// for repo-level scope — see battery.go's scope-honesty note). Aggregates "any anchored
// skill regressed vs baseline?" into one command; --gate gives it teeth (exit 4 on any
// reject) for release/CI use.
//
// Field consensus (AutoDesign Eq 6 / held-out acceptance gating): accept only if no
// regression vs baseline. Per-skill eval-report answers this per skill; the battery answers
// it for every anchored skill at once, at the moment it matters (before release/merge).
//
// skills_battery.go — battery 子命令：held-out 回归电池，覆盖解析出的 eval 目录（默认
// 用户级，--dir/FORGE_EVAL_DIR 可切仓库级——见 battery.go 的范围诚实性注释）里所有已锚定
// baseline 的 skill。把「有没有任何已锚定 skill 相对 baseline 回归」聚合成一条命令；
// --gate 给它牙齿（任一 reject 则 exit 4），供发版/CI 使用。
//
// 领域共识（AutoDesign Eq 6 / held-out 验收门禁）：不回归才接受。单 skill 的
// eval-report 逐个回答；电池在要紧时刻（发版/合并前）对所有已锚定 skill 一次答完。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var skBattGate bool
var skBattJSON bool

var skillsBatteryCmd = &cobra.Command{
	Use:   "battery",
	Short: "回归电池：所有已锚定 baseline 的 skill 一次判完（用户级 eval 数据，任一回归即 reject）",
	Long: `聚合每个已锚定 baseline 的 skill（latest run vs baseline），判据与
eval-report 同源（JudgeSkillAccept 单一真相源）：

  forge skills battery            # 人读报告
  forge skills battery --json     # 机器可读 BatteryReport
  forge skills battery --gate     # 门禁模式：任一 skill reject → BLOCKED(stderr) + exit 4

范围：读默认 eval 目录（~/.forge/evals，FORGE_EVAL_DIR/--dir 可覆盖——仓库内
evals/ 目录让 CI 按仓库隔离跑电池）——默认覆盖本机所有已锚定 skill，非按仓库
隔离。零锚定机器上电池为空：--gate 的 exit 0 意为「没检查任何东西」而非
「已验证无回归」（该情形会打印显式 vacuous 提示）。

覆盖缺口显性化：有 run 但未锚定 baseline 的 skill 列为 unanchored（advisory，
不阻断）——用 eval-baseline 锚定后纳入电池保护。`,
	RunE: runSkillsBattery,
}

func runSkillsBattery(cmd *cobra.Command, args []string) error {
	dir, err := evalDataDir()
	if err != nil {
		return err
	}
	rep, err := skillseval.BuildBattery(dir)
	if err != nil {
		return err
	}

	if skBattJSON {
		out, merr := json.MarshalIndent(rep, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Println(string(out))
	} else {
		printBatteryReport(rep)
	}

	// Gate contract (aligned with `skills audit --gate`): non-zero exit only in --gate mode;
	// report-only invocations stay exit 0 so the battery can run anywhere without side effects.
	// The BLOCKED line goes to STDERR so `--json --gate | jq .` never receives non-JSON bytes
	// (exit code + stderr carry the gate signal; stdout stays the data channel).
	//
	// 门禁契约（对齐 `skills audit --gate`）：非零退出只在 --gate 模式；纯报告调用保持
	// exit 0，电池可在任何场合运行而无副作用。BLOCKED 行走 STDERR——`--json --gate |
	// jq .` 不再吃到非 JSON 字节（退出码 + stderr 承载门禁信号；stdout 只做数据通道）。
	if skBattGate && rep.GateBlocked {
		fmt.Fprintln(os.Stderr, "BLOCKED: 回归电池有 reject 项——先修复回归或重锚 baseline 再放行")
		os.Exit(4)
	}
	// Vacuous-gate honesty (review F3): an empty battery exits 0 without having checked
	// anything — on a fresh CI runner that must not read as "no regression verified".
	// stderr + exit 0 keeps the advisory channel separate from the data channel.
	//
	// 空电池诚实性（审查 F3）：零锚定电池没检查任何东西就 exit 0——在崭新 CI runner 上
	// 不该被读成「已验证无回归」。stderr + exit 0 保持提示通道与数据通道分离。
	if skBattGate && rep.Total == 0 {
		fmt.Fprintln(os.Stderr, "ADVISORY: 电池为空（无已锚定 baseline）——exit 0 意为未检查任何 skill，非已验证无回归；eval-baseline 锚定后才有保护")
	}
	return nil
}

// printBatteryReport renders the human-readable battery report: rejected rows first (signal
// first), then advisory rows, then accepts; unanchored coverage gap last.
//
// printBatteryReport 渲染人读电池报告：先 reject 行（信号优先），再 advisory 行，再
// accept 行；未锚定覆盖缺口收尾。
func printBatteryReport(rep *skillseval.BatteryReport) {
	fmt.Printf("回归电池：%d 个已锚定 skill（accepted=%d rejected=%d）\n", rep.Total, rep.Accepted, rep.Rejected)
	for _, r := range rep.Skills {
		if r.Accept {
			continue
		}
		fmt.Printf("  🔴 reject %s  regressions=%d net=%d health=%.2f\n", r.Skill, r.RegressionCount, r.NetRegressions, r.HealthScore)
		fmt.Printf("     %s\n", strings.Join(r.Reasons, "; "))
	}
	for _, r := range rep.Skills {
		if !r.Accept || len(r.Reasons) == 0 {
			continue
		}
		// Advisory accepts: judgment impossible (no run / stale anchor) or not comparable —
		// surfaced so "accept" is never misread as "verified no regression".
		//
		// advisory accept：判定不可能（无 run/标记过期）或不可比——浮出以免 accept 被
		// 误读成「已验证无回归」。
		fmt.Printf("  🟡 accept(advisory) %s  %s\n", r.Skill, strings.Join(r.Reasons, "; "))
	}
	for _, r := range rep.Skills {
		if !r.Accept || len(r.Reasons) != 0 {
			continue
		}
		fmt.Printf("  ✅ accept %s  latest=%s health=%.2f\n", r.Skill, r.LatestRun, r.HealthScore)
	}
	if len(rep.Unanchored) > 0 {
		fmt.Printf("  ⚪ unanchored（有 run 未锚定 baseline，不在电池保护内）: %s\n", strings.Join(rep.Unanchored, ", "))
	}
	if rep.Total == 0 {
		fmt.Println("（无已锚定 baseline——eval-baseline 锚定后电池生效）")
	}
}

func init() {
	skillsBatteryCmd.Flags().BoolVar(&skBattGate, "gate", false, "门禁模式：任一 skill reject → BLOCKED + exit 4")
	skillsBatteryCmd.Flags().BoolVar(&skBattJSON, "json", false, "输出机器可读 BatteryReport JSON")
	skillsCmd.AddCommand(skillsBatteryCmd)
}

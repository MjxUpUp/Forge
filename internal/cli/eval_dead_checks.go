package cli

// eval_dead_checks.go — W0.1 死检查报告（`forge eval dead-checks`）。
//
// 「hook 太多太重」痛点的数据驱动减法工具：聚合 checklog 台账 + hazard 事件，
// 按 W0 宪法给每个检查分三态——live（观察窗内有拦截/fail）、dead-candidate
// （触发充足但拦截为零 → 减法候选）、insufficient-data（观察不足，继续观察）。
//
// 诚实边界（必读）：本报告只覆盖【写 checklog 的检查面】（门禁/披露类）。嵌入式
// hook 的每次 PASS 不逐次落账是刻意设计（每工具调用落账的开销本身就是要消的重量），
// 因此 hook 类检查的「触发率」是下界——blocked 计数为零 + 观察充足的检查才进
// dead-candidate，观察不足的如实标注 insufficient-data，绝不把无数据包装成「已证死」。
//
// eval_dead_checks.go — W0.1 dead-check report (`forge eval dead-checks`).
// Data-driven slimming: aggregates the checklog ledger + hazard events and
// classifies each check live / dead-candidate / insufficient-data per the W0
// constitution. Honest boundary: only checklog-writing surfaces are covered
// (embedded hooks deliberately do not ledger every PASS — that per-call cost is
// itself weight we are removing), so trigger counts for hook-shaped checks are
// lower bounds; insufficient observation is reported as such, never dressed up
// as "proven dead".

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/spf13/cobra"
)

// deadCheckMinObservations 是 dead-candidate 判定的最小观察数：观察窗内同
// check 台账条目 ≥ 此值且拦截/fail 为零才谈「死」——数字对齐 W0 宪法「观察窗
// 90 天」，30 条约为每 3 天一次的最低存在感。
const deadCheckMinObservations = 30

// deadCheckRow 是报告的一行。
type deadCheckRow struct {
	Check     string `json:"check"`
	Observed  int    `json:"observed"`
	Blocked   int    `json:"blocked"`
	Warn      int    `json:"warn"`
	Pass      int    `json:"pass"`
	Skipped   int    `json:"skipped"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Verdict   string `json:"verdict"` // live | dead-candidate | insufficient-data
}

func runEvalDeadChecks(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	windowStr, _ := cmd.Flags().GetString("window")
	window, err := parseDeadCheckWindow(windowStr)
	if err != nil {
		return err
	}
	root := evalRepoRoot()
	cutoff := time.Now().Add(-window)

	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return fmt.Errorf("BLOCKED: 读取 checklog 台账失败: %v", err)
	}

	type agg struct {
		observed, blocked, warn, pass, skipped int
		first, last                            time.Time
	}
	byCheck := map[string]*agg{}
	for _, e := range entries {
		if e.RecordedAt.Before(cutoff) {
			continue
		}
		a := byCheck[string(e.Check)]
		if a == nil {
			a = &agg{}
			byCheck[string(e.Check)] = a
		}
		// 审查 m2：观察数只计真正执行过的检查（Checked=false 是 skip，不是
		// 证据）——skip 30 次零评估的检查不得被推成 dead-candidate。
		if !e.Checked {
			a.skipped++
			continue
		}
		a.observed++
		switch {
		case e.EffectiveLevel().IsFailure():
			a.blocked++
		case e.EffectiveLevel() == checklog.LevelWarn:
			a.warn++
		default:
			a.pass++
		}
		if a.first.IsZero() || e.RecordedAt.Before(a.first) {
			a.first = e.RecordedAt
		}
		if e.RecordedAt.After(a.last) {
			a.last = e.RecordedAt
		}
	}

	rows := make([]deadCheckRow, 0, len(byCheck))
	for check, a := range byCheck {
		rows = append(rows, deadCheckRow{
			Check: check, Observed: a.observed, Blocked: a.blocked, Warn: a.warn,
			Pass: a.pass, Skipped: a.skipped,
			FirstSeen: a.first.Format("2006-01-02"), LastSeen: a.last.Format("2006-01-02"),
			Verdict: classifyDeadCheck(a.observed, a.blocked),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Verdict != rows[j].Verdict {
			return rows[i].Verdict == "dead-candidate"
		}
		return rows[i].Observed > rows[j].Observed
	})

	// hazard 侧（hazard-guard 的拦截/放行/数据上下文事件有独立台账，单独聚合——
	// hazard-guard 的 checklog 触发率同样是下界，hazard 台账才是它的真实触发面）。
	hazardBlocks, hazardReleases, hazardData := 0, 0, 0
	hazardReadErr := ""
	if p, perr := forgedata.ProjectFor(root); perr != nil {
		hazardReadErr = "project 解析失败: " + perr.Error()
	} else if evs, eerr := hazard.LoadEvents(p); eerr != nil {
		hazardReadErr = "事件台账读取失败: " + eerr.Error()
	} else {
		for _, e := range evs {
			if e.Ts.Before(cutoff) {
				continue
			}
			switch e.Type {
			case hazard.EventBlock:
				hazardBlocks++
			case hazard.EventRelease:
				hazardReleases++
			case hazard.EventData:
				hazardData++
			}
		}
	}

	if asJSON {
		out, _ := json.MarshalIndent(map[string]any{
			"window_days":     int(window.Hours() / 24),
			"checks":          rows,
			"hazard_blocks":   hazardBlocks,
			"hazard_releases": hazardReleases,
			"hazard_data":     hazardData,
		}, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("死检查报告（观察窗 %d 天；最小观察 %d 条）\n", int(window.Hours()/24), deadCheckMinObservations)
	fmt.Println("诚实边界：仅覆盖写 checklog 的检查面；嵌入式 hook 的 PASS 不逐次落账（刻意设计），hook 类触发率为下界。")
	fmt.Println()
	for _, r := range rows {
		marker := ""
		switch r.Verdict {
		case "dead-candidate":
			marker = " ← 减法候选（观察充足、拦截为零）"
		case "live":
			marker = ""
		default:
			marker = " （观察不足，继续观察）"
		}
		fmt.Printf("  %-28s 观察 %4d / 拦截 %3d / warn %3d / pass %4d / 跳过 %3d  [%s]%s\n",
			r.Check, r.Observed, r.Blocked, r.Warn, r.Pass, r.Skipped, r.Verdict, marker)
	}
	fmt.Printf("\nhazard-guard 独立台账：拦截 %d / confirm 放行 %d / 数据上下文放行 %d\n", hazardBlocks, hazardReleases, hazardData)
	if hazardReadErr != "" {
		fmt.Printf("  ⚠ hazard 台账读取失败（%s）——hazard 计数不可信，零值不代表真零拦截。\n", hazardReadErr)
	} else if hazardBlocks == 0 {
		fmt.Println("  ⚠ 观察窗内 hazard-guard 零拦截——确认观察窗覆盖了真实高危操作，而非观察不足。")
	}
	fmt.Println("\n处置规则（W0 宪法）：dead-candidate 须人工复核归因（台账是否覆盖其真实触发面）后才可下线；下线走 census/ADR 留痕，恢复是配置级操作。")
	return nil
}

// classifyDeadCheck 三态判定（纯函数，单测钉住）：有拦截/fail 即 live（拦截是
// 存在性的决定性证据，与观察量无关）；零拦截但观察不足 → insufficient-data
// （绝不把无数据包装成「已证死」）；观察充足且零拦截 → dead-candidate。
func classifyDeadCheck(observed, blocked int) string {
	if blocked > 0 {
		return "live"
	}
	if observed < deadCheckMinObservations {
		return "insufficient-data"
	}
	return "dead-candidate"
}

// parseDeadCheckWindow 解析观察窗（"90d" / "720h" / 裸天数），非法输入 fail-closed。
func parseDeadCheckWindow(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 90 * 24 * time.Hour, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if len(s) > 1 && strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "d"), "%d", &days); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return 0, fmt.Errorf("BLOCKED: 非法观察窗 %q（示例：90d / 2160h）", s)
}

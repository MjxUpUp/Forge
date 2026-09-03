package evalkit

// report.go — 季度自评测报告（P4，docs/design/forge-evaluation-system.md §六）：
// 汇集最近一次 dashboard 快照、golden/traps/decompose 报告，产出带环比占位与
// 诚实性页脚的 Markdown。报告只汇编已落盘的证据——绝不现场造数。
//
// report.go — quarterly self-evaluation report: assembles the latest dashboard
// snapshot, golden/traps/decompose reports into Markdown with period-over-period
// placeholder and the honesty footer. The report only compiles evidence already
// on disk — it never fabricates numbers on the fly.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReportSection is one assembled evidence section.
//
// ReportSection 是报告的一个证据小节。
type ReportSection struct {
	Kind string // dashboard | golden | traps | decompose
	Path string
	Body string
}

// BuildQuarterlyReport scans <evalDir>/forge/ for the latest report of each
// kind and renders the quarterly Markdown. Missing evidence is stated as
// missing (never backfilled).
//
// BuildQuarterlyReport 扫描 <evalDir>/forge/ 下各类型最新报告并渲染季度
// Markdown。缺失的证据如实标注缺失（绝不补造）。
func BuildQuarterlyReport(evalDir string, quarter string, now time.Time) (string, []string, error) {
	dir := evalDataDir(evalDir)
	var missing []string
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Forge 自评测季报（%s）\n\n生成时间：%s\n\n", quarter, now.UTC().Format("2006-01-02 15:04")))
	b.WriteString("> 页脚纪律（调研结论）：单边引用任何一个数字都是方法性误导——本报告所有指标附误用注记；两轨数字冲突时以 finding 呈现而非取齐。\n\n")

	// dashboard 快照。
	snap := latestJSON(filepath.Join(dir, "snapshots"), "snapshot-")
	if snap == "" {
		missing = append(missing, "dashboard 快照（forge eval dashboard 尚未运行）")
	} else {
		b.WriteString(sectionFor("dashboard 遥测快照（C4/C7）", snap))
	}

	// golden / traps / decompose 报告。
	for _, kind := range []struct{ prefix, title string }{
		{"golden-report-", "门禁 golden 基线（C1/C2）"},
		{"trap-report-", "对抗陷阱（C2）"},
		{"decompose-", "方差分解（Track A）"},
		{"judge-audit-", "判分器审计（C2）"},
		{"resume-drill-", "接续演练（C3）"},
	} {
		p := latestJSON(dir, kind.prefix)
		if p == "" {
			missing = append(missing, kind.title+"（尚无报告）")
			continue
		}
		b.WriteString(fmt.Sprintf("## %s\n\n证据文件：%s\n\n", kind.title, filepath.Base(p)))
		b.WriteString(sectionSummary(kind.prefix, p))
	}
	b.WriteString("\n## 缺失证据\n\n")
	for _, m := range missing {
		b.WriteString(fmt.Sprintf("- %s\n", m))
	}
	b.WriteString("\n## 环比\n\n- 首季为基线季：环比自下一季度开始（上一季报告缺失时如实标注）。\n")
	return b.String(), missing, nil
}

// sectionSummary extracts a compact summary line from a report JSON.
//
// sectionSummary 从报告 JSON 提取一行紧凑摘要。
func sectionSummary(prefix, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("- 读取失败：%v\n", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		return fmt.Sprintf("- 解析失败：%v\n", err)
	}
	var parts []string
	for _, key := range []string{"capture_rate", "precision", "false_positive", "fidelity"} {
		if v, ok := generic[key].(map[string]any); ok {
			parts = append(parts, fmt.Sprintf("%s %v/%v（value=%.3f）", key, v["numerator"], v["denominator"], v["value"]))
		}
	}
	if hv, ok := generic["decomposition"].(map[string]any); ok {
		parts = append(parts, fmt.Sprintf("HV/MV=%v reversals=%v", hv["hv_over_mv"], hv["reversals"]))
	}
	if len(parts) == 0 {
		return "- （报告已落盘；摘要想从结构化字段读——散文解析是反契约）\n"
	}
	return "- " + strings.Join(parts, "；") + "\n"
}

func sectionFor(kind, path string) string {
	return fmt.Sprintf("## %s\n\n证据文件：%s\n\n", kind, filepath.Base(path))
}

// latestJSON returns the newest file in dir starting with prefix ("": none).
//
// latestJSON 返回 dir 内以 prefix 开头的最新文件（无则空串）。
func latestJSON(dir, prefix string) string {
	matches, err := filepath.Glob(filepath.Join(dir, prefix+"*.json"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}
